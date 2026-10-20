package world

import (
	"context"
	"strings"
	"time"

	"sa/server/internal/battle"
	"sa/server/internal/data"
	"sa/server/internal/proto"
	"sa/server/internal/store"
)

const (
	// EncounterDivisor：每走一步 rand()%120 < 当前遇敌值 就遇敌（char_walk.c）。
	// setup.cf 的 ENEMYACTION=10 会把它放大 10 倍（_ENEMY_ACTION），那是那台私服调低遇敌率的设定，这里用原版的 120。
	EncounterDivisor = 120
	// TurnTime 是每回合选指令的时间（客户端 BATTLE_CNT_DOWN_TIME 30 秒）。
	TurnTime = 30 * time.Second
	// MaxLevel 是 setup.cf 的 MAXLEVEL。
	MaxLevel = 140
	// LevelUpPoints：每升一级得到的点数（battle.c：没有 _SKILLUPPOINT_CF 时为 3）。
	LevelUpPoints = 3
	// CharmPerLevel：升级时魅力增加（CH_FIX_PLAYERLEVELUP），上限 100。
	CharmPerLevel = 2
)

// battleRoom 是一场战斗（BattleArray）：一起打的玩家（组队时最多 5 人，站 0..4，各自的宠物站 +5）共用一个 battle.Battle。
type battleRoom struct {
	b       *battle.Battle
	members []*entity // 还在这场战斗里的玩家（第一个是队长）
	turn    int
	timer   *time.Timer
	started time.Time
	// npc 是开这场战斗的 NPC（BattleArray.createindex，遇敌为 nil）；onEnd 在结果送出之后以队长的 "win" / "lose" / "escape" 调用
	// （原版 BattleArray.WinFunc 只在打赢时由 BATTLE_Finish 调用，见 startBattleWith）。
	npc   *entity
	onEnd func(result string)
	no    int  // 战斗编号（地图上的战斗记号）
	help  bool // 我方开着求救（BSIDE_FLG_HELP_OK），别人可以加入
	field int  // 战斗背景（加入的人用同一张）
	// pvp 是玩家对玩家（BATTLE_TYPE_P_vs_P，duel.go）；charaLost：已经有玩家倒下（BATTLE_FLG_CHARALOST，之后逃跑就要扣 DP）
	pvp, charaLost bool
}

// member 是站在 slot（或它的宠物位置 slot+5）的玩家。
func (r *battleRoom) member(slot int) *entity {
	for _, m := range r.members {
		if m.fight != nil && (m.fight.slot == slot || m.fight.slot+5 == slot) {
			return m
		}
	}
	return nil
}

// fight 是一个玩家在一场战斗里的部分。
type fight struct {
	room *battleRoom
	b    *battle.Battle // = room.b
	slot int            // 玩家在战斗中的位置
	cmd  *battle.Command
	// pet 是出战的宠物在宠物栏的格位（-1 没带），站在 slot+5；petCmd 是它这回合的指令
	pet    int
	petCmd *battle.Command
	swapTo int // 这回合要叫出的宠物格位（换宠物的指令）
	// itemsDirty：装备在战斗中坏掉或损坏等级变了，回合结束时重发道具栏
	itemsDirty bool
	petsDirty  bool // 宠物身上的道具磨损了（_TAKE_ITEMDAMAGE_FOR_PET）
	dp         int  // PvP 这场的 DP 增减（CHAR_WORKGETEXP）
	dead       bool // PvP 倒下时算过 DP 了（CHAR_ISDIE）
}

// rollEncounter 在走完一步后判定是否遇敌（char_walk.c CHAR_walk_check 的遇敌部分）：
// 遇敌值在区域的 [下限, 上限] 之间，每步 rand()%120 < 遇敌值 就遇敌并把遇敌值重置为下限，否则 +1。
func (w *World) rollEncounter(e *entity) {
	bd := w.game.Battle
	if bd == nil || e.fight != nil {
		return
	}
	a := bd.AreaAt(e.floor, e.x, e.y)
	if w.npcEnemyBump(e) { // 走到战斗 NPC 身上：不算遇敌，改由它开打（EVENY_npcEncount）
		return
	}
	if a == nil {
		return
	}
	// 驱魔中不遇敌（CHAR_walk：noenemy > 0 时整段跳过，遇敌值也不加）；8252 / 8253 / 8254 / 8256 号地图例外
	if e.noenemy > 0 && !(e.floor >= 8252 && e.floor <= 8256 && e.floor != 8255) {
		return
	}
	e.cep = max(a.ProbMin, min(a.ProbMax, e.cep))
	if w.rnd.IntN(EncounterDivisor) < e.cep {
		e.cep = a.ProbMin
		w.startBattle(e, a)
	} else if e.cep < a.ProbMax {
		e.cep++
	}
}

// startBattle 按遇敌区域生成敌人并开打（BATTLE_CreateVsEnemy）。
func (w *World) startBattle(e *entity, a *data.EncountArea) {
	specs := battle.PickEnemies(w.game.Battle, a, w.rnd)
	if len(specs) == 0 {
		return
	}
	enemies := make([]*battle.Fighter, len(specs))
	for i, s := range specs {
		enemies[i] = battle.NewEnemy(s, battle.EnemySlot(i), w.game.Battle.Skills, w.rnd)
	}
	w.startBattleWith(e, enemies, nil, nil)
}

// startBattleWith 是 BATTLE_CreateVsEnemy 共用的部分：e 对已经产生好、排好位置的 enemies 开打。
// npc、onEnd 见 fight；有 onEnd 的战斗不判定偷袭（BATTLE_SurpriseCheck：WinFunc 不为 NULL 时返回 0）。
func (w *World) startBattleWith(e *entity, enemies []*battle.Fighter, npc *entity, onEnd func(result string)) {
	b := battle.New(w.rnd)
	w.battleSeq++
	room := &battleRoom{b: b, started: w.now(), npc: npc, onEnd: onEnd, no: w.battleSeq}
	for i, m := range w.battleMembers(e) { // 队长在 0，队员依序 1..4（BATTLE_CreateVsEnemy 的 BATTLE_PartyNewEntry）
		w.joinRoom(room, m, i)
	}
	for _, f := range enemies {
		b.Add(f)
	}
	b.CanCapture = func(a, d *battle.Fighter) bool { m := room.member(a.Slot); return m != nil && w.canCapture(m, d) }
	b.NoAbduct = npc != nil || onEnd != nil         // BATTLE_Abduct：有 WinFunc 的战斗带不走人
	b.OnHit = func(a, d *battle.Fighter, dmg int) { // 装备磨损（crush.go）：打人的与挨打的各自的装备
		for _, m := range room.members {
			if m.fight != nil {
				w.battleCrush(m, m.fight, a, d, dmg)
			}
		}
	}
	b.OnTooth = func(a, d *battle.Fighter) {
		if m := room.member(d.Slot); m != nil && d.Slot == m.fight.slot {
			w.battleTooth(m, m.fight)
		}
	}
	b.AttMagics, b.Magics = w.game.Battle.AttMagics, w.game.Battle.Magics
	// 熟练度等讯息原版用 CHAR_talkToCli 送给施法者 / 受害者，宠物没有连线所以只有玩家自己看得到
	b.Notice = func(slot int, text string) {
		if m := room.member(slot); m != nil && slot == m.fight.slot {
			w.sysMsg(m, text, colorRed)
		}
	}
	surprise := 0 // 1 偷袭成功（敌人第一回合不动），2 被偷袭（自己第一回合不动）
	if onEnd == nil {
		surprise = b.Surprise(e.luck)
	}
	field := w.battleField(e)
	room.field = field
	for _, m := range room.members {
		w.sendBattleStart(m, field, surprise)
		w.broadcastMark(m) // CHAR_sendBattleEffect(ON)
	}
	if surprise == 2 {
		w.resolveTurn(room) // 被偷袭：第一回合不能下指令，直接结算
		return
	}
	w.armTurnTimer(room, 0)
}

// joinRoom 让 m 站到位置 slot（宠物在 slot+5）。
func (w *World) joinRoom(room *battleRoom, m *entity, slot int) {
	w.cancelTrade(m)
	m.clearAction()
	b := room.b
	p := w.playerFighter(m, slot)
	b.Add(p)
	pet := -1
	if i := m.pets.Battle; i >= 0 && m.pets.Pets[i] != nil && m.pets.Pets[i].HP > 0 {
		pet = i
		b.Add(w.petFighter(m, i, slot+5))
	}
	m.fight = &fight{room: room, b: b, slot: slot, pet: pet}
	room.members = append(room.members, m)
}

// sendBattleStart 送 m 这场战斗的全部人（BATTLE_CharSendOne 的 B| 字串开头）。
func (w *World) sendBattleStart(m *entity, field, surprise int) {
	b := m.fight.b
	msg := &proto.BattleStart{Field: field, MySlot: m.fight.slot, TurnTime: int(TurnTime / time.Millisecond), Surprise: surprise, Pvp: m.fight.room.pvp}
	for _, f := range b.F {
		if f != nil {
			msg.Fighters = append(msg.Fighters, w.fighterInfo(m.fight.room, f))
		}
	}
	m.client.Send(msg)
}

// fighterInfo 是一个战斗者在 B| 字串里的资料。
func (w *World) fighterInfo(room *battleRoom, f *battle.Fighter) proto.BattleFighter {
	bf := proto.BattleFighter{Slot: f.Slot, Name: f.Name, Sprite: f.Sprite,
		Level: f.Level, HP: f.HP, MaxHp: f.MaxHP, Mp: f.MP, MaxMp: f.MaxMP}
	if r := f.Ride; r != nil {
		// 落马后换回拿武器的形象（BATTLE_changeRideImage）
		bf.RideName, bf.RideLevel, bf.RideHp, bf.RideMaxHp = r.Name, r.Level, r.HP, r.MaxHP
		if o := room.member(f.Slot); o != nil {
			bf.FallSprite = w.weaponSprite(o)
		}
	}
	return bf
}

// battleField 是 BATTLE_getBattleFieldNo：脚下地块在 battlemap.txt 里对应的三个背景随机取一个。
func (w *World) battleField(e *entity) int {
	f := w.game.Floors[e.floor]
	if f == nil || !f.In(e.x, e.y) {
		return 0
	}
	m := w.game.Battle.BattleMap[int(f.Tiles[e.y*f.W+e.x])]
	return m[w.rnd.IntN(3)]
}

// workStats 是角色的工作值（CHAR_complianceParameter）：基本状态换算（点数是原版内部值 /100）再加上装备（ITEM_equipEffect）。
type workStats struct {
	Atk, Def, Quick, MaxHP, MaxMP, Charm, Luck, Avoid, Critical int
	Attr                                                        [4]int
}

func (w *World) stats(e *entity) workStats {
	c := e.char
	d := battle.Derive(float64(c.Vital*100), float64(c.Str*100), float64(c.Tgh*100), float64(c.Dex*100))
	s := workStats{Atk: d.Atk, Def: d.Def, Quick: d.Quick, MaxHP: d.HP, MaxMP: 100, Charm: c.Charm, Luck: max(1, e.luck),
		Attr: [4]int{c.Earth * 10, c.Water * 10, c.Fire * 10, c.Wind * 10}}
	if e.inv == nil {
		return s
	}
	mods, attr := w.equipMods(e.inv[:data.EquipSlots], [5]int{d.Atk, d.Def, d.Quick, d.HP, 100})
	s.Atk = max(0, s.Atk+mods[data.ModAtk])
	s.Def = max(0, s.Def+mods[data.ModDef])
	s.Quick = max(0, s.Quick+mods[data.ModQuick])
	s.MaxHP = max(1, s.MaxHP+mods[data.ModHP])
	s.MaxMP = max(0, min(1000, s.MaxMP+mods[data.ModMP]))
	s.Luck = max(1, min(5, s.Luck+mods[data.ModLuck]))
	s.Charm = max(0, min(100, s.Charm+mods[data.ModCharm]))
	s.Avoid, s.Critical = mods[data.ModAvoid], mods[data.ModCritical]
	for i := range s.Attr {
		s.Attr[i] = max(0, min(100, s.Attr[i]+attr[i]))
	}
	return s
}

func (w *World) maxHPMP(e *entity) (int, int) {
	s := w.stats(e)
	return s.MaxHP, s.MaxMP
}

// playerFighter 把角色换算成战斗数值。
func (w *World) playerFighter(e *entity, slot int) *battle.Fighter {
	c := e.char
	s := w.stats(e)
	hp := c.HP
	if hp < 0 || hp > s.MaxHP {
		hp = s.MaxHP
	}
	f := &battle.Fighter{Slot: slot, Name: c.Name, Sprite: e.sprite, Level: c.Level, HP: hp, MaxHP: s.MaxHP,
		MP: min(c.MP, s.MaxMP), MaxMP: s.MaxMP, Atk: s.Atk, Def: s.Def, Quick: s.Quick, Charm: s.Charm,
		Base: [4]float64{float64(c.Vital * 100), float64(c.Str * 100), float64(c.Tgh * 100), float64(c.Dex * 100)},
		Attr: s.Attr, Luck: s.Luck, Player: true, CharID: int(c.ID), Weapon: -1}
	if wp := w.weapon(e.inv); wp != nil {
		f.Weapon, f.AttackMin, f.AttackMax = wp.Type, wp.AttackMin, wp.AttackMax
	}
	f.Ride = w.mount(e)
	f.Pig, f.BaseSprite = e.char.Pig, w.weaponSprite(e) // 乌力化（item2.go）、变回自己用的形象
	if f.Pig > 0 {
		f.Ride = nil // 乌力不能骑宠
	}
	if e.magic == nil {
		e.magic = &battle.MagicSkill{}
	}
	f.Mag = e.magic
	return f
}

// armTurnTimer：玩家迟迟不下指令时按"无行动"结算。时限 = 客户端播放上一回合动画的时间 + 30 秒 + 余量。
func (w *World) armTurnTimer(room *battleRoom, events int) {
	if room.timer != nil {
		room.timer.Stop()
	}
	turn := room.turn
	d := TurnTime + time.Duration(events)*1500*time.Millisecond + 5*time.Second
	room.timer = time.AfterFunc(d, func() {
		w.Do(func() {
			if room.turn == turn && len(room.members) > 0 {
				w.resolveTurn(room)
			}
		})
	})
}

// BattleCommand 收下玩家这回合的指令，人和宠物的指令都到了（或宠物不在场）就结算。kind 为 item 时 item 是道具栏的格位。
// 玩家倒下时只等宠物的指令（原客户端倒下后直接跳到宠物的指令选单）。
func (w *World) BattleCommand(charID int, kind string, target, item int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || e.fight == nil || e.fight.cmd != nil || !e.fight.b.F[e.fight.slot].Alive() {
			return
		}
		c := battle.Command{Kind: battle.Kind(kind), Target: target}
		switch c.Kind {
		case battle.Attack, battle.Guard, battle.Escape, battle.None, battle.Capture:
		case battle.UseItem:
			// lssproto_B 的 "I|格位|目标"：目标要合道具的种类（ITEM_isTargetValid）；地图专用的道具由客户端挡（原版服务端不查）
			var def *data.ItemDef
			if item >= data.EquipSlots && item < data.ItemSlots && e.inv[item] != nil {
				def = w.game.Items[e.inv[item].ID]
			}
			if def == nil || def.UseFunc == "" || def.Field == 2 || !itemTargetValid(def, target, e.fight.slot) {
				c = battle.Command{Kind: battle.None}
				break
			}
			c.ItemFunc, c.ItemArg, c.ItemName, c.ItemSlot = def.UseFunc, def.Argument, def.Name, item
			if def.MagicID >= 0 {
				c.ItemMagic = w.game.Battle.Magics[def.MagicID]
			}
			if def.UseFunc == "ITEM_useMagic" {
				// ITEM_useMagic_Battle → MAGIC_DirectUse(…, 0)：咒术是参数的编号，MP 照头上装备的 magicusemp（没戴东西就施不出来）
				c.ItemMagic, c.MP = w.game.Battle.Magics[data.Atoi(def.Argument)], 0
				if head := e.inv[data.SlotHead]; head == nil || w.game.Items[head.ID] == nil {
					c.ItemMagic = nil
				} else {
					c.MP = w.game.Items[head.ID].MagicMP
				}
			}
		case battle.PetSwap:
			// lssproto_B 的 "S|n"：-1 收回，其余叫出第 n 格（骑着的不行，原版改为收回）；倒下的叫不出来（BATTLE_PetDefaultEntry）
			c.Target = -1
			e.fight.swapTo = -1
			if item >= 0 && item < PetSlots && item != e.pets.Ride && e.pets.Pets[item] != nil {
				e.fight.swapTo = item
				if e.pets.Pets[item].HP > 0 {
					c.Swap = w.petFighter(e, item, e.fight.slot+5)
				}
			}
		case battle.Magic:
			// lssproto_B 的 "J|装备位|目标"：装备着的道具上的咒术（MAGIC_GetArrayNo），目标要合咒术的种类（MAGIC_isTargetValid）
			m, mp := w.equipMagic(e, item)
			if m == nil || m.Field == 2 || !magicTargetValid(m, target) {
				c = battle.Command{Kind: battle.None}
				break
			}
			c.Magic, c.MP = m, mp
			if strings.Contains(m.Option, "SKILL") {
				c = w.attSkill(e, m, mp, target) // _ITEM_ATTSKILLMAGIC：装备的咒术其实是宠物技能
			}
		default:
			c = battle.Command{Kind: battle.None}
		}
		e.fight.cmd = &c
		w.tryResolve(e.fight.room)
	})
}

// BattlePetCommand 收下宠物这回合的指令：用第 skill 格的技能（lssproto_B 的 "W|技能|目标"），-1 为什么也不做。
// 只能用战斗中可用的技能（PETSKILL_FIELD_ALL / BATTLE）。
func (w *World) BattlePetCommand(charID, skill, target int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || e.fight == nil || e.fight.petCmd != nil {
			return
		}
		ft := e.fight
		pf := ft.b.F[ft.slot+5]
		if ft.pet < 0 || !pf.Alive() {
			return
		}
		c := battle.Command{Kind: battle.None}
		if skill >= 0 && skill < len(pf.Skills) {
			if s := pf.Skills[skill]; s != nil && (s.Field == 0 || s.Field == 1) {
				c = battle.Command{Kind: battle.Attack, Target: target, Skill: s}
			}
		}
		ft.petCmd = &c
		w.tryResolve(ft.room)
	})
}

// tryResolve：这场战斗里每个玩家还在场上的人与宠物都下了指令就结算（BATTLE_ActionSelectCheck）。
func (w *World) tryResolve(room *battleRoom) {
	for _, m := range room.members {
		ft := m.fight
		if ft.b.F[ft.slot].Alive() && ft.cmd == nil {
			return
		}
		if pf := ft.b.F[ft.slot+5]; ft.pet >= 0 && pf.Alive() && ft.petCmd == nil {
			return
		}
	}
	w.resolveTurn(room)
}

func (w *World) resolveTurn(room *battleRoom) {
	cmds := map[int]battle.Command{}
	for _, m := range room.members {
		ft := m.fight
		if ft.cmd != nil {
			cmds[ft.slot] = *ft.cmd
		}
		if ft.petCmd != nil {
			cmds[ft.slot+5] = *ft.petCmd
		}
	}
	ev := room.b.Resolve(cmds)
	for _, m := range room.members {
		w.afterMemberTurn(m, ev)
	}
	room.turn++
	msg := &proto.BattleTurn{Turn: room.turn, Events: make([]proto.BattleEvent, 0, len(ev))}
	for _, x := range ev {
		msg.Events = append(msg.Events, proto.BattleEvent{Actor: x.Actor, Kind: string(x.Kind), Target: x.Target,
			Result: string(x.Result), Damage: x.Damage, HP: x.HP, Dead: x.Dead, OK: x.OK, Skill: x.Skill,
			Status: x.Status, Guardian: x.Guardian, Combo: x.Combo, ActorHp: x.ActorHP, MpDamage: x.MPDamage, Guarded: x.Guarded,
			PetDamage: x.PetDamage, RideHp: x.RideHP, Fall: x.Fall, Effect: x.Effect, ActorMp: x.ActorMP,
			Name: x.Name, Sprite: x.Sprite, Level: x.Level, MaxHp: x.MaxHP, Att: x.Att})
	}
	for _, m := range room.members {
		m.client.Send(msg)
	}
	if room.pvp {
		w.duelDeaths(room) // BATTLE_AddDuelPoint
	}
	if over, win := room.b.Over(); over {
		w.endBattle(room, win)
		return
	}
	// 逃走的人马上离开战斗（BATTLE_Exit），其他人继续打
	for _, m := range append([]*entity(nil), room.members...) {
		if !room.b.F[m.fight.slot].Escaped {
			continue
		}
		if room.pvp {
			w.duelEscape(room, m)
		}
		room.removeMember(m)
		w.endMember(m, -1)
	}
	w.armTurnTimer(room, len(ev))
}

// removeMember 把 m 从这场战斗的玩家里拿掉（它的 fight 不动）。
func (r *battleRoom) removeMember(m *entity) {
	for i, x := range r.members {
		if x == m {
			r.members = append(r.members[:i:i], r.members[i+1:]...)
			return
		}
	}
}

// afterMemberTurn 是一回合结算后 m 自己的部分：宠物栏、装备磨损、道具消耗。
func (w *World) afterMemberTurn(e *entity, ev []battle.Event) {
	ft := e.fight
	w.afterTurn(e, ev)
	if ft.itemsDirty {
		ft.itemsDirty = false
		e.char.Items = e.inv.String()
		w.sendInventory(e)
	}
	if ft.petsDirty {
		ft.petsDirty = false
		e.char.Pets = e.pets.String()
		w.sendPets(e)
	}
	// 道具在实际用出去时才消耗（行动前倒下就没用掉；没有效果的道具大多也照样用掉，见 battle/item.go）
	if ft.cmd != nil && ft.cmd.Kind == battle.UseItem {
		for _, x := range ev {
			if x.Actor == ft.slot && x.Consume {
				if it := e.inv[ft.cmd.ItemSlot]; it != nil {
					if def := w.game.Items[it.ID]; def != nil && def.UseFunc == "ITEM_useMagic" && def.Uses-it.Used > 0 {
						it.Used++ // 有次数（damagebreak）的施法道具先扣次数
					} else if it.Pile = it.count() - 1; it.Pile <= 0 || x.ConsumeAll {
						e.inv[ft.cmd.ItemSlot] = nil
					}
					e.char.Items = e.inv.String()
					w.sendInventory(e)
				}
				break
			}
		}
	}
	ft.cmd, ft.petCmd = nil, nil
}

// endBattle 结算这场战斗的每个玩家，最后以队长的结果调用 onEnd。
func (w *World) endBattle(room *battleRoom, win int) {
	if room.timer != nil {
		room.timer.Stop()
	}
	result := ""
	for i, m := range append([]*entity(nil), room.members...) {
		r := w.endMember(m, win)
		if i == 0 {
			result = r
		}
	}
	room.members = nil
	if room.onEnd != nil && result != "" {
		room.onEnd(result)
	}
}

// endMember 结算一个玩家：赢了按打倒的敌人给经验（battle.c 的经验分配 + CHAR_LevelUpCheck），倒下的人 HP 回到 1。回传结果。
func (w *World) endMember(e *entity, win int) string {
	ft := e.fight
	e.fight = nil
	p := ft.b.F[ft.slot]
	res := &proto.BattleEnd{Result: "lose"}
	switch {
	case p.Escaped:
		res.Result = "escape"
	case win == ft.slot/battle.SideOffset:
		res.Result = "win"
	}
	pvp := ft.room.pvp
	c := &e.char
	c.HP, c.MP = p.HP, p.MP
	c.Items = e.inv.String() // 战斗中的耐久磨损
	if p.HP <= 0 {
		c.HP = 1 // 原版：战斗结束时倒下的角色复活为 HP 1
	}
	res.Pets = []proto.PetResult{}
	if res.Result == "win" && !pvp { // PvP 没有经验、石币与道具（dpbattle：改算 DP）
		// BATTLE_AddExp：经验给打倒敌人的那个人（玩家或宠物），战斗结束时还活着的才拿得到
		petExp := e.addExp.boost(w.petKillExp(ft)) // 宠物用主人的智慧之果
		if p.HP > 0 {
			for _, f := range ft.b.F {
				if f != nil && f.Enemy() && f.HP <= 0 && f.KilledBy == ft.slot {
					res.Exp += battle.KillExp(f.Exp, c.Level, f.Level)
				}
			}
			res.Exp = e.addExp.boost(res.Exp) // 智慧之果（BATTLE_GetExp 的 FreeModeExp）
			res.LevelUp = w.gainExp(c, res.Exp)
			res.Items = w.giveDrops(e, ft.b)
		}
		res.Pets = w.petResult(e, ft, petExp)
		if p.HP > 0 {
			res.Pets = append(res.Pets, w.rideResult(e, ft)...)
		}
	}
	w.settleRide(e, ft)
	if p.HP <= 0 && !pvp {
		// BATTLE_NormalDeadExtra（只有 P_vs_E）：玩家倒下魅力 CH_FIX_PLAYERDEAD（10 级以下减半）
		div := 1
		if c.Level <= 10 {
			div = 2
		}
		c.Charm = max(0, min(100, c.Charm+battle.CHFixPlayerDead/div))
	}
	w.settlePet(e, ft)
	c.Magic = magicString(e.magic)
	if p.Pig > 0 && c.Pig != p.Pig { // 战斗中变成乌力（PETSKILL_BecomePig）：出来还是乌力，变身取消
		c.Pig, e.metamo = p.Pig, nil
		w.updateSprite(e)
	}
	res.Level = c.Level
	e.client.Send(res)
	if pvp {
		w.duelResult(e, ft)
	}
	w.sendStatus(e)
	w.broadcastMark(e) // CHAR_sendBattleEffect(OFF)
	if p.Escaped {
		w.leaveParty(e, false) // 逃走：CHAR_DischargePartyNoMsg（队长逃走就解散）
	}
	if ft.pet >= 0 || p.Ride != nil {
		e.char.Pets = e.pets.String()
		w.sendPets(e)
	}
	w.saveProgress(e)
	return res.Result
}

// gainExp 加经验并检查升级，返回升了几级（CHAR_LevelUpCheck）：
// 每升一级 +3 点、魅力 +2（上限 100）、DP +（新等级 × 10）。
func (w *World) gainExp(c *store.Character, exp int) int {
	c.Exp += exp
	ups := 0
	for c.Level < MaxLevel {
		next := w.game.Battle.NextLevelExp(c.Level)
		if next < 0 || c.Exp < next {
			break
		}
		c.DP += (c.Level + 1) * 10
		c.Level++
		ups++
	}
	c.Points += ups * LevelUpPoints
	c.Charm = min(100, c.Charm+ups*CharmPerLevel)
	return ups
}

// StatUp 分配一点升级点数（原版 lssproto_SKUP：0 体力、1 腕力、2 耐力、3 敏捷）。
func (w *World) StatUp(charID, stat int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || e.fight != nil || e.char.Points <= 0 || stat < 0 || stat > 3 {
			return
		}
		c := &e.char
		full := c.HP < 0
		*[]*int{&c.Vital, &c.Str, &c.Tgh, &c.Dex}[stat]++
		c.Points--
		if full {
			c.HP = -1
		}
		w.sendStatus(e)
		w.saveProgress(e)
	})
}

// giveDrops 是战利品分配（battle.c）：打倒的敌人身上的道具，每人最多拿 3 件，满 3 件后新道具一半机率随机换掉一件；
// 道具栏满了就拿不到。回传拿到的道具名。
func (w *World) giveDrops(e *entity, b *battle.Battle) []string {
	var got []int
	for _, f := range b.F {
		if f == nil || f.Player || f.HP > 0 {
			continue
		}
		for _, id := range f.Drops {
			if len(got) < 3 {
				got = append(got, id)
			} else if w.rnd.IntN(2) == 1 {
				got[w.rnd.IntN(3)] = id
			}
		}
	}
	names := []string{}
	changed := false
	for _, id := range got {
		def := w.game.Items[id]
		if def == nil {
			continue
		}
		if w.addItem(e.inv, w.newItem(def)) < 0 {
			w.itemMsg(e, "道具栏已满，"+def.Name+"没能拿到。")
			continue
		}
		names = append(names, def.Name)
		changed = true
	}
	if changed {
		e.char.Items = e.inv.String()
		w.sendInventory(e)
	}
	return names
}

// statusOf 生成状态窗口数据（原版 menu.cpp 状态窗口显示的 pc.* 字段）。
// 四属性在存储里是建角点数 0..10，原版 pc.earth 等是 0..100，这里乘 10。HP 为 -1 时表示满。
func (w *World) statusOf(e *entity) *proto.Status {
	c := e.char
	s := w.stats(e)
	hp := c.HP
	if hp < 0 || hp > s.MaxHP {
		hp = s.MaxHP
	}
	next := 0
	if w.game.Battle != nil {
		next = max(0, w.game.Battle.NextLevelExp(c.Level))
	}
	return &proto.Status{Name: c.Name, Face: c.Face, Level: c.Level, Exp: c.Exp, NextExp: next,
		HP: hp, MaxHp: s.MaxHP, Mp: min(c.MP, s.MaxMP), MaxMp: s.MaxMP, Atk: s.Atk, Def: s.Def, Agi: s.Quick,
		Charm: s.Charm, DP: c.DP, Vital: c.Vital, Str: c.Str, Tgh: c.Tgh, Dex: c.Dex,
		Attrs: s.Attr[:], Hometown: c.Hometown, Trans: c.Trans,
		Points: c.Points}
}

// saveProgress 在后台保存等级与各项数值。
func (w *World) saveProgress(e *entity) {
	c := e.char
	w.saves <- func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := w.db.SaveProgress(ctx, c); err != nil {
			w.log.Error("保存数值失败", "char", c.ID, "err", err)
		}
	}
}

// abandonBattle：战斗中下线，自己的部分作废（保留当前 HP，倒下的按 1），在场上的人与宠物当作离开；
// 没有别人了就整场作废，否则等其他人的指令。
func (w *World) abandonBattle(e *entity) {
	ft := e.fight
	if ft == nil {
		return
	}
	room := ft.room
	room.removeMember(e)
	p := ft.b.F[ft.slot]
	p.Escaped = true
	if pf := ft.b.F[ft.slot+5]; pf != nil {
		pf.Escaped = true
	}
	e.char.HP, e.char.MP = max(1, p.HP), p.MP
	e.char.Magic = magicString(e.magic)
	w.settleRide(e, ft)
	if pf := ft.b.F[ft.slot+5]; ft.pet >= 0 && pf != nil && e.pets.Pets[ft.pet] != nil {
		e.pets.Pets[ft.pet].HP = max(1, pf.HP)
		e.char.Pets = e.pets.String()
	}
	e.fight = nil
	w.saveProgress(e)
	switch {
	case len(room.members) == 0:
		if room.timer != nil {
			room.timer.Stop()
		}
	default:
		w.tryResolve(room)
	}
}

// statusOfFor 测试用：在世界协程里取一份状态。
func (w *World) statusOfFor(charID int) (st *proto.Status) {
	w.Do(func() {
		if e := w.players[charID]; e != nil {
			st = w.statusOf(e)
		}
	})
	return
}
