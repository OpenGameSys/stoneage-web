package world

import (
	"fmt"
	"strconv"
	"strings"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 道具店（npc_itemshop.c）：选单（买 / 卖 / 离开）→ 买的清单（选道具、个数、确认）或卖的清单（选身上的道具、确认）。
// 窗口内容照原版 WN 字串的各栏改成结构化的 ShopWindow；客户端回传仍照原版的 data："1"/"2"/"3"（选单）、
// "第几项|个数"（买）、"格位|个数"（卖），"0" 表示回选单。

// 窗口编号（CHAR_WINDOWTYPE_WINDOWITEMSHOP_*）
const (
	seqShopStart   = 240
	seqShopBuy     = 242
	seqShopSell    = 243
	seqShopLimit   = 245
	seqShopExpress = 246
	maxShopItems   = 32 // MAXSHOPITEM 33：数到第 33 个就停
)

// shopTypes 是 npc_itemshop.c 的 TypeTable（_ITEM_TYPETABLE 未定义的那一组）：LimitItemType 的名字 → 道具种类；
// ACCESSORY、OFFENCE、DEFENCE 是一组种类。
var shopTypes = map[string]int{"FIST": 0, "AXE": 1, "CLUB": 2, "SPEAR": 3, "BOW": 4, "SHIELD": 5, "HELM": 6, "ARMOUR": 7,
	"BRACELET": 8, "ANCLET": 9, "NECKLACE": 10, "RING": 11, "BELT": 12, "EARRING": 13, "NOSERING": 14, "AMULET": 15,
	"OTHER": 16, "BOOMERANG": 17, "BOUNDTHROW": 18, "BREAKTHROW": 19, "ACCESSORY": 30, "OFFENCE": 40, "DEFENCE": 50}

func shopTypeMatch(t, itemType int) bool {
	switch t {
	case 30:
		return 8 <= itemType && itemType <= 15
	case 40:
		return 0 <= itemType && itemType <= 4 || 17 <= itemType && itemType <= 19
	case 50:
		return 5 <= itemType && itemType <= 7
	}
	return t == itemType
}

// maxGold 是 CHAR_getMaxHaveGold（_FIX_MAX_GOLD）：身上金钱上限按转生次数。
func maxGold(trans int) int {
	switch trans {
	case 0:
		return 1_000_000
	case 1:
		return 2_000_000
	case 2:
		return 5_000_000
	case 3:
		return 10_000_000
	case 4:
		return 50_000_000
	}
	return 100_000_000
}

func atof(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

// idRange 拆 "编号" 或 "起-迄"（顺序反了会对调），回传 [起, 迄]。
func idRange(s string) (int, int, bool) {
	if a, b, ok := strings.Cut(s, "-"); ok {
		lo, hi := data.Atoi(a), data.Atoi(b)
		return min(lo, hi), max(lo, hi), true
	}
	if s == "" {
		return 0, 0, false
	}
	return data.Atoi(s), data.Atoi(s), true
}

// shopTalk 是 NPC_ItemShopTalked：面对面 2 格内或站在旁边。talk 是对着店员说的话：含 buy_msg / sell_msg 的词就直接进买 / 卖，
// 含 other_msg 的词回 hint_msg。点选店员（talk 为空）时：LIMITSHOP 显示 sellonly_msg 的确认、EVENT 店说 sellonly_msg、
// EXPRESS 显示长毛象快递的选单，其余打开买卖选单。
func (w *World) shopTalk(p, n *entity, talk string) {
	if !faceToFace(n, p, 2) && max(abs(n.x-p.x), abs(n.y-p.y)) > 1 {
		return
	}
	arg := n.npc.Arg
	limit := strings.Contains(arg, "LIMITSHOP")
	_, hasSellOnly := data.ArgField(arg, "sellonly_msg")
	words := func(key string) bool {
		list, ok := data.ArgField(arg, key)
		if !ok || talk == "" {
			return false
		}
		for _, w := range strings.Split(list, ",") {
			if w != "" && strings.Contains(talk, w) {
				return true
			}
		}
		return false
	}
	switch {
	case words("buy_msg"):
		if !limit {
			w.shopBuyList(p, n)
		} else if hasSellOnly {
			w.shopLimit(p, n)
		}
		return
	case words("sell_msg"):
		w.shopSellList(p, n)
		return
	case words("other_msg"):
		if hint, ok := data.ArgField(arg, "hint_msg"); ok {
			w.npcSay(p, n, hint)
		}
		return
	case talk != "":
		return // 原版说了其它话也会走到下面开选单；这里只有点选店员才开，免得附近聊天一直弹窗
	}
	w.shopOpen(p, n)
}

// shopOpen 是 NPC_ItemShopTalked 末尾按店的种类打开的第一个窗口。
func (w *World) shopOpen(p, n *entity) {
	arg := n.npc.Arg
	limit, event := strings.Contains(arg, "LIMITSHOP"), strings.Contains(arg, "EVENT")
	sellOnly, hasSellOnly := data.ArgField(arg, "sellonly_msg")
	switch {
	case event:
		if hasSellOnly {
			w.npcSay(p, n, sellOnly)
		}
	case limit:
		if hasSellOnly {
			w.shopLimit(p, n)
		}
	case strings.Contains(arg, "EXPRESS"):
		main, _ := data.ArgField(arg, "main_msg")
		// NPC_ExpressmanCheck：main_msg 都有一个换行，所以"打工"（买）是第 2 个可选行、"交付行李"（卖）是第 4 个
		w.sendWindow(p, n, "select", BtnCancel, seqShopExpress,
			"4\n　　　　　　　"+n.name+"\n\n"+npcText(main)+"\n\n　　　　　＜  打工  ＞　　　\n\n　　　　  ＜交付行李＞")
	default:
		main, _ := data.ArgField(arg, "main_msg")
		w.sendShop(p, n, &proto.ShopWindow{Seq: seqShopStart, Mode: "menu", Title: n.name, Msg: npcText(main)})
	}
}

// shopLimit 是 NPC_LimitItemShop：只收购的店，问要不要卖。
func (w *World) shopLimit(p, n *entity) {
	msg, _ := data.ArgField(n.npc.Arg, "sellonly_msg")
	w.sendWindow(p, n, "message", BtnYesNo, seqShopLimit, "\n\n"+npcText(msg))
}

func (w *World) sendShop(p, n *entity, m *proto.ShopWindow) {
	m.ID = n.id
	m.MaxGold = maxGold(p.char.Trans)
	m.Fame = p.char.Fame / 100 // 原版选单与清单都会带上声望（_NEW_MANOR_LAW 的客户端显示）
	if m.Items == nil {
		m.Items = []proto.ShopItem{}
	}
	p.window = &npcWindow{npc: n.id, seq: m.Seq, kind: "shop"}
	p.client.Send(m)
}

// shopGoods 是 NPC_GetItemList：ItemList 以 "," 分开，可写 "起-迄"；不存在的道具略过，最多 32 项。
// 价钱 = 原价（或 ChangeItemCost 的对应项）× buy_rate；有 CostFame 的店每项另要声望（原版内部值，显示时 ÷100）。
func (w *World) shopGoods(arg string) []shopGood {
	rate := 1.0
	if r, ok := data.ArgField(arg, "buy_rate"); ok {
		rate = atof(r)
	}
	fame, hasFame := data.ArgField(arg, "CostFame")
	cost, hasCost := data.ArgField(arg, "ChangeItemCost")
	fames, costs := strings.Split(fame, ","), strings.Split(cost, ",")
	list, _ := data.ArgField(arg, "ItemList")
	var out []shopGood
	curFame, curCost := 0, 0
	for i, s := range strings.Split(list, ",") {
		// 原版逐项读 CostFame、ChangeItemCost 的第 i 项，读不到就沿用上一项
		if hasFame && i < len(fames) {
			curFame = data.Atoi(fames[i])
		}
		if hasCost && i < len(costs) {
			curCost = data.Atoi(costs[i])
		}
		lo, hi, ok := idRange(s)
		if !ok {
			continue
		}
		for id := lo; id <= hi; id++ {
			def := w.game.Items[id]
			if def == nil {
				continue
			}
			if len(out) == maxShopItems {
				return out
			}
			price := def.Cost
			if hasCost {
				price = curCost
			}
			g := shopGood{def: def, price: int(float64(price) * rate), fame: -1}
			if hasFame {
				g.fame = curFame
			}
			out = append(out, g)
		}
	}
	return out
}

type shopGood struct {
	def   *data.ItemDef
	price int
	fame  int // 声望价（CostFame，内部值），-1 表示没有
}

// shopBuyList 是 NPC_ItemShop_BuyMain：店名、main_msg、buy_main 与各项提示文字，加上货品清单。
func (w *World) shopBuyList(p, n *entity) {
	arg := n.npc.Arg
	f := func(k string) string { v, _ := data.ArgField(arg, k); return npcText(v) }
	m := &proto.ShopWindow{Seq: seqShopBuy, Mode: "buy", Title: n.name, Msg: f("main_msg"), Main: f("buy_main"),
		What: f("what_msg"), Level: f("level_msg"), Realy: f("realy_msg"), Full: f("itemfull_msg")}
	for _, g := range w.shopGoods(arg) {
		fame := -1
		if g.fame >= 0 {
			fame = g.fame / 100
		}
		m.Items = append(m.Items, proto.ShopItem{Name: g.def.Name, Level: g.def.Level, Price: g.price, Image: g.def.Image,
			Info: g.def.Memo, Fame: fame})
	}
	w.sendShop(p, n, m)
}

// sellPrice 是 NPC_GetLimtItemList + NPC_GetSellItemList：符合 LimitItemType / LimitItemNo 的才收（ok），
// 价钱 = 原价 × special_rate（special_item 里的道具，没写 special_rate 为 1.2）或 sell_rate；都没有时为 -1（不收）。
func (w *World) sellPrice(arg string, def *data.ItemDef) (price int, ok bool) {
	if types, has := data.ArgField(arg, "LimitItemType"); has {
		for _, t := range strings.Split(types, ",") {
			if v, known := shopTypes[t]; known && shopTypeMatch(v, def.Type) {
				ok = true
				break
			}
		}
	}
	if nos, has := data.ArgField(arg, "LimitItemNo"); has && !ok {
		for _, s := range strings.Split(nos, ",") {
			if lo, hi, valid := idRange(s); valid && lo <= def.ID && def.ID <= hi {
				ok = true
				break
			}
		}
	}
	if special, has := data.ArgField(arg, "special_item"); has {
		rate := 1.2
		if r, has := data.ArgField(arg, "special_rate"); has {
			rate = atof(r)
		}
		for _, s := range strings.Split(special, ",") {
			if lo, hi, valid := idRange(s); valid && lo <= def.ID && def.ID <= hi {
				return int(float64(def.Cost) * rate), ok
			}
		}
	}
	if r, has := data.ArgField(arg, "sell_rate"); has {
		return int(float64(def.Cost) * atof(r)), ok
	}
	return -1, ok
}

// shopSellList 是 NPC_ItemShop_SellMain：身上（道具栏）的道具，店里不收的标成灰色。
func (w *World) shopSellList(p, n *entity) {
	arg := n.npc.Arg
	f := func(k string) string { v, _ := data.ArgField(arg, k); return npcText(v) }
	realy := "realy_msg"
	if strings.Contains(arg, "EXPRESS") {
		realy = "exrealy_msg"
	}
	m := &proto.ShopWindow{Seq: seqShopSell, Mode: "sell", Title: n.name, Msg: f("main_msg"), Main: f("sell_main"),
		Stone: f("stone_msg"), Realy: f(realy), SellOnly: strings.Contains(arg, "LIMITSHOP")}
	for i := data.EquipSlots; i < data.ItemSlots; i++ {
		it := p.inv[i]
		if it == nil {
			continue
		}
		def := w.game.Items[it.ID]
		if def == nil {
			continue
		}
		price, ok := w.sellPrice(arg, def)
		m.Items = append(m.Items, proto.ShopItem{Name: def.Name, Level: def.Level, Price: price, Image: def.Image,
			Info: def.Memo, Sealed: !ok || price < 0, Slot: i, Pile: it.count(), Fame: -1})
	}
	w.sendShop(p, n, m)
}

// shopReply 是 NPC_ItemShopWindowTalked。离店员超过 3 格不处理。
func (w *World) shopReply(p, n *entity, seq, button int, sel string) {
	if max(abs(n.x-p.x), abs(n.y-p.y)) > 3 {
		return
	}
	switch seq {
	case seqShopStart:
		switch data.Atoi(sel) {
		case 1:
			w.shopBuyList(p, n)
		case 2:
			w.shopSellList(p, n)
		}
	case seqShopBuy:
		if w.shopBuy(p, n, sel) {
			w.shopBuyList(p, n)
		} else {
			w.shopOpen(p, n)
		}
	case seqShopSell:
		if data.Atoi(sel) == 0 { // 回选单（LIMITSHOP 是离开）
			if !strings.Contains(n.npc.Arg, "LIMITSHOP") {
				w.shopOpen(p, n)
			}
			return
		}
		if w.shopSell(p, n, sel) {
			w.shopSellList(p, n)
		} else {
			w.sendWindow(p, n, "message", BtnOK, seqShopLimit, "\n\n哎呀!对不起\n\n对不起啊 ! 可不可以再选一次呢？")
		}
	case seqShopLimit:
		if button == BtnYes || button == BtnOK {
			w.shopSellList(p, n)
		}
	case seqShopExpress:
		switch data.Atoi(sel) {
		case 2:
			w.shopBuyList(p, n)
		case 4:
			w.shopSellList(p, n)
		}
	}
}

// shopBuy 是 NPC_SetNewItem + NPC_AddItemBuy："第几项|个数"；个数超过道具栏空格时按空格数买，钱不够或没有空格就失败。
func (w *World) shopBuy(p, n *entity, sel string) bool {
	a, b, _ := strings.Cut(sel, "|")
	idx, count := data.Atoi(a), data.Atoi(b)
	goods := w.shopGoods(n.npc.Arg)
	if idx < 1 || idx > len(goods) || count <= 0 {
		return false
	}
	free := 0
	for i := data.EquipSlots; i < data.ItemSlots; i++ {
		if p.inv[i] == nil {
			free++
		}
	}
	count = min(count, free)
	g := goods[idx-1]
	total, fame := g.price*count, max(0, g.fame)*count
	if count == 0 || p.char.Gold < total || p.char.Fame < fame {
		return false
	}
	for range count {
		if w.addItem(p.inv, w.newItem(g.def)) < 0 {
			break
		}
	}
	p.char.Gold -= total
	w.manorTax(n, total) // addNpcFamilyTax：庄园的家族另外得到 10%
	if fame > 0 {
		p.char.Fame -= fame
		w.sysMsg(p, fmt.Sprintf("成功扣除声望:%dＦ", fame/100), colorYellow)
	}
	w.itemsChanged(p)
	return true
}

// shopSell 是 NPC_SellNewItem："格位|个数"（客户端 _ITEM_PILENUMS 会带个数，没带时是 1）。店里不收、卖了会超过金钱上限就失败。
func (w *World) shopSell(p, n *entity, sel string) bool {
	a, b, hasCount := strings.Cut(sel, "|")
	slot, count := data.Atoi(a), 1
	if hasCount {
		count = data.Atoi(b)
	}
	if slot < data.EquipSlots || slot >= data.ItemSlots || p.inv[slot] == nil || count <= 0 {
		return false
	}
	it := p.inv[slot]
	def := w.game.Items[it.ID]
	if def == nil || count > it.count() {
		return false
	}
	price, ok := w.sellPrice(n.npc.Arg, def)
	if !ok || price < 0 || price*count+p.char.Gold >= maxGold(p.char.Trans) {
		return false
	}
	if it.count() > count {
		it.Pile = it.count() - count
	} else {
		p.inv[slot] = nil
	}
	p.char.Gold += price * count
	w.itemsChanged(p)
	return true
}

// maxPersonalFame 是 MAX_PERSONALFAME（内部值）。
const maxPersonalFame = 100_000_000

// earnFame 是 CHAR_earnFame（_PERSONAL_FAME、_NEW_MANOR_LAW）：声望与个人气势一起加减（内部值），限制在 0..上限并存档，
// 再更新家族名单上的数字。来源是家族冒险（第一次做完事件时 AddFMAdv）与饲育（宠物 30 级以上升级，petFeed）。
// 不能在拿着家族锁的时候调用。
func (w *World) earnFame(p *entity, fame int) {
	p.char.Fame = min(maxPersonalFame, max(0, p.char.Fame+fame))
	p.fmc.Momentum = min(maxMomentum, max(0, p.fmc.Momentum+fame))
	p.char.Family = p.fmc.String()
	w.saveProgress(p)
	w.famFame(p, 0, 0)
}

// petFeed 是 CHAR_PetLevelUp 里的"饲育家族声望"：宠物升到 31 级以上的每一级，主人得到 上一级所需经验 ÷ 20000 的声望，
// 正式族人另外加到家族的饲育声望。
func (w *World) petFeed(e *entity, p *Pet, from int) {
	feed := 0
	for lv := from + 1; lv <= p.Level; lv++ {
		if lv > 30 {
			feed += max(0, w.game.Battle.NextLevelExp(lv-2)) / 20000
		}
	}
	if feed > 0 {
		w.earnFame(e, feed)
		w.famFame(e, 0, feed)
	}
}
