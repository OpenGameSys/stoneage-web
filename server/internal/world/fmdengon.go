package world

// 家族布告栏（npc_fmdengon.c，_FM_MODIFY：窗口都不带 NPC，也能从家族选单的"家族布告栏"（FM D）打开）：
// 家族成员列表（WN 18）、家族留言（WN 19）、家族之间留言板（WN 20）、申请家族据点（WN 21）、家族之间强者表（WN 22 / 23 / 42 / 43）、
// 庄园占据时间表。庄园族战没有做，族战时间表永远是空的。

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 窗口编号（CHAR_WINDOWTYPE_FM_*）
const (
	seqFmDengon     = 370
	seqFmsDengon    = 371
	seqFmMessage1   = 372 // 据点的说明
	seqFmMessage2   = 373 // 成员列表的说明
	seqFmSelect     = 374
	seqFmMemberList = 375
	seqFmPointList  = 376
	seqFmDPTop      = 377
	seqFmDPMe       = 378
	seqFmDPSelect   = 379
)

const fmNotMember = "              『警       告』\n 抱歉！你不是家族人员，不得使用公布栏。"

// fmDengonMenu 是 NPC_FmDengonLooked（_FM_MODIFY 版）。
func (w *World) fmDengonMenu(e *entity) {
	menu := "                  『家族布告栏』" +
		"\n                   家族成员列表" +
		"\n                     家族留言" +
		"\n                  家族之间留言板" +
		"\n                   申请家族据点" +
		"\n                  家族之间强者表" +
		"\n                  庄园族战时间表" +
		"\n                  庄园占据时间表"
	w.fmWindow(e, "select", BtnCancel, seqFmSelect, menu)
}

// fmDengonTalk：布告栏 NPC（FmDengon）点了就开选单。
func (w *World) fmDengonTalk(p *entity) {
	defer w.famLock()()
	w.fmDengonMenu(p)
}

// fmWindowReply 是 NPC_FmDengonWindowTalked（itemWindowReply 按编号转来）。回传 false 表示不是家族的窗口。
func (w *World) fmWindowReply(e *entity, win *npcWindow, button int, data string) bool {
	if win.seq < seqFmDengon || win.seq > seqFmDPSelect {
		return false
	}
	defer w.famLock()()
	fm, _, m := w.fmOf(e)
	switch win.seq {
	case seqFmSelect:
		w.fmSelect(e, fm, m, data)
	case seqFmMessage2:
		if button&BtnOK != 0 && fm != nil {
			w.fmMemberList(e, fm, 0)
		}
	case seqFmMemberList:
		if fm == nil {
			return true
		}
		start := fmInt(fmSplit(data), 0)
		switch {
		case button&BtnNext != 0:
			start += 10
		case button&BtnPrev != 0:
			start -= 10
		default:
			return true
		}
		w.fmMemberList(e, fm, start)
	case seqFmDengon:
		if fm == nil {
			return true
		}
		f := fmSplit(data)
		w.fmBoard(e, &fm.Memos, fmMemoMax, "fmboard", seqFmDengon, button, fmInt(f, 0), fmField(f, 1), func() { w.fam.save(fm) })
	case seqFmsDengon:
		f := fmSplit(data)
		if button&BtnOK != 0 && (m == nil || m.Duty != fmDutyLeader) {
			w.fmMessage(e, "              『警       告』\n 抱歉！你不是族长，所以仅能查看。")
			return true
		}
		w.fmBoard(e, &w.fam.fmsMemo, fmsMemoMax, "fmsboard", seqFmsDengon, button, fmInt(f, 0), fmField(f, 1), func() { w.fam.saveKV("fmsmemo", w.fam.fmsMemo) })
	case seqFmMessage1:
		if button&BtnOK != 0 {
			w.fmWindow(e, "fmpoints", BtnOK, seqFmPointList, w.fmPointList())
		}
	case seqFmDPSelect:
		w.fmDPSelect(e, fm, fmInt(fmSplit(data), 0))
	case seqFmDPTop:
		start := fmInt(fmSplit(data), 0)
		switch {
		case button&BtnNext != 0:
			start += 10
		case button&BtnPrev != 0:
			start -= 10
		default:
			return true
		}
		if start < 0 || start >= 30 {
			return true
		}
		w.fmTop30(e, start)
	case seqFmDPMe:
		if button&BtnPrev != 0 {
			w.fmDPMenu(e)
		}
	}
	return true
}

// fmSelect 是布告栏选单（CHAR_WINDOWTYPE_FM_SELECT）。
func (w *World) fmSelect(e *entity, fm *family, m *fmMember, data string) {
	switch fmInt(fmSplit(data), 0) {
	case 1: // 家族成员列表：只有族长、长老能开
		if fm == nil {
			w.fmMessage(e, fmNotMember)
			return
		}
		if m.Duty == fmDutyLeader || m.Duty == fmDutyElder {
			w.fmWindow(e, "message", BtnOK, seqFmMessage2, "               『族 长 需 知』\n请小心处理族员的资料，一经修改後就无法回复原态，敬请小心。")
		}
	case 2: // 家族留言
		if fm == nil {
			w.fmMessage(e, fmNotMember)
			return
		}
		w.fmBoard(e, &fm.Memos, fmMemoMax, "fmboard", seqFmDengon, 0, 0, "", nil)
	case 3: // 家族之间留言板
		w.fmBoard(e, &w.fam.fmsMemo, fmsMemoMax, "fmsboard", seqFmsDengon, 0, 0, "", nil)
	case 4: // 申请家族据点
		text := "               『列 表 需 知』\n此表族长可以申请，其馀仅能查看。"
		if m != nil && m.Duty == fmDutyLeader {
			text = "               『族 长 需 知』\n请小心慎选所申请的据点，一但选取据点後就无法回复原态，敬请小心。"
		}
		w.fmWindow(e, "message", BtnOK, seqFmMessage1, text)
	case 5:
		w.fmDPMenu(e)
	case 6: // 庄园族战时间表（_FM_FMPOINTPK_LIST）：没有庄园争夺战
		w.fmWindow(e, "widemessage", BtnOK, seqNone, "               『庄园族战时间表』\n\n目前没有排定的庄园族战。")
	case 7: // 庄园占据时间表（原版是客户端另做的 WN 1020，这里用宽的文字窗口）
		var sb strings.Builder
		sb.WriteString("               『庄园占据时间表』\n")
		now := w.now()
		for i, mt := range manorTable {
			owner, since := "没有任何家族", ""
			if s := w.fam.manors[i]; s.Owner > 0 && w.fam.list[s.Owner] != nil {
				owner = w.fam.list[s.Owner].Name
				d := now.Sub(time.Unix(s.Since, 0))
				since = fmt.Sprintf("已占据 %d 天 %d 小时", int(d.Hours())/24, int(d.Hours())%24)
			}
			fmt.Fprintf(&sb, "\n%-14s %-16s %s", mt.name, owner, since)
		}
		w.fmWindow(e, "widemessage", BtnOK, seqNone, sb.String())
	}
}

// fmMemberList 是 WN 18（WINDOW_FMMESSAGETYPE_SELECT）：10 个族人一页 "格位|名字|等级|在线|声望|能否当族长|职位|星球|气势"，
// 最后一行是 "是否继续召募家族人员|这页的第一个|招募中"。
func (w *World) fmMemberList(e *entity, fm *family, start int) {
	var list []int
	for i, m := range fm.Members {
		if m != nil {
			list = append(list, i)
		}
	}
	if start >= len(list) {
		start -= 10
	}
	start = max(start, 0)
	var sb strings.Builder
	for k := start; k < start+10; k++ {
		if k < len(list) {
			i := list[k]
			m := fm.Members[i]
			online := b2i(w.fam.online[m.ID] != nil)
			fmt.Fprintf(&sb, "%d|%s|%d|%d|%d|%d|%d|%d|%d", i, fmEscape(m.Name), m.Level, online, m.Fame/100, b2i(m.Event), m.Duty, online, m.Momentum/100)
		}
		sb.WriteByte('\n')
	}
	fmt.Fprintf(&sb, "是否继续召募家族人员|%d|%d\n", start, b2i(fm.Accept))
	buttons := BtnOK
	switch {
	case start+10 >= len(list) && start > 0:
		buttons |= BtnPrev
	case start+10 >= len(list):
	case start == 0:
		buttons |= BtnNext
	default:
		buttons |= BtnPrev | BtnNext
	}
	w.fmWindow(e, "fmmembers", buttons, seqFmMemberList, sb.String())
}

// fmBoard 是家族留言板（WN 19 / 20）：每页 7 则，最新的在最下面；一则是 "内容|月/日 时:分 名字"（内容跳脱过）。
// idx 是这页最后一则的位置（至少 6），翻页每次 7 则；OK 带文字就是留言（加在最后、超过上限删最旧的）。
func (w *World) fmBoard(e *entity, memos *[]string, limit int, kind string, seq, button, idx int, text string, save func()) {
	last := max(6, len(*memos)-1)
	switch {
	case button&BtnOK != 0:
		text = strings.TrimSpace(strings.NewReplacer("\n", " ", "\r", " ").Replace(text))
		if text == "" {
			return
		}
		text = cutBytes(text, 58) // 输入行的长度
		t := w.now()
		*memos = append(*memos, fmt.Sprintf("%s|%2d/%02d %2d:%02d %s", fmEscape(text), int(t.Month()), t.Day(), t.Hour(), t.Minute(), e.char.Name))
		if len(*memos) > limit {
			*memos = (*memos)[len(*memos)-limit:]
		}
		save()
		last = max(6, len(*memos)-1)
		idx = last
	case button&BtnNext != 0:
		idx = min(idx+7, last)
	case button&BtnPrev != 0:
		idx = max(idx-7, 6)
	case button == 0:
		idx = last
	default:
		return
	}
	idx = min(max(idx, 6), last)
	var sb strings.Builder
	for i := idx - 6; i <= idx; i++ {
		if i < len(*memos) {
			sb.WriteString((*memos)[i])
		}
		sb.WriteByte('\n')
	}
	fmt.Fprintf(&sb, "%d\n", idx)
	buttons := BtnOK | BtnCancel
	if idx > 6 {
		buttons |= BtnPrev
	}
	if idx < last {
		buttons |= BtnNext
	}
	w.fmWindow(e, kind, buttons, seq, sb.String())
}

// fmPointList 是 fmpointlist.pointlistarray：每个庄园一行 "编号|村|东|南|占有的家族|家族名|家族声望|家族格位|村号|占有的时间"。
func (w *World) fmPointList() string {
	var sb strings.Builder
	for i, mt := range manorTable {
		s := w.fam.manors[i]
		name, pop := "", 0
		if fm := w.fam.list[s.Owner]; fm != nil {
			name, pop = fm.Name, fm.popular()
		} else {
			s = manorState{Owner: -1}
		}
		fmt.Fprintf(&sb, "%d|%d|%d|%d|%d|%s|%d|%d|%d|%d\n", i, mt.town, mt.x, mt.y, s.Owner, fmEscape(name), pop, s.Owner-1, mt.village, s.Since)
	}
	return sb.String()
}

// fmDPMenu 是强者表的选单（CHAR_WINDOWTYPE_FM_DPSELECT，_NEW_MANOR_LAW 没有合成、料理）。
func (w *World) fmDPMenu(e *entity) {
	w.fmWindow(e, "select", 0, seqFmDPSelect, "\n              三十大家族声望列表\n"+
		"              十大冒险家族\n"+
		"              十大饲育家族\n"+
		"              十大战斗家族\n"+
		"              十大气势家族\n"+
		"              自己家族声望列表\n"+
		"              自己家族气势排名\n")
}

// fmTopLine 是综合排行的一行（AC 的 FM_TOP_INTEGRATE）："格位|名次|家族名|族长|家族声望|家族总声望|自己家族"。
// 原版第 7 段是气势，客户端却当成"自己家族"的记号；这里照客户端送记号。
func fmTopLine(fm *family, rank int, mine bool) string {
	return fmt.Sprintf("%d|%d|%s|%s|%d|%d|%d", fm.ID-1, rank, fmEscape(fm.Name), fmEscape(fm.leader().Name), fm.popular()/100, fm.total()/100, b2i(mine))
}

// fmTop30 是 WN 22：三十大家族声望列表，10 个一页，最后一行是这页的第一个。
func (w *World) fmTop30(e *entity, start int) {
	list := w.fam.rank((*family).total)
	var sb strings.Builder
	for i := start; i < start+10; i++ {
		if i < len(list) {
			sb.WriteString(fmTopLine(list[i], i+1, false))
		}
		sb.WriteByte('\n')
	}
	fmt.Fprintf(&sb, "%d\n", start)
	buttons := BtnOK
	switch {
	case start+10 >= 30:
		buttons |= BtnPrev
	case start == 0:
		buttons |= BtnNext
	default:
		buttons |= BtnPrev | BtnNext
	}
	w.fmWindow(e, "fmtop30", buttons, seqFmDPTop, sb.String())
}

// fmDPSelect 是强者表选单的回传：1 三十大、2 冒险、3 饲育、4 战斗、5 气势、6 自己家族附近的声望、7 自己家族的气势。
func (w *World) fmDPSelect(e *entity, mine *family, n int) {
	fam := w.fam
	top10 := func(kind string, key func(*family) int) {
		var sb strings.Builder
		for i, fm := range fam.rank(key) {
			if i >= 10 {
				break
			}
			fmt.Fprintf(&sb, "%d|%d|%s|%s|%d\n", fm.ID-1, i+1, fmEscape(fm.Name), fmEscape(fm.leader().Name), key(fm)/100)
		}
		w.fmWindow(e, kind, BtnOK|BtnPrev, seqFmDPMe, sb.String())
	}
	switch n {
	case 1:
		w.fmTop30(e, 0)
	case 2:
		top10("fmdp", func(f *family) int { return f.Adv })
	case 3:
		top10("fmdp", func(f *family) int { return f.Feed })
	case 4:
		top10("fmdp", func(f *family) int { return f.PK })
	case 5:
		top10("fmmomentum", (*family).momentum)
	case 6, 7:
		if mine == nil {
			w.fmMessage(e, fmNotMember)
			return
		}
		key := (*family).total
		if n == 7 {
			key = (*family).momentum
		}
		list := fam.rank(key)
		at := 0
		for i, fm := range list {
			if fm == mine {
				at = i
			}
		}
		if n == 7 {
			_, m := mine.member(e.char.ID)
			text := fmt.Sprintf("%d|%d|%s|%s|%d|%d\n", mine.ID-1, at+1, fmEscape(mine.Name), fmEscape(mine.leader().Name), mine.momentum()/100, m.Momentum/100)
			w.fmWindow(e, "fmownmomentum", BtnOK|BtnPrev, seqFmDPMe, text)
			return
		}
		start := max(0, min(at-4, len(list)-10))
		var sb strings.Builder
		for i := start; i < start+10; i++ {
			if i < len(list) {
				sb.WriteString(fmTopLine(list[i], i+1, list[i] == mine))
			}
			sb.WriteByte('\n')
		}
		sb.WriteString(strconv.Itoa(start) + "\n")
		w.fmWindow(e, "fmtop30", BtnOK|BtnPrev, seqFmDPMe, sb.String())
	}
}
