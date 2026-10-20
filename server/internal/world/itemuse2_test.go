package world

import (
	"testing"

	"sa/server/internal/proto"
)

// 神秘礼物（ITEM_MysteriousGift）给一只宠物；运气道具设定运气；驱魔中不遇敌；诱魔（原地遇敌）在时钟跳时开打、走一步就停。
func TestItemsBatch2(t *testing.T) {
	w, db, g := setup(t)
	c, cl := petOwner(t, w, db, "gifted", 10, 60)
	id := int(c.ID)
	gift := itemByFunc(t, g, "ITEM_MysteriousGift", "|")
	slot := bagSlot(w, id, gift)
	cl.take()
	w.ItemUse(id, slot, 0)
	if _, ok := find[*proto.Pets](cl.take()); !ok {
		t.Error("神秘礼物应给一只宠物")
	}
	luck := itemByFunc(t, g, "ITEM_Luck", "大吉")
	w.ItemUse(id, bagSlot(w, id, luck), 0)
	w.Do(func() {
		if w.players[id].luck != 5 {
			t.Errorf("大吉应是运气 5：%d", w.players[id].luck)
		}
	})
	no := itemByFunc(t, g, "ITEM_useNoenemy", "")
	w.ItemUse(id, bagSlot(w, id, no), 0)
	w.Do(func() {
		e := w.players[id]
		if e.noenemy != 6 {
			t.Fatalf("驱魔应 6 分钟：%d", e.noenemy)
		}
		e.cep = 1 << 20
		w.placeOnFloor(e, 100, 600, 450, 0)
		w.rollEncounter(e)
		if e.fight != nil {
			t.Error("驱魔中不应遇敌")
		}
		e.noenemy = 0
	})
	enc := itemByFunc(t, g, "ITEM_useEncounter", "")
	w.ItemUse(id, bagSlot(w, id, enc), 0)
	cl.take()
	w.Do(func() {
		e := w.players[id]
		if !e.stay {
			t.Fatal("诱魔应原地遇敌")
		}
		w.tickPlayers()
		if e.fight == nil {
			t.Error("时钟跳时应开打")
		}
	})
}
