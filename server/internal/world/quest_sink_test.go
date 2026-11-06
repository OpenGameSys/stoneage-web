//go:build questsink

package world

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 一转前置任务的两条链（精灵王要 39 / 40 / 42 / 46 做完 + 80 级）：
//   40 巴那姆的湖水 / 火（giiru/event/sink）：真红 sink07 REQUEST 40 → 不死鸟 sink01 给羽毛 2706 → 伊拉 sink02 ACCEPT 40 换火的羽毛 2707、EndSetFlg:40
//   42 风（sainasu/event）：威尔 genou_1 REQUEST 41 → 姆哈萨 genou_3 REQUEST 42 → 梦德族长 event12 ACCEPT 41 给风的竖琴 2736、EndSetFlg:41
//       → 威尔 genou_1 ACCEPT 42 换风的锁 2735、EndSetFlg:42
// 前置的 4（成人礼，jaruga/event/event04_1 / event04_2）也照玩家的方式做一遍。

// qsNPC 找参数档正好是 argFile 的 ExChangeMan（findNPC 是子串比对，sink044 会对上 sink044a）。
func qsNPC(t *testing.T, w *World, fn, argFile string) *entity {
	t.Helper()
	var found *entity
	w.Do(func() {
		for _, es := range w.floors {
			for _, e := range es {
				if e.npc == nil || e.npc.Function != fn {
					continue
				}
				for _, a := range e.npc.Args {
					if a == "file:"+argFile {
						found = e
						return
					}
				}
			}
		}
	})
	if found == nil {
		t.Fatalf("数据里没有 %s（%s）", fn, argFile)
	}
	return found
}

// qsStand 让玩家站到 NPC 右边一格、面向它，清掉讯息。
func qsStand(w *World, cl *fakeClient, id int, n *entity) {
	w.Do(func() { w.placeOnFloor(w.players[id], n.floor, n.x+1, n.y, 1) }) // 方向 1 是 (−1,0)，朝 NPC
	cl.take()
}

func qsFlags(w *World, id, ev int) (done, now bool) {
	exChar(w, id, func(e *entity) { done, now = e.eventDone(ev), e.eventNow(ev) })
	return
}

func qsWant(t *testing.T, w *World, id, ev int, done, now bool) {
	t.Helper()
	d, n := qsFlags(w, id, ev)
	if d != done || n != now {
		t.Fatalf("事件 %d：做完=%v 进行中=%v，应为 %v / %v", ev, d, n, done, now)
	}
}

func qsCount(t *testing.T, w *World, id, item, want int) {
	t.Helper()
	if got := exCount(w, id, item); got != want {
		t.Fatalf("道具 %d 应有 %d 个：%d", item, want, got)
	}
}

// qsTalk 和 NPC 说话，窗口要含 want（want 为空时要没有窗口）。
func qsTalk(t *testing.T, w *World, cl *fakeClient, id int, n *entity, want string) *proto.NpcWindow {
	t.Helper()
	qsStand(w, cl, id, n)
	win, msgs := exTalk(w, cl, id, n)
	if want == "" {
		if win != nil {
			t.Fatalf("%s 不应开窗口：%+v", n.name, win)
		}
		return nil
	}
	if win == nil || !strings.Contains(win.Text, want) {
		t.Fatalf("%s 的窗口应含「%s」：%+v / %s", n.name, want, win, fmtMsgs(msgs))
	}
	if strings.Contains(win.Text, `\n`) {
		t.Errorf("%s 的窗口里有没换掉的 \\n：%q", n.name, win.Text)
	}
	return win
}

// qsReply 按一个按钮，下一个窗口要含 want（want 为空时要没有窗口）。
func qsReply(t *testing.T, w *World, cl *fakeClient, id int, n *entity, win *proto.NpcWindow, btn int, want string) (*proto.NpcWindow, []proto.Message) {
	t.Helper()
	if win.Buttons&btn == 0 {
		t.Fatalf("窗口没有按钮 %d：%+v", btn, win)
	}
	next, msgs := exReply(w, cl, id, n, win, btn)
	if want == "" {
		if next != nil {
			t.Fatalf("按 %d 后不应再有窗口：%+v", btn, next)
		}
		return nil, msgs
	}
	if next == nil || !strings.Contains(next.Text, want) {
		t.Fatalf("按 %d 后窗口应含「%s」：%+v / %s", btn, want, next, fmtMsgs(msgs))
	}
	return next, msgs
}

func qsLevel(w *World, id, lv int) {
	exChar(w, id, func(e *entity) { e.char.Level = lv })
}

// qsEvent4 照玩家的方式做完成人礼（事件 4）：审判官 REQUEST → 使者给 15 个仪玉 → 审判官 ACCEPT。
func qsEvent4(t *testing.T, w *World, cl *fakeClient, id int) {
	t.Helper()
	judge := qsNPC(t, w, "ExChangeMan", "jaruga/event/event04_1")
	envoy := qsNPC(t, w, "ExChangeMan", "jaruga/event/event04_2")
	win := qsTalk(t, w, cl, id, judge, "成人")
	win, _ = qsReply(t, w, cl, id, judge, win, BtnYes, "仪之玉")
	qsReply(t, w, cl, id, judge, win, BtnOK, "")
	qsWant(t, w, id, 4, false, true)
	win = qsTalk(t, w, cl, id, envoy, "举行仪式")
	win, _ = qsReply(t, w, cl, id, envoy, win, BtnYes, "仪玉")
	qsReply(t, w, cl, id, envoy, win, BtnOK, "")
	qsCount(t, w, id, 2417, 15)
	win = qsTalk(t, w, cl, id, judge, "见证")
	win, _ = qsReply(t, w, cl, id, judge, win, BtnYes, "成人")
	qsReply(t, w, cl, id, judge, win, BtnOK, "")
	qsCount(t, w, id, 2417, 0)
	qsCount(t, w, id, 2418, 1)
	qsWant(t, w, id, 4, true, false)
}

// ---------------- 资料：地图、道具、敌人、走不走得到 ----------------

type qsEdge struct {
	to   int
	x, y int // 落点
	how  string
}

// qsGraph：楼层之间的传送（mapwarp、npcgen_warp、传送员的 WARP、战斗 NPC 打赢后的 warpfl）。
func qsGraph(w *World) map[int][]qsEdge {
	g := w.game
	out := map[int][]qsEdge{}
	for fl, m := range g.Warps {
		for p, wp := range m {
			out[fl] = append(out[fl], qsEdge{wp.ToFloor, wp.ToX, wp.ToY, fmt.Sprintf("传送点 %d(%d,%d)", fl, p[0], p[1])})
		}
	}
	for fl, ns := range g.NPCs {
		for _, n := range ns {
			switch n.Function {
			case "WarpMan":
				for _, seg := range strings.Split(n.Arg, "|") {
					if !strings.HasPrefix(seg, "WARP:") {
						continue
					}
					for _, pt := range strings.Split(strings.TrimPrefix(seg, "WARP:"), ";") {
						v := strings.Split(pt, ",")
						if len(v) >= 3 {
							free, _ := data.ArgField(n.Arg, "FREE")
							out[fl] = append(out[fl], qsEdge{data.Atoi(v[0]), data.Atoi(v[1]), data.Atoi(v[2]),
								fmt.Sprintf("传送员 %s %d(%d,%d) FREE:%s", n.Name, fl, n.X, n.Y, free)})
						}
					}
				}
			case "NPCEnemy":
				if f := npcArgNum(n.Arg, "warpfl"); f > 0 {
					out[fl] = append(out[fl], qsEdge{f, npcArgNum(n.Arg, "warpx"), npcArgNum(n.Arg, "warpy"),
						fmt.Sprintf("战斗 NPC %s %d(%d,%d)", n.Name, fl, n.X, n.Y)})
				}
			}
		}
	}
	return out
}

// qsRoute：从四个出生村出发，按楼层图找一条到 target 的路，回传每一步的说明（找不到为 nil）。
func qsRoute(graph map[int][]qsEdge, target int) []string {
	type node struct {
		prev int
		how  string
	}
	seen := map[int]node{}
	var q []int
	for _, h := range data.Hometowns {
		seen[h.Floor] = node{-1, "出生村"}
		q = append(q, h.Floor)
	}
	for len(q) > 0 {
		f := q[0]
		q = q[1:]
		if f == target {
			var path []string
			for c := f; c != -1; c = seen[c].prev {
				path = append([]string{fmt.Sprintf("%d ← %s", c, seen[c].how)}, path...)
			}
			return path
		}
		for _, e := range graph[f] {
			if _, ok := seen[e.to]; !ok {
				seen[e.to] = node{f, e.how}
				q = append(q, e.to)
			}
		}
	}
	return nil
}

// qsReach：楼层上从进来的落点出发（不踩传送点）走得到 (x,y) 旁边一格吗。
func qsReach(w *World, graph map[int][]qsEdge, floor, x, y int) (bool, int) {
	g := w.game
	f := g.Floors[floor]
	if f == nil {
		return false, 0
	}
	type pt struct{ x, y int }
	var q []pt
	seen := map[pt]bool{}
	for _, es := range graph {
		for _, e := range es {
			if e.to == floor && f.In(e.x, e.y) {
				p := pt{e.x, e.y}
				if !seen[p] {
					seen[p] = true
					q = append(q, p)
				}
			}
		}
	}
	starts := len(q)
	for len(q) > 0 {
		p := q[0]
		q = q[1:]
		if max(abs(p.x-x), abs(p.y-y)) <= 1 && (p.x != x || p.y != y) {
			return true, starts
		}
		for d := 0; d < 8; d++ {
			n := pt{p.x + MOVE[d][0], p.y + MOVE[d][1]}
			if seen[n] || !f.In(n.x, n.y) || !g.Tiles.Walkable(f, n.x, n.y) {
				continue
			}
			seen[n] = true
			if _, warp := g.WarpAt(floor, n.x, n.y); warp {
				continue
			}
			q = append(q, n)
		}
	}
	return false, starts
}

// NPC 摆放、NpcWarp 的目的地、道具、敌人都在；每个任务 NPC 所在的楼层从出生村走得到、NPC 旁边站得到人。
func TestQuestSinkData(t *testing.T) {
	w, _, g := setup(t)
	graph := qsGraph(w)
	files := []string{
		"jaruga/event/event04_1", "jaruga/event/event04_2",
		"giiru/event/sink/sink07", "giiru/event/sink/sink01", "giiru/event/sink/sink02", "giiru/event/sink/sink06",
		"giiru/event/sink/sink03", "giiru/event/sink/sink044", "giiru/event/sink/sink044a", "giiru/event/sink/sink044b",
		"giiru/event/sink/sink044c", "giiru/event/sink/sink055", "giiru/event/sink/sink055a",
		"sainasu/event/genou_1", "sainasu/event/genou_2", "sainasu/event/genou_3", "sainasu/event/genou_4", "sainasu/event/event12",
	}
	for _, f := range files {
		n := qsNPC(t, w, "ExChangeMan", f)
		fl := g.Floors[n.floor]
		route := qsRoute(graph, n.floor)
		ok, starts := qsReach(w, graph, n.floor, n.x, n.y)
		t.Logf("%-28s %s 在 %d %s (%d,%d)；站得到旁边=%v（%d 个落点）", f, n.name, n.floor, fl.Name, n.x, n.y, ok, starts)
		if route == nil {
			t.Errorf("%s：从出生村到不了 %d", f, n.floor)
		} else {
			t.Logf("    路线：%s", strings.Join(route, " → "))
		}
		if !ok {
			t.Errorf("%s：楼层 %d 上走不到 NPC (%d,%d) 旁边", f, n.floor, n.x, n.y)
		}
		// NpcWarp 的每个目的地都要有地图、在范围内
		var arg string
		w.Do(func() { arg = n.npc.Arg })
		for _, b := range strings.Split(arg, "EventEnd") {
			if spec, ok := data.ArgField(b, "NpcWarp"); ok {
				for _, s := range strings.Split(spec, ",") {
					v := strings.Split(s, ".")
					if len(v) < 3 {
						t.Errorf("%s：NpcWarp 写错 %q", f, s)
						continue
					}
					tf := g.Floors[data.Atoi(v[0])]
					if tf == nil || !tf.In(data.Atoi(v[1]), data.Atoi(v[2])) {
						t.Errorf("%s：NpcWarp 的目的地 %s 没有地图或超出范围", f, s)
					}
				}
			}
			// 段里提到的道具都要有定义
			for _, key := range []string{"GetItem", "DelItem", "GetRandItem"} {
				if spec, ok := data.ArgField(b, key); ok {
					for _, tok := range strings.Split(spec, ",") {
						id, _, _ := strings.Cut(tok, "*")
						if strings.TrimSpace(id) == "" {
							t.Logf("    %s：%s 有空的编号（%q，atoi 成 0）", f, key, spec)
							continue
						}
						if g.Items[data.Atoi(id)] == nil {
							t.Errorf("%s：%s 的道具 %s 不存在", f, key, id)
						}
					}
				}
			}
		}
	}
	// 悟、唐（sb_go / sb_dan）：梦德族长那层的战斗 NPC，打赢传送到 11107 (23,35)
	for _, f := range []string{"100/sb_go.arg", "100/sb_dan.arg"} {
		n := qsNPC(t, w, "NPCEnemy", f)
		var arg string
		w.Do(func() { arg = n.npc.Arg })
		for _, s := range enemyNo(arg, 64) {
			if g.Battle.Enemies[data.Atoi(s)] == nil {
				t.Errorf("%s 的敌人 %s 不存在", f, s)
			}
		}
		t.Logf("%s %s 在 %d (%d,%d) 敌人 %v warp %d(%d,%d)", f, n.name, n.floor, n.x, n.y, enemyNo(arg, 64),
			npcArgNum(arg, "warpfl"), npcArgNum(arg, "warpx"), npcArgNum(arg, "warpy"))
	}
	// 打赢悟 / 唐的落点 (23,35) 走得到族长 (24,6) 旁边吗
	chief := qsNPC(t, w, "ExChangeMan", "sainasu/event/event12")
	if ok := qsReachFrom(w, 11107, 23, 35, chief.x, chief.y); !ok {
		t.Errorf("11107 (23,35) 走不到族长 (%d,%d) 旁边", chief.x, chief.y)
	}
	// 巴那姆的水壶 2708 在哪里买：ss_30504_10_11（EVENT 店，要说 buy_msg）
	shop := findNPC(t, w, "ItemShop", "ss_30504_10_11")
	if r := qsRoute(graph, shop.floor); r == nil {
		t.Errorf("水壶店的楼层 %d 到不了", shop.floor)
	} else {
		t.Logf("水壶店 %s 在 %d (%d,%d)；路线：%s", shop.name, shop.floor, shop.x, shop.y, strings.Join(r, " → "))
	}
	// 掉落：哪些敌人掉 2706 / 2708 / 2736 / 2758（不必要，只是记一下）
	for _, it := range []int{2706, 2708, 2736, 2758, 2417} {
		var who []string
		for id, e := range g.Battle.Enemies {
			for k, d := range e.Items {
				if d == it && e.ItemProbs[k] > 0 {
					who = append(who, fmt.Sprintf("%d", id))
				}
			}
		}
		sort.Strings(who)
		t.Logf("掉落 %d 的敌人：%v", it, who)
	}
}

// qsReachFrom：同一层从 (sx,sy) 走得到 (x,y) 旁边吗（不踩传送点）。
func qsReachFrom(w *World, floor, sx, sy, x, y int) bool {
	g := w.game
	f := g.Floors[floor]
	type pt struct{ x, y int }
	q := []pt{{sx, sy}}
	seen := map[pt]bool{{sx, sy}: true}
	for len(q) > 0 {
		p := q[0]
		q = q[1:]
		if max(abs(p.x-x), abs(p.y-y)) <= 1 && (p.x != x || p.y != y) {
			return true
		}
		for d := 0; d < 8; d++ {
			n := pt{p.x + MOVE[d][0], p.y + MOVE[d][1]}
			if seen[n] || !f.In(n.x, n.y) || !g.Tiles.Walkable(f, n.x, n.y) {
				continue
			}
			seen[n] = true
			if _, warp := g.WarpAt(floor, n.x, n.y); warp {
				continue
			}
			q = append(q, n)
		}
	}
	return false
}

// ---------------- 事件 40 ----------------

func TestQuestSink40(t *testing.T) {
	w, db, g := setup(t)
	shinku := qsNPC(t, w, "ExChangeMan", "giiru/event/sink/sink07")
	phoenix := qsNPC(t, w, "ExChangeMan", "giiru/event/sink/sink01")
	ira := qsNPC(t, w, "ExChangeMan", "giiru/event/sink/sink02")
	envoy := qsNPC(t, w, "ExChangeMan", "giiru/event/sink/sink06")
	c, cl := besideNPC(t, w, db, "firequest", shinku)
	id := int(c.ID)
	qsLevel(w, id, 80)

	// 没做成人礼：真红所有的段都要 ENDEV=4，只随机说 NomalMainMsg
	qsStand(w, cl, id, shinku)
	if win, msgs := exTalk(w, cl, id, shinku); win != nil {
		t.Fatalf("没有 4 时真红不应开窗口：%+v", win)
	} else if say, ok := find[*proto.NpcTalk](msgs); !ok || !strings.Contains(say.Text, "吉鲁") && !strings.Contains(say.Text, "恐怖") {
		t.Fatalf("应说 NomalMainMsg：%s", fmtMsgs(msgs))
	}
	// 没接任务时伊拉只说最后一段
	qsTalk(t, w, cl, id, ira, "巴那姆壶")
	qsEvent4(t, w, cl, id)

	// 1. 真红 REQUEST 40：RequestMsg（是/否）→ ThanksMsg1..3（下一页、下一页、确定）
	win := qsTalk(t, w, cl, id, shinku, "精灵仪式的人吗")
	if win.Buttons != BtnYesNo {
		t.Fatalf("RequestMsg 应是是/否：%+v", win)
	}
	win, _ = qsReply(t, w, cl, id, shinku, win, BtnYes, "果然没错")
	qsWant(t, w, id, 40, false, true)
	win, _ = qsReply(t, w, cl, id, shinku, win, BtnNext, "火之女神伊拉")
	win, _ = qsReply(t, w, cl, id, shinku, win, BtnNext, "不死鸟的羽毛")
	qsReply(t, w, cl, id, shinku, win, BtnOK, "")
	// 再说话：进行中问要不要放弃，按否不放弃
	win = qsTalk(t, w, cl, id, shinku, "不想接受精灵仪式")
	qsReply(t, w, cl, id, shinku, win, BtnNo, "伊拉大人在看着你")
	qsWant(t, w, id, 40, false, true)

	// 伊拉：没有羽毛时说要羽毛；火的使者也一样
	qsTalk(t, w, cl, id, ira, "请将不死鸟的羽毛带给我")
	qsTalk(t, w, cl, id, envoy, "不死鸟羽毛好像没带")

	// 2. 不死鸟（20309）：ACCEPT 给羽毛 2706，之后换位置（NpcWarp 的第一个 20309.18.13）
	if phoenix.floor != 20309 || phoenix.x != 46 || phoenix.y != 45 {
		t.Fatalf("不死鸟应在 20309 (46,45)：%d (%d,%d)", phoenix.floor, phoenix.x, phoenix.y)
	}
	win = qsTalk(t, w, cl, id, phoenix, "收集我的羽毛")
	win, _ = qsReply(t, w, cl, id, phoenix, win, BtnYes, "带着我的羽毛去吧")
	qsReply(t, w, cl, id, phoenix, win, BtnOK, "")
	qsCount(t, w, id, 2706, 1)
	var pf, px, py int
	w.Do(func() { pf, px, py = phoenix.floor, phoenix.x, phoenix.y })
	if pf != 20309 || px != 18 || py != 13 {
		t.Fatalf("不死鸟给完羽毛应换到 20309 (18,13)：%d (%d,%d)", pf, px, py)
	}
	qsTalk(t, w, cl, id, phoenix, "将我的羽毛带在身上")
	qsTalk(t, w, cl, id, shinku, "不死鸟的羽毛")
	qsTalk(t, w, cl, id, envoy, "这只羽毛是不死鸟的羽毛")

	// 3. 伊拉（30016）：ACCEPT 40 交出羽毛、拿到火的羽毛 2707、魅力 +1、EndSetFlg:40
	var charm int
	exChar(w, id, func(e *entity) { charm = e.char.Charm })
	win = qsTalk(t, w, cl, id, ira, "控制住了深红的洞窟")
	win, _ = qsReply(t, w, cl, id, ira, win, BtnYes, "征服火者")
	qsReply(t, w, cl, id, ira, win, BtnOK, "")
	qsCount(t, w, id, 2706, 0)
	qsCount(t, w, id, 2707, 1)
	qsWant(t, w, id, 40, true, false)
	exChar(w, id, func(e *entity) {
		if e.char.Charm != min(100, charm+1) {
			t.Errorf("魅力应 +1：%d → %d", charm, e.char.Charm)
		}
	})
	qsTalk(t, w, cl, id, ira, "你已经是火之征服者")
	qsTalk(t, w, cl, id, shinku, "得到伊拉大人的承认")
	qsTalk(t, w, cl, id, envoy, "火之征服者")
	// 做完之后不死鸟只说第一段
	qsTalk(t, w, cl, id, phoenix, "驯服所有精灵")
	_ = g
}

// 放弃任务 40 再接：放弃时 EndStopMsg、清掉进行中、魅力 −1。
func TestQuestSink40Abandon(t *testing.T) {
	w, db, _ := setup(t)
	shinku := qsNPC(t, w, "ExChangeMan", "giiru/event/sink/sink07")
	c, cl := besideNPC(t, w, db, "firequit", shinku)
	id := int(c.ID)
	qsLevel(w, id, 80)
	exChar(w, id, func(e *entity) { e.setEventDone(4); e.char.Charm = 50 })
	win := qsTalk(t, w, cl, id, shinku, "精灵仪式的人吗")
	win, _ = qsReply(t, w, cl, id, shinku, win, BtnYes, "果然没错")
	qsWant(t, w, id, 40, false, true)
	win = qsTalk(t, w, cl, id, shinku, "不想接受精灵仪式")
	qsReply(t, w, cl, id, shinku, win, BtnYes, "不要太过于相信自己的力量")
	qsWant(t, w, id, 40, false, false)
	exChar(w, id, func(e *entity) {
		if e.char.Charm != 49 {
			t.Errorf("放弃委托魅力 −1：%d", e.char.Charm)
		}
	})
}

// 巴那姆的水壶（不是 40 必要的，伊拉第二段 / 火的使者的奖品与 177 的火之守护）：
// 水壶店（ss_30504_10_11，EVENT 店）说 buy_msg 买 2708 → 湖边老人说"很多"换 2709 → 多洛加 sink055 换 2712 →
// 多洛加 sink044 换 2717 → 火的使者换随机奖品；sink044 拿空壶以外的换回空壶。
func TestQuestSinkPot(t *testing.T) {
	w, db, g := setup(t)
	shop := findNPC(t, w, "ItemShop", "ss_30504_10_11")
	old := qsNPC(t, w, "ExChangeMan", "giiru/event/sink/sink03")
	d044 := qsNPC(t, w, "ExChangeMan", "giiru/event/sink/sink044")
	d055 := qsNPC(t, w, "ExChangeMan", "giiru/event/sink/sink055")
	envoy := qsNPC(t, w, "ExChangeMan", "giiru/event/sink/sink06")
	ira := qsNPC(t, w, "ExChangeMan", "giiru/event/sink/sink02")
	c, cl := besideNPC(t, w, db, "potquest", shop)
	id := int(c.ID)
	qsLevel(w, id, 80)
	exChar(w, id, func(e *entity) { e.char.Gold = 100000 })

	// 点选只说 sellonly_msg；说 buy_msg 的话才打开买的清单
	qsStand(w, cl, id, shop)
	w.Talk(id, shop.id)
	msgs := cl.take()
	if _, ok := find[*proto.ShopWindow](msgs); ok {
		t.Fatal("EVENT 店点选不应开清单")
	}
	buyMsg, _ := data.ArgField(shop.npc.Arg, "buy_msg")
	w.Chat(id, "near", buyMsg, 0, 3)
	buy, ok := find[*proto.ShopWindow](cl.take())
	if !ok || buy.Mode != "buy" || len(buy.Items) != 1 || buy.Items[0].Name != g.Items[2708].Name {
		t.Fatalf("说 buy_msg 应打开只有水壶的清单：%+v", buy)
	}
	w.NpcWindowReply(id, shop.id, buy.Seq, 0, "1|1")
	cl.take()
	qsCount(t, w, id, 2708, 1)

	// 湖边老人：点选是说明；说"很多"→ 2709
	qsTalk(t, w, cl, id, old, "告诉我需要多少")
	w.Chat(id, "near", "很多", 0, 3)
	win, _ := find[*proto.NpcWindow](cl.take())
	if win == nil || !strings.Contains(win.Text, "想要很多") {
		t.Fatalf("说很多应是那段的 AcceptMsg：%+v", win)
	}
	win, _ = qsReply(t, w, cl, id, old, win, BtnYes, "装得满满的")
	qsReply(t, w, cl, id, old, win, BtnOK, "")
	qsCount(t, w, id, 2708, 0)
	qsCount(t, w, id, 2709, 1)

	// 多洛加 sink055：2709 → 2712，换位置
	win = qsTalk(t, w, cl, id, d055, "要喝壶里的水吗")
	win, _ = qsReply(t, w, cl, id, d055, win, BtnYes, "恐龙喝完水后离开了")
	qsReply(t, w, cl, id, d055, win, BtnOK, "")
	qsCount(t, w, id, 2709, 0)
	qsCount(t, w, id, 2712, 1)
	var f, x, y int
	w.Do(func() { f, x, y = d055.floor, d055.x, d055.y })
	if f != 30001 || x != 10 || y != 6 {
		t.Errorf("sink055 应换到 30001 (10,6)：%d (%d,%d)", f, x, y)
	}

	// 多洛加 sink044：2712 → 2717
	win = qsTalk(t, w, cl, id, d044, "要喝水壶中的水吗")
	win, _ = qsReply(t, w, cl, id, d044, win, BtnYes, "恐龙喝了水后离开了")
	qsReply(t, w, cl, id, d044, win, BtnOK, "")
	qsCount(t, w, id, 2712, 0)
	qsCount(t, w, id, 2717, 1)

	// 伊拉：没有 177 时拿着 2717 是第 8 段的 MESSAGE（ENDEV=40 那组或单独 ITEM=2717 那组）
	qsTalk(t, w, cl, id, ira, "火之征服者")

	// 火的使者：交出 2717 拿随机奖品
	win = qsTalk(t, w, cl, id, envoy, "巴那姆之壶")
	win, _ = qsReply(t, w, cl, id, envoy, win, BtnYes, "赞美")
	_, msgs = qsReply(t, w, cl, id, envoy, win, BtnOK, "")
	qsCount(t, w, id, 2717, 0)
	got := 0
	for _, it := range []int{2693, 2696, 256, 459, 1460} {
		got += exCount(w, id, it)
	}
	if got != 1 {
		t.Fatalf("应拿到 GetRandItem 的一个：%s", fmtMsgs(msgs))
	}

	// 有 2710（一半）时 sink044 的第 4 段换回空壶
	giveItem(w, id, 2710)
	win = qsTalk(t, w, cl, id, d044, "要喝水壶中的水吗")
	win, _ = qsReply(t, w, cl, id, d044, win, BtnYes, "水壶的水好像都没有了")
	qsReply(t, w, cl, id, d044, win, BtnOK, "")
	qsCount(t, w, id, 2710, 0)
	qsCount(t, w, id, 2708, 1)
}

// ---------------- 事件 42 ----------------

func TestQuestSink42(t *testing.T) {
	w, db, _ := setup(t)
	wil := qsNPC(t, w, "ExChangeMan", "sainasu/event/genou_1")
	muhasa := qsNPC(t, w, "ExChangeMan", "sainasu/event/genou_3")
	chief := qsNPC(t, w, "ExChangeMan", "sainasu/event/event12")
	keeper := qsNPC(t, w, "ExChangeMan", "sainasu/event/genou_4")
	envoy := qsNPC(t, w, "ExChangeMan", "sainasu/event/genou_2")
	c, cl := besideNPC(t, w, db, "windquest", wil)
	id := int(c.ID)
	qsLevel(w, id, 80)

	// 没有 4：威尔只说最后一段；姆哈萨也是最后一段
	qsTalk(t, w, cl, id, wil, "你现在还不能控制风")
	qsTalk(t, w, cl, id, muhasa, "突然被他们包围住")
	qsTalk(t, w, cl, id, chief, "") // 族长没有成立的段：说 NomalMainMsg
	qsEvent4(t, w, cl, id)

	// 1. 威尔 REQUEST 41
	win := qsTalk(t, w, cl, id, wil, "我是威尔")
	win, _ = qsReply(t, w, cl, id, wil, win, BtnYes, "风之竖琴")
	qsReply(t, w, cl, id, wil, win, BtnOK, "")
	qsWant(t, w, id, 41, false, true)

	// 41 进行中、42 还没接：族长没有成立的段
	qsTalk(t, w, cl, id, chief, "")

	// 2. 姆哈萨（100 (346,356)）REQUEST 42
	win = qsTalk(t, w, cl, id, muhasa, "谢谢你在我危急的时候救我")
	win, _ = qsReply(t, w, cl, id, muhasa, win, BtnYes, "胖特族")
	qsReply(t, w, cl, id, muhasa, win, BtnOK, "")
	qsWant(t, w, id, 42, false, true)
	qsWant(t, w, id, 41, false, true)
	qsTalk(t, w, cl, id, muhasa, "真的非常感谢你")

	// 2.5 悟 / 唐：打赢传送到 11107 (23,35)
	gou := qsNPC(t, w, "NPCEnemy", "100/sb_go.arg")
	qsStand(w, cl, id, gou)
	talkYes(t, w, id, cl, gou)
	msgs := npcEnemyWin(t, w, id, cl)
	if ef, ok := find[*proto.EnterFloor](msgs); !ok || ef.Floor != 11107 || ef.X != 23 || ef.Y != 35 {
		t.Fatalf("打赢悟应传送到 11107 (23,35)：%+v", ef)
	}

	// 3. 族长 ACCEPT 41：拿到风的竖琴 2736、41 做完、42 还在进行中
	win = qsTalk(t, w, cl, id, chief, "我的事就是风的竖琴啊")
	win, _ = qsReply(t, w, cl, id, chief, win, BtnYes, "打倒那边的悟跟唐的人")
	qsReply(t, w, cl, id, chief, win, BtnOK, "")
	qsCount(t, w, id, 2736, 1)
	qsWant(t, w, id, 41, true, false)
	qsWant(t, w, id, 42, false, true)
	qsTalk(t, w, cl, id, chief, "帮特一族")

	// 竖琴丢了：族长第 3 段再给一次
	exChar(w, id, func(e *entity) {
		for i, it := range e.inv {
			if it != nil && it.ID == 2736 {
				e.inv[i] = nil
			}
		}
	})
	win = qsTalk(t, w, cl, id, chief, "那就是风之竖琴的事")
	win, _ = qsReply(t, w, cl, id, chief, win, BtnYes, "打倒那边的刚跟丹的人")
	qsReply(t, w, cl, id, chief, win, BtnOK, "")
	qsCount(t, w, id, 2736, 1)

	// 看守人（11206）给风的手镯 2758（不是 42 必要的）
	win = qsTalk(t, w, cl, id, keeper, "风之手环")
	win, _ = qsReply(t, w, cl, id, keeper, win, BtnYes, "神圣使者")
	qsReply(t, w, cl, id, keeper, win, BtnOK, "")
	qsCount(t, w, id, 2758, 1)
	qsTalk(t, w, cl, id, keeper, "你好像已经得到")

	// 风的使者：42 还没做完时是"请你完成"
	qsTalk(t, w, cl, id, envoy, "请你完成风之女神威尔大人")

	// 4. 威尔 ACCEPT 42：交出竖琴、拿到风的锁 2735、42 做完
	win = qsTalk(t, w, cl, id, wil, "把风之竖琴给拿回来了")
	win, _ = qsReply(t, w, cl, id, wil, win, BtnYes, "征服风的人")
	qsReply(t, w, cl, id, wil, win, BtnOK, "")
	qsCount(t, w, id, 2736, 0)
	qsCount(t, w, id, 2735, 1)
	qsWant(t, w, id, 42, true, false)
	qsWant(t, w, id, 41, true, false)
	qsTalk(t, w, cl, id, wil, "需要莫大的努力与辛劳")
	qsTalk(t, w, cl, id, envoy, "帮忙把风之竖琴找回来")
}

// 身上带着布鲁顿（模板 95）时，威尔的第 2 段（LV>0&PET>0-95）抢在 REQUEST 41 前面：接不到任务（原版数据同样如此）。
func TestQuestSink42Bruton(t *testing.T) {
	w, db, g := setup(t)
	wil := qsNPC(t, w, "ExChangeMan", "sainasu/event/genou_1")
	c, cl := besideNPC(t, w, db, "brutonowner", wil)
	id := int(c.ID)
	qsLevel(w, id, 80)
	exChar(w, id, func(e *entity) { e.setEventDone(4) })
	var ene *data.Enemy
	for _, e := range g.Battle.Enemies {
		if e.TempNo == 95 {
			ene = e
			break
		}
	}
	if ene == nil {
		t.Skip("没有模板 95")
	}
	t.Logf("模板 95：%s", g.Battle.Temps[95].Name)
	givePet(t, w, id, ene, 10)
	qsTalk(t, w, cl, id, wil, "御风恐龙给带来了")
	qsWant(t, w, id, 41, false, false)
}

// qsWarpMan 找 floor 上 (x,y) 的传送员。
func qsWarpMan(t *testing.T, w *World, floor, x, y int) *entity {
	t.Helper()
	var found *entity
	w.Do(func() {
		for _, e := range w.floors[floor] {
			if e.npc != nil && e.npc.Function == "WarpMan" && e.x == x && e.y == y {
				found = e
			}
		}
	})
	if found == nil {
		t.Fatalf("%d (%d,%d) 没有传送员", floor, x, y)
	}
	return found
}

// 路上要过的关：守人（ENDEV=4 + 等级）、仪的值班者（LV>29）、风的守护兽（打赢传送到 11215 (9,16)）。
func TestQuestSinkGates(t *testing.T) {
	w, db, _ := setup(t)
	gates := []struct {
		floor, x, y int
		lv          int
		to          int
	}{
		{10202, 31, 13, 30, 10204}, // 仪的值班者 → 成人礼
		{30502, 45, 41, 65, 30001}, // 守人 → 深红的洞窟（伊拉）
		{30503, 26, 5, 65, 30015},  // 守人 → 深红的洞窟另一边
		{100, 278, 325, 70, 11201}, // 守人 → 玄黄的洞窟（威尔）
	}
	for i, gt := range gates {
		n := qsWarpMan(t, w, gt.floor, gt.x, gt.y)
		c, cl := besideNPC(t, w, db, fmt.Sprintf("gate%d", i), n)
		id := int(c.ID)
		qsLevel(w, id, gt.lv)
		exChar(w, id, func(e *entity) { e.setEventDone(4) })
		qsStand(w, cl, id, n)
		w.Talk(id, n.id)
		win, ok := find[*proto.NpcWindow](cl.take())
		if !ok || win.Buttons&BtnYes == 0 {
			t.Fatalf("%s %d(%d,%d) 条件够时应问要不要过去：%+v", n.name, gt.floor, gt.x, gt.y, win)
		}
		w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
		ef, ok := find[*proto.EnterFloor](cl.take())
		if !ok || ef.Floor != gt.to {
			t.Fatalf("%s 应传送到 %d：%+v", n.name, gt.to, ef)
		}
		t.Logf("%s %d(%d,%d) → %d (%d,%d)", n.name, gt.floor, gt.x, gt.y, ef.Floor, ef.X, ef.Y)
		// 等级差一级：不传送
		qsLevel(w, id, gt.lv-1)
		qsStand(w, cl, id, n)
		w.Talk(id, n.id)
		if win, ok := find[*proto.NpcWindow](cl.take()); ok && gt.lv > 1 {
			w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
			if ef, moved := find[*proto.EnterFloor](cl.take()); moved {
				t.Fatalf("%s 等级不够不应传送：%+v", n.name, ef)
			}
		}
	}
	// 盗贼（100 (443,127)，暗语传送员）：说"在卡坦有梦德"（村姑 event10、卡坦的年轻人 event11 会提到）才到盗贼的基地 11101
	thief := qsWarpMan(t, w, 100, 443, 127)
	{
		c, cl := besideNPC(t, w, db, "thiefgate", thief)
		qsStand(w, cl, int(c.ID), thief) // 不先点选，直接对着它说暗语
		w.Chat(int(c.ID), "near", "在卡坦有梦德", 0, 3)
		m := cl.take()
		if ef, ok := find[*proto.EnterFloor](m); !ok || ef.Floor != 11101 {
			t.Fatalf("说出暗语应传送到 11101：%+v %s", ef, fmtMsgs(m))
		}
	}
	// 风的守护兽（11214）：打赢传送到 11215 (9,16)
	n := qsNPC(t, w, "NPCEnemy", "100/sb_kgi.arg")
	c, cl := besideNPC(t, w, db, "windguard", n)
	id := int(c.ID)
	qsLevel(w, id, 80)
	qsStand(w, cl, id, n)
	talkYes(t, w, id, cl, n)
	msgs := npcEnemyWin(t, w, id, cl)
	if ef, ok := find[*proto.EnterFloor](msgs); !ok || ef.Floor != 11215 || ef.X != 9 || ef.Y != 16 {
		t.Fatalf("打赢风的守护兽应传送到 11215 (9,16)：%+v", ef)
	}
}

// 伊拉的火之守护（177 进行中的支线，不是 40 必要的）：LV>79、ENDEV=177、ENDEV=4、带 2717 → MESSAGE 段交出 2717 拿 19292。
func TestQuestSinkFireGuard(t *testing.T) {
	w, db, _ := setup(t)
	ira := qsNPC(t, w, "ExChangeMan", "giiru/event/sink/sink02")
	c, cl := besideNPC(t, w, db, "fireguard", ira)
	id := int(c.ID)
	qsLevel(w, id, 80)
	exChar(w, id, func(e *entity) { e.setEventDone(4); e.setEventDone(177) })
	giveItem(w, id, 2717)
	qsTalk(t, w, cl, id, ira, "做的好")
	qsCount(t, w, id, 2717, 0)
	qsCount(t, w, id, 19292, 1)
	// 拿了守护之后：要 ITEM!=19292 的几段都不成立，落到最后一段（没有 40 时）
	qsTalk(t, w, cl, id, ira, "你怎么来到了这儿")
}

// 风的使者（genou_2）：42 做完后带布鲁顿（模板 95）与风的手镯 2758 换奖品。第一段 EVENT 原版就写坏了
// （"…&PET>60-95,&ITEM=2758LV>0&PET>59-95&ITEM=2758"：第 1 组没有 ITEM=2758，第 2 组以空条件开头永远不成立），
// 所以 61 级以上的布鲁顿不带手镯也能换。
func TestQuestSinkWindEnvoy(t *testing.T) {
	w, db, g := setup(t)
	envoy := qsNPC(t, w, "ExChangeMan", "sainasu/event/genou_2")
	c, cl := besideNPC(t, w, db, "windenvoy", envoy)
	id := int(c.ID)
	qsLevel(w, id, 80)
	exChar(w, id, func(e *entity) { e.setEventDone(4); e.setEventDone(42) })
	var ene *data.Enemy
	for _, e := range g.Battle.Enemies {
		if e.TempNo == 95 {
			ene = e
			break
		}
	}
	if ene == nil {
		t.Skip("没有模板 95")
	}
	givePet(t, w, id, ene, 50)
	giveItem(w, id, 2758)
	win := qsTalk(t, w, cl, id, envoy, "奉献给风之神殿吗")
	win, _ = qsReply(t, w, cl, id, envoy, win, BtnYes, "挺不错")
	qsReply(t, w, cl, id, envoy, win, BtnOK, "")
	qsCount(t, w, id, 2758, 0)
	exChar(w, id, func(e *entity) {
		for _, p := range e.pets.Pets {
			if p != nil {
				t.Errorf("布鲁顿应被收走：%s", p.Name)
			}
		}
	})
	// 第一段：61 级的布鲁顿、不带手镯也成立
	givePet(t, w, id, ene, 61)
	qsTalk(t, w, cl, id, envoy, "奉献给风之神殿吗")
}
