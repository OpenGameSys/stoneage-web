package world

import (
	"fmt"

	"sa/server/internal/battle"
	"sa/server/internal/data"
)

// 装备的耐久（_TAKE_ITEMDAMAGE，battle_event.c BATTLE_ItemCrushSeq / BATTLE_ItemCrush）：每一下有伤害的攻击，
// 挨打的人随机一件装备（不含武器）、打人的人的武器各磨损一些：武器 n = (伤害 − 50) / 40（至少 1），防具 n = (200 − 伤害) / 20（负的当 1，
// 原版写反了：越轻的伤害磨得越多），扣 RAND(n, n × 1.4)。耐久到 0 就"因过度损坏而消失"；耐久降到 70% / 50% / 30% 以下时损坏等级
// 变成受损 / 毁坏 / 不堪使用（只是显示，原版不影响能力）。按数据的最大耐久（20 万以上），一般打一辈子也磨不坏。
// E啮齿术（BATTLE_S_ToothCrushe）直接把一件装备咬到 70% / 30% / 10%，再低就咬坏。
// 宠物身上的道具（_TAKE_ITEMDAMAGE_FOR_PET）照同样的规则磨损：挨打时随机一件（爪除外），攻击时磨爪；宠物没有连线，所以不说话。

var crushWords = []string{"完好", "受损", "毁坏", "不堪使用", "碎片"}

// petCrushOrder 是原服务端宠物身上的格位顺序（CHAR_PET_HEAD 头、牙、爪、胸、背、翅、脚）对到这里的格位（data.PetEquipSlot）；
// 原版第 2 格（爪）就是 CHAR_ARM 那一格，所以当作武器。
var petCrushOrder = [data.PetItemSlots]int{0, 2, data.PetSlotClaw, 3, 4, 1, 6}

// crushSlot 是 BATTLE_ItemCrushCheck：从随机的一格起找有装备、不是武器的那一格；第一次没有就只在 0..4 之间往下找。
// items 是角色的装备位或宠物身上的道具（pet）。
func (w *World) crushSlot(items []*Item, pet bool) int {
	if len(items) == 0 {
		return -1
	}
	n, at := data.EquipSlots, func(r int) int { return r }
	if pet {
		n, at = data.PetItemSlots, func(r int) int { return petCrushOrder[r] }
	}
	r := w.rnd.IntN(n)
	for k := 0; k < n; k++ {
		if items[at(r)] != nil && r != data.SlotArm {
			return at(r)
		}
		r = (r + 1) % 5
	}
	return -1
}

// battleCrush 是战斗中的 OnHit（BATTLE_ItemCrushSeq）：e 的人或宠物挨打时磨一件防具，打人时磨武器（宠物是爪）。
func (w *World) battleCrush(e *entity, ft *fight, a, d *battle.Fighter, dmg int) {
	if d.Slot == ft.slot {
		if s := w.crushSlot(e.inv[:data.EquipSlots], false); s >= 0 {
			w.wearItem(e, ft, e.inv[:], s, dmg, false, false)
		}
	}
	if a.Slot == ft.slot && e.inv[data.SlotArm] != nil {
		w.wearItem(e, ft, e.inv[:], data.SlotArm, dmg, true, false)
	}
	if ft.pet < 0 || e.pets.Pets[ft.pet] == nil {
		return
	}
	p := e.pets.Pets[ft.pet]
	if d.Slot == ft.slot+5 {
		if s := w.crushSlot(p.Items, true); s >= 0 {
			w.wearItem(e, ft, p.Items, s, dmg, false, true)
		}
	}
	if a.Slot == ft.slot+5 && p.item(data.PetSlotClaw) != nil {
		w.wearItem(e, ft, p.Items, data.PetSlotClaw, dmg, true, true)
	}
}

// wearItem 是 BATTLE_ItemCrush。
func (w *World) wearItem(e *entity, ft *fight, items []*Item, slot, dmg int, weapon, pet bool) {
	it := items[slot]
	def := w.game.Items[it.ID]
	if def == nil || def.Durability < 1 {
		return
	}
	n := (200 - dmg) / 20
	if weapon {
		if n = (dmg - 50) / 40; n <= 0 {
			n = 1
		}
	} else if n < 0 {
		n = 1
	}
	lo, hi := float64(n), float64(n)*1.4
	cur := itemDur(def, it) - int(lo+float64(int((hi-lo+1)*w.rnd.Float64())))
	w.setDur(e, ft, items, slot, def, cur, pet)
}

// setDur 改耐久：到 0 消失，否则损坏等级变了（70% 以下）就说一声（宠物的不说）。
func (w *World) setDur(e *entity, ft *fight, items []*Item, slot int, def *data.ItemDef, cur int, pet bool) {
	it := items[slot]
	dirty := &ft.itemsDirty
	if pet {
		dirty = &ft.petsDirty
	}
	if cur <= 0 {
		items[slot] = nil
		if !pet {
			w.sysMsg(e, def.Name+"因过度损坏而消失。", colorYellow)
		}
		*dirty = true
		return
	}
	it.Dur = cur
	pct := cur * 100 / def.Durability
	level := 0
	switch {
	case pct < 30:
		level = 3
	case pct < 50:
		level = 2
	case pct < 70:
		level = 1
	}
	if level != it.Crush && pct < 70 {
		it.Crush = level
		if !pet {
			w.sysMsg(e, fmt.Sprintf("%s是%s的。", def.Name, crushWords[level]), colorRed)
		}
		*dirty = true
	}
}

// battleTooth 是战斗中的 OnTooth（E啮齿术咬到 e 的人；原版的条件写成宠物也跳过，所以只咬人）。
func (w *World) battleTooth(e *entity, ft *fight) {
	s := w.crushSlot(e.inv[:data.EquipSlots], false)
	if s < 0 {
		return
	}
	it := e.inv[s]
	def := w.game.Items[it.ID]
	if def == nil || def.Durability < 1 {
		return
	}
	d := float64(itemDur(def, it)) / float64(def.Durability)
	cur, level := 0, 0
	switch {
	case d >= 1:
		cur, level = int(float64(def.Durability)*0.70), 1
	case d >= 0.5:
		cur, level = int(float64(def.Durability)*0.30), 2
	case d >= 0.3:
		cur, level = int(float64(def.Durability)*0.10), 3
	}
	if cur <= 0 {
		w.setDur(e, ft, e.inv[:], s, def, 0, false)
		return
	}
	it.Dur, it.Crush = cur, level
	w.sysMsg(e, fmt.Sprintf("%s是%s的。", def.Name, crushWords[level]), colorYellow)
	ft.itemsDirty = true
}
