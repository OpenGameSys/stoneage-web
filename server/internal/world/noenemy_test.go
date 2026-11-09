package world

import (
	"strings"
	"testing"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

func chatTexts(cl *fakeClient) string {
	var b strings.Builder
	for _, m := range cl.take() {
		if c, ok := m.(*proto.ChatMsg); ok {
			b.WriteString(c.Text + "\n")
		}
	}
	return b.String()
}

// walkableAt 找楼层 fl 上第一个能走的格子。
func walkableAt(t *testing.T, g *data.Game, fl int) (int, int) {
	f := g.Floors[fl]
	if f == nil {
		t.Skipf("没有楼层 %d", fl)
	}
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			if g.Tiles.Walkable(f, x, y) {
				return x, y
			}
		}
	}
	t.Skipf("楼层 %d 没有能走的格子", fl)
	return 0, 0
}

func invSlotOf(w *World, id, itemID int) int {
	slot := -1
	w.Do(func() {
		p := w.players[id]
		for i := range p.inv {
			if p.inv[i] != nil && p.inv[i].ID == itemID {
				slot = i
			}
		}
	})
	return slot
}

// 太阳神的首饰 Lv1（18546，noen:40，装备等级 40）：没转生的 1 级装不上，转生过的装得上；在 100 层装上说隐藏行踪、不遇敌，
// 300 层照样遇敌；拿下说光芒消失。月亮首饰（rand:500）装着永远过不了 RAND(0,100) > 500；星星之守护（2068，rand:40）用掉、记下 40。
func TestEquipNoEnemy(t *testing.T) {
	w, db, g := setup(t)
	for _, id := range []int{18546, 20126, 2068} {
		if g.Items[id] == nil {
			t.Skipf("没有道具 %d", id)
		}
	}
	x, y := walkableAt(t, g, 100)
	c := newChar(t, db, "sunamulet", 100, x, y)
	cl := &fakeClient{}
	w.Enter(c, cl)
	id := int(c.ID)
	giveItem(w, id, 18546)
	sun := invSlotOf(w, id, 18546)
	cl.take()

	w.ItemMove(id, sun, -1)
	if invSlotOf(w, id, 18546) != sun {
		t.Fatal("没转生的 1 级人物不该装得上 40 级的首饰")
	}
	w.Do(func() { w.players[id].char.Trans = 1 })
	w.ItemMove(id, sun, -1)
	if s := invSlotOf(w, id, 18546); s >= data.EquipSlots {
		t.Fatalf("转生过的应免等级装上：在格 %d", s)
	}
	if txt := chatTexts(cl); !strings.Contains(txt, "一道奇异的光芒隐藏了你的行踪") {
		t.Fatalf("在 100 层装上应说隐藏行踪：%q", txt)
	}
	w.Do(func() {
		p := w.players[id]
		if !w.encounterBlocked(p) {
			t.Error("100 层装着 Lv1 应不遇敌")
		}
		p.floor = 300
		if w.encounterBlocked(p) {
			t.Error("300 层 Lv1 管不到")
		}
		p.floor = 100
	})
	w.ItemMove(id, invSlotOf(w, id, 18546), -1)
	if txt := chatTexts(cl); !strings.Contains(txt, "环绕着你的光芒消失了") {
		t.Fatalf("拿下应说光芒消失：%q", txt)
	}
	w.Do(func() {
		if w.encounterBlocked(w.players[id]) {
			t.Error("拿下之后应照常遇敌")
		}
	})

	// 月亮首饰：装着 eqRand = 500，永远不遇敌；拿下回复
	giveItem(w, id, 20126)
	w.Do(func() { w.players[id].char.Level = 80 })
	w.ItemMove(id, invSlotOf(w, id, 20126), -1)
	w.Do(func() {
		p := w.players[id]
		if p.eqRand != 500 {
			t.Fatalf("月亮首饰应设 eqRand 500：%d", p.eqRand)
		}
		for i := 0; i < 200; i++ {
			if w.randEnemyPass(p) {
				t.Fatal("rand:500 不该遇敌")
			}
		}
	})
	w.ItemMove(id, invSlotOf(w, id, 20126), -1)
	w.Do(func() {
		if w.players[id].eqRand != 0 {
			t.Fatal("拿下月亮首饰应清掉 eqRand")
		}
	})
	cl.take()

	// 星星之守护：用掉、eqRand 40
	giveItem(w, id, 2068)
	star := invSlotOf(w, id, 2068)
	w.ItemUse(id, star, 0)
	w.Do(func() {
		p := w.players[id]
		if p.eqRand != 40 || p.inv[star] != nil {
			t.Fatalf("星星之守护应用掉并设 40：eqRand=%d 道具=%v", p.eqRand, p.inv[star])
		}
	})
	if txt := chatTexts(cl); !strings.Contains(txt, "遇敌率降低了") {
		t.Fatalf("应说遇敌率降低了：%q", txt)
	}
}

// 上线时装着太阳神的首饰也生效（CHAR_login 执行 attachfunc）。
func TestEquipNoEnemyLogin(t *testing.T) {
	w, db, g := setup(t)
	if g.Items[18548] == nil {
		t.Skip("没有道具 18548")
	}
	x, y := walkableAt(t, g, 500)
	c := newChar(t, db, "sunlogin", 500, x, y)
	inv := parseInventory(c.Items)
	inv[data.SlotDeco1] = w.newItem(g.Items[18548])
	c.Items = inv.String()
	cl := &fakeClient{}
	w.Enter(c, cl)
	if txt := chatTexts(cl); !strings.Contains(txt, "一道奇异的光芒隐藏了你的行踪") {
		t.Fatalf("上线时在 500 层装着 Lv3 应说隐藏行踪：%q", txt)
	}
	w.Do(func() {
		if !w.encounterBlocked(w.players[int(c.ID)]) {
			t.Error("500 层装着 Lv3 应不遇敌")
		}
	})
}
