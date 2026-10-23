package battle

import (
	"math"

	"sa/server/internal/data"
)

// 宠物在战斗中的规则（gmsv/battle/battle.c、battle_event.c）：
//   捕获        BATTLE_Capture / BATTLE_CaptureCheck
//   听不听指令  BATTLE_PetLoyalCheck（忠诚度 CHAR_WORKFIXAI 分档）与 BATTLE_PetRandomSkill
//   忠诚度变动  AI_FIX_*（battle.h），这里累计在 Fighter.AIDelta

// 忠诚度变动（battle.h AI_FIX_*，CHAR_VARIABLEAI 的单位：100 为 1 点忠诚度）
const (
	AIFixPetLevelUp   = 5 * 100
	AIFixPetWin       = 1
	AIFixPetGoldWin   = 2 * 10
	aiFixSekkan       = -2 * 100
	aiFixPetRecovery  = 10      // AI_FIX_PETRECOVERY：战斗中被回复（每场一次）
	aiFixPetRessurect = 3 * 100 // AI_FIX_PETRESSURECT：战斗中被复活
	AIFixPlayerDead   = -1 * 100
	AIFixPetDead      = -5 * 100
	CHFixPlayerDead   = -2 // CH_FIX_PLAYERDEAD：玩家倒下时魅力
	CHFixPetEscape    = -1 // CH_FIX_PETESCAPE：宠物逃走时主人的魅力
	// 击飞（BATTLE_UltimateExtra）：玩家被击飞的魅力、他的宠物与被击飞的宠物的忠诚度（10 级以下减半）
	CHFixPlayerUltimate = -4
	AIFixPlayerUltimate = -10 * 100
	AIFixPetUltimate    = -10 * 100
	DefaultMaxAI        = 60 // CHAR_DEFAULTMAXAI：刚捕获的宠物忠诚度最多 60
	MaxVariableAI       = 100 * 100
	MinVariableAI       = -100 * 100
	petSkillSearches    = 50 // PETSKILLSERCHTIME
)

// CaptureRate 是 BATTLE_CaptureCheck 的捕获机率（%）：
// (10 − HP²/最大HP + 自己等级/2 − 对方等级/2 + 自己敏捷/15 − 对方敏捷/15 + 捕获难度 + 运气) × 魅力 / 50，
// 对方睡着时 +15，最多 99。不是可捕获的敌人、或对方比自己高 5 级以上时为 0（不能捕获）。
func CaptureRate(a, d *Fighter) float64 {
	if !d.Enemy() || d.Seed == nil || !d.Seed.Capture || a.Level+5 < d.Level {
		return 0
	}
	maxHP := float64(max(1, d.MaxHP))
	hp := float64(d.HP)
	per := (10 - hp*hp/maxHP) + (float64(a.Level)/2 - float64(d.Level)/2) +
		(float64(a.FixQuick)/15 - float64(d.FixQuick)/15) + float64(d.Seed.Get+a.Luck)
	per = per * float64(a.Charm) / 50
	if d.status[StSleep] > 0 {
		per += 15
	}
	return math.Min(per, 99)
}

// capture 是 BATTLE_Capture：目标不在就改抓对方随机一个（BATTLE_TargetAdjust）；
// 先做世界层的检查（必要道具、宠物栏空位），再按机率 RAND(1,100) < 机率 判定。抓到的敌人离开战场。
func (b *Battle) capture(f *Fighter, target int, ev *[]Event) {
	t := b.pickTarget(f, target)
	if t == nil {
		*ev = append(*ev, Event{Actor: f.Slot, Kind: None, Target: -1, HP: f.HP, Status: f.Status(), Guardian: -1})
		return
	}
	ok := b.CanCapture == nil || b.CanCapture(f, t)
	if ok {
		per := CaptureRate(f, t)
		if per > 0 {
			per = math.Min(99, per+float64(f.CaptureUp)) // WORKMODCAPTURE（捕获之果等）
		}
		ok = per > 0 && b.rand(1, 100) < per
	}
	f.CaptureUp = 0 // 捕捉一次后归零（battle_event.c）
	if ok {
		t.Escaped, t.Captured = true, true
	}
	*ev = append(*ev, Event{Actor: f.Slot, Kind: Capture, Target: t.Slot, OK: ok, HP: t.HP, Status: t.Status(), Guardian: -1, ActorHP: f.HP})
}

// 宠物不听话时的行为（PETAI_MODE_*）
const (
	petNormal = iota
	petTargetRandom
	petRandomAct
	petOwnerAttack
	petEnemyAttack
	petEscape
	petNoAct
)

// defaultAttacker 是 BATTLE_DefaultAttacker：某一方能当目标的人里随机一个，没有时 -1。
func (b *Battle) defaultAttacker(side int) int {
	cand := b.targetable(side)
	if len(cand) == 0 {
		return -1
	}
	return cand[b.randi(0, len(cand)-1)]
}

// loyalCheck 是 BATTLE_PetLoyalCheck：行动前按忠诚度决定宠物听不听话。
//
//	≥80 一定听；≥70/60/50/40 有 10/20/35/50% 改打随机目标；≥30、≥20 有 70% 随便用个技能；
//	≥10 有 80% 攻击主人、否则攻击敌人；不到 10 有 60% 攻击主人、否则逃走。
//
// 没有任何技能的宠物不听话时什么也不做。不听话时先送出 Disobey 事件。返回 false 表示这次不行动（已逃走）。
func (b *Battle) loyalCheck(f *Fighter, c Command, ev *[]Event) (Command, bool) {
	ai := f.Loyalty
	r := b.randi(1, 100)
	mode := petNormal
	switch {
	case ai >= 80:
	case ai >= 70:
		if r < 10 {
			mode = petTargetRandom
		}
	case ai >= 60:
		if r < 20 {
			mode = petTargetRandom
		}
	case ai >= 50:
		if r < 35 {
			mode = petTargetRandom
		}
	case ai >= 40:
		if r < 50 {
			mode = petTargetRandom
		}
	case ai >= 20:
		if r < 70 {
			mode = petRandomAct
		}
	case ai >= 10:
		if r < 80 {
			mode = petOwnerAttack
		} else {
			mode = petEnemyAttack
		}
	default:
		if r < 60 {
			mode = petOwnerAttack
		} else {
			mode = petEscape
		}
	}
	if mode == petNormal {
		return c, true
	}
	has := false
	for _, s := range f.Skills {
		has = has || s != nil
	}
	if !has {
		mode = petNoAct
	}
	*ev = append(*ev, Event{Actor: f.Slot, Kind: Disobey, Target: -1, HP: f.HP, Status: f.Status(), Guardian: -1})
	self := c.Kind == Guard || c.Target == f.Slot // 原本是防御或对自己用
	var n Command
	switch mode {
	case petNoAct:
		n = Command{Kind: None}
	case petTargetRandom:
		if self {
			n = Command{Kind: None}
		} else {
			side := 1
			if c.Target >= 0 && c.Target < SideOffset {
				side = 0
			}
			n = c
			n.Target = b.defaultAttacker(side)
		}
	case petRandomAct:
		if c.Target == f.Slot {
			n = Command{Kind: None}
		} else {
			n = b.petRandomSkill(f)
		}
	case petOwnerAttack:
		n = Command{Kind: Attack, Target: f.Slot - 5}
	case petEnemyAttack:
		n = Command{Kind: Attack, Target: b.defaultAttacker(1 - f.Side())}
	case petEscape:
		// BATTLE_LostEscape：离开战场，主人的出战宠物改为休息，主人魅力 −1（由世界层处理）
		f.Escaped = true
		*ev = append(*ev, Event{Actor: f.Slot, Kind: Leave, Target: -1, HP: f.HP, Status: f.Status(), Guardian: -1, OK: true})
		return c, false
	}
	f.guard = false
	return b.setup(f, n), true
}

// petRandomSkill 是 BATTLE_PetRandomSkill：随机挑技能栏的一格对随机一个敌人用。
// 原版在技能栏里绕圈找第 n 个战斗能用的技能（找 50 次），但最后用的还是第 n 格（PETSKILL_Use(iNum)），
// 所以那一格是空的或不能在战斗用时就什么也不做；攻击魔法与合体技不会被随机选到。
func (b *Battle) petRandomSkill(f *Fighter) Command {
	n := b.randi(0, len(f.Skills)-1)
	to := b.defaultAttacker(1 - f.Side())
	if to < 0 {
		return Command{Kind: None}
	}
	usable := func(s *data.Skill) bool {
		return s != nil && (s.Field == 0 || s.Field == 1) && s.Func != "PETSKILL_AttackMagic" && s.Func != "PETSKILL_Combined"
	}
	any := false
	for _, s := range f.Skills {
		any = any || usable(s)
	}
	if !any || !usable(f.Skills[n]) {
		return Command{Kind: None}
	}
	return Command{Kind: Attack, Target: to, Skill: f.Skills[n]}
}

// petSwap 是 BATTLE_COM_PETIN / PETOUT（BATTLE_PetIn、BATTLE_PetOut）：先收回在场的宠物（倒下的也收回，BATTLE_PetDefaultExit），
// 再叫出指定的宠物（BATTLE_PetDefaultEntry：倒下、HP 0 的叫不出来）。没有宠物可收回又不叫出时是"什么也不做"。
// 收回的宠物留在 Recalled，世界层按 BATTLE_Exit 结算它。
func (b *Battle) petSwap(f *Fighter, c Command, ev *[]Event) {
	slot := f.Slot + 5
	base := Event{Actor: f.Slot, Target: slot, Guardian: -1, HP: f.HP, Status: f.Status()}
	old := b.F[slot]
	if old == nil && c.Swap == nil {
		e := base
		e.Kind, e.Target = None, -1
		*ev = append(*ev, e)
		return
	}
	if old != nil {
		b.F[slot] = nil
		b.Recalled = append(b.Recalled, old)
		e := base
		e.Kind = PetIn
		*ev = append(*ev, e)
	}
	if p := c.Swap; p != nil && p.HP > 0 {
		p.Slot = slot
		b.Add(p)
		e := base
		e.Kind, e.Name, e.Sprite, e.Level, e.HP, e.MaxHP, e.Status = PetOut, p.Name, p.Sprite, p.Level, p.HP, p.MaxHP, StNone
		*ev = append(*ev, e)
	}
}
