package battle

import (
	"math"
	"strconv"
	"strings"

	"sa/server/internal/data"
)

// 第二批：战斗中的道具（battle_item.c）与变身类的技能、火线猎杀、召唤龙、战斗中的变身（battle.c / battle_magic.c）。
//
//	ITEM_useFieldChange  战场属性："地 50 回合 6"（原版找 "turn"，数据写的是"回合"，所以一律 3 回合）
//	ITEM_firecracker     鞭炮："KPET:901_902"，对方是这些宠物模板的敌人 / 宠物时吓跑（"%s被吓跑了！"），对自己人没有作用、不用掉
//	ITEM_Refresh         解除指定的状态（只解最高编号的那一个），整叠用掉
//	ITEM_ResAndDef       复活 + 光镜守："100 吸 turn 1"；不能对敌方用（_PREVENT_TEAMATTACK）
//	ITEM_useMagic        用道具施法（参数是 magic.txt 编号，MP 照头上装备的 magicusemp，世界层算好）
//	ITEM_metamo / MAGIC_Metamo  战斗中变身成自己的宠物（Vary 事件，世界层记下时间）
//	PETSKILL_BecomeFox   媚惑术：打中可捕获的敌人时 31% 变成小狐狸 3 回合：只能攻击 / 防御 / 待机，攻防敏 ×0.8
//	PETSKILL_BecomePig   乌力化：打中对方玩家时 30% 变成乌力 60 秒（可累加，战斗后照样是乌力，时间由世界层算）：
//	                     不能用咒术与技能（改为防御）、空手
//	PETSKILL_Firekill    火线猎杀：攻 ×0.8 打一下（不看光镜守、不会反击），再对那一排放火魔法（魔法等级：玩家照火的熟练度、敌人等级 ×0.9、宠物 5）
//	MAGIC_ToCallDragon   召唤龙："图|威力"：以威力当攻击力对每个对象算一次普通伤害（BATTLE_DamageCalc）

const (
	foxSprite    = 101749 // 小狐狸
	pigSprite    = 100250 // 乌力（原版的参数写法是坏的，永远用预设值）
	sprTransform = 101750 // 变身时对象身上的效果
	sprFireRow   = 101735 // 火线猎杀的火焰
)

// useItem2 是第二批的道具；handled 为 false 表示不是这里的。
func (b *Battle) useItem2(f *Fighter, c Command, ev *[]Event) bool {
	none := func() {
		*ev = append(*ev, Event{Actor: f.Slot, Kind: UseItem, Target: -1, Guardian: -1, Skill: c.ItemName, HP: f.HP, Status: f.Status()})
	}
	arg := c.ItemArg
	switch c.ItemFunc {
	case "ITEM_useFieldChange":
		attr, at, tok := 0, len(arg)+1, ""
		for i, ch := range []string{"无", "地", "水", "火", "风"} {
			if k := strings.Index(arg, ch); k >= 0 && k < at {
				attr, at, tok = i, k, ch
			}
		}
		if tok == "" {
			b.itemEvents(f, c, ev, func() {}) // 没有属性也用掉
			return true
		}
		pow := itemArgNum(arg[at:], tok, 0, 30)
		if pow < 0 || pow > 100 {
			pow = 30
		}
		b.fieldAttr, b.fieldPow, b.fieldTurns = attr, pow, itemArgNum(arg, "turn", 1, 3)
		b.itemEvents(f, c, ev, func() {
			*ev = append(*ev, Event{Actor: f.Slot, Kind: FieldAttr, Target: -1, Guardian: -1, Skill: c.ItemName, Status: attr})
		})
	case "ITEM_firecracker":
		b.firecracker(f, c, ev)
	case "ITEM_Refresh":
		st := -1
		for i, at := 1, len(arg)+1; i < stCount; i++ {
			if k := strings.Index(arg, statusChar[i]); k >= 0 && k < at {
				st, at = i, k
			}
		}
		b.itemEvents(f, c, ev, func() {
			if st > 0 {
				b.magicStatusRecovery(f, itemMagic(c, "MAGIC_StatusRecovery", statusChar[st]), ev)
			}
		})
		for i := len(*ev) - 1; i >= 0; i-- {
			if (*ev)[i].Consume {
				(*ev)[i].ConsumeAll = true
			}
		}
	case "ITEM_ResAndDef":
		p := strings.Fields(arg)
		list := b.magicTargets(c.Target, true)
		if f.Player && otherSide(f, list, true) {
			b.say(f, "光镜守..不得施予非玩家敌方。")
			none()
			return true
		}
		kind := ""
		for _, k := range []string{"吸", "反", "无"} {
			if strings.Contains(arg, k) {
				kind = k
				break
			}
		}
		if kind == "" {
			none()
			return true
		}
		pow := 0
		if len(p) > 0 {
			pow = data.Atoi(strings.TrimSuffix(p[0], "%"))
		}
		turn := itemArgNum(arg, "turn", 1, 3)
		b.itemEvents(f, c, ev, func() {
			b.magicRessurect(f, itemMagic(c, "MAGIC_Ressurect", strconv.Itoa(pow)), ev)
			for _, i := range b.magicTargets(c.Target, false) {
				t := b.F[i]
				setMagicDef(t, kind+" turn "+strconv.Itoa(turn))
				*ev = append(*ev, b.itemEvent(f, t, c, MagicHit, sprMagicDef))
			}
		})
	case "ITEM_useMagic":
		if c.ItemMagic == nil {
			none()
			return true
		}
		first := len(*ev)
		b.castMagic(f, Command{Kind: Magic, Magic: c.ItemMagic, MP: c.MP, Target: c.Target}, ev)
		(*ev)[first].Consume = true // 次数（damagebreak）由世界层扣
	case "ITEM_metamo":
		b.battleMetamo(f, c, ev, leadNum(arg), true)
	default:
		return false
	}
	return true
}

// firecracker 是 ITEM_useFirecracker_Battle（_FIX_FIRECRACKER）：乌力化中不能用；对自己人没有作用（不用掉）；
// 对方的宠物模板（CHAR_PETID）在 KPET 名单里就吓跑（离开战场），否则"什麽事都没发生。"。
func (b *Battle) firecracker(f *Fighter, c Command, ev *[]Event) {
	none := Event{Actor: f.Slot, Kind: UseItem, Target: -1, Guardian: -1, Skill: c.ItemName, HP: f.HP, Status: f.Status()}
	kpet, ok := data.ArgField(c.ItemArg, "KPET")
	if f.Pig > 0 || !ok || c.Target < 0 || c.Target >= Slots || b.F[c.Target] == nil {
		if f.Pig > 0 {
			b.say(f, "什麽也没发生。")
		}
		*ev = append(*ev, none)
		return
	}
	t := b.F[c.Target]
	if t != f && t.Side() == f.Side() {
		*ev = append(*ev, none)
		return
	}
	e := b.itemEvent(f, t, c, MagicFail, 0)
	e.Consume = true
	*ev = append(*ev, e)
	if t == f {
		return
	}
	temp := t.TempNo
	if t.Seed != nil {
		temp = t.Seed.TempNo
	}
	for _, s := range strings.Split(strings.TrimLeft(kpet, ":"), "_") {
		if id := data.Atoi(s); id > 0 && id == temp && !t.Player {
			b.say(f, t.Name+"被吓跑了！")
			(*ev)[len(*ev)-1].Result = MagicHit
			b.flee(t, ev)
			return
		}
	}
	b.say(f, "什麽事都没发生。")
}

// battleMetamo 是战斗中的 ITEM_metamo / MAGIC_Metamo：只能变成自己出战的宠物（对自己用是变回来）；
// 骑着、乌力化中、拿远距离武器时不能变。形象立刻换（原版下一回合的 BC 才换），时间由世界层记（Event.Damage = 秒数）。
func (b *Battle) battleMetamo(f *Fighter, c Command, ev *[]Event, secs int, item bool) {
	name := c.ItemName
	if c.Magic != nil {
		name = c.Magic.Name
	}
	fail := func(msg string) {
		b.say(f, msg)
		*ev = append(*ev, Event{Actor: f.Slot, Kind: UseItem, Target: -1, Guardian: -1, Skill: name, HP: f.HP, Status: f.Status()})
	}
	switch {
	case f.riding():
		fail("无法变身，骑乘中不能变身！")
		return
	case item && f.Pig > 0:
		fail("无法变身，乌力化中不能变身！")
		return
	case f.throwing():
		fail("使用远距离武器无法变身！")
		return
	}
	var t *Fighter
	if c.Target >= 0 && c.Target < Slots {
		t = b.F[c.Target]
	}
	if t == nil || (t != f && t.Slot != f.Slot+5) {
		fail("无法变身，只能变成自己的宠物！")
		return
	}
	if t == f {
		f.Sprite = f.BaseSprite
		b.say(f, "变回自己！")
	} else {
		f.Sprite = t.Sprite
		b.say(f, "变身成"+t.Name+"！")
	}
	*ev = append(*ev, Event{Actor: f.Slot, Kind: Vary, Target: t.Slot, Guardian: -1, Skill: name, Sprite: f.Sprite, Damage: secs,
		HP: f.HP, Status: f.Status(), Consume: true})
}

// transformAfter 是媚惑术 / 乌力化：普通攻击打完后看最后一下（没被闪开、没打空、对方还站着）决定要不要变身。
func (b *Battle) transformAfter(f *Fighter, s *data.Skill, first int, ev *[]Event) {
	var last *Event
	for i := len(*ev) - 1; i >= first; i-- {
		if (*ev)[i].Actor == f.Slot && (*ev)[i].Kind == Attack {
			last = &(*ev)[i]
			break
		}
	}
	if last == nil || last.Result == Miss || last.Result == Dodge || last.Result == AllGuard {
		return
	}
	t := b.F[last.Target]
	if last.Guardian >= 0 {
		t = b.F[last.Guardian]
	}
	if !t.Alive() {
		return
	}
	if s.Func == "PETSKILL_BecomeFox" {
		// 原版看 CHAR_WORK_PETFLG（敌人"可以捕获"的旗标），玩家的宠物与玩家都不会中
		if t.Player || t.Seed == nil || !t.Seed.Capture || f.Pig > 0 || b.Rnd.IntN(100) >= 31 {
			return
		}
		if t.fox == 0 {
			t.foxSave = t.Sprite
		}
		t.fox, t.Sprite = b.Turn, foxSprite
	} else {
		if !t.Player || t.Side() == f.Side() || b.Rnd.IntN(100) >= 30 {
			return
		}
		if t.riding() {
			t.Ride.Fell = true
		}
		if t.Pig <= 0 {
			t.Pig = 60
		} else {
			t.Pig += 60
		}
		t.Sprite = pigSprite
		b.say(t, "乌力时间:"+strconv.Itoa(t.Pig)+"秒")
	}
	*ev = append(*ev, Event{Actor: t.Slot, Kind: Vary, Target: -1, Guardian: -1, Skill: s.Name, Sprite: t.Sprite, Effect: sprTransform,
		HP: t.HP, Status: t.Status()})
}

// transformLimit 是变身中下指令的限制（battle.c 8661 / 8676）：小狐狸只能攻击 / 防御 / 待机（其余改为待机），攻防敏 ×0.8；
// 乌力只能攻击 / 防御 / 待机 / 道具 / 逃跑 / 捕捉 / 换宠物（其余改为防御），空手。
func (b *Battle) transformLimit(f *Fighter, c Command) Command {
	basic := c.Skill == nil || c.Skill.Func == "PETSKILL_NormalAttack" || c.Skill.Func == "PETSKILL_NormalGuard" || c.Skill.Func == "PETSKILL_None"
	if f.fox > 0 {
		f.Weapon = data.ItemFist
		if !((c.Kind == Attack || c.Kind == Guard || c.Kind == None) && basic) {
			b.say(f, "变成小狐狸後只能攻击、防御和待机！")
			c = Command{Kind: None}
		}
		scale(f, 0.8, 0.8, 0.8)
	}
	if f.Player && f.Pig > 0 {
		f.Weapon = data.ItemFist
		switch c.Kind {
		case Attack, Guard, None, UseItem, Escape, Capture, PetSwap:
			if c.Kind == Attack && !basic {
				c = Command{Kind: Guard}
				b.say(f, "变成乌力後不能使用咒术和职业技能！")
			}
		default:
			c = Command{Kind: Guard}
			b.say(f, "变成乌力後不能使用咒术和职业技能！")
		}
	}
	return c
}

// transformTick 是每次行动后（battle.c 10536 / 10562）：小狐狸过了 2 回合变回来；乌力报剩下的时间。
func (b *Battle) transformTick(f *Fighter, ev *[]Event) {
	if f.fox > 0 && b.Turn-f.fox > 2 {
		f.fox, f.Sprite = 0, f.foxSave
		f.Atk, f.Def, f.Quick = f.FixAtk, f.FixDef, f.FixQuick
		*ev = append(*ev, Event{Actor: f.Slot, Kind: Vary, Target: -1, Guardian: -1, Sprite: f.Sprite, HP: f.HP, Status: f.Status()})
	}
	if f.Player && f.Pig > 0 {
		b.say(f, "乌力时间:"+strconv.Itoa(f.Pig)+"秒")
	}
}

// firekill 是火线猎杀（battle.c 9220 / BATTLE_MultiAttMagic_Fire）：对自己人不能用；攻 ×0.8 打一下（不看光镜守、不会反击），
// 再对目标那一排每个人放火魔法（魔法等级 4、威力 200）。
func (b *Battle) firekill(f *Fighter, c Command, ev *[]Event) {
	t := b.pickTarget(f, c.Target)
	if t == nil || t.Side() == f.Side() {
		if t != nil {
			b.say(f, "不能对同队使用此技能!")
		}
		*ev = append(*ev, Event{Actor: f.Slot, Kind: None, Target: -1, HP: f.HP, Status: f.Status(), Guardian: -1})
		return
	}
	f.Atk = f.FixAtk * 8 / 10
	first := len(*ev)
	e, _ := b.attackOnce(f, t, attackOpts{name: c.Skill.Name, noCounter: true, noBarrier: true}, false)
	*ev = append(*ev, e)
	a := 5 // 宠物
	switch {
	case f.Player && f.Mag != nil:
		a = f.Mag.Lv[2]
	case f.Enemy():
		a = int(float64(f.Level) * 0.9)
	}
	trueMagic := b.Rnd.IntN(100) <= a
	row := t.Slot / 5 * 5
	for i := row; i < row+5; i++ {
		x := b.F[i]
		if !x.Alive() || x.hidden {
			continue
		}
		me := Event{Actor: f.Slot, Kind: Magic, Target: x.Slot, Guardian: -1, Skill: c.Skill.Name, Result: MagicDamage, Effect: sprFireRow, ActorHP: f.HP}
		if b.magicDodge(x, 2) {
			me.Result, me.HP, me.Status = Dodge, x.HP, x.Status()
			*ev = append(*ev, me)
			continue
		}
		R := 0
		if x.Mag != nil {
			R = x.Mag.Res[2]
		} else if x.Enemy() {
			R = int(float64(x.Level) * 0.5)
		}
		K := math.Max(0, float64(a)*1.4-float64(R))
		M := math.Max(1, float64(a))
		A := K*K/(M*M) + float64(b.Rnd.IntN(20))/100
		dmg := b.magicAdjust(f, x, 4, 2, int(200*1.4*A))
		if x.riding() {
			ch := dmg * b.charaRatio(x, 2) / 10
			pet := dmg - ch
			x.Ride.HP -= pet
			if x.Ride.HP <= 0 {
				x.Ride.HP, x.Ride.Fell, me.Fall = 0, true, true
			}
			me.PetDamage, dmg = pet, ch
		}
		x.HP -= dmg
		x.status[StSleep] = 0
		if x.HP <= 0 {
			x.HP, me.Dead = 0, true
			x.KilledBy, x.KilledRide = f.Slot, f.riding()
		}
		me.Damage, me.HP, me.Status, me.RideHP = dmg, x.HP, x.Status(), x.rideHP()
		*ev = append(*ev, me)
	}
	if !trueMagic && f.Player && f.Mag != nil {
		b.magicAtkExp(f, 2, 4, 1)
	}
	b.combo(*ev, first)
}

// toCallDragon 是 MAGIC_ToCallDragon（BATTLE_MultiToCallDragonMagic）："图|威力"：每个对象先判定魔法闪避，
// 再以威力当攻击力算一次普通伤害（BATTLE_DamageCalc），骑着时人与骑宠各一半。
func (b *Battle) toCallDragon(f *Fighter, c Command, ev *[]Event) {
	p := strings.Split(c.Magic.Option, "|")
	img, power := data.Atoi(p[0]), 0
	if len(p) > 1 {
		power = data.Atoi(p[1])
	}
	tmp := &Fighter{Slot: f.Slot, Name: f.Name, Level: f.Level, Atk: power, FixAtk: power, Luck: 1, Weapon: -1}
	for _, i := range b.magicTargets(c.Target, false) {
		t := b.F[i]
		e := b.magicEvent(f, t, c, MagicDamage, img)
		if b.magicDodge(t, 0) {
			e.Result = Dodge
			*ev = append(*ev, e)
			continue
		}
		dmg := max(1, b.damage(tmp, t))
		if t.riding() {
			pet := dmg / 2
			t.Ride.HP -= pet
			if t.Ride.HP <= 0 {
				t.Ride.HP, t.Ride.Fell, e.Fall = 0, true, true
			}
			e.PetDamage, dmg = pet, dmg-pet
		}
		t.HP -= dmg
		t.status[StSleep] = 0
		if t.HP <= 0 {
			t.HP, e.Dead = 0, true
			t.KilledBy, t.KilledRide = f.Slot, f.riding()
		}
		e.Damage, e.HP, e.Status, e.RideHP = dmg, t.HP, t.Status(), t.rideHP()
		*ev = append(*ev, e)
	}
}
