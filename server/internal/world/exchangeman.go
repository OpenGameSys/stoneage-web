package world

import (
	"fmt"
	"strings"

	"sa/server/internal/data"
)

// 任务交换（npc_exchangeman.c，functionset ExChangeMan，340 个）：参数以 EventEnd 分成若干事件段，按 EventNo / TYPE（REQUEST、ACCEPT、
// MESSAGE、CLEAN）与条件找出第一个成立的段，显示对应的窗口，交出 / 拿到道具与石币、设定任务旗标、传送 NPC 等。
//
// 原版的流程分散在 NPC_TypeCheck（说话时选段）、NPC_MsgDisp（按编号显示窗口，多数编号会重新选一次段）、NPC_EventAdd（REQUEST
// 接下任务时给东西、放弃时收回）与 NPC_AcceptDel（ACCEPT / MESSAGE 交换）。构建宏 _NEWEVENT、_EXCHANGEMAN_REQUEST_DELPET、
// _NPC_EXCHANGEMANTRANS、_ADD_reITEM 有定义；_ITEM_PILENUMS、_ANGEL_SUMMON、_NPC_FUSION、_CHAR_PROFESSION 等没有。
// 宠物条件、GetPet、DelPet 见 petnpc.go；GetEgg（_NPC_FUSION）与 pet_skill（数据里没有用到）没做。

// 窗口编号（CHAR_WINDOWTYPE_WINDOWEVENT_*）
const (
	seqExStart       = 231 // STARTMSG：一般的文字窗口
	seqExNowEvent    = 232 // NOWEVENT：要不要放弃进行中的委托
	seqExReqMain     = 234 // REQMAINMSG：委托内容（是 / 否）
	seqExAccMain     = 235 // ACCMAINMSG：交换内容（是 / 否）
	seqExClean       = 237 // CLEANMSG：要不要清掉旗标
	seqExReqThank    = 238 // REQTHANK：接下委托的道谢（分页）
	seqExAccThank    = 239 // ACCTHANK：没有 _NEWEVENT 时交换完的道谢（这里用不到）
	seqExAccThankNew = 430 // ACCTHANKNEW：_NEWEVENT 的交换道谢，最后一页按 OK 才交换
)

// NPC_MsgDisp 的编号（照原版数字）
const (
	exStop         = 0  // StopMsg：进行中的委托要放弃吗
	exNoStop       = 1  // NoStopMsg：不放弃
	exThanks       = 2  // ThanksMsg：接下委托（NPC_EventAdd）
	exItemFull     = 4  // ItemFullMsg
	exPetFull      = 10 // PetFullMsg：宠物栏满了
	exRequest      = 5  // RequestMsg
	exAccept       = 6  // AcceptMsg
	exEndStop      = 7  // EndStopMsg：放弃委托（收回给的东西）
	exAccDone      = 8  // 交换（NPC_AcceptDel），不发窗口
	exCleanMain    = 11 // CleanMainMsg
	exCleanDone    = 12 // CleanFlgMsg：清掉 CleanFlg
	exStoneFull    = 13 // StoneFullMsg
	exStoneLess    = 14 // StoneLessMsg
	exReqThankPage = 22 // ThanksMsg 的下一页（REQTHANK）
	exReqPage      = 55 // RequestMsg 的下一页
	exAccPage      = 66 // AcceptMsg 的下一页
	exAccThanks    = 81 // _NEWEVENT 的 ThanksMsg 各页（ACCTHANKNEW）
	exAccThankPage = 82 // ThanksMsg 的下一页（ACCTHANK）
	exStartPage    = 99 // NomalWindowMsg1.. 各页（STARTMSG），最后一页才交换
)

// NPC_EventBigSmallLastCheck 的比较方式
const (
	exEq = iota
	exLess
	exGreater
	exNe
)

// exState 是原版记在玩家身上的工作值：key 是 KeyWord 说对的那一段（CHAR_WORKSHOPRELEVANT，段号 + 1，0 为没有），
// page 是分页（CHAR_WORKSHOPRELEVANTTRD：(段号 + 1) × 100 + 第几页；_NEWEVENT 的交换道谢直接从 0 数页）。
// 窗口打开时记在 npcWindow 的 cost / event，回传时取回。
type exState struct{ key, page int }

// exchangeTalk 是 NPC_ExChangeManTalked；talk 是对着 NPC 说的话（点选时为空，KeyWord 段要说对才成立）。
// 要和 NPC 面对面（2 格内）或站在旁边面向它。
func (w *World) exchangeTalk(p, n *entity, talk string) {
	defer w.flushQuests(p)
	if !faceToFace(n, p, 2) && !inFront(p, n, 1) {
		return
	}
	w.exTypeCheck(p, n, talk)
}

// exTypeCheck 是 NPC_TypeCheck：依序看每一段，跳过没有 EventNo 的、事件做完了的、KeyWord 不是这句话的，
// 条件成立且有 TYPE 的就照 TYPE 处理；一段都不成立时随机说一句 NomalMainMsg（以 "," 分开）。
func (w *World) exTypeCheck(p, n *entity, talk string) {
	arg := n.npc.Arg
	for k, b := range strings.Split(arg, "EventEnd") {
		st := exState{}
		no, ok := data.ArgField(b, "EventNo")
		if !ok {
			continue
		}
		evNo := data.Atoi(no)
		if strings.Contains(no, "-") {
			evNo = -1
		}
		if p.eventDone(evNo) {
			continue
		}
		if kw, ok := data.ArgField(b, "KeyWord"); ok {
			if kw != talk {
				continue
			}
			st.key = k + 1
		}
		if w.exEventCheck(p, b, n) < 0 {
			continue
		}
		typ, _ := data.ArgField(b, "TYPE")
		switch {
		case strings.Contains(typ, "REQUEST"): // NPC_RequestMain：已经接下的问要不要放弃，否则说明委托
			if p.eventNow(data.Atoi(no)) {
				w.exMsgDisp(p, n, exStop, st)
			} else {
				w.exMsgDisp(p, n, exRequest, st)
			}
			return
		case strings.Contains(typ, "ACCEPT"): // NPC_AcceptMain
			w.exMsgDisp(p, n, exAccept, st)
			return
		case strings.Contains(typ, "MESSAGE"):
			w.exMessage(p, n, b, evNo, st)
			return
		case strings.Contains(typ, "CLEAN"): // NPC_CleanMain
			w.exMsgDisp(p, n, exCleanMain, st)
			return
		}
	}
	if msg, ok := data.ArgField(arg, "NomalMainMsg"); ok {
		lines := strings.Split(msg, ",")
		w.npcSay(p, n, lines[w.rnd.IntN(len(lines))])
	}
}

// exMessage 是 NPC_TypeCheck 的 MESSAGE：有 NomalMsg 的先交换（NPC_AcceptDel）再用说的；有 NomalWindowMsg 的交换后用窗口显示
// （"是"按钮）。写了 NomalWindowMsg1 的是分页，先显示第一页（"下一页"），到最后一页才交换（NPC_MsgDisp 99）。
// 交换成功后设"进行中"，传送 NPC，按 EndSetFlg / CleanFlg 设旗标。
func (w *World) exMessage(p, n *entity, b string, evNo int, st exState) {
	msg, say := data.ArgField(b, "NomalMsg")
	if !say {
		var ok bool
		if msg, ok = data.ArgField(b, "NomalWindowMsg"); !ok {
			return
		}
		if _, ok := data.ArgField(b, "NomalWindowMsg1"); ok {
			w.exWindow(p, n, BtnNext, seqExStart, msg, st)
			return
		}
	}
	if !w.exAcceptDel(p, n, st) {
		return
	}
	if evNo != -1 {
		p.setEventNow(evNo)
	}
	if say {
		w.npcSay(p, n, msg)
	}
	w.exWarpNpc(n, b)
	w.exEndFlags(p, b, evNo)
	w.saveProgress(p)
	if !say {
		w.exWindow(p, n, BtnYes, seqExStart, msg, st)
	}
}

// exchangeReply 是 NPC_ExChangeManWindowTalked：离 NPC 超过 2 格不处理；按窗口编号与按钮决定下一个 NPC_MsgDisp。
func (w *World) exchangeReply(p, n *entity, win *npcWindow, button int, _ string) {
	defer w.flushQuests(p)
	if max(abs(n.x-p.x), abs(n.y-p.y)) > 2 {
		return
	}
	st := exState{key: win.cost, page: win.event}
	// 下一页 / 上一页：页数加减一后显示 num
	turn := func(num int) {
		switch button {
		case BtnNext:
			st.page++
		case BtnPrev:
			st.page--
		default:
			return
		}
		w.exMsgDisp(p, n, num, st)
	}
	switch win.seq {
	case seqExClean:
		if button == BtnYes {
			w.exMsgDisp(p, n, exCleanDone, st)
		}
	case seqExStart:
		turn(exStartPage)
	case seqExReqThank:
		turn(exReqThankPage)
	case seqExAccThank:
		turn(exAccThankPage)
	case seqExNowEvent:
		switch button {
		case BtnYes:
			w.exMsgDisp(p, n, exEndStop, st)
		case BtnNo:
			w.exMsgDisp(p, n, exNoStop, st)
		}
	case seqExReqMain:
		if button == BtnYes {
			st.page = 0
			w.exMsgDisp(p, n, exThanks, st)
		} else {
			turn(exReqPage)
		}
	case seqExAccMain:
		if button == BtnYes {
			st.page = 0
			w.exMsgDisp(p, n, exAccThanks, st)
		} else {
			turn(exAccPage)
		}
	case seqExAccThankNew:
		if button == BtnOK {
			st.page = 0
			w.exMsgDisp(p, n, exAccDone, st)
		} else {
			turn(exAccThanks)
		}
	}
}

// exWindow 发文字窗口（参数里写成两个字的 \n 是换行），并把 exState 记在窗口上。
func (w *World) exWindow(p, n *entity, buttons, seq int, text string, st exState) {
	w.sendWindow(p, n, "message", buttons, seq, npcText(text))
	p.window.cost, p.window.event = st.key, st.page
}

// exMsgDisp 是 NPC_MsgDisp：分页的几种（22、55、66、82）用 page 记的段，其余重新选段（exSelect）。
// 原版在一段都不成立时会拿最后一段、参数里找不到对应文字时会发出没初始化的字串；这里都不发窗口。
func (w *World) exMsgDisp(p, n *entity, num int, st exState) {
	var (
		k, evNo int
		b       string
	)
	switch num {
	case exReqThankPage, exReqPage, exAccPage, exAccThankPage:
		blocks := strings.Split(n.npc.Arg, "EventEnd")
		if k = st.page/100 - 1; k < 0 || k >= len(blocks) {
			return
		}
		b = blocks[k]
	default:
		var ok bool
		if k, b, evNo, _, ok = w.exSelect(p, n, st.key); !ok {
			return
		}
	}
	first := (k+1)*100 + 1 // 原版 CHAR_WORKSHOPRELEVANTTRD = 1 + i × 100：这一段的第 1 页
	has := func(key string) bool { return strings.Contains(b, key) }
	text, found := "", false
	field := func(key string) { text, found = data.ArgField(b, key) }
	buttons, seq := BtnOK, seqExStart
	out := func() {
		if found {
			w.exWindow(p, n, buttons, seq, text, st)
		}
	}
	switch num {
	case exStop:
		if field("StopMsg"); found {
			buttons, seq = BtnYesNo, seqExNowEvent
		}
	case exNoStop:
		field("NoStopMsg")
	case exEndStop:
		// 放弃委托：收回接下时给的道具（NPC_EventAdd 模式 1），清掉"进行中"，魅力减 1
		if field("EndStopMsg"); found {
			w.exEventAdd(p, n, 1, st)
			p.clearEventNow(evNo)
			if p.char.Charm > 0 {
				p.char.Charm--
				w.sendStatus(p)
			}
			w.saveProgress(p)
		}
	case exThanks:
		if field("ThanksMsg"); found {
			if !w.exEventAdd(p, n, 0, st) {
				return
			}
			p.setEventNow(evNo)
			w.exEndFlags(p, b, evNo)
			w.saveProgress(p)
		}
		st.page = first
		w.exWarpNpc(n, b)
		if has("ThanksMsg2") {
			buttons, seq = BtnNext, seqExReqThank
		}
	case exReqThankPage:
		text, found, buttons, seq = exPage(b, "ThanksMsg", st.page, seqExReqThank, BtnOK)
	case exAccThankPage:
		text, found, buttons, seq = exPage(b, "ThanksMsg", st.page, seqExAccThank, BtnOK)
	case exReqPage:
		text, found, buttons, seq = exPage(b, "RequestMsg", st.page, seqExReqMain, BtnYesNo)
	case exAccPage:
		text, found, buttons, seq = exPage(b, "AcceptMsg", st.page, seqExAccMain, BtnYesNo)
	case exRequest, exAccept:
		key, main := "RequestMsg", seqExReqMain
		if num == exAccept {
			key, main = "AcceptMsg", seqExAccMain
		}
		if field(key); found {
			text = exCostText(p, b, text)
			buttons, seq = BtnYesNo, main
		}
		st.page = first
		if has(key + "2") {
			buttons, seq = BtnNext, main
		}
	case exAccThanks:
		// 第 0 页是 ThanksMsg（写了 ThanksMsg1 时是 ThanksMsg1），之后是 ThanksMsg2、3……；最后一页是 OK，按了才交换
		seq = seqExAccThankNew
		work := st.page % 100
		next := fmt.Sprintf("ThanksMsg%d", work+2)
		if work == 0 && !has("ThanksMsg1") {
			field("ThanksMsg")
			next = "ThanksMsg2"
		} else {
			field(fmt.Sprintf("ThanksMsg%d", work+1))
		}
		if has(next) {
			buttons = BtnNext
		}
	case exAccDone:
		if _, ok := data.ArgField(b, "ThanksMsg"); ok {
			if !w.exAcceptDel(p, n, st) {
				return
			}
			w.exEndFlags(p, b, evNo)
			if v, ok := data.ArgField(b, "Charm"); ok && p.char.Charm < 100 && evNo > 0 {
				p.char.Charm = min(100, p.char.Charm+data.Atoi(v))
				w.sendStatus(p)
			}
			w.saveProgress(p)
		}
		w.exWarpNpc(n, b)
		return // _NEWEVENT：交换完不再发窗口
	case exStartPage:
		work := st.page % 100
		if work == 1 {
			field("NomalWindowMsg1")
			if has("NomalWindowMsg2") {
				buttons = BtnNext
			}
			break
		}
		field(fmt.Sprintf("NomalWindowMsg%d", work))
		if has(fmt.Sprintf("NomalWindowMsg%d", work+1)) {
			buttons = BtnNext
			break
		}
		// 最后一页：交换、设旗标、传送 NPC，窗口换成"是"
		if !w.exAcceptDel(p, n, st) {
			return
		}
		if evNo != -1 {
			p.setEventNow(evNo)
		}
		w.exWarpNpc(n, b)
		w.exEndFlags(p, b, evNo)
		w.saveProgress(p)
		buttons = BtnYes
	case exItemFull:
		field("ItemFullMsg")
	case exPetFull:
		field("PetFullMsg")
	case exStoneFull:
		field("StoneFullMsg")
	case exStoneLess:
		field("StoneLessMsg")
	case exCleanMain:
		if field("CleanMainMsg"); found {
			buttons, seq = BtnYesNo, seqExClean
		}
	case exCleanDone:
		if field("CleanFlgMsg"); found {
			exCleanFlags(p, b)
			w.saveProgress(p)
		}
	}
	out()
}

// exPage 是 NPC_MsgDisp 22 / 55 / 66 / 82 的分页：第 page%100 页，第 1 页是 key 本身，之后是 key2、key3……
// 后面还有就是"下一页"，没有了用 last 的按钮；第 1 页就是最后一页时原版落到预设的 OK、STARTMSG。
func exPage(b, key string, page, seq, last int) (text string, found bool, buttons, rseq int) {
	work := page % 100
	if work == 1 {
		text, found = data.ArgField(b, key)
		if strings.Contains(b, key+"2") {
			return text, found, BtnNext, seq
		}
		return text, found, BtnOK, seqExStart
	}
	text, found = data.ArgField(b, fmt.Sprintf("%s%d", key, work))
	if strings.Contains(b, fmt.Sprintf("%s%d", key, work+1)) {
		return text, found, BtnNext, seq
	}
	return text, found, last, seq
}

// exCostText：说明里的 %8d 填 DelStone 的价钱（原版 sprintf(token, buf2, cost)）。
func exCostText(p *entity, b, msg string) string {
	if !strings.Contains(msg, "%8d") {
		return msg
	}
	s, _ := data.ArgField(b, "DelStone")
	return strings.Replace(msg, "%8d", fmt.Sprintf("%8d", exCost(p, s)), 1)
}

// exCost 是 NPC_EventGetCost："LV*n" 按等级 × n，否则就是数字。
func exCost(p *entity, s string) int {
	if strings.Contains(s, "LV") {
		_, k, _ := strings.Cut(s, "*")
		return p.char.Level * data.Atoi(k)
	}
	return data.Atoi(s)
}

// exEndFlags：EndSetFlg 有写时先清掉这个事件的"进行中"（NPC_NowEventSetFlgCls），再把列出的编号设为做完（NPC_EventSetFlg）；
// 接着做 CleanFlg。
func (w *World) exEndFlags(p *entity, b string, evNo int) {
	if s, ok := data.ArgField(b, "EndSetFlg"); ok {
		if evNo != -1 {
			p.clearEventNow(evNo)
		}
		for _, f := range strings.Split(s, ",") {
			if n := data.Atoi(f); !p.eventDone(n) {
				w.addFMAdv(p, n) // NPC_EventSetFlg：第一次做完才给
			}
			p.setEventDone(data.Atoi(f))
		}
	}
	exCleanFlags(p, b)
}

// exCleanFlags：CleanFlg 列出的编号"做完""进行中"都清掉（NPC_NowEndEventSetFlgCls）。
func exCleanFlags(p *entity, b string) {
	if s, ok := data.ArgField(b, "CleanFlg"); ok {
		for _, f := range strings.Split(s, ",") {
			p.clearEvent(data.Atoi(f))
		}
	}
}

// ---------------- 选段与条件 ----------------

// exSelect 是 NPC_MsgDisp / NPC_EventFile 开头的选段：跳过做完的事件与 KeyWord 不是刚才说对的那段，取第一个条件成立的（不看 TYPE）。
// 回传段号、段落、事件编号（atoi，没写为 0）与成立的是 EVENT 的第几组（从 1 起）。
func (w *World) exSelect(p, n *entity, key int) (k int, b string, evNo, group int, ok bool) {
	for k, b := range strings.Split(n.npc.Arg, "EventEnd") {
		no, _ := data.ArgField(b, "EventNo")
		evNo := data.Atoi(no)
		if p.eventDone(evNo) {
			continue
		}
		if _, has := data.ArgField(b, "KeyWord"); has && key != k+1 {
			continue
		}
		if g := w.exEventCheck(p, b, n); g > 0 {
			return k, b, evNo, g, true
		}
	}
	return 0, "", 0, 0, false
}

// exEventCheck 是 NPC_ExChangeManEventCheck：EVENT 以 "," 分成几组（任一组成立即可），组内以 "&" 连接（全部成立），
// 回传成立的第几组（从 1 起），都不成立（或没写 EVENT）为 -1。n 是说话的 NPC（宠物条件的 Pet_Name 要用，可为 nil）。
func (w *World) exEventCheck(p *entity, b string, n *entity) int {
	ev, _ := data.ArgField(b, "EVENT")
	name, need := "", false
	if n != nil {
		name, need = exPetName(n, b)
	}
	for i, g := range strings.Split(ev, ",") {
		ok := true
		for _, c := range strings.Split(g, "&") {
			if !w.exBigSmall(p, c, name, need) {
				ok = false
				break
			}
		}
		if ok {
			return i + 1
		}
	}
	return -1
}

// exGroup 取 EVENT 的第 g 组（从 1 起，EVDEL 按成立的那组交出道具）。
func exGroup(b string, g int) string {
	ev, _ := data.ArgField(b, "EVENT")
	if gs := strings.Split(ev, ","); g >= 1 && g <= len(gs) {
		return gs[g-1]
	}
	return ""
}

// exBigSmall 是 NPC_EventBigSmallCheck：含 "PET" 的是宠物条件（NPC_PetLvCheck，petnpc.go；name / needName 是段里的 Pet_Name）；
// 其余按 "<"、">"、"!="、"=" 的顺序找比较符。"=" 的右边带 "*" 的是 NPC_EventReduce（不论左边写什么，都是"编号*个数"的道具数量）。
func (w *World) exBigSmall(p *entity, c, name string, needName bool) bool {
	if strings.Contains(c, "PET") {
		return exPetCheck(p, c, name, needName)
	}
	split := func(op string) (string, int) {
		v := strings.Split(c, op)
		return v[0], data.Atoi(v[1])
	}
	switch {
	case strings.Contains(c, "<"):
		name, n := split("<")
		return w.exFreeIf(p, name, n, exLess)
	case strings.Contains(c, ">"):
		name, n := split(">")
		return w.exFreeIf(p, name, n, exGreater)
	case strings.Contains(c, "!="):
		name, n := split("!=")
		return w.exFreeIf(p, name, n, exNe)
	case strings.Contains(c, "="):
		if strings.Contains(c, "*") {
			return w.exReduce(p, c)
		}
		name, n := split("=")
		return w.exFreeIf(p, name, n, exEq)
	}
	return false
}

// exCmp 是 NPC_EventBigSmallLastCheck：mine 与 want 比较。
func exCmp(want, mine, flg int) bool {
	switch flg {
	case exEq:
		return mine == want
	case exLess:
		return mine < want
	case exGreater:
		return mine > want
	case exNe:
		return mine != want
	}
	return false
}

// exFreeIf 是 NPC_EventFreeIfCheck 里有对应数据的几项：LV、ITEM、ENDEV、NOWEV、SP（记录点）、TIME（SA 时段）、TRANS、reITEM（空格数）。
// 照原版：LV、ENDEV、SP 只在 "!=" 时把结果反过来（所以 "LV!=n" 实际是"等级正好是 n"，ENDEV / SP 的 "<" ">" 与 "=" 相同），
// NOWEV 写成 "!=" 时总是成立。IMAGE、DR、HERO_* 等没有对应的数据或构建宏没定义，不成立。
func (w *World) exFreeIf(p *entity, name string, n, flg int) bool {
	c := &p.char
	flip := func(hit bool) bool {
		if hit {
			return flg != exNe
		}
		return flg == exNe
	}
	switch name {
	case "LV": // NPC_EventLevelCheck
		return flip(exCmp(n, c.Level, flg))
	case "ITEM":
		return w.exItemCheck(p, n, flg)
	case "ENDEV": // NPC_ENDEventNoCheck
		return flip(p.eventDone(n))
	case "NOWEV": // NPC_NOWEventNoCheck
		return p.eventNow(n) || flg == exNe
	case "SP": // NPC_SavePointCheck
		return flip(savePointFlag(p, n))
	case "TIME": // NPC_TiemCheck：getLSTime 的时段
		return exCmp(n, lsSection(w.saHour()), flg)
	case "TRANS": // _NPC_EXCHANGEMANTRANS
		return exCmp(n, c.Trans, flg)
	}
	if strings.Contains(name, "reITEM") { // _ADD_reITEM：道具栏的空格数
		return exCmp(n, w.freeBagSlots(p), flg)
	}
	return false
}

// exItemCheck 是 NPC_EventItemCheck：只看道具栏（不含装备）。"=" 是有一件编号相符；"!=" 是一件都没有；
// "<" ">" 照原版要每一件都符合才继续，最后仍回不成立（等于一律不成立）。
func (w *World) exItemCheck(p *entity, id, flg int) bool {
	for i := data.EquipSlots; i < data.ItemSlots; i++ {
		it := p.inv[i]
		if it == nil {
			continue
		}
		if exCmp(id, it.ID, flg) {
			if flg == exEq {
				return true
			}
			continue
		}
		if flg != exEq {
			return false
		}
	}
	return flg == exNe
}

// exReduce 是 NPC_EventReduce："ITEM=编号*个数"：整个道具栏（含装备）里这个编号的个数（堆叠的按堆叠数）够不够。
func (w *World) exReduce(p *entity, c string) bool {
	v := strings.Split(c, "=")[1]
	id, k, _ := strings.Cut(v, "*")
	want, cnt := data.Atoi(k), 0
	for _, it := range p.inv {
		if it != nil && it.ID == data.Atoi(id) {
			if cnt += it.count(); cnt >= want {
				return true
			}
		}
	}
	return false
}

// lsSection 是 getLSTime：SA 时（0..1023）所属的时段，LS_NIGHT 0、LS_MORNING 1、LS_NOON 2、LS_EVENING 3（handletime.h 的分界）。
func lsSection(hour int) int {
	switch {
	case 700 < hour && hour <= 930:
		return 1
	case 200 < hour && hour <= 300:
		return 3
	case 300 < hour && hour <= 700:
		return 0
	}
	return 2
}

// ---------------- 交出与拿到 ----------------

// exItemRoom 是 NPC_ItemFullCheck：要拿到的道具（GetItem 的个数，GetRandItem 算一格）放不放得下。DelItem 交出的会空出位子：
// 写了个数的按个数算，只写编号的按道具栏里那种有几件算（原版内层回圈借用了外层的计数，数完第一个只写编号的就不再往下看）；
// EVDEL 按成立的那组条件里的 ITEM，NotDel 列出的不交出、不算。mode 1（放弃委托）不检查拿到的部分。
func (w *World) exItemRoom(p *entity, b string, mode int, group string) bool {
	need := 0
	if spec, ok := data.ArgField(b, "DelItem"); ok {
		if strings.Contains(spec, "EVDEL") {
			notDel, hasNot := data.ArgField(b, "NotDel")
			for _, part := range strings.Split(group, "&") {
				if !strings.Contains(part, "ITEM") {
					continue
				}
				v := exAfterEq(part)
				id, k, star := strings.Cut(v, "*")
				if hasNot && exListed(notDel, data.Atoi(id)) {
					continue
				}
				if star {
					need -= data.Atoi(k)
				} else {
					need -= w.exBagEntries(p, data.Atoi(v))
				}
			}
		} else {
			for _, tok := range strings.Split(spec, ",") {
				if _, k, ok := strings.Cut(tok, "*"); ok {
					need -= data.Atoi(k)
					continue
				}
				need -= w.exBagEntries(p, data.Atoi(tok))
				break
			}
		}
	}
	randCnt := 0
	if _, ok := data.ArgField(b, "GetRandItem"); ok && mode == 0 {
		randCnt = 1
		if need == 0 && w.freeBagSlots(p) == 0 {
			return false
		}
	}
	if spec, ok := data.ArgField(b, "GetItem"); ok && mode == 0 {
		for _, tok := range strings.Split(spec, ",") {
			if _, k, ok := strings.Cut(tok, "*"); ok {
				need += data.Atoi(k)
			} else {
				need++
			}
		}
		if w.freeBagSlots(p) < need+randCnt {
			return false
		}
	}
	return true
}

// exAfterEq：条件 "ITEM=…" / "ITEM!=…" 第一个 "=" 之后到下一个 "=" 之前的部分。
func exAfterEq(s string) string {
	if v := strings.Split(s, "="); len(v) > 1 {
		return v[1]
	}
	return ""
}

// exListed：以 "," 分开的编号表里有没有 id（NotDel）。
func exListed(list string, id int) bool {
	for _, s := range strings.Split(list, ",") {
		if data.Atoi(s) == id {
			return true
		}
	}
	return false
}

// exBagEntries：道具栏（不含装备）里这个编号有几格。
func (w *World) exBagEntries(p *entity, id int) int {
	n := 0
	for i := data.EquipSlots; i < data.ItemSlots; i++ {
		if it := p.inv[i]; it != nil && it.ID == id {
			n++
		}
	}
	return n
}

// exTake 从整个道具栏（照原版从第 0 格起，含装备）收走编号 id 的道具：all 时全部，否则 n 个（堆叠的按个数）。
// 每收走一格说一次 say(道具名)。
func (w *World) exTake(p *entity, id, n int, all bool, say func(string) string) bool {
	changed := false
	for i := 0; i < data.ItemSlots && (all || n > 0); i++ {
		it := p.inv[i]
		if it == nil || it.ID != id {
			continue
		}
		name := ""
		if def := w.game.Items[id]; def != nil {
			name = def.Name
		}
		w.sysMsg(p, say(name), colorWhite)
		switch {
		case all:
			p.inv[i] = nil
		case it.count() <= n:
			n -= it.count()
			p.inv[i] = nil
		default:
			it.Pile = it.count() - n
			n = 0
		}
		changed = true
	}
	return changed
}

// exDelSay 是 NPC_EventDelItem 的说法：Break 时"某某 坏了"，否则"交出某某"（只写编号、全部交出的加句号）。
func exDelSay(brk, all bool) func(string) string {
	return func(name string) string {
		switch {
		case brk:
			return name + " 坏了"
		case all:
			return "交出" + name + "。"
		}
		return "交出" + name
	}
}

// exDelItem 是 NPC_EventDelItem："编号*个数" 交出那么多个，只写编号的全部交出（没有也不算失败）。
func (w *World) exDelItem(p *entity, spec string, brk bool) bool {
	changed := false
	for _, tok := range strings.Split(spec, ",") {
		if id, k, ok := strings.Cut(tok, "*"); ok {
			changed = w.exTake(p, data.Atoi(id), data.Atoi(k), false, exDelSay(brk, false)) || changed
		} else {
			changed = w.exTake(p, data.Atoi(tok), 0, true, exDelSay(brk, true)) || changed
		}
	}
	return changed
}

// exDelItemEVDEL 是 NPC_EventDelItemEVDEL：按成立的那组条件里的 ITEM 交出（NotDel 列出的留下，"-1" 为没有）。
func (w *World) exDelItemEVDEL(p *entity, group, notDel string, brk bool) bool {
	changed := false
	keep := func(id int) bool { return !strings.Contains(notDel, "-1") && exListed(notDel, id) }
	for _, part := range strings.Split(group, "&") {
		if !strings.Contains(part, "ITEM") {
			continue
		}
		v := exAfterEq(part)
		if id, k, ok := strings.Cut(v, "*"); ok {
			if !keep(data.Atoi(id)) {
				changed = w.exTake(p, data.Atoi(id), data.Atoi(k), false, exDelSay(brk, false)) || changed
			}
		} else if !keep(data.Atoi(v)) {
			changed = w.exTake(p, data.Atoi(v), 0, true, exDelSay(brk, true)) || changed
		}
	}
	return changed
}

// exDelItems 处理一段的 DelItem（EVDEL 或编号表）。
func (w *World) exDelItems(p *entity, b, group string) bool {
	spec, ok := data.ArgField(b, "DelItem")
	if !ok {
		return false
	}
	brk := strings.Contains(b, "Break")
	if strings.Contains(spec, "EVDEL") {
		notDel, ok := data.ArgField(b, "NotDel")
		if !ok {
			notDel = "-1"
		}
		return w.exDelItemEVDEL(p, group, notDel, brk)
	}
	return w.exDelItem(p, spec, brk)
}

// exRandItem 是 NPC_RandItemGet：GetRandItem 的编号里随机给一个（原版 rand()%n、0 换成 n，各个机会相同），说"收下了某某"。
func (w *World) exRandItem(p *entity, spec string) bool {
	ids := strings.Split(spec, ",")
	def := w.game.Items[data.Atoi(ids[w.rnd.IntN(len(ids))])]
	if def == nil || w.addItem(p.inv, w.newItem(def)) < 0 {
		return false
	}
	w.sysMsg(p, "收下了"+def.Name, colorWhite)
	return true
}

// exAddItems 是 NPC_EventAddItem："编号*个数" 给那么多个，只写编号的给一个，每个说"拿到某某。"；不明的编号或放不下就停（回 false）。
func (w *World) exAddItems(p *entity, spec string) (ok, changed bool) {
	for _, tok := range strings.Split(spec, ",") {
		id, n := tok, 1
		if s, k, star := strings.Cut(tok, "*"); star {
			id, n = s, data.Atoi(k)
		}
		for j := 0; j < n; j++ {
			def := w.game.Items[data.Atoi(id)]
			if def == nil || w.addItem(p.inv, w.newItem(def)) < 0 {
				return false, changed
			}
			w.sysMsg(p, "拿到"+def.Name+"。", colorWhite)
			changed = true
		}
	}
	return true, changed
}

// exEventAdd 是 NPC_EventAdd：mode 0 是接下委托（交出 DelItem、付 DelStone、拿到 GetItem / GetRandItem），
// mode 1 是放弃委托（GetItem 给过的收回；照原版 DelItem 与 DelStone 也照样执行）。放不下显示 ItemFullMsg，钱不够显示 StoneLessMsg。
// 原版 DelItem:EVDEL 在这里读的是没初始化的缓冲区，这里按成立的那组条件处理（数据里 EVDEL 只出现在 ACCEPT）。
func (w *World) exEventAdd(p, n *entity, mode int, st exState) bool {
	_, b, _, g, ok := w.exSelect(p, n, st.key)
	if !ok {
		return false
	}
	group := exGroup(b, g)
	if !w.exItemRoom(p, b, mode, group) {
		w.exMsgDisp(p, n, exItemFull, st)
		return false
	}
	stone, pay := data.ArgField(b, "DelStone")
	cost := exCost(p, stone)
	if pay && p.char.Gold-cost < 0 {
		w.exMsgDisp(p, n, exStoneLess, st)
		return false
	}
	changed := false
	defer func() {
		if changed {
			w.itemsChanged(p)
		}
	}()
	if spec, ok := data.ArgField(b, "GetPet"); ok && mode == 0 && !w.exGetPet(p, spec, 0) {
		w.exMsgDisp(p, n, exPetFull, st) // 接下委托时给的是任务宠物
		return false
	}
	changed = w.exDelItems(p, b, group)
	if pay {
		p.char.Gold -= cost
		w.sysMsg(p, fmt.Sprintf("付出%d的stone。", cost), colorWhite)
		changed = true
	}
	randSpec, hasRand := data.ArgField(b, "GetRandItem")
	randDone := false
	if spec, ok := data.ArgField(b, "GetItem"); ok {
		if mode == 0 {
			slots := len(strings.Split(spec, ","))
			if hasRand {
				slots++
			}
			if w.freeBagSlots(p) < slots {
				w.exMsgDisp(p, n, exItemFull, st)
				return false
			}
			if hasRand {
				randDone = true
				changed = w.exRandItem(p, randSpec) || changed
			}
			ok, add := w.exAddItems(p, spec)
			changed = changed || add
			if !ok {
				return false
			}
		} else {
			changed = w.exDelItem(p, spec, strings.Contains(b, "Break")) || changed
		}
	}
	if hasRand && !randDone && mode == 0 {
		if w.freeBagSlots(p) == 0 {
			w.exMsgDisp(p, n, exItemFull, st)
			return false
		}
		changed = w.exRandItem(p, randSpec) || changed
	}
	if spec, ok := data.ArgField(b, "DelPet"); ok && mode == 0 && !w.exDelPets(p, n, b, spec, group) {
		return false
	}
	return true
}

// exAcceptDel 是 NPC_AcceptDel（mode 0）：先检查放不放得下（ItemFullMsg）、钱够不够付 DelStone（StoneLessMsg）、
// 拿到 GetStone 会不会超过身上的上限（StoneFullMsg），然后拿到石币、交出 DelItem、付钱、拿到 GetRandItem 与 GetItem。
func (w *World) exAcceptDel(p, n *entity, st exState) bool {
	_, b, _, g, ok := w.exSelect(p, n, st.key)
	if !ok {
		return false
	}
	group := exGroup(b, g)
	if !w.exItemRoom(p, b, 0, group) {
		w.exMsgDisp(p, n, exItemFull, st)
		return false
	}
	stone, pay := data.ArgField(b, "DelStone")
	cost := exCost(p, stone)
	if pay && p.char.Gold-cost < 0 {
		w.exMsgDisp(p, n, exStoneLess, st)
		return false
	}
	gs, get := data.ArgField(b, "GetStone")
	gain := data.Atoi(gs)
	if get && p.char.Gold+gain >= maxGold(p.char.Trans) { // CHAR_getMaxHaveGold
		w.exMsgDisp(p, n, exStoneFull, st)
		return false
	}
	if spec, ok := data.ArgField(b, "DelPet"); ok && !w.exDelPets(p, n, b, spec, group) {
		return false
	}
	if spec, ok := data.ArgField(b, "GetPet"); ok && !w.exGetPet(p, spec, 1) {
		w.exMsgDisp(p, n, exPetFull, st)
		return false
	}
	if get {
		p.char.Gold += gain
		w.sysMsg(p, fmt.Sprintf("得到%d的stone", gain), colorWhite)
	}
	w.exDelItems(p, b, group)
	if pay {
		p.char.Gold -= cost
		w.sysMsg(p, fmt.Sprintf("付出%d的stone。", cost), colorWhite)
	}
	if spec, ok := data.ArgField(b, "GetRandItem"); ok {
		w.exRandItem(p, spec)
	}
	if spec, ok := data.ArgField(b, "GetItem"); ok {
		w.exAddItems(p, spec)
	}
	w.itemsChanged(p) // 道具、石币、能力值都重新送一次并存档
	return true
}

// ---------------- 传送 NPC ----------------

// exWarpNpc 是 NPC_EventWarpNpc：NpcWarp 是以 "," 分开的 "楼层.x.y"，NPC 依序换到下一个位置（最后一个之后回到第一个）。
// 原版用 CHAR_WORK_EVENTWARP 记下一个的序号（初值 1）；这里按 NPC 现在站在第几个位置来找下一个（不在表上时用第一个），
// 数据里 NPC 出生在表上的最后一个位置，两者相同。写错（不到三段）的跳过。
func (w *World) exWarpNpc(n *entity, b string) {
	spec, ok := data.ArgField(b, "NpcWarp")
	if !ok {
		return
	}
	parse := func(s string) (f, x, y int, ok bool) {
		v := strings.Split(s, ".")
		if len(v) < 3 {
			return 0, 0, 0, false
		}
		return data.Atoi(v[0]), data.Atoi(v[1]), data.Atoi(v[2]), true
	}
	pts := strings.Split(spec, ",")
	start := 0
	for i, s := range pts {
		if f, x, y, ok := parse(s); ok && f == n.floor && x == n.x && y == n.y {
			start = i + 1
			break
		}
	}
	if start >= len(pts) {
		start = 0
	}
	for _, s := range pts[start:] {
		if f, x, y, ok := parse(s); ok {
			w.exMoveNpc(n, f, x, y)
			return
		}
	}
}

// exMoveNpc 是 CHAR_warpToSpecificPoint 对 NPC：坐标不在地图内就不动；否则从原位置消失、在新位置出现（附近的玩家各自更新）。
func (w *World) exMoveNpc(n *entity, floor, x, y int) {
	f := w.game.Floors[floor]
	if f == nil || x < 0 || y < 0 || x >= f.W || y >= f.H {
		return
	}
	w.removeFromFloor(n)
	n.floor, n.x, n.y = floor, x, y
	w.addToFloor(n)
	w.announce(n)
}
