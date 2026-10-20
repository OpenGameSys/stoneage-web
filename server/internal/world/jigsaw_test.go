package world

import (
	"testing"

	"sa/server/internal/data"
)

// 拼图：拼图放进自己的小底板（改名"小底板Ａ(1)"、拼图用掉），放不进别的底板；小底板拼满才能放进大底板。
func TestJigsaw(t *testing.T) {
	w, db, g := setup(t)
	c, _ := petOwner(t, w, db, "jigsaw", 10, 60)
	id := int(c.ID)
	byImage := map[int]*data.ItemDef{}
	for _, d := range g.Items {
		if d.UseFunc == "ITEM_Jigsaw" {
			byImage[d.Image] = d
		}
	}
	big, boardA, boardB, pieceA1 := byImage[25150], byImage[25151], byImage[25152], byImage[25160]
	if big == nil || boardA == nil || boardB == nil || pieceA1 == nil {
		t.Skip("没有拼图道具")
	}
	bag := data.EquipSlots
	w.Do(func() {
		e := w.players[id]
		e.inv[bag], e.inv[bag+1], e.inv[bag+2], e.inv[bag+3] = w.newItem(boardA), w.newItem(boardB), w.newItem(pieceA1), w.newItem(big)
	})
	w.ItemUse(id, bag+2, bag+1) // 放进 Ｂ：不对
	w.Do(func() {
		if w.players[id].inv[bag+2] == nil {
			t.Fatal("Ａ的拼图不该放进Ｂ底板")
		}
	})
	w.ItemUse(id, bag+2, bag)
	w.Do(func() {
		e := w.players[id]
		if b := e.inv[bag]; e.inv[bag+2] != nil || b.Arg != "100000000" || b.Name != boardA.Name+"(1)" {
			t.Fatalf("应放进Ａ的第一格：%+v", b)
		}
	})
	w.ItemUse(id, bag, bag+3) // 没拼满的小底板放不进大底板
	w.Do(func() {
		e := w.players[id]
		if e.inv[bag] == nil || e.inv[bag+3].Arg != "" {
			t.Fatal("没拼满的小底板不该放进大底板")
		}
		e.inv[bag].Arg = "111111111"
	})
	w.ItemUse(id, bag, bag+3)
	w.Do(func() {
		e := w.players[id]
		if e.inv[bag] != nil || e.inv[bag+3].Arg != "100000000" {
			t.Errorf("拼满的小底板应放进大底板第一格：%+v", e.inv[bag+3])
		}
	})
}
