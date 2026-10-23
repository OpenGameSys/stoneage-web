package battle

import (
	"strconv"
	"strings"

	"sa/server/internal/data"
)

// 技能（petskill.txt）按函数名实现，对应 gmsv/battle/pet_skill.c 的 PETSKILL_* 与 battle.c 里的执行：
//   PETSKILL_None / NormalAttack / NormalGuard   待机 / 攻击 / 防御
//   ContinuationAttack  连续攻击 N 下，每下伤害 ÷N
//   ChargeAttack        蓄力 N 回合，之后以攻击力 +攻% 攻击
//   Mighty              一击必杀：伤害 ×倍，对方闪避 +避
//   PowerBalance        背水之战：本回合攻/防按百分比增减后攻击
//   StatusChange        附带异常状态的攻击（毒/麻/眠/石/醉/乱 turn N），攻击力按攻% 减
//   GuardBreak          破除防御：只伤得了防御中的对手，且无视防御
//   GuardBreak2         破除防御之2：对方防御时 ×1.3，否则 ×0.7
//   NoGuard             不防守战法：本回合不攻击，被攻击时闪避与反击机率提高
//   Guardian            忠犬：守护同一列的另一排（位置 −5），自己攻击力 −20%
//   EarthRound          地球一周：本回合钻进地底（不会被打），下回合伤害 +攻%
//   WildViolentAttack   狂暴攻击：3..10 下，每下伤害 ÷次数，对方闪避 +避
//   DamageToHp          嗜血：伤害的一部分回到自己的 HP
//   MpDamage            MP 攻击：削减玩家 MP 的百分比
//   ENEMYSKILL_ReHP     敌人回复：随机回复一个 HP 不到 2/3 的同伴 100..最大 HP；没人要回复时改为攻击
//   ENEMYSKILL_ReLife   敌人复活：随机复活一个倒下的同伴为一半 HP；没有时改为攻击
// 其余技能（攻击魔法、沉默、魔障、偷窃、落马等）还没实现，按普通攻击处理。

// optNum 取技能参数里某个关键字后面的数字，如 "攻%+90" 的 攻% → 90，"回避30" 的 避 → 30。
func optNum(opt, key string) (int, bool) {
	i := strings.Index(opt, key)
	if i < 0 {
		return 0, false
	}
	s := strings.TrimLeft(opt[i+len(key):], " ")
	end := 0
	for end < len(s) && (s[end] >= '0' && s[end] <= '9' || s[end] == '.' || end == 0 && (s[end] == '+' || s[end] == '-')) {
		end++
	}
	f, err := strconv.ParseFloat(s[:end], 64)
	if err != nil {
		return 0, false
	}
	return int(f), true
}

// leadNum 是 sscanf(opt, "%d")：开头的整数。
func leadNum(opt string) int {
	n, _ := optNum(" "+opt, " ")
	return n
}

// field 取以 | 分隔的第 i 段（从 1 起）的整数。
func field(opt string, i int) int {
	p := strings.Split(opt, "|")
	if i > len(p) {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(p[i-1]))
	return n
}

// applyMods：攻%/防%/敏% 按基本值加减（PETSKILL_* 里 CHAR_WORKFIXSTR + FIXSTR × %）。
func applyMods(f *Fighter, opt string) {
	if v, ok := optNum(opt, "攻%"); ok {
		f.Atk = f.FixAtk + int(float64(f.FixAtk)*float64(v)/100)
	}
	if v, ok := optNum(opt, "防%"); ok {
		f.Def = f.FixDef + int(float64(f.FixDef)*float64(v)/100)
	}
	if v, ok := optNum(opt, "敏%"); ok {
		f.Quick = f.FixQuick + int(float64(f.FixQuick)*float64(v)/100)
	}
}

// setup 是下指令时的处理（PETSKILL_Use 调用各技能函数的部分）：设定本回合的数值加减、防御、守护、蓄力等。
func (b *Battle) setup(f *Fighter, c Command) Command {
	f.com = c.Kind
	if f.charge >= 0 || f.hidden {
		f.com = Attack
		return c
	}
	c = b.transformLimit(f, c) // 小狐狸 / 乌力的限制（item2.go）
	s := c.Skill
	if s != nil {
		if c2, ok := b.setup2(f, c); ok {
			s, c = nil, c2
		}
	}
	if s != nil {
		switch s.Func {
		case "PETSKILL_None":
			c = Command{Kind: None, Skill: s}
		case "PETSKILL_NormalAttack":
			c.Skill = nil
		case "PETSKILL_NormalGuard":
			c = Command{Kind: Guard, Skill: s}
		case "PETSKILL_GuardBreak", "PETSKILL_GuardBreak2", "PETSKILL_Mighty", "PETSKILL_PowerBalance",
			"PETSKILL_StatusChange", "PETSKILL_WildViolentAttack":
			applyMods(f, s.Option)
		case "PETSKILL_NoGuard":
			c = Command{Kind: NoGuard, Skill: s}
			f.duckMod, _ = optNum(s.Option, "避%")
			f.ctrMod, _ = optNum(s.Option, "击%")
		case "PETSKILL_Guardian":
			applyMods(f, s.Option)
			owner := f.Slot - 5
			if owner >= f.Side()*SideOffset && owner < f.Side()*SideOffset+SideOffset {
				b.guardian[owner] = f.Slot
			}
			if strings.Contains(s.Option, "COM:") && strings.Contains(s.Option, "防御") {
				c.Kind = Guard
			}
		case "PETSKILL_ChargeAttack":
			n := leadNum(s.Option)
			if n < 1 || n > 10 {
				n = 1
			}
			f.charge = n
			f.chargeP, _ = optNum(s.Option, "攻%")
		case "PETSKILL_EarthRound":
			c.Kind = Hide
			f.earthP, _ = optNum(s.Option, "攻%")
			f.earthT, f.earthN = c.Target, s.Name
		case "ENEMYSKILL_ReHP":
			c.Kind = Heal
		case "ENEMYSKILL_ReLife":
			c.Kind = Revive
		case "PETSKILL_AttackMagic", "PETSKILL_Combined", "PETSKILL_Nocast", "PETSKILL_Barrier", "PETSKILL_Weaken",
			"PETSKILL_MagicStatusChange", "PETSKILL_Refresh", "PETSKILL_Deeppoison":
			if mc, ok := b.skillMagic(f, s, c.Target); ok {
				c = mc
			} else {
				c = Command{Kind: None, Skill: s}
			}
		case "PETSKILL_ContinuationAttack":
		default:
			c.Skill = nil // 没做的技能（见 skill2.go 开头）当作普通攻击
		}
	}
	f.com = c.Kind
	if c.Kind == Attack && c.Skill != nil && !attackGroup[c.Skill.Func] {
		f.com = skillCom // 指令不是 BATTLE_COM_ATTACK，被打时不会反击
	}
	if c.Kind == Guard {
		f.guard = true
	}
	return c
}

// skillCom 是"用技能攻击、但不是普通攻击那一组"的本回合行动（BATTLE_Counter 只让 BATTLE_COM_ATTACK 的人反击）。
const skillCom Kind = "skill"

// attackGroup 是 battle.c 里和 BATTLE_COM_ATTACK 同一组 case 的技能：打完会把指令改成攻击，会引起、也会做反击。
var attackGroup = map[string]bool{"PETSKILL_ChargeAttack": true, "PETSKILL_PowerBalance": true, "PETSKILL_StatusChange": true,
	"PETSKILL_EarthRound": true, "PETSKILL_Mighty": true, "PETSKILL_Guardian": true, "PETSKILL_ContinuationAttack": true,
	"PETSKILL_AttackCrazed": true, "PETSKILL_WildViolentAttack": true, "PETSKILL_SpeedyAttack": true,
	"PETSKILL_BecomeFox": true, "PETSKILL_BecomePig": true, "PETSKILL_ShowMercy": true}

// skillAttack 执行攻击类的指令（普通攻击或攻击类技能）。
func (b *Battle) skillAttack(f *Fighter, c Command, ev *[]Event) {
	s := c.Skill
	if s == nil {
		o := attackOpts{hits: b.playerHits(f)}
		switch f.Weapon {
		case data.ItemFist:
			if o.hits > 1 {
				o.div = float64(o.hits) // 拳套类武器连击时伤害平分（gDamageDiv）
			}
		case data.ItemBreakThrow:
			o.status, o.turns = StParalysis, 0 // 碎石器打中附加麻痹 1 回合（BATTLE_CommandExec：gBattleStausChange = BATTLE_ST_PARALYSIS）
		case data.ItemBoomerang:
			b.boomerang(f, c.Target, ev)
			return
		}
		b.attackSeq(f, c.Target, o, ev)
		return
	}
	if b.skill2(f, c, ev) {
		return
	}
	o := attackOpts{name: s.Name}
	switch s.Func {
	case "PETSKILL_PowerBalance", "PETSKILL_WildViolentAttack", "PETSKILL_Mighty", "PETSKILL_GuardBreak", "PETSKILL_GuardBreak2":
		statEvents(f, s.Option, ev) // 背水之战等：先提示攻 / 防 / 敏的变化
	}
	switch s.Func {
	case "PETSKILL_ContinuationAttack":
		n := leadNum(s.Option)
		if n < 1 || n > 10 {
			n = 1
		}
		o.hits, o.div = n, float64(n)
	case "PETSKILL_Mighty":
		bai := 2.0
		if v, ok := optNum(s.Option, "倍"); ok {
			bai = float64(v)
		}
		o.dmgMod = bai
		o.duckMod, _ = optNum(s.Option, "避")
	case "PETSKILL_StatusChange":
		for i := 1; i < stCount; i++ {
			if strings.Contains(s.Option, statusChar[i]) {
				o.status = i
				break
			}
		}
		o.turns = 3
		if v, ok := optNum(s.Option, "turn"); ok {
			o.turns = v
		}
	case "PETSKILL_GuardBreak":
		o.gbreak, o.noCounter = 1, true
	case "PETSKILL_GuardBreak2":
		o.gbreak, o.noCounter = 2, true
	case "PETSKILL_WildViolentAttack":
		o.hits = b.randi(3, 10)
		o.div = float64(o.hits)
		o.duckMod, _ = optNum(s.Option, "避")
	case "PETSKILL_DamageToHp":
		// 攻击力在 setup2（第一段 / 100 是整数除法）；不会引起反击
		o.drain, o.noCounter = field(s.Option, 2), true
	case "PETSKILL_MpDamage":
		o.mpDamage, o.noCounter = field(s.Option, 2), true
	}
	b.attackSeq(f, c.Target, o, ev)
}

// playerHits 是 BATTLE_GetAttackCount 与 battle.c 的 attack_max：装备武器时攻击次数在武器的 [最小, 最大] 之间；
// 空手时 10 级以上的玩家按运气连击：RAND(1,1000) ≤ 10+运气×5 时 5..10 下，≤ 30+运气×5 时 3 下，≤ 70+运气×5 时 2 下，否则 1 下。
// shootKind 是远程武器的演出种类（送给客户端的 Event.Shoot）：弓 1、投掷斧 2、碎石器 3、回力镖 4，其余 0。
func shootKind(weapon int) int {
	switch weapon {
	case data.ItemBow:
		return 1
	case data.ItemBoundThrow:
		return 2
	case data.ItemBreakThrow:
		return 3
	case data.ItemBoomerang:
		return 4
	}
	return 0
}

// boomerangOrder 是 battle.c 的 BoomerangVsTbl：回力镖飞过一排的顺序（排内 4、2、0、1、3 号位）。
var boomerangOrder = [5]int{4, 2, 0, 1, 3}

// boomerang 是 BATTLE_COM_BOOMERANG：拿着回力镖普通攻击时打对方一整排，每人伤害 ×0.3（gBattleDamageModyfy）；
// 目标那一排（目标不在就按预设目标）不能是自己那一排；排内按 BoomerangVsTbl 的顺序、敌方（side 1）倒着飞；
// 投掷类武器不反击也不被反击（counterCheck）。同一次的几下标成 Combo，客户端合成一次飞行。
func (b *Battle) boomerang(a *Fighter, target int, ev *[]Event) {
	none := func() {
		*ev = append(*ev, Event{Actor: a.Slot, Kind: None, Target: -1, HP: a.HP, Status: a.Status(), Guardian: -1})
	}
	t := b.pickTarget(a, target)
	if t == nil {
		none()
		return
	}
	row := t.Slot / 5
	if row == a.Slot/5 {
		none()
		return
	}
	first := len(*ev)
	for k := 0; k < 5; k++ {
		i := k
		if a.Side() == 1 {
			i = 4 - k
		}
		d := b.F[row*5+boomerangOrder[i]]
		if !d.Alive() || d.hidden {
			continue
		}
		e, _ := b.attackOnce(a, d, attackOpts{dmgMod: 0.3, noCounter: true}, false)
		*ev = append(*ev, e)
		if over, _ := b.Over(); over || !a.Alive() {
			break
		}
	}
	if len(*ev) == first {
		none()
		return
	}
	for i := first; i < len(*ev)-1; i++ {
		(*ev)[i].Combo = true
	}
}

func (b *Battle) playerHits(f *Fighter) int {
	if f.Weapon >= 0 && f.AttackMax > 0 {
		return max(1, b.randi(max(1, f.AttackMin), f.AttackMax))
	}
	if !f.Player || f.Level < 10 {
		return 1
	}
	luck := min(5, max(0, f.Luck)) * 5
	r := b.randi(1, 1000)
	switch {
	case r <= 10+luck:
		return b.randi(5, 10)
	case r <= 30+luck:
		return 3
	case r <= 70+luck:
		return 2
	}
	return 1
}

// support 是敌人的回复与复活（BATTLE_E_ENEMYREHP / BATTLE_E_ENEMYREFILE），没有对象时改为普通攻击。
func (b *Battle) support(f *Fighter, c Command, ev *[]Event) {
	side := f.Side() * SideOffset
	var cand []int
	for i := side; i < side+SideOffset; i++ {
		x := b.F[i]
		if x == nil || x.Escaped {
			continue
		}
		if c.Kind == Heal && x.HP > 0 && x.HP < x.MaxHP*2/3 || c.Kind == Revive && x.HP <= 0 {
			cand = append(cand, i)
		}
	}
	if len(cand) == 0 {
		b.attackSeq(f, c.Target, attackOpts{}, ev)
		return
	}
	t := b.F[cand[b.randi(0, len(cand)-1)]]
	e := Event{Actor: f.Slot, Kind: c.Kind, Target: t.Slot, Guardian: -1, ActorHP: f.HP}
	if c.Skill != nil {
		e.Skill = c.Skill.Name
	}
	if c.Kind == Heal {
		e.Damage, e.PetDamage = t.heal(b.randi(100, t.MaxHP)) // 骑着时连骑宠一起回复
	} else {
		t.HP = max(1, t.MaxHP/2)
		t.status = [stCount]int{}
		e.Damage = t.HP
	}
	e.HP, e.Status = t.HP, t.Status()
	*ev = append(*ev, e)
}

// counterTable 是 battle_event.c 的 CounterTbl：按双方的武器类别查反击加成（BATTLE_ItemType2ItemMap）。
// 类别：0 无、1 爪（空手/拳套）、2 斧、3 棍、4 枪、5 弓、6 投掷。
func counterTable(atk, def int) int {
	cat := func(w int) int {
		switch w {
		case -1, data.ItemFist:
			return 1
		case data.ItemAxe:
			return 2
		case data.ItemClub:
			return 3
		case data.ItemSpear:
			return 4
		case data.ItemBow:
			return 5
		case data.ItemBoomerang, data.ItemBoundThrow, data.ItemBreakThrow:
			return 6
		}
		return 0
	}
	tbl := [7][8]int{
		{10, 9, 8, 8, 5, 0, 0, 0},
		{10, 9, 7, 7, 6, 0, 0, 0},
		{9, 8, 10, 10, 7, 0, 0, 0},
		{8, 8, 10, 10, 7, 0, 0, 0},
		{6, 6, 8, 8, 9, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0},
	}
	return tbl[cat(atk)][cat(def)]
}
