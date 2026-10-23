package world

import (
	"fmt"
	"sort"
	"strings"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 任务日志（gmsv char.c CHAR_JobDaily，_JOBDAILY；资料 data/jobdaily.txt）：
// 每一行 "编号|条件|任务名|状态"，条件的写法与 NPC 的事件条件相同（NOWEV= / ENDEV=，"," 或、"&" 且、"!=" 不等于），
// 成立的行就送给客户端（原版 lssproto_JOBDAILY_send 的 "编号|说明|状态|#" 串，客户端每页 10 项）。
// 原版只在客户端按"任务查询"时送一次；这里上线时送、任务旗标一变就重送（flushQuests），客户端拿它做追踪与提示。
// 另外给每项加一个原版没有的提示（QuestEntry.hint）：参数里带这个事件编号的任务交换 NPC 叫什么、在哪一层，
// 让玩家知道去找谁（原版的 jobdaily.txt 只有任务名）。

// questLog 是 CHAR_JobDaily：挑出条件成立的行。
func (w *World) questLog(e *entity) []proto.QuestEntry {
	out := []proto.QuestEntry{}
	hints := w.questHints()
	for _, j := range w.game.Quests {
		if !w.passCheck(e, j.Rule) {
			continue
		}
		hint := hints[j.ID]
		if j.Hint != "" {
			hint = w.questHintText(j.Hint)
		}
		out = append(out, proto.QuestEntry{ID: j.ID, Name: j.Name, State: j.State, Hint: hint})
	}
	return out
}

// sendQuests 把任务日志送给客户端，并清掉"旗标改过"的记号；记下这份清单的签名（refreshQuests 比对用）。
func (w *World) sendQuests(e *entity) {
	e.questDirty = false
	q := w.questLog(e)
	e.questSig = questSig(q)
	if e.client != nil {
		e.client.Send(&proto.QuestLog{Quests: q, Available: w.availableQuests(e)})
	}
}

// availableQuests 是可以接的任务（额外新功能，原版的任务查询只列接了的）：所有任务交换 NPC（ExChangeMan）的 REQUEST 段里，
// 事件还没做完、也还没接、条件（EVENT=）现在就成立、不用说暗语（KeyWord）的。任务名用 jobdaily 里写了这个事件 NOWEV= 的那一行，
// 没有就叫"<NPC名>的委托"；提示是 NPC 名与楼层名；编号是事件编号。同一个事件只列一次。
func (w *World) availableQuests(e *entity) []proto.QuestEntry {
	out := []proto.QuestEntry{}
	seen := map[int]bool{}
	for fl, es := range w.floors {
		fname := ""
		if f := w.game.Floors[fl]; f != nil {
			fname = f.Name
		}
		for _, n := range es {
			if n.npc == nil || n.npc.Function != "ExChangeMan" || n.npc.Name == "" {
				continue
			}
			for _, b := range strings.Split(n.npc.Arg, "EventEnd") {
				no, ok := data.ArgField(b, "EventNo")
				ev := data.Atoi(no)
				if !ok || ev <= 0 || seen[ev] || e.eventDone(ev) || e.eventNow(ev) {
					continue
				}
				if typ, _ := data.ArgField(b, "TYPE"); !strings.Contains(typ, "REQUEST") {
					continue
				}
				if _, kw := data.ArgField(b, "KeyWord"); kw || w.exEventCheck(e, b, n) < 0 {
					continue
				}
				seen[ev] = true
				name := w.questNameOf(ev)
				if name == "" {
					name = n.npc.Name + "的委托"
				}
				hint := n.npc.Name
				if fname != "" {
					hint += "（" + fname + "）"
				}
				out = append(out, proto.QuestEntry{ID: ev, Name: name, State: "可接", Hint: hint})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// questNameOf：jobdaily 里条件写了 NOWEV=<事件> 的那一行的任务名。
func (w *World) questNameOf(ev int) string {
	key := fmt.Sprintf("NOWEV=%d", ev)
	for _, j := range w.game.Quests {
		for _, c := range strings.FieldsFunc(j.Rule, func(r rune) bool { return r == ',' || r == '&' }) {
			if strings.TrimSpace(c) == key {
				return j.Name
			}
		}
	}
	return ""
}

func questSig(q []proto.QuestEntry) string {
	var b strings.Builder
	for _, e := range q {
		fmt.Fprintf(&b, "%d|%s;", e.ID, e.State)
	}
	return b.String()
}

// refreshQuests：道具栏变了（用道具记进度的任务，如 JOT 的检查证明石）就重算，清单有变才送。sendInventory 调它。
func (w *World) refreshQuests(e *entity) {
	if e.client == nil || e.kind != "player" {
		return
	}
	if questSig(w.questLog(e)) != e.questSig {
		w.sendQuests(e)
	}
}

// flushQuests：这次对话 / 指令里任务旗标改过了（setEventDone 等把 questDirty 记下）就重送任务日志。
// 任务交换 NPC 的入口（exchangeTalk / exchangeReply）与 GM 的 /event 用 defer 调它。
func (w *World) flushQuests(e *entity) {
	if e != nil && e.questDirty {
		w.sendQuests(e)
	}
}

// QuestReq：客户端打开任务窗口时要一次（原版 lssproto_JOBDAILY_recv）。
func (w *World) QuestReq(charID int) {
	w.Do(func() {
		if e := w.players[charID]; e != nil {
			w.sendQuests(e)
		}
	})
}

// questHintText：jobdaily_h5.txt 第 5 栏的提示："npc:名字" 换成 "名字（楼层名）"（同名的 NPC 都列，最多 3 个），其余原样。
func (w *World) questHintText(h string) string {
	name, ok := strings.CutPrefix(h, "npc:")
	if !ok {
		return h
	}
	var out []string
	for floor, npcs := range w.game.NPCs {
		for _, n := range npcs {
			if n.Name != name || len(out) >= 3 {
				continue
			}
			where := name
			if f := w.game.Floors[floor]; f != nil && f.Name != "" {
				where += "（" + f.Name + "）"
			}
			out = append(out, where)
		}
	}
	if len(out) == 0 {
		return name
	}
	return strings.Join(out, "、")
}

// questHints：事件编号 → "NPC 名（楼层名）" 清单（最多 3 个）。看的是任务交换 NPC（ExChangeMan）参数里各段的 EventNo
// 与 EndSetFlg（这个 NPC 会把哪些事件设成做完）。算一次就记着。
func (w *World) questHints() map[int]string {
	if w.qhints != nil {
		return w.qhints
	}
	lists := map[int][]string{}
	seen := map[int]map[string]bool{}
	add := func(id int, where string) {
		if id <= 0 || where == "" {
			return
		}
		if seen[id] == nil {
			seen[id] = map[string]bool{}
		}
		if seen[id][where] || len(lists[id]) >= 3 {
			return
		}
		seen[id][where] = true
		lists[id] = append(lists[id], where)
	}
	for floor, npcs := range w.game.NPCs {
		fname := ""
		if f := w.game.Floors[floor]; f != nil {
			fname = f.Name
		}
		for _, n := range npcs {
			if n.Function != "ExChangeMan" || n.Name == "" {
				continue
			}
			where := n.Name
			if fname != "" {
				where += "（" + fname + "）"
			}
			for _, b := range strings.Split(n.Arg, "EventEnd") {
				if no, ok := data.ArgField(b, "EventNo"); ok && !strings.Contains(no, "-") {
					add(data.Atoi(no), where)
				}
				if s, ok := data.ArgField(b, "EndSetFlg"); ok {
					for _, f := range strings.Split(s, ",") {
						add(data.Atoi(f), where)
					}
				}
			}
		}
	}
	w.qhints = map[int]string{}
	for id, l := range lists {
		w.qhints[id] = strings.Join(l, "、")
	}
	return w.qhints
}
