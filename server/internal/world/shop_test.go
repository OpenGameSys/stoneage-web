package world

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 道具店（ss_1001_17_13：buy_rate 1.0、sell_rate 0.2、LimitItemType:OFFENCE）：
// 选单 → 买的清单 → 买 2 个（扣钱、进道具栏）→ 卖掉一个（得原价两成）；钱不够买不了；店里不收的道具卖不掉。
func TestItemShop(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "ItemShop", "ss_1001_17_13")
	c, cl := besideNPC(t, w, db, "shopper", n)
	id := int(c.ID)
	w.Do(func() { w.players[id].char.Gold = 1000 })
	w.Talk(id, n.id)
	menu, ok := find[*proto.ShopWindow](cl.take())
	if !ok || menu.Mode != "menu" || menu.Title != n.name || menu.Msg != "欢迎光临" {
		t.Fatalf("应打开买卖选单：%+v", menu)
	}
	w.NpcWindowReply(id, n.id, menu.Seq, 0, "1")
	buy, ok := find[*proto.ShopWindow](cl.take())
	if !ok || buy.Mode != "buy" || len(buy.Items) == 0 || buy.Main != "每一个都是上等货。" {
		t.Fatalf("应打开买的清单：%+v", buy)
	}
	first := buy.Items[0]
	def := g.Items[0]
	if first.Name != def.Name || first.Price != def.Cost {
		t.Fatalf("第一项应是 0 号道具、原价：%+v vs %+v", first, def)
	}
	w.NpcWindowReply(id, n.id, buy.Seq, 0, "1|2")
	msgs := cl.take()
	inv, ok := find[*proto.Inventory](msgs)
	if again, _ := find[*proto.ShopWindow](msgs); !ok || inv.Gold != 1000-2*first.Price || again == nil || again.Mode != "buy" {
		t.Fatalf("买 2 个应扣 %d 石币并回到清单：%+v", 2*first.Price, msgs)
	}
	var have int
	w.Do(func() { have = w.countItem(w.players[id], 0) })
	if have != 2 {
		t.Fatalf("道具栏应有 2 个：%d", have)
	}
	// 钱不够：回到选单
	w.Do(func() { w.players[id].char.Gold = 0 })
	w.NpcWindowReply(id, n.id, again(t, cl, w, id, n).Seq, 0, "1|1")
	if m, ok := find[*proto.ShopWindow](cl.take()); !ok || m.Mode != "menu" {
		t.Fatalf("钱不够应回到选单：%+v", m)
	}
	// 卖：武器（OFFENCE）才收
	w.NpcWindowReply(id, n.id, menu.Seq, 0, "2")
	sell, ok := find[*proto.ShopWindow](cl.take())
	if !ok || sell.Mode != "sell" || len(sell.Items) != 2 {
		t.Fatalf("卖的清单应列出身上的道具：%+v", sell)
	}
	it := sell.Items[0]
	offence := def.Type <= data.ItemBow || def.Type >= data.ItemBoomerang && def.Type <= data.ItemBreakThrow
	if it.Sealed == offence || it.Price != int(float64(def.Cost)*0.2) || it.Pile != 1 {
		t.Fatalf("卖价应为原价两成、只收武器：%+v（种类 %d）", it, def.Type)
	}
	w.NpcWindowReply(id, n.id, sell.Seq, 0, itoa(it.Slot)+"|1")
	msgs = cl.take()
	if offence {
		if inv, ok := find[*proto.Inventory](msgs); !ok || inv.Gold != it.Price {
			t.Fatalf("卖掉一个应得 %d：%+v", it.Price, msgs)
		}
	} else if win, ok := find[*proto.NpcWindow](msgs); !ok || !strings.Contains(win.Text, "对不起") {
		t.Fatalf("不收的道具应说对不起：%+v", msgs)
	}
	// 卖了会超过金钱上限就不收
	w.Do(func() { w.players[id].char.Gold = maxGold(0) - 1 })
	w.Talk(id, n.id)
	menu, _ = find[*proto.ShopWindow](cl.take())
	w.NpcWindowReply(id, n.id, menu.Seq, 0, "2")
	sell, _ = find[*proto.ShopWindow](cl.take())
	w.NpcWindowReply(id, n.id, sell.Seq, 0, itoa(sell.Items[0].Slot)+"|1")
	if win, ok := find[*proto.NpcWindow](cl.take()); !ok || !strings.Contains(win.Text, "对不起") {
		t.Fatal("超过金钱上限不应卖出")
	}
}

// again 重新打开买的清单（回到选单后再选"买"）。
func again(t *testing.T, cl *fakeClient, w *World, id int, n *entity) *proto.ShopWindow {
	t.Helper()
	w.Talk(id, n.id)
	menu, _ := find[*proto.ShopWindow](cl.take())
	w.NpcWindowReply(id, n.id, menu.Seq, 0, "1")
	buy, ok := find[*proto.ShopWindow](cl.take())
	if !ok {
		t.Fatal("打不开买的清单")
	}
	return buy
}

func itoa(i int) string { return strconv.Itoa(i) }

// 只收购的店（LIMITSHOP，lbis_20802_45_41 收 1234-1249 号的肉）：点选时问要不要卖，按"是"进卖的清单；
// 对着它说 buy_msg 的词也一样；"0"（离开）不回选单。
func TestLimitShop(t *testing.T) {
	w, db, _ := setup(t)
	n := findNPC(t, w, "ItemShop", "lbis_20802_45_41")
	c, cl := besideNPC(t, w, db, "butcher", n)
	id := int(c.ID)
	giveItem(w, id, 1234)
	w.Talk(id, n.id)
	win, ok := find[*proto.NpcWindow](cl.take())
	if !ok || win.Buttons != BtnYesNo || !strings.Contains(win.Text, "吊唁") {
		t.Fatalf("应问要不要卖：%+v", win)
	}
	w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
	sell, ok := find[*proto.ShopWindow](cl.take())
	if !ok || sell.Mode != "sell" || !sell.SellOnly || len(sell.Items) != 1 || sell.Items[0].Sealed {
		t.Fatalf("应进卖的清单、肉可以卖：%+v", sell)
	}
	w.NpcWindowReply(id, n.id, sell.Seq, 0, "0")
	if _, ok := find[*proto.ShopWindow](cl.take()); ok {
		t.Fatal("只收购的店按离开不应回选单")
	}
	w.Chat(id, "near", "我想卖东西", 0, 3)
	if s, ok := find[*proto.ShopWindow](cl.take()); !ok || s.Mode != "sell" {
		t.Fatalf("说 sell_msg 的词应直接进卖的清单：%+v", s)
	}
}

// 每一家道具店都点一遍、买第一项、卖第一项：不应出错。
func TestShopSweep(t *testing.T) {
	w, db, _ := setup(t)
	var shops []*entity
	w.Do(func() {
		for _, es := range w.floors {
			for _, e := range es {
				if e.npc != nil && e.npc.Function == "ItemShop" {
					shops = append(shops, e)
				}
			}
		}
	})
	c, cl := besideNPC(t, w, db, "sweeper", shops[0])
	id := int(c.ID)
	opened := 0
	for _, n := range shops {
		w.Do(func() {
			e := w.players[id]
			e.char.Gold = 100000
			w.placeOnFloor(e, n.floor, n.x+1, n.y, 2)
		})
		cl.take()
		w.Talk(id, n.id)
		for _, reply := range []string{"1", "1|1", "2", "10|1", "0"} {
			msgs := cl.take()
			if s, ok := find[*proto.ShopWindow](msgs); ok {
				opened++
				w.NpcWindowReply(id, n.id, s.Seq, 0, reply)
			} else if win, ok := find[*proto.NpcWindow](msgs); ok {
				w.NpcWindowReply(id, n.id, win.Seq, BtnYes, reply)
			}
		}
	}
	if opened == 0 {
		t.Fatal("应打开一些商店窗口")
	}
	t.Logf("%d 家店，打开 %d 次商店窗口", len(shops), opened)
}

// 声望价的店（family/npc/shop1_3：1345 号要 30000 石币 + 声望 400（显示 4Ｆ））：声望不够买不了，够了扣钱扣声望。
func TestShopFame(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "ItemShop", "family/npc/shop1_3")
	if g.Items[1345] == nil {
		t.Skip("没有 1345 号道具")
	}
	c, cl := besideNPC(t, w, db, "famous", n)
	id := int(c.ID)
	w.Do(func() { e := w.players[id]; e.char.Gold, e.char.Fame = 100000, 399 })
	open := func() *proto.ShopWindow {
		w.Talk(id, n.id)
		menu, _ := find[*proto.ShopWindow](cl.take())
		w.NpcWindowReply(id, n.id, menu.Seq, 0, "1")
		buy, ok := find[*proto.ShopWindow](cl.take())
		if !ok || buy.Mode != "buy" {
			t.Fatalf("应打开买的清单：%+v", buy)
		}
		return buy
	}
	buy := open()
	if it := buy.Items[0]; it.Price != 30000 || it.Fame != 4 || buy.Fame != 3 {
		t.Fatalf("第一项应是 30000 石币、4Ｆ，身上 3Ｆ：%+v 声望 %d", it, buy.Fame)
	}
	w.NpcWindowReply(id, n.id, buy.Seq, 0, "1|1")
	if m, _ := find[*proto.ShopWindow](cl.take()); m == nil || m.Mode != "menu" {
		t.Fatal("声望不够应买不了（回到选单）")
	}
	w.Do(func() { w.players[id].char.Fame = 1000 })
	buy = open()
	w.NpcWindowReply(id, n.id, buy.Seq, 0, "1|1")
	msgs := cl.take()
	var gold, fame int
	w.Do(func() { e := w.players[id]; gold, fame = e.char.Gold, e.char.Fame })
	if gold != 70000 || fame != 600 {
		t.Fatalf("应扣 30000 石币与 400 声望：金钱 %d 声望 %d %+v", gold, fame, msgs)
	}
	if m, ok := find[*proto.ChatMsg](msgs); !ok || m.Text != "成功扣除声望:4Ｆ" {
		t.Fatalf("应提示扣除声望：%+v", msgs)
	}
}

// 长毛象快递（EXPRESS）：选单第 2 行"打工"进买、第 4 行"交付行李"进卖（main_msg 有换行，与原版回传一致）。
func TestShopExpress(t *testing.T) {
	w, db, _ := setup(t)
	n := findNPC(t, w, "ItemShop", "ss_1011_13_13")
	c, cl := besideNPC(t, w, db, "porter", n)
	id := int(c.ID)
	w.Talk(id, n.id)
	win, ok := find[*proto.NpcWindow](cl.take())
	if !ok || win.Kind != "select" {
		t.Fatalf("应显示快递选单：%+v", win)
	}
	// 用客户端的算法数出"打工""交付行李"是第几个可选行
	lines := strings.Split(win.Text, "\n")[1:]
	row := func(s string) int {
		for i, l := range lines {
			if strings.Contains(l, s) {
				return i - 4 + 1
			}
		}
		return -1
	}
	if row("＜  打工  ＞") != 2 || row("＜交付行李＞") != 4 {
		t.Fatalf("打工、交付行李应是第 2、4 个可选行：%d %d\n%s", row("＜  打工  ＞"), row("＜交付行李＞"), win.Text)
	}
	w.NpcWindowReply(id, n.id, win.Seq, 0, "4")
	if s, ok := find[*proto.ShopWindow](cl.take()); !ok || s.Mode != "sell" {
		t.Fatalf("交付行李应进卖的清单：%+v", s)
	}
}

// 寄放道具的店（pis_1009_14_13：每件 50 石币）：寄放 → 道具栏少一件、扣钱、寄放清单多一件；不能寄放的不收；取回 → 回到道具栏。
func TestPoolItemShop(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "PoolItemShop", "pis_1009_14_13")
	c, cl := besideNPC(t, w, db, "keeper", n)
	id := int(c.ID)
	giveItem(w, id, 0)
	w.Do(func() { w.players[id].char.Gold = 100 })
	menu := func(choice string) []proto.Message {
		w.Talk(id, n.id)
		win, ok := find[*proto.NpcWindow](cl.take())
		if !ok || win.Kind != "select" || !strings.Contains(win.Text, "寄放道具") {
			t.Fatalf("应显示寄放选单：%+v", win)
		}
		w.NpcWindowReply(id, n.id, win.Seq, 0, choice)
		return cl.take()
	}
	pool, ok := find[*proto.ShopWindow](menu("1"))
	if !ok || pool.Mode != "pool" || pool.Rest != 30 || len(pool.Items) != 1 || pool.Items[0].Price != 50 {
		t.Fatalf("寄放清单应列出身上的道具、每件 50：%+v", pool)
	}
	it := pool.Items[0]
	if it.Sealed != !poolable(g.Items[0]) {
		t.Fatalf("能不能寄放应照道具的旗标：%+v", it)
	}
	w.NpcWindowReply(id, n.id, pool.Seq, 0, itoa(it.Slot))
	msgs := cl.take()
	var bag, gold int
	var saved string
	w.Do(func() { e := w.players[id]; bag, gold, saved = w.countItem(e, 0), e.char.Gold, e.char.Pool })
	if bag != 0 || gold != 50 || len(parsePool(saved)) != 1 {
		t.Fatalf("寄放后道具栏应少一件、扣 50：道具 %d 金钱 %d 寄放 %s %+v", bag, gold, saved, msgs)
	}
	if again, ok := find[*proto.ShopWindow](msgs); !ok || again.Rest != 29 || len(again.Items) != 0 {
		t.Fatalf("应重发寄放清单：%+v", again)
	}
	draw, ok := find[*proto.ShopWindow](menu("2"))
	if !ok || draw.Mode != "draw" || len(draw.Items) != 1 || draw.Items[0].Name != g.Items[0].Name {
		t.Fatalf("取回清单应列出寄放的道具：%+v", draw)
	}
	w.NpcWindowReply(id, n.id, draw.Seq, 0, "1")
	cl.take()
	w.Do(func() { e := w.players[id]; bag, saved = w.countItem(e, 0), e.char.Pool })
	if bag != 1 || saved != "" {
		t.Fatalf("取回后应回到道具栏：道具 %d 寄放 %q", bag, saved)
	}
	// 存档
	time.Sleep(50 * time.Millisecond)
	if s, _ := db.GetCharacter(context.Background(), c.AccountID, c.ID); s.Gold != 50 || s.Pool != "" {
		t.Fatalf("应存档：%+v", s)
	}
}
