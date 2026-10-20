package world

import (
	"fmt"
	"strings"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 传送员（npc_warpman.c）、万事通（npc_windowman.c）、记录点（npc_savepoint.c）、按时间出现的人（npc_timeman.c）。

// 窗口编号（CHAR_WINDOWTYPE_*；万事通是设定档的 winno + 100）
const (
	seqNewWarpMan = 1   // _NEW_WARPMAN 的 NEW_WARPMAN
	seqSavePoint  = 230 // CHAR_WINDOWTYPE_WINDOWSAVEPOINT_START
	seqWarpMan    = 271 // CHAR_WINDOWTYPE_WINDOWWARPMAN_MAIN
	seqWarpManErr = 272 // CHAR_WINDOWTYPE_WINDOWWARPMAN_ERR
)

// npcSay 是 CHAR_talkToCli(talker, npc, ...)：NPC 对玩家说一句（聊天栏 + 头顶气泡）。
func (w *World) npcSay(p, n *entity, text string) {
	p.client.Send(&proto.NpcTalk{ID: n.id, Name: n.name, Text: text})
}

// npcText 把参数里写成 "\n" 两个字的换行换成真的换行（原客户端收到 WN 文字后做 makeStringFromEscaped）。
func npcText(s string) string { return strings.ReplaceAll(s, `\n`, "\n") }

// inFront 是 NPC_Util_charIsInFrontOfChar：b 在 a 面向的方向上 1..dist 格之内。
func inFront(a, b *entity, dist int) bool {
	if a.floor != b.floor || (a.x == b.x && a.y == b.y) {
		return false
	}
	m := MOVE[a.dir]
	for i := 1; i <= dist; i++ {
		if a.x+m[0]*i == b.x && a.y+m[1]*i == b.y {
			return true
		}
	}
	return false
}

// faceToFace 是 NPC_Util_isFaceToFace（这台服务端 _ATTESTAION_ID == 1 的版本）：i ≤ dist 格内，b 在 a 面前 i 格，
// 或 a 在 b 面前 i 格，有一边成立就算（隔着柜台时，玩家面向 NPC 就行）。另一个版本要两边都面对面。
func faceToFace(a, b *entity, dist int) bool {
	if a.floor != b.floor || (a.x == b.x && a.y == b.y) {
		return false
	}
	ma, mb := MOVE[a.dir], MOVE[b.dir]
	for i := 1; i <= dist; i++ {
		if a.x+ma[0]*i == b.x && a.y+ma[1]*i == b.y || b.x+mb[0]*i == a.x && b.y+mb[1]*i == a.y {
			return true
		}
	}
	return false
}

// countOnFloor 是 NPC_FloorUse：某楼层上的玩家人数。
func (w *World) countOnFloor(floor int) int {
	n := 0
	for _, e := range w.floors[floor] {
		if e.client != nil {
			n++
		}
	}
	return n
}

// ---------------- 传送员 ----------------

// warpmanTalk 是 NPC_WarpManTalked。要和 NPC 面对面（2 格内）或站在它旁边面向它。
// talk 是玩家对着 NPC 说的话（点选 NPC 时为空），参数里有 warp_msg（暗语）的传送员只认暗语。
func (w *World) warpmanTalk(p, n *entity, talk string) {
	if !faceToFace(n, p, 2) && !inFront(p, n, 1) {
		return
	}
	arg := n.npc.Arg
	if strings.Contains(arg, "NEWWARPMAN") {
		w.newWarpmanOpen(p, n, talk)
		return
	}
	// 还没有组队，NPC_PARTY_CHAECK 一定通过
	if rt := w.checkWarpMsg(p, n, arg, talk); rt != 0 {
		if rt == 1 {
			if msg, ok := data.ArgField(arg, "nomal_msg"); ok {
				w.npcSay(p, n, msg)
			}
		}
		return
	}
	w.warpmanOpen(p, n)
}

// checkWarpMsg 是 CheckWarpMsg：0 = 没有暗语，1 = 说的不是暗语，2/3 = 条件不符或交道具失败，4 = 已传送。
func (w *World) checkWarpMsg(p, n *entity, arg, talk string) int {
	word, ok := data.ArgField(arg, "warp_msg")
	if !ok {
		return 0
	}
	if word != talk {
		return 1
	}
	free, ok := data.ArgField(arg, "FREE")
	if !ok {
		return 2
	}
	if !w.passCheck(p, free) {
		if msg, ok := data.ArgField(arg, "NomalMsg"); ok {
			w.npcSay(p, n, msg)
		}
		return 2
	}
	if !w.runEventAction(p, arg) {
		return 3
	}
	if msg, ok := data.ArgField(arg, "FreeMsg"); ok {
		w.npcSay(p, n, msg)
	}
	w.warpByArg(p, arg)
	return 4
}

// warpmanOpen 是 NPC_WarpMan_selectWindow：符合 FREE 条件时显示 FreeMsg（免费），否则显示 PayMsg（MONEY 是价钱，
// "LV*n" 按等级算，带 "-" 表示不能付钱通过，只给看），都没有时显示 NomalMsg。%4d 填目标楼层的人数，%8d 填价钱。
func (w *World) warpmanOpen(p, n *entity) {
	arg := n.npc.Arg
	people := 0
	if strings.Contains(arg, "%4d") {
		warp, _ := data.ArgField(arg, "WARP")
		target := data.Atoi(strings.Split(warp, ",")[0])
		people = w.countOnFloor(target)
		if fl, ok := data.ArgField(arg, "Floor"); ok { // NPC_FloorUseOtherFloor：另外几层也算进去
			for _, f := range strings.Split(fl, ",") {
				if f := data.Atoi(f); f != target {
					people += w.countOnFloor(f)
				}
			}
		}
	}
	fill := func(s string, money int) string {
		s = strings.Replace(s, "%4d", fmt.Sprintf("%4d", people), 1)
		return npcText(strings.Replace(s, "%8d", fmt.Sprintf("%8d", money), 1))
	}
	free, _ := data.ArgField(arg, "FREE")
	cost := 0
	var text string
	if w.passCheck(p, free) || strings.Contains(free, "ALLFREE") {
		msg, ok := data.ArgField(arg, "FreeMsg")
		if !ok {
			return
		}
		text, cost = fill(msg, 0), -1
	} else if pay, ok := data.ArgField(arg, "PayMsg"); ok {
		money := 100
		price, _ := data.ArgField(arg, "MONEY")
		if strings.Contains(pay, "%8d") {
			if _, k, lv := strings.Cut(price, "*"); lv && strings.Contains(price, "LV") {
				money = p.char.Level * data.Atoi(k)
			} else {
				money = data.Atoi(price)
			}
		} else if strings.Contains(price, "-") {
			// 只有说明、不能付钱通过（原版发 OK 窗口后结束）
			w.sendWindow(p, n, "message", BtnOK, seqWarpMan, fill(pay, 0))
			return
		} else if !strings.Contains(pay, "%4d") {
			money = data.Atoi(price) // 原版只有不带 %4d 的说明才读 MONEY，带 %4d 的维持预设的 100
		}
		text = fill(pay, money)
		if p.floor == 117 { // 原版特例：117 号地图收身上金钱的九成，身无分文就送回 117 (225,13)
			money = p.char.Gold
			if money >= 10 {
				money = money * 9 / 10
			} else if money == 0 {
				w.warpTo(p, 117, 225, 13)
				return
			}
		}
		cost = money
	} else if msg, ok := data.ArgField(arg, "NomalMsg"); ok {
		w.sendWindow(p, n, "message", BtnOK, seqWarpMan, npcText(msg))
		return
	} else {
		return
	}
	w.sendWindow(p, n, "message", BtnYesNo, seqWarpMan, text)
	p.window.cost = cost
}

// newWarpmanOpen 是 _NEW_WARPMAN 的 NPC_NewWarpMan_selectWindow：参数用 "OVER" 分成几段，
// 依序找第一个 TALKEVENTn 段里 FREE 条件成立的，显示它的 FreeMsg；都不成立时第一段（NOFREE）的 NomalMsg 用说的。
func (w *World) newWarpmanOpen(p, n *entity, talk string) {
	blocks := strings.Split(n.npc.Arg, "OVER")
	for i, b := range blocks {
		if !strings.Contains(b, "TALKEVENT") {
			continue
		}
		if rt := w.checkWarpMsg(p, n, b, talk); rt > 1 {
			return
		} else if rt != 0 {
			continue
		}
		free, ok := data.ArgField(b, "FREE")
		if !ok {
			return
		}
		if w.passCheck(p, free) || strings.Contains(free, "ALLFREE") {
			msg, ok := data.ArgField(b, "FreeMsg")
			if !ok {
				return
			}
			w.sendWindow(p, n, "message", BtnYesNo, seqNewWarpMan, npcText(msg))
			p.window.cost, p.window.event = -1, i // 原版记在 CHAR_WORKSHOPRELEVANT（段号 - 1）
			return
		}
	}
	if len(blocks) == 0 || !strings.Contains(blocks[0], "NOFREE") {
		return
	}
	if msg, ok := data.ArgField(blocks[0], "NomalMsg"); ok {
		w.npcSay(p, n, msg)
	}
}

// warpmanReply 是 NPC_WarpManWindowTalked：按"是"之后执行参数里的动作（DelItem 交出道具），收钱（不够时显示 MoneyMsg），然后传送。
func (w *World) warpmanReply(p, n *entity, win *npcWindow, button int) {
	if max(abs(n.x-p.x), abs(n.y-p.y)) > 2 || win.seq == seqWarpManErr {
		return
	}
	arg := n.npc.Arg
	if win.seq == seqNewWarpMan {
		found := false
		for _, b := range strings.Split(arg, "OVER") {
			if strings.Contains(b, fmt.Sprintf("TALKEVENT%d", win.event)) {
				arg, found = b, true
				break
			}
		}
		if !found {
			return
		}
		if button != BtnYes {
			if msg, ok := data.ArgField(arg, "CancelMsg"); ok {
				w.npcSay(p, n, msg)
			}
			return
		}
	} else if button != BtnYes {
		return // 只有 OK 的说明窗口回传不到这里（NpcWindowReply 只认窗口上有的按钮）
	}
	// 免费通过的要在这时再检查一次条件：打开窗口之后可能已经把门票丢了（服务端权威，原版没有重新检查）。
	// 钱不够要在交出道具之前就挡下（原版先 DelItem 再检查金钱，钱不够时道具已经被收走）。
	if win.cost < 0 {
		if free, _ := data.ArgField(arg, "FREE"); !w.passCheck(p, free) && !strings.Contains(free, "ALLFREE") {
			return
		}
	} else if p.char.Gold < win.cost {
		msg, ok := data.ArgField(arg, "MoneyMsg")
		if !ok || msg == "" {
			msg = "\n\n　　　　似乎金钱不足唷。　　\n\n　请存好钱後，再过来。"
		}
		w.sendWindow(p, n, "message", BtnOK, seqWarpManErr, npcText(msg))
		return
	}
	if !w.runEventAction(p, arg) {
		return
	}
	if win.cost > 0 {
		p.char.Gold -= win.cost // CHAR_DelGold
		w.sendInventory(p)
		w.saveProgress(p)
	}
	w.warpByArg(p, arg)
}

// runEventAction 是 Action_RunDoEventAction 里有资料用到的几项，照原版顺序：AddGold（加钱，最多到金钱上限）、
// DelGold（扣钱，不够说"你身上的钱不够喔！"）、DelItem（交出道具）、AddItem（拿到道具，逗号分开，空格不够说"物品栏空间不足！！"）。
// 原版一项一项做，后面的失败时前面的已经做了（例如钱扣了、道具不够）；这里先全部检查，都可以才一起做。
func (w *World) runEventAction(p *entity, arg string) bool {
	c := &p.char
	delGold, hasDel := data.ArgField(arg, "DelGold")
	if hasDel && c.Gold < data.Atoi(delGold) {
		w.sysMsg(p, "你身上的钱不够喔！", colorYellow)
		return false
	}
	delSpec, hasDelItem := data.ArgField(arg, "DelItem")
	if hasDelItem && !w.haveItems(p, parseNeeds(delSpec, ",", true)) {
		w.sysMsg(p, "条件不足", colorYellow)
		return false
	}
	var adds []*data.ItemDef
	if spec, ok := data.ArgField(arg, "AddItem"); ok {
		for _, s := range strings.Split(spec, ",") {
			if def := w.game.Items[data.Atoi(s)]; def != nil {
				adds = append(adds, def)
			}
		}
		if len(adds) > w.freeBagSlots(p) {
			w.sysMsg(p, "物品栏空间不足！！", colorYellow)
			return false
		}
	}
	addGold, hasAdd := data.ArgField(arg, "AddGold")
	if !hasAdd && !hasDel && !hasDelItem && len(adds) == 0 {
		return true // 没有任何动作
	}
	if hasAdd {
		c.Gold = min(maxGold(c.Trans), c.Gold+data.Atoi(addGold))
	}
	if hasDel {
		c.Gold -= data.Atoi(delGold)
		w.sysMsg(p, fmt.Sprintf("交出%d石币", data.Atoi(delGold)), colorYellow)
	}
	if hasDelItem {
		w.delItems(p, delSpec)
	}
	for _, def := range adds {
		w.addItem(p.inv, w.newItem(def))
		w.sysMsg(p, "拿到"+def.Name+"。", colorYellow)
	}
	w.itemsChanged(p) // 金钱、道具、存档
	return true
}

// warpByArg 是 NPC_WarpMsg：WARP 可以写几个以 ";" 分开的 "楼层,x,y"，随机挑一个（GetRand_WarpPoint）；0,0,0 表示不传送。
func (w *World) warpByArg(p *entity, arg string) {
	spec, ok := data.ArgField(arg, "WARP")
	if !ok {
		return
	}
	var pts [][3]int
	for _, s := range strings.Split(spec, ";") {
		if len(pts) == 20 {
			break
		}
		v := strings.Split(s, ",")
		for len(v) < 3 {
			v = append(v, "")
		}
		pts = append(pts, [3]int{data.Atoi(v[0]), data.Atoi(v[1]), data.Atoi(v[2])})
	}
	// RAND(0, i) 含 i：抽到最后一个之后（空的）或楼层不对时用第一个
	i := w.rnd.IntN(len(pts) + 1)
	if i >= len(pts) || pts[i][0] <= 0 {
		i = 0
	}
	pt := pts[i]
	if pt == [3]int{} {
		return
	}
	w.warpTo(p, pt[0], pt[1], pt[2])
}

// warpTo 是 CHAR_warpToSpecificPoint：坐标不在地图内（MAP_IsValidCoordinate）就不动。
func (w *World) warpTo(p *entity, floor, x, y int) {
	f := w.game.Floors[floor]
	if f == nil || x < 0 || y < 0 || x >= f.W || y >= f.H {
		return
	}
	w.placeOnFloor(p, floor, x, y, p.dir)
	w.save(p)
}

// ---------------- 万事通 ----------------

// windowmanOpen 是 NPC_Windowman_selectWindow：要站在 NPC 旁边面向它。
func (w *World) windowmanOpen(p, n *entity, no int) {
	def := n.npc.Windows[no]
	if def == nil {
		return
	}
	kind := "message"
	if def.Type == data.WinTypeSelect {
		kind = "select"
	}
	w.sendWindow(p, n, kind, def.Buttons, def.No+100, def.Message)
}

// windowmanReply 是 NPC_WindowmanWindowTalked：选单按回传的行号（第 n 行是第 n 个 selected），其余按按钮找到按钮处理，
// 检查持有/不持有道具后打开下一个窗口。
func (w *World) windowmanReply(p, n *entity, seq, button int, sel string) {
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
	if proc < 0 || proc >= data.WinProcCount || def.Procs[proc] == nil {
		return
	}
	b := def.Procs[proc]
	next := -1
	if b.HaveItem != -1 {
		if !w.hasItem(p, b.HaveItem) {
			return
		}
		next = b.HaveItemGoto
	}
	if b.NoItem != -1 {
		if w.hasItem(p, b.NoItem) {
			return
		}
		next = b.NoItemGoto
	}
	if next == -1 {
		next = b.Goto
	}
	w.windowmanOpen(p, n, next)
}

// ---------------- 记录点 ----------------

// elderPos 是 CHAR_getElderPosition：记录点的位置；没记录过或编号不对时用出生村落。
func (w *World) elderPos(e *entity) data.Hometown {
	if h, ok := w.game.Elders[e.char.Elder]; ok && e.char.Elder >= 0 {
		return h
	}
	if e.char.Hometown >= 0 && e.char.Hometown < len(data.Hometowns) {
		return data.Hometowns[e.char.Hometown]
	}
	return data.Hometowns[0]
}

// savePointFlag 是 NPC_CheckFlg(CHAR_SAVEPOINT, id)；出生村落的那一位建角时就设了（CHAR_getInitElderPosition）。
func savePointFlag(e *entity, id int) bool {
	return id == e.char.Hometown || (id >= 0 && id < 64 && e.char.SavePoints&(1<<id) != 0)
}

// savePointArg 取记录点的 ID；没有 ID 的记录点原版初始化失败。
func savePointArg(n *entity) (int, bool) {
	id, ok := data.ArgField(n.npc.Arg, "ID")
	return data.Atoi(id), ok
}

// savePointTalk 是 NPC_SavePointTalked：要在 NPC 前 2 格内面向它。第一次要交出 GetItem 的道具才登记（RequestMsg / RealyMsg 窗口），
// 登记过的直接把记录点设在这里，并说 NomalMsg 加上开机时间与在线人数。
func (w *World) savePointTalk(p, n *entity) {
	if !inFront(p, n, 2) && (p.x != n.x || p.y != n.y) {
		return
	}
	id, ok := savePointArg(n)
	if !ok {
		return
	}
	arg := n.npc.Arg
	c := &p.char
	if strings.Contains(arg, "NOITEM") && id >= 0 && id < 64 {
		c.SavePoints |= 1 << id
	}
	if !savePointFlag(p, id) {
		if _, ok := w.savePointItems(p, arg); ok {
			msg, _ := data.ArgField(arg, "RealyMsg")
			w.sendWindow(p, n, "message", BtnYesNo, seqSavePoint, npcText(msg))
		} else {
			msg, _ := data.ArgField(arg, "RequestMsg")
			w.sendWindow(p, n, "message", BtnOK, seqSavePoint, npcText(msg))
		}
		return
	}
	c.Elder = id
	w.saveProgress(p)
	msg, _ := data.ArgField(arg, "NomalMsg")
	text := strings.Replace(msg, "%s", c.Name, 1)
	up := int(w.now().Sub(w.started).Seconds())
	dd, hh, mm, ss := up/86400, up%86400/3600, up%3600/60, up%60
	switch {
	case dd > 0:
		text += fmt.Sprintf("本星球已经启动了 %d 日 %d 小时 %d 分 %d 秒。", dd, hh, mm, ss)
	case hh > 0:
		text += fmt.Sprintf("本星球已经启动了 %d 小时 %d 分 %d 秒。", hh, mm, ss)
	default:
		text += fmt.Sprintf("本星球已经启动了 %d 分 %d 秒。", mm, ss)
	}
	text += fmt.Sprintf("目前线上人数为 %d 人。", len(w.players))
	w.npcSay(p, n, text)
}

// savePointItems 是 NPC_UsedCheck：GetItem 以 "," 分成几种交法（组内以 "&" 连接），找第一种身上都有的；没写 GetItem 时不用交东西。
func (w *World) savePointItems(p *entity, arg string) ([]itemNeed, bool) {
	spec, ok := data.ArgField(arg, "GetItem")
	if !ok {
		return nil, true
	}
	for _, group := range strings.Split(spec, ",") {
		if needs := parseNeeds(group, "&", false); len(needs) > 0 && w.haveItems(p, needs) {
			return needs, true
		}
	}
	return nil, false
}

// savePointReply 是 NPC_SavePointWindowTalked：在 RealyMsg 窗口按"是"就收走道具、登记记录点并显示 OkMsg。
func (w *World) savePointReply(p, n *entity, seq, button int) {
	if max(abs(n.x-p.x), abs(n.y-p.y)) > 2 || seq != seqSavePoint || button != BtnYes {
		return
	}
	id, ok := savePointArg(n)
	if !ok {
		return
	}
	needs, ok := w.savePointItems(p, n.npc.Arg)
	if !ok || !w.takeItems(p, needs, false) {
		return
	}
	if id >= 0 && id < 64 {
		p.char.SavePoints |= 1 << id
	}
	p.char.Elder = id
	w.saveProgress(p)
	msg, _ := data.ArgField(n.npc.Arg, "OkMsg")
	w.sendWindow(p, n, "message", BtnOK, seqSavePoint, npcText(msg))
}

// ---------------- 按时间出现的人 ----------------

// SA 时间（handletime.c RealTimeToLSTime）：一个 SA 日 = 5400 秒，分 1024 时。
const (
	saEra = 912766409 + 5400
	saDay = 5400
)

func (w *World) saHour() int {
	secs := w.now().Unix() - saEra
	return int(((secs%saDay)+saDay)%saDay) * 1024 / saDay
}

// timeTable 是 npc_timeman.c 的 TimeTble：参数 time 含这些字时，SA 时间在 (born, dead) 之间出现，其余时间换成 change_no 的图（CLS 或没写就是消失）。
var timeTable = []struct {
	name       string
	born, dead int
}{
	{"ALLNIGHT", 301, 700}, {"ALLNOON", 701, 300}, {"AM", 501, 125}, {"PM", 126, 500},
	{"FORE", 701, 125}, {"AFTER", 126, 300}, {"EVNING", 301, 500}, {"MORNING", 501, 700}, {"FREE", 0, 1024},
}

const timeManHidden = 9999

// timeManSprite 是 NPC_TimeManWatch 的判断：现在该用原图（mode 0）还是替代图（mode 1）。
func (w *World) timeManSprite(n *entity, hour int) (sprite, mode int) {
	arg := n.npc.Arg
	alt := timeManHidden
	if s, ok := data.ArgField(arg, "change_no"); ok && !strings.Contains(s, "CLS") {
		alt = data.Atoi(s)
	}
	t, _ := data.ArgField(arg, "time")
	for _, e := range timeTable {
		if !strings.Contains(t, e.name) {
			continue
		}
		on := e.born < hour && hour < e.dead
		if e.born >= e.dead {
			on = e.born < hour && hour < 1024 || 0 < hour && hour < e.dead
		}
		if on {
			return n.npc.Sprite, 0
		}
		return alt, 1
	}
	return n.npc.Sprite, 0
}

// updateTimeMen 按 SA 时间换图：变成 9999 的从视野里消失，回来的重新出现，换图的发 ObjSprite。
func (w *World) updateTimeMen() {
	hour := w.saHour()
	for _, n := range w.timeMen {
		s, _ := w.timeManSprite(n, hour)
		if s == n.sprite {
			continue
		}
		wasHidden := n.hidden()
		n.sprite = s
		for _, o := range w.floors[n.floor] {
			if o.client == nil {
				continue
			}
			switch {
			case n.hidden() && o.visible[n.id]:
				delete(o.visible, n.id)
				o.client.Send(&proto.ObjDisappear{IDs: []int{n.id}})
			case !n.hidden() && wasHidden && near(o, n):
				o.visible[n.id] = true
				o.client.Send(&proto.ObjAppear{Objs: []proto.ObjInfo{n.info()}})
			case !n.hidden() && o.visible[n.id]:
				o.client.Send(&proto.ObjSprite{ID: n.id, Sprite: s})
			}
		}
	}
}

// timeManTalk 是 NPC_TimeManTalked：消失时不说话；出现时说 main_msg、换图时说 change_msg（以 "," 分开的随机一句）。
func (w *World) timeManTalk(p, n *entity) {
	if !faceToFace(n, p, 2) && max(abs(n.x-p.x), abs(n.y-p.y)) > 1 {
		return
	}
	if n.hidden() || !inFront(p, n, 3) {
		return
	}
	key := "main_msg"
	if _, mode := w.timeManSprite(n, w.saHour()); mode == 1 {
		key = "change_msg"
	}
	msg, _ := data.ArgField(n.npc.Arg, key)
	lines := strings.Split(msg, ",")
	w.npcSay(p, n, lines[w.rnd.IntN(len(lines))])
}
