package world

import (
	"strings"
	"testing"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 交易安全：放上桌的道具在对方锁定后被动了（卖掉 / 寄放），确定时整笔不换；交易中不能找 NPC、不能捡东西。
func TestTradeIntactAndBusy(t *testing.T) {
	w, db, g := setup(t)
	ca, cla := petOwner(t, w, db, "卖方", 10, 60)
	cb, clb := petOwner(t, w, db, "买方", 10, 60)
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
		ea.char.Gold, eb.char.Gold = 0, 500
	})
	w.SetFlags(b, fsParty|fsTrade)
	w.Trade(a, "request", 0, 0)
	w.Trade(a, "item", bag, 0)
	w.Trade(b, "gold", 0, 300)
	w.Trade(a, "lock", 0, 0)
	w.Trade(b, "lock", 0, 0)
	cla.take()
	clb.take()
	// 交易中不能对 NPC 说话、不能捡东西（原版 CHAR_WORKTRADEMODE）
	w.Do(func() {
		ea := w.players[a]
		if !ea.busy() {
			t.Fatal("交易中应算 busy")
		}
		ea.inv[bag] = nil // 模拟桌上的道具没了（例如卖给了 NPC）
	})
	w.Trade(a, "ok", 0, 0)
	w.Trade(b, "ok", 0, 0)
	w.Do(func() {
		ea, eb := w.players[a], w.players[b]
		if eb.char.Gold != 500 || ea.char.Gold != 0 {
			t.Errorf("桌上的东西不见了就不应成交：卖方 %d 买方 %d", ea.char.Gold, eb.char.Gold)
		}
		if ea.trade != nil || eb.trade != nil {
			t.Error("交易应结束")
		}
	})
	if !strings.Contains(fmtMsgs(clb.take()), "交易失败") {
		t.Error("买方应看到交易失败")
	}
}

// 交易把骑着的宠物换掉时 Ride 要清掉（否则换来的宠物落在同一格就变成"骑着"它）。
func TestTradeTakePetClearsRide(t *testing.T) {
	w, db, g := setup(t)
	c, _ := petOwner(t, w, db, "骑士", 10, 60)
	id := int(c.ID)
	slot := givePet(t, w, id, petEnemy(t, g, 1), 5)
	w.Do(func() {
		e := w.players[id]
		e.pets.Ride = slot
		pets := w.tradeTakePets(&tradeSide{e: e, pets: []int{slot}})
		if len(pets) != 1 || e.pets.Ride != -1 {
			t.Errorf("拿走骑着的宠物后 Ride 应清掉：%d", e.pets.Ride)
		}
	})
}

// 摆摊安全：摆着时不能动道具 / 找 NPC / 骑宠；买的时候核对开张时记下的道具（编号与个数）。
func TestVendorSnapshot(t *testing.T) {
	w, db, g := setup(t)
	s, cls := petOwner(t, w, db, "摊主", 10, 60)
	b, _ := petOwner(t, w, db, "顾客", 10, 60)
	sid, bid := int(s.ID), int(b.ID)
	itemID, otherID := 0, 0
	for id, def := range g.Items {
		if !def.VanishDrop && def.MaxPile <= 1 && def.Type == data.ItemOther && def.Name != "" {
			if itemID == 0 {
				itemID = id
			} else if otherID == 0 && id != itemID {
				otherID = id
			}
		}
	}
	var slot, other int
	w.Do(func() {
		se, be := w.players[sid], w.players[bid]
		slot = w.addItem(se.inv, &Item{ID: itemID})
		other = w.addItem(se.inv, &Item{ID: otherID})
		be.char.Gold = 1000
		w.placeOnFloor(be, se.floor, se.x+1, se.y, 1)
	})
	w.Vendor(sid, "set", "", []int{0}, []int{slot}, []int{100})
	cls.take()
	w.ItemMove(sid, other, slot) // 摆着时不能动道具
	w.Do(func() {
		se := w.players[sid]
		if !se.busy() || se.inv[slot] == nil || se.inv[slot].ID != itemID {
			t.Fatalf("摆着时 ItemMove 应被挡：%v", se.inv[slot])
		}
		// 硬把那一格换成别的东西：买的时候应核对不过、那项作废
		se.inv[slot] = &Item{ID: otherID}
	})
	w.Vendor(bid, "open", "", nil, nil, nil)
	w.Vendor(bid, "buy", "", nil, []int{0}, nil)
	w.Do(func() {
		se, be := w.players[sid], w.players[bid]
		if be.char.Gold != 1000 || se.inv[slot] == nil {
			t.Errorf("内容和开张时不一样就不应卖出：买方 %d 石币，格子 %v", be.char.Gold, se.inv[slot])
		}
	})
}

// 送信中的宠物不能卖给宠物店、不能寄放。
func TestPetShopRejectsAwayPet(t *testing.T) {
	w, db, g := setup(t)
	c, _ := petOwner(t, w, db, "寄信人", 10, 60)
	id := int(c.ID)
	slot := givePet(t, w, id, petEnemy(t, g, 1), 5)
	n := findNPC(t, w, "PetShop", "")
	w.Do(func() {
		e := w.players[id]
		e.pets.Pets[slot].Away = true
		if msg := w.petSell(e, n, slot); !strings.Contains(msg, "送信中") || e.pets.Pets[slot] == nil {
			t.Errorf("送信中的宠物不应卖掉：%q", msg)
		}
		if msg := w.petPool(e, n, slot); !strings.Contains(msg, "送信中") || e.pets.Pets[slot] == nil {
			t.Errorf("送信中的宠物不应寄放：%q", msg)
		}
	})
}

// 存档队列的合并：同一个 key 连续排 200 笔，只写最新的那一笔（世界协程不会被刷爆的存档卡住）。
func TestSaveQueueCoalesces(t *testing.T) {
	w, _, _ := setup(t)
	ran := make(chan int, 300)
	for i := 0; i < 200; i++ {
		v := i
		w.enqueue("test", func() { ran <- v })
	}
	w.Flush(5e9)
	close(ran)
	var got []int
	for v := range ran {
		got = append(got, v)
	}
	if len(got) < 1 || len(got) > 2 || got[len(got)-1] != 199 {
		t.Fatalf("应只写最新的一笔：%v", got)
	}
	_ = proto.Message(nil)
}
