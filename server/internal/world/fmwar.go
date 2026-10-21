package world

import (
	"fmt"
	"strings"
	"time"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 家族对战：庄园争夺战（踢馆）与家族 PK 场（npc_manorsman.c、npc_scheduleman.c、npc_fmwarpman.c、npc_fmpkman.c、npc_fmpkcallman.c，
// 原版的排程与胜负由 AC 服务器（saac acfamily.c ACFixFMPK）串起来；这里全部在 Families 里做，所有线路共用）。
//
//	对战场   1..4 是四个庄园（踢馆登记人 manorsman.arg 的 manorid、家族对战向导的 ID），11..15 是家族 PK 场（场地预约人的 id）。
//	         向导的 WARP1 / WARP2 是主队 / 客队的进场点（它的楼层就是对战场），服务员的 WARP 是离场点。
//	踢馆     踢馆登记人（庄园村的族长家旁）：没有庄园的正式家族的族长，对有主人的庄园下战书，选明天 20 / 21 / 22 点开打
//	         （原版 50V50，挑战期每天 20:00~24:00、挑战资格按气势排名；这里先到先得），准备时间 15 分钟。
//	         打完不论胜负 5 天休战（原版一天休战 + 四天准备）。
//	预约     场地预约人（家族 PK 场，楼层 141）：族长预约之后几个整点的一场，挑对手家族与规则（生存战 / 打飞战），
//	         对方族长到同一个预约人那里接受才算排定（原版 challengetimeout 内没接受就取消，这里到时间没接受就取消）。
//	进场     到了对战时间，两边家族的人到向导那里进场（有组队不行），先进来的人等准备时间过后"开战罗"；
//	         这段时间里两边的人面对面按"决斗"就能开打（不用双方都开决斗开关），倒下或逃跑的人算出局，
//	         由服务员那里的离场点送出去（CheckLeavePK "战败离场"）。族长可以用家族召唤员把在线的族人叫来。
//	判胜     一边的人都出局（或都离开）另一边就赢；撑到整点 59 分（FMWARPMAN_CLEANPLACE）按规则：生存战剩的人多的赢，
//	         打飞战出局的人少的赢，一样是平手。庄园战客队赢就接管庄园（原版 AC 换 fmpoint 的主人）；家族 PK 场赢家从输家拿走
//	         十分之一的家族声望（ACFixFMPK：award = 输家声望 / 10，记在 fmpk）。打完全部送出场。
//	限制     对战在约战的那条线路上打（Families 记着是哪个 World 在管这场），别的线路只看得到排程。

const (
	warNone      = 0
	warChallenge = 1 // 等客队族长同意（家族 PK 场）
	warScheduled = 2
	warPrepare   = 3 // 可以进场
	warFighting  = 4
	warDone      = 5

	manorWarPrepare = 15                 // 庄园战的准备分钟（原版 prepare_time 15）
	pkWarPrepare    = 5                  // 家族 PK 场预设 5
	manorWarPeace   = 5 * 24 * time.Hour // 休战 + 准备期
	pkWarKeep       = time.Hour          // 打完的结果留一小时给人看
	warEndMinute    = 59                 // FMWARPMAN_CLEANPLACE
	warMaxPlayers   = 50

	fmWarpEnterSeq  = 30
	fmPkMainSeq     = 31
	fmPkLeaveSeq    = 32
	callMainSeq     = 33
	callAskSeq      = 34
	callLeaveSeq    = 35
	callComeSeq     = 36 // npc 0 的窗口（被召唤的人可能在别的楼层 / 线路）
	manorAskSeq     = 37
	manorConfirmSeq = 38
	manorTimeSeq    = 39
	schedMainSeq    = 40
	schedFamilySeq  = 41
	schedTimeSeq    = 42
	schedRuleSeq    = 43
	schedAcceptSeq  = 44
)

// fmWar 是一个对战场的排程（原版 FamilyPKSchedule）。
type fmWar struct {
	ID        int    `json:"id"`
	Manor     int    `json:"manor"` // 庄园下标 0..3，家族 PK 场 -1
	Host      int    `json:"host"`  // 家族 ID（庄园战是庄园的主人）
	Guest     int    `json:"guest"`
	HostName  string `json:"hn"`
	GuestName string `json:"gn"`
	At        int64  `json:"at"`   // 开始进场的时间
	Prepare   int    `json:"prep"` // 几分钟后开打
	Win       int    `json:"win"`  // 0 生存战 1 打飞战
	Flag      int    `json:"flag"`
	Count0    [2]int `json:"c0"`     // 开战时双方人数
	Winner    int    `json:"winner"` // 家族 ID，0 平手
	DoneAt    int64  `json:"done"`
	Until     int64  `json:"until"` // 休战到（庄园战）
	last      int    // 上次报过倒数的分钟
}

func (x *fmWar) sides() (int, int) { return x.Host, x.Guest }

func (x *fmWar) names() string { return fmt.Sprintf("『%s ＶＳ %s』", x.GuestName, x.HostName) }

// warPlace 是一个对战场在地图上的位置（从向导与服务员的参数来）。
type warPlace struct {
	floor int
	enter [2][3]int // 主队、客队的进场点
	exit  [3]int    // 离场点（服务员的 WARP）
}

// warOf：对战场 id 的排程（锁里用；没有就建一个空的）。
func (w *World) warOf(id int) *fmWar {
	f := w.fam
	if f.wars == nil {
		f.wars = map[int]*fmWar{}
	}
	x := f.wars[id]
	if x == nil {
		x = &fmWar{ID: id, Manor: -1, Flag: warNone}
		if id >= 1 && id <= len(manorTable) {
			x.Manor = id - 1
		}
		f.wars[id] = x
	}
	return x
}

func (w *World) saveWars() { w.fam.saveKV("fmwars", w.fam.wars) }

// warOfFamily：fm 参加的（还没打完的）那场。
func (w *World) warOfFamily(fm *family) *fmWar {
	for _, x := range w.fam.wars {
		if (x.Host == fm.ID || x.Guest == fm.ID) && x.Flag >= warChallenge && x.Flag <= warFighting {
			return x
		}
	}
	return nil
}

// warPlaces 扫一次 NPC：向导（FMWarpMan：ID、WARP1、WARP2）与服务员（FMPKMan：ID、WARP）。
func (w *World) warPlaces() map[int]*warPlace {
	if w.warPlace != nil {
		return w.warPlace
	}
	w.warPlace = map[int]*warPlace{}
	get := func(id int) *warPlace {
		p := w.warPlace[id]
		if p == nil {
			p = &warPlace{}
			w.warPlace[id] = p
		}
		return p
	}
	for _, npcs := range w.game.NPCs {
		for i := range npcs {
			n := &npcs[i]
			id := npcArgNum(n.Arg, "ID")
			switch n.Function {
			case "FMWarpMan":
				p := get(id)
				p.enter[0] = argPoint(n.Arg, "WARP1")
				p.enter[1] = argPoint(n.Arg, "WARP2")
				p.floor = p.enter[0][0]
			case "FMPKMan":
				get(id).exit = argPoint(n.Arg, "WARP")
			}
		}
	}
	return w.warPlace
}

// argPoint 读 "KEY:楼层,x,y"。
func argPoint(arg, key string) [3]int {
	s, _ := data.ArgField(arg, key)
	f := strings.Split(s, ",")
	if len(f) < 3 {
		return [3]int{}
	}
	return [3]int{data.Atoi(f[0]), data.Atoi(f[1]), data.Atoi(f[2])}
}

// warAtFloor：这层楼是哪个对战场（没有为 nil）。
func (w *World) warAtFloor(floor int) (*fmWar, *warPlace) {
	for id, p := range w.warPlaces() {
		if p.floor == floor && floor != 0 {
			if x := w.fam.wars[id]; x != nil {
				return x, p
			}
		}
	}
	return nil, nil
}

// warCount：对战场上两边家族各几个人（这条线路）。
func (w *World) warCount(x *fmWar, p *warPlace) (int, int) {
	a, b := 0, 0
	for _, o := range w.floors[p.floor] {
		if o.kind != "player" {
			continue
		}
		switch o.fm.Index {
		case x.Host:
			a++
		case x.Guest:
			b++
		}
	}
	return a, b
}

// sayFamily 对一个家族在线的人说（任何线路）。
func (w *World) sayFamily(fmID int, text string, color int) {
	fm := w.fam.list[fmID]
	if fm == nil {
		return
	}
	for _, m := range fm.Members {
		if m != nil {
			w.fam.tell(m.ID, func(w2 *World, e *entity) { w2.sysMsg(e, text, color) })
		}
	}
}

// shoutAll 对全部在线的人说（CHAR_talkToAll）。
func (w *World) shoutAll(text string, color int) {
	for id := range w.fam.online {
		w.fam.tell(id, func(w2 *World, e *entity) { w2.sysMsg(e, text, color) })
	}
}

func (w *World) warFamilyName(id int) string {
	if fm := w.fam.list[id]; fm != nil {
		return fm.Name
	}
	return "？"
}

// warOpponents：e 与 o 在正在开打的对战场上、分属两边的家族（这时按"决斗"不用对方开决斗开关）。
func (w *World) warOpponents(e, o *entity) bool {
	defer w.famLock()()
	x, p := w.warAtFloor(e.floor)
	if x == nil || x.Flag != warFighting || o.floor != p.floor || e.fm.Index == 0 || o.fm.Index == 0 {
		return false
	}
	return e.fm.Index == x.Host && o.fm.Index == x.Guest || e.fm.Index == x.Guest && o.fm.Index == x.Host
}

// warLose：在对战场上倒下 / 逃跑（battle.c 把 CHAR_WORKFMPKFLAG 设成 -1）：下一次 tick 送出场。
func (w *World) warLose(e *entity) {
	defer w.famLock()()
	if x, _ := w.warAtFloor(e.floor); x != nil && x.Flag == warFighting && (e.fm.Index == x.Host || e.fm.Index == x.Guest) {
		e.warOut = true
	}
}

// ---------------- 每 5 秒：排程推进与判胜（NPC_FMWarpManLoop） ----------------

func (w *World) tickWars() {
	defer w.famLock()()
	now := w.now()
	for id, x := range w.fam.wars {
		place := w.warPlaces()[id]
		if home := w.fam.warHome[id]; home != nil && home != w && x.Flag >= warPrepare && x.Flag <= warFighting {
			continue // 别的线路在管这场
		}
		switch x.Flag {
		case warChallenge:
			if now.Unix() >= x.At { // 到时间还没人接受
				w.sayFamily(x.Host, "对方家族没有接受挑战，预约取消。", colorYellow)
				w.resetWar(x)
			}
		case warScheduled:
			if now.Unix() >= x.At {
				x.Flag, x.last = warPrepare, -1
				w.fam.warHome[id] = w
				w.sayFamily(x.Host, x.names()+" 可以准备进场了！", colorYellow)
				w.sayFamily(x.Guest, x.names()+" 可以准备进场了！", colorYellow)
				w.saveWars()
			}
		case warPrepare:
			left := int(time.Unix(x.At, 0).Add(time.Duration(x.Prepare) * time.Minute).Sub(now).Minutes())
			if now.Unix() >= x.At+int64(x.Prepare)*60 {
				x.Flag, x.last = warFighting, -1
				if place != nil {
					x.Count0[0], x.Count0[1] = w.warCount(x, place)
				}
				msg := x.names() + " 开战罗～！！"
				w.sayFamily(x.Host, msg, colorYellow)
				w.sayFamily(x.Guest, msg, colorRed)
				if x.Manor >= 0 { // _FMPOINT_PK_MESSAGE
					w.shoutAll(fmt.Sprintf("庄园所有权争夺战%s开～战～罗～", x.names()), colorYellow)
				}
				w.saveWars()
			} else if left != x.last && place != nil {
				x.last = left
				w.warTalkFloor(x, place, fmt.Sprintf("距离对战时间还剩下：%4d分钟", left+1))
			}
		case warFighting:
			if place == nil {
				continue
			}
			w.warKickLosers(x, place)
			a, b := w.warCount(x, place)
			winner, over := 0, false
			switch {
			case a == 0 && b == 0:
				over = true
			case a == 0:
				winner, over = x.Guest, true
			case b == 0:
				winner, over = x.Host, true
			case now.Minute() >= warEndMinute && now.Unix() >= x.At+int64(x.Prepare)*60+60:
				over = true
				la, lb := a, b
				if x.Win == 1 { // 打飞战：被打出去的少的赢
					la, lb = x.Count0[1]-b, x.Count0[0]-a
				}
				if la > lb {
					winner = x.Host
				} else if lb > la {
					winner = x.Guest
				}
			}
			if over {
				w.finishWar(x, place, winner)
			}
		case warDone:
			until := x.Until
			if x.Manor < 0 {
				until = x.DoneAt + int64(pkWarKeep/time.Second)
			}
			if now.Unix() >= until {
				w.resetWar(x)
			}
		}
	}
}

func (w *World) resetWar(x *fmWar) {
	id := x.ID
	*x = fmWar{ID: id, Manor: x.Manor, Flag: warNone}
	delete(w.fam.warHome, id)
	w.saveWars()
}

// warTalkFloor：对战场上（与向导所在楼层）两边家族的人（NPC_talkToFloor）。
func (w *World) warTalkFloor(x *fmWar, p *warPlace, text string) {
	for _, o := range w.floors[p.floor] {
		if o.kind == "player" && (o.fm.Index == x.Host || o.fm.Index == x.Guest) {
			w.sysMsg(o, text, colorYellow)
		}
	}
}

// warKickLosers 是 CheckLeavePK：倒下 / 逃跑过的人送出场。
func (w *World) warKickLosers(x *fmWar, p *warPlace) {
	for _, o := range append([]*entity(nil), w.playersOn(p.floor)...) {
		if o.warOut && o.fight == nil {
			o.warOut = false
			w.leaveParty(o, false)
			w.sysMsg(o, "战败离场", colorRed)
			w.warpTo(o, p.exit[0], p.exit[1], p.exit[2])
		}
	}
}

// playersOn：这层楼上的玩家实体。
func (w *World) playersOn(floor int) []*entity {
	var out []*entity
	for _, o := range w.floors[floor] {
		if o.kind == "player" {
			out = append(out, o)
		}
	}
	return out
}

// finishWar：判了胜负（winner 0 平手）：庄园战客队赢就接管庄园；家族 PK 场赢家拿走输家十分之一的声望（ACFixFMPK）；
// 场上的人全部送出去（NPC_BattleOut / NPC_CleanPK）。
func (w *World) finishWar(x *fmWar, p *warPlace, winner int) {
	x.Flag, x.Winner, x.DoneAt = warDone, winner, w.now().Unix()
	if x.Manor >= 0 {
		x.Until = x.DoneAt + int64(manorWarPeace/time.Second)
	}
	result := "平手"
	switch winner {
	case x.Host:
		result = x.HostName + " 获胜"
	case x.Guest:
		result = x.GuestName + " 获胜"
	}
	if winner != 0 {
		win, lose := w.fam.list[winner], w.fam.list[x.Host+x.Guest-winner]
		if win != nil && lose != nil {
			if x.Manor >= 0 {
				if winner == x.Guest {
					w.claimManor(win, x.Manor)
				}
			} else {
				award := lose.total() / 10 // ACFixFMPK：输家声望的十分之一（内部值）
				lose.PK -= award
				win.PK += award
				w.fam.save(win)
				w.fam.save(lose)
				w.resyncFamily(win)
				w.resyncFamily(lose)
			}
		}
	}
	kind := "家族ＰＫ"
	if x.Manor >= 0 {
		kind = "庄园所有权争夺战"
	}
	msg := fmt.Sprintf("%s%s 结束：%s！", kind, x.names(), result)
	w.sayFamily(x.Host, msg, colorYellow)
	w.sayFamily(x.Guest, msg, colorYellow)
	if x.Manor >= 0 {
		w.shoutAll(msg, colorYellow)
	}
	for _, o := range append([]*entity(nil), w.playersOn(p.floor)...) {
		o.warOut = false
		if o.fight == nil {
			w.leaveParty(o, false)
			w.warpTo(o, p.exit[0], p.exit[1], p.exit[2])
		}
	}
	w.saveWars()
}

// ---------------- 踢馆登记人（manorsman：庄园战的约战） ----------------

func (w *World) manorSmanTalk(p, n *entity) {
	if max(abs(n.x-p.x), abs(n.y-p.y)) > 2 || !faceToFace(n, p, 2) {
		return
	}
	defer w.famLock()()
	id := npcArgNum(n.npc.Arg, "manorid")
	if id < 1 || id > len(manorTable) {
		return
	}
	x := w.warOf(id)
	now := w.now()
	switch x.Flag {
	case warNone:
		w.sendWindow(p, n, "message", BtnYes|BtnNo, manorAskSeq,
			"庄园所有权争夺战的挑战资格\n\n一、没有庄园的家族\n二、家族已经正式成立\n三、由族长对有主人的庄园下战书\n四、先到先得，一个庄园一次只受理一场\n\n按 OK 确认你的资格，按 NO 离开")
	case warScheduled, warPrepare:
		w.sendWindow(p, n, "message", BtnOK, 0, fmt.Sprintf("庄园所有权争夺战\n%s\n\n预定将在%s後开始。", x.names(), untilText(time.Unix(x.At, 0).Sub(now))))
	case warFighting:
		w.sendWindow(p, n, "message", BtnOK, 0, fmt.Sprintf("庄园所有权争夺战\n%s\n\n开～打～罗～\n还没进场的人赶快进场吧。", x.names()))
	case warDone:
		w.sendWindow(p, n, "message", BtnOK, 0, fmt.Sprintf("现在非挑战时期，要挑战的话\n请%s後再来申请。", untilText(time.Unix(x.Until, 0).Sub(now))))
	}
}

func untilText(d time.Duration) string {
	if d <= 0 {
		return "片刻"
	}
	dd, hh, mm := int(d.Hours())/24, int(d.Hours())%24, int(d.Minutes())%60
	var sb strings.Builder
	if dd > 0 {
		fmt.Fprintf(&sb, " %d 天", dd)
	}
	if hh > 0 {
		fmt.Fprintf(&sb, " %d 小时", hh)
	}
	if mm > 0 || sb.Len() == 0 {
		fmt.Fprintf(&sb, " %d 分钟", max(1, mm))
	}
	return sb.String()
}

func (w *World) manorSmanReply(p, n *entity, win *npcWindow, button int, sel string) {
	defer w.famLock()()
	id := npcArgNum(n.npc.Arg, "manorid")
	if id < 1 || id > len(manorTable) {
		return
	}
	x := w.warOf(id)
	fm, _, m := w.fmOf(p)
	switch win.seq {
	case manorAskSeq:
		if button&BtnYes == 0 {
			return
		}
		owner := w.fam.list[w.fam.manors[id-1].Owner]
		msg := ""
		switch {
		case fm == nil || m.Duty != fmDutyLeader:
			msg = "只有族长可以下战书喔。"
		case owner == nil:
			msg = "现在并没有庄园家族！\n直接去申请迁入就可以了哟。"
		case owner == fm:
			msg = "这个庄园已经是你的家族的喔。"
		case !fm.Setup:
			msg = "你的家族还没有正式成立喔。"
		case w.fam.manorOf(fm) >= 0:
			msg = "一个家族只能拥有一个庄园，\n如果庄园家族之间需要切磋\n请到家族ＰＫ场。"
		case w.warOfFamily(fm) != nil:
			msg = "你的家族正在挑战其他庄园，\n请把机会留给其他家族吧！"
		case x.Flag != warNone:
			msg = "这个庄园已经有人约战了喔。"
		}
		if msg != "" {
			w.sendWindow(p, n, "message", BtnOK, 0, msg)
			return
		}
		w.sendWindow(p, n, "message", BtnYes|BtnNo, manorConfirmSeq,
			"庄园挑战会有一天的准备期\n你可以随时询问我以得知剩下的时间\n挑战结束後不论胜负将会有五天的休战期\n休战期间不能挑战\n\n你确定要挑战这个庄园吗？")
	case manorConfirmSeq:
		if button&BtnYes == 0 || fm == nil || m.Duty != fmDutyLeader || x.Flag != warNone {
			return
		}
		w.sendWindow(p, n, "select", BtnCancel, manorTimeSeq, "2\n请选择你要下族战的时间\n\n            50V50明天２０：００\n            50V50明天２１：００\n            50V50明天２２：００")
	case manorTimeSeq:
		k := data.Atoi(sel)
		owner := w.fam.list[w.fam.manors[id-1].Owner]
		if button != 0 || k < 1 || k > 3 || fm == nil || m.Duty != fmDutyLeader || owner == nil || owner == fm || x.Flag != warNone {
			return
		}
		t := w.now()
		at := time.Date(t.Year(), t.Month(), t.Day()+1, 19+k, 0, 0, 0, t.Location())
		*x = fmWar{ID: id, Manor: id - 1, Host: owner.ID, Guest: fm.ID, HostName: owner.Name, GuestName: fm.Name,
			At: at.Unix(), Prepare: manorWarPrepare, Win: 0, Flag: warScheduled}
		w.fam.warHome[id] = w
		w.saveWars()
		w.shoutAll(fmt.Sprintf("%s 家族获得挑战庄园资格", fm.Name), colorYellow)
		w.sendWindow(p, n, "message", BtnOK, 0, "庄园挑战已经设定完成，请好好准备。")
		w.sayFamily(owner.ID, fmt.Sprintf("%s 家族向我们的%s下了战书：%s 开打！", fm.Name, manorTable[id-1].name, at.Format("1月2日 15:04")), colorRed)
	}
}

// ---------------- 场地预约人（scheduleman：家族 PK 场的预约） ----------------

func (w *World) schedulemanTalk(p, n *entity) {
	if max(abs(n.x-p.x), abs(n.y-p.y)) > 2 || !faceToFace(n, p, 2) {
		return
	}
	defer w.famLock()()
	id := npcArgNum(n.npc.Arg, "id")
	x := w.warOf(id)
	fm, _, m := w.fmOf(p)
	var sb strings.Builder
	fmt.Fprintf(&sb, "1\n　　　★家族ＰＫ场 %d 号★\n", id)
	switch x.Flag {
	case warNone:
		sb.WriteString("目前没有预约。\n")
	case warChallenge:
		fmt.Fprintf(&sb, "%s 向 %s 下了战书\n%s　等对方族长接受\n", x.HostName, x.GuestName, time.Unix(x.At, 0).Format("15:04"))
	case warScheduled, warPrepare, warFighting:
		fmt.Fprintf(&sb, "%s\n%s　%s\n", x.names(), time.Unix(x.At, 0).Format("1月2日 15:04"), []string{"", "", "已排定", "进场中", "开打中"}[x.Flag])
	case warDone:
		fmt.Fprintf(&sb, "%s 已结束：%s\n", x.names(), map[bool]string{true: "平手", false: w.warFamilyName(x.Winner) + " 获胜"}[x.Winner == 0])
	}
	leader := fm != nil && m.Duty == fmDutyLeader && fm.Setup
	opts := []string{}
	switch {
	case leader && x.Flag == warNone:
		opts = append(opts, "《 预约对战 》")
	case leader && x.Flag == warChallenge && x.Guest == fm.ID:
		opts = append(opts, "《 接受挑战 》")
	case leader && x.Flag <= warScheduled && x.Flag != warNone && x.Host == fm.ID:
		opts = append(opts, "《 取消预约 》")
	}
	if len(opts) == 0 {
		if fm == nil || m.Duty != fmDutyLeader {
			sb.WriteString("\n只有族长才能预约家族ＰＫ喔。")
		} else if !fm.Setup {
			sb.WriteString("\n你的家族还没有正式成立喔。")
		}
		w.sendWindow(p, n, "message", BtnOK, 0, strings.TrimPrefix(sb.String(), "1\n"))
		return
	}
	sb.WriteString("\n" + strings.Join(opts, "\n"))
	w.sendWindow(p, n, "select", BtnCancel, schedMainSeq, sb.String())
}

func (w *World) schedulemanReply(p, n *entity, win *npcWindow, button int, sel string) {
	defer w.famLock()()
	id := npcArgNum(n.npc.Arg, "id")
	x := w.warOf(id)
	fm, _, m := w.fmOf(p)
	if fm == nil || m.Duty != fmDutyLeader || !fm.Setup {
		return
	}
	k := data.Atoi(sel)
	switch win.seq {
	case schedMainSeq:
		if button != 0 || k != 1 {
			return
		}
		switch {
		case x.Flag == warNone:
			if w.warOfFamily(fm) != nil {
				w.sendWindow(p, n, "message", BtnOK, 0, "你已经安排过战斗，将机会留给其他家族吧。")
				return
			}
			// 挑对手：其他正式成立的家族（最多 10 个）
			var names strings.Builder
			var ids []int
			names.WriteString("1\n要向哪个家族下战书？\n")
			for _, f := range w.fam.sorted() {
				if f != fm && f.Setup && len(ids) < 10 {
					ids = append(ids, f.ID)
					names.WriteString(f.Name + "\n")
				}
			}
			if len(ids) == 0 {
				w.sendWindow(p, n, "message", BtnOK, 0, "现在没有别的家族可以约战。")
				return
			}
			w.sendWindow(p, n, "select", BtnCancel, schedFamilySeq, names.String())
			p.window.ids = ids
		case x.Flag == warChallenge && x.Guest == fm.ID:
			w.sendWindow(p, n, "message", BtnYes|BtnNo, schedAcceptSeq, fmt.Sprintf("%s 家族向你们下战书：\n%s　%s\n\n接受挑战吗？", x.HostName,
				time.Unix(x.At, 0).Format("1月2日 15:04"), []string{"生存战", "打飞战"}[x.Win]))
		case x.Flag != warNone && x.Flag <= warScheduled && x.Host == fm.ID:
			w.sayFamily(x.Guest, fmt.Sprintf("%s 家族取消了 %s 的对战预约。", fm.Name, time.Unix(x.At, 0).Format("15:04")), colorYellow)
			w.resetWar(x)
			w.sendWindow(p, n, "message", BtnOK, 0, "清除家族挑战。")
		}
	case schedFamilySeq:
		if button != 0 || k < 1 || k > len(win.ids) || x.Flag != warNone {
			return
		}
		guest := w.fam.list[win.ids[k-1]]
		if guest == nil {
			return
		}
		x.Host, x.Guest, x.HostName, x.GuestName = fm.ID, guest.ID, fm.Name, guest.Name
		var sb strings.Builder
		sb.WriteString("1\n请选择对战时间\n")
		t := w.now()
		for i := 1; i <= 6; i++ {
			sb.WriteString(time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+i, 0, 0, 0, t.Location()).Format("1月2日 15:04") + "\n")
		}
		w.sendWindow(p, n, "select", BtnCancel, schedTimeSeq, sb.String())
	case schedTimeSeq:
		if button != 0 || k < 1 || k > 6 || x.Flag != warNone || x.Host != fm.ID {
			return
		}
		t := w.now()
		x.At = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+k, 0, 0, 0, t.Location()).Unix()
		w.sendWindow(p, n, "select", BtnCancel, schedRuleSeq, "1\n胜利条件？\n生存战（时间到时剩的人多的赢）\n打飞战（被打出去的人少的赢）")
	case schedRuleSeq:
		if button != 0 || k < 1 || k > 2 || x.Flag != warNone || x.Host != fm.ID || x.At == 0 {
			return
		}
		x.Win, x.Prepare, x.Flag = k-1, pkWarPrepare, warChallenge
		w.fam.warHome[id] = w
		w.saveWars()
		w.sendWindow(p, n, "message", BtnOK, 0, "家族挑战设定完成。\n请对方族长到这里来接受挑战。")
		w.sayFamily(x.Guest, fmt.Sprintf("%s 家族向我们下了战书（家族ＰＫ场 %d 号，%s）！请族长到场地预约人那里接受。", fm.Name, id, time.Unix(x.At, 0).Format("15:04")), colorRed)
	case schedAcceptSeq:
		if button&BtnYes == 0 || x.Flag != warChallenge || x.Guest != fm.ID {
			return
		}
		x.Flag = warScheduled
		w.saveWars()
		w.sendWindow(p, n, "message", BtnOK, 0, "你的家族已经接受挑战。")
		w.sayFamily(x.Host, fmt.Sprintf("%s 家族接受了挑战：%s 在家族ＰＫ场 %d 号开打！", fm.Name, time.Unix(x.At, 0).Format("15:04"), id), colorYellow)
	}
}

// ---------------- 家族对战向导（FMWarpMan：进场） ----------------

func (w *World) fmWarpManTalk(p, n *entity) {
	if max(abs(n.x-p.x), abs(n.y-p.y)) > 2 || !faceToFace(n, p, 2) {
		return
	}
	defer w.famLock()()
	arg := n.npc.Arg
	x := w.warOf(npcArgNum(arg, "ID"))
	place := w.warPlaces()[x.ID]
	now := w.now()
	msg := func(key string, a ...any) string {
		s, _ := data.ArgField(arg, key)
		return fmt.Sprintf(npcText(s), a...)
	}
	mine := p.fm.Index != 0 && (p.fm.Index == x.Host || p.fm.Index == x.Guest)
	a, b := 0, 0
	if place != nil {
		a, b = w.warCount(x, place)
	}
	switch {
	case x.Flag == warNone || x.Flag == warChallenge:
		w.sendWindow(p, n, "message", BtnOK, 0, msg("TalkMsg"))
	case x.Flag == warScheduled:
		if mine {
			w.sendWindow(p, n, "message", BtnOK, 0, msg("NextMsg", a+b, now.Hour(), now.Minute()))
		} else {
			w.sendWindow(p, n, "message", BtnOK, 0, msg("AskMsg", x.GuestName, x.HostName))
		}
	case x.Flag == warPrepare:
		switch {
		case !mine:
			w.sendWindow(p, n, "message", BtnOK, 0, msg("AskMsg", x.GuestName, x.HostName))
		case p.party != nil:
			w.sendWindow(p, n, "message", BtnOK, 0, msg("PartyMsg"))
		default:
			w.sendWindow(p, n, "message", BtnYes|BtnNo, fmWarpEnterSeq, msg("FreeMsg", x.GuestName, x.HostName, now.Hour(), now.Minute()))
		}
	case x.Flag == warFighting:
		w.sendWindow(p, n, "message", BtnOK, 0, msg("BusyMsg", x.HostName, a, x.GuestName, b, now.Hour(), now.Minute()))
	default:
		result := "平手"
		if x.Winner != 0 {
			result = w.warFamilyName(x.Winner) + "获胜"
		}
		w.sendWindow(p, n, "message", BtnOK, 0, msg("EndMsg", x.GuestName, x.HostName, result))
	}
}

func (w *World) fmWarpManReply(p, n *entity, win *npcWindow, button int) {
	if win.seq != fmWarpEnterSeq || button&BtnYes == 0 || p.party != nil || p.fight != nil {
		return
	}
	defer w.famLock()()
	x := w.warOf(npcArgNum(n.npc.Arg, "ID"))
	place := w.warPlaces()[x.ID]
	if place == nil || x.Flag != warPrepare && x.Flag != warFighting || p.fm.Index == 0 {
		return
	}
	side := -1
	switch p.fm.Index {
	case x.Host:
		side = 0
	case x.Guest:
		side = 1
	}
	if side < 0 {
		return
	}
	a, b := w.warCount(x, place)
	if (side == 0 && a >= warMaxPlayers) || (side == 1 && b >= warMaxPlayers) {
		w.sysMsg(p, "场内人数已满。", colorYellow)
		return
	}
	pt := place.enter[side]
	w.warpTo(p, pt[0], pt[1], pt[2])
}

// ---------------- 家族对战服务员（FMPKMan：场内） ----------------

func (w *World) fmPkManTalk(p, n *entity) {
	if !faceToFace(p, n, 2) && max(abs(n.x-p.x), abs(n.y-p.y)) > 1 {
		return
	}
	s, _ := data.ArgField(n.npc.Arg, "MainMsg")
	w.sendWindow(p, n, "select", 0, fmPkMainSeq, "3\n　　    　　★家族ＰＫ场★\n"+npcText(s)+"\n           《 察看双方人数 》\n            《 离开ＰＫ场 》")
}

func (w *World) fmPkManReply(p, n *entity, win *npcWindow, button int, sel string) {
	arg := n.npc.Arg
	switch win.seq {
	case fmPkMainSeq:
		switch data.Atoi(sel) {
		case 1:
			defer w.famLock()()
			x := w.warOf(npcArgNum(arg, "ID"))
			a, b := 0, 0
			if place := w.warPlaces()[x.ID]; place != nil {
				a, b = w.warCount(x, place)
			}
			s, _ := data.ArgField(arg, "ViewMsg")
			w.sendWindow(p, n, "message", BtnOK, 0, fmt.Sprintf("\n%s\n\n%s:%4d人\n\n%s:%4d人", npcText(s), x.HostName, a, x.GuestName, b))
		case 2:
			s, _ := data.ArgField(arg, "LeavepkMsg")
			w.sendWindow(p, n, "message", BtnYes|BtnNo, fmPkLeaveSeq, npcText(s))
		}
	case fmPkLeaveSeq:
		if button&BtnYes == 0 || p.fight != nil {
			return
		}
		if p.party != nil { // NPC_PARTY_CHAECK1
			w.sysMsg(p, "无法以团队方式离开唷！请先解散团队！", colorYellow)
			return
		}
		pt := argPoint(arg, "WARP")
		w.warpTo(p, pt[0], pt[1], pt[2])
	}
}

// ---------------- 家族召唤员（FMPKCallMan） ----------------

func (w *World) fmPkCallManTalk(p, n *entity) {
	if !faceToFace(p, n, 2) && !inFront(p, n, 1) {
		return
	}
	s, _ := data.ArgField(n.npc.Arg, "MainMsg")
	w.sendWindow(p, n, "select", 0, callMainSeq, "3\n               ★家族ＰＫ场★\n"+npcText(s)+"\n              《召唤家族成员》\n               《返回记录点》\n                  《取消》")
}

func (w *World) fmPkCallManReply(p, n *entity, win *npcWindow, button int, sel string) {
	arg := n.npc.Arg
	text := func(key string, a ...any) string {
		s, _ := data.ArgField(arg, key)
		if strings.Contains(s, "%s") {
			return fmt.Sprintf(npcText(s), a...)
		}
		return npcText(s)
	}
	switch win.seq {
	case callMainSeq:
		switch data.Atoi(sel) {
		case 1:
			defer w.famLock()()
			fm, _, m := w.fmOf(p)
			switch {
			case fm == nil || m.Duty != fmDutyLeader:
				w.sendWindow(p, n, "message", BtnOK, 0, text("NoLeaderMsg"))
			case !fm.Setup:
				w.sendWindow(p, n, "message", BtnOK, 0, text("NoSetupMsg", fm.Name))
			default:
				x := w.warOfFamily(fm)
				switch {
				case x == nil || x.Flag < warScheduled:
					w.sendWindow(p, n, "message", BtnOK, 0, "你没有家族约战，请先约战吧。")
				case w.now().Add(time.Hour).Unix() < x.At:
					w.sendWindow(p, n, "message", BtnOK, 0, "请在对战前一小时再来召唤族员吧!")
				default:
					w.sendWindow(p, n, "message", BtnYes|BtnNo, callAskSeq, text("CallMsg", fm.Name))
				}
			}
		case 2:
			w.sendWindow(p, n, "message", BtnYes|BtnNo, callLeaveSeq, text("LeavepkMsg"))
		}
	case callAskSeq:
		if button&BtnYes == 0 {
			return
		}
		defer w.famLock()()
		fm, _, m := w.fmOf(p)
		if fm == nil || m.Duty != fmDutyLeader {
			return
		}
		w.sendWindow(p, n, "message", BtnOK, 0, "\n召唤中···请稍候！\n＠＃＄％↑＆＊···")
		pt := argPoint(arg, "WARP")
		floor := p.floor
		for _, mb := range fm.Members { // NPC_CallFMMember：在线、不在这层、不在战斗、不是申请中的族人各问一次
			if mb == nil || mb.ID == p.char.ID || !mb.formal() {
				continue
			}
			w.fam.tell(mb.ID, func(w2 *World, e *entity) {
				if e.floor == floor || e.fight != nil || e.watch != nil {
					return
				}
				e.window = &npcWindow{npc: 0, seq: callComeSeq, kind: "message", buttons: BtnYes | BtnNo, ids: []int{pt[0], pt[1], pt[2]}}
				e.client.Send(&proto.NpcWindow{ID: 0, Kind: "message", Buttons: BtnYes | BtnNo, Seq: callComeSeq,
					Text: "\n家族已经在ＰＫ罗～要不要加入呢？\n不过若是在组队状态中，将会脱离团队唷！"})
			})
		}
	case callLeaveSeq:
		if button&BtnYes == 0 || p.fight != nil {
			return
		}
		if p.party != nil {
			w.sendWindow(p, n, "message", BtnOK, 0, "\n无法以团队方式离开唷！\n请先解散团队！")
			return
		}
		h := w.elderPos(p)
		w.placeOnFloor(p, h.Floor, h.X, h.Y, p.dir)
		w.save(p)
	}
}

// warComeReply：被召唤的人按了"是"（npc 0 的窗口）。
func (w *World) warComeReply(e *entity, win *npcWindow, button int) {
	if button&BtnYes == 0 || len(win.ids) < 3 || e.fight != nil || e.watch != nil {
		return
	}
	w.leaveParty(e, false)
	w.warpTo(e, win.ids[0], win.ids[1], win.ids[2])
}
