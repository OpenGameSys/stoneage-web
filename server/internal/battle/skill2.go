package battle

import (
	"strings"

	"sa/server/internal/data"
)

// 其余的宠物技能（pet_skill.c 的设定 + battle.c / battle_event.c 的执行），只实现这台服务端的宏开着的部分：
//
//	FallGround      落马术：攻%，打中骑着的人时 RAND(0,100) > 50 就落马
//	AttackCrazed    狂乱暴走：攻 ×0.8 防 ×0.7，打 N 下（第二下起打对方随机的人，不含 9 / 19 号位），会被反击；参数不是数字时只打一下
//	BattleTearDamage 撕裂伤口：攻 ×0.9 防 ×0.8，再加上对方缺的 HP 的 %（对方满血时没有伤害）
//	Gyrate          回旋攻击：攻%，打目标那一整排每人一下
//	Abduct          旅程伙伴：带着对方一起离开战场（对方是宠物时按忠诚度），自己一定离开；对玩家没有效果
//	SetMagicPet     大地铠甲 / 束缚 / 脱力 / 乏劲：N 回合的加减；原版只改到 WORKFIX*，实际只有敏捷（闪避）有作用，HP 是回复
//	Mdfyattack      属性转换攻击：攻方属性当作只有那一种；Modifyattack 强化攻击：对方有那个属性时 +20%
//	BattleTimid     怯战：攻 ×0.7 防 ×0.4 敏 ×0.8，打中后 15% 把对方赶出战场；2BattleTimid 狂狮怒吼：命% 的机率把宠物赶回去
//	Refresh         破除沉默 / 净化：同 MAGIC_StatusRecovery；Deeppoison 剧毒：同 MAGIC_StatusChange（回合数 +2）
//	Regret          憾甲一击：打目标，目标是宠物时连主人一起打（×0.8），每下 20% 晕眩（这回合与下回合不能行动）
//	BattleModel     群蜂乱舞 / 肆无忌蛋：N 个物件分给敌方每人，打中时附加状态
//	ShowMercy       手下留情：普通攻击，打不死对方
//	Vary            暗月变身：变成狼人 6 次行动（攻 ×1.3、防 ×0.5、敏 ×1.3），原版这些加减只改到 WORKFIX*，这里直接加在工作值上
//	SetDuck         闪避术：3 回合 61% 自动闪避
//	SpeedyAttack    疾速攻击：防 ×0.7，出手速度 ×1.3；DamageToHp2 浴血狂袭：攻敏 ×1.2、会心 ×1.3、出手 ×1.2、吸血 50%
//	Lighttakeed     穿透术 / 破镜重圆：对方的光镜守正好是这种时，自己也得到一次
//	Steal           偷窃：50% 偷走玩家的钱（8..12%）或随机一件道具（不会给偷的人），之后离开战场（由世界层处理）
//	BatFly / DivideAttack  群蝠四窜 / 分身地裂：敌方每人扣 HP 的固定比例（不会闪避），群蝠的伤害回到自己身上
// 除了普通攻击那一组（连续攻击、一击必杀、狂暴攻击、疾速攻击、狂乱暴走、手下留情…），技能攻击都不会引起反击。
// 乌力化 / 媚惑术的变身与火线猎杀的火魔法在 item2.go；E啮齿术的损坏装备由世界层做（crush.go OnTooth）。
// 名字没有登记在 PETSKILL_functbl 的（MpDamage2、deadattack、Relifeact、SelfExplodeAttack、BecomeEatting）什么也不做。

// 世界层要处理的事件
const (
	Flee   Kind = "flee"   // 被赶出 / 自己离开战场（怯战、旅程伙伴、偷窃）：Actor 是离开的人
	Abduct Kind = "abduct" // 旅程伙伴：OK 表示带走了对方
	Steal  Kind = "steal"  // 偷窃：OK 表示得手，Result 是 "gold" 或 "item"（由世界层扣）
	Vary   Kind = "vary"   // 变身 / 变回：Sprite 是新的图
)

// unregistered 是 petskill.txt 里写了、但 PETSKILL_functbl 没有的函数名（PETSKILL_Use 失败，什么也不做）。
// petskill2.txt 新出现的进化、挑衅反击、食物化、偷窃T改、黏液攻击、魔幻之术、骑乘术、增时等也都没有登记。
var unregistered = map[string]bool{"PETSKILL_MpDamage2": true, "PETSKILL_deadattack": true, "PETSKILL_Relifeact": true,
	"PETSKILL_SelfExplodeAttack": true, "PETSKILL_BecomeEatting": true, "PETSKILL_Evolution": true, "PETSKILL_ProvokeFight": true,
	"PETSKILL_Becomefood": true, "PETSKILL_FixBecomefood": true, "PETSKILL_Steal2": true, "PETSKILL_Mucus": true, "PETSKILL_Take": true,
	"PETSKILL_Ride": true, "PETSKILL_Additemtime": true}

// dexBonus 是束缚 / 风之吹息加在敏捷上的量（Other_DefcharWorkInt：以耐力为底，原版的笔误）。
func (f *Fighter) dexBonus() int {
	if f.dexTurns <= 0 {
		return 0
	}
	return f.FixDef * f.dexP / 100
}

// elemCode 是 "EA|20" 的属性：EA 地、WA 水、FI 火、WI 风 → 1..4。
func elemCode(opt string) int {
	switch strings.TrimSpace(strings.Split(opt, "|")[0]) {
	case "EA":
		return 1
	case "WA":
		return 2
	case "FI":
		return 3
	case "WI":
		return 4
	}
	return 0
}

// scale 把本回合的攻防敏设成基本值的倍数（设定函数里的 WORKFIX* × 倍率）。
func scale(f *Fighter, atk, def, quick float64) {
	if atk > 0 {
		f.Atk = int(float64(f.FixAtk) * atk)
	}
	if def > 0 {
		f.Def = int(float64(f.FixDef) * def)
	}
	if quick > 0 {
		f.Quick = int(float64(f.FixQuick) * quick)
	}
}

// setup2 是 skill2.go 这些技能下指令时的处理；handled 为 false 表示不是这里的技能。
func (b *Battle) setup2(f *Fighter, c Command) (Command, bool) {
	s := c.Skill
	none := Command{Kind: None, Skill: s}
	sameSide := c.Target >= 0 && c.Target < Slots && c.Target/SideOffset == f.Side()
	switch s.Func {
	case "PETSKILL_FallGround", "PETSKILL_Gyrate", "PETSKILL_Regret":
		if s.Func == "PETSKILL_FallGround" && sameSide { // _SKILLLIMIT：不能对自己人用
			return none, true
		}
		applyMods(f, s.Option)
	case "PETSKILL_AttackCrazed":
		if f.Player {
			return none, true
		}
		scale(f, 0.8, 0.7, 0)
	case "PETSKILL_BattleTearDamage":
		if f.Player {
			return none, true
		}
		scale(f, 0.9, 0.8, 0)
	case "PETSKILL_BattleTimid":
		if f.Player || sameSide {
			return none, true
		}
		scale(f, 0.7, 0.4, 0.8)
	case "PETSKILL_2BattleTimid":
		timidMods(f, s.Option)
	case "PETSKILL_Lighttakeed":
		if f.Player {
			return none, true
		}
		scale(f, 0.7, 0.5, 0)
	case "PETSKILL_SpeedyAttack":
		scale(f, 0, 0.7, 0) // 参数的 敏% 没有读
	case "PETSKILL_DamageToHp2":
		scale(f, 1.2, 0, 1.2)
	case "PETSKILL_Firekill":
		scale(f, 0.8, 0, 0)
	case "PETSKILL_BattleModel":
		// 第 6 段以空白分开，第一项含"攻"才算（基准是 WORKATTACKPOWER）；其余两项按原版写法实际没有作用
		p := strings.Split(s.Option, "|")
		if len(p) > 5 {
			if tok := strings.Fields(p[5]); len(tok) > 0 && strings.Contains(tok[0], "攻") {
				if v, ok := optNum(tok[0], "攻%"); ok {
					f.Atk += f.Atk * v / 100
				}
			}
		}
	case "PETSKILL_Vary":
		applyMods(f, s.Option)
	case "PETSKILL_Mdfyattack", "PETSKILL_Modifyattack":
		if elemCode(s.Option) == 0 {
			return none, true
		}
	case "PETSKILL_DamageToHp", "PETSKILL_MpDamage":
		if sameSide {
			return none, true
		}
		// 原版 atoi(第一段) / 100 是整数除法：只有 -100 的那一种攻击力加倍
		if k := field(s.Option, 1) / 100; k != 0 {
			f.Atk = f.FixAtk - f.FixAtk*k
		}
	case "PETSKILL_WildViolentAttack":
		if sameSide {
			return none, true
		}
		applyMods(f, s.Option)
	case "PETSKILL_Explode":
		// 爆裂攻击：不是 PvP 时就是普通攻击（PETSKILL_Explode 改成 BATTLE_COM_ATTACK）；PvP 时命中 + 腕力 ×50%、攻击力 = 腕力 ×70%
		if !b.PvP {
			c.Skill = nil
			break
		}
		f.Quick += int(f.Base[1] / 100 * 0.5) // WORKFIXDEX 只在这一回合
		f.Atk = int(f.Base[1] / 100 * 0.7)
	case "PETSKILL_ShowMercy", "PETSKILL_BecomePig", "PETSKILL_BecomeFox", "PETSKILL_ToothCrushe",
		"PETSKILL_SetMagicPet", "PETSKILL_SetDuck", "PETSKILL_Abduct", "PETSKILL_Steal", "PETSKILL_BatFly", "PETSKILL_DivideAttack":
	default:
		if unregistered[s.Func] {
			return none, true
		}
		return c, false
	}
	return c, true
}

// timidMods 是狂狮怒吼的 "-攻%50+敏%30"：- 设为基本值的 p%，+ 设为基本值 + p%。
func timidMods(f *Fighter, opt string) {
	for _, k := range []struct {
		key  string
		fix  int
		dest *int
	}{{"攻%", f.FixAtk, &f.Atk}, {"防%", f.FixDef, &f.Def}, {"敏%", f.FixQuick, &f.Quick}} {
		if v, ok := optNum(opt, "-"+k.key); ok {
			*k.dest = k.fix * v / 100
		}
		if v, ok := optNum(opt, "+"+k.key); ok {
			*k.dest = k.fix + k.fix*v/100
		}
	}
}

// skill2 执行这些技能；handled 为 false 表示不是这里的技能。
func (b *Battle) skill2(f *Fighter, c Command, ev *[]Event) bool {
	s := c.Skill
	o := attackOpts{name: s.Name, noCounter: true}
	switch s.Func {
	case "PETSKILL_FallGround":
		o.fallRide = true
	case "PETSKILL_AttackCrazed":
		o.noCounter, o.randomNext = false, true
		o.hits = max(1, leadNum(s.Option))
	case "PETSKILL_BattleTearDamage":
		o.tear = leadNum(s.Option)
	case "PETSKILL_Mdfyattack":
		o.mdfyElem = elemCode(s.Option)
	case "PETSKILL_Modifyattack":
		o.modifyElem = elemCode(s.Option)
	case "PETSKILL_Lighttakeed":
		o.lightTake = strings.TrimSpace(s.Option)
	case "PETSKILL_ShowMercy":
		o.noCounter, o.mercy = false, true
	case "PETSKILL_Explode":
		o.explode = true
	case "PETSKILL_SpeedyAttack":
		o.noCounter = false
	case "PETSKILL_DamageToHp2":
		o.criMod, o.drain = 1.3, leadNum(s.Option)
	case "PETSKILL_ToothCrushe":
		// E啮齿术：打一下（不会反击），有伤害、没被光镜守挡掉时咬坏对方玩家的一件装备
		first := len(*ev)
		b.attackSeq(f, c.Target, o, ev)
		for i := len(*ev) - 1; i >= first; i-- {
			x := (*ev)[i]
			if x.Actor != f.Slot || x.Kind != Attack {
				continue
			}
			if x.Damage > 0 && x.Result != Vanished && x.Result != Absorbed && x.Result != Reflected && b.OnTooth != nil {
				t := x.Target
				if x.Guardian >= 0 {
					t = x.Guardian
				}
				if d := b.F[t]; d != nil && d.Player {
					b.OnTooth(f, d)
				}
			}
			break
		}
		return true
	case "PETSKILL_Firekill":
		b.firekill(f, c, ev)
		return true
	case "PETSKILL_BecomeFox", "PETSKILL_BecomePig":
		first := len(*ev)
		o.noCounter = false
		b.attackSeq(f, c.Target, o, ev)
		b.transformAfter(f, s, first, ev)
		return true
	case "PETSKILL_BattleTimid", "PETSKILL_2BattleTimid":
		b.timid(f, c, o, ev)
		return true
	case "PETSKILL_Gyrate":
		b.gyrate(f, c, o, ev)
		return true
	case "PETSKILL_Regret":
		b.regret(f, c, o, ev)
		return true
	case "PETSKILL_BattleModel":
		b.battleModel(f, c, o, ev)
		return true
	case "PETSKILL_SetMagicPet":
		b.setMagicPet(f, c, ev)
		return true
	case "PETSKILL_SetDuck":
		b.setDuck(f, c, ev)
		return true
	case "PETSKILL_Vary":
		b.vary(f, c, ev)
		return true
	case "PETSKILL_Abduct":
		b.abduct(f, c, ev)
		return true
	case "PETSKILL_Steal":
		b.steal(f, c, ev)
		return true
	case "PETSKILL_BatFly", "PETSKILL_DivideAttack":
		b.drainAll(f, s, ev)
		return true
	default:
		return false
	}
	b.attackSeq(f, c.Target, o, ev)
	return true
}

// crazedTarget 是狂乱暴走的第二下起：对方还站着的人里随机一个（BATTLE_TargetListSet 只取 0..8 / 10..18）。
func (b *Battle) crazedTarget(side int) int {
	var cand []int
	for _, i := range b.targetable(side) {
		if i%SideOffset != 9 {
			cand = append(cand, i)
		}
	}
	if len(cand) == 0 {
		return -1
	}
	return cand[b.randi(0, len(cand)-1)]
}

// lightTake：对方的光镜守里排第一的（无效 > 吸收 > 反射）正好是参数那一种时，自己的那一种 = 对方剩下的 + 1
// （打中前判定，对方的那一次照样会用掉；反射时原版的"对方"已经换成自己，所以是自己的镜 +1）。
func (b *Battle) lightTake(a, d *Fighter, kind string) {
	switch {
	case d.Vanish > 0:
		if kind == "VANISH" {
			a.Vanish = d.Vanish
		}
	case d.Absorb > 0:
		if kind == "ABSROB" {
			a.Absorb = d.Absorb
		}
	case d.Reflect > 0:
		if kind == "REFLEC" {
			a.Reflect++
		}
	}
}

// lastHit 是 ev 里 a 最后一下攻击打到的人（被守护时是守护者）与伤害。
func (b *Battle) lastHit(a *Fighter, ev []Event, from int) (*Fighter, int) {
	for i := len(ev) - 1; i >= from; i-- {
		e := ev[i]
		if e.Actor == a.Slot && e.Kind == Attack {
			t := e.Target
			if e.Guardian >= 0 {
				t = e.Guardian
			}
			if t >= 0 && t < Slots {
				return b.F[t], e.Damage
			}
		}
	}
	return nil, 0
}

// flee 让 t 离开战场（BATTLE_Exit / BATTLE_PetDefaultExit）：玩家离开时出场的宠物跟着走。
func (b *Battle) flee(t *Fighter, ev *[]Event) {
	if t == nil || t.Escaped {
		return
	}
	t.Escaped = true
	*ev = append(*ev, Event{Actor: t.Slot, Kind: Flee, Target: -1, Guardian: -1, HP: t.HP, Status: t.Status()})
	if t.Player && t.Slot%SideOffset < 5 {
		if p := b.F[t.Slot+5]; p.Alive() {
			p.Escaped = true
			*ev = append(*ev, Event{Actor: p.Slot, Kind: Flee, Target: -1, Guardian: -1, HP: p.HP, Status: p.Status()})
		}
	}
}

// timid 是怯战（BATTLE_S_Timid）与狂狮怒吼：先打一下，伤害大于 1 时按机率把对方赶走
// （怯战 15%：宠物回去休息、其余离开战场；狂狮怒吼 命%：只对宠物，被叫回主人身边）。
func (b *Battle) timid(f *Fighter, c Command, o attackOpts, ev *[]Event) {
	first := len(*ev)
	b.attackSeq(f, c.Target, o, ev)
	t, dmg := b.lastHit(f, *ev, first)
	if t == nil || !t.Alive() || dmg <= 1 {
		return
	}
	if c.Skill.Func == "PETSKILL_BattleTimid" {
		if b.Rnd.IntN(100) < 15 {
			b.flee(t, ev)
		}
		return
	}
	per, _ := optNum(c.Skill.Option, "命%")
	if t.Pet && b.Rnd.IntN(100) < per {
		b.flee(t, ev)
	}
}

// gyrate 是回旋攻击：目标所在的那一排（0..4 / 5..9 / 10..14 / 15..19）每个还站着的人各打一下。
func (b *Battle) gyrate(f *Fighter, c Command, o attackOpts, ev *[]Event) {
	t := b.pickTarget(f, c.Target)
	if t == nil {
		*ev = append(*ev, Event{Actor: f.Slot, Kind: None, Target: -1, HP: f.HP, Status: f.Status(), Guardian: -1})
		return
	}
	row := t.Slot / 5 * 5
	first := len(*ev)
	for i := row; i < row+5; i++ {
		if x := b.F[i]; x.Alive() && !x.hidden && f.Alive() {
			e, _ := b.attackOnce(f, x, o, false)
			*ev = append(*ev, e)
		}
	}
	for i := first; i < len(*ev)-1; i++ {
		(*ev)[i].Combo = true
	}
}

// regret 是憾甲一击：打目标；目标在宠物那一排时再打它的主人（伤害 ×0.8）。每一下 命% 的机率晕眩。
func (b *Battle) regret(f *Fighter, c Command, o attackOpts, ev *[]Event) {
	t := b.pickTarget(f, c.Target)
	if t == nil {
		*ev = append(*ev, Event{Actor: f.Slot, Kind: None, Target: -1, HP: f.HP, Status: f.Status(), Guardian: -1})
		return
	}
	per := 20
	if v, ok := optNum(c.Skill.Option, "命%"); ok {
		per = v
	}
	hit := func(d *Fighter, o attackOpts) {
		e, _ := b.attackOnce(f, d, o, false)
		if x := b.F[e.Target]; e.Damage > 0 && x.Alive() && x.Status() == StNone && x.dizzy <= 0 && b.randi(1, 100) < per {
			x.dizzy = 2 // PROFESSION_BATTLE_StatusAttackCheck(…, 12, 20)：WORKDIZZY = 2
			x.charge = -1
		}
		*ev = append(*ev, e)
	}
	hit(t, o)
	if t.Slot%SideOffset >= 5 && f.Alive() {
		if owner := b.F[t.Slot-5]; owner.Alive() {
			(*ev)[len(*ev)-1].Combo = true
			o.dmgMod = 0.8
			hit(owner, o)
		}
	}
}

// battleModel 是群蜂乱舞 / 肆无忌蛋（BATTLE_BattleModel）："种类|物件数|状态|回合|机率|加减|图"。
// 物件数 ≥ 敌方人数时每人一个、多的随机分；否则前几个人各一个（种类有 1 时其余的人也各一个）。
// 每个物件是一下不会被反击的攻击，打中且对方还站着时按机率附加状态。
func (b *Battle) battleModel(f *Fighter, c Command, o attackOpts, ev *[]Event) {
	p := strings.Split(c.Skill.Option, "|")
	for len(p) < 6 {
		p = append(p, "")
	}
	typ, objs := data.Atoi(p[0]), data.Atoi(p[1])
	st, turns, per := statusOf(p[2]), data.Atoi(p[3]), data.Atoi(p[4])
	if objs <= 0 {
		objs = b.randi(1, 10)
	}
	objs = min(objs, 10)
	targets := b.targetable(1 - f.Side())
	if len(targets) == 0 {
		*ev = append(*ev, Event{Actor: f.Slot, Kind: None, Target: -1, HP: f.HP, Status: f.Status(), Guardian: -1})
		return
	}
	var order []int
	if objs >= len(targets) {
		order = append(order, targets...)
		for n := len(targets); n < objs; n++ {
			order = append(order, targets[b.randi(0, len(targets)-1)])
		}
	} else {
		order = append(order, targets[:objs]...)
		if typ&1 != 0 {
			order = append(order, targets[objs:]...)
		}
	}
	first := len(*ev)
	for _, i := range order {
		d := b.F[i]
		if !d.Alive() || !f.Alive() {
			continue
		}
		e, _ := b.attackOnce(f, d, o, false)
		if x := b.F[e.Target]; st > 0 && e.Damage > 0 && x.Alive() && b.statusChance(f, x, st, per, 30, 1) {
			x.status[st] = turns
			if st == StParalysis || st == StSleep || st == StStone || st == StBarrier {
				x.charge = -1
			}
			e.Status = x.Status()
		}
		*ev = append(*ev, e)
	}
	for i := first; i < len(*ev)-1; i++ {
		(*ev)[i].Combo = true
	}
}

// skillTargets 把技能的目标种类换成咒术的目标编号：我方全体 20 / 21、对方全体 21 / 20，其余照点的人。
func skillTargets(f *Fighter, s *data.Skill, target int) int {
	switch s.Target {
	case data.MagicTargetAllMySide:
		return 20 + f.Side()
	case data.MagicTargetAllOtherSide:
		return 21 - f.Side()
	}
	return target
}

// skillEffect 是技能放在某人身上的效果（BD / BM 之类的显示），客户端照咒术播放。
func (b *Battle) skillEffect(f, t *Fighter, s *data.Skill, r Result, effect int) Event {
	return Event{Actor: f.Slot, Kind: Magic, Target: t.Slot, Guardian: -1, Skill: s.Name, Result: r, Effect: effect,
		HP: t.HP, Status: t.Status(), ActorHP: f.HP, ActorMP: f.MP, RideHP: t.rideHP()}
}

func (b *Battle) combo(ev []Event, first int) {
	for i := first; i < len(ev)-1; i++ {
		ev[i].Combo = true
	}
}

// setMagicPet 是 PETSKILL_SetMagicPet_Battle："回合|数值|种类"。HP 是回复；其余已经有技能加减的人跳过，
// 设定 N 回合（只有 DEX 真的有作用，见文件开头）。
func (b *Battle) setMagicPet(f *Fighter, c Command, ev *[]Event) {
	s := c.Skill
	p := strings.Split(s.Option, "|")
	for len(p) < 3 {
		p = append(p, "")
	}
	turns, nums, kind := data.Atoi(p[0]), data.Atoi(p[1]), strings.TrimSpace(p[2])
	first := len(*ev)
	for _, i := range b.magicTargets(skillTargets(f, s, c.Target), false) {
		t := b.F[i]
		switch kind {
		case "HP":
			heal, petHeal := t.heal(nums) // 骑着时连骑宠一起回复
			e := b.skillEffect(f, t, s, MagicHeal, sprHeal1)
			e.Damage, e.PetDamage = heal, petHeal
			*ev = append(*ev, e)
			continue
		case "DEX":
			if t.dexTurns > 0 || t.skillDuck > 0 {
				continue
			}
			t.dexTurns, t.dexP = turns, nums
		default:
			if t.dexTurns > 0 || t.skillDuck > 0 {
				continue
			}
		}
		*ev = append(*ev, b.skillEffect(f, t, s, MagicHit, sprStatus))
	}
	if len(*ev) == first {
		*ev = append(*ev, Event{Actor: f.Slot, Kind: Magic, Target: -1, Guardian: -1, Skill: s.Name, Result: MagicFail, HP: f.HP, Status: f.Status()})
	}
	b.combo(*ev, first)
}

// setDuck 是闪避术（PETSKILL_SetDuck_Battle）："回合|机率"，只能对自己；已经在闪避中时不会重来。
func (b *Battle) setDuck(f *Fighter, c Command, ev *[]Event) {
	r := MagicFail
	if f.skillDuck <= 0 {
		f.skillDuck, f.duckP = field(c.Skill.Option, 1), field(c.Skill.Option, 2)
		r = MagicHit
	}
	*ev = append(*ev, b.skillEffect(f, f, c.Skill, r, sprStatus))
}

// varySprite 是暗月变身的狼人图（PETSKILL_Vary：没有 "图%" 时 101428）。
func varySprite(opt string) int {
	if v, ok := optNum(opt, "图%"); ok && v > 0 {
		return v
	}
	return 101428
}

// vary 是暗月变身：这回合不攻击，变成狼人，6 次行动后（含这次）变回来（afterAct）。
func (b *Battle) vary(f *Fighter, c Command, ev *[]Event) {
	if f.vary <= 0 {
		f.varySave = [4]int{f.FixAtk, f.FixDef, f.FixQuick, f.Sprite}
		f.FixAtk += f.FixAtk * magicNum(c.Skill.Option, "攻%", 0) / 100
		f.FixDef += f.FixDef * magicNum(c.Skill.Option, "防%", 0) / 100
		f.FixQuick += f.FixQuick * magicNum(c.Skill.Option, "敏%", 0) / 100
		f.Sprite = varySprite(c.Skill.Option)
	}
	f.vary = 6
	*ev = append(*ev, Event{Actor: f.Slot, Kind: Vary, Target: -1, Guardian: -1, Skill: c.Skill.Name, Sprite: f.Sprite, HP: f.HP, Status: f.Status()})
}

// afterAct：每次行动之后变身的次数减一，到了就变回来（battle.c 的 WORKTURN）。
func (b *Battle) afterAct(f *Fighter, ev *[]Event) {
	b.transformTick(f, ev)
	if f.vary <= 0 {
		return
	}
	if f.vary--; f.vary == 0 {
		f.FixAtk, f.FixDef, f.FixQuick, f.Sprite = f.varySave[0], f.varySave[1], f.varySave[2], f.varySave[3]
		f.Atk, f.Def, f.Quick = f.FixAtk, f.FixDef, f.FixQuick
		*ev = append(*ev, Event{Actor: f.Slot, Kind: Vary, Target: -1, Guardian: -1, Sprite: f.Sprite, HP: f.HP, Status: f.Status()})
	}
}

// NoAbduct：这场战斗不能被带走（原版战斗有 WinFunc 或是 LUA NPC 开的，即 NPC 战斗）。
// abduct 是旅程伙伴（BATTLE_Abduct）：对玩家没有效果；对宠物写了数字时忠诚度低于它才成功（机率 200），
// 否则机率 max((对方等级 − 自己等级) × 0.6 + 30, 50)。不论成功与否自己都离开战场。
func (b *Battle) abduct(f *Fighter, c Command, ev *[]Event) {
	t := b.pickTarget(f, c.Target)
	e := Event{Actor: f.Slot, Kind: Abduct, Target: -1, Guardian: -1, Skill: c.Skill.Name, HP: f.HP, Status: f.Status()}
	if t == nil || t.Player {
		if t != nil {
			e.Target = t.Slot
		}
		*ev = append(*ev, e)
		return
	}
	e.Target = t.Slot
	per := 0.0
	if ai := leadNum(c.Skill.Option); t.Pet && ai > 0 {
		if t.Loyalty < ai {
			per = 200
		}
	} else {
		per = max(float64(t.Level-f.Level)*0.6+30, 50)
	}
	if b.NoAbduct {
		per = 0
	}
	e.OK = float64(b.randi(1, 100)) < per
	*ev = append(*ev, e)
	if e.OK {
		b.flee(t, ev)
	}
	b.flee(f, ev)
}

// steal 是偷窃（BATTLE_Steal）：只偷玩家，50% 得手，再一半一半偷钱或道具（世界层照 Result 扣），得手后自己离开战场。
func (b *Battle) steal(f *Fighter, c Command, ev *[]Event) {
	t := b.pickTarget(f, c.Target)
	e := Event{Actor: f.Slot, Kind: Steal, Target: -1, Guardian: -1, Skill: c.Skill.Name, HP: f.HP, Status: f.Status()}
	if t != nil {
		e.Target = t.Slot
	}
	if t != nil && t.Player && b.randi(1, 100) < 50 {
		e.OK = true
		e.Result = "gold"
		if b.Rnd.IntN(2) == 1 {
			e.Result = "item"
		}
	}
	*ev = append(*ev, e)
	if e.OK {
		b.flee(f, ev)
	}
}

// drainAll 是群蝠四窜（BATTLE_BatFly）与分身地裂（BATTLE_DivideAttack）：敌方每人扣固定比例，不会闪避、不看光镜守。
// 群蝠：没骑宠的扣 HP/10，骑着的人与骑宠各扣 HP/20（至少 1），扣的总量回到自己身上；
// 分身：玩家 MP 减半，没骑宠的扣 HP/5，骑着的人与骑宠各扣 HP/10。
func (b *Battle) drainAll(f *Fighter, s *data.Skill, ev *[]Event) {
	bat := s.Func == "PETSKILL_BatFly"
	first, total := len(*ev), 0
	for _, i := range b.targetable(1 - f.Side()) {
		t := b.F[i]
		e := Event{Actor: f.Slot, Kind: Attack, Target: t.Slot, Guardian: -1, Skill: s.Name, Result: Normal}
		div, rideDiv := 5, 10
		if bat {
			div, rideDiv = 10, 20
		}
		var dmg int
		if t.riding() {
			dmg = max(1, t.HP/rideDiv)
			pd := max(1, t.Ride.HP/rideDiv)
			t.Ride.HP -= pd
			if t.Ride.HP <= 0 {
				t.Ride.HP, t.Ride.Fell, e.Fall = 0, true, true
			}
			e.PetDamage = pd
			total += pd
		} else {
			dmg = max(1, t.HP/div)
		}
		if !bat && t.Player && t.MP > 0 {
			e.MPDamage = t.MP / 2
			t.MP -= e.MPDamage
		}
		t.HP -= dmg
		total += dmg
		if t.HP <= 0 {
			t.HP, e.Dead = 0, true
			t.KilledBy, t.KilledRide = f.Slot, f.riding()
		}
		e.Damage, e.HP, e.Status, e.RideHP = dmg, t.HP, t.Status(), t.rideHP()
		*ev = append(*ev, e)
	}
	if bat {
		f.HP = min(f.MaxHP, f.HP+total)
	}
	for i := first; i < len(*ev); i++ {
		(*ev)[i].ActorHP = f.HP
	}
	if len(*ev) == first {
		*ev = append(*ev, Event{Actor: f.Slot, Kind: None, Target: -1, HP: f.HP, Status: f.Status(), Guardian: -1})
	}
	b.combo(*ev, first)
}
