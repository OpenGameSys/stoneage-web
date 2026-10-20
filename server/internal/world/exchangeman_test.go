package world

import (
	"strings"
	"testing"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// exTalk 说话（或对 NPC 说一句话）后取回窗口；没有窗口时 ok 为 false。
func exTalk(w *World, cl *fakeClient, charID int, n *entity) (*proto.NpcWindow, []proto.Message) {
	w.Talk(charID, n.id)
	msgs := cl.take()
	win, _ := find[*proto.NpcWindow](msgs)
	return win, msgs
}

// exReply 回传窗口后取回下一个窗口。
func exReply(w *World, cl *fakeClient, charID int, n *entity, win *proto.NpcWindow, button int) (*proto.NpcWindow, []proto.Message) {
	w.NpcWindowReply(charID, n.id, win.Seq, button, "")
	msgs := cl.take()
	next, _ := find[*proto.NpcWindow](msgs)
	return next, msgs
}

func exCount(w *World, charID, id int) (n int) {
	w.Do(func() { n = w.countItem(w.players[charID], id) })
	return
}

func exChar(w *World, charID int, f func(e *entity)) {
	w.Do(func() { f(w.players[charID]) })
}

// 门票贩卖员（chatroom/ticket1000）：等级 11 以上的是 ACCEPT（DelStone:LV*3，AcceptMsg 的 %8d 填价钱）。
// 是 → 道谢（ACCTHANKNEW 的 OK）→ OK 才交换；钱不够显示 StoneLessMsg。换到后交出别的门票（DelItem 只写编号的全部交出）；
// 有门票之后是 MESSAGE（NomalMsg 用说的）。
func TestExchangeTicket(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "ExChangeMan", "chatroom/ticket1000")
	if g.Items[2597] == nil || g.Items[2521] == nil {
		t.Skip("没有门票道具")
	}
	c, cl := besideNPC(t, w, db, "ticketer", n)
	id := int(c.ID)
	exChar(w, id, func(e *entity) { e.char.Level, e.char.Gold = 50, 100 })

	win, _ := exTalk(w, cl, id, n)
	if win == nil || win.Seq != seqExAccMain || win.Buttons != BtnYesNo || !strings.Contains(win.Text, "     150s") || strings.Contains(win.Text, `\n`) {
		t.Fatalf("应显示 AcceptMsg（%%8d 填 50×3）：%+v", win)
	}
	win, _ = exReply(w, cl, id, n, win, BtnYes)
	if win == nil || win.Seq != seqExAccThankNew || win.Buttons != BtnOK || !strings.Contains(win.Text, "把票交给你") {
		t.Fatalf("按是应显示道谢的 OK 窗口：%+v", win)
	}
	win, _ = exReply(w, cl, id, n, win, BtnOK)
	if win == nil || win.Seq != seqExStart || !strings.Contains(win.Text, "钱不够") || exCount(w, id, 2597) != 0 {
		t.Fatalf("钱不够应显示 StoneLessMsg、不给门票：%+v", win)
	}

	exChar(w, id, func(e *entity) { e.char.Gold = 1000 })
	giveItem(w, id, 2521)
	win, _ = exTalk(w, cl, id, n)
	win, _ = exReply(w, cl, id, n, win, BtnYes)
	win, msgs := exReply(w, cl, id, n, win, BtnOK)
	if win != nil {
		t.Fatalf("交换完不应再有窗口：%+v", win)
	}
	if exCount(w, id, 2597) != 1 || exCount(w, id, 2521) != 0 {
		t.Fatalf("应拿到门票 2597、交出 2521：%d %d", exCount(w, id, 2597), exCount(w, id, 2521))
	}
	if inv, ok := find[*proto.Inventory](msgs); !ok || inv.Gold != 850 {
		t.Fatalf("应付 150 石币：%+v", inv)
	}

	_, msgs = exTalk(w, cl, id, n)
	if say, ok := find[*proto.NpcTalk](msgs); !ok || !strings.Contains(say.Text, "已经有票了") {
		t.Fatalf("有门票后应说 NomalMsg：%+v", msgs)
	}
	if _, ok := find[*proto.NpcWindow](msgs); ok {
		t.Fatal("NomalMsg 不应开窗口")
	}
}

// 村子的学者（extra/event/hougyoku，EventNo 45）：REQUEST（RequestMsg 两页 → 是 → 拿到结晶石 2764、45 进行中）；
// 再说话问要不要放弃（否 → NoStopMsg；是 → EndStopMsg、收回结晶石、清掉进行中）；凑齐 4 个宝玉后 ACCEPT 交换手镯 2765、
// EndSetFlg:45，NPC 按 NpcWarp 换到下一个位置；之后 45 号的段都跳过，变成 MESSAGE 的 NomalWindowMsg。
func TestExchangeQuest(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "ExChangeMan", "extra/event/hougyoku")
	for _, it := range []int{2760, 2761, 2762, 2763, 2764, 2765} {
		if g.Items[it] == nil {
			t.Skip("没有宝玉道具")
		}
	}
	c, cl := besideNPC(t, w, db, "scholar", n)
	id := int(c.ID)

	if win, _ := exTalk(w, cl, id, n); win == nil || win.Seq != seqExStart || win.Buttons != BtnYes || !strings.Contains(win.Text, "我在寻找能够帮助我解开这个谜") {
		t.Fatalf("等级不够应是最后一段 MESSAGE（LV>0）：%+v", win)
	}
	exChar(w, id, func(e *entity) { e.char.Level = 60 })

	accept := func() {
		t.Helper()
		win, _ := exTalk(w, cl, id, n)
		if win == nil || win.Seq != seqExReqMain || win.Buttons != BtnNext || !strings.Contains(win.Text, "4个宝玉的行踪") {
			t.Fatalf("应显示 RequestMsg 第一页：%+v", win)
		}
		win, _ = exReply(w, cl, id, n, win, BtnNext)
		if win == nil || win.Seq != seqExReqMain || win.Buttons != BtnYesNo || !strings.Contains(win.Text, "不可思议的石头") {
			t.Fatalf("下一页应是 RequestMsg2 的是/否：%+v", win)
		}
		win, _ = exReply(w, cl, id, n, win, BtnYes)
		if win == nil || win.Seq != seqExStart || win.Buttons != BtnOK || !strings.Contains(win.Text, "结晶石") {
			t.Fatalf("接下委托应显示 ThanksMsg：%+v", win)
		}
		var now bool
		exChar(w, id, func(e *entity) { now = e.eventNow(45) })
		if exCount(w, id, 2764) != 1 || !now {
			t.Fatal("应拿到结晶石、45 进行中")
		}
	}
	accept()

	win, _ := exTalk(w, cl, id, n)
	if win == nil || win.Seq != seqExNowEvent || win.Buttons != BtnYesNo {
		t.Fatalf("进行中应问要不要放弃：%+v", win)
	}
	if win, _ = exReply(w, cl, id, n, win, BtnNo); win == nil || !strings.Contains(win.Text, "就拜托你了") {
		t.Fatalf("按否应显示 NoStopMsg：%+v", win)
	}
	win, _ = exTalk(w, cl, id, n)
	if win, _ = exReply(w, cl, id, n, win, BtnYes); win == nil || !strings.Contains(win.Text, "虽然很遗憾") {
		t.Fatalf("按是应显示 EndStopMsg：%+v", win)
	}
	var now bool
	exChar(w, id, func(e *entity) { now = e.eventNow(45) })
	if exCount(w, id, 2764) != 0 || now {
		t.Fatal("放弃后应收回结晶石、清掉进行中")
	}

	accept()
	for _, it := range []int{2760, 2761, 2762, 2763} {
		giveItem(w, id, it)
	}
	win, _ = exTalk(w, cl, id, n)
	if win == nil || win.Seq != seqExAccMain || !strings.Contains(win.Text, "4个宝玉全部凑齐") {
		t.Fatalf("凑齐后应显示 AcceptMsg：%+v", win)
	}
	win, _ = exReply(w, cl, id, n, win, BtnYes)
	if win == nil || win.Seq != seqExAccThankNew || win.Buttons != BtnOK {
		t.Fatalf("应显示道谢：%+v", win)
	}
	if exCount(w, id, 2765) != 0 {
		t.Fatal("按 OK 之前不应交换")
	}
	exReply(w, cl, id, n, win, BtnOK)
	for _, it := range []int{2760, 2761, 2762, 2763, 2764} {
		if exCount(w, id, it) != 0 {
			t.Fatalf("应交出 %d", it)
		}
	}
	var done bool
	exChar(w, id, func(e *entity) { done, now = e.eventDone(45), e.eventNow(45) })
	if exCount(w, id, 2765) != 1 || !done || now {
		t.Fatalf("应拿到手镯、45 做完：done=%v now=%v", done, now)
	}
	var floor, x, y int
	w.Do(func() { floor, x, y = n.floor, n.x, n.y })
	if g.Floors[4000] == nil {
		t.Log("没有 4000 号地图，不检查 NpcWarp")
	} else if floor != 4000 || x != 53 || y != 101 {
		t.Fatalf("NPC 应换到 4000(53,101)：%d(%d,%d)", floor, x, y)
	}

	w.Do(func() { w.placeOnFloor(w.players[id], n.floor, n.x+1, n.y, 2) })
	cl.take()
	win, _ = exTalk(w, cl, id, n)
	if win == nil || win.Seq != seqExStart || win.Buttons != BtnYes || !strings.Contains(win.Text, "受了你很大的照顾") {
		t.Fatalf("做完后 45 号的段应跳过，显示 MESSAGE：%+v", win)
	}
}

// 巴那姆的湖水（giiru/event/sink/sink03）：点选时 KeyWord 段不成立（显示 MESSAGE 的说明）；
// 对 NPC 说"很多"才是那一段 ACCEPT，换到的是那一段的道具。
func TestExchangeKeyWord(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "ExChangeMan", "giiru/event/sink/sink03")
	if g.Items[2708] == nil || g.Items[2709] == nil {
		t.Skip("没有壶的道具")
	}
	c, cl := besideNPC(t, w, db, "drinker", n)
	id := int(c.ID)
	giveItem(w, id, 2708)
	win, _ := exTalk(w, cl, id, n)
	if win == nil || win.Seq != seqExStart || !strings.Contains(win.Text, "告诉我需要多少") {
		t.Fatalf("点选应显示 MESSAGE：%+v", win)
	}
	w.Chat(id, "near", "很多", 0, 3)
	win, _ = find[*proto.NpcWindow](cl.take())
	if win == nil || win.Seq != seqExAccMain || !strings.Contains(win.Text, "想要很多") {
		t.Fatalf("说\"很多\"应显示那一段的 AcceptMsg：%+v", win)
	}
	win, _ = exReply(w, cl, id, n, win, BtnYes)
	if win == nil || !strings.Contains(win.Text, "装得满满的") {
		t.Fatalf("应显示那一段的 ThanksMsg：%+v", win)
	}
	exReply(w, cl, id, n, win, BtnOK)
	if exCount(w, id, 2709) != 1 || exCount(w, id, 2708) != 0 {
		t.Fatal("应交出 2708、拿到 2709")
	}
}

// 条件比较的几个原版细节（NPC_EventBigSmallCheck / NPC_EventFreeIfCheck）。
func TestExchangeConditions(t *testing.T) {
	w, db, _ := setup(t)
	n := findNPC(t, w, "ExChangeMan", "chatroom/ticket1000")
	c, _ := besideNPC(t, w, db, "checker", n)
	w.Do(func() {
		p := w.players[int(c.ID)]
		p.char.Level = 10
		p.setEventDone(7)
		w.addItem(p.inv, &Item{ID: 2597, Pile: 3})
		cases := []struct {
			cond string
			want bool
		}{
			{"LV<11", true}, {"LV>10", false}, {"LV=10", true},
			{"LV!=10", true}, {"LV!=11", false}, // 原版 "!=" 反过来
			{"ENDEV=7", true}, {"ENDEV>7", true}, {"ENDEV!=7", false}, {"ENDEV!=8", true},
			{"NOWEV=7", false}, {"NOWEV!=7", true},
			{"ITEM=2597", true}, {"ITEM!=2597", false}, {"ITEM!=2521", true}, {"ITEM<9999", false},
			{"ITEM=2597*3", true}, {"ITEM=2597*4", false},
			{"PET>0-560*1", false}, {"DR<11", false}, {"", false},
		}
		for _, tc := range cases {
			if got := w.exBigSmall(p, tc.cond, "", false); got != tc.want {
				t.Errorf("%q = %v，应为 %v", tc.cond, got, tc.want)
			}
		}
		if g := w.exEventCheck(p, "EVENT:LV>50,DR=1,LV<11&ITEM=2597", nil); g != 3 {
			t.Errorf("应是第 3 组成立：%d", g)
		}
	})
}

// 和数据里每一个任务交换的 NPC 说话（含 KeyWord），窗口按几种按钮一路回下去：不应出错。
func TestExchangeSweep(t *testing.T) {
	w, db, _ := setup(t)
	var npcs []*entity
	w.Do(func() {
		for _, es := range w.floors {
			for _, e := range es {
				if e.npc != nil && e.npc.Function == "ExChangeMan" {
					npcs = append(npcs, e)
				}
			}
		}
	})
	if len(npcs) == 0 {
		t.Skip("数据里没有 ExChangeMan")
	}
	c, cl := besideNPC(t, w, db, "exsweeper", npcs[0])
	id := int(c.ID)
	windows := 0
	pick := func(win *proto.NpcWindow, pref int) int {
		if win.Buttons&pref != 0 {
			return pref
		}
		for b := 1; b <= BtnNext; b <<= 1 {
			if win.Buttons&b != 0 {
				return b
			}
		}
		return 0
	}
	for _, n := range npcs {
		var words []string
		w.Do(func() {
			for _, seg := range strings.Split(n.npc.Arg, "|") {
				if strings.HasPrefix(seg, "KeyWord:") {
					kw, _ := data.ArgField(seg, "KeyWord")
					words = append(words, kw)
				}
			}
		})
		for _, pref := range []int{BtnYes, BtnNo, BtnNext, BtnOK} {
			for _, word := range append([]string{""}, words...) {
				w.Do(func() {
					e := w.players[id]
					e.char.Gold, e.char.Level = 1000, 80
					w.placeOnFloor(e, n.floor, n.x+1, n.y, 1) // 方向 1 朝 NPC（x−1）
				})
				cl.take()
				if word == "" {
					w.Talk(id, n.id)
				} else {
					w.Chat(id, "near", word, 0, 1)
				}
				win, _ := find[*proto.NpcWindow](cl.take())
				for step := 0; win != nil && step < 8; step++ {
					windows++
					var next *proto.NpcWindow
					next, _ = exReply(w, cl, id, n, win, pick(win, pref))
					win = next
				}
			}
		}
	}
	if windows == 0 {
		t.Fatal("应至少打开一些窗口")
	}
	t.Logf("%d 个 NPC，打开 %d 次窗口", len(npcs), windows)
}
