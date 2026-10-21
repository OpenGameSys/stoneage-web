package world

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"sa/server/internal/data"
	"sa/server/internal/proto"
	"sa/server/internal/store"
)

// 传言板（npc_dengon.c）、家族银行（npc_bankman.c）、美容师（npc_charm.c）、占卜师（npc_luckyman.c）、猜拳（npc_janken.c）。

const (
	seqDengon      = 50  // CHAR_WINDOWTYPE_DENGON
	seqJankenStart = 210 // CHAR_WINDOWTYPE_JANKEN_*
	seqJankenMain  = 211
	seqJankenEnd   = 212
	seqLuckyMan    = 261 // CHAR_WINDOWTYPE_WINDOWPETSHOP_START（原版占卜师借用这个编号）
	seqCharmStart  = 282 // CHAR_WINDOWTYPE_CHARM_*
	seqCharmEnd    = 283
	seqNone        = -1 // 原版用 -1 发的说明窗口，回传不处理
)

// gbkWidth 是字串在原版（GBK）里的字节宽：全角 2、半角 1。
func gbkWidth(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xff {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// pad 是 sprintf 的 "%16s"（right）/ "%-16s"：按字节宽补空白。
func pad(s string, w int, left bool) string {
	sp := strings.Repeat(" ", max(0, w-gbkWidth(s)))
	if left {
		return s + sp
	}
	return sp + s
}

// ---------------- 传言板 ----------------

const dengonPerPage = 7 // MESSAGEINONEWINDOW

// dengonBoard 是原版的档名 <楼层>_<x>_<y>（这里用 NPC 设定的位置，不受随机落点影响）。
func dengonBoard(n *entity) string { return fmt.Sprintf("%d_%d_%d", n.npc.Floor, n.npc.X1, n.npc.Y1) }

func (w *World) dbCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*time.Second)
}

// dengonText 是 NPC_DengonReadString：标题一行，接着第 id−6..id 则留言（每则是"内容\n日期 名字"，空的那则是空行）。
// 只留最新的 DengonKeep 则。
func (w *World) dengonText(n *entity, id, maxID int) string {
	id = min(id, maxID)
	from := max(1, id-dengonPerPage+1, maxID-store.DengonKeep+1)
	ctx, cancel := w.dbCtx()
	defer cancel()
	posts, err := w.db.DengonPosts(ctx, dengonBoard(n), from, from+dengonPerPage-1)
	if err != nil {
		w.log.Error("读取传言板失败", "board", dengonBoard(n), "err", err)
	}
	var sb strings.Builder
	sb.WriteString("                         传言板\n")
	for i := range dengonPerPage {
		sb.WriteString(posts[from+i])
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (w *World) dengonMax(n *entity) int {
	ctx, cancel := w.dbCtx()
	defer cancel()
	id, err := w.db.DengonMaxID(ctx, dengonBoard(n))
	if err != nil {
		w.log.Error("读取传言板失败", "board", dengonBoard(n), "err", err)
	}
	return id
}

// dengonOpen 是 NPC_DengonLooked：站在旁边，显示最新的一页，可以留言（宽窗口加输入行）。
func (w *World) dengonOpen(p, n *entity) {
	maxID := w.dengonMax(n)
	w.sendWindow(p, n, "wideinput", BtnOK|BtnCancel|BtnPrev, seqDengon, w.dengonText(n, maxID, maxID))
	p.window.event = maxID // CHAR_WORKSHOPRELEVANT：目前看到第几则
}

// dengonReply 是 NPC_DengonWindowTalked：上一页 / 下一页翻 7 则，确定把输入的文字留在板上（加上日期与名字）。
func (w *World) dengonReply(p, n *entity, win *npcWindow, button int, text string) {
	if max(abs(n.x-p.x), abs(n.y-p.y)) > 3 || win.seq != seqDengon {
		return
	}
	maxID := w.dengonMax(n)
	switch button {
	case BtnNext, BtnPrev:
		id := win.event + dengonPerPage
		if button == BtnPrev {
			id = win.event - dengonPerPage
		}
		id = min(id, maxID)
		if id < 2 {
			id = 1
		}
		btn := BtnOK | BtnCancel
		switch id {
		case maxID:
			btn |= BtnPrev
		case 1:
			btn |= BtnNext
		default:
			btn |= BtnPrev | BtnNext
		}
		w.sendWindow(p, n, "wideinput", btn, seqDengon, w.dengonText(n, id, maxID))
		p.window.event = id
	case BtnOK:
		text = strings.TrimSpace(strings.NewReplacer("\n", " ", "\r", " ").Replace(text))
		if text == "" || !utf8.ValidString(text) {
			return
		}
		for gbkWidth(text) > 58 { // 输入行的长度（msgWNLen）
			_, size := utf8.DecodeLastRuneInString(text)
			text = text[:len(text)-size]
		}
		t := w.now()
		post := fmt.Sprintf("%s\n                             %2d/%02d %2d:%02d %s", text, int(t.Month()), t.Day(), t.Hour(), t.Minute(), p.char.Name)
		ctx, cancel := w.dbCtx()
		id, err := w.db.AddDengon(ctx, dengonBoard(n), post)
		cancel()
		if err != nil {
			w.log.Error("写入传言板失败", "board", dengonBoard(n), "err", err)
			return
		}
		w.sendWindow(p, n, "wideinput", BtnOK|BtnCancel|BtnPrev, seqDengon, w.dengonText(n, id, id))
		p.window.event = id
	}
}

// ---------------- 家族银行 ----------------

// bankmanReply 是 NPC_BankmanWindowTalked：和万事通一样按设定档翻窗口；去 5 是家族银行个人帐户（FM "B|G|存款"，没有家族但还有存款的
// 也能领）、6 是家族共同帐户（"B|T|家族的钱"）、7 是退出家族，都要有家族。
func (w *World) bankmanReply(p, n *entity, seq, button int, sel string) {
	if max(abs(n.x-p.x), abs(n.y-p.y)) > 1 {
		return
	}
	def := n.npc.Windows[seq-100]
	if def == nil {
		return
	}
	proc := -1
	if def.Type == data.WinTypeSelect {
		proc = data.Atoi(sel) + data.WinProcSelect - 1
	} else {
		for i, b := range []int{BtnOK, BtnCancel, BtnYes, BtnNo, BtnPrev, BtnNext} {
			if button&b != 0 {
				proc = i
				break
			}
		}
	}
	if proc <= 0 || proc >= data.WinProcCount { // 原版 button > 0 才处理（OK 什么都不做）
		return
	}
	next := -1
	if b := def.Procs[proc]; b != nil {
		next = b.Goto
	}
	if button == BtnYes {
		next = 5
	}
	switch next {
	case 5, 6, 7:
		defer w.famLock()()
		fm, _, _ := w.fmOf(p)
		switch {
		case fm == nil && (next != 5 || p.fmc.Bank < 1):
			w.fmMessage(p, "\n您的资格不符合！\n请先加入家族！")
		case next == 5:
			p.client.Send(&proto.FamilyMsg{Text: fmt.Sprintf("B|G|%d", p.fmc.Bank)})
		case next == 6:
			p.client.Send(&proto.FamilyMsg{Text: fmt.Sprintf("B|T|%d", fm.Gold)})
		default:
			w.fmLeave(p)
		}
	default:
		w.windowmanOpen(p, n, next)
	}
}

// ---------------- 美容师 ----------------

// charmCost 是 NPC_CharmCost：等级 × 10 × (魅力 ÷ 3) × (转生 + 1)，魅力满 100 时 −1（不必再做）。
func charmCost(c *store.Character) int {
	if c.Charm >= 100 {
		return -1
	}
	charm := c.Charm
	if charm <= 1 {
		charm = 3
	}
	return c.Level * 10 * (charm / 3) * (c.Trans + 1)
}

const charmTitle = "　　　　         美容师      "

func (w *World) charmOpen(p, n *entity) {
	if !faceToFace(n, p, 2) && !inFront(p, n, 1) {
		return
	}
	w.sendWindow(p, n, "select", 0, seqCharmStart, "4\n 　　　       美容师     \n\n 　欢迎光临，今天要做什麽呢？\n \n\n　　　　《　给我魅力　》 \n\n　　　　《 什麽也不做 》 ")
}

// charmReply 是 NPC_CharmWindowTalked：选"给我魅力"报价（是 / 否），按"是"付钱，魅力 +5（最多 100）。
func (w *World) charmReply(p, n *entity, seq, button int, sel string) {
	if max(abs(n.x-p.x), abs(n.y-p.y)) > 2 {
		return
	}
	c := &p.char
	switch {
	case seq == seqCharmStart && data.Atoi(sel) == 2:
		if cost := charmCost(c); cost < 0 {
			w.sendWindow(p, n, "message", BtnOK, seqCharmEnd, charmTitle+"\n\n　　　　你的魅力真是完美\n\n　　　请永远保持你的魅力唷。")
		} else {
			w.sendWindow(p, n, "message", BtnYesNo, seqCharmEnd, fmt.Sprintf(charmTitle+
				"\n　现在,千禧特惠价实施中??\n\n 要将你的魅力上升五点的话\n　　　　需要%6d的stone唷！\n\n　　　即使这样也可以吗？", cost))
		}
	case seq == seqCharmEnd && button == BtnYes:
		cost := charmCost(c)
		if cost < 0 {
			return // 原版魅力满了还按"是"会因价钱 −1 反而加钱，这里不处理
		}
		if cost > c.Gold {
			w.sendWindow(p, n, "message", BtnOK, seqCharmEnd, charmTitle+"\n\n　　 哎呀!你的钱不够唷！\n　　    先去存好钱下次再来吧！")
			return
		}
		c.Gold -= cost
		c.Charm = min(100, c.Charm+5) // CHARMHEAL
		w.itemsChanged(p)             // 金钱、状态、存档
		w.sendWindow(p, n, "message", BtnOK, seqCharmEnd, charmTitle+"\n\n 　嗯,这样你的魅力就上升罗！\n\n"+charmLine(c.Sprite))
	}
}

// charmLine：按形象种类（char_data.c 的 CHAR_IMAGETYPE_*）说不同的话。玩家形象 100000 + 20k + 5j：
// k = 0 小男孩、1..3 少年、4..5 男性、6 小女孩、7..9 少女、10..11 女性。
func charmLine(sprite int) string {
	switch k := (sprite - 100000) / 20; {
	case k == 0 || k == 6:
		return "　　  真的变得好可爱唷！"
	case k >= 1 && k <= 3:
		return "　　是不是比刚才要帅多了呢？"
	case k == 4 || k == 5:
		return " 　　　变得更酷了呢！"
	case k >= 7 && k <= 9:
		return "　　　愈来愈可爱了呢！"
	case k == 10 || k == 11:
		return "    可真是变得愈来愈美了呢！"
	}
	return ""
}

// ---------------- 占卜师 ----------------

// luckyCost 是 NPC_GetMoney："LV*n" 为等级 × n，否则是数字。
func luckyCost(p *entity, arg string) int {
	s, _ := data.ArgField(arg, "Stone")
	if _, n, ok := strings.Cut(s, "*"); ok && strings.Contains(s, "LV") {
		return p.char.Level * data.Atoi(n)
	}
	return data.Atoi(s)
}

// luckyOpen 是 NPC_LuckyManTalked：报价（main_msg 的 %8d 填价钱），是 / 否。
func (w *World) luckyOpen(p, n *entity) {
	if !faceToFace(n, p, 2) && !inFront(p, n, 1) {
		return
	}
	msg, _ := data.ArgField(n.npc.Arg, "main_msg")
	msg = strings.Replace(msg, "%8d", fmt.Sprintf("%8d", luckyCost(p, n.npc.Arg)), 1)
	w.sendWindow(p, n, "message", BtnYesNo, seqLuckyMan, "　　　　　　　　☆占卜师☆　\n\n"+npcText(msg))
}

// luckyReply 是 NPC_LuckyManWindowTalked + NPC_LuckyDisp：付钱后按自己的运气（1..5）从 luckN 里随机挑一句。
func (w *World) luckyReply(p, n *entity, button int) {
	if button != BtnYes || max(abs(n.x-p.x), abs(n.y-p.y)) > 2 {
		return
	}
	arg := n.npc.Arg
	cost := luckyCost(p, arg)
	if p.char.Gold < cost {
		msg, _ := data.ArgField(arg, "NoMoney")
		w.sendWindow(p, n, "message", BtnOK, seqLuckyMan, "　　　　　　　　☆占卜师☆　\n\n"+npcText(msg))
		return
	}
	p.char.Gold -= cost
	w.sendInventory(p)
	w.saveProgress(p)
	list, _ := data.ArgField(arg, fmt.Sprintf("luck%d", p.char.Luck))
	lines := strings.Split(list, ",")
	w.sendWindow(p, n, "message", BtnOK, seqLuckyMan, "　　　　　　　☆今日的运势☆　\n\n"+npcText(lines[w.rnd.IntN(len(lines))]))
}

// ---------------- 猜拳 ----------------

const jankenChoices = "\n\n\n               《  石头  》\n\n               《  剪刀  》\n\n               《  布  》"

func (w *World) jankenOpen(p, n *entity) {
	if max(abs(n.x-p.x), abs(n.y-p.y)) > 1 {
		return
	}
	msg, ok := data.ArgField(n.npc.Arg, "MainMsg")
	if !ok {
		return
	}
	w.sendWindow(p, n, "message", BtnYesNo, seqJankenStart, npcText(msg))
}

// jankenReply 是 NPC_JankenWindowTalked / NPC_JnakenJudge：按"是"先收参加券（EntryItem），然后剪刀石头布；
// 选单第 3 / 5 / 7 行是石头 / 剪刀 / 布。赢了给 WinItem、传到 WinWarp，输了给 LoseItem、传到 LoseWarp，平手再来。
func (w *World) jankenReply(p, n *entity, seq, button int, sel string) {
	if max(abs(n.x-p.x), abs(n.y-p.y)) > 1 {
		return
	}
	arg := n.npc.Arg
	switch seq {
	case seqJankenStart:
		if button != BtnYes {
			return
		}
		if spec, ok := data.ArgField(arg, "EntryItem"); ok {
			// 原版没券时先发 NoItem 又接着开始猜拳（后面的窗口盖掉前面的），这里没券就停在 NoItem
			if !w.takeItems(p, parseNeeds(spec, ",", false), false) {
				msg, _ := data.ArgField(arg, "NoItem")
				w.sendWindow(p, n, "message", BtnOK, seqJankenEnd, npcText(msg))
				return
			}
		}
		w.sendWindow(p, n, "select", 0, seqJankenMain, "              剪刀石头布！！"+jankenChoices)
	case seqJankenMain:
		player := map[int]int{3: 0, 5: 1, 7: 2}[data.Atoi(sel)]
		if s := data.Atoi(sel); s != 3 && s != 5 && s != 7 {
			return
		}
		npc := w.rnd.IntN(3)
		// 0 石头、1 剪刀、2 布：石头赢剪刀、剪刀赢布、布赢石头
		result := 0
		switch {
		case (player+1)%3 == npc:
			result = 1
		case (npc+1)%3 == player:
			result = 2
		}
		if result == 0 {
			w.sendWindow(p, n, "select", 0, seqJankenMain, "              平手再来！！"+jankenChoices)
			return
		}
		hands := []string{" 石头 ", " 剪刀 ", "  布  "}
		itemKey, warpKey, word := "WinItem", "WinWarp", "胜利"
		if result == 2 {
			itemKey, warpKey, word = "LoseItem", "LoseWarp", "输了"
		}
		if spec, ok := data.ArgField(arg, itemKey); ok { // NPC_EventAddItem
			for _, nd := range parseNeeds(spec, ",", false) {
				if def := w.game.Items[nd.ID]; def != nil {
					for range max(1, nd.N) {
						w.addItem(p.inv, w.newItem(def))
					}
				}
			}
			w.itemsChanged(p)
		}
		text := fmt.Sprintf("              ～　结  果　～      \n\n  %s      %s\n        [%s]    VS    [%s]\n\n\n      %s %s",
			pad(n.name, 16, false), pad(p.char.Name, 16, true), hands[npc], hands[player], pad(p.char.Name, 16, true), word)
		if spot, ok := data.ArgField(arg, warpKey); ok {
			v := strings.Split(spot, ",")
			for len(v) < 3 {
				v = append(v, "")
			}
			w.warpTo(p, data.Atoi(v[0]), data.Atoi(v[1]), data.Atoi(v[2]))
		}
		// NPC_JnakenJudge：传送之后，赢了高兴、输了悲伤（自己也看到）
		if result == 2 {
			w.doAction(p, actSad, true)
		} else {
			w.doAction(p, actHappy, true)
		}
		w.sendWindow(p, n, "message", BtnOK, seqJankenEnd, text)
	}
}
