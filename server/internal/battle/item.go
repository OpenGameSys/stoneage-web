package battle

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"sa/server/internal/data"
)

// 战斗中使用道具（battle_item.c，"I|格位|目标"）：Command{Kind: UseItem, ItemFunc, ItemArg, ItemName, Target}。
// 目标编号同咒术（0..19 单人、20 / 21 一边、22 全体）；单人的对象不在时改为同一边随机一个还站着的（BATTLE_MultiList）。
//
//	ITEM_useRecovery       体 → HP、否则 气 → MP（只用一种；MP 的数值正好是 1 时补满，_MAGICPET_SKILL 的写法）；骑着时按两边缺的 HP 分
//	ITEM_useStatusChange   附加状态："毒 turn 10 成 300"（BATTLE_StatusAttackCheck(成, 30, 1.0)）
//	ITEM_useStatusRecovery 解除状态（只解最高编号的那一个）
//	ITEM_useCaptureUp      玩家的捕获机率 +RAND(0.9p, 1.1p)（累加，捕捉一次后归零）
//	ITEM_useImprecate      咒（每回合扣 MP，只对玩家）/ 恩（每回合回 HP）/ 祝（每回合回 MP，只对玩家）："咒:5x3" = 每回合 5、3 回合；整叠用掉
//	ITEM_useMRecovery      光 / 镜 / 守（道具的咒术决定种类，"单:3" 是次数）；不能对敌方用（_PREVENT_TEAMATTACK）
//	ITEM_useRessurect      复活倒下的人（0 为全满）
// Event.Consume 表示道具用掉了（世界层扣掉）；ConsumeAll 是整叠。原版大多数道具没有效果也照样用掉。

// itemArgNum 照原版 sscanf(p + 字节数, "%d")：找到 key 之后再跳过 skip 个字（原版跳的是字节，GBK 的一个中文是 2 字节），
// 再读一个整数（前面的空白可以跳过）；读不到时回传 def。
func itemArgNum(arg, key string, skip, def int) int {
	i := strings.Index(arg, key)
	if i < 0 {
		return def
	}
	s := arg[i+len(key):]
	for k := 0; k < skip && s != ""; k++ {
		_, n := utf8.DecodeRuneInString(s)
		s = s[n:]
	}
	s = strings.TrimLeft(s, " \t")
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

// itemEvents 把 fn 产生的事件补上施法者的 MP、连成一组，第一个标上用掉；没有事件时补一个"没有效果"。
func (b *Battle) itemEvents(f *Fighter, c Command, ev *[]Event, fn func()) {
	first := len(*ev)
	fn()
	if len(*ev) == first {
		*ev = append(*ev, Event{Actor: f.Slot, Kind: UseItem, Target: -1, Guardian: -1, Skill: c.ItemName, HP: f.HP, Status: f.Status()})
	}
	for i := first; i < len(*ev); i++ {
		(*ev)[i].ActorMP = f.MP
		(*ev)[i].Combo = i < len(*ev)-1
	}
	(*ev)[first].Consume = true
	(*ev)[first].ConsumeAll = c.ItemFunc == "ITEM_useImprecate"
}

// itemMagic 把道具的效果包成咒术，借用 magic.go 的执行（FromItem：不做 _PREVENT_TEAMATTACK 的检查）。
func itemMagic(c Command, fn, opt string) Command {
	m := &data.Magic{Name: c.ItemName, Func: fn, Option: opt, Idx: -1}
	return Command{Kind: Magic, Magic: m, Target: c.Target, FromItem: true}
}

func (b *Battle) useItem(f *Fighter, c Command, ev *[]Event) {
	none := func() {
		*ev = append(*ev, Event{Actor: f.Slot, Kind: UseItem, Target: -1, Guardian: -1, Skill: c.ItemName, HP: f.HP, Status: f.Status()})
	}
	arg := c.ItemArg
	switch c.ItemFunc {
	case "ITEM_useRecovery":
		kind, p := "", 0
		switch {
		case strings.Contains(arg, "体"):
			kind, p = "hp", itemArgNum(arg, "体", 0, 0)
		case strings.Contains(arg, "气"):
			kind, p = "mp", itemArgNum(arg, "气", 0, 0)
		default:
			none() // 忠 / 魅 / 全 在战斗中没有作用，也不会用掉
			return
		}
		b.itemEvents(f, c, ev, func() { b.itemRecovery(f, c, kind, p, ev) })
	case "ITEM_useStatusChange":
		st := statusOf(arg)
		if st < 0 {
			none()
			return
		}
		turn, per := itemArgNum(arg, "turn", 1, 0), itemArgNum(arg, "成", 1, 15)
		opt := statusChar[st] + " turn " + strconv.Itoa(turn) + " 成 " + strconv.Itoa(per)
		b.itemEvents(f, c, ev, func() { b.magicStatusChange(f, itemMagic(c, "MAGIC_StatusChange", opt), ev) })
	case "ITEM_useStatusRecovery":
		if statusOf(arg) < 0 {
			none()
			return
		}
		b.itemEvents(f, c, ev, func() { b.magicStatusRecovery(f, itemMagic(c, "MAGIC_StatusRecovery", arg), ev) })
	case "ITEM_useRessurect":
		b.itemEvents(f, c, ev, func() { b.magicRessurect(f, itemMagic(c, "MAGIC_Ressurect", strconv.Itoa(leadNum(arg))), ev) })
	case "ITEM_useCaptureUp":
		p := leadNum(arg)
		if p == 0 {
			p = 5
		}
		b.itemEvents(f, c, ev, func() {
			for _, i := range b.magicTargets(c.Target, false) {
				if t := b.F[i]; t.Player {
					t.CaptureUp += int(b.rand(float64(p)*0.9, float64(p)*1.1))
					*ev = append(*ev, b.itemEvent(f, t, c, MagicHit, sprStatus))
				}
			}
		})
	case "ITEM_useImprecate":
		b.itemEvents(f, c, ev, func() { b.imprecate(f, c, ev) })
	case "ITEM_useMRecovery":
		p := strings.Split(arg, ":")
		turn := 0
		if len(p) > 1 {
			turn = data.Atoi(p[1])
		}
		kind := ""
		if c.ItemMagic != nil {
			for _, k := range []string{"吸", "反", "无"} {
				if strings.Contains(c.ItemMagic.Option, k) {
					kind = k
					break
				}
			}
		}
		list := b.magicTargets(c.Target, false)
		if turn <= 0 || kind == "" {
			none()
			return
		}
		if f.Player && otherSide(f, list, true) {
			b.say(f, "光镜守..不得施予非玩家敌方。")
			none()
			return
		}
		b.itemEvents(f, c, ev, func() {
			for _, i := range list {
				t := b.F[i]
				setMagicDef(t, kind+" turn "+strconv.Itoa(turn))
				*ev = append(*ev, b.itemEvent(f, t, c, MagicHit, sprMagicDef))
			}
		})
	default:
		if !b.useItem2(f, c, ev) {
			none() // 其余道具在战斗中没有效果
		}
	}
}

func (b *Battle) itemEvent(f, t *Fighter, c Command, r Result, effect int) Event {
	return Event{Actor: f.Slot, Kind: UseItem, Target: t.Slot, Guardian: -1, Skill: c.ItemName, Result: r, Effect: effect,
		HP: t.HP, Status: t.Status(), ActorHP: f.HP, RideHP: t.rideHP()}
}

// itemRecovery 是 BATTLE_MultiRecovery：HP 或 MP 回复 RAND(0.9p, 1.1p)（-1 为补满），宠物第一次被回复忠诚度 +AI_FIX_PETRECOVERY。
func (b *Battle) itemRecovery(f *Fighter, c Command, kind string, p int, ev *[]Event) {
	effect := sprHeal3
	switch {
	case p <= 100:
		effect = sprHeal1
	case p <= 300:
		effect = sprHeal2
	}
	for _, i := range b.magicTargets(c.Target, false) {
		t := b.F[i]
		up := int(b.rand(float64(p)*0.9, float64(p)*1.1))
		e := b.itemEvent(f, t, c, MagicHeal, effect)
		if kind == "hp" {
			if p == -1 {
				up = t.MaxHP
			}
			if t.riding() {
				cm, pm := t.MaxHP-t.HP, t.Ride.MaxHP-t.Ride.HP
				ch := up * cm * 100 / max(1, cm+pm) / 100
				pet := min(t.Ride.MaxHP-t.Ride.HP, up-ch)
				t.Ride.HP += pet
				e.PetDamage, up = pet, ch
			}
			up = min(up, t.MaxHP-t.HP)
			t.HP += up
			e.Damage = up
		} else {
			if p == 1 {
				up = t.MaxMP
			}
			up = min(up, t.MaxMP-t.MP)
			t.MP += up
			e.MPDamage = up
		}
		if t.Pet && !t.recovered {
			t.recovered = true
			t.AIDelta += aiFixPetRecovery
		}
		e.HP, e.RideHP = t.HP, t.rideHP()
		*ev = append(*ev, e)
	}
}

// imprecate 是 BATTLE_ImprecateRecovery："咒:5x3" / "恩:10x8" / "祝:100x3"（第一个出现的那一种）。
// 咒与祝只对玩家；已经有同一种在作用中的人不会再上。效果在每次行动前（statusSeq）发生。
func (b *Battle) imprecate(f *Fighter, c Command, ev *[]Event) {
	kind := -1
	var spec string
	for _, seg := range strings.Split(c.ItemArg, "|") {
		for k, key := range []string{"咒", "恩", "祝"} {
			if kind < 0 && strings.Contains(seg, key) {
				kind = k
				if _, v, ok := strings.Cut(seg, ":"); ok {
					spec = v
				}
			}
		}
	}
	if kind < 0 {
		return
	}
	pw, rounds, _ := strings.Cut(spec, "x")
	power, n := data.Atoi(pw), data.Atoi(rounds)
	for _, i := range b.magicTargets(c.Target, false) {
		t := b.F[i]
		if (kind != 1 && !t.Player) || t.imprecate[kind][1] > 0 {
			continue
		}
		t.imprecate[kind] = [2]int{power, n}
		if t.Pet && !t.recovered {
			t.recovered = true
			t.AIDelta += aiFixPetRecovery
		}
		*ev = append(*ev, b.itemEvent(f, t, c, MagicHit, sprStatus))
	}
}

// tickImprecate 是 BATTLE_StatusSeq 里咒 / 恩 / 祝的部分：每次行动前扣 MP、回 HP、回 MP，次数减一。
func (b *Battle) tickImprecate(f *Fighter, ev *[]Event) {
	hp, mp, any := 0, 0, false
	for k := range f.imprecate {
		if f.imprecate[k][1] <= 0 {
			continue
		}
		any = true
		p := f.imprecate[k][0]
		switch k {
		case 0:
			mp -= p
		case 1:
			hp += p
		case 2:
			mp += p
		}
		f.imprecate[k][1]--
	}
	if !any {
		return
	}
	oldHP, oldMP := f.HP, f.MP
	f.HP = max(1, min(f.MaxHP, f.HP+hp))
	f.MP = max(0, min(f.MaxMP, f.MP+mp))
	if f.riding() && hp != 0 {
		f.Ride.HP = max(1, min(f.Ride.MaxHP, f.Ride.HP+hp))
	}
	*ev = append(*ev, Event{Actor: f.Slot, Kind: Regen, Target: -1, Guardian: -1, Damage: f.HP - oldHP, MPDamage: f.MP - oldMP,
		HP: f.HP, Status: f.Status(), ActorMP: f.MP, RideHP: f.rideHP()})
}

// Regen 是咒 / 恩 / 祝每回合的增减（BD）：Damage 是 HP 的变化、MPDamage 是 MP 的变化（可以是负的）。
const Regen Kind = "regen"
