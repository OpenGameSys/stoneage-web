package world

// 摆摊（_STREET_VENDOR：callfromcli.c lssproto_STREET_VENDOR_recv、char.c CHAR_sendStreetVendor / CHAR_sendStreetVendorDataToCli；
// 客户端 field.cpp 的摆摊钮与 StreetVendorWndfunc / StreetVendorBuyWndfunc）。这里只做石币摆摊（_NEW_STREET_VENDOR 的声望 / 积分摊位不做）。
//
//	摆摊钮（Vendor cmd open）
//	  面前没有人：自己没摆 → 打开设定窗口（VendorSetup）；已经摆着 → 收起来改内容（有人在看就通知他"店家取消交易"）。
//	              自己站的格子上已经有别人在摆 → "你所站的位置已经有人在摆摊了"。
//	  面前有人：对方摆着 → 看他的摊位（VendorList，双方都是"交易中"，头上有记号）；东西卖完了说"店家东西卖完了"；
//	            对方没摆 / 正在和别人交易 → 和"面前没有人"一样处理自己的摊位。
//	设定（set）  最多 20 项（MAX_SELL_ITEM），每项是道具栏的一格或一只宠物加价钱（0..1000 万）；登出消失的道具、家族守护兽、
//	            骑着的宠物、送信中的宠物不能卖（那一项取消）；标题最多 16 个字节，空的是"欢迎光临!!"，前面加"[石]"。
//	            摆着时不能走动、不能组队 / 交易 / 战斗（Move 挡掉，和原版客户端 iOnStreetVendor 一样）。
//	收摊（close）、买方不买了（leave）、看一项的详细资料（detail）、买（buy：选好的几项一起结帐，
//	            钱不够 / 店家放不下石币（金钱上限，这里没有个人银行）/ 道具栏或宠物栏满了就停下，前面买到的算数；
//	            税按 rules.vendorTax 的百分比（setup.cf STREETVENDORPOINT 的石币税率，原版 0），至少 1 石币）。
//	登出、被传送走、开打时摊位收起来、买方离开（vendorClear）。

import (
	"fmt"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

const (
	vendorMax      = 20       // MAX_SELL_ITEM
	vendorPriceMax = 10000000 // CHAR_sendStreetVendor 的价钱上限
	vendorTitleMax = 16       // _STREET_VENDOR_CHECK：标题的 GBK 字节数
)

type vendorEntry struct {
	kind  int // 0 道具 1 宠物（SV_KIND）
	slot  int // 道具栏格位 / 宠物栏格位（SV_INDEX）
	price int
	sold  bool
	// 开张时那一格的内容（买的时候核对，免得店家换了东西还按原价卖）：道具的编号与个数、宠物
	itemID int
	pile   int
	pet    *Pet
}

// vendorStall 是一个摊位（CHAR_WORKSTREETVENDOR 1 / 2 与贩卖清单）。
type vendorStall struct {
	title   string
	entries []vendorEntry
	who     *entity // 正在看摊位的买家（CHAR_WORKSTREETVENDOR_WHO）
}

// Vendor 是 lssproto_STREET_VENDOR_recv：cmd 是 open / set / close / leave / detail / buy。
func (w *World) Vendor(charID int, cmd, title string, kinds, slots, prices []int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || e.fight != nil || e.watch != nil {
			return
		}
		switch cmd {
		case "open":
			if e.party != nil {
				w.sysMsg(e, "组队状态下不能交易", colorYellow)
				return
			}
			w.vendorOpen(e)
		case "set":
			w.vendorSet(e, title, kinds, slots, prices)
		case "close":
			w.vendorEnd(e)
		case "leave":
			w.vendorLeave(e)
		case "detail":
			if len(slots) > 0 {
				w.vendorDetail(e, slots[0])
			}
		case "buy":
			w.vendorBuy(e, slots)
		}
	})
}

// vendorOpen 是 'O'：按摆摊钮。
func (w *World) vendorOpen(e *entity) {
	if e.trade != nil {
		return
	}
	fx, fy := e.x+MOVE[e.dir][0], e.y+MOVE[e.dir][1]
	var front []*entity
	for _, o := range w.floors[e.floor] {
		if o != e && o.kind == "player" && o.x == fx && o.y == fy {
			front = append(front, o)
		}
	}
	for _, o := range front {
		if o.party != nil || o.trade != nil {
			continue
		}
		if o.stall == nil || e.stall != nil {
			break // 对方没摆（或自己摆着）：照自己的摊位处理
		}
		if o.stall.who != nil && o.stall.who != e {
			w.sysMsg(e, "店家正在和别人交易", colorYellow)
			return
		}
		if w.vendorSoldOut(o) {
			w.sysMsg(e, "店家东西卖完了", colorYellow)
			return
		}
		w.vendorLeave(e)
		e.buying, o.stall.who = o, e
		w.sysMsg(e, "此家店铺以石币方式交易!", colorRed)
		w.vendorSendList(e, o)
		w.broadcastMark(e)
		w.broadcastMark(o)
		return
	}
	if e.stall == nil {
		for _, o := range w.floors[e.floor] {
			if o != e && o.kind == "player" && o.x == e.x && o.y == e.y && o.stall != nil {
				w.sysMsg(e, "你所站的位置已经有人在摆摊了", colorYellow)
				return
			}
		}
		w.vendorLeave(e)
		e.client.Send(&proto.VendorSetup{Entries: []proto.VendorEntry{}})
		return
	}
	// 已经摆着：收起来改内容（CHAR_WORKSTREETVENDOR 1 / 2 → -1）
	msg := &proto.VendorSetup{Entries: w.vendorEntries(e)}
	w.vendorEnd(e)
	e.client.Send(msg)
}

// vendorSet 是 'S'：设定贩卖内容并开张。
func (w *World) vendorSet(e *entity, title string, kinds, slots, prices []int) {
	if e.buying != nil || e.trade != nil || e.party != nil {
		return
	}
	w.vendorEnd(e)
	if gbkLen(title) > vendorTitleMax {
		w.sysMsg(e, "摊位标题过长！", colorYellow)
		return
	}
	if title == "" {
		title = "欢迎光临!!"
	}
	st := &vendorStall{title: "[石]" + title}
	for i := 0; i < len(kinds) && i < len(slots) && i < len(prices) && len(st.entries) < vendorMax; i++ {
		price := max(0, min(vendorPriceMax, prices[i]))
		v := vendorEntry{kind: kinds[i], slot: slots[i], price: price}
		switch v.kind {
		case 0:
			if v.slot < data.EquipSlots || v.slot >= data.ItemSlots || e.inv[v.slot] == nil {
				continue
			}
			def := w.game.Items[e.inv[v.slot].ID]
			if def == nil {
				continue
			}
			if def.VanishDrop {
				w.sysMsg(e, "丢在地上会消失的道具不可贩卖，该选项取消", colorYellow)
				continue
			}
			v.itemID, v.pile = e.inv[v.slot].ID, e.inv[v.slot].count()
		case 1:
			if v.slot < 0 || v.slot >= PetSlots || e.pets.Pets[v.slot] == nil {
				continue
			}
			p := e.pets.Pets[v.slot]
			switch {
			case p.Guard:
				w.sysMsg(e, "家族守护兽无法贩卖，该选项取消", colorYellow)
				continue
			case e.pets.Ride == v.slot:
				w.sysMsg(e, "骑乘中的宠物无法贩卖，该选项取消", colorYellow)
				continue
			case p.Away:
				w.sysMsg(e, "送信中的宠物无法贩卖，该选项取消", colorYellow)
				continue
			}
			v.pet = p
		default:
			continue
		}
		dup := false
		for _, x := range st.entries {
			if x.kind == v.kind && x.slot == v.slot {
				dup = true
			}
		}
		if !dup {
			st.entries = append(st.entries, v)
		}
	}
	if len(st.entries) == 0 {
		return
	}
	e.stall = st
	e.clearAction()
	w.sysMsg(e, "开始摆摊。再按一次摆摊钮可以修改内容或收摊。", colorYellow)
	w.broadcastMark(e)
}

// vendorEnd 是 'E'：收摊；有人在看就通知他取消。
func (w *World) vendorEnd(e *entity) {
	st := e.stall
	if st == nil {
		return
	}
	if b := st.who; b != nil {
		st.who, b.buying = nil, nil
		b.client.Send(&proto.VendorClose{})
		w.sysMsg(b, "店家取消交易", colorYellow)
		w.broadcastMark(b)
	}
	e.stall = nil
	w.broadcastMark(e)
}

// vendorLeave 是 'N'：买方不买了（或离开）。
func (w *World) vendorLeave(e *entity) {
	s := e.buying
	if s == nil {
		return
	}
	e.buying = nil
	if s.stall != nil && s.stall.who == e {
		s.stall.who = nil
		w.broadcastMark(s)
	}
	e.client.Send(&proto.VendorClose{})
	w.broadcastMark(e)
}

// vendorClear：登出、被传送、开打时收摊、离开别人的摊位。
func (w *World) vendorClear(e *entity) {
	w.vendorEnd(e)
	w.vendorLeave(e)
}

func (w *World) vendorSoldOut(s *entity) bool {
	for _, v := range s.entries() {
		if !v.sold {
			return false
		}
	}
	return true
}

func (e *entity) entries() []vendorEntry {
	if e.stall == nil {
		return nil
	}
	return e.stall.entries
}

// vendorEntries 是 CHAR_sendStreetVendorDataToCli 的内容：还没卖掉的各项（道具带名字、个数、图；宠物带名字、等级、形象）。
func (w *World) vendorEntries(s *entity) []proto.VendorEntry {
	out := []proto.VendorEntry{}
	for i, v := range s.entries() {
		if v.sold {
			continue
		}
		x := proto.VendorEntry{Index: i, Kind: v.kind, Slot: v.slot, Price: v.price}
		if v.kind == 0 {
			it := s.inv[v.slot]
			if it == nil || w.game.Items[it.ID] == nil {
				continue
			}
			def := w.game.Items[it.ID]
			x.Name, x.Pile, x.Image = def.Name, it.count(), def.Image
			if it.Name != "" {
				x.Name = it.Name
			}
		} else {
			p := s.pets.Pets[v.slot]
			if p == nil {
				continue
			}
			x.Name, x.Level, x.Sprite = p.Name, p.Level, p.Sprite
			if p.FreeName != "" {
				x.Name = p.FreeName + "（" + p.Name + "）"
			}
		}
		out = append(out, x)
	}
	return out
}

func (w *World) vendorSendList(b, s *entity) {
	b.client.Send(&proto.VendorList{Seller: s.name, Title: s.stall.title, Entries: w.vendorEntries(s)})
}

// vendorDetail 是 'D'：买方要看某一项的详细资料（CHAR_sendStreetVendorOneDataToCli）。
func (w *World) vendorDetail(b *entity, index int) {
	s := b.buying
	if s == nil || s.stall == nil || index < 0 || index >= len(s.stall.entries) {
		return
	}
	v := s.stall.entries[index]
	msg := &proto.VendorDetail{Index: index, Kind: v.kind}
	if v.kind == 0 {
		it := s.inv[v.slot]
		if it == nil || w.game.Items[it.ID] == nil {
			return
		}
		msg.Item = invItem(v.slot, it, w.game.Items[it.ID])
	} else {
		if s.pets.Pets[v.slot] == nil {
			return
		}
		msg.Pet = w.petInfo(s, v.slot)
	}
	b.client.Send(msg)
}

// vendorBuy 是 'B'：买方选好的几项一起结帐。
func (w *World) vendorBuy(b *entity, want []int) {
	s := b.buying
	if s == nil || s.stall == nil {
		return
	}
	tax := max(0, min(100, w.Rules.VendorTax))
	bought := 0
	for n, index := range want {
		if n >= vendorMax {
			break
		}
		if index < 0 || index >= len(s.stall.entries) || s.stall.entries[index].sold {
			continue
		}
		v := &s.stall.entries[index]
		if v.price <= 0 { // 议价物品不能卖
			continue
		}
		if b.char.Gold < v.price {
			w.sysMsg(b, "你身上石币不够。", colorRed)
			break
		}
		if s.char.Gold+v.price > maxGold(s.char.Trans) {
			w.sysMsg(b, "店家放不下石币了，交易取消。", colorRed)
			w.sysMsg(s, "你身上的石币已满，有一笔交易取消了。", colorRed)
			break
		}
		var name string
		if v.kind == 0 {
			it := s.inv[v.slot]
			if it == nil || w.game.Items[it.ID] == nil || it.ID != v.itemID || it.count() != v.pile {
				v.sold = true // 那一格的东西和开张时不一样（照理不会：摆着时不能动道具），这项作废
				continue
			}
			if w.addItem(b.inv, it) < 0 {
				w.sysMsg(b, "道具栏满了。", colorRed)
				break
			}
			s.inv[v.slot] = nil
			name = w.game.Items[it.ID].Name
		} else {
			p := s.pets.Pets[v.slot]
			if p == nil || p != v.pet || s.pets.Ride == v.slot {
				v.sold = true
				continue
			}
			if b.pets.free() < 0 {
				w.sysMsg(b, "宠物栏满了。", colorRed)
				break
			}
			pets := w.tradeTakePets(&tradeSide{e: s, pets: []int{v.slot}})
			w.tradeGivePets(b, pets)
			name = p.Name
		}
		fee := 0
		if tax > 0 {
			fee = max(1, v.price*tax/100)
		}
		b.char.Gold -= v.price
		s.char.Gold += v.price - fee
		v.sold = true
		bought++
		w.sysMsg(b, fmt.Sprintf("买下了%s，花费 %d 石币。", name, v.price), colorYellow)
		if fee > 0 {
			w.sysMsg(s, fmt.Sprintf("%s买走了%s，得到 %d 石币（税 %d）。", b.name, name, v.price-fee, fee), colorYellow)
		} else {
			w.sysMsg(s, fmt.Sprintf("%s买走了%s，得到 %d 石币。", b.name, name, v.price), colorYellow)
		}
	}
	if bought > 0 {
		for _, x := range [2]*entity{b, s} {
			w.itemsChanged(x)
			w.petsChanged(x)
			w.saveProgress(x)
		}
		if w.vendorSoldOut(s) {
			w.sysMsg(s, "东西都卖完了。", colorYellow)
		}
	}
	w.vendorSendList(b, s)
}
