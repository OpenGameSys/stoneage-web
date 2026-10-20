package world

import (
	"strings"
	"testing"
	"time"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// groundPair 建两个在同一处的角色：a 面向一格能走的地方，b 站在那一格的旁边。
func groundPair(t *testing.T) (*World, *data.Game, int, int, *fakeClient, *fakeClient, [2]int) {
	t.Helper()
	w, db, g := setup(t)
	ca, cla := petOwner(t, w, db, "dropper", 30, 60)
	cb, clb := petOwner(t, w, db, "picker", 30, 60)
	a, b := int(ca.ID), int(cb.ID)
	var tile [2]int
	w.Do(func() {
		e := w.players[a]
		f := w.game.Floors[e.floor]
		for d, m := range MOVE {
			x, y := e.x+m[0], e.y+m[1]
			if w.game.Tiles.Walkable(f, x, y) {
				e.dir, tile = d, [2]int{x, y}
				break
			}
		}
		// b 站在 a 的脚下，面向那一格
		pb := w.players[b]
		pb.x, pb.y, pb.dir = e.x, e.y, e.dir
		w.refreshView(pb)
		w.refreshView(e)
	})
	cla.take()
	clb.take()
	return w, g, a, b, cla, clb, tile
}

// 丢道具（CHAR_DropItem）：放在面前那格，附近的人看得到；别人能捡（ITEMUP=1），捡到说"拾获"；20 秒后没人捡就消失。
func TestGroundItem(t *testing.T) {
	w, _, a, b, _, clb, tile := groundPair(t)
	giveItem(w, a, 13088)
	slot := -1
	w.Do(func() {
		for i, it := range w.players[a].inv {
			if it != nil && it.ID == 13088 {
				slot = i
			}
		}
	})
	w.ItemDrop(a, slot)
	app, ok := find[*proto.ObjAppear](clb.take())
	if !ok || app.Objs[0].Kind != "item" || app.Objs[0].X != tile[0] || app.Objs[0].Y != tile[1] {
		t.Fatalf("道具应放在面前那格：%+v %v", app, tile)
	}
	w.Do(func() {
		if w.players[a].inv[slot] != nil {
			t.Error("丢了的道具应离开道具栏")
		}
	})
	var dir int
	w.Do(func() { dir = w.players[b].dir })
	w.PickUp(b, dir)
	if !strings.Contains(fmtMsgs(clb.take()), "拾获 ") {
		t.Fatal("别人也能捡")
	}
	w.Do(func() {
		got := false
		for _, it := range w.players[b].inv {
			got = got || it != nil && it.ID == 13088
		}
		if !got {
			t.Error("捡到的道具应进道具栏")
		}
	})
	// 放太久就消失
	giveItem(w, a, 13088)
	w.ItemDrop(a, slot)
	clb.take()
	w.Do(func() {
		start := time.Now()
		w.now = func() time.Time { return start.Add(itemDeleteTime + time.Second) }
		w.sweepGround()
	})
	if _, ok := find[*proto.ObjDisappear](clb.take()); !ok {
		t.Error("放了 20 秒的道具应消失")
	}
}

// 丢石币（CHAR_DropMoney）：同一格合起来；捡起来加到身上。
func TestGroundGold(t *testing.T) {
	w, _, a, b, _, clb, _ := groundPair(t)
	w.Do(func() { w.players[a].char.Gold = 1000 })
	w.GoldDrop(a, 300)
	w.GoldDrop(a, 200)
	w.GoldDrop(a, 5000) // 不够就不丢
	n := 0
	w.Do(func() {
		for _, o := range w.floors[w.players[a].floor] {
			if o.kind == "gold" {
				n++
				if o.ground.gold != 500 || o.name != "500 Stone" {
					t.Errorf("同一格的石币应合起来：%+v", o.ground)
				}
			}
		}
		if w.players[a].char.Gold != 500 {
			t.Errorf("身上应剩 500：%d", w.players[a].char.Gold)
		}
	})
	if n != 1 {
		t.Fatalf("应只有一堆石币：%d", n)
	}
	clb.take()
	var dir int
	w.Do(func() { dir = w.players[b].dir })
	w.PickUp(b, dir)
	if !strings.Contains(fmtMsgs(clb.take()), "拾获 500 Stone") {
		t.Error("应捡到 500 Stone")
	}
}

// 丢宠物（_PET_dropPet）：放在地上，只有丢的人能捡回来（PETUP=0）。
func TestGroundPet(t *testing.T) {
	w, g, a, b, cla, clb, _ := groundPair(t)
	slot := givePet(t, w, a, petEnemy(t, g, 1), 5)
	w.PetDrop(a, slot)
	app, ok := find[*proto.ObjAppear](clb.take())
	if !ok || app.Objs[0].Kind != "pet" || app.Objs[0].Level != 5 {
		t.Fatalf("宠物应放在地上：%+v", app)
	}
	var dir int
	w.Do(func() { dir = w.players[b].dir })
	w.PickUp(b, dir)
	if !strings.Contains(fmtMsgs(clb.take()), "无法拾获该宠物。") {
		t.Error("别人的宠物捡不起来")
	}
	cla.take()
	w.Do(func() { dir = w.players[a].dir })
	w.PickUp(a, dir)
	if !strings.Contains(fmtMsgs(cla.take()), "拾获 ") {
		t.Fatal("丢的人能捡回来")
	}
	w.Do(func() {
		if w.players[a].pets.Pets[0] == nil {
			t.Error("宠物应回到宠物栏")
		}
	})
}
