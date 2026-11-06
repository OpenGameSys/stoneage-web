package world

import (
	"fmt"
	"slices"
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
// 事件还没做完、也还没接、不用说暗语（KeyWord）的。条件（EVENT=）现在就成立的是"可接"；不成立但差的都是做得到的事
// （先做完 / 先接别的任务、带道具或宠物、等级、转生、时段、记录点）的是"未满足"，提示后面接"还差：…"（questMissing）；
// 差的里有做不到的（已经做过的分支、等级太高、没开的系统）就不列。任务名用 jobdaily 里写了这个事件 NOWEV= 的那一行，
// 没有就叫"<NPC名>的委托"；提示是 NPC 名与楼层名；编号是事件编号。同一个事件只列一次（有好几段时取差最少的）。
// 排序：可接的在前（按编号），未满足的按差几样、再按编号。
func (w *World) availableQuests(e *entity) []proto.QuestEntry {
	type cand struct {
		entry proto.QuestEntry
		miss  int // 0 是可接
	}
	best := map[int]cand{}
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
				if !ok || ev <= 0 || e.eventDone(ev) || e.eventNow(ev) {
					continue
				}
				if c, seen := best[ev]; seen && c.miss == 0 {
					continue
				}
				if typ, _ := data.ArgField(b, "TYPE"); !strings.Contains(typ, "REQUEST") {
					continue
				}
				if _, kw := data.ArgField(b, "KeyWord"); kw {
					continue
				}
				name := w.questNameOf(ev)
				if name == "" {
					name = n.npc.Name + "的委托"
				}
				var missing []string
				if w.exEventCheck(e, b, n) < 0 {
					if missing = w.questMissing(e, b, n, name); len(missing) == 0 {
						continue // 做不到
					}
				}
				if c, seen := best[ev]; seen && c.miss <= len(missing) {
					continue
				}
				hint := n.npc.Name
				if fname != "" {
					hint += "（" + fname + "）"
				}
				state := "可接"
				if len(missing) > 0 {
					state = "未满足"
					hint += "　还差：" + strings.Join(missing, "；")
				}
				best[ev] = cand{proto.QuestEntry{ID: ev, Name: name, State: state, Hint: hint}, len(missing)}
			}
		}
	}
	list := make([]cand, 0, len(best))
	for _, c := range best {
		list = append(list, c)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].miss != list[j].miss {
			return list[i].miss < list[j].miss
		}
		return list[i].entry.ID < list[j].entry.ID
	})
	out := make([]proto.QuestEntry, len(list))
	for i, c := range list {
		out[i] = c.entry
	}
	return out
}

// questMissing：REQUEST 段的条件现在不成立时，各组（"," 分开）里取差最少、而且差的都做得到的那组，回传差的是什么；
// 每组都有做不到的就回传 nil。判断单项成不成立用 exBigSmall（与 NPC 本身一样）。
// 同一句只列一次；先完成 / 先接的任务和这个任务同名（同一个任务的前一段）时说"前一段"。
func (w *World) questMissing(e *entity, b string, n *entity, own string) []string {
	ev, _ := data.ArgField(b, "EVENT")
	name, need := exPetName(n, b)
	var best []string
	for _, g := range strings.Split(ev, ",") {
		var miss []string
		ok := true
		for _, c := range strings.Split(g, "&") {
			c = strings.TrimSpace(c)
			if c == "" || w.exBigSmall(e, c, name, need) {
				continue
			}
			m := w.questNeed(e, c, name)
			if m == "" {
				ok = false
				break
			}
			if rest, found := strings.CutPrefix(m, "先完成："); found && rest == own {
				m = "先完成前一段"
			} else if rest, found := strings.CutPrefix(m, "先接："); found && rest == own {
				m = "先接前一段"
			}
			if !slices.Contains(miss, m) {
				miss = append(miss, m)
			}
		}
		if ok && len(miss) > 0 && (best == nil || len(miss) < len(best)) {
			best = miss
		}
	}
	return best
}

// questNeed 把一项不成立的条件说成玩家看得懂的话；做不到的（已经做过的分支、等级 / 转生太高、身上要没有某个旗标、
// 这台服务端没有的系统）回传 ""。
func (w *World) questNeed(e *entity, c, petName string) string {
	if strings.Contains(c, "PET") {
		pc := parseExPet(c)
		pet := petName
		if t := w.game.Battle.Temps[pc.temp]; pet == "" && t != nil {
			pet = t.Name
		}
		if pet == "" {
			pet = "指定的宠物"
		}
		s := "带着宠物：" + pet
		if pc.flg == exGreater && pc.level > 0 {
			s += fmt.Sprintf("（%d 级以上）", pc.level+1)
		}
		if pc.count > 1 {
			s += fmt.Sprintf("×%d", pc.count)
		}
		return s
	}
	op := ""
	for _, o := range []string{"<", ">", "!=", "="} {
		if strings.Contains(c, o) {
			op = o
			break
		}
	}
	if op == "" {
		return ""
	}
	key, val, _ := strings.Cut(c, op)
	if key == "ITEM" && op == "=" && strings.Contains(val, "*") { // NPC_EventReduce
		id, k, _ := strings.Cut(val, "*")
		return fmt.Sprintf("带着：%s×%d", w.itemDefName(data.Atoi(id)), data.Atoi(k))
	}
	n := data.Atoi(val)
	switch key {
	case "ENDEV":
		if op == "!=" || !w.questSettable()[n] {
			return ""
		}
		return "先完成：" + w.questTitle(n)
	case "NOWEV":
		if op == "!=" || e.eventDone(n) || !w.questSettable()[n] {
			return ""
		}
		return "先接：" + w.questTitle(n)
	case "LV":
		switch {
		case op == ">" && e.char.Level <= n && n+1 <= MaxLevel:
			return fmt.Sprintf("等级 %d 以上", n+1)
		case op == "!=" && e.char.Level < n: // 原版 LV!=n 实际是等级正好 n
			return fmt.Sprintf("等级正好 %d", n)
		}
	case "TRANS":
		if op == ">" {
			return fmt.Sprintf("%d 转以上", n+1)
		}
		if op == "=" && e.char.Trans < n {
			return fmt.Sprintf("%d 转", n)
		}
	case "ITEM":
		switch op {
		case "=":
			return "带着：" + w.itemDefName(n)
		case "!=":
			return "身上不能有：" + w.itemDefName(n)
		}
	case "TIME":
		if op == "=" && n >= 0 && n < 4 {
			return "在" + [4]string{"夜里", "早上", "中午", "傍晚"}[n] + "去"
		}
	case "SP":
		if op != "!=" {
			return "先在" + w.savePointName(n) + "登记记录点"
		}
	}
	if strings.Contains(key, "reITEM") && op == ">" {
		return fmt.Sprintf("道具栏空出 %d 格以上", n+1)
	}
	return ""
}

// questTitle：事件编号的任务名（jobdaily 的 NOWEV= / ENDEV= 那一行；转生补的旗标说第几次转生），都没有就用"<NPC名>的委托"或"任务 n"。
func (w *World) questTitle(ev int) string {
	if i := slices.Index(transSetEnd, ev); i >= 0 {
		return fmt.Sprintf("第 %d 次转生", i+1)
	}
	if name := w.questNameOf(ev); name != "" {
		return name
	}
	key := fmt.Sprintf("ENDEV=%d", ev)
	for _, j := range w.game.Quests {
		for _, c := range strings.FieldsFunc(j.Rule, func(r rune) bool { return r == ',' || r == '&' }) {
			if strings.TrimSpace(c) == key {
				return j.Name
			}
		}
	}
	if h := w.questHints()[ev]; h != "" {
		first, _, _ := strings.Cut(h, "、")
		if who, _, ok := strings.Cut(first, "（"); ok {
			first = who
		}
		return first + "的委托"
	}
	return fmt.Sprintf("任务 %d", ev)
}

// questSettable：资料里有办法设起来的事件——任何 NPC 参数里的 EventNo / EndSetFlg / NowSetFlg，加上转生（transmigration.go）
// 与宠物转生（71）补的旗标。前置任务不在里面（例如要私服的任务、这台服务端没有 NPC 的任务）的就当做不到，不列。
func (w *World) questSettable() map[int]bool {
	if w.qsettable != nil {
		return w.qsettable
	}
	set := map[int]bool{71: true}
	for _, ev := range transSetEnd {
		set[ev] = true
	}
	for _, ev := range transSixEvents {
		set[ev] = true
	}
	for _, npcs := range w.game.NPCs {
		for _, n := range npcs {
			for _, seg := range strings.Split(n.Arg, "|") {
				for _, key := range []string{"EventNo", "EndSetFlg", "NowSetFlg"} {
					if v, ok := strings.CutPrefix(strings.TrimSpace(seg), key+":"); ok {
						for _, f := range strings.Split(v, ",") {
							if ev := data.Atoi(f); ev > 0 {
								set[ev] = true
							}
						}
					}
				}
			}
		}
	}
	w.qsettable = set
	return set
}

func (w *World) itemDefName(id int) string {
	if d := w.game.Items[id]; d != nil {
		return d.Name
	}
	return fmt.Sprintf("道具 %d", id)
}

// savePointName：记录点编号（ID）所在的楼层名。
func (w *World) savePointName(id int) string {
	for floor, npcs := range w.game.NPCs {
		for _, n := range npcs {
			if n.Function == "SavePoint" {
				if v, ok := data.ArgField(n.Arg, "ID"); ok && data.Atoi(v) == id {
					if f := w.game.Floors[floor]; f != nil && f.Name != "" {
						return f.Name
					}
				}
			}
		}
	}
	return fmt.Sprintf("第 %d 个村子", id)
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
