package world

import (
	"fmt"
	"strings"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 交易（char/trade.c lssproto_TD_recv，_TRADESYSTEM2 + _ITEM_PILEFORTRADE）：
//
//	开始     面前那一格只有一个可以交易的玩家（没在战斗、没组队、开着"交易"、没在交易）就直接打开两边的交易窗口（没有同意的步骤，
//	         _COMFIRM_TRADE_REQUEST 没开）；自己也不能在组队或战斗中
//	放东西   道具一次放一个（堆叠的再放一次加一个）、宠物、石币（覆盖）；放不进去（丢地上会消失的、名字 * 开头的绑定道具 / 宠物、
//	         宠物身上有绑定道具、对方照顾不了的宠物）就整个交易取消
//	锁定     两边都"锁定"后才能按"确定"；后按的那一边触发交换，先按的那一边收到"对方确定"
//	交换     检查双方道具栏、宠物栏、石币上限，都够了才一次换完；成功"交易ＯＫ！"、失败"交易失败！"
//	取消     对方"%s将交易取消了！"，自己"交易取消！"
//
// 与原版不同：交换在拷贝上做完再一起写回（原版失败时不还原）；骑着的宠物服务端也挡（原版只有客户端挡）；
// 对方下线、开打、被传走时交易取消（原版要等下一次放东西才发现）；交易中头上的记号（CHAR_ACTTRADE）由 ObjMark.trade 送；
// 楼层 50000 的 PK 交易（_TRADE_PK）没做。

// tradeSide 是交易的一边（TradeList 的一格）。
type tradeSide struct {
	e       *entity
	items   [][2]int // 放上去的道具：格位、个数（最多 15 项）
	pets    []int    // 放上去的宠物格位（最多 5 只）
	gold    int
	confirm bool // 按了"锁定"
	ok      bool // 按了"确定"（CHAR_TRADE_LOCK）
}

// trade 是一场交易。
type trade struct {
	sides [2]*tradeSide
}

// side 回传 e 的那一边与对方。
func (t *trade) side(e *entity) (*tradeSide, *tradeSide) {
	if t.sides[0].e == e {
		return t.sides[0], t.sides[1]
	}
	return t.sides[1], t.sides[0]
}

// busy：在战斗或交易中，不能动道具与宠物（原版 CHAR_WORKTRADEMODE != FREE 时那些指令都不理）。
func (e *entity) busy() bool {
	return e.fight != nil || e.watch != nil || e.trade != nil || e.stall != nil || e.buying != nil
}

// tradeIntact 是 tradeSwap 前的核对：放上去的道具还在那一格、数量够，宠物还在（放上去之后不能动道具，但以防万一：
// 对方锁定后自己把那件卖给 NPC、寄放起来，换到的就是空的）。
func tradeIntact(s *tradeSide) bool {
	for _, x := range s.items {
		it := s.e.inv[x[0]]
		if it == nil || it.count() < x[1] {
			return false
		}
	}
	for _, i := range s.pets {
		if s.e.pets.Pets[i] == nil || s.e.pets.Ride == i {
			return false
		}
	}
	return s.e.char.Gold >= s.gold
}

// Trade 是 lssproto_TD_recv：cmd 为 request（D|D）、item / gold / pet（T|…|I / G / P）、lock（C）、ok（K）、cancel（W）。
func (w *World) Trade(charID int, cmd string, slot, amount int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil {
			return
		}
		if cmd == "request" {
			w.tradeRequest(e)
			return
		}
		t := e.trade
		if t == nil {
			return
		}
		me, other := t.side(e)
		switch cmd {
		case "item", "gold", "pet":
			if me.confirm || e.fight != nil || other.e.fight != nil {
				w.tradeCancel(e) // 锁定后还放东西：TRADE_Close
				return
			}
			if !w.tradePut(me, other, cmd, slot, amount) {
				w.tradeCancel(e)
			}
		case "lock":
			if !me.confirm {
				me.confirm = true
				other.e.client.Send(&proto.TradeState{State: "locked"})
			}
		case "ok":
			if !me.confirm || !other.confirm || me.ok {
				return
			}
			me.ok = true
			if !other.ok {
				return
			}
			other.e.client.Send(&proto.TradeState{State: "ok"})
			w.tradeFinish(t, w.tradeSwap(t))
		case "cancel":
			w.tradeCancel(e)
		}
	})
}

// tradeRequest 是 TRADE_Search。
func (w *World) tradeRequest(e *entity) {
	if e.busy() || e.party != nil {
		return
	}
	fx, fy := e.x+MOVE[e.dir][0], e.y+MOVE[e.dir][1]
	var cands []*entity
	any, found := false, false
	for _, o := range w.floors[e.floor] {
		if o.x != fx || o.y != fy || o == e {
			continue
		}
		any = true
		if o.kind != "player" {
			continue
		}
		found = true
		if !o.busy() && o.party == nil && o.fs&fsTrade != 0 {
			cands = append(cands, o)
		}
	}
	switch {
	case !any:
		w.sysMsg(e, "前方什麽人也没有！", colorYellow)
	case len(cands) == 0 && found:
		w.sysMsg(e, "真是抱歉，对方不愿意跟你交易！", colorYellow)
	case len(cands) == 0:
		w.sysMsg(e, "前方什麽人也没有！", colorYellow)
	case len(cands) > 1:
		w.sysMsg(e, "前方不只一位玩家喔！", colorYellow)
	default:
		o := cands[0]
		w.sysMsg(e, fmt.Sprintf("请稍候，连络%s中···", o.name), colorYellow)
		t := &trade{sides: [2]*tradeSide{{e: e}, {e: o}}}
		e.trade, o.trade = t, t
		e.client.Send(&proto.TradeOpen{ID: o.id, Name: o.name})
		o.client.Send(&proto.TradeOpen{ID: e.id, Name: e.name})
		w.broadcastMark(e) // 交易中的记号（CHAR_sendTradeEffect）
		w.broadcastMark(o)
	}
}

// tradePut 是 TRADE_ShowItem：放上去并告诉对方；不能放时回传 false。
func (w *World) tradePut(me, other *tradeSide, kind string, slot, amount int) bool {
	e := me.e
	switch kind {
	case "item":
		if slot < data.EquipSlots || slot >= data.ItemSlots || e.inv[slot] == nil {
			return false
		}
		it := e.inv[slot]
		def := w.game.Items[it.ID]
		if def == nil || def.VanishDrop || strings.HasPrefix(def.Name, "*") {
			if def != nil {
				w.sysMsg(e, def.Name+"无法交易。", colorYellow)
			}
			return false
		}
		n := 0
		for i, x := range me.items {
			if x[0] == slot { // 同一格再放一次：多放一个（不能超过那一叠）
				if x[1] >= it.count() {
					return false
				}
				me.items[i][1]++
				n = me.items[i][1]
			}
		}
		if n == 0 {
			if len(me.items) >= 15 {
				return false
			}
			me.items = append(me.items, [2]int{slot, 1})
			n = 1
		}
		x := invItem(slot, it, def)
		x.Pile = n
		other.e.client.Send(&proto.TradeItem{Slot: slot, Count: n, Items: []proto.InvItem{x}})
	case "gold":
		if amount < 0 || amount > e.char.Gold {
			return false
		}
		me.gold = amount
		other.e.client.Send(&proto.TradeGold{Amount: amount})
	case "pet":
		if slot < 0 || slot >= PetSlots || e.pets.Pets[slot] == nil || len(me.pets) >= PetSlots {
			return false
		}
		p := e.pets.Pets[slot]
		for _, s := range me.pets {
			if s == slot {
				return false
			}
		}
		if slot == e.pets.Ride || p.Away {
			return false // 骑着的宠物（原客户端挡）、飞出去的邮件宠物
		}
		if strings.HasPrefix(p.Name, "*") {
			w.sysMsg(e, "绑定宠物无法交易！", colorYellow)
			return false
		}
		if p.Guard {
			w.sysMsg(e, "家族守护兽无法交易。", colorYellow)
			return false
		}
		for _, it := range p.Items {
			if it != nil && strings.HasPrefix(w.itemName(it), "*") {
				w.sysMsg(e, "宠物身上有绑定的道具无法交易！", colorYellow)
				return false
			}
		}
		if o := other.e; p.Level > o.char.Level+20 && o.char.Trans == 0 {
			w.sysMsg(e, "对方无法照顾该宠物。", colorYellow)
			return false
		}
		me.pets = append(me.pets, slot)
		other.e.client.Send(&proto.TradePet{Pets: []proto.PetInfo{w.petInfo(e, slot)}})
	default:
		return false
	}
	return true
}

// tradeTake 从 inv 拿出这一边放上去的道具（整叠的拿走、部分的分出来）。
func tradeTake(inv *inventory, items [][2]int) []*Item {
	var out []*Item
	for _, x := range items {
		it := inv[x[0]]
		if it == nil {
			continue
		}
		if x[1] >= it.count() {
			inv[x[0]] = nil
			out = append(out, it)
			continue
		}
		part := *it
		part.Pile = x[1]
		it.Pile = it.count() - x[1]
		out = append(out, &part)
	}
	return out
}

// cloneInventory 深拷贝一份道具栏（交换先在拷贝上做）。
func cloneInventory(inv *inventory) *inventory {
	c := &inventory{}
	for i, it := range inv {
		if it != nil {
			x := *it
			c[i] = &x
		}
	}
	return c
}

// tradeSwap 是 TRADE_CheckTradeList + TRADE_HandleTrade：检查空间后一次换完。
func (w *World) tradeSwap(t *trade) bool {
	a, b := t.sides[0], t.sides[1]
	if !tradeIntact(a) || !tradeIntact(b) {
		return false
	}
	invA, invB := cloneInventory(a.e.inv), cloneInventory(b.e.inv)
	fromA, fromB := tradeTake(invA, a.items), tradeTake(invB, b.items)
	fits := func(inv *inventory, items []*Item) bool {
		for _, it := range items {
			if w.addItem(inv, it) < 0 {
				return false
			}
		}
		return true
	}
	ok := true
	short := func(side, other *tradeSide, what string) {
		w.sysMsg(side.e, "交易後"+what, colorYellow)
		w.sysMsg(other.e, "对方交易後"+what, colorYellow)
		ok = false
	}
	if !fits(invA, fromB) {
		short(a, b, "物品栏位不足。")
	}
	if !fits(invB, fromA) {
		short(b, a, "物品栏位不足。")
	}
	freePets := func(s *tradeSide) int { return s.e.pets.freeCount() + len(s.pets) }
	if len(b.pets) > freePets(a) {
		short(a, b, "宠物栏位不足。")
	}
	if len(a.pets) > freePets(b) {
		short(b, a, "宠物栏位不足。")
	}
	goldFits := func(s, o *tradeSide) bool { return maxGold(s.e.char.Trans)-s.e.char.Gold+s.gold >= o.gold }
	if !goldFits(a, b) {
		short(a, b, "石币超过上限。")
	}
	if !goldFits(b, a) {
		short(b, a, "石币超过上限。")
	}
	if a.gold > a.e.char.Gold || b.gold > b.e.char.Gold || !ok {
		return false
	}
	*a.e.inv, *b.e.inv = *invA, *invB
	petsA, petsB := w.tradeTakePets(a), w.tradeTakePets(b)
	w.tradeGivePets(a.e, petsB)
	w.tradeGivePets(b.e, petsA)
	a.e.char.Gold += b.gold - a.gold
	b.e.char.Gold += a.gold - b.gold
	return true
}

// tradeTakePets 从宠物栏拿走放上去的宠物，出战 / 待命的位置跟着清掉。
func (w *World) tradeTakePets(s *tradeSide) []*Pet {
	bag := s.e.pets
	var out []*Pet
	for _, i := range s.pets {
		if p := bag.Pets[i]; p != nil {
			out = append(out, p)
			bag.Pets[i] = nil
			bag.Standby &^= 1 << i
			if bag.Battle == i {
				bag.Battle = -1
			}
			if bag.Ride == i {
				bag.Ride = -1 // 不然换来的宠物落在同一格就变成"骑着"它
			}
		}
	}
	return out
}

// tradeGivePets 放进空格，主人改成新主人（CHAR_OWNERCDKEY，所以新主人能改名）。
func (w *World) tradeGivePets(e *entity, pets []*Pet) {
	for _, p := range pets {
		p.Owner = e.char.ID
		w.addPet(e, p)
	}
}

// tradeFinish 送结果、关掉两边的窗口；成功时存档（CHAR_charSaveFromConnect）。
func (w *World) tradeFinish(t *trade, ok bool) {
	for _, s := range t.sides {
		e := s.e
		e.trade = nil
		if ok {
			w.sysMsg(e, "交易ＯＫ！", colorYellow)
			w.sysMsg(e, "系统自动为您存档!", colorRed)
		} else {
			w.sysMsg(e, "交易失败！", colorYellow)
		}
		e.client.Send(&proto.TradeClose{})
		w.broadcastMark(e)
		w.itemsChanged(e)
		w.petsChanged(e)
		w.saveProgress(e)
	}
}

// tradeCancel 是 TRADE_Close：e 取消（或因为别的事被取消）。
func (w *World) tradeCancel(e *entity) {
	t := e.trade
	if t == nil {
		return
	}
	_, other := t.side(e)
	for _, s := range t.sides {
		s.e.trade = nil
		s.e.client.Send(&proto.TradeClose{})
		w.broadcastMark(s.e)
		w.sendPets(s.e) // 客户端放上去时藏起来的宠物重新显示
	}
	w.sysMsg(other.e, fmt.Sprintf("%s将交易取消了！", e.name), colorYellow)
	w.sysMsg(e, "交易取消！", colorYellow)
}

// cancelTrade：下线、开打、被传走时取消交易。
func (w *World) cancelTrade(e *entity) {
	if e.trade != nil {
		w.tradeCancel(e)
	}
}
