package world

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 在地图上使用道具（lssproto_ID_recv → CHAR_ItemUse → 道具的 usefunc，item_event.c）。target 0 是自己、1..5 是第几只宠物
// （Callfromcli_Util_getTargetCharaindex；6..10 的队友要等组队）。道具种类不是"其他"（16）与"料理"（20）的当作装备品（双击装上）。
// 原版服务端不查道具的 FIELD / TARGET，只有客户端挡；只能在战斗中用的道具（附加 / 解除状态、捕获之果、咒、光镜守、复活）在地图上什么也不做。
//
//	ITEM_useRecovery     体（HP）/ 气（MP，只对人）/ 魅（魅力，只对人）/ 忠（忠诚度，只对宠物）/ 全；对象可以是自己或宠物
//	ITEM_useSkillCanned  技能罐头：对宠物用，选一个技能栏学会参数里的技能（整个道具用掉）
//	ITEM_Addexp          智慧之果："增130分60" = 学习经验的能力 +130%、60 分钟（同样的百分比累加时间，上限 172800 分钟）
//	ITEM_useWarp         "种类 楼层 x y" 传送
//	ITEM_useWarpForNum   有次数的飞行道具：飞到记录的地点，并把现在的位置记成下一次的地点（只在几个城镇能记）
//	ITEM_Constitution    "VI:1|ST:10"：把体腕耐敏的点数换回可分配的点数
//	ITEM_metamo          变身镜：变成自己的宠物 N 秒（对自己用是变回来）
// 其余没做的 usefunc 回"什麽也没有发生。"（原版 Lua RunItemUseEvent 失败时的讯息）。

// 道具窗口用的 NPC 编号 0（没有 NPC）与 seq
const itemWindowSkillCanned = 41 // ITEM_WINDOWTYPE_SELECTPETSKILL_SELECT

// argNum 是 sscanf(strstr(arg, key) + 一个字, "%d")：key 后面的整数（前面的空白可以跳过），找不到 key 或没有数字时 def。
func argNum(arg, key string, def int) int {
	i := strings.Index(arg, key)
	if i < 0 {
		return def
	}
	s := strings.TrimLeft(arg[i+len(key):], " \t")
	end := 0
	for end < len(s) && (s[end] >= '0' && s[end] <= '9' || end == 0 && s[end] == '-') {
		end++
	}
	v, err := strconv.Atoi(s[:end])
	if err != nil {
		return def
	}
	return v
}

// useUp 用掉道具栏 slot 的一个（CHAR_DelItem：堆叠的减一）。
func (w *World) useUp(e *entity, slot int) {
	if it := e.inv[slot]; it != nil {
		if it.Pile = it.count() - 1; it.Pile <= 0 {
			e.inv[slot] = nil
		}
	}
}

// ItemUse 在地图上使用道具栏 slot 的道具，对象见文件开头。
func (w *World) ItemUse(charID, slot, target int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || e.busy() || slot < data.EquipSlots || slot >= data.ItemSlots || e.inv[slot] == nil {
			return
		}
		def := w.game.Items[e.inv[slot].ID]
		if def == nil {
			return
		}
		if def.UseFunc == "ITEM_useLearnRideCode" {
			w.learnRideItem(e, slot, def)
			return
		}
		if def.Type != data.ItemOther && def.Type != data.ItemDish {
			if data.EquipSlot(def.Type) >= 0 {
				w.ItemMoveLocked(e, slot) // 装备品：双击就装上（CHAR_moveEquipItem）
			}
			return
		}
		if def.UseFunc == "ITEM_Jigsaw" && def.Image >= jigsawBigBoard && def.Image <= jigsawLastPiece {
			w.jigsaw(e, target, slot) // lssproto_ID_recv：拼图的 toindex 是底板的格位，不换成对象
			return
		}
		var pet *Pet
		who := e // 用在哪个玩家身上（6..10 是队伍的第几格，Callfromcli_Util_getTargetCharaindex）
		switch {
		case target == 0:
		case target >= 1 && target <= PetSlots:
			if pet = e.pets.Pets[target-1]; pet == nil {
				return
			}
		case target >= 6 && target < 6+partyMax:
			if who = w.partyAt(e, target-6); who == nil || who.fight != nil || def.UseFunc != "ITEM_useRecovery" {
				return
			}
		default:
			return
		}
		switch def.UseFunc {
		case "ITEM_useRecovery":
			w.useRecoveryField(e, who, slot, def, pet)
		case "ITEM_useSkillCanned":
			if pet == nil {
				w.sysMsg(e, "道具 "+def.Name+"仅限宠物使用。", colorYellow)
				return
			}
			e.window = &npcWindow{npc: 0, seq: itemWindowSkillCanned, kind: "petskillslot", slot: slot, pet: target - 1}
			e.client.Send(&proto.NpcWindow{ID: 0, Kind: "petskillslot", Seq: itemWindowSkillCanned, Text: strconv.Itoa(target - 1)})
		case "ITEM_Addexp":
			w.useAddExp(e, slot, def, pet)
		case "ITEM_useWarp":
			w.useWarp(e, slot, def)
		case "ITEM_useWarpForNum":
			w.useWarpForNum(e, slot, def)
		case "ITEM_Constitution":
			w.useConstitution(e, slot, def)
		case "ITEM_metamo":
			if e.char.Pig > 0 {
				w.sysMsg(e, "无法变身，乌力化中不能变身！", colorYellow)
				return
			}
			if w.metamo(e, pet, argNum(" "+def.Argument, " ", 0)) {
				w.useUp(e, slot)
				w.itemsChanged(e)
			}
		case "ITEM_MysteriousGift", "ITEM_Refresh", "ITEM_Luck", "ITEM_Gold", "ITEM_AddFame", "ITEM_useSkup", "ITEM_useNoenemy",
			"ITEM_useEncounter", "ITEM_useDeathcounter", "ITEM_useMaxRedSocks", "ITEM_useMaxRedSocksNew", "ITEM_changePetOwner",
			"ITEM_useRenameItem", "ITEM_WaterWordStatus":
			w.useItem2(e, slot, def, pet)
		case "ITEM_useStatusChange", "ITEM_useStatusRecovery", "ITEM_useCaptureUp", "ITEM_useMRecovery",
			"ITEM_useImprecate", "ITEM_useRessurect", "ITEM_useFieldChange", "ITEM_firecracker", "ITEM_ResAndDef", "ITEM_useMagic":
			// 地图上的分支是空的：不说话、不用掉
		default:
			w.sysMsg(e, "什麽也没有发生。", colorWhite)
		}
	})
}

// useRecoveryField 是 ITEM_useRecovery_Field：各关键字独立判定（体 / 气 / 魅 / 忠 / 全），数值 RAND(0.9n, 1.1n)，没写数字时 1。
// 一样都用不上（例如对宠物用"气"）时不用掉；否则就算实际没有变化也用掉。
func (w *World) useRecoveryField(e, who *entity, slot int, def *data.ItemDef, pet *Pet) {
	arg := def.Argument
	if strings.Contains(arg, "解猪") { // _ITEM_UNBECOMEPIG：解除乌力化，不是乌力时不用掉
		if e.char.Pig > 0 {
			e.char.Pig = -1
			w.updateSprite(e)
			w.sysMsg(e, "乌力化失效了。", colorWhite)
			w.useUp(e, slot)
			w.itemsChanged(e)
		}
		return
	}
	roll := func(key string) int {
		n := argNum(arg, key, 1)
		lo, hi := float64(n)*0.9, float64(n)*1.1
		return int(lo + float64(int((hi-lo+1)*w.rnd.Float64())))
	}
	all := strings.Contains(arg, "全")
	var hp, mp, charm, ai int
	hpOn, mpOn := all || strings.Contains(arg, "体"), (all || strings.Contains(arg, "气")) && pet == nil
	charmOn, aiOn := strings.Contains(arg, "魅") && pet == nil, strings.Contains(arg, "忠") && pet != nil
	if !hpOn && !mpOn && !charmOn && !aiOn {
		return
	}
	switch {
	case all:
		hp, mp = 10000000, 100
	default:
		if hpOn {
			hp = roll("体")
		}
		if mpOn {
			mp = roll("气")
		}
	}
	if charmOn {
		charm = roll("魅")
	}
	if aiOn {
		ai = roll("忠") * 100
	}
	name := who.char.Name
	var dHP, dMP, dCharm, dAI int
	if pet != nil {
		name = pet.displayName()
		max := w.petMaxHP(pet)
		old := pet.HP
		pet.HP = clamp(pet.HP+hp, 1, max)
		dHP = pet.HP - old
		if aiOn {
			before := w.petLoyalty(e, pet)
			pet.addVariableAI(ai)
			dAI = w.petLoyalty(e, pet) - before
		}
	} else {
		c := &who.char
		maxHP, maxMP := w.maxHPMP(who)
		cur := c.HP
		if cur < 0 {
			cur = maxHP
		}
		n := clamp(cur+hp, 1, maxHP)
		dHP = n - cur
		c.HP = n
		if n >= maxHP {
			c.HP = -1
		}
		m := clamp(c.MP+mp, 1, maxMP)
		dMP, c.MP = m-c.MP, m
		ch := clamp(c.Charm+charm, 0, 100)
		dCharm, c.Charm = ch-c.Charm, ch
	}
	say := func(on bool, d int, up, down string) {
		if !on {
			return
		}
		if d >= 0 {
			w.sysMsg(e, fmt.Sprintf(up, name, d), colorWhite)
		} else {
			w.sysMsg(e, fmt.Sprintf(down, name, -d), colorWhite)
		}
	}
	say(hpOn, dHP, "%s的耐久力回复%d", "%s的耐久力减低%d")
	say(mpOn, dMP, "%s的气力回复%d", "%s的气力减低%d")
	say(charmOn, dCharm, "%s的魅力上升%d", "%s的魅力下降%d")
	say(aiOn, dAI, "%s的忠诚度上升%d", "%s的忠诚度下降%d")
	w.useUp(e, slot)
	w.itemsChanged(e)
	if pet != nil {
		w.petsChanged(e)
	}
	if who != e {
		w.sendStatus(who)
		w.saveProgress(who)
	}
}

func clamp(v, lo, hi int) int { return max(lo, min(hi, v)) }

// itemWindowReply 处理道具开的窗口（NPC 编号 0）。目前只有技能罐头选技能栏（ITEM_usePetSkillCanned_WindowResult）。
func (w *World) itemWindowReply(e *entity, win *npcWindow, button int, data_ string) {
	switch win.seq {
	case partySelectSeq:
		w.partySelectReply(e, win, button, data_)
		return
	case cardSelectSeq:
		w.cardSelectReply(e, win, button, data_)
		return
	case duelSelectSeq:
		w.duelSelectReply(e, win, button, data_)
		return
	case battleSelectSeq:
		w.joinBattleReply(e, win, button, data_)
		return
	}
	if w.fmWindowReply(e, win, button, data_) {
		return
	}
	if win.seq >= renameSelectSeq && win.seq <= renameErrorSeq {
		w.renameReply(e, win, button, data_)
		return
	}
	if win.kind != "petskillslot" {
		return
	}
	n, err := strconv.Atoi(data_)
	if err != nil || n < 0 || n >= petSkillMax {
		return
	}
	it := e.inv[win.slot]
	if it == nil {
		return
	}
	def := w.game.Items[it.ID]
	if def == nil || def.UseFunc != "ITEM_useSkillCanned" {
		w.sysMsg(e, "禁止非法学习宠物技能！", colorRed)
		return
	}
	pet := e.pets.Pets[win.pet]
	sk := w.game.Battle.Skills[data.Atoi(def.Argument)]
	if pet == nil || sk == nil || n >= pet.Slot {
		return
	}
	pet.Skills[n] = sk.ID
	e.inv[win.slot] = nil // 整个道具消失
	w.sysMsg(e, fmt.Sprintf("宠物%s学习%s，道具 %s消失了。", pet.displayName(), sk.Name, def.Name), colorYellow)
	w.petsChanged(e)
	w.itemsChanged(e)
}

// ---------------- 智慧之果 ----------------

// addExp 是提升学习经验的能力（CHAR_WORKITEM_ADDEXP / ADDEXPTIME），存在 characters.addexp。
type addExp struct{ power, secs int }

func parseAddExp(s string) addExp {
	p, t, _ := strings.Cut(s, ",")
	return addExp{data.Atoi(p), data.Atoi(t)}
}

func (a addExp) String() string {
	if a.secs <= 0 {
		return ""
	}
	return fmt.Sprintf("%d,%d", a.power, a.secs)
}

// boost 是 BATTLE_GetExp 的 FreeModeExp：经验 + 经验 × 百分比 / 100（宠物用主人的）。
func (a addExp) boost(exp int) int {
	if a.secs <= 0 || a.power == 0 {
		return exp
	}
	return exp + exp*a.power/100
}

// useAddExp 是 ITEM_Addexp（_FIX_ADD_EXP）：只能对自己或自己的宠物；同样的百分比时把剩下的时间加上去。
// 原版宠物身上的这两个值不起作用（_PET_ADD_EXP 没开，宠物用主人的），这里对宠物用只说话、用掉。
func (w *World) useAddExp(e *entity, slot int, def *data.ItemDef, pet *Pet) {
	power, minutes := argNum(def.Argument, "增", 0), argNum(def.Argument, "分", -1)
	if minutes < 0 {
		return
	}
	name := e.char.Name
	if pet != nil {
		name = pet.displayName()
	} else {
		if power == e.addExp.power {
			minutes += e.addExp.secs / 60
		}
		if minutes >= 172800 {
			w.sysMsg(e, "智慧之果累计时间及效果已达上限。", colorYellow)
			return
		}
		e.addExp = addExp{power, minutes * 60}
		e.char.AddExp = e.addExp.String()
	}
	w.useUp(e, slot)
	w.sysMsg(e, fmt.Sprintf("%s学习经验的能力提升了%d％，时效剩余%d分钟。", name, power, minutes), colorYellow)
	w.itemsChanged(e)
}

// tickAddExp 是 CONNECT_SysEvent_Loop 的那一段（每 300 秒）：时间减 300，用完时说"消失了"，整点前后提醒剩几小时。
func (w *World) tickAddExp(e *entity) {
	a := &e.addExp
	if a.secs <= 0 {
		return
	}
	if a.secs -= 300; a.secs <= 0 {
		*a = addExp{}
		w.sysMsg(e, "提升学习经验的能力消失了!", colorYellow)
	} else if a.secs%3600 < 300 && a.secs >= 3600 {
		w.sysMsg(e, fmt.Sprintf("提升学习经验的能力剩大约 %d 小时。", a.secs/3600), colorYellow)
	}
	e.char.AddExp = a.String()
	w.saveProgress(e)
}

// ---------------- 传送 ----------------

// noWarpFloors 是 ITEM_WarpForAny 里不能用传送道具的楼层。
var noWarpFloors = map[int]bool{117: true, 8513: true, 17003: true, 17005: true}

// warpForAny 是 ITEM_WarpForAny（没有组队，只有单人的分支）：传送前收走 2609、2704 号道具（ITEM_WarpDelErrorItem）。
func (w *World) warpForAny(e *entity, floor, x, y int) bool {
	if noWarpFloors[e.floor] {
		w.sysMsg(e, "此处无法使用。", colorYellow)
		return false
	}
	f := w.game.Floors[floor]
	if f == nil || !f.In(x, y) {
		return false
	}
	for i, it := range e.inv {
		if it != nil && (it.ID == 2609 || it.ID == 2704) {
			e.inv[i] = nil
		}
	}
	w.placeOnFloor(e, floor, x, y, e.dir)
	w.save(e)
	return true
}

func (w *World) useWarp(e *entity, slot int, def *data.ItemDef) {
	p := strings.Fields(def.Argument)
	if len(p) < 4 {
		return
	}
	if w.warpForAny(e, data.Atoi(p[1]), data.Atoi(p[2]), data.Atoi(p[3])) {
		w.useUp(e, slot)
		w.itemsChanged(e)
	}
}

// warpRecordFloors 是 ITEM_useWarpForNum 能记录地点的楼层与名字。
var warpRecordFloors = map[int]string{100: "萨伊那斯", 200: "加鲁卡", 300: "吉鲁岛", 400: "沙姆岛", 700: "伊甸园区", 701: "伊甸大陆西方",
	702: "尼克斯城区", 703: "伊甸大陆南方", 704: "亚伊欧城区", 705: "伊甸大陆东方", 707: "伊甸大陆北方", 708: "塔耳塔城区"}

// useWarpForNum 是 ITEM_useWarpForNum：次数（道具的 damagebreak）减一；用完时道具消失，否则在能记录的楼层把现在的位置记成下一次的地点；
// 然后飞到原来记录的地点（次数在传送前就扣了，飞不成也一样）。
func (w *World) useWarpForNum(e *entity, slot int, def *data.ItemDef) {
	it := e.inv[slot]
	arg, uses := def.Argument, def.Uses
	if it.Arg != "" {
		arg, uses = it.Arg, it.Uses
	}
	p := strings.Fields(arg)
	if len(p) < 4 {
		return
	}
	flg, floor, x, y := p[0], data.Atoi(p[1]), data.Atoi(p[2]), data.Atoi(p[3])
	if uses--; uses <= 0 {
		w.useUp(e, slot)
	} else {
		name, ok := warpRecordFloors[e.floor]
		if !ok {
			w.sysMsg(e, "此处无法使用。", colorYellow)
			return
		}
		it.Arg, it.Uses = fmt.Sprintf("%s %d %d %d", flg, e.floor, e.x, e.y), uses
		w.sysMsg(e, fmt.Sprintf("飞行纪录点(%s,%d,%d)", name, e.x, e.y), colorYellow)
	}
	w.itemsChanged(e)
	w.warpForAny(e, floor, x, y)
}

// ---------------- 体质 ----------------

// useConstitution 是 ITEM_Constitution：参数 "VI:1|ST:10"（体 / 腕 / 耐 / 敏，1..100 点）把基本点数换回可分配的点数；
// 某一项不够时说"物品无效。"并停下（之前换过的照样算、道具不用掉）。都做完后不论有没有换都用掉。
func (w *World) useConstitution(e *entity, slot int, def *data.ItemDef) {
	c := &e.char
	stats := map[string]*int{"VI": &c.Vital, "ST": &c.Str, "TG": &c.Tgh, "DE": &c.Dex}
	changed := false
	for _, seg := range strings.Split(def.Argument, "|") {
		k, v, ok := strings.Cut(seg, ":")
		stat := stats[strings.ToUpper(strings.TrimSpace(k))]
		n := data.Atoi(v)
		if !ok || stat == nil || n <= 0 || n > 100 {
			continue
		}
		if *stat < n || c.Vital+c.Str+c.Tgh+c.Dex-n <= 0 {
			w.sysMsg(e, "物品无效。", colorYellow)
			w.itemsChanged(e)
			return
		}
		*stat -= n
		c.Points += n
		changed = true
	}
	if changed {
		w.sysMsg(e, "你的身体感到变化。", colorYellow)
	} else {
		w.sysMsg(e, "物品无效。", colorYellow)
	}
	if maxHP, _ := w.maxHPMP(e); c.HP > maxHP {
		c.HP = -1
	}
	w.useUp(e, slot)
	w.itemsChanged(e)
}

// ---------------- 变身 ----------------

// metamoState 是变身（CHAR_WORKITEMMETAMO）：变成的形象、到期的时间、变身时身上的衣服（脱下衣服就变回来，CHAR_sendItemDetachEvent）。
type metamoState struct {
	sprite int
	until  time.Time
	body   *Item
}

// metamo 是 ITEM_metamo / MAGIC_Metamo：变成自己的宠物 secs 秒，pet 为 nil 是变回自己。回传是否成立（成立才用掉道具 / 扣 MP）。
func (w *World) metamo(e *entity, pet *Pet, secs int) bool {
	if e.pets.Ride >= 0 {
		w.sysMsg(e, "无法变身，骑乘中不能变身！", colorYellow)
		return false
	}
	if wp := w.weapon(e.inv); wp != nil && (wp.Type == data.ItemBow || wp.Type == data.ItemBoundThrow ||
		wp.Type == data.ItemBreakThrow || wp.Type == data.ItemBoomerang) {
		w.sysMsg(e, "使用远距离武器无法变身！", colorYellow)
		return false
	}
	if pet == nil {
		e.metamo = nil
		w.sysMsg(e, "变回自己！", colorYellow)
	} else {
		e.metamo = &metamoState{sprite: pet.Sprite, until: w.now().Add(time.Duration(secs) * time.Second), body: e.inv[data.SlotBody]}
		w.sysMsg(e, "变身成"+pet.Name+"！", colorYellow)
	}
	w.updateSprite(e)
	return true
}

// checkMetamo：时间到了（CONNECT_SysEvent_Loop："变身失效了。"）或脱下了衣服（"变身失效了！"）就变回来。
func (w *World) checkMetamo(e *entity) {
	m := e.metamo
	if m == nil {
		return
	}
	switch {
	case e.inv[data.SlotBody] != m.body:
		e.metamo = nil
		w.sysMsg(e, "变身失效了！", colorWhite)
	case !w.now().Before(m.until):
		e.metamo = nil
		w.sysMsg(e, "变身失效了。", colorWhite)
	default:
		return
	}
	w.updateSprite(e)
}

// tickPlayers 每 5 秒（世界的时钟）：变身到期、原地遇敌；每 10 / 60 秒乌力化的时间；每分钟驱魔；每 300 秒智慧之果。
func (w *World) tickPlayers() {
	w.ticks++
	for _, e := range w.players {
		if e.fight == nil {
			w.checkMetamo(e)
		}
		if w.ticks%2 == 0 {
			w.tickPig(e, false)
		}
		if w.ticks%12 == 0 {
			w.tickPig(e, true)
			w.tickWater(e)
			if e.noenemy > 0 {
				if e.noenemy--; e.noenemy == 0 {
					w.sysMsg(e, "守护消失了。", colorWhite)
				}
			}
		}
		if w.ticks%60 == 0 {
			w.tickAddExp(e)
		}
		// CHAR_BattleStayLoop：原地遇敌（原版每 2.5 秒，这里跟世界的时钟每 5 秒）
		if e.stay && e.fight == nil && w.game.Battle != nil {
			if a := w.game.Battle.AreaAt(e.floor, e.x, e.y); a != nil {
				w.startBattle(e, a)
			}
		}
	}
}
