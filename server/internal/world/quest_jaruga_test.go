//go:build questjaruga

package world

import (
	"fmt"
	"strings"
	"testing"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 探查：任务相关楼层上的 NPC、道具从哪里来、从村子走得到哪些楼层。
func TestJarugaExplore(t *testing.T) {
	w, _, g := setup(t)
	floors := map[int]bool{3413: true, 3400: true, 21015: true, 21014: true, 21003: true, 21215: true, 21214: true, 21308: true, 31302: true, 31301: true, 21301: true}
	w.Do(func() {
		for fl := range floors {
			for _, e := range w.floors[fl] {
				if e.npc == nil {
					continue
				}
				t.Logf("NPC %d(%d,%d) %s fn=%s type=%s args=%v", fl, e.x, e.y, e.npc.Name, e.npc.Function, e.npc.Type, e.npc.Args)
			}
			if f := g.Floors[fl]; f != nil {
				t.Logf("floor %d %q %dx%d", fl, f.Name, f.W, f.H)
			} else {
				t.Logf("floor %d MISSING", fl)
			}
		}
	})
	// 道具来源
	for _, it := range []int{2698, 2699, 2700, 2701, 2766, 2767, 2768, 2769, 2770} {
		for _, e := range g.Battle.Enemies {
			for i, d := range e.Items {
				if d == it {
					var where []string
					for _, gr := range g.Battle.Groups {
						for _, id := range gr.Enemies {
							if id == e.ID {
								for _, a := range g.Battle.Areas {
									for _, ag := range a.Groups {
										if ag == gr.ID {
											where = append(where, fmt.Sprintf("group %d(%s appear=%d hide=%d) area %d floor %d [%d,%d-%d,%d] z=%d", gr.ID, gr.Name, gr.AppearItem, gr.HideItem, a.Index, a.Floor, a.X1, a.Y1, a.X2, a.Y2, a.ZOrder))
										}
									}
								}
								where = append(where, fmt.Sprintf("group %d(%s)", gr.ID, gr.Name))
							}
						}
					}
					t.Logf("item %d dropped by enemy %d %s lv%d-%d prob %d‰; %v", it, e.ID, e.Name, e.LvMin, e.LvMax, e.ItemProbs[i], where)
				}
			}
		}
		w.Do(func() {
			for _, es := range w.floors {
				for _, e := range es {
					if e.npc != nil && e.npc.Function == "NPCEnemy" && strings.Contains(e.npc.Arg, fmt.Sprint(it)) {
						t.Logf("item %d mentioned by NPCEnemy %d(%d,%d) %s: %s", it, e.floor, e.x, e.y, e.npc.Name, e.npc.Arg)
					}
				}
			}
		})
	}
	// 可达性：静态传送点 + 传送员（不看条件，记下条件）
	type edge struct {
		to   int
		desc string
	}
	adj := map[int][]edge{}
	for fl, m := range g.Warps {
		for k, wp := range m {
			adj[fl] = append(adj[fl], edge{wp.ToFloor, fmt.Sprintf("warp %d(%d,%d)->%d(%d,%d) %s", fl, k[0], k[1], wp.ToFloor, wp.ToX, wp.ToY, wp.Source)})
		}
	}
	w.Do(func() {
		for fl, es := range w.floors {
			for _, e := range es {
				if e.npc == nil || (e.npc.Function != "WarpMan" && e.npc.Function != "NPCEnemy") {
					continue
				}
				for _, blk := range strings.Split(e.npc.Arg, "OVER") {
					spec, ok := data.ArgField(blk, "WARP")
					if !ok {
						continue
					}
					free, _ := data.ArgField(blk, "FREE")
					for _, s := range strings.Split(spec, ";") {
						v := strings.Split(s, ",")
						if to := data.Atoi(v[0]); to > 0 {
							adj[fl] = append(adj[fl], edge{to, fmt.Sprintf("%s %s %d(%d,%d) FREE=%s -> %s", e.npc.Function, e.npc.Name, fl, e.x, e.y, free, s)})
						}
					}
				}
			}
		}
	})
	prev := map[int]string{}
	from := map[int]int{}
	q := []int{}
	for _, h := range data.Hometowns {
		q = append(q, h.Floor)
		prev[h.Floor] = "start"
	}
	for len(q) > 0 {
		f := q[0]
		q = q[1:]
		for _, e := range adj[f] {
			if _, ok := prev[e.to]; !ok {
				prev[e.to] = e.desc
				from[e.to] = f
				q = append(q, e.to)
			}
		}
	}
	for _, target := range []int{3413, 3400, 200, 21002, 21003, 21014, 21015, 21214, 21215, 21308, 31302, 30305} {
		if _, ok := prev[target]; !ok {
			t.Logf("floor %d NOT REACHABLE", target)
			continue
		}
		var path []string
		for f := target; prev[f] != "start"; f = from[f] {
			path = append([]string{prev[f]}, path...)
		}
		t.Logf("path to %d:\n  %s", target, strings.Join(path, "\n  "))
	}
}

// ---------------- 两条任务链 ----------------

// qj 是跑任务链用的小工具：一个玩家、一个假客户端。
type qj struct {
	t  *testing.T
	w  *World
	id int
	cl *fakeClient
}

// at 让玩家站到 NPC 右边一格、面向它（NPC 可能被 NpcWarp 换过位置，取它现在的位置）。
func (q *qj) at(n *entity) {
	q.w.Do(func() { q.w.placeOnFloor(q.w.players[q.id], n.floor, n.x+1, n.y, 2) })
	q.cl.take()
}

func (q *qj) talk(n *entity) *proto.NpcWindow {
	q.at(n)
	win, _ := exTalk(q.w, q.cl, q.id, n)
	return win
}

func (q *qj) reply(n *entity, win *proto.NpcWindow, btn int) *proto.NpcWindow {
	q.t.Helper()
	if win == nil {
		q.t.Fatalf("没有窗口可以回")
	}
	next, _ := exReply(q.w, q.cl, q.id, n, win, btn)
	return next
}

// expect：窗口要在，含 want，按钮是 buttons（0 不检查）。
func (q *qj) expect(step string, win *proto.NpcWindow, want string, buttons int) {
	q.t.Helper()
	if win == nil || !strings.Contains(win.Text, want) || (buttons != 0 && win.Buttons != buttons) {
		q.t.Fatalf("[%s] 应显示含「%s」、按钮 %d 的窗口：%+v", step, want, buttons, win)
	}
	if strings.Contains(win.Text, `\n`) {
		q.t.Errorf("[%s] 窗口里留着没换行的 \\n：%q", step, win.Text)
	}
	if strings.Contains(win.Text, `\`) {
		q.t.Logf("[%s] 注意：窗口里有反斜线：%q", step, win.Text)
	}
	q.t.Logf("[%s] OK：%q", step, firstLine(win.Text))
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i] + "…"
	}
	return s
}

func (q *qj) flags(ev int) (done, now bool) {
	exChar(q.w, q.id, func(e *entity) { done, now = e.eventDone(ev), e.eventNow(ev) })
	return
}

func (q *qj) wantFlags(step string, ev int, done, now bool) {
	q.t.Helper()
	if d, n := q.flags(ev); d != done || n != now {
		q.t.Fatalf("[%s] 事件 %d 应 done=%v now=%v，实际 done=%v now=%v", step, ev, done, now, d, n)
	}
}

func (q *qj) wantItem(step string, item, n int) {
	q.t.Helper()
	if c := exCount(q.w, q.id, item); c != n {
		q.t.Fatalf("[%s] 道具 %d 应有 %d 个，实际 %d", step, item, n, c)
	}
}

func (q *qj) charm() (c int) {
	exChar(q.w, q.id, func(e *entity) { c = e.char.Charm })
	return
}

// warpMan：对传送员说话 → 是/否 → 按"是"，应传送到 (floor,x,y)。
func (q *qj) warpMan(step string, n *entity, floor, x, y int) {
	q.t.Helper()
	q.at(n)
	q.w.Talk(q.id, n.id)
	win, ok := find[*proto.NpcWindow](q.cl.take())
	if !ok || win.Buttons&BtnYes == 0 {
		q.t.Fatalf("[%s] 传送员应给是/否窗口：%+v", step, win)
	}
	q.w.NpcWindowReply(q.id, n.id, win.Seq, BtnYes, "")
	ef, ok := find[*proto.EnterFloor](q.cl.take())
	if !ok || ef.Floor != floor || ef.X != x || ef.Y != y {
		q.t.Fatalf("[%s] 应传送到 %d(%d,%d)：%+v", step, floor, x, y, ef)
	}
	q.t.Logf("[%s] OK：传送到 %d(%d,%d)", step, floor, x, y)
}

// warpManDenied：条件不合时按了"是"也不传送。
func (q *qj) warpManDenied(step string, n *entity) {
	q.t.Helper()
	q.at(n)
	q.w.Talk(q.id, n.id)
	win, ok := find[*proto.NpcWindow](q.cl.take())
	if ok {
		q.w.NpcWindowReply(q.id, n.id, win.Seq, BtnYes, "")
	}
	if _, moved := find[*proto.EnterFloor](q.cl.take()); moved {
		q.t.Fatalf("[%s] 条件不合不应传送", step)
	}
	q.t.Logf("[%s] OK：条件不合不传送", step)
}

// guardian：战斗 NPC 说话 → 是 → 打赢 → 应传送到 (floor,x,y)。
func (q *qj) guardian(step string, n *entity, floor, x, y int) {
	q.t.Helper()
	q.at(n)
	talkYes(q.t, q.w, q.id, q.cl, n)
	msgs := npcEnemyWin(q.t, q.w, q.id, q.cl)
	ef, ok := find[*proto.EnterFloor](msgs)
	if !ok || ef.Floor != floor || ef.X != x || ef.Y != y {
		q.t.Fatalf("[%s] 打赢应传送到 %d(%d,%d)：%+v", step, floor, x, y, ef)
	}
	q.t.Logf("[%s] OK：打赢后传送到 %d(%d,%d)", step, floor, x, y)
}

// npcAt 找楼层 floor 上功能 fn、参数文件正好是 file 的 NPC（findNPC 是子串比对，hekisei 会对到 hekisei01）。
func npcAt(t *testing.T, w *World, fn, file string, floor int) *entity {
	t.Helper()
	var found *entity
	w.Do(func() {
		for _, e := range w.floors[floor] {
			if e.npc != nil && e.npc.Function == fn && strings.Join(e.npc.Args, "|") == "file:"+file {
				found = e
			}
		}
	})
	if found == nil {
		t.Fatalf("楼层 %d 上没有 %s（%s）", floor, fn, file)
	}
	return found
}

// dropOn：item 是否由出现在 floor 遇敌区域里的敌人掉落。
func dropOn(g *data.Game, item, floor int) (string, bool) {
	for _, a := range g.Battle.Areas {
		if a.Floor != floor {
			continue
		}
		for _, gid := range a.Groups {
			gr := g.Battle.Groups[gid]
			if gr == nil {
				continue
			}
			for _, eid := range gr.Enemies {
				e := g.Battle.Enemies[eid]
				if e == nil {
					continue
				}
				for i, d := range e.Items {
					if d == item && e.ItemProbs[i] > 0 {
						return fmt.Sprintf("敌人 %d %s（队伍 %d %s，出现道具 %d、不出现道具 %d）在楼层 %d 掉落 %d‰", e.ID, e.Name, gr.ID, gr.Name, gr.AppearItem, gr.HideItem, floor, e.ItemProbs[i]), true
					}
				}
			}
		}
	}
	return "", false
}

// reach：只走静态传送点（mapwarp 与 npcgen_warp），from 到不到得了 to。
func reach(g *data.Game, from, to int) bool {
	seen := map[int]bool{from: true}
	q := []int{from}
	for len(q) > 0 {
		f := q[0]
		q = q[1:]
		if f == to {
			return true
		}
		for _, wp := range g.Warps[f] {
			if !seen[wp.ToFloor] {
				seen[wp.ToFloor] = true
				q = append(q, wp.ToFloor)
			}
		}
	}
	return false
}

// randItemsExist：GetRandItem 列的编号都要在道具表里。
func randItemsExist(t *testing.T, g *data.Game, n *entity) {
	t.Helper()
	spec, _ := data.ArgField(n.npc.Arg, "GetRandItem")
	var missing []string
	for _, s := range strings.Split(spec, ",") {
		if g.Items[data.Atoi(s)] == nil {
			missing = append(missing, s)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%s 的 GetRandItem 有 %d 个编号不在道具表里：%v", n.npc.Name, len(missing), missing)
	} else {
		t.Logf("%s 的 GetRandItem %d 个编号都在道具表里", n.npc.Name, len(strings.Split(spec, ",")))
	}
}

func dropAll(w *World, id, item int) {
	w.Do(func() { w.exTake(w.players[id], item, 0, true, func(s string) string { return s }) })
}

// 事件 39：琉璃的洞窟（蒂雅特）。
func TestJarugaRuri39(t *testing.T) {
	w, db, g := setup(t)
	for _, it := range []int{2698, 2699, 2700, 2701} {
		if g.Items[it] == nil {
			t.Fatalf("道具表里没有 %d", it)
		}
	}
	laru := npcAt(t, w, "ExChangeMan", "jaruga/event/ruri02", 3413)
	ibu := npcAt(t, w, "ExChangeMan", "jaruga/event/ruri03", 200)
	tiat := npcAt(t, w, "ExChangeMan", "jaruga/event/ruri01", 21015)
	master := npcAt(t, w, "ExChangeMan", "jaruga/event/ruri04", 21015)
	bigBag := npcAt(t, w, "ExChangeMan", "jaruga/event/ruri05", 21015)
	smallBag := npcAt(t, w, "ExChangeMan", "jaruga/event/ruri06", 21015)
	gate := npcAt(t, w, "WarpMan", "genout/wpm_200_359_671", 200)
	judge := npcAt(t, w, "WarpMan", "genout/wpm_21003_34_10", 21003)
	beast := npcAt(t, w, "NPCEnemy", "200/sb_dbt.arg", 21014)

	c, cl := besideNPC(t, w, db, "ruriquest", laru)
	q := &qj{t: t, w: w, id: int(c.ID), cl: cl}

	// 0. 没做成人礼：拉鲁只有 MESSAGE
	win := q.talk(laru)
	q.expect("拉鲁/未成人", win, "离家出走", BtnYes)
	q.wantFlags("拉鲁/未成人", 39, false, false)
	q.warpManDenied("守人/1级", gate)

	exChar(w, q.id, func(e *entity) { e.char.Level = 80; e.setEventDone(4) })

	// 1. 拉鲁接委托：RequestMsg（是/否）→ 是 → ThanksMsg 1..4 → 拿到项链 2699、39 进行中
	win = q.talk(laru)
	q.expect("拉鲁/委托", win, "成人的仪式", BtnYesNo)
	win = q.reply(laru, win, BtnYes)
	q.expect("拉鲁/道谢1", win, "两个星期前", BtnNext)
	q.wantFlags("拉鲁/接下", 39, false, true)
	q.wantItem("拉鲁/接下", 2699, 1)
	win = q.reply(laru, win, BtnNext)
	q.expect("拉鲁/道谢2", win, "真实的真相", BtnNext)
	win = q.reply(laru, win, BtnNext)
	q.expect("拉鲁/道谢3", win, "琉璃的洞窟", BtnNext)
	win = q.reply(laru, win, BtnNext)
	q.expect("拉鲁/道谢4", win, "项炼一起", BtnOK)

	// 1b. 再说话问要不要放弃：否 → NoStopMsg；是 → EndStopMsg、收回项链、清掉进行中、魅力 −1
	win = q.talk(laru)
	q.expect("拉鲁/放弃?", win, "不可能的事", BtnYesNo)
	win = q.reply(laru, win, BtnNo)
	q.expect("拉鲁/不放弃", win, "就请你们帮忙", 0)
	exChar(w, q.id, func(e *entity) { e.char.Charm = 50 })
	win = q.talk(laru)
	win = q.reply(laru, win, BtnYes)
	q.expect("拉鲁/放弃", win, "我们会设法帮你", 0)
	q.wantFlags("拉鲁/放弃", 39, false, false)
	q.wantItem("拉鲁/放弃", 2699, 0)
	if q.charm() != 49 {
		t.Fatalf("放弃应魅力 −1：%d", q.charm())
	}
	win = q.talk(laru)
	win = q.reply(laru, win, BtnYes)
	exFinish(w, cl, q.id, laru, win)
	q.wantFlags("拉鲁/重接", 39, false, true)
	q.wantItem("拉鲁/重接", 2699, 1)

	// 1c. 项链弄丢了：ACCEPT 再给一次
	dropAll(w, q.id, 2699)
	win = q.talk(laru)
	q.expect("拉鲁/项链不见", win, "项炼不见了", BtnYesNo)
	win = q.reply(laru, win, BtnYes)
	q.expect("拉鲁/再给", win, "再给你一次", BtnOK)
	q.reply(laru, win, BtnOK)
	q.wantItem("拉鲁/再给", 2699, 1)
	q.wantFlags("拉鲁/再给", 39, false, true)

	// 2. 大地之羽 2700：琉璃的洞窟 2 楼的逃走鲁尼帖斯掉落；原版带着日记 2698 时这队不出现
	if s, ok := dropOn(g, 2700, 21002); ok {
		t.Logf("[大地之羽] %s", s)
	} else {
		t.Fatal("21002 没有掉大地之羽的敌人")
	}

	// 3. 伊布（200 大地图）：ACCEPT → 是 → ThanksMsg（OK）→ OK 才拿到日记 2698，伊布换到 NpcWarp 的下一个位置
	win = q.talk(ibu)
	q.expect("伊布/交日记", win, "你有遇到我爸爸吗", BtnYesNo)
	win = q.reply(ibu, win, BtnYes)
	q.expect("伊布/道谢", win, "请你拿着这本日记", BtnOK)
	q.wantItem("伊布/按OK前", 2698, 0)
	q.reply(ibu, win, BtnOK)
	q.wantItem("伊布/交日记", 2698, 1)
	var ibuPos [3]int
	w.Do(func() { ibuPos = [3]int{ibu.floor, ibu.x, ibu.y} })
	if ibuPos != [3]int{200, 215, 1017} {
		t.Errorf("伊布应换到 200(215,1017)：%v", ibuPos)
	}
	win = q.talk(ibu)
	q.expect("伊布/已有日记", win, "我一定会用我自己的力量", BtnYes)

	// 4. 进洞：200(359,671) 守人（LV>64&ENDEV=4）→ 21001；传送点走到 21003；大地的审判（ITEM=2700 或 NOWEV/ENDEV=39）→ 21003(34,7)
	q.warpMan("守人", gate, 21001, 4, 3)
	if !reach(g, 21001, 21003) || !reach(g, 21003, 21014) {
		t.Fatal("21001 → 21003、21003 → 21014 应由传送点连通")
	}
	q.warpMan("大地的审判", judge, 21003, 34, 7)

	// 5. 大地的守护兽（21014）：打赢传送到 21015(9,17)
	for _, s := range enemyNo(beast.npc.Arg, 64) {
		if g.Battle.Enemies[data.Atoi(s)] == nil {
			t.Errorf("守护兽的敌人 %s 不存在", s)
		}
	}
	q.guardian("大地的守护兽", beast, 21015, 9, 17)

	// 6. 蒂雅特：ACCEPT（NOWEV=39 & 2698 & 2699）→ 是 → ThanksMsg 1..3 → OK：交出日记与项链，拿到大地的羁绊 2701，39 做完、魅力 +1
	giveItem(w, q.id, 2700) // 在 21002 打到的大地之羽
	charm := q.charm()
	win = q.talk(tiat)
	q.expect("蒂雅特/交付", win, "竟然能控制住琉璃的洞窟", BtnYesNo)
	win = q.reply(tiat, win, BtnYes)
	q.expect("蒂雅特/道谢1", win, "跟雅哈奴说到", BtnNext)
	win = q.reply(tiat, win, BtnNext)
	q.expect("蒂雅特/道谢2", win, "50年的使命", BtnNext)
	win = q.reply(tiat, win, BtnNext)
	q.expect("蒂雅特/道谢3", win, "大地之绊", BtnOK)
	q.wantFlags("蒂雅特/按OK前", 39, false, true)
	if win = q.reply(tiat, win, BtnOK); win != nil {
		t.Fatalf("交换完不应再有窗口：%+v", win)
	}
	q.wantFlags("蒂雅特/完成", 39, true, false)
	q.wantItem("蒂雅特/完成", 2698, 0)
	q.wantItem("蒂雅特/完成", 2699, 0)
	q.wantItem("蒂雅特/完成", 2701, 1)
	q.wantItem("蒂雅特/完成", 2700, 1)
	if q.charm() != charm+1 {
		t.Errorf("魅力应 +1：%d → %d", charm, q.charm())
	}

	// 7. 之后：蒂雅特说袋子（有大地之羽）；大地的主人；大袋子拿走大地之羽、随机给一件
	win = q.talk(tiat)
	q.expect("蒂雅特/有羽毛", win, "征服大地之人", BtnYes)
	win = q.talk(master)
	q.expect("大地的主人", win, "大袋子或小袋子", BtnYes)
	randItemsExist(t, g, bigBag)
	randItemsExist(t, g, smallBag)
	win = q.talk(bigBag)
	q.expect("大袋子", win, "要收下这个大袋子吗", BtnYesNo)
	win = q.reply(bigBag, win, BtnYes)
	q.expect("大袋子/道谢", win, "从袋子中出现了很多道具", BtnOK)
	before, after := 0, 0
	exChar(w, q.id, func(e *entity) { before = w.freeBagSlots(e) })
	q.reply(bigBag, win, BtnOK)
	q.wantItem("大袋子", 2700, 0)
	exChar(w, q.id, func(e *entity) { after = w.freeBagSlots(e) })
	if after != before { // 交出一件、拿到一件
		t.Errorf("大袋子应换到一件道具：空格 %d → %d", before, after)
	}
	win = q.talk(smallBag)
	q.expect("小袋子/没有羽毛", win, "袋口被牢牢的绑住", BtnYes)
	win = q.talk(tiat)
	q.expect("蒂雅特/做完", win, "任务已经完成", BtnYes)
	win = q.talk(laru)
	q.expect("拉鲁/做完", win, "我非常了解我老婆的心情", BtnYes)
	win = q.talk(ibu)
	q.expect("伊布/做完", win, "母亲竟然", BtnYes)
}

// 伊布的 NpcWarp 有 9 个点（第 6、7 个都是 200.115.1011）：原版 CHAR_WORK_EVENTWARP 依序走、走完回到第 1 个。
// 连续交 18 次日记，伊布应照原版的顺序走两圈。
func TestJarugaIbuWander(t *testing.T) {
	w, db, _ := setup(t)
	ibu := npcAt(t, w, "ExChangeMan", "jaruga/event/ruri03", 200)
	c, cl := besideNPC(t, w, db, "ibuwalk", ibu)
	q := &qj{t: t, w: w, id: int(c.ID), cl: cl}
	exChar(w, q.id, func(e *entity) { e.char.Level = 80; e.setEventDone(4); e.setEventNow(39) })
	want := []string{"215.1017", "198.1001", "186.1032", "173.1008", "99.1085", "115.1011", "115.1011", "218.953", "158.1005"}
	var got []string
	for i := 0; i < 18; i++ {
		dropAll(w, q.id, 2698)
		win := q.talk(ibu)
		win = q.reply(ibu, win, BtnYes)
		q.reply(ibu, win, BtnOK)
		w.Do(func() { got = append(got, fmt.Sprintf("%d.%d", ibu.x, ibu.y)) })
	}
	for i := range got {
		if got[i] != want[i%len(want)] {
			t.Errorf("第 %d 次交日记后伊布应在 %s，实际 %s（全部：%v）", i+1, want[i%len(want)], got[i], got)
			break
		}
	}
}

// 事件 46：碧青的洞窟（艾妮基）。
func TestJarugaHekisei46(t *testing.T) {
	w, db, g := setup(t)
	for _, it := range []int{2766, 2767, 2768, 2769, 2770} {
		if g.Items[it] == nil {
			t.Fatalf("道具表里没有 %d", it)
		}
	}
	envoy := npcAt(t, w, "ExChangeMan", "jaruga/event/hekisei", 3400)
	enikki := npcAt(t, w, "ExChangeMan", "jaruga/event/hekisei01", 21215)
	envoy2 := npcAt(t, w, "ExChangeMan", "jaruga/event/hekisei02", 21215)
	sage := npcAt(t, w, "ExChangeMan", "jaruga/event/hekisei03", 21308)
	fii := npcAt(t, w, "ExChangeMan", "jaruga/event/hekisei04", 31302)
	smallBox := npcAt(t, w, "ExChangeMan", "jaruga/event/hekisei05", 21215)
	bigBox := npcAt(t, w, "ExChangeMan", "jaruga/event/hekisei06", 21215)
	gate := npcAt(t, w, "WarpMan", "genout/wpm_200_73_696", 200)
	sageGate := npcAt(t, w, "WarpMan", "genout/wpm_21308_20_10", 21308)
	afterGate := npcAt(t, w, "WarpMan", "genout/wpm_200_316_711", 200)
	beast := npcAt(t, w, "NPCEnemy", "200/sb_mzn.arg", 21214)

	c, cl := besideNPC(t, w, db, "hekiquest", envoy)
	q := &qj{t: t, w: w, id: int(c.ID), cl: cl}

	// 0. 没有 39/40/42：MESSAGE
	exChar(w, q.id, func(e *entity) { e.char.Level = 80; e.setEventDone(4) })
	win := q.talk(envoy)
	q.expect("水的使者/没有加护", win, "最近全世界的水都变的很肮脏", BtnYes)
	exChar(w, q.id, func(e *entity) { e.setEventDone(39) })

	// 1. 水的使者（奇喀喀村）：REQUEST → 是 → ThanksMsg → 46 进行中
	win = q.talk(envoy)
	q.expect("水的使者/委托", win, "大地的加护", BtnYesNo)
	win = q.reply(envoy, win, BtnYes)
	q.expect("水的使者/道谢", win, "碧青洞窟", BtnOK)
	q.wantFlags("水的使者", 46, false, true)

	// 2. 进碧青的洞窟：200(73,696) 看门人（LV>64&ENDEV=4）→ 21201 … 21214 → 水的守护兽 → 21215(28,97)
	q.warpMan("碧青看门人", gate, 21201, 61, 46)
	if !reach(g, 21201, 21214) {
		t.Fatal("21201 → 21214 应由传送点连通")
	}
	for _, s := range enemyNo(beast.npc.Arg, 64) {
		if g.Battle.Enemies[data.Atoi(s)] == nil {
			t.Errorf("水的守护兽的敌人 %s 不存在", s)
		}
	}
	q.guardian("水的守护兽", beast, 21215, 28, 97)

	// 3. 艾妮基（没有水晶）：ACCEPT 说明 → 是 → ThanksMsg 1..2 → OK；旗标、道具不变
	win = q.talk(enikki)
	q.expect("艾妮基/说明", win, "再来我会把详情说出来", BtnYesNo)
	win = q.reply(enikki, win, BtnYes)
	q.expect("艾妮基/说明1", win, "碧青水晶", BtnNext)
	win = q.reply(enikki, win, BtnNext)
	q.expect("艾妮基/说明2", win, "后方洞窟里的贤者", BtnOK)
	q.reply(enikki, win, BtnOK)
	q.wantFlags("艾妮基/说明后", 46, false, true)
	win = q.talk(envoy2)
	q.expect("艾妮基的使者", win, "深蓝色石头", BtnYes)

	// 4. 菲伊（吉鲁的海底通道 31302）：ACCEPT → 群青的水 2768
	win = q.talk(fii)
	q.expect("菲伊", win, "你需要这个湖水吗", BtnYesNo)
	win = q.reply(fii, win, BtnYes)
	q.expect("菲伊/道谢", win, "请拿去吧", BtnOK)
	q.reply(fii, win, BtnOK)
	q.wantItem("菲伊", 2768, 1)
	win = q.talk(fii)
	q.expect("菲伊/已有", win, "湖水派上场了没有", BtnYes)

	// 5. 深蓝色石头 2767：石之岛的敌人"深蓝色石头"掉落；水苍之贝 2766：碧青的洞窟各层的敌人掉落
	if s, ok := dropOn(g, 2767, 30305); ok {
		t.Logf("[深蓝色石头] %s，楼层名 %q", s, g.Floors[30305].Name)
	} else {
		t.Fatal("30305 没有掉深蓝色石头的敌人")
	}
	if s, ok := dropOn(g, 2766, 21214); ok {
		t.Logf("[水苍之贝] %s", s)
	} else {
		t.Error("21214 没有掉水苍之贝的敌人")
	}

	// 6. 贤者在水苍的洞窟地下 8 楼（21308）：从 21215 经 21315 往上走得到；贤者的看门人要 2767 & 2768
	if !reach(g, 21215, 21308) {
		t.Fatal("21215 → 21308 应由传送点连通")
	}
	q.warpManDenied("贤者的看门人/没石头", sageGate)
	giveItem(w, q.id, 2767)
	q.warpMan("贤者的看门人", sageGate, 21308, 20, 7)

	// 7. 贤者：ACCEPT → 是 → ThanksMsg → OK：交出 2767、2768，拿到碧青水晶 2769
	win = q.talk(sage)
	q.expect("贤者", win, "你要我制造碧青水晶吗", BtnYesNo)
	win = q.reply(sage, win, BtnYes)
	q.expect("贤者/道谢", win, "深蓝色石头跟群青之水交给我", BtnOK)
	q.reply(sage, win, BtnOK)
	q.wantItem("贤者", 2767, 0)
	q.wantItem("贤者", 2768, 0)
	q.wantItem("贤者", 2769, 1)
	q.wantFlags("贤者", 46, false, true)

	// 8. 艾妮基：ACCEPT（2769）→ 是 → ThanksMsg → OK：交出水晶，拿到水的戒指 2770，46 做完、魅力 +1
	charm := q.charm()
	win = q.talk(enikki)
	q.expect("艾妮基/交付", win, "可以接受水的加护的人物", BtnYesNo)
	win = q.reply(enikki, win, BtnYes)
	q.expect("艾妮基/道谢", win, "水的戒指", BtnOK)
	q.reply(enikki, win, BtnOK)
	q.wantItem("艾妮基/完成", 2769, 0)
	q.wantItem("艾妮基/完成", 2770, 1)
	q.wantFlags("艾妮基/完成", 46, true, false)
	if q.charm() != charm+1 {
		t.Errorf("魅力应 +1：%d → %d", charm, q.charm())
	}

	// 9. 之后：完成后的话；有水苍之贝时选箱子；200(316,711) 看门人放行到水苍的洞窟
	win = q.talk(enikki)
	q.expect("艾妮基/做完", win, "真的非常谢谢你", BtnYes)
	win = q.talk(envoy)
	q.expect("水的使者/做完", win, "实在太感谢你了", BtnYes)
	win = q.talk(sage)
	q.expect("贤者/做完", win, "派上场了吗", BtnYes)
	randItemsExist(t, g, bigBox)
	randItemsExist(t, g, smallBox)
	giveItem(w, q.id, 2766)
	win = q.talk(enikki)
	q.expect("艾妮基/水苍之贝", win, "征服水之人", BtnYes)
	win = q.talk(smallBox)
	q.expect("小箱子", win, "要收下这个小箱子吗", BtnYesNo)
	win = q.reply(smallBox, win, BtnYes)
	q.reply(smallBox, win, BtnOK)
	q.wantItem("小箱子", 2766, 0)
	win = q.talk(bigBox)
	q.expect("大箱子/没有贝", win, "箱子关的很紧", BtnYes)
	giveItem(w, q.id, 2766)
	q.warpMan("水苍的洞窟看门人", afterGate, 21301, 7, 3)
}
