package battle

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"sa/server/internal/data"
)

// 咒术（精灵，gmsv/battle/battle_magic.c、magic/magic.c）。指令是 Command{Kind: Magic, Magic, MP, Target}：
// 玩家的咒术来自装备着的道具（MP 是道具上写的），敌人与宠物的攻击魔法来自技能 PETSKILL_AttackMagic / Combined（不扣 MP）。
// 目标编号同原版：0..19 单人；20 己方（0..9）、21 敌方（10..19）、22 全体；26 / 25 / 23 / 24 是 0..4 / 5..9 / 10..14 / 15..19 那一排。
//
//	MAGIC_Recovery / OtherRecovery   回复 HP：RAND(0.9p, 1.1p)，骑着时按双方缺的 HP 分；宠物第一次被回复忠诚度 +AI_FIX_PETRECOVERY
//	MAGIC_Ressurect / ResAndDef      复活倒下的人（0 为全满），ResAndDef 再加上吸收 / 反射 / 无效的次数
//	MAGIC_StatusRecovery             解除状态（"全" 解除最高编号的那个）
//	MAGIC_StatusChange / Weaken / Barrier / Nocast   附加状态：BATTLE_StatusAttackCheck(成, 30, 1.0)
//	MAGIC_MagicDef                   光 / 镜 / 守：吸收、反射、无效 N 次物理攻击
//	MAGIC_FieldAttChange / AttReverse 战场属性、属性反转
//	MAGIC_MagicStatusChange          魔抗（抗魔值 +n%）/ 铁壁（防御 +n..n+19%）；火 / 电 / 冰抗是职业技能用的，这里没有效果
//	MAGIC_AttMagic                   攻击魔法（attmagic.bin 的范围，_FIX_MAGICDAMAGE 的公式与熟练度）
// MAGIC_Metamo（变身）只在地图上有意义；ToCallDragon（召唤）、AttSkill 还没做，当作没有效果。

// MagicSkill 是魔法熟练度（CHAR_EARTH_EXP.. / EARTH_RESIST.. / ATTMAGIC_EXP.. / DEFMAGIC_EXP..，地水火风），
// 玩家与宠物才有，由世界层保存；Fighter.Mag 指向世界层的那一份，战斗中直接改。
type MagicSkill struct {
	Lv, Res, AtkExp, DefExp [4]int
}

// 咒术的效果动画（anim_tbl.h）
const (
	sprMagicUser   = 100600 // MAGIC_EFFECT_USER
	sprHeal1       = 100601
	sprHeal2       = 100602
	sprHeal3       = 100603
	sprCure        = 100604 // SPR_tyusya
	sprStatus      = 100605 // SPR_hoshi
	sprReverse     = 100606
	sprRevive1     = 100607
	sprRevive2     = 100608
	sprRevive3     = 100609
	sprMagicDef    = 100610
	sprMagicResist = 101411
)

// 魔法的效果结果（Event.Result）
const (
	MagicHeal   Result = "heal"
	MagicRevive Result = "revive"
	MagicCure   Result = "cure"
	MagicHit    Result = "hit"  // 附加状态成功 / 守护类生效
	MagicFail   Result = "fail" // 附加状态没成功
	MagicDamage Result = "damage"
)

var elemChar = []string{"地", "水", "火", "风"}

// magicNum 取参数里某个关键字后面的数字，如 "毒 turn 3 成 20" 的 turn → 3。
func magicNum(opt, key string, def int) int {
	if v, ok := optNum(opt, key); ok {
		return v
	}
	return def
}

// magicTargets 是 BATTLE_MultiList（dead 时是 MultiListDead）：把目标编号展开成位置。
// 单人目标不在了（倒下或离场）时改为同一方随机一个还在的人；一排没有人时改为同一方的另一排。
func (b *Battle) magicTargets(toNo int, dead bool) []int {
	ok := func(i int) bool {
		f := b.F[i]
		if f == nil || f.Escaped || f.hidden {
			return false
		}
		if dead {
			return f.HP <= 0
		}
		return f.HP > 0
	}
	span := func(lo, hi int) []int {
		var out []int
		for i := lo; i <= hi; i++ {
			if ok(i) {
				out = append(out, i)
			}
		}
		return out
	}
	switch {
	case toNo >= 0 && toNo < Slots:
		if ok(toNo) {
			return []int{toNo}
		}
		if dead {
			return nil
		}
		side := toNo / SideOffset * SideOffset
		if cand := span(side, side+SideOffset-1); len(cand) > 0 {
			return []int{cand[b.randi(0, len(cand)-1)]}
		}
	case toNo == 20:
		return span(0, 9)
	case toNo == 21:
		return span(10, 19)
	case toNo == 22:
		return span(0, 19)
	case toNo >= 23 && toNo <= 26:
		lo := map[int]int{26: 0, 25: 5, 23: 10, 24: 15}[toNo]
		if r := span(lo, lo+4); len(r) > 0 {
			return r
		}
		other := map[int]int{0: 5, 5: 0, 10: 15, 15: 10}[lo]
		return span(other, other+4)
	}
	return nil
}

// castMagic 是 MAGIC_DirectUse：沉默中不能吟唱；MP 不够不能用（先扣 MP 再施法）；按函数名执行。
func (b *Battle) castMagic(f *Fighter, c Command, ev *[]Event) {
	m := c.Magic
	base := Event{Actor: f.Slot, Kind: Magic, Target: -1, Guardian: -1, Skill: m.Name, HP: f.HP, Status: f.Status()}
	if f.status[StNocast] > 0 {
		base.Result = MagicFail
		*ev = append(*ev, base)
		b.say(f, "沉默中无法吟唱咒术")
		return
	}
	if f.MP < c.MP {
		base.Result = MagicFail
		*ev = append(*ev, base)
		return
	}
	f.MP -= c.MP
	first := len(*ev)
	switch m.Func {
	case "MAGIC_Recovery", "MAGIC_OtherRecovery":
		b.magicRecovery(f, c, ev)
	case "MAGIC_Ressurect", "MAGIC_ResAndDef":
		b.magicRessurect(f, c, ev)
	case "MAGIC_StatusRecovery":
		b.magicStatusRecovery(f, c, ev)
	case "MAGIC_StatusChange", "MAGIC_Weaken", "MAGIC_Barrier", "MAGIC_Nocast":
		b.magicStatusChange(f, c, ev)
	case "MAGIC_MagicDef":
		b.magicDef(f, c, ev)
	case "MAGIC_FieldAttChange":
		b.magicField(f, c, ev)
	case "MAGIC_AttReverse":
		b.magicReverse(f, c, ev)
	case "MAGIC_MagicStatusChange":
		b.magicStatus2(f, c, ev)
	case "MAGIC_AttMagic":
		b.attackMagic(f, c, ev)
	case "MAGIC_ToCallDragon":
		b.toCallDragon(f, c, ev)
	case "MAGIC_Metamo":
		b.battleMetamo(f, c, ev, leadNum(m.Option), false)
	}
	if len(*ev) == first {
		*ev = append(*ev, base) // 没有对象或没有效果
	}
	for i := first; i < len(*ev)-1; i++ {
		(*ev)[i].Combo = true
	}
	for i := first; i < len(*ev); i++ {
		(*ev)[i].ActorMP = f.MP
	}
}

// say 是施法者自己看得到的系统讯息（CHAR_talkToCli），由世界层送出。
func (b *Battle) say(f *Fighter, text string) {
	if b.Notice != nil {
		b.Notice(f.Slot, text)
	}
}

func (b *Battle) magicEvent(f, t *Fighter, c Command, r Result, effect int) Event {
	return Event{Actor: f.Slot, Kind: Magic, Target: t.Slot, Guardian: -1, Skill: c.Magic.Name, Result: r, Effect: effect,
		HP: t.HP, Status: t.Status(), ActorHP: f.HP, RideHP: t.rideHP()}
}

// otherSide：目标里有对方的人（_PREVENT_TEAMATTACK：玩家不能回复敌人、不能对自己人附加状态）。
func otherSide(f *Fighter, list []int, other bool) bool {
	for _, i := range list {
		if (i/SideOffset != f.Side()) == other {
			return true
		}
	}
	return false
}

func (b *Battle) magicRecovery(f *Fighter, c Command, ev *[]Event) {
	p := magicNum(" "+c.Magic.Option, " ", 0)
	list := b.magicTargets(c.Target, false)
	if c.Magic.Target == data.MagicTargetMyself {
		list = []int{f.Slot}
	}
	if f.Player && !c.FromItem && !b.PvP && otherSide(f, list, true) {
		// MAGIC_Recovery / MAGIC_OtherRecovery 的 _PREVENT_TEAMATTACK（MP 已经扣了；PvP 不检查）
		if c.Magic.Func == "MAGIC_OtherRecovery" {
			b.say(f, "滋润精灵..不得施予非玩家敌方。")
		} else {
			b.say(f, "恩惠精灵..不得施予非玩家敌方。")
		}
		return
	}
	effect := sprHeal3
	switch {
	case p <= 100:
		effect = sprHeal1
	case p <= 300:
		effect = sprHeal2
	}
	for _, i := range list {
		t := b.F[i]
		heal := int(b.rand(float64(p)*0.9, float64(p)*1.1))
		if t.riding() {
			// 骑着时按两边缺的 HP 比例分
			cm, pm := t.MaxHP-t.HP, t.Ride.MaxHP-t.Ride.HP
			ch := heal * cm * 100 / max(1, cm+pm) / 100
			t.Ride.HP = min(t.Ride.MaxHP, t.Ride.HP+heal-ch)
			heal = ch
		}
		t.HP = min(t.MaxHP, t.HP+heal)
		if t.Pet && !t.recovered {
			t.recovered = true
			t.AIDelta += aiFixPetRecovery
		}
		e := b.magicEvent(f, t, c, MagicHeal, effect)
		e.Damage = heal
		*ev = append(*ev, e)
	}
}

func (b *Battle) magicRessurect(f *Fighter, c Command, ev *[]Event) {
	opt := c.Magic.Option
	p := magicNum(" "+opt, " ", 0)
	effect := sprRevive3
	switch {
	case p <= 0:
		effect = sprRevive3
	case p <= 100:
		effect = sprRevive1
	case p <= 300:
		effect = sprRevive2
	}
	if c.Magic.Func == "MAGIC_ResAndDef" {
		effect = sprMagicDef
	}
	for _, i := range b.magicTargets(c.Target, true) {
		t := b.F[i]
		if b.PvP && t.Player {
			continue // BATTLE_MultiRessurect：PvP 不能复活玩家（宠物可以）
		}
		hp := t.MaxHP
		if p > 0 {
			hp = min(t.MaxHP, max(1, int(b.rand(float64(p)*0.9, float64(p)*1.1))))
		}
		t.HP, t.status, t.KilledBy = hp, [stCount]int{}, -1
		if t.Pet {
			t.AIDelta += aiFixPetRessurect
		}
		if c.Magic.Func == "MAGIC_ResAndDef" {
			setMagicDef(t, opt)
		}
		e := b.magicEvent(f, t, c, MagicRevive, effect)
		e.Damage = hp
		*ev = append(*ev, e)
	}
}

// setMagicDef：参数 "吸 turn 1" / "反 turn 1" / "无 turn 2"。
func setMagicDef(t *Fighter, opt string) {
	n := magicNum(opt, "turn", 1)
	switch {
	case strings.Contains(opt, "吸"):
		t.Absorb = n
	case strings.Contains(opt, "反"):
		t.Reflect = n
	case strings.Contains(opt, "无"):
		t.Vanish = n
	}
}

func (b *Battle) magicStatusRecovery(f *Fighter, c Command, ev *[]Event) {
	want := statusOf(c.Magic.Option)
	for _, i := range b.magicTargets(c.Target, false) {
		t := b.F[i]
		top := 0
		for s := stCount - 1; s > 0; s-- {
			if t.status[s] > 0 {
				top = s
				break
			}
		}
		if top == 0 || (want != 0 && want != top) {
			continue
		}
		t.status[top] = 0
		*ev = append(*ev, b.magicEvent(f, t, c, MagicCure, sprCure))
	}
}

func (b *Battle) magicStatusChange(f *Fighter, c Command, ev *[]Event) {
	opt := c.Magic.Option
	st := statusOf(opt)
	if st <= 0 {
		return
	}
	turn, per := magicNum(opt, "turn", 0), magicNum(opt, "成", 15)
	list := b.magicTargets(c.Target, false)
	if f.Player && !c.FromItem && !b.PvP && otherSide(f, list, false) {
		b.say(f, "无法对己方施予异常状态。")
		return
	}
	for _, i := range list {
		t := b.F[i]
		ok := !(st == StNocast && t.Pet) && b.statusChance(f, t, st, per, 30, 1)
		if ok {
			switch st {
			case StWeaken, StBarrier:
				t.status[st] = turn + 1
			default:
				t.status[st] = turn
			}
			if st == StParalysis || st == StSleep || st == StStone || st == StBarrier {
				t.charge = -1 // 这回合的指令也取消（行动时 canMove 不成立）
			}
		}
		r := MagicFail
		if ok {
			r = MagicHit
		}
		e := b.magicEvent(f, t, c, r, sprStatus)
		e.OK = ok
		*ev = append(*ev, e)
	}
}

func (b *Battle) magicDef(f *Fighter, c Command, ev *[]Event) {
	list := b.magicTargets(c.Target, false)
	if f.Player && !c.FromItem && otherSide(f, list, true) {
		b.say(f, "光镜守..不得施予非玩家敌方。")
		return
	}
	for _, i := range list {
		t := b.F[i]
		setMagicDef(t, c.Magic.Option)
		*ev = append(*ev, b.magicEvent(f, t, c, MagicHit, sprMagicDef))
	}
}

// magicField 是 MAGIC_FieldAttChange："地 60 turn 3"（强度不在 0..100 时为 30，回合预设 3），"无" 回到无属性。
func (b *Battle) magicField(f *Fighter, c Command, ev *[]Event) {
	opt := c.Magic.Option
	attr := 0
	for i, ch := range elemChar {
		if strings.HasPrefix(strings.TrimSpace(opt), ch) {
			attr = i + 1
		}
	}
	b.fieldAttr, b.fieldPow, b.fieldTurns = attr, 30, 0
	if attr > 0 {
		w := strings.Fields(opt)
		if len(w) > 1 {
			if v, err := strconv.Atoi(w[1]); err == nil && v >= 0 && v <= 100 {
				b.fieldPow = v
			}
		}
		b.fieldTurns = magicNum(opt, "turn", 3)
	}
	*ev = append(*ev, Event{Actor: f.Slot, Kind: FieldAttr, Target: -1, Guardian: -1, Skill: c.Magic.Name, Status: attr})
}

func (b *Battle) magicReverse(f *Fighter, c Command, ev *[]Event) {
	for _, i := range b.magicTargets(c.Target, false) {
		t := b.F[i]
		t.reversed = !t.reversed
		e := b.magicEvent(f, t, c, MagicHit, sprReverse)
		e.OK = t.reversed
		*ev = append(*ev, e)
	}
}

// magicStatus2 是 MAGIC_MagicStatusChange："魔抗|3|20|单"（单人时目标不能是一方全体）；已有这类效果的不重复。
func (b *Battle) magicStatus2(f *Fighter, c Command, ev *[]Event) {
	p := strings.Split(c.Magic.Option, "|")
	for len(p) < 4 {
		p = append(p, "")
	}
	if p[3] == "单" && c.Target >= 20 {
		return
	}
	effect := sprHeal2
	switch p[0] {
	case "魔抗":
		effect = sprMagicResist
	case "火抗":
		effect = 101802
	case "电抗":
		effect = 101804
	case "冰抗":
		effect = 101803
	}
	for _, i := range b.magicTargets(c.Target, false) {
		t := b.F[i]
		if t.magStatus > 0 {
			continue
		}
		t.magStatus, t.magTurns, t.magN = magicStatusKind(p[0]), data.Atoi(p[1]), data.Atoi(p[2])
		*ev = append(*ev, b.magicEvent(f, t, c, MagicHit, effect))
	}
}

// 魔法状态（CHAR_DEFMAGICSTATUS / MAGICSUPERWALL …）
const (
	magNone = iota
	magResist
	magSuperWall
	magFire
	magThunder
	magIce
)

func magicStatusKind(s string) int {
	for i, k := range []string{"", "魔抗", "铁壁", "火抗", "电抗", "冰抗"} {
		if k != "" && s == k {
			return i
		}
	}
	return magNone
}

// tickMagicStatus 是 BATTLE_MagicStatusSeq：每回合结束时魔法状态的回合数 −1。
func (b *Battle) tickMagicStatus() {
	for _, f := range b.F {
		if f != nil && f.magStatus > 0 {
			if f.magTurns--; f.magTurns <= 0 {
				f.magStatus, f.magN = 0, 0
			}
		}
	}
}

// charTable 是 battle_magic.c CharTable：四排（敌方后排、敌方前排、己方宠物、己方人物）× 五格的位置，中间优先。
var charTable = [4][5]int{
	{13, 11, 10, 12, 14},
	{18, 16, 15, 17, 19},
	{8, 6, 5, 7, 9},
	{3, 1, 0, 2, 4},
}

// charPos 是 CharTableIdx：位置 → (排, 格)。
func charPos(slot int) (row, col int) {
	for r := range charTable {
		for k, s := range charTable[r] {
			if s == slot {
				return r, k
			}
		}
	}
	return 0, 0
}

// attMagicArea 是 BATTLE_MultiAttMagic 的范围：attmagic.bin 的 siField 以目标为中心套上去（只算目标那一方、还站着的人）。
func (b *Battle) attMagicArea(field [3][5]bool, toNo int) []int {
	alive := func(s int) bool { return b.F[s].Alive() && !b.F[s].hidden }
	var out []int
	switch {
	case toNo >= 0 && toNo < Slots:
		br, bc := charPos(toNo)
		for i, j := 0, br-1; j <= br+1; i, j = i+1, j+1 {
			if toNo < 10 && (j < 2 || j > 3) || toNo >= 10 && (j < 0 || j > 1) {
				continue
			}
			for k := 0; k < 5; k++ {
				x := bc - 2 + k
				if x >= 0 && x <= 4 && field[i][k] && alive(charTable[j][x]) {
					out = append(out, charTable[j][x])
				}
			}
		}
	case toNo == 20 || toNo == 21:
		r0 := 2
		if toNo == 21 {
			r0 = 0
		}
		for i := 0; i < 2; i++ {
			for j := 0; j < 5; j++ {
				if field[i][j] && alive(charTable[r0+i][j]) {
					out = append(out, charTable[r0+i][j])
				}
			}
		}
	case toNo >= 23 && toNo <= 26:
		br := toNo - 23
		for i, j := 0, br-1; j <= br+1; i, j = i+1, j+1 {
			if toNo >= 25 && (j < 2 || j > 3) || toNo <= 24 && (j < 0 || j > 1) {
				continue
			}
			for k := 0; k < 5; k++ {
				if field[i][k] && alive(charTable[j][k]) {
					out = append(out, charTable[j][k])
				}
			}
		}
	}
	return out
}

// attackMagic 是 MAGIC_AttMagic + BATTLE_MultiAttMagic（_FIX_MAGICDAMAGE）：参数 "火|100|1" 是属性、威力、魔法等级。
//
//	熟练度 L：玩家与宠物是那个属性的魔法熟练度，敌人是等级 × 0.9；抗魔 R：玩家与宠物是抗魔值，敌人是等级 × 0.5（魔抗再加 n%）
//	这次算不算成功：rand()%100 ≤ L；闪避：玩家 运气×3 + 抗魔×0.15，其余 等级×0.2（最多 30）
//	伤害：K = max(0, 1.4L − R)，A = K² / max(1,L)² + (rand()%20)/100，威力 × (1 + 魔法等级/10) × A，
//	      再按"魔法牵引属性"（那个属性 10×等级 + 10×等级×(原属性/50)）算相克与战场属性；没成功 ×0.7
//	骑着时按 BATTLE_CalcCharaRatio 分给骑宠；挨打的醒过来；没成功时玩家 / 宠物涨熟练度，伤害 200 以上时挨打的涨抗魔
func (b *Battle) attackMagic(f *Fighter, c Command, ev *[]Event) {
	p := strings.Split(c.Magic.Option, "|")
	for len(p) < 3 {
		p = append(p, "")
	}
	elem := -1
	for i, ch := range elemChar {
		if strings.HasPrefix(p[0], ch) {
			elem = i
		}
	}
	power, lv := data.Atoi(p[1]), data.Atoi(p[2])
	idx := c.Magic.Idx*2 + 1
	if f.Slot >= SideOffset {
		idx = c.Magic.Idx * 2
	}
	if elem < 0 || c.Magic.Idx < 0 || idx >= len(b.AttMagics) {
		return
	}
	am := b.AttMagics[idx]
	toNo := c.Target
	if toNo >= 0 && toNo < Slots && !(b.F[toNo].Alive() && !b.F[toNo].hidden) {
		cand := b.targetable(1 - f.Side())
		if len(cand) == 0 {
			return
		}
		toNo = cand[b.randi(0, len(cand)-1)]
	}
	list := b.attMagicArea(am.Field, toNo)
	L := 0
	if f.Mag != nil {
		L = f.Mag.Lv[elem]
	} else if f.Enemy() {
		L = int(float64(f.Level) * 0.9)
	}
	trueMagic := b.Rnd.IntN(100) <= L
	hits := 0
	for _, i := range list {
		t := b.F[i]
		e := b.magicEvent(f, t, c, MagicDamage, am.Sprite)
		// 客户端放动画的位置（BuildAttackMagicData）：攻击方式|时间差|显示方式|x|y
		e.Att = fmt.Sprintf("%d|%d|%d|%d|%d", am.AttackType, am.SliceTime, am.ShowType, am.Sx, am.Sy)
		if b.magicDodge(t, elem) {
			e.Result = Dodge
			*ev = append(*ev, e)
			continue
		}
		R := 0
		if t.Mag != nil {
			R = t.Mag.Res[elem]
		} else if t.Enemy() {
			R = int(float64(t.Level) * 0.5)
		}
		if t.magStatus == magResist && R > 0 {
			R += R * t.magN / 100
		}
		K := math.Max(0, float64(L)*1.4-float64(R))
		M := math.Max(1, float64(L))
		A := K*K/(M*M) + float64(b.Rnd.IntN(20))/100
		apower := int(float64(power) * (1 + float64(lv)/10) * A)
		dmg := b.magicAdjust(f, t, lv, elem, apower)
		if !trueMagic {
			dmg = int(float64(dmg) * 0.7)
		}
		hits++
		if t.Mag != nil {
			b.magicDefExp(t, elem, lv, dmg)
		}
		// 骑着时分给骑宠（BATTLE_CalcCharaRatio）
		if t.riding() {
			ch := dmg * b.charaRatio(t, elem) / 10
			pet := dmg - ch
			if t.HP-ch < 0 {
				ch = t.HP
			}
			t.Ride.HP -= pet
			if t.Ride.HP <= 0 {
				t.Ride.HP, t.Ride.Fell, e.Fall = 0, true, true
			}
			e.PetDamage = pet
			dmg = ch
		}
		t.HP -= dmg
		if t.status[StSleep] > 0 {
			t.status[StSleep] = 0
		}
		if t.HP <= 0 {
			t.HP, e.Dead = 0, true
			t.KilledBy, t.KilledRide = f.Slot, f.riding()
		}
		e.Damage, e.HP, e.Status, e.RideHP = dmg, t.HP, t.Status(), t.rideHP()
		*ev = append(*ev, e)
	}
	if !trueMagic && f.Mag != nil && hits > 0 {
		b.magicAtkExp(f, elem, lv, hits)
	}
}

// magicDodge 是 BATTLE_MagicDodge。
func (b *Battle) magicDodge(t *Fighter, elem int) bool {
	luck := 0.0
	if t.Player {
		luck = float64(t.Luck) * 3
		if t.Mag != nil {
			luck += float64(t.Mag.Res[elem]) * 0.15
		}
	} else {
		luck = math.Min(30, float64(t.Level)*0.2)
	}
	return b.Rnd.IntN(100)+1 <= int(luck)
}

// magicAttrs 是 battle_magic.c 的 BATTLE_GetAttr：四属性与"无"，骑着时与骑宠平均。
func magicAttrs(f *Fighter) ([4]int, int) {
	a := f.attrs()
	if f.riding() {
		for i := range a {
			a[i] = (a[i] + f.Ride.Attr[i]) / 2
		}
	}
	none := 100
	for i := range a {
		a[i] = max(0, a[i])
		none -= a[i]
	}
	return a, max(0, none)
}

// magicAdjust 是 BATTLE_getMagicAdjustInt：施法者的属性换成魔法牵引属性后算相克，再乘战场属性。
func (b *Battle) magicAdjust(f, t *Fighter, lv, elem, dmg int) int {
	at, atNone := magicAttrs(f)
	ml := lv * 10
	var pull [4]int
	pull[elem] = ml + ml*(at[elem]/50)
	df, dfNone := magicAttrs(t)
	out := attrAdjustN(pull, atNone, df, dfNone, dmg)
	return int(float64(out) * b.fieldPower(pull) / b.fieldPower(df))
}

// charaRatio 是 BATTLE_CalcCharaRatio：骑着时人分到的比例（÷10，2..8）。
func (b *Battle) charaRatio(t *Fighter, elem int) int {
	eff := func(a [4]int) int {
		v := (100*a[(elem+1)%4] - 100*a[elem]) / 1000
		return 20 + max(-10, min(10, v))
	}
	ch, pt := eff(t.Attr), eff(t.Ride.Attr)
	r := 10 * ch / (ch + pt)
	return max(2, min(8, r))
}

// magicAtkExp 是 Magic_ComputeAttExp：没成功的魔法涨那个属性的熟练度（等级 × 3 × 打到几人，超过 100 升一级），相克的属性减一半。
func (b *Battle) magicAtkExp(f *Fighter, elem, lv, hits int) {
	m := f.Mag
	add := lv * 3 * hits
	m.AtkExp[elem] += add
	if m.AtkExp[elem] > 100 {
		m.AtkExp[elem] = 0
		if m.Lv[elem] < 100 {
			m.Lv[elem]++
			b.say(f, elemChar[elem]+"魔法熟练度提升为"+strconv.Itoa(m.Lv[elem])+"。")
		}
	}
	o := (elem + 1) % 4
	if m.Lv[o] > 1 {
		m.AtkExp[o] -= add / 2
		if m.AtkExp[o] < 0 {
			m.AtkExp[o] = 0
			m.Lv[o]--
			b.say(f, elemChar[o]+"魔法熟练度下降为"+strconv.Itoa(m.Lv[o])+"。")
		}
	}
}

// magicDefExp 是 Magic_ComputeDefExp：受到 200 以上的魔法伤害时涨抗魔（伤害/20 × 等级 × 2，超过 100 升一级），相克的属性 −2。
func (b *Battle) magicDefExp(t *Fighter, elem, lv, dmg int) {
	if dmg < 200 {
		return
	}
	m := t.Mag
	m.DefExp[elem] += dmg / 20 * lv * 2
	if m.DefExp[elem] > 100 {
		m.DefExp[elem] = 0
		if m.Res[elem] < 100 {
			m.Res[elem]++
			b.say(t, elemChar[elem]+"抗魔值提升为"+strconv.Itoa(m.Res[elem])+"。")
		}
	}
	o := (elem + 1) % 4
	if m.Res[o] > 1 {
		if m.DefExp[o] -= 2; m.DefExp[o] < 0 {
			m.DefExp[o] = 90
			m.Res[o]--
			b.say(t, elemChar[o]+"抗魔值下降为"+strconv.Itoa(m.Res[o])+"。")
		}
	}
}

// magicRows 是 battle.c TargetIndex：宠物攻击魔法里打一排（26）或一方全体（20）的魔法，其余是单人。
var magicRows = map[int]int{303: 26, 311: 26, 317: 26, 321: 26, 323: 26, 305: 20, 306: 20, 312: 20, 318: 20, 324: 20, 325: 20}

// skillMagic 把用咒术的技能换成咒术指令（不扣 MP）：
//
//	PETSKILL_AttackMagic       参数 "magic N"（预设 313），一排的打目标那一排、全体的打目标那一方
//	                           （原版宠物对敌人用一排的魔法时会算成己方那一排，这里照目标那一方算）
//	PETSKILL_Combined          "综合法|N|id1|…"：随机一个 magic.txt 的咒术；回复、净化、守护类对自己这一方用
//	PETSKILL_Nocast / Barrier / Weaken / MagicStatusChange   参数同咒术；敌人用时原版只打到选的那一个人
func (b *Battle) skillMagic(f *Fighter, s *data.Skill, target int) (Command, bool) {
	var m *data.Magic
	switch s.Func {
	case "PETSKILL_AttackMagic":
		id := magicNum(s.Option, "magic", 313)
		if m = b.Magics[id]; m == nil {
			return Command{}, false
		}
		switch magicRows[id] {
		case 26:
			if target >= 0 && target < Slots {
				target = map[int]int{0: 26, 1: 25, 2: 23, 3: 24}[target/5]
			}
		case 20:
			if target >= 10 {
				target = 21
			} else {
				target = 20
			}
		}
	case "PETSKILL_Combined":
		p := strings.Split(s.Option, "|")
		if len(p) < 3 {
			return Command{}, false
		}
		ids := p[2:]
		if n := data.Atoi(p[1]); n > 0 && n < len(ids) {
			ids = ids[:n]
		}
		if m = b.Magics[data.Atoi(ids[b.randi(0, len(ids)-1)])]; m == nil {
			return Command{}, false
		}
		switch m.Func {
		case "MAGIC_Recovery", "MAGIC_OtherRecovery", "MAGIC_StatusRecovery", "MAGIC_MagicDef", "MAGIC_Ressurect", "MAGIC_ResAndDef":
			target = f.Slot
			if m.Target == data.MagicTargetWholeOtherSide || m.Target == data.MagicTargetAllMySide {
				target = 20 + f.Side()
			}
		}
	default:
		fn := map[string]string{"PETSKILL_Nocast": "MAGIC_Nocast", "PETSKILL_Barrier": "MAGIC_Barrier",
			"PETSKILL_Weaken": "MAGIC_Weaken", "PETSKILL_MagicStatusChange": "MAGIC_MagicStatusChange",
			"PETSKILL_Refresh": "MAGIC_StatusRecovery", "PETSKILL_Deeppoison": "MAGIC_StatusChange"}[s.Func]
		opt := s.Option
		if s.Func == "PETSKILL_Deeppoison" {
			// PETSKILL_Deeppoison_Battle：BATTLE_MultiStatusChange 的回合数是参数 + 2
			opt = "剧 turn " + strconv.Itoa(magicNum(opt, "turn", 0)+2) + " 成 " + strconv.Itoa(magicNum(opt, "成", 15))
		}
		m = &data.Magic{Name: s.Name, Func: fn, Option: opt, Target: s.Target, Idx: -1}
		// 用"我方全体 / 对方全体"的技能：点哪个都是那一方全体
		if s.Target == data.MagicTargetAllOtherSide || s.Target == data.MagicTargetAllMySide {
			target = skillTargets(f, s, target)
		}
		if s.Func == "PETSKILL_MagicStatusChange" && strings.HasSuffix(s.Option, "全") {
			target = 20 + f.Side()
		}
	}
	return Command{Kind: Magic, Magic: m, Target: target, Skill: s}, true
}
