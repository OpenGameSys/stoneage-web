package world

import "sa/server/internal/data"

// 装备的免遇敌 / 降低遇敌（原版 item_event.c，记在连线上的 eqnoenemy / eqrandenemy，不存档）：
//
//	ITEM_equipNoenemy / ITEM_remNoenemy  太阳神的首饰（attachfunc / detachfunc，参数 noen:40 / 80 / 120）：
//	    装着时在特定楼层不遇敌：≥40 楼层 100、200，≥80 再加 300、400，≥120 再加 500，≥200 任何楼层（char_walk.c CHAR_walk）
//	ITEM_randEnemyEquip / ITEM_RerandEnemyEquip  月亮首饰（参数 rand:N）：装着时遇敌判定成立后，RAND(0,100) > N 才真的遇敌
//	ITEM_randEnemyEquipOne  星星之守护（usefunc，用掉）：同上，直到下线、或拿下月亮首饰（clearEqRandenemy 共用一个值）
//
// 驱魔（ITEM_useNoenemy）在 itemuse2.go。8252 / 8253 / 8254 / 8256 号地图三种都不管用。

// noenFloors 是太阳神的首饰各级不遇敌的楼层（noen ≥ 200 是任何楼层）。
var noenFloors = []struct {
	level  int
	floors []int
}{{120, []int{100, 200, 300, 400, 500}}, {80, []int{100, 200, 300, 400}}, {40, []int{100, 200}}}

// noenHere 是装着 noen 等级 level 的首饰时，楼层 fl 不遇敌（ITEM_equipNoenemy 的讯息与 CHAR_walk 共用同一张表）。
func noenHere(level, fl int) bool {
	if level >= 200 {
		return true
	}
	for _, t := range noenFloors {
		if level >= t.level {
			for _, f := range t.floors {
				if f == fl {
					return true
				}
			}
			return false
		}
	}
	return false
}

// noEnemyFloor 是驱魔与免遇敌装备都不管用的楼层。
func noEnemyFloor(fl int) bool { return fl >= 8252 && fl <= 8256 && fl != 8255 }

// eqNoenemy 是 getEqNoenemy：装着的太阳神的首饰的 noen（饰品位不能装同种类的两件，所以最多一件；没有为 0）。
func (w *World) eqNoenemy(e *entity) int {
	lv := 0
	for i := 0; i < data.EquipSlots; i++ {
		if it := e.inv[i]; it != nil {
			if def := w.game.Items[it.ID]; def != nil && def.AttachFunc == "ITEM_equipNoenemy" {
				lv = max(lv, itemArg(def, "noen"))
			}
		}
	}
	return lv
}

// encounterBlocked 是 CHAR_walk 的 noen：驱魔中、或装着太阳神的首饰而这一层在它的楼层表里，就不遇敌（遇敌值也不加）。
func (w *World) encounterBlocked(e *entity) bool {
	if noEnemyFloor(e.floor) {
		return false
	}
	return e.noenemy > 0 || noenHere(w.eqNoenemy(e), e.floor)
}

// randEnemyPass 是 _Item_MoonAct：遇敌判定成立后，有 eqrandenemy 时 RAND(0,100) 要大于它才真的遇敌。
func (w *World) randEnemyPass(e *entity) bool {
	return e.eqRand <= 0 || w.rnd.IntN(101) > e.eqRand
}

// itemAttach / itemDetach 是道具的 attachfunc / detachfunc（CHAR_EquipItem / CHAR_UnequipItem，上线时 CHAR_login 也对装着的执行 attach）。
func (w *World) itemAttach(e *entity, def *data.ItemDef) {
	switch def.AttachFunc {
	case "ITEM_equipNoenemy":
		if noenHere(itemArg(def, "noen"), e.floor) {
			w.sysMsg(e, "一道奇异的光芒隐藏了你的行踪。", colorWhite)
		} else {
			w.sysMsg(e, "什麽事也没有发生。", colorWhite)
		}
	case "ITEM_randEnemyEquip":
		if n := itemArg(def, "rand"); n > 0 {
			e.eqRand = n
			w.sysMsg(e, "遇敌率降低了。", colorWhite)
		}
	}
}

func (w *World) itemDetach(e *entity, def *data.ItemDef) {
	switch def.DetachFunc {
	case "ITEM_remNoenemy":
		if noenHere(itemArg(def, "noen"), e.floor) {
			w.sysMsg(e, "环绕着你的光芒消失了。", colorWhite)
		} else {
			w.sysMsg(e, "什麽事也没有发生。", colorWhite)
		}
	case "ITEM_RerandEnemyEquip":
		if e.eqRand > 0 {
			e.eqRand = 0
			w.sysMsg(e, "遇敌率回复。", colorWhite)
		}
	}
}

// equipChanged 在道具在装备位与道具栏之间移动后，对拿下的执行 detach、装上的执行 attach（装备位之间互换不算）。
func (w *World) equipChanged(e *entity, off, on *Item) {
	if off != nil {
		if def := w.game.Items[off.ID]; def != nil {
			w.itemDetach(e, def)
		}
	}
	if on != nil {
		if def := w.game.Items[on.ID]; def != nil {
			w.itemAttach(e, def)
		}
	}
}

// loginAttach 是 CHAR_login 对装着的道具执行 attachfunc。
func (w *World) loginAttach(e *entity) {
	for i := 0; i < data.EquipSlots; i++ {
		if it := e.inv[i]; it != nil {
			if def := w.game.Items[it.ID]; def != nil && def.AttachFunc != "" {
				w.itemAttach(e, def)
			}
		}
	}
}

// useRandEnemyOne 是 ITEM_randEnemyEquipOne（星星之守护）：参数 rand:N，N > 0 才有效，用掉。
func (w *World) useRandEnemyOne(e *entity, slot int, def *data.ItemDef) {
	n := itemArg(def, "rand")
	if n <= 0 {
		return
	}
	e.eqRand = n
	w.sysMsg(e, "遇敌率降低了。", colorWhite)
	w.sysMsg(e, "道具 "+def.Name+"消失了。", colorYellow)
	w.useUp(e, slot)
	w.itemsChanged(e)
}

// itemArg 是 ITEM_getArgument(参数, key) 的整数值（"noen:40"、"rand:500"），没有为 0。
func itemArg(def *data.ItemDef, key string) int {
	s, _ := data.ArgField(def.Argument, key)
	return data.Atoi(s)
}
