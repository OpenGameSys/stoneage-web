package world

import (
	"strings"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// NPC 参数里的条件与动作（npc/npc_eventaction.c）：传送员的 FREE 条件、DelItem 交出道具等。

// passCheck 是 NPC_ActionPassCheck：条件以 "," 分成几组（任一组成立即可），组内以 "&" 连接（全部成立）。
func (w *World) passCheck(p *entity, cond string) bool {
	if cond == "" {
		return false // getStringFromIndexWithDelim 在空字串上取不到第一段
	}
	for _, group := range strings.Split(cond, ",") {
		ok := true
		for _, c := range strings.Split(group, "&") {
			if !w.condCheck(p, c) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// 比较方式（NPC_ActionBigSmallLastCheck 的 flg）
const (
	cmpEq = iota
	cmpLess
	cmpGreater
)

// condCheck 是 NPC_ActionBigSmallCheck：按 "<"、">"、"!="、"=" 的顺序找比较符；"ITEM=编号*个数" 要持有那么多个、
// "ITEM=编号^个数" 要正好那么多个（_NPC_ActionFreeCmp）。"-" 之后是宠物编号（_NEW_WARPMAN 的 PET 条件）。
func (w *World) condCheck(p *entity, c string) bool {
	temp := -1
	if l, r, ok := strings.Cut(c, "-"); ok {
		c, temp = l, data.Atoi(r)
	}
	switch {
	case strings.Contains(c, "<"):
		name, val, _ := strings.Cut(c, "<")
		return w.freeIf(p, name, data.Atoi(val), cmpLess, temp)
	case strings.Contains(c, ">"):
		name, val, _ := strings.Cut(c, ">")
		return w.freeIf(p, name, data.Atoi(val), cmpGreater, temp)
	case strings.Contains(c, "!"):
		name, val, _ := strings.Cut(c, "!=")
		return !w.freeIf(p, name, data.Atoi(val), cmpEq, temp)
	case strings.Contains(c, "="):
		name, val, _ := strings.Cut(c, "=")
		if id, n, ok := strings.Cut(val, "*"); ok { // NPC_ActionWarpManReduce
			return w.countItem(p, data.Atoi(id)) >= data.Atoi(n)
		}
		if id, n, ok := strings.Cut(val, "^"); ok { // NPC_ActionWarpManReduce2
			return w.countItem(p, data.Atoi(id)) == data.Atoi(n)
		}
		return w.freeIf(p, name, data.Atoi(val), cmpEq, temp)
	}
	return false
}

func compare(want, have, flg int) bool {
	switch flg {
	case cmpEq:
		return have == want
	case cmpLess:
		return have < want
	case cmpGreater:
		return have > want
	}
	return false
}

// freeIf 是 NPC_ActionFreeIfCheck 里这里有对应数据的几项：等级、金钱、转生、道具栏里的道具、装备、任务旗标（ENDEV/NOWEV）、宠物、
// 庄园（MANOR=n：是庄园 n（0 起）的家族的正式族人；FM=楼层：正式族人而且占有的庄园的族长家是那一层）。组队、时间等一律不成立。
func (w *World) freeIf(p *entity, name string, n, flg, temp int) bool {
	c := &p.char
	switch name {
	case "LV":
		return compare(n, c.Level, flg)
	case "GOLD":
		return compare(n, c.Gold, flg)
	case "TRANS":
		return compare(n, c.Trans, flg)
	case "PET": // ActionCheckMyPet："PET<等级-模板编号"
		return w.petCheck(p, n, flg, temp)
	case "ITEM": // NPC_ActionItemCheck：道具栏（不含装备）里有任一件编号符合
		for i := data.EquipSlots; i < data.ItemSlots; i++ {
			if it := p.inv[i]; it != nil && compare(n, it.ID, flg) {
				return true
			}
		}
	case "reITEM": // NPC_ActionReItemCheck：道具栏的空格数
		return compare(n, w.freeBagSlots(p), flg)
	case "rePET": // NPC_ActionRePetCheck：宠物栏的空格数
		return compare(n, p.pets.freeCount(), flg)
	case "ENDEV", "EVEND": // NPC_EventCheckFlg：比较符不论，有那个旗标就成立（"!=" 由 condCheck 取反）
		return p.eventDone(n)
	case "NOWEV", "EVNOW":
		return p.eventNow(n)
	case "MANOR":
		return n > -1 && p.fm.formal() && p.fm.Manor == n
	case "FM":
		return p.fm.formal() && p.fm.Floor == n
	case "EQUIT": // NPC_ActioneEquitCheck：装备中的道具
		for i := 0; i < data.EquipSlots; i++ {
			if it := p.inv[i]; it != nil && compare(n, it.ID, flg) {
				return true
			}
		}
	}
	return false
}

// countItem 数道具栏（不含装备）里某编号的道具有几个（堆叠的按堆叠数算）。
func (w *World) countItem(p *entity, id int) int {
	n := 0
	for i := data.EquipSlots; i < data.ItemSlots; i++ {
		if it := p.inv[i]; it != nil && it.ID == id {
			n += it.count()
		}
	}
	return n
}

// hasItem 是万事通的 checkhaveitem：整个道具栏（含装备）里有没有这个编号。
func (w *World) hasItem(p *entity, id int) bool {
	for _, it := range p.inv {
		if it != nil && it.ID == id {
			return true
		}
	}
	return false
}

// itemNeed 是要收走的一种道具：N 个，或 All 时身上那种全部。
type itemNeed struct {
	ID, N int
	All   bool
}

// parseNeeds 把 "编号*个数,编号,..."（sep 为分隔符）拆成 itemNeed；没写个数时 all 决定是"全部"还是"1 个"。
func parseNeeds(spec, sep string, all bool) []itemNeed {
	var out []itemNeed
	for _, s := range strings.Split(spec, sep) {
		if s == "" {
			continue
		}
		if id, n, ok := strings.Cut(s, "*"); ok {
			out = append(out, itemNeed{ID: data.Atoi(id), N: data.Atoi(n)})
		} else if all {
			out = append(out, itemNeed{ID: data.Atoi(s), All: true})
		} else {
			out = append(out, itemNeed{ID: data.Atoi(s), N: 1})
		}
	}
	return out
}

// haveItems：道具栏里每一种都有足够的个数。
func (w *World) haveItems(p *entity, needs []itemNeed) bool {
	for _, nd := range needs {
		if !nd.All && w.countItem(p, nd.ID) < nd.N {
			return false
		}
	}
	return true
}

// takeItems 从道具栏（不含装备）收走道具；不够时什么都不收，回 false。say 时每收一件在聊天栏写"交出某某。"。
// 收走后存档并更新道具栏。
func (w *World) takeItems(p *entity, needs []itemNeed, say bool) bool {
	if !w.haveItems(p, needs) {
		return false
	}
	changed := false
	for _, nd := range needs {
		left := nd.N
		for i := data.EquipSlots; i < data.ItemSlots && (nd.All || left > 0); i++ {
			it := p.inv[i]
			if it == nil || it.ID != nd.ID {
				continue
			}
			n := it.count()
			if !nd.All {
				n = min(n, left)
				left -= n
			}
			if say {
				if def := w.game.Items[it.ID]; def != nil {
					w.sysMsg(p, "交出"+def.Name+"。", colorYellow)
				}
			}
			if it.count() > n {
				it.Pile = it.count() - n
			} else {
				p.inv[i] = nil
			}
			changed = true
		}
	}
	if changed {
		w.itemsChanged(p)
	}
	return true
}

// delItems 是 NPC_ActionDelItem："编号*个数" 要交出那么多个，不够就说"条件不足"并失败；只写编号的把身上那种全部交出（没有也不算失败）。
func (w *World) delItems(p *entity, spec string) bool {
	if !w.takeItems(p, parseNeeds(spec, ",", true), true) {
		w.sysMsg(p, "条件不足", colorYellow)
		return false
	}
	return true
}

// 聊天栏的系统文字颜色（CHAR_COLOR*，客户端 FONT_PAL 的下标）
const (
	colorWhite  = 0
	colorYellow = 4
	colorRed    = 6
)

// sysMsg 是 CHAR_talkToCli(talker, -1, ...)：不带说话者的系统文字。
func (w *World) sysMsg(p *entity, text string, color int) {
	p.client.Send(&proto.ChatMsg{Channel: "near", Text: text, Color: color})
}
