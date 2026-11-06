//go:build questcave

package world

import (
	"fmt"
	"strings"
	"testing"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 漆黑的洞窟（32001..32021）与天空之岛任务（事件 69..72）的走查：按玩家的走法一步步做，每步检查旗标 / 道具 / 位置。
// 走路用 qRoute：在地图上按可走的格子做 BFS（站着 NPC 的格子当作走不过去），踩到传送点就到目标楼层；
// qGo 把找到的路线照做：站到传送点前一格、真的用 Move 踩上去，检查 EnterFloor。

type qpos struct{ f, x, y int }

type qhop struct {
	from qpos
	dir  int
	warp data.Warp
}

// qBlocked 是各楼层站着 NPC（不算没形象的传送点）的格子。
func qBlocked(w *World) map[qpos]bool {
	out := map[qpos]bool{}
	w.Do(func() {
		for f, es := range w.floors {
			for _, e := range es {
				if e.npc != nil && e.client == nil {
					out[qpos{f, e.x, e.y}] = true
				}
			}
		}
	})
	return out
}

func qRoute(g *data.Game, blocked map[qpos]bool, start qpos, allow func(int) bool, goal func(qpos) bool) ([]qhop, qpos, bool) {
	type link struct {
		prev qpos
		hop  *qhop
		root bool
	}
	seen := map[qpos]link{start: {root: true}}
	queue := []qpos{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if goal(cur) {
			var hops []qhop
			for p := cur; !seen[p].root; p = seen[p].prev {
				if h := seen[p].hop; h != nil {
					hops = append([]qhop{*h}, hops...)
				}
			}
			return hops, cur, true
		}
		f := g.Floors[cur.f]
		if f == nil {
			continue
		}
		for d := 0; d < 8; d++ {
			nx, ny := cur.x+MOVE[d][0], cur.y+MOVE[d][1]
			if !g.Tiles.Walkable(f, nx, ny) || blocked[qpos{cur.f, nx, ny}] {
				continue
			}
			next := qpos{cur.f, nx, ny}
			var hop *qhop
			if wp, ok := g.WarpAt(cur.f, nx, ny); ok {
				if !allow(wp.ToFloor) || g.Floors[wp.ToFloor] == nil {
					continue
				}
				next = qpos{wp.ToFloor, wp.ToX, wp.ToY}
				hop = &qhop{cur, d, wp}
			}
			if _, ok := seen[next]; ok {
				continue
			}
			seen[next] = link{prev: cur, hop: hop}
			queue = append(queue, next)
		}
	}
	return nil, qpos{}, false
}

// qReach 列出从 start 能走到的楼层（BFS 不设目标）。
func qReach(g *data.Game, blocked map[qpos]bool, start qpos, allow func(int) bool) map[int]bool {
	floors := map[int]bool{}
	qRoute(g, blocked, start, allow, func(p qpos) bool { floors[p.f] = true; return false })
	return floors
}

func qRange(lo, hi int, extra ...int) func(int) bool {
	return func(f int) bool {
		if f >= lo && f <= hi {
			return true
		}
		for _, e := range extra {
			if f == e {
				return true
			}
		}
		return false
	}
}

func qNear(n *entity) func(qpos) bool {
	return func(p qpos) bool {
		return p.f == n.floor && max(abs(p.x-n.x), abs(p.y-n.y)) == 1
	}
}

func qPos(w *World, id int) (p qpos) {
	w.Do(func() { e := w.players[id]; p = qpos{e.floor, e.x, e.y} })
	return
}

func qPlace(w *World, cl *fakeClient, id int, p qpos, dir int) {
	w.Do(func() { w.placeOnFloor(w.players[id], p.f, p.x, p.y, dir) })
	cl.take()
}

// qGo 照 BFS 找到的路线走到 goal：每个传送点都真的踩上去；回传走过的楼层。
func qGo(t *testing.T, w *World, blocked map[qpos]bool, id int, cl *fakeClient, allow func(int) bool, goal func(qpos) bool, what string) []int {
	t.Helper()
	start := qPos(w, id)
	hops, end, ok := qRoute(w.game, blocked, start, allow, goal)
	if !ok {
		t.Fatalf("%s：从 %v 走不到", what, start)
	}
	route := []int{start.f}
	for _, h := range hops {
		qPlace(w, cl, id, h.from, h.dir)
		w.Do(func() { e := w.players[id]; e.stepTokens = StepBurst; e.lastStep = w.now() })
		w.Move(id, h.from.x, h.from.y, h.dir)
		msgs := cl.take()
		ef, moved := find[*proto.EnterFloor](msgs)
		if !moved || ef.Floor != h.warp.ToFloor || ef.X != h.warp.ToX || ef.Y != h.warp.ToY {
			t.Fatalf("%s：从 %v 往 %d 踩传送点 %+v 应到 %d(%d,%d)：%s", what, h.from, h.dir, h.warp, h.warp.ToFloor, h.warp.ToX, h.warp.ToY, fmtMsgs(msgs))
		}
		if route[len(route)-1] != ef.Floor {
			route = append(route, ef.Floor)
		}
	}
	qPlace(w, cl, id, end, 0)
	t.Logf("%s：%v → %v，楼层 %v（踩了 %d 个传送点）", what, start, end, route, len(hops))
	return route
}

// qNPC 找 NPC（不像 findNPC 找不到就 Skip）。
func qNPC(w *World, fn, key string) *entity {
	var found *entity
	w.Do(func() {
		for _, es := range w.floors {
			for _, e := range es {
				if e.npc == nil || e.npc.Function != fn {
					continue
				}
				for _, a := range e.npc.Args { // 参数文件名要整个对上（event69_1 不能找到 event69_11）
					if strings.HasSuffix(a, key) {
						found = e
					}
				}
			}
		}
	})
	return found
}

func qMustNPC(t *testing.T, w *World, fn, key string, floor, x, y int) *entity {
	t.Helper()
	n := qNPC(w, fn, key)
	if n == nil {
		t.Fatalf("没有 %s（%s）", fn, key)
	}
	if floor > 0 && (n.floor != floor || (x|y != 0 && (n.x != x || n.y != y))) {
		t.Errorf("%s 应在 %d(%d,%d)：%d(%d,%d)", key, floor, x, y, n.floor, n.x, n.y)
	}
	return n
}

func qWin(t *testing.T, w *World, id int, cl *fakeClient, n *entity) *proto.NpcWindow {
	t.Helper()
	w.Talk(id, n.id)
	msgs := cl.take()
	win, ok := find[*proto.NpcWindow](msgs)
	if !ok {
		t.Fatalf("和 %s 说话应有窗口：%s", n.name, fmtMsgs(msgs))
	}
	return win
}

func qWarpYes(t *testing.T, w *World, id int, cl *fakeClient, n *entity, want qpos) {
	t.Helper()
	win := qWin(t, w, id, cl, n)
	if win.Buttons != BtnYesNo {
		t.Fatalf("%s 应问是/否：%+v", n.name, win)
	}
	w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
	msgs := cl.take()
	if ef, ok := find[*proto.EnterFloor](msgs); !ok || ef.Floor != want.f || ef.X != want.x || ef.Y != want.y {
		t.Fatalf("%s 按是应传到 %v：%s", n.name, want, fmtMsgs(msgs))
	}
}

// qFight：确认窗口按是开打 → 检查出场的敌人都在 enemyno 里 → 打赢，回传结算的讯息。
func qFight(t *testing.T, w *World, id int, cl *fakeClient, n *entity) []proto.Message {
	t.Helper()
	g := w.game
	ids := enemyNo(n.npc.Arg, 64)
	if len(ids) == 0 {
		t.Fatalf("%s 没有 enemyno", n.name)
	}
	want := map[string]bool{}
	for _, s := range ids {
		e := g.Battle.Enemies[data.Atoi(s)]
		if e == nil {
			t.Errorf("%s 的 enemyno %s 不存在", n.name, s)
			continue
		}
		want[g.Battle.Temps[e.TempNo].Name] = true
	}
	st := talkYes(t, w, id, cl, n)
	cnt := 0
	for _, f := range st.Fighters {
		if f.Slot >= 10 {
			cnt++
			if !want[f.Name] {
				t.Errorf("%s 出场的 %s 不在 enemyno 里", n.name, f.Name)
			}
		}
	}
	if cnt == 0 {
		t.Fatalf("%s 没有敌人出场", n.name)
	}
	return npcEnemyWin(t, w, id, cl)
}

func qWantFloor(t *testing.T, msgs []proto.Message, want qpos, what string) {
	t.Helper()
	if ef, ok := find[*proto.EnterFloor](msgs); !ok || ef.Floor != want.f || ef.X != want.x || ef.Y != want.y {
		t.Fatalf("%s 应传到 %v：%s", what, want, fmtMsgs(msgs))
	}
}

func qFlags(w *World, id int) (s string) {
	w.Do(func() { c := w.players[id].char; s = fmt.Sprintf("end=%q now=%q", c.EndEvents, c.NowEvents) })
	return
}

func qEv(w *World, id int, f func(p *entity)) { w.Do(func() { f(w.players[id]) }) }

// qF 是在 w.Do 里面用的 qFlags（不能再 w.Do）。
func qBad(t *testing.T, bad *bool, format string, args ...any) {
	t.Helper()
	t.Errorf(format, args...)
	*bad = true
}

func qF(p *entity) string { return fmt.Sprintf("end=%q now=%q", p.char.EndEvents, p.char.NowEvents) }

// ================= Part A：漆黑的洞窟 =================

func TestQuestCaveRoute(t *testing.T) {
	w, db, g := setup(t)
	for f := 32001; f <= 32021; f++ {
		if g.Floors[f] == nil {
			t.Fatalf("没有 %d 号地图", f)
		}
	}
	// 洞窟里的每个传送点：目标楼层存在、目标格可走
	for f := 32001; f <= 32021; f++ {
		for xy, wp := range g.Warps[f] {
			if !g.Tiles.Walkable(g.Floors[f], xy[0], xy[1]) {
				t.Logf("（提示）传送点 %d(%d,%d) 本身的格子不能走、踩不到（%s）", f, xy[0], xy[1], wp.Source)
			}
			if tf := g.Floors[wp.ToFloor]; tf == nil || !g.Tiles.Walkable(tf, wp.ToX, wp.ToY) {
				t.Errorf("传送点 %d(%d,%d) → %d(%d,%d) 的目标格不能走（%s）", f, xy[0], xy[1], wp.ToFloor, wp.ToX, wp.ToY, wp.Source)
			}
		}
	}
	blocked := qBlocked(w)

	gate := qMustNPC(t, w, "WarpMan", "wpm_300_108_225", 300, 108, 225)
	c, cl := besideNPC(t, w, db, "caver", gate)
	id := int(c.ID)
	// 守门人旁边的格子要能走、而且连得到 300 号地图的大片地方
	if _, _, ok := qRoute(g, blocked, qpos{300, 109, 225}, qRange(300, 300), func(p qpos) bool { return abs(p.x-108)+abs(p.y-225) > 30 }); !ok {
		t.Errorf("守门人（300 108,225）旁边走不出去")
	}

	// 条件不够：等级 80 没旗标、或有旗标只有 79 级 → PayMsg（MONEY:-1 只有确定），伪造"是"也不传
	exChar(w, id, func(e *entity) { e.char.Level = 80 })
	for i, prep := range []func(e *entity){
		func(e *entity) {},
		func(e *entity) {
			e.char.Level = 79
			for _, ev := range []int{39, 40, 42, 46} {
				e.setEventDone(ev)
			}
		},
		func(e *entity) { e.char.Level = 80; setFlag(&e.char.EndEvents, 46, false) },
	} {
		exChar(w, id, prep)
		win := qWin(t, w, id, cl, gate)
		if win.Buttons != BtnOK || !strings.Contains(win.Text, "没有接受精灵们加护的人不能继续前进") {
			t.Errorf("情况 %d：条件不够应显示 PayMsg：%+v", i, win)
		}
		w.NpcWindowReply(id, gate.id, win.Seq, BtnYes, "")
		if _, moved := find[*proto.EnterFloor](cl.take()); moved {
			t.Errorf("情况 %d：条件不够不应传送", i)
		}
	}
	exChar(w, id, func(e *entity) {
		e.char.Level = 80
		for _, ev := range []int{39, 40, 42, 46} {
			e.setEventDone(ev)
		}
	})
	win := qWin(t, w, id, cl, gate)
	if !strings.Contains(win.Text, "你要继续前进的话，就请进吧") {
		t.Errorf("应显示 FreeMsg：%q", win.Text)
	}
	cl.take()
	qWarpYes(t, w, id, cl, gate, qpos{32001, 29, 3})

	// 32001 → 32010 漆黑的守护兽
	skr := qMustNPC(t, w, "NPCEnemy", "sb_skr.arg", 32010, 54, 30)
	qGo(t, w, blocked, id, cl, qRange(32001, 32010), qNear(skr), "到漆黑的守护兽")
	// 守护兽打赢前到不了 32011 以后
	if fl := qReach(g, blocked, qPos(w, id), qRange(32001, 32021)); fl[32011] {
		t.Errorf("不打守护兽也能走到 32011：%v", fl)
	}
	win = qWin(t, w, id, cl, skr)
	if !strings.Contains(win.Text, "具备知识与力量") {
		t.Errorf("守护兽应问 askbattlemsg：%q", win.Text)
	}
	w.NpcWindowReply(id, skr.id, win.Seq, BtnNo, "")
	cl.take()
	before := qFlags(w, id)
	qWantFloor(t, qFight(t, w, id, cl, skr), qpos{32011, 56, 36}, "打赢漆黑的守护兽")
	if after := qFlags(w, id); after != before {
		t.Errorf("守护兽不设旗标：%s → %s", before, after)
	}

	// 32011 → 32016 守门人（南边）
	gate16 := qMustNPC(t, w, "WarpMan", "wpm_32016_31_39", 32016, 31, 39)
	qGo(t, w, blocked, id, cl, qRange(32011, 32016), func(p qpos) bool { return qNear(gate16)(p) && p.y > 39 }, "到 32016 守门人")
	if fl := qReach(g, blocked, qPos(w, id), qRange(32011, 32021)); fl[32017] {
		t.Errorf("不找守门人也能走到 32017：%v", fl)
	}
	exChar(w, id, func(e *entity) { setFlag(&e.char.EndEvents, 42, false) })
	win = qWin(t, w, id, cl, gate16)
	if win.Buttons != BtnOK || !strings.Contains(win.Text, "继续前进所需的条件啊") {
		t.Errorf("32016 守门人：条件不够应显示 PayMsg：%+v", win)
	}
	exChar(w, id, func(e *entity) { e.setEventDone(42) })
	qWarpYes(t, w, id, cl, gate16, qpos{32016, 31, 37})

	// 32016 → 32020 帖拉所伊朵
	skt := qMustNPC(t, w, "NPCEnemy", "sb_skt.arg", 32020, 20, 9)
	qGo(t, w, blocked, id, cl, qRange(32016, 32020), qNear(skt), "到帖拉所伊朵")
	if fl := qReach(g, blocked, qPos(w, id), qRange(32016, 32021)); fl[32021] {
		t.Errorf("不打帖拉所伊朵也能走到 32021：%v", fl)
	}
	qWantFloor(t, qFight(t, w, id, cl, skt), qpos{32021, 29, 57}, "打赢帖拉所伊朵")

	// 32021：守门人 → 精灵王
	king := transNPC(t, w)
	if _, _, ok := qRoute(g, blocked, qPos(w, id), qRange(32021, 32021), qNear(king)); ok {
		t.Errorf("不经过 32021 守门人就走得到精灵王")
	}
	gate21 := qMustNPC(t, w, "WarpMan", "wpm_32021_31_51", 32021, 31, 51)
	qGo(t, w, blocked, id, cl, qRange(32021, 32021), func(p qpos) bool { return qNear(gate21)(p) && p.y > 51 }, "到 32021 守门人")
	qWarpYes(t, w, id, cl, gate21, qpos{32021, 32, 47})
	qGo(t, w, blocked, id, cl, qRange(32021, 32021), qNear(king), "到精灵王")
	w.Talk(id, king.id)
	// 没带守护兽：精灵王说 nonemsg（转生本身另有测试）
	if win, ok := find[*proto.NpcWindow](cl.take()); !ok || !strings.Contains(win.Text, "转生所需要的条件") {
		t.Errorf("没带守护兽的精灵王应说 nonemsg：%+v", win)
	}
	// 回程：32021 (53,49) → 300 (108,225)
	route := qGo(t, w, blocked, id, cl, qRange(32021, 32021, 300), func(p qpos) bool { return p.f == 300 }, "从精灵王回 300")
	if p := qPos(w, id); p != (qpos{300, 108, 225}) {
		t.Errorf("回程应到 300(108,225)：%v %v", p, route)
	}
}

// 考古家（add80 sikkoku01..05）与洞窟里的记录点。
func TestQuestCaveArchaeologists(t *testing.T) {
	w, db, g := setup(t)
	blocked := qBlocked(w)
	for _, it := range []int{2771, 2772, 2773, 2774, 2775, 2701, 2707, 2735, 2770, 2205, 1887, 1803, 1851} {
		if g.Items[it] == nil {
			t.Errorf("没有道具 %d", it)
		}
	}
	type arch struct {
		key   string
		floor int
		x, y  int
		item  int
		from  qpos
		allow func(int) bool
	}
	archs := []arch{
		{"sikkoku01", 32002, 4, 23, 2771, qpos{32001, 29, 3}, qRange(32001, 32010)},
		{"sikkoku02", 32006, 46, 46, 2772, qpos{32001, 29, 3}, qRange(32001, 32010)},
		{"sikkoku03", 32009, 49, 20, 2773, qpos{32001, 29, 3}, qRange(32001, 32010)},
		{"sikkoku04", 32014, 14, 48, 2774, qpos{32011, 56, 36}, qRange(32011, 32016)},
	}
	var c0 int
	var cl0 *fakeClient
	for i, a := range archs {
		n := qMustNPC(t, w, "ExChangeMan", "add80/giiru/event/"+a.key, a.floor, a.x, a.y)
		if _, _, ok := qRoute(g, blocked, a.from, a.allow, qNear(n)); !ok {
			t.Errorf("%s：从 %v 走不到考古家", a.key, a.from)
		}
		if i == 0 {
			c, cl := besideNPC(t, w, db, "digger", n)
			c0, cl0 = int(c.ID), cl
		} else {
			qPlace(w, cl0, c0, qpos{n.floor, n.x + 1, n.y}, 1)
		}
		win, msgs := exTalk(w, cl0, c0, n)
		if win == nil || !strings.Contains(win.Text, "证据") || exCount(w, c0, a.item) != 1 {
			t.Errorf("%s 第一次应讲故事并给 %d：%+v %s", a.key, a.item, win, fmtMsgs(msgs))
		}
		if win != nil {
			exReply(w, cl0, c0, n, win, BtnOK)
		}
		win, _ = exTalk(w, cl0, c0, n)
		if win == nil || strings.Contains(win.Text, "证据") || exCount(w, c0, a.item) != 1 {
			t.Errorf("%s 第二次只讲故事、不再给：%+v", a.key, win)
		}
		if win != nil {
			exReply(w, cl0, c0, n, win, BtnOK)
		}
	}
	// 第五位（32016 守门人南边）
	n5 := qMustNPC(t, w, "ExChangeMan", "add80/giiru/event/sikkoku05", 32016, 31, 43)
	if _, _, ok := qRoute(g, blocked, qpos{32016, 54, 42}, qRange(32016, 32016), qNear(n5)); !ok {
		t.Errorf("sikkoku05：从 32016 入口走不到")
	}
	c, cl := besideNPC(t, w, db, "digger5", n5)
	id := int(c.ID)
	win, _ := exTalk(w, cl, id, n5)
	if win == nil || !strings.Contains(win.Text, "4位通") {
		t.Errorf("什么都没有应叫你去找 4 位考古家：%+v", win)
	}
	for _, it := range []int{2771, 2772, 2773, 2774} {
		giveItem(w, id, it)
	}
	win, _ = exTalk(w, cl, id, n5)
	if win == nil || !strings.Contains(win.Text, "大地的守护兽") {
		t.Errorf("四件证据都有应提示大地的守护兽：%+v", win)
	}
	exEvDone := func(ev int) (b bool) { qEv(w, id, func(p *entity) { b = p.eventDone(ev) }); return }
	qEv(w, id, func(p *entity) { p.setEventDone(48) })
	win, _ = exTalk(w, cl, id, n5)
	if win == nil || !strings.Contains(win.Text, "水的守护兽") {
		t.Errorf("ENDEV 48 应提示水的守护兽：%+v", win)
	}
	for _, it := range []int{2701, 2707, 2735, 2770} {
		giveItem(w, id, it)
	}
	win, _ = exTalk(w, cl, id, n5)
	if win == nil || !strings.Contains(win.Text, "精灵给你的信物") {
		t.Fatalf("带四件信物应显示 AcceptMsg：%+v", win)
	}
	msgs := exFinish(w, cl, id, n5, win)
	if exCount(w, id, 2775) != 1 || !exEvDone(53) {
		t.Errorf("交换后应拿到 2775、ENDEV 53：%d %s %s", exCount(w, id, 2775), qFlags(w, id), fmtMsgs(msgs))
	}

	// 记录点（要肉）
	for _, sp := range []struct {
		key  string
		from qpos
		lo   int
		hi   int
		meat []int
	}{{"sp_32007_27_52", qpos{32001, 29, 3}, 32001, 32010, []int{2205, 1887}}, {"sp_32014_51_9", qpos{32011, 56, 36}, 32011, 32016, []int{1803, 1851}}} {
		n := qNPC(w, "SavePoint", sp.key)
		if n == nil {
			t.Errorf("没有记录点 %s", sp.key)
			continue
		}
		if _, _, ok := qRoute(g, blocked, sp.from, qRange(sp.lo, sp.hi), qNear(n)); !ok {
			t.Errorf("%s：走不到记录点 %d(%d,%d)", sp.key, n.floor, n.x, n.y)
		}
		c, cl := besideNPC(t, w, db, "saver"+sp.key[3:8], n)
		sid := int(c.ID)
		for _, m := range sp.meat {
			giveItem(w, sid, m)
		}
		cl.take()
		w.Talk(sid, n.id)
		win, ok := find[*proto.NpcWindow](cl.take())
		if !ok || win.Buttons != BtnYesNo {
			t.Errorf("%s 带了肉应问是否记录：%+v", sp.key, win)
			continue
		}
		w.NpcWindowReply(sid, n.id, win.Seq, BtnYes, "")
		cl.take()
		var elder int
		w.Do(func() { elder = w.players[sid].char.Elder })
		h, ok := g.Elders[elder]
		if !ok || h.Floor != n.floor {
			t.Errorf("%s 记录后长老编号 %d → %+v", sp.key, elder, h)
		}
		for _, m := range sp.meat {
			if exCount(w, sid, m) != 0 {
				t.Errorf("%s 应收走肉 %d", sp.key, m)
			}
		}
	}
}

// ================= Part B：天空之岛（事件 69..72） =================

func TestQuestSkyIsland(t *testing.T) {
	w, db, g := setup(t)
	blocked := qBlocked(w)
	for _, it := range []int{19621, 19622, 19623, 19624, 1292, 19633, 19634, 19635, 19636, 19637, 19638, 19639, 19640, 19641, 19642, 19643, 19644, 19645, 13061, 13062, 13063, 13064} {
		if g.Items[it] == nil {
			t.Errorf("没有道具 %d", it)
		}
	}
	if e := g.Battle.Enemies[1479]; e == nil || e.TempNo != 718 {
		t.Errorf("祖母送的玛蕾菲雅（敌人 1479）应是模板 718：%+v", e)
	}
	if tp := g.Battle.Temps[718]; tp != nil {
		t.Logf("（提示）玛蕾菲雅模板 718 的 LimitLevel = %d", tp.LimitLevel)
	}
	for _, gid := range []int{834, 835, 836, 837, 838, 839, 840, 841, 842, 843, 844, 845} {
		if g.Battle.Groups[gid] == nil {
			t.Errorf("没有遇敌组 %d", gid)
		}
	}
	for f := 30600; f <= 30699; f++ {
		if g.Floors[f] != nil {
			t.Logf("地图 %d：%s", f, g.Floors[f].Name)
		}
	}

	// ---- 事件 69：愿藏的祖父（2000）接任务 ----
	gpa := qMustNPC(t, w, "ExChangeMan", "king/event69_3", 2000, 50, 48)
	c, cl := besideNPC(t, w, db, "skyhero", gpa)
	id := int(c.ID)
	ev := func(f func(p *entity)) { qEv(w, id, f) }
	bad := false
	exChar(w, id, func(e *entity) { e.char.Level = 100; e.setEventDone(4) })
	win, _ := exTalk(w, cl, id, gpa)
	if win == nil || !strings.Contains(win.Text, "帮我找找新藏") {
		t.Fatalf("祖父应委托找新藏：%+v", win)
	}
	exFinish(w, cl, id, gpa, win)
	ev(func(p *entity) {
		if !p.eventNow(69) {
			qBad(t, &bad, "接下委托应 NOWEV 69：%s", qF(p))
		}
	})
	if bad {
		t.FailNow()
	}

	// ---- 库伊老爷爷（30600）给护身符 ----
	kui := qMustNPC(t, w, "ExChangeMan", "king/event69_1", 30600, 15, 10)
	if _, _, ok := qRoute(g, blocked, qpos{30600, 14, 6}, qRange(30600, 30600), qNear(kui)); !ok {
		t.Errorf("从 300 进 30600 走不到库伊老爷爷")
	}
	qPlace(w, cl, id, qpos{kui.floor, kui.x + 1, kui.y}, 1)
	win, _ = exTalk(w, cl, id, kui)
	if win == nil || !strings.Contains(win.Text, "新藏的年轻人") {
		t.Fatalf("库伊老爷爷应说 AcceptMsg1：%+v", win)
	}
	exFinish(w, cl, id, kui, win)
	if exCount(w, id, 19621) != 1 {
		t.Fatalf("应拿到护身符 19621")
	}

	// ---- 卡卡金宝（30606）：交护身符进洞 ----
	frogGate := qMustNPC(t, w, "WarpMan", "king/event69_2", 30606, 29, 9)
	qPlace(w, cl, id, qpos{30606, 0, 6}, 0)
	qGo(t, w, blocked, id, cl, qRange(30606, 30606), qNear(frogGate), "从 300 进 30606 到卡卡金宝")
	qWarpYes(t, w, id, cl, frogGate, qpos{30601, 86, 34})
	if exCount(w, id, 19621) != 0 {
		t.Errorf("卡卡金宝应收走护身符")
	}

	// ---- 新藏（30602）给金珠 ----
	shinzo := qMustNPC(t, w, "ExChangeMan", "king/event69_4", 30602, 47, 23)
	qGo(t, w, blocked, id, cl, qRange(30601, 30607), qNear(shinzo), "到新藏")
	win, _ = exTalk(w, cl, id, shinzo)
	if win == nil || !strings.Contains(win.Text, "没想到还能遇到其他人") {
		t.Fatalf("新藏应说 AcceptMsg1：%+v", win)
	}
	exFinish(w, cl, id, shinzo, win)
	if exCount(w, id, 19622) != 1 {
		t.Fatalf("应拿到金珠 19622")
	}

	// ---- 里昂蛙王（30605 战斗，add80）→ 30607 → 黑玉 ----
	frogBoss := qNPC(w, "NPCEnemy", "event69_5.arg")
	frogKing := qNPC(w, "ExChangeMan", "event69_6")
	if frogBoss == nil || frogKing == nil {
		t.Fatalf("没有里昂蛙王（event69_5 / event69_6）：%v %v", frogBoss, frogKing)
	}
	qGo(t, w, blocked, id, cl, qRange(30601, 30607), qNear(frogBoss), "到里昂蛙王（战斗）")
	qWantFloor(t, qFight(t, w, id, cl, frogBoss), qpos{30607, 7, 11}, "打赢里昂蛙王")
	if exCount(w, id, 19622) != 1 {
		t.Errorf("里昂蛙王没写 steal，金珠应留着")
	}
	qGo(t, w, blocked, id, cl, qRange(30607, 30607), qNear(frogKing), "到里昂蛙王（交换）")
	win, _ = exTalk(w, cl, id, frogKing)
	if win == nil || !strings.Contains(win.Text, "回来还东西") {
		t.Fatalf("里昂蛙王应说 AcceptMsg1：%+v", win)
	}
	exFinish(w, cl, id, frogKing, win)
	if exCount(w, id, 19623) != 1 || exCount(w, id, 19622) != 0 {
		t.Fatalf("应拿金珠换到黑玉：黑玉 %d 金珠 %d", exCount(w, id, 19623), exCount(w, id, 19622))
	}

	// ---- 回新藏：事件 70（交黑玉）→ 事件 69 结束 ----
	qGo(t, w, blocked, id, cl, qRange(30601, 30607), qNear(shinzo), "回新藏")
	win, _ = exTalk(w, cl, id, shinzo)
	if win == nil || !strings.Contains(win.Text, "真高兴又见到你") {
		t.Fatalf("新藏应说 RequestMsg1（事件 70）：%+v", win)
	}
	exFinish(w, cl, id, shinzo, win)
	ev(func(p *entity) {
		if !p.eventNow(70) || w.countItem(p, 19623) != 0 {
			qBad(t, &bad, "接下事件 70、交出黑玉：%s", qF(p))
		}
	})
	if bad {
		t.FailNow()
	}
	win, _ = exTalk(w, cl, id, shinzo)
	if win == nil || !strings.Contains(win.Text, "吉鲁岛冒险") {
		t.Fatalf("新藏应说 AcceptMsg1（结束 69）：%+v", win)
	}
	exFinish(w, cl, id, shinzo, win)
	ev(func(p *entity) {
		if !p.eventDone(69) || !p.eventNow(70) { // EventNo:-1 的 EndSetFlg 不清 NOWEV 69（原版 NPC_EventSetFlg 也不清）
			qBad(t, &bad, "应 ENDEV 69、NOWEV 70：%s", qF(p))
		}
	})
	if bad {
		t.FailNow()
	}
	// 出洞回 300
	qGo(t, w, blocked, id, cl, qRange(30601, 30607, 300), func(p qpos) bool { return p.f == 300 }, "出洞回 300")

	// ---- 祖父：去找老太婆接玛蕾菲雅 ----
	qPlace(w, cl, id, qpos{gpa.floor, gpa.x + 1, gpa.y}, 1)
	win, _ = exTalk(w, cl, id, gpa)
	if win == nil || !strings.Contains(win.Text, "老太婆") {
		t.Errorf("祖父应叫你去找老太婆：%+v", win)
	}

	// ---- 愿藏的祖母（event69_7）：GetPet 1479、GetItem 19624、EndSetFlg 70 ----
	// 8.0 摆在 2000 (49,47)：那格与旁边要能站
	if f := g.Floors[2000]; !g.Tiles.Walkable(f, 49, 47) || !g.Tiles.Walkable(f, 50, 47) {
		t.Logf("（提示）2000 (49,47) / (50,47) 能不能走：%v %v", g.Tiles.Walkable(f, 49, 47), g.Tiles.Walkable(f, 50, 47))
	}
	{
		gm := &entity{floor: 2000, x: 49, y: 47}
		h := data.Hometowns[1]
		_, end, ok := qRoute(g, blocked, qpos{h.Floor, h.X, h.Y}, qRange(2000, 2006), qNear(gm))
		t.Logf("（提示）从玛丽娜丝村长家 %v 走得到 2000 (49,47) 旁边：%v %v", h, ok, end)
	}
	gma := qNPC(w, "ExChangeMan", "event69_7")
	if gma == nil {
		t.Errorf("断点：没有摆放愿藏的祖母（king/event69_7）——事件 70 只有她会 EndSetFlg:70 并给玛蕾菲雅（1479）、项链 19624；这里改用 GM 的方式补上旗标继续走查")
		mare := enemyPet(t, w, 1479, 79, int64(id))
		ev(func(p *entity) {
			setFlag(&p.char.NowEvents, 70, false)
			p.setEventDone(70)
			w.addPet(p, mare)
		})
		giveItem(w, id, 19624)
	} else {
		qPlace(w, cl, id, qpos{gma.floor, gma.x + 1, gma.y}, 1)
		win, _ = exTalk(w, cl, id, gma)
		if win == nil || !strings.Contains(win.Text, "接走玛蕾菲雅") {
			t.Fatalf("祖母应说 AcceptMsg（给玛蕾）：%+v", win)
		}
		exFinish(w, cl, id, gma, win)
		ev(func(p *entity) {
			mare := false
			for _, pet := range p.pets.Pets {
				mare = mare || pet != nil && pet.TempNo == 718
			}
			if !p.eventDone(70) || !mare || w.countItem(p, 19624) != 1 {
				t.Errorf("祖母应给玛蕾菲雅（模板 718）、项链 19624、ENDEV 70：%v %d %s", mare, w.countItem(p, 19624), qF(p))
			}
		})
	}
	// 宠物转生（精灵王）会 NowEventSetFlg 71，另有测试；这里直接设
	ev(func(p *entity) { p.setEventNow(71) })

	// ---- 事件 71：40 号地图的水池说暗语 → 封印迷宫 ----
	pool := qMustNPC(t, w, "WarpMan", "king/event69_9", 40, 10, 9)
	qPlace(w, cl, id, qpos{pool.floor, pool.x + 1, pool.y}, 1)
	w.Chat(id, "near", "地为界水为凭火为引风为信", 0, 3)
	qWantFloor(t, cl.take(), qpos{30621, 25, 22}, "水池说暗语")
	earth := qMustNPC(t, w, "WarpMan", "king/event69_10", 30620, 21, 15)
	t.Logf("从 30621 能到的楼层：%v", qReach(g, blocked, qpos{30621, 25, 22}, qRange(30600, 30699)))
	qGo(t, w, blocked, id, cl, qRange(30611, 30640), qNear(earth), "封印迷宫到地精灵玛蕾菲雅")
	qWarpYes(t, w, id, cl, earth, qpos{30619, 14, 20})

	marina := qMustNPC(t, w, "ExChangeMan", "king/event69_11", 30619, 19, 17)
	samu := qMustNPC(t, w, "ExChangeMan", "king/event69_12", 30619, 22, 20)
	dark := qMustNPC(t, w, "NPCEnemy", "king/event69_13.arg", 30619, 19, 20)
	reach := qReach(g, blocked, qpos{30619, 14, 20}, qRange(30600, 30699))
	t.Logf("从 30619 能到的楼层：%v", reach)
	for _, f := range []int{30611, 30612, 30613, 30614} {
		if !reach[f] {
			t.Errorf("从 30619 到不了掉材料的 %d", f)
		}
	}
	for _, f := range []int{30611, 30612, 30613, 30614} {
		var from qpos
		for xy := range g.Warps[f] {
			from = qpos{f, xy[0], xy[1]}
			break
		}
		// 从掉材料的楼层（任一传送点旁）走得回玛丽娜丝
		ok := false
		for d := 0; d < 8 && !ok; d++ {
			st := qpos{f, from.x + MOVE[d][0], from.y + MOVE[d][1]}
			if g.Tiles.Walkable(g.Floors[f], st.x, st.y) {
				_, _, ok = qRoute(g, blocked, st, qRange(30611, 30619), qNear(marina))
			}
		}
		if !ok {
			t.Errorf("从 %d 走不回玛丽娜丝", f)
		}
	}
	qGo(t, w, blocked, id, cl, qRange(30619, 30619), qNear(marina), "到玛丽娜丝")
	win, _ = exTalk(w, cl, id, marina)
	if win == nil || !strings.Contains(win.Text, "怎会有人类在这里") {
		t.Fatalf("玛丽娜丝应说 AcceptMsg1：%+v", win)
	}
	exFinish(w, cl, id, marina, win)
	for _, it := range []int{19633, 19634, 19635, 19636} {
		giveItem(w, id, it)
	}
	win, _ = exTalk(w, cl, id, marina)
	if win == nil || !strings.Contains(win.Text, "四种属性的材料") {
		t.Fatalf("四种材料应换四色原石：%+v", win)
	}
	exFinish(w, cl, id, marina, win)
	if exCount(w, id, 19645) != 1 || exCount(w, id, 19633) != 0 {
		t.Fatalf("应拿到四色原石 19645、交出材料")
	}
	qGo(t, w, blocked, id, cl, qRange(30619, 30619), qNear(samu), "到萨姆吉尔")
	win, _ = exTalk(w, cl, id, samu)
	if win == nil || !strings.Contains(win.Text, "再加把劲") {
		t.Errorf("萨姆吉尔应鼓励：%+v", win)
	}
	for _, it := range []int{19637, 19638, 19639, 19640} {
		giveItem(w, id, it)
	}
	qGo(t, w, blocked, id, cl, qRange(30619, 30619), qNear(marina), "回玛丽娜丝")
	win, _ = exTalk(w, cl, id, marina)
	if win == nil || !strings.Contains(win.Text, "完美的灵力护铠") {
		t.Fatalf("高级宝石应换灵力护铠：%+v", win)
	}
	exFinish(w, cl, id, marina, win)
	ev(func(p *entity) {
		if !w.hasItem(p, 1292) || w.hasItem(p, 19645) || !p.eventDone(122) || !p.eventDone(123) {
			qBad(t, &bad, "应拿到灵力护铠 1292、ENDEV 122/123：%s", qF(p))
		}
	})
	if bad {
		t.FailNow()
	}
	// 黑暗精灵王：没装备护铠 → NEWEVENT2 传回原地
	qGo(t, w, blocked, id, cl, qRange(30619, 30619), qNear(dark), "到黑暗精灵王")
	qWantFloor(t, qFight(t, w, id, cl, dark), qpos{30619, 19, 22}, "没穿护铠打赢黑暗精灵王")
	// 装上护铠 → NEWEVENT1 → 天空之岛
	ev(func(p *entity) {
		for i := data.EquipSlots; i < data.ItemSlots; i++ {
			if it := p.inv[i]; it != nil && it.ID == 1292 {
				slot := data.EquipSlot(g.Items[1292].Type)
				p.inv[slot], p.inv[i] = it, p.inv[slot]
				return
			}
		}
	})
	qGo(t, w, blocked, id, cl, qRange(30619, 30619), qNear(dark), "再到黑暗精灵王")
	qWantFloor(t, qFight(t, w, id, cl, dark), qpos{30691, 87, 68}, "穿着护铠打赢黑暗精灵王")

	// ---- 事件 72：天空之岛 ----
	sky := qMustNPC(t, w, "ExChangeMan", "king/event69_14", 30691, 85, 66)
	qGo(t, w, blocked, id, cl, qRange(30691, 30691), qNear(sky), "到天空之岛的地精灵")
	win, _ = exTalk(w, cl, id, sky)
	if win == nil || !strings.Contains(win.Text, "恭喜你成功拯救了尼斯大陆") {
		t.Fatalf("地精灵应委托事件 72：%+v", win)
	}
	exFinish(w, cl, id, sky, win)
	win, _ = exTalk(w, cl, id, sky)
	if win == nil || !strings.Contains(win.Text, "羽毛") {
		t.Fatalf("地精灵应给羽毛：%+v %s", win, qFlags(w, id))
	}
	exFinish(w, cl, id, sky, win)
	ev(func(p *entity) {
		n := 0
		for _, it := range []int{13061, 13062, 13063, 13064} {
			n += w.countItem(p, it)
		}
		if n != 1 || !p.eventDone(71) || !p.eventNow(72) { // NOWEV 71 照原版留着（EventNo:-1）
			qBad(t, &bad, "应拿到一根羽毛、ENDEV 71、NOWEV 72：羽毛 %d %s", n, qF(p))
		}
	})
	if bad {
		t.FailNow()
	}

	guards := []struct {
		battle, talk string
		bfloor       int
		land         qpos
		ring         int
	}{
		{"event69_15.arg", "king/event69_16", 30691, qpos{30696, 16, 14}, 19641},
		{"event69_17.arg", "king/event69_18", 30692, qpos{30697, 18, 14}, 19642},
		{"event69_19.arg", "king/event69_20", 30693, qpos{30698, 14, 14}, 19643},
		{"event69_21.arg", "king/event69_22", 30694, qpos{30699, 17, 14}, 19644},
	}
	for _, gd := range guards {
		b := qMustNPC(t, w, "NPCEnemy", gd.battle, gd.bfloor, 0, 0)
		qGo(t, w, blocked, id, cl, qRange(30689, 30699), qNear(b), "到 "+b.name+"（战斗）")
		qWantFloor(t, qFight(t, w, id, cl, b), gd.land, "打赢 "+b.name)
		tk := qMustNPC(t, w, "ExChangeMan", gd.talk, gd.land.f, 0, 0)
		qGo(t, w, blocked, id, cl, qRange(gd.land.f, gd.land.f), qNear(tk), "到 "+tk.name+"（戒指）")
		win, _ = exTalk(w, cl, id, tk)
		if win == nil {
			t.Fatalf("%s 应有窗口", tk.name)
		}
		exFinish(w, cl, id, tk, win)
		if exCount(w, id, gd.ring) != 1 {
			t.Errorf("%s 应给戒指 %d：%s", tk.name, gd.ring, qFlags(w, id))
		}
	}

	// 柯黑穆肯：30695 的守门 → 30689 迷宫 → 战斗 → 30670
	kGate := qMustNPC(t, w, "WarpMan", "king/event69_26", 30695, 31, 15)
	qGo(t, w, blocked, id, cl, qRange(30689, 30699), qNear(kGate), "从风之界到柯黑穆肯（守门）")
	qWarpYes(t, w, id, cl, kGate, qpos{30689, 12, 34})
	kBoss := qMustNPC(t, w, "NPCEnemy", "event69_23.arg", 30689, 46, 29)
	qGo(t, w, blocked, id, cl, qRange(30689, 30689), qNear(kBoss), "迷宫到柯黑穆肯（战斗）")
	qWantFloor(t, qFight(t, w, id, cl, kBoss), qpos{30670, 15, 9}, "打赢柯黑穆肯")
	kTalk := qMustNPC(t, w, "ExChangeMan", "king/event69_24", 30670, 15, 5)
	qGo(t, w, blocked, id, cl, qRange(30670, 30670), qNear(kTalk), "到柯黑穆肯（结束）")
	win, _ = exTalk(w, cl, id, kTalk)
	if win == nil || !strings.Contains(win.Text, "勇敢的年轻人") {
		t.Fatalf("柯黑穆肯应说 AcceptMsg1：%+v", win)
	}
	exFinish(w, cl, id, kTalk, win)
	ev(func(p *entity) {
		if !p.eventDone(72) { // NOWEV 72 照原版留着（EventNo:-1）
			t.Errorf("应 ENDEV 72：%s", qF(p))
		}
	})
	ev(func(p *entity) {
		got := 0
		for i := data.EquipSlots; i < data.ItemSlots; i++ {
			if it := p.inv[i]; it != nil && it.ID >= 28013 && it.ID <= 28061 {
				got++
				t.Logf("柯黑穆肯给的：%d %s（%s）", it.ID, w.game.Items[it.ID].Name, w.game.Items[it.ID].UseFunc)
			}
		}
		bonda := false
		for _, pet := range p.pets.Pets {
			bonda = bonda || pet != nil && pet.TempNo == w.game.Battle.Enemies[484].TempNo
		}
		if !bonda {
			t.Error("柯黑穆肯应送邦达霍恩（敌人 484）")
		}
		if got != 0 {
			// serv/bin 的 event69_24 被私服改成 GetRandItem 圣兽原石 / 2D 人龙宝石（换私服宠的道具）；
			// serv/data-8.0 是 GetPet:484（邦达霍恩，ThanksMsg 说"送只邦达霍恩"、还写了 PetFullMsg）
			t.Errorf("柯黑穆肯给的是私服的换宠道具（%d 件），官方是 GetPet:484 邦达霍恩", got)
		}
	})
	// 小黑精灵送回 40 号
	imp := qMustNPC(t, w, "WarpMan", "king/event69_25", 30695, 43, 29)
	qGo(t, w, blocked, id, cl, qRange(30670, 30699), qNear(imp), "到小黑精灵")
	qWarpYes(t, w, id, cl, imp, qpos{40, 9, 10})
	ev(func(p *entity) {
		for _, e := range []int{4, 69, 70, 71, 72} {
			if !p.eventDone(e) {
				t.Errorf("最后应 ENDEV %d：%s", e, qF(p))
			}
		}
	})
}
