package world

import (
	"strings"
	"testing"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// petTalkSpot 在 floor 上找一格能站、面前一格也能走（都不是传送点）的地方：玩家站上去面向那格，宠物放在那格。
func petTalkSpot(t *testing.T, g *data.Game, floor int) (x, y, d int) {
	t.Helper()
	f := g.Floors[floor]
	if f == nil {
		t.Fatalf("没有 %d 号地图", floor)
	}
	for y := 1; y < f.H-1; y++ {
		for x := 1; x < f.W-1; x++ {
			if _, warp := g.WarpAt(floor, x, y); warp || !g.Tiles.Walkable(f, x, y) {
				continue
			}
			if d, ok := walkableStep(g, f, x, y); ok {
				return x, y, d
			}
		}
	}
	t.Fatalf("%d 号地图找不到能站的地方", floor)
	return
}

// petTalkTo 把 pet 放在 floor 上玩家面前的地上，点它说话，回传宠物窗口（没有则 nil）与收到的消息。
func petTalkTo(t *testing.T, w *World, g *data.Game, cl *fakeClient, id int, pet *Pet, owner int64, floor int) (*proto.NpcWindow, []proto.Message) {
	t.Helper()
	x, y, d := petTalkSpot(t, g, floor)
	var o *entity
	w.Do(func() {
		p := w.players[id]
		p.window = nil
		w.placeOnFloor(p, floor, x, y, d)
		o = w.putGround(floor, x+MOVE[d][0], y+MOVE[d][1], "pet", pet.displayName(), pet.Sprite, &ground{pet: pet, owner: owner})
	})
	cl.take()
	w.Talk(id, o.id)
	var msgs []proto.Message
	w.Do(func() { w.removeFromFloor(o) })
	msgs = cl.take()
	win, _ := find[*proto.NpcWindow](msgs)
	return win, msgs
}

// 玛蕾菲雅（模板 718，等级上限 10）：到了上限、在 ptalk01 指定的楼层跟她说话，上限一段段提高到 79；
// 不在那层只说提示、不提高；没到上限说 TALKRUN 的闲话。
func TestPetTalkMarefia(t *testing.T) {
	w, db, g := setup(t)
	h := data.Hometowns[0]
	c := newChar(t, db, "mare", h.Floor, h.X, h.Y)
	cl := &fakeClient{}
	w.Enter(c, cl)
	id := int(c.ID)
	pet := enemyPet(t, w, 1479, 5, c.ID)
	if pet.TempNo != 718 || pet.LimitLevel != 10 {
		t.Fatalf("祖母给的玛蕾菲雅应是模板 718、等级上限 10：%d %d", pet.TempNo, pet.LimitLevel)
	}

	win, _ := petTalkTo(t, w, g, cl, id, pet, c.ID, 1000)
	if win == nil || win.Buttons != BtnOK || pet.LimitLevel != 10 {
		t.Fatalf("5 级（没到上限）应说 TALKRUN 的闲话、上限不变：%+v %d", win, pet.LimitLevel)
	}
	if !strings.Contains(win.Text, "新藏哥哥") && !strings.Contains(win.Text, "走来走去") && !strings.Contains(win.Text, "善待宠物") {
		t.Errorf("1..9 级的闲话应是 TALKRUN17..19 其中一句：%q", win.Text)
	}

	// ptalk01 的 EVENTRUN：等级 = 上限时，在这层说话上限变成下一个
	steps := []struct{ lv, floor, limit int }{
		{10, 1000, 15}, {15, 1400, 20}, {20, 1200, 25}, {25, 5542, 30}, {30, 4000, 35}, {35, 20301, 40}, {40, 3300, 45},
		{45, 21201, 50}, {50, 20105, 55}, {55, 6000, 60}, {60, 31901, 65}, {65, 30703, 70}, {70, 31201, 75}, {75, 40, 79},
	}
	for _, s := range steps {
		pet.Level = s.lv
		other := 1000
		if s.floor == 1000 {
			other = 1400
		}
		if win, _ := petTalkTo(t, w, g, cl, id, pet, c.ID, other); win == nil || pet.LimitLevel != s.lv {
			t.Fatalf("%d 级不在 %d 号说话应只给提示、上限不变：%+v %d", s.lv, s.floor, win, pet.LimitLevel)
		}
		if win, _ := petTalkTo(t, w, g, cl, id, pet, c.ID, s.floor); win == nil || pet.LimitLevel != s.limit {
			t.Fatalf("%d 级在 %d 号说话上限应变成 %d：%+v %d", s.lv, s.floor, s.limit, win, pet.LimitLevel)
		}
	}
	// 30703 那一步（65 级）给三朵花 19688
	if g.Items[19688] != nil && exCount(w, id, 19688) != 3 {
		t.Errorf("在 30703 应拿到三朵花 19688：%d", exCount(w, id, 19688))
	}
	pet.Level = 79
	win, _ = petTalkTo(t, w, g, cl, id, pet, c.ID, 2000)
	if win == nil || !strings.Contains(win.Text, "拯救精灵王") || pet.LimitLevel != 79 {
		t.Errorf("79 级应说要去拯救精灵王：%+v %d", win, pet.LimitLevel)
	}
}

// 别人的宠物：宠物说"陌生人？…"，不开窗口、不改上限；没有对话档的宠物不理人；对着宠物说话（聊天）也算。
func TestPetTalkOwnerAndChat(t *testing.T) {
	w, db, g := setup(t)
	h := data.Hometowns[0]
	a, b := newChar(t, db, "owner", h.Floor, h.X, h.Y), newChar(t, db, "stranger", h.Floor, h.X, h.Y)
	ca, cb := &fakeClient{}, &fakeClient{}
	w.Enter(a, ca)
	w.Enter(b, cb)
	pet := enemyPet(t, w, 1479, 10, a.ID)

	win, msgs := petTalkTo(t, w, g, cb, int(b.ID), pet, a.ID, 1000)
	talk, ok := find[*proto.NpcTalk](msgs)
	if win != nil || !ok || !strings.Contains(talk.Text, "陌生人") || pet.LimitLevel != 10 {
		t.Fatalf("不是主人应只说陌生人：%+v %+v %d", win, talk, pet.LimitLevel)
	}

	other := enemyPet(t, w, petEnemy(t, g, 2).ID, 10, a.ID) // 乌力乌力：私服的 ptalk02 没收
	if win, _ := petTalkTo(t, w, g, ca, int(a.ID), other, a.ID, 1000); win != nil {
		t.Errorf("没有对话档的宠物不应开窗口：%+v", win)
	}

	// 聊天：站在宠物面前说一句话
	x, y, d := petTalkSpot(t, g, 1000)
	w.Do(func() {
		p := w.players[int(a.ID)]
		p.window = nil
		w.placeOnFloor(p, 1000, x, y, d)
		w.putGround(1000, x+MOVE[d][0], y+MOVE[d][1], "pet", pet.displayName(), pet.Sprite, &ground{pet: pet, owner: a.ID})
	})
	ca.take()
	w.Chat(int(a.ID), "near", "你好", 0, 1)
	w.Do(func() {})
	if win, ok := find[*proto.NpcWindow](ca.take()); !ok || pet.LimitLevel != 15 {
		t.Errorf("对着宠物说话也应触发（10 级在 1000 号上限变 15）：%+v %d", win, pet.LimitLevel)
	}
}
