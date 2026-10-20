package world

import (
	"strings"
	"testing"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 交易：对方没开"交易"不行；开了就直接打开；放道具与石币、两边锁定再确定后交换；取消。
func TestTrade(t *testing.T) {
	w, db, g := setup(t)
	ca, cla := petOwner(t, w, db, "seller", 10, 60)
	cb, clb := petOwner(t, w, db, "buyer", 10, 60)
	a, b := int(ca.ID), int(cb.ID)
	var potion *data.ItemDef
	for _, d := range g.Items {
		if d.UseFunc == "ITEM_useRecovery" && !d.VanishDrop && !strings.HasPrefix(d.Name, "*") && (potion == nil || d.ID < potion.ID) {
			potion = d
		}
	}
	bag := data.EquipSlots
	w.Do(func() {
		ea, eb := w.players[a], w.players[b]
		eb.x, eb.y = ea.x+MOVE[ea.dir][0], ea.y+MOVE[ea.dir][1]
		ea.inv[bag] = w.newItem(potion)
		ea.char.Gold, eb.char.Gold = 500, 100
	})
	w.Trade(a, "request", 0, 0)
	if !strings.Contains(fmtMsgs(cla.take()), "真是抱歉，对方不愿意跟你交易！") {
		t.Fatal("对方没开交易应被拒绝")
	}
	w.SetFlags(b, fsParty|fsTrade)
	w.Trade(a, "request", 0, 0)
	if _, ok := find[*proto.TradeOpen](clb.take()); !ok {
		t.Fatal("对方应打开交易窗口")
	}
	w.Trade(a, "item", bag, 0)
	w.Trade(a, "gold", 0, 200)
	if msgs := clb.take(); len(msgs) < 2 {
		t.Fatalf("对方应看到道具与石币：%v", fmtMsgs(msgs))
	}
	w.Trade(a, "ok", 0, 0) // 还没锁定，不算
	w.Trade(a, "lock", 0, 0)
	w.Trade(b, "lock", 0, 0)
	w.Trade(a, "ok", 0, 0)
	w.Trade(b, "ok", 0, 0)
	w.Do(func() {
		ea, eb := w.players[a], w.players[b]
		if ea.trade != nil || eb.trade != nil || ea.inv[bag] != nil || eb.char.Gold != 300 || ea.char.Gold != 300 {
			t.Errorf("应交换完：%v %d %d", ea.inv[bag], ea.char.Gold, eb.char.Gold)
		}
		found := false
		for _, it := range eb.inv {
			found = found || it != nil && it.ID == potion.ID
		}
		if !found {
			t.Error("买家应拿到道具")
		}
	})
	if !strings.Contains(fmtMsgs(cla.take()), "交易ＯＫ！") {
		t.Error("应说交易ＯＫ")
	}
	clb.take()
	// 取消
	w.Trade(a, "request", 0, 0)
	w.Trade(b, "cancel", 0, 0)
	w.Do(func() {
		if w.players[a].trade != nil {
			t.Error("取消后不该还在交易")
		}
	})
	if !strings.Contains(fmtMsgs(cla.take()), "buyer将交易取消了！") {
		t.Error("应说对方取消了")
	}
}
