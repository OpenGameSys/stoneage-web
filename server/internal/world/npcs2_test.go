package world

import (
	"strings"
	"testing"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

func findFn(t *testing.T, w *World, fn string) *entity {
	t.Helper()
	var found *entity
	w.Do(func() {
		for _, es := range w.floors {
			for _, e := range es {
				if e.npc != nil && e.npc.Function == fn {
					found = e
					return
				}
			}
		}
	})
	if found == nil {
		t.Skipf("数据里没有 %s", fn)
	}
	return found
}

// 传言板：空板 → 留言 → 看到自己的留言（内容、名字）；留到第 8 则后能往上一页翻到第 1 则。
func TestDengon(t *testing.T) {
	w, db, _ := setup(t)
	n := findFn(t, w, "Dengon")
	c, cl := besideNPC(t, w, db, "poster", n)
	id := int(c.ID)
	w.Talk(id, n.id)
	win, ok := find[*proto.NpcWindow](cl.take())
	if !ok || win.Kind != "wideinput" || !strings.HasPrefix(win.Text, "                         传言板\n") || win.Buttons != BtnOK|BtnCancel|BtnPrev {
		t.Fatalf("应打开传言板：%+v", win)
	}
	for i := 1; i <= 8; i++ {
		w.NpcWindowReply(id, n.id, win.Seq, BtnOK, "第"+itoa(i)+"则留言")
		win, ok = find[*proto.NpcWindow](cl.take())
		if !ok {
			t.Fatalf("留言后应重新显示传言板")
		}
	}
	if !strings.Contains(win.Text, "第8则留言") || !strings.Contains(win.Text, "poster") || strings.Contains(win.Text, "第1则留言") {
		t.Fatalf("最新一页应是第 2..8 则：\n%s", win.Text)
	}
	w.NpcWindowReply(id, n.id, win.Seq, BtnPrev, "")
	win, _ = find[*proto.NpcWindow](cl.take())
	if !strings.Contains(win.Text, "第1则留言") || win.Buttons != BtnOK|BtnCancel|BtnNext {
		t.Fatalf("上一页应翻到第 1 则：%+v\n%s", win, win.Text)
	}
	// 另一个角色也看得到（存在库里）
	c2, cl2 := besideNPC(t, w, db, "reader", n)
	w.Talk(int(c2.ID), n.id)
	if win, _ := find[*proto.NpcWindow](cl2.take()); win == nil || !strings.Contains(win.Text, "第8则留言") {
		t.Fatal("别人也应看到留言")
	}
	// 空白不留
	w.NpcWindowReply(int(c2.ID), n.id, seqDengon, BtnOK, "   ")
	if _, again := find[*proto.NpcWindow](cl2.take()); again {
		t.Fatal("空白留言不应写入")
	}
}

// 美容师：报价 = 等级 × 10 × (魅力 ÷ 3) × (转生 + 1)，付钱后魅力 +5；钱不够不加。
func TestCharm(t *testing.T) {
	w, db, _ := setup(t)
	n := findFn(t, w, "Charm")
	c, cl := besideNPC(t, w, db, "pretty", n)
	id := int(c.ID)
	w.Do(func() { e := w.players[id]; e.char.Level, e.char.Charm, e.char.Gold = 3, 60, 100 })
	w.Talk(id, n.id)
	win, _ := find[*proto.NpcWindow](cl.take())
	w.NpcWindowReply(id, n.id, win.Seq, 0, "2")
	win, ok := find[*proto.NpcWindow](cl.take())
	cost := 3 * 10 * (60 / 3)
	if !ok || win.Buttons != BtnYesNo || !strings.Contains(win.Text, itoa(cost)) {
		t.Fatalf("应报价 %d：%+v", cost, win)
	}
	w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
	if win, _ := find[*proto.NpcWindow](cl.take()); win == nil || !strings.Contains(win.Text, "钱不够") {
		t.Fatal("钱不够应拒绝")
	}
	w.Do(func() { w.players[id].char.Gold = 1000 })
	w.Talk(id, n.id)
	win, _ = find[*proto.NpcWindow](cl.take())
	w.NpcWindowReply(id, n.id, win.Seq, 0, "2")
	win, _ = find[*proto.NpcWindow](cl.take())
	w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
	cl.take()
	var charm, gold int
	w.Do(func() { e := w.players[id]; charm, gold = e.char.Charm, e.char.Gold })
	if charm != 65 || gold != 1000-cost {
		t.Fatalf("魅力应 +5、扣 %d：魅力 %d 金钱 %d", cost, charm, gold)
	}
}

// 占卜师（sainasu/uranai：等级 × 1）：付钱后按运气说 luckN 里的一句。
func TestLuckyMan(t *testing.T) {
	w, db, _ := setup(t)
	n := findFn(t, w, "LuckyMan")
	c, cl := besideNPC(t, w, db, "fortune", n)
	id := int(c.ID)
	w.Do(func() { e := w.players[id]; e.char.Level, e.char.Gold, e.char.Luck = 7, 10, 3 })
	w.Talk(id, n.id)
	win, ok := find[*proto.NpcWindow](cl.take())
	if !ok || !strings.Contains(win.Text, "       7S") {
		t.Fatalf("应报价 7：%+v", win)
	}
	w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
	win, _ = find[*proto.NpcWindow](cl.take())
	luck3, _ := data.ArgField(n.npc.Arg, "luck3")
	line := strings.TrimPrefix(win.Text, "　　　　　　　☆今日的运势☆　\n\n")
	if !strings.Contains(luck3, line) || line == win.Text {
		t.Fatalf("应说运气 3 的一句：%q", win.Text)
	}
	var gold int
	w.Do(func() { gold = w.players[id].char.Gold })
	if gold != 3 {
		t.Fatalf("应扣 7：%d", gold)
	}
}

// 猜拳（jan_802_29_19：要参加券 2751）：没券停在 NoItem；有券收券后出拳，分出胜负就传送。
func TestJanken(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "Janken", "jan_802_29_19")
	if g.Items[2751] == nil {
		t.Skip("没有参加券")
	}
	c, cl := besideNPC(t, w, db, "rps", n)
	id := int(c.ID)
	w.Talk(id, n.id)
	win, _ := find[*proto.NpcWindow](cl.take())
	w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
	if win, _ := find[*proto.NpcWindow](cl.take()); win == nil || !strings.Contains(win.Text, "参加券") {
		t.Fatal("没券应显示 NoItem")
	}
	giveItem(w, id, 2751)
	w.Talk(id, n.id)
	win, _ = find[*proto.NpcWindow](cl.take())
	w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
	win, ok := find[*proto.NpcWindow](cl.take())
	if !ok || win.Kind != "select" || !strings.Contains(win.Text, "石头") {
		t.Fatalf("应出现猜拳选单：%+v", win)
	}
	var ticket int
	w.Do(func() { ticket = w.countItem(w.players[id], 2751) })
	if ticket != 0 {
		t.Fatal("应收走参加券")
	}
	for i := 0; i < 50; i++ {
		w.NpcWindowReply(id, n.id, win.Seq, 0, "3")
		msgs := cl.take()
		if ef, ok := find[*proto.EnterFloor](msgs); ok {
			res, _ := find[*proto.NpcWindow](msgs)
			if res == nil || !strings.Contains(res.Text, "结  果") {
				t.Fatalf("应显示结果：%+v", msgs)
			}
			won := strings.Contains(res.Text, "胜利")
			if won && (ef.Floor != 802 || ef.X != 29 || ef.Y != 15) || !won && (ef.Floor != 801 || ef.X != 27 || ef.Y != 21) {
				t.Fatalf("胜负与传送地点不符：%v %+v", won, ef)
			}
			return
		}
		win, _ = find[*proto.NpcWindow](msgs)
		if win == nil || !strings.Contains(win.Text, "平手") {
			t.Fatalf("平手应再来：%+v", msgs)
		}
	}
	t.Fatal("50 次都平手")
}

// 家族银行：选个人帐户 → 没有家族，回"资格不符合"。
func TestBankman(t *testing.T) {
	w, db, _ := setup(t)
	n := findFn(t, w, "Bankman")
	c, cl := besideNPC(t, w, db, "saver2", n)
	id := int(c.ID)
	w.Talk(id, n.id)
	win, ok := find[*proto.NpcWindow](cl.take())
	if !ok || win.Kind != "select" || !strings.Contains(win.Text, "银行管理员") {
		t.Fatalf("应打开银行选单：%+v", win)
	}
	w.NpcWindowReply(id, n.id, win.Seq, 0, "2")
	if win, _ := find[*proto.NpcWindow](cl.take()); win == nil || !strings.Contains(win.Text, "资格不符合") {
		t.Fatalf("没有家族应回资格不符合：%+v", win)
	}
}
