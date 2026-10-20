package world

import (
	"fmt"

	"sa/server/internal/battle"
	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 宠物身上的道具（_PET_ITEM）：
//
//	lssproto_PETITEM_recv → CHAR_movePetItem(宠物, from, to)：小于 7 的是宠物身上的格（data.PetEquipSlot），
//	                        9 以上是道具栏；to = -1 是装到该放的那一格，to = -2 是丢掉（宠物身上的不能丢）
//	装上去的条件            忠诚变动值（CHAR_VARIABLEAI）≥ 100、种类对得上那一格、宠物等级够；
//	                        骑着的宠物只有原客户端挡（"骑宠不可装装备！"），服务端不管
//	能力                    ITEM_equipEffect 同样加到宠物身上（petDerive）
//	磨损                    _TAKE_ITEMDAMAGE_FOR_PET：战斗中挨打 / 攻击时照角色的规则磨（crush.go），宠物没有连线所以不说话
//
// 原客户端每种宠物能装的部位底图不同（byShowPetItemBackground[HIWORD(fusion)]），但这台服务端送的 fusion 只有融合旗标，
// 种族永远是 0（乌力），所以客户端一律用乌力那一组；服务端本来就不检查种族。

// item 是宠物身上第 i 格的道具。
func (p *Pet) item(i int) *Item {
	if i < 0 || i >= len(p.Items) {
		return nil
	}
	return p.Items[i]
}

func (p *Pet) setItem(i int, it *Item) {
	if p.Items == nil {
		if it == nil {
			return
		}
		p.Items = make([]*Item, data.PetItemSlots)
	}
	p.Items[i] = it
	for _, x := range p.Items {
		if x != nil {
			return
		}
	}
	p.Items = nil // 都拿下来了就不存
}

// petDerive 是宠物的工作值（CHAR_complianceParameter）：基本状态换算后加上身上道具的能力；attr 是道具加的属性。
func (w *World) petDerive(p *Pet) (battle.Stats, [4]int) {
	d := p.derive()
	if p.Items == nil {
		return d, [4]int{}
	}
	mods, attr := w.equipMods(p.Items, [5]int{d.Atk, d.Def, d.Quick, d.HP, 100})
	d.Atk = max(0, d.Atk+mods[data.ModAtk])
	d.Def = max(0, d.Def+mods[data.ModDef])
	d.Quick = max(0, d.Quick+mods[data.ModQuick])
	d.HP = max(1, d.HP+mods[data.ModHP])
	return d, attr
}

// petMaxHP 是宠物的最大 HP（含道具）。
func (w *World) petMaxHP(p *Pet) int {
	d, _ := w.petDerive(p)
	return d.HP
}

// PetItemEquip 是 lssproto_PETITEM_recv（CHAR_movePetItem）。战斗中不能动。
func (w *World) PetItemEquip(charID, petSlot, from, to int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || e.busy() || petSlot < 0 || petSlot >= PetSlots || e.pets.Pets[petSlot] == nil {
			return
		}
		p := e.pets.Pets[petSlot]
		bag := func(i int) bool { return i >= data.EquipSlots && i < data.ItemSlots }
		pet := func(i int) bool { return i >= 0 && i < data.PetItemSlots }
		switch {
		case to == -1 && bag(from):
			if it := e.inv[from]; it != nil {
				if def := w.game.Items[it.ID]; def != nil {
					to = data.PetEquipSlot(def.Type) // 不是宠物装备时是 -1，下面就不动
				}
			}
		case to == -2:
			if from < data.PetItemSlots {
				w.sysMsg(e, "无法丢弃宠物身上的物品。", colorYellow)
			} else {
				w.dropItem(e, from)
			}
			return
		}
		var bagSlot, petItem int
		switch {
		case bag(from) && pet(to):
			bagSlot, petItem = from, to
		case pet(from) && bag(to):
			bagSlot, petItem = to, from
		default:
			return
		}
		mine, worn := e.inv[bagSlot], p.item(petItem)
		name := p.displayName()
		if bag(from) && mine == nil {
			return
		}
		if pet(from) && worn == nil {
			return
		}
		if pet(from) {
			w.sysMsg(e, fmt.Sprintf("宠物%s 卸下%s", name, w.itemName(worn)), colorWhite) // 原版先说卸下，再检查换上去的那一件
		}
		if mine != nil {
			if msg, color := w.petCanEquip(p, mine, petItem); msg != "" {
				w.sysMsg(e, msg, color)
				return
			}
			if bag(from) && worn != nil {
				w.sysMsg(e, fmt.Sprintf("宠物%s 卸下%s", name, w.itemName(worn)), colorWhite)
			}
			w.sysMsg(e, fmt.Sprintf("宠物%s 装备%s", name, w.itemName(mine)), colorWhite)
		}
		e.inv[bagSlot] = worn
		p.setItem(petItem, mine)
		p.HP = min(p.HP, w.petMaxHP(p))
		w.itemsChanged(e)
		w.petsChanged(e)
	})
}

// petCanEquip 是 CHAR_movePetItem 装上去之前的检查，可以装时回传空字串。
func (w *World) petCanEquip(p *Pet, it *Item, slot int) (string, int) {
	def := w.game.Items[it.ID]
	switch {
	case p.VariableAI < 100:
		return "无法训服宠物装备该物品。", colorRed
	case def == nil || data.PetEquipSlot(def.Type) != slot:
		return "无法装备该项物品。", colorWhite
	case p.Level < def.Level:
		return "宠物等级尚未到达装备该项物品！", colorYellow
	}
	return "", 0
}

// petItems 是宠物窗口与道具窗口的宠物页要显示的身上道具（CHAR_sendPetItemData）。
func (w *World) petItems(p *Pet) []proto.InvItem {
	out := []proto.InvItem{}
	for i, it := range p.Items {
		if it == nil {
			continue
		}
		def := w.game.Items[it.ID]
		if def == nil {
			continue
		}
		x := invItem(i, it, def)
		x.Equip, x.Usable = -1, false
		out = append(out, x)
	}
	return out
}
