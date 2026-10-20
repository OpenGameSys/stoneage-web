package world

import (
	"strconv"
	"strings"
	"testing"

	"sa/server/internal/proto"
)

// 宠物店（ps_1003_12_13：pool_flg:1）：卖宠物 → 选宠物 → 价格的是/否 → 卖掉拿钱。
func TestPetShopSell(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "PetShop", "ps_1003_12_13")
	ene := petEnemy(t, g, 1)
	c, cl := besideNPC(t, w, db, "seller", n)
	id := int(c.ID)
	slot := givePet(t, w, id, ene, 10)
	w.PetState(id, slot, "battle")
	cl.take()

	w.Talk(id, n.id)
	win, ok := find[*proto.NpcWindow](cl.take())
	if !ok || win.Kind != "select" || !strings.Contains(win.Text, "卖掉宠物") {
		t.Fatalf("应打开宠物店选单：%+v", win)
	}
	w.NpcWindowReply(id, n.id, win.Seq, 0, "3") // 有寄放的店：第 3 项是卖掉宠物
	win, ok = find[*proto.NpcWindow](cl.take())
	if !ok || win.Kind != "petselect" {
		t.Fatalf("应打开选宠物的窗口：%+v", win)
	}
	w.NpcWindowReply(id, n.id, win.Seq, 0, "1")
	win, ok = find[*proto.NpcWindow](cl.take())
	if !ok || !strings.Contains(win.Text, "买卖价格") {
		t.Fatalf("应显示价格：%+v", win)
	}
	var want int
	w.Do(func() {
		e := w.players[id]
		want = w.petSellCost(e, n, e.pets.Pets[slot])
	})
	if want <= 0 || !strings.Contains(win.Text, " "+strconv.Itoa(want)+" STONE") {
		t.Fatalf("价格应为 %d：%s", want, win.Text)
	}
	w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
	msgs := cl.take()
	if p, ok := find[*proto.Pets](msgs); !ok || len(p.Pets) != 0 || p.Battle != -1 {
		t.Fatalf("宠物应卖掉、出战改为没有：%+v", p)
	}
	if inv, ok := find[*proto.Inventory](msgs); !ok || inv.Gold != want {
		t.Errorf("应拿到 %d 石币：%+v", want, inv)
	}
}

// 寄放与领回：寄放收 50 + 等级×4，领回不收钱；寄放的宠物存在宠物栏的 pool 里。
func TestPetShopPool(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "PetShop", "ps_1003_12_13")
	ene := petEnemy(t, g, 1)
	c, cl := besideNPC(t, w, db, "pooler", n)
	id := int(c.ID)
	givePet(t, w, id, ene, 3)
	w.Do(func() { w.players[id].char.Gold = 1000 })
	cl.take()

	w.Talk(id, n.id)
	win, _ := find[*proto.NpcWindow](cl.take())
	w.NpcWindowReply(id, n.id, win.Seq, 0, "1") // 寄放宠物
	win, _ = find[*proto.NpcWindow](cl.take())
	if win == nil || win.Kind != "petselect" {
		t.Fatalf("应选要寄放的宠物：%+v", win)
	}
	w.NpcWindowReply(id, n.id, win.Seq, 0, "1")
	win, _ = find[*proto.NpcWindow](cl.take())
	if win == nil || !strings.Contains(win.Text, "寄放价格            54 stone") {
		t.Fatalf("1 级角色寄放价格 54：%+v", win)
	}
	w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
	msgs := cl.take()
	if p, _ := find[*proto.Pets](msgs); p == nil || len(p.Pets) != 0 {
		t.Fatalf("宠物应寄放出去：%+v", p)
	}
	w.Do(func() {
		e := w.players[id]
		if len(e.pets.Pool) != 1 || e.char.Gold != 1000-54 {
			t.Errorf("寄放栏 %d 只、石币 %d", len(e.pets.Pool), e.char.Gold)
		}
	})
	end, _ := find[*proto.NpcWindow](msgs)
	// 领回：结束窗口按 OK 回到选单，选第 2 项
	w.NpcWindowReply(id, n.id, end.Seq, BtnOK, "")
	win, _ = find[*proto.NpcWindow](cl.take())
	w.NpcWindowReply(id, n.id, win.Seq, 0, "2")
	win, _ = find[*proto.NpcWindow](cl.take())
	if win == nil || win.Kind != "select" || !strings.Contains(win.Text, "LV.  3") {
		t.Fatalf("应列出寄放的宠物：%+v", win)
	}
	w.NpcWindowReply(id, n.id, win.Seq, 0, "1")
	win, _ = find[*proto.NpcWindow](cl.take())
	if win == nil || !strings.Contains(win.Text, "领回[") {
		t.Fatalf("应问要不要领回：%+v", win)
	}
	w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
	if p, _ := find[*proto.Pets](cl.take()); p == nil || len(p.Pets) != 1 {
		t.Fatalf("应领回宠物：%+v", p)
	}
}

// 宠物技能店（psks_1003_18_13）：列出技能与价格；买了换掉宠物那一格的技能并扣钱。
func TestPetSkillShop(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "PetSkillShop", "psks_1003_18_13")
	ene := petEnemy(t, g, 1)
	c, cl := besideNPC(t, w, db, "trainer", n)
	id := int(c.ID)
	slot := givePet(t, w, id, ene, 3)
	w.Do(func() { w.players[id].char.Gold = 5000 })
	cl.take()

	w.Talk(id, n.id)
	win, ok := find[*proto.NpcWindow](cl.take())
	if !ok || win.Kind != "petskillshop" || !strings.HasPrefix(win.Text, "1|") || !strings.Contains(win.Text, "|攻击|1000|") {
		t.Fatalf("应打开宠物技能店：%+v", win)
	}
	// pet_skill:0,1,2,3,…：第 4 个是破除防御（1500），教到第 2 格
	w.NpcWindowReply(id, n.id, win.Seq, 0, "4|1|2|0")
	msgs := cl.take()
	p, ok := find[*proto.Pets](msgs)
	if !ok || len(p.Pets) != 1 {
		t.Fatal("应更新宠物栏")
	}
	learned := false
	for _, s := range p.Pets[0].Skills {
		learned = learned || s.Slot == 1 && s.Name == "破除防御"
	}
	if !learned {
		t.Errorf("第 2 格应换成破除防御：%+v", p.Pets[0].Skills)
	}
	if inv, _ := find[*proto.Inventory](msgs); inv == nil || inv.Gold != 3500 {
		t.Errorf("应扣 1500 石币：%+v", inv)
	}
	if again, _ := find[*proto.NpcWindow](msgs); again == nil || again.Kind != "petskillshop" {
		t.Error("买完应重新显示窗口")
	}
	// 钱不够不卖
	w.Do(func() { w.players[id].char.Gold = 10 })
	w.NpcWindowReply(id, n.id, win.Seq, 0, "4|1|3|0")
	w.Do(func() {
		if s := w.players[id].pets.Pets[slot].Skills[2]; s == 3 {
			t.Error("钱不够不应学到")
		}
	})
}
