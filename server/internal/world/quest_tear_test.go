package world

import (
	"strings"
	"testing"
	"time"

	"sa/server/internal/data"

	"sa/server/internal/proto"
)

// 泪之石（sa60/tear，事件 136，6 转的条件之一）：夕姬（加加 200，夜里）接任务拿女戒 → 水世界 817 (189,513) 的传送点按身上的东西分流 →
// 洛林（8027）女戒换项链 → 夕姬项链换信 → 传送点到 8028 → 洛林（8028）看了信传到 8029 → 打赢杰夫（带着 2414）传到 8015 →
// 破帽子给男戒 → 夕姬收男戒与信，给 5 个泪之石，事件 136 做完。
func TestQuestTear(t *testing.T) {
	w, db, g := setup(t)
	for _, f := range []int{200, 817, 8015, 8027, 8028, 8029} {
		if g.Floors[f] == nil {
			t.Skipf("没有 %d 号地图", f)
		}
	}
	yuki := findNPC(t, w, "ExChangeMan", "sa60/tear/tear.arg")
	if yuki.floor != 200 || yuki.x != 106 || yuki.y != 1011 {
		t.Fatalf("夕姬应在 200 (106,1011)：%d (%d,%d)", yuki.floor, yuki.x, yuki.y)
	}
	c, cl := besideNPC(t, w, db, "tear", yuki)
	id := int(c.ID)
	// 夜里（TIME=0：SA 时间 300..700）
	w.Do(func() {
		base := w.now()
		for i := 0; i < 24*60; i++ {
			at := base.Add(time.Duration(i) * time.Minute)
			w.now = func() time.Time { return at }
			if lsSection(w.saHour()) == 0 {
				return
			}
		}
		t.Fatal("找不到夜里的时间")
	})
	w.Do(func() {
		p := w.players[id]
		p.char.Level = 80
		p.setEventDone(4)
	})
	talk := func(n *entity) {
		t.Helper()
		w.Do(func() {
			p := w.players[id]
			p.window = nil
			w.placeOnFloor(p, n.floor, n.x+1, n.y, 1)
		})
		cl.take()
		win, _ := exTalk(w, cl, id, n)
		if win == nil {
			t.Fatalf("%s 没有开窗口", n.name)
		}
		for i := 0; i < 12 && win != nil; i++ { // 夕姬最后一段有 7 页，exFinish 只按 6 次
			btn := BtnOK
			switch {
			case win.Buttons&BtnYes != 0:
				btn = BtnYes
			case win.Buttons&BtnNext != 0:
				btn = BtnNext
			}
			win, _ = exReply(w, cl, id, n, win, btn)
		}
	}
	has := func(item, n int) {
		t.Helper()
		if got := exCount(w, id, item); got != n {
			t.Fatalf("道具 %d 应有 %d 个：%d", item, n, got)
		}
	}
	// 水世界的传送点（FREEMORE）：从旁边一格踩上去
	warpPoint := func(want int) {
		t.Helper()
		w.Do(func() {
			p := w.players[id]
			p.window = nil
			w.placeOnFloor(p, 817, 188, 513, 5)
			p.stepTokens, p.lastStep = StepBurst, time.Time{}
		})
		cl.take()
		w.Move(id, 188, 513, 5)
		ef, ok := find[*proto.EnterFloor](cl.take())
		if !ok || ef.Floor != want {
			t.Fatalf("817 (189,513) 的传送点应传到 %d：%+v", want, ef)
		}
	}

	warpPoint(8027) // 还没接任务：FREE:LV>0 → 8027
	talk(yuki)
	has(1678, 1)
	w.Do(func() {
		if !w.players[id].eventNow(136) {
			t.Fatal("接了任务 136 应进行中")
		}
	})

	warpPoint(8027) // 带着女戒 → 8027
	talk(findNPC(t, w, "ExChangeMan", "sa60/tear/tear1.arg"))
	has(1678, 0)
	has(1676, 1)

	talk(yuki) // 项链换信
	has(1676, 0)
	has(1679, 1)

	warpPoint(8028) // 带着信 → 8028
	lorin := findNPC(t, w, "WarpMan", "sa60/tear/tear6.arg")
	w.Do(func() {
		p := w.players[id]
		w.placeOnFloor(p, lorin.floor, lorin.x+1, lorin.y, 1)
	})
	cl.take()
	w.Talk(id, lorin.id)
	win, _ := find[*proto.NpcWindow](cl.take())
	if win == nil {
		t.Fatal("洛林（8028）应问是不是给他的信")
	}
	w.NpcWindowReply(id, lorin.id, win.Seq, BtnYes, "")
	if ef, ok := find[*proto.EnterFloor](cl.take()); !ok || ef.Floor != 8029 {
		t.Fatalf("洛林看了信应传到 8029：%+v", ef)
	}

	// 杰夫（战斗 NPC）：带着 2414 打赢传到 8015
	if g.Items[2414] == nil {
		t.Fatal("没有道具 2414")
	}
	giveItem(w, id, 2414)
	jeff := findNPC(t, w, "NPCEnemy", "sa60/tear/tear2.arg")
	w.Do(func() {
		p := w.players[id]
		w.placeOnFloor(p, jeff.floor, jeff.x+1, jeff.y, 1)
	})
	cl.take()
	w.Talk(id, jeff.id)
	win, _ = find[*proto.NpcWindow](cl.take())
	if win == nil {
		t.Fatal("杰夫应问要不要打")
	}
	w.NpcWindowReply(id, jeff.id, win.Seq, BtnYes, "")
	cl.take()
	msgs := npcEnemyWin(t, w, id, cl)
	w.Do(func() {})
	msgs = append(msgs, cl.take()...)
	if ef, ok := find[*proto.EnterFloor](msgs); !ok || ef.Floor != 8015 {
		t.Fatalf("打赢杰夫应传到 8015：%s", fmtMsgs(msgs))
	}

	talk(findNPC(t, w, "ExChangeMan", "sa60/tear/tear3.arg")) // 破帽子：有信给男戒
	has(1677, 1)
	warpPoint(8015) // 带着男戒 → 8015

	talk(yuki) // 男戒与信 → 5 个泪之石
	has(1677, 0)
	has(1679, 0)
	has(1680, 5)
	w.Do(func() {
		p := w.players[id]
		if !p.eventDone(136) || p.eventNow(136) {
			t.Fatalf("事件 136 应做完：%q / %q", p.char.EndEvents, p.char.NowEvents)
		}
	})
	warpPoint(8015) // 做完之后 → 8015
}

// 可接任务里的"未满足"：140 级、什么都没做的角色，琉璃洞窟（39）与泪之石（136）列成未满足、还差成人礼；
// 做完成人礼后泪之石只差"夜里"（白天时）；要精灵召唤任务（HERO_I_NOW，系统没开）的古瓜委托（224）不列；可接的排在前面。
func TestQuestUnmet(t *testing.T) {
	w, db, g := setup(t)
	h := data.Hometowns[0]
	c := newChar(t, db, "unmet", h.Floor, h.X, h.Y)
	cl := &fakeClient{}
	w.Enter(c, cl)
	id := int(c.ID)
	avail := func() (map[int]proto.QuestEntry, []proto.QuestEntry) {
		m := map[int]proto.QuestEntry{}
		var l []proto.QuestEntry
		w.Do(func() {
			l = w.availableQuests(w.players[id])
			for _, q := range l {
				m[q.ID] = q
			}
		})
		return m, l
	}
	w.Do(func() {
		w.players[id].char.Level = 140
		base := w.now()
		for i := 0; i < 24*60; i++ { // 白天
			at := base.Add(time.Duration(i) * time.Minute)
			w.now = func() time.Time { return at }
			if lsSection(w.saHour()) == 2 {
				return
			}
		}
	})
	m, l := avail()
	for _, ev := range []int{39, 136} {
		if q := m[ev]; q.State != "未满足" || !strings.Contains(q.Hint, "先完成：成人礼") {
			t.Errorf("事件 %d 应未满足、还差成人礼：%+v", ev, q)
		}
	}
	if q := m[4]; q.State != "可接" {
		t.Errorf("成人礼应可接：%+v", q)
	}
	if _, ok := m[224]; ok && g.Floors[7130] != nil {
		t.Errorf("要精灵召唤任务的不应列：%+v", m[224])
	}
	seenUnmet := false
	for _, q := range l {
		if q.State == "未满足" {
			seenUnmet = true
		} else if seenUnmet {
			t.Fatalf("可接的应排在未满足前面：%+v", l)
		}
	}
	w.Do(func() { w.players[id].setEventDone(4) })
	m, _ = avail()
	if q := m[136]; q.State != "未满足" || !strings.HasSuffix(q.Hint, "还差：在夜里去") {
		t.Errorf("做完成人礼后泪之石只差夜里：%+v", q)
	}
	if q := m[39]; q.State != "可接" {
		t.Errorf("做完成人礼后琉璃洞窟应可接：%+v", q)
	}
}

// questStepOnto：站到 (x,y) 旁边一格能走、不是传送点的地方，朝它走一步（踩传送点），回传收到的消息。
func questStepOnto(t *testing.T, w *World, g *data.Game, cl *fakeClient, id, floor, x, y int) []proto.Message {
	t.Helper()
	f := g.Floors[floor]
	for d := 0; d < 8; d++ {
		sx, sy := x-MOVE[d][0], y-MOVE[d][1]
		if _, warp := g.WarpAt(floor, sx, sy); warp || !g.Tiles.Walkable(f, sx, sy) {
			continue
		}
		w.Do(func() {
			p := w.players[id]
			p.window = nil
			w.placeOnFloor(p, floor, sx, sy, d)
			p.stepTokens, p.lastStep = StepBurst, time.Time{}
		})
		cl.take()
		w.Move(id, sx, sy, d)
		return cl.take()
	}
	t.Fatalf("%d (%d,%d) 旁边没有能站的格子", floor, x, y)
	return nil
}

// 贪婪的林务局长（eden3/lumberjack，事件 115，雷尔任务的前置）：弗斯温蒂（玛丽娜丝渔村 2000）接任务 → 伐木场 703 的传送点到 7454 →
// 打赢工头布鲁斯壮到 7453 → 布鲁斯壮给命令书 → 伊甸园 7000 的传送点到 7024 → 打赢林务局长柯比罗尔到 7015 → 柯比罗尔用命令书换悔过书 →
// 弗斯温蒂收悔过书，事件 115 做完、给风魔四戒。
func TestQuestLumberjack(t *testing.T) {
	w, db, g := setup(t)
	for _, f := range []int{2000, 703, 7000, 7015, 7024, 7453, 7454} {
		if g.Floors[f] == nil {
			t.Skipf("没有 %d 号地图", f)
		}
	}
	windy := findNPC(t, w, "ExChangeMan", "eden3/lumberjack/windy")
	if windy.floor != 2000 || windy.x != 46 || windy.y != 71 {
		t.Fatalf("弗斯温蒂应在 2000 (46,71)：%d (%d,%d)", windy.floor, windy.x, windy.y)
	}
	c, cl := besideNPC(t, w, db, "lumber", windy)
	id := int(c.ID)
	w.Do(func() {
		p := w.players[id]
		p.char.Level = 100
		for _, ev := range []int{4, 81, 112, 113, 114} {
			p.setEventDone(ev)
		}
	})
	talk := func(n *entity) {
		t.Helper()
		w.Do(func() {
			p := w.players[id]
			p.window = nil
			w.placeOnFloor(p, n.floor, n.x+1, n.y, 1)
		})
		cl.take()
		win, _ := exTalk(w, cl, id, n)
		if win == nil {
			t.Fatalf("%s 没有开窗口", n.name)
		}
		for i := 0; i < 12 && win != nil; i++ {
			btn := BtnOK
			switch {
			case win.Buttons&BtnYes != 0:
				btn = BtnYes
			case win.Buttons&BtnNext != 0:
				btn = BtnNext
			}
			win, _ = exReply(w, cl, id, n, win, btn)
		}
	}
	fight := func(n *entity, want int) {
		t.Helper()
		w.Do(func() {
			p := w.players[id]
			p.window = nil
			w.placeOnFloor(p, n.floor, n.x+1, n.y, 1)
		})
		cl.take()
		w.Talk(id, n.id)
		win, _ := find[*proto.NpcWindow](cl.take())
		if win == nil {
			t.Fatalf("%s 应问要不要打", n.name)
		}
		w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
		cl.take()
		msgs := npcEnemyWin(t, w, id, cl)
		w.Do(func() {})
		msgs = append(msgs, cl.take()...)
		if ef, ok := find[*proto.EnterFloor](msgs); !ok || ef.Floor != want {
			t.Fatalf("打赢%s应传到 %d：%s", n.name, want, fmtMsgs(msgs))
		}
	}
	has := func(item, n int) {
		t.Helper()
		if got := exCount(w, id, item); got != n {
			t.Fatalf("道具 %d 应有 %d 个：%d", item, n, got)
		}
	}

	talk(windy)
	w.Do(func() {
		if !w.players[id].eventNow(115) {
			t.Fatal("弗斯温蒂应开任务 115")
		}
	})
	if ef, ok := find[*proto.EnterFloor](questStepOnto(t, w, g, cl, id, 703, 334, 182)); !ok || ef.Floor != 7454 {
		t.Fatalf("接了任务、没有悔过书，伐木场的传送点应到 7454：%+v", ef)
	}
	fight(findNPC(t, w, "NPCEnemy", "eden3/lumberjack/bluestrong.arg"), 7453)
	talk(findNPC(t, w, "ExChangeMan", "eden3/lumberjack/bluestrong"))
	has(20214, 1)
	if ef, ok := find[*proto.EnterFloor](questStepOnto(t, w, g, cl, id, 7000, 27, 161)); !ok || ef.Floor != 7024 {
		t.Fatalf("带着命令书，伊甸园的传送点应到 7024：%+v", ef)
	}
	fight(findNPC(t, w, "NPCEnemy", "eden3/lumberjack/koberoll.arg"), 7015)
	talk(findNPC(t, w, "ExChangeMan", "eden3/lumberjack/kobe"))
	has(20214, 0)
	has(20198, 1)
	talk(windy)
	has(20198, 0)
	has(20185, 1)
	w.Do(func() {
		p := w.players[id]
		if !p.eventDone(115) || p.eventNow(115) {
			t.Fatalf("事件 115 应做完：%q / %q", p.char.EndEvents, p.char.NowEvents)
		}
	})
}
