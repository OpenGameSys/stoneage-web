package world

import (
	"fmt"
	"strings"

	"sa/server/internal/data"
)

// 猜谜（npc_quiz.c，functionset Quiz，22 个）：参数 Quiznum 出几题，题目从题库（question.txt）按 Type / Answer / Level
// 的位元挑；选择题（二选一、三选一）用选单作答，写答案的用输入行。全部答完按答对的题数（Border / GetItem / Warp 的
// "门槛,内容" 对）说话、给道具、传送。参加可能要 EntryItem（参加券）或 EntryStone（石币）。

const (
	seqQuizStart = 284 // CHAR_WINDOWTYPE_QUIZ_*
	seqQuizMain  = 285
	quizPlayers  = 8 // MEPLAYER：一个出题人同时最多 8 人
)

// quizState 是原版的 struct pl：这一轮出了几题、答对几题、问过的题、这题正确的是第几个选项。
type quizState struct {
	npc            int
	asked          []int
	quizNo, answer int
	ansNo          int // 选择题：正确选项的位置；写答案：−1
	current        int // 这题在题库的下标
}

// quizSession 取这个玩家与出题人的这一轮（没有回传 nil）。
func (w *World) quizSession(p, n *entity) *quizState {
	if q := w.quizzes[p.id]; q != nil && q.npc == n.id {
		return q
	}
	return nil
}

// quizTalk 是 NPC_QuizTalked：要在旁边；道具栏满了（又没有参加券可以交出来空出位子）显示 ItemFullMsg，否则 StartMsg 的是 / 否。
func (w *World) quizTalk(p, n *entity) {
	if max(abs(n.x-p.x), abs(n.y-p.y)) > 1 {
		return
	}
	arg := n.npc.Arg
	if !w.quizRoom(p, arg) {
		if msg, ok := data.ArgField(arg, "ItemFullMsg"); ok {
			w.sendWindow(p, n, "message", BtnOK, seqQuizStart, npcText(msg))
		}
		return
	}
	if msg, ok := data.ArgField(arg, "StartMsg"); ok {
		w.sendWindow(p, n, "message", BtnYesNo, seqQuizStart, npcText(msg))
	}
}

// quizRoom 是 NPC_QuizItemFullCheck：道具栏有空格，或参加券交出去会空出位子。
func (w *World) quizRoom(p *entity, arg string) bool {
	if w.freeBagSlots(p) > 0 {
		return true
	}
	spec, ok := data.ArgField(arg, "EntryItem")
	return ok && w.haveItems(p, parseNeeds(spec, ",", false))
}

// quizReply 是 NPC_QuizWindowTalked。
func (w *World) quizReply(p, n *entity, win *npcWindow, button int, sel string) {
	if max(abs(n.x-p.x), abs(n.y-p.y)) > 1 {
		return
	}
	arg := n.npc.Arg
	switch win.seq {
	case seqQuizStart:
		if button != BtnYes {
			return
		}
		// NPC_Quiz_selectWindow(1)：先查参加券与石币，再看人满了没有，都可以才收下开始
		item, hasItem := data.ArgField(arg, "EntryItem")
		stone, hasStone := data.ArgField(arg, "EntryStone")
		if hasItem && !w.haveItems(p, parseNeeds(item, ",", false)) || hasStone && p.char.Gold < data.Atoi(stone) {
			if msg, ok := data.ArgField(arg, "NoEntryMsg"); ok {
				w.sendWindow(p, n, "message", BtnOK, seqQuizStart, npcText(msg))
			}
			return
		}
		if !w.quizSeat(p, n) {
			w.sendWindow(p, n, "message", BtnOK, seqQuizStart, "\n\n\n        请,稍等一下")
			return
		}
		if hasItem {
			w.takeItems(p, parseNeeds(item, ",", false), true)
		}
		if hasStone { // NPC_EntryStoneDel：不够时扣到 0
			cost := data.Atoi(stone)
			p.char.Gold = max(0, p.char.Gold-cost)
			w.sysMsg(p, fmt.Sprintf("%d 交出stone。", cost), colorWhite)
			w.sendInventory(p)
			w.saveProgress(p)
		}
		w.quizzes[p.id] = &quizState{npc: n.id, ansNo: -1}
		w.quizNext(p, n)
	case seqQuizMain:
		q := w.quizSession(p, n)
		if q == nil {
			return
		}
		qs := w.game.Quiz[q.current]
		switch {
		case button == BtnOK && q.ansNo < 0: // 写答案：输入里含有正确答案就算对
			if qs.Select[0] != "" && strings.Contains(sel, qs.Select[0]) {
				q.answer++
			}
			w.quizNext(p, n)
		case button == BtnCancel:
			delete(w.quizzes, p.id)
		case data.Atoi(sel) == 0:
		default:
			if data.Atoi(sel) == q.ansNo {
				q.answer++
			}
			w.quizNext(p, n)
		}
	}
}

// quizSeat 是 NPC_PlayerCheck + NPC_RealyCheack：已经不在出题人旁边的人让出位子；自己重来就从头开始。
func (w *World) quizSeat(p, n *entity) bool {
	used := 0
	for id, q := range w.quizzes {
		if q.npc != n.id {
			continue
		}
		o := w.players[id]
		if id == p.id || o == nil || o.floor != n.floor || max(abs(n.x-o.x), abs(n.y-o.y)) > 1 {
			delete(w.quizzes, id)
			continue
		}
		used++
	}
	return used < quizPlayers
}

// quizPool 是 NPC_GetQuestion：参数 Type 是题目种类的位元（第 k 位为种类 k+1），Answer、Level 是答法与等级的位元；没写或 ≤ 0 时都可以。
func (w *World) quizPool(arg string) []int {
	mask := func(key string) int {
		v, _ := data.ArgField(arg, key)
		if n := data.Atoi(v); n > 0 {
			return n
		}
		return 0xffff
	}
	typ, ans, lv := mask("Type"), mask("Answer"), mask("Level")
	var out []int
	for i, q := range w.game.Quiz {
		if q.Type < 1 || typ&(1<<(q.Type-1)) == 0 || ans&q.AnswerType != q.AnswerType || lv&q.Level != q.Level {
			continue
		}
		out = append(out, i)
	}
	return out
}

// quizNext 是 NPC_Quiz_selectWindow(2)：还没出完就出下一题（不重复），出完了结算。
func (w *World) quizNext(p, n *entity) {
	q := w.quizSession(p, n)
	if q == nil {
		return
	}
	arg := n.npc.Arg
	num, _ := data.ArgField(arg, "Quiznum")
	if q.quizNo >= data.Atoi(num) {
		w.quizFinish(p, n, q)
		return
	}
	var pool []int
	for _, i := range w.quizPool(arg) {
		if !containsInt(q.asked, i) {
			pool = append(pool, i)
		}
	}
	if len(pool) == 0 {
		delete(w.quizzes, p.id) // 原版题目不够时这一轮作废
		return
	}
	q.current = pool[w.rnd.IntN(len(pool))]
	q.asked = append(q.asked, q.current)
	q.quizNo++
	qs := w.game.Quiz[q.current]
	text, rows := qs.Text, 4
	// 题目长度决定选项从第几行起（原版用 GBK 字节数：短于 40 补一行，长于 79 占两行）
	switch width := gbkWidth(text); {
	case width < 40:
		text += "\n"
	case width > 79:
		rows = 5
	}
	if qs.AnswerType == 4 {
		if gbkWidth(text) < 80 {
			text += "\n\n"
		} else {
			text += "\n"
		}
		q.ansNo = -1
		w.sendWindow(p, n, "input", BtnOK, seqQuizMain, fmt.Sprintf("   quiz 第 %d 题\n%s \n↓写在这里\n※(写完请按ＯＫ)", q.quizNo, text))
		return
	}
	choices := 2
	if qs.AnswerType == 2 {
		choices = 3
	}
	// NPC_QuizRandAnswer：正确答案随机放在一个位置，其余的随机排在剩下的位置
	q.ansNo = w.rnd.IntN(choices) + 1
	var wrong []string
	for i := 0; i < choices; i++ {
		if i != qs.Answer-1 {
			wrong = append(wrong, qs.Select[i])
		}
	}
	w.rnd.Shuffle(len(wrong), func(i, j int) { wrong[i], wrong[j] = wrong[j], wrong[i] })
	opts := make([]string, 0, choices)
	for i := 1; i <= choices; i++ {
		if i == q.ansNo {
			opts = append(opts, qs.Select[qs.Answer-1])
		} else {
			opts, wrong = append(opts, wrong[0]), wrong[1:]
		}
	}
	nums := []string{"１", "２", "３"}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d\n   quiz 第 %d 题\n%s \n", rows, q.quizNo, text)
	for i, o := range opts {
		fmt.Fprintf(&sb, "\n%s． %s", nums[i], o)
	}
	w.sendWindow(p, n, "select", BtnCancel, seqQuizMain, sb.String())
}

// quizBorder 是 NPC_QuizBorderLine："门槛,内容,门槛,内容..."，第一个 答对数 ≥ 门槛 的内容。
func quizBorder(spec string, answer int) (string, bool) {
	parts := strings.Split(spec, ",")
	for i := 0; i+1 < len(parts); i += 2 {
		if answer >= data.Atoi(parts[i]) {
			return parts[i+1], true
		}
	}
	return "", false
}

// quizFinish：全部答完，按答对的题数给道具（多个以 "." 分开时随机一个，NPC_QuizAddItem）、说 Border 的话、传送（Warp 的 "楼层.x.y"）。
func (w *World) quizFinish(p, n *entity, q *quizState) {
	delete(w.quizzes, p.id)
	arg := n.npc.Arg
	if spec, ok := data.ArgField(arg, "GetItem"); ok {
		if list, ok := quizBorder(spec, q.answer); ok {
			ids := strings.Split(list, ".")
			if def := w.game.Items[data.Atoi(ids[w.rnd.IntN(len(ids))])]; def != nil {
				if w.addItem(p.inv, w.newItem(def)) < 0 {
					w.sysMsg(p, "道具栏已满", colorWhite)
					return
				}
				w.sysMsg(p, "拿到"+def.Name+"。", colorWhite)
				w.itemsChanged(p)
			}
		}
	}
	if spec, ok := data.ArgField(arg, "Border"); ok {
		if msg, ok := quizBorder(spec, q.answer); ok {
			w.npcSay(p, n, msg)
		} else if msg, ok := data.ArgField(arg, "FailureMsg"); ok {
			w.npcSay(p, n, msg)
		}
	}
	if spec, ok := data.ArgField(arg, "Warp"); ok {
		if spot, ok := quizBorder(spec, q.answer); ok {
			v := strings.Split(spot, ".")
			for len(v) < 3 {
				v = append(v, "")
			}
			w.warpTo(p, data.Atoi(v[0]), data.Atoi(v[1]), data.Atoi(v[2]))
		}
	}
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
