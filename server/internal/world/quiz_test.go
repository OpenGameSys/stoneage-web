package world

import (
	"strings"
	"testing"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

func TestQuizData(t *testing.T) {
	_, _, g := setup(t)
	if len(g.Quiz) < 10 {
		t.Fatalf("题库太少：%d", len(g.Quiz))
	}
}

// 猜谜（quz_800_29_19：要参加券 2752、出 1 题、答对 1 题给 2754 并传到 800(29,15)）：
// 没券 → NoEntryMsg；有券 → 收券出题，答对拿道具、传送。
func TestQuiz(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "Quiz", "quz_800_29_19")
	if g.Items[2752] == nil || g.Items[2754] == nil {
		t.Skip("没有参加券道具")
	}
	c, cl := besideNPC(t, w, db, "quizzer", n)
	id := int(c.ID)
	start := func() {
		w.Talk(id, n.id)
		win, ok := find[*proto.NpcWindow](cl.take())
		if !ok || win.Buttons != BtnYesNo || !strings.Contains(win.Text, "跟我比赛猜谜") {
			t.Fatalf("应显示 StartMsg：%+v", win)
		}
		w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
	}
	start()
	if win, _ := find[*proto.NpcWindow](cl.take()); win == nil || !strings.Contains(win.Text, "参加券") {
		t.Fatal("没有参加券应显示 NoEntryMsg")
	}
	giveItem(w, id, 2752)
	start()
	win, ok := find[*proto.NpcWindow](cl.take())
	if !ok || !strings.Contains(win.Text, "quiz 第 1 题") {
		t.Fatalf("应出第 1 题：%+v", win)
	}
	var q *quizState
	var ticket int
	w.Do(func() { q = w.quizzes[id]; ticket = w.countItem(w.players[id], 2752) })
	if q == nil || ticket != 0 {
		t.Fatal("应收走参加券并开始一轮")
	}
	qs := g.Quiz[q.current]
	if win.Kind == "select" {
		w.NpcWindowReply(id, n.id, win.Seq, 0, itoa(q.ansNo))
	} else {
		w.NpcWindowReply(id, n.id, win.Seq, BtnOK, qs.Select[0])
	}
	msgs := cl.take()
	ef, moved := find[*proto.EnterFloor](msgs)
	var prize int
	w.Do(func() { prize = w.countItem(w.players[id], 2754) })
	if !moved || ef.Floor != 800 || ef.X != 29 || ef.Y != 15 || prize != 1 {
		t.Fatalf("答对应拿到 2754 并传到 800(29,15)：%+v 道具 %d", msgs, prize)
	}
	if nt, ok := find[*proto.NpcTalk](msgs); !ok || !strings.Contains(nt.Text, "干的不错") {
		t.Fatalf("应说 Border 的话：%+v", msgs)
	}
}

// 选择题的选项：正确答案在 ansNo 的位置，选项都来自这题。
func TestQuizChoices(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "Quiz", "quz_")
	c, cl := besideNPC(t, w, db, "chooser", n)
	id := int(c.ID)
	for i := 0; i < 20; i++ {
		w.Do(func() {
			w.quizzes[id] = &quizState{npc: n.id, ansNo: -1}
			w.quizNext(w.players[id], n)
		})
		win, ok := find[*proto.NpcWindow](cl.take())
		if !ok {
			continue
		}
		var q *quizState
		w.Do(func() { q = w.quizzes[id] })
		qs := g.Quiz[q.current]
		if win.Kind != "select" {
			continue
		}
		lines := clientLines(win.Text) // 客户端也按 40 个半角宽折行
		start := data.Atoi(lines[0])
		body := lines[1:]
		row := body[start+q.ansNo-1]
		if !strings.Contains(row, qs.Select[qs.Answer-1]) {
			t.Fatalf("第 %d 个可选行应是正确答案 %q：\n%s", q.ansNo, qs.Select[qs.Answer-1], win.Text)
		}
	}
}

// clientLines 照客户端 getStrSplit：按换行切，再按 40 个半角宽（全角 2）折行。
func clientLines(text string) []string {
	var out []string
	for _, para := range strings.Split(text, "\n") {
		line, w := "", 0
		for _, r := range para {
			cw := 1
			if r > 0xff {
				cw = 2
			}
			if w+cw > 40 {
				out = append(out, line)
				line, w = "", 0
			}
			line += string(r)
			w += cw
		}
		out = append(out, line)
	}
	return out
}
