package world

import (
	"fmt"
	"strings"
	"time"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 长毛象巴士与加美航空（npc/npc_bus.c、npc_airplane.c "Made from Bus"）：会沿着路线走动的 NPC，乘客是它的队员。
//
//	参数  routenum、routetoN（巴士 "x,y;x,y;…"，飞机 "楼层,x,y;…"，换楼层就连乘客一起传送）、routenameN（路线名）、
//	      waittime（到站后等几秒再开，预设 180）、needstone（车资）、needlevel、denieditem（不准带的道具）、oneway（飞机单程）、
//	      msg_gettingon / msg_notparty / msg_overparty / msg_denieditem / msglevel / msg_stone / msg_start / msg_end（没写用预设的话）。
//	上车  面向停着的巴士说话（NPC_BusCheckJoinParty）：开走了不能上（msg_gettingon）、组队中不能上、满员（队伍 5 格，司机占 1 格）、
//	      带着禁带道具或货物、等级不够、钱不够都不行；付了车资就加入巴士的队伍（CHAR_JoinParty_Main：巴士是队长，乘客跟着它走）。
//	运行  NPC_BusLoop：mode 0 在站上等 waittime（乘客再对它说话可以提早出发）→ mode 1 每 NPC_BUS_LOOPTIME 走一步
//	      → 走完全部点 mode 3，3 秒后解散队伍（乘客下车）、
//	      随机挑一条路线、掉头（roundtrip）回到 mode 0。飞机 oneway 时到站后直接传回起点。
//	巴士的队伍沿用 party.go：巴士实体带一个什么都不送的 nopClient，队员跟随（partyFollow）、换楼层一起传（placeOnFloor）都照旧。
//	原版的 SE 60（喇叭）与路线名当作称号显示（CHAR_OWNTITLE）这里没有：路线名写在上车的讯息里。
//	路线  原版 MAP_walkAble 对 CHAR_TYPEBUS（巴士、飞机与它们的乘客）一律回 TRUE：不看通行，每步朝目标点直走（NPC_Util_getDirFromTwoPoint）。
//	      以前这里照一般 NPC 查通行、被挡就往旁边滑，在桥头会来回打转卡死。现在开机时把每条路线相邻两站之间连成直线的逐格路径
//	      （busRoute.path，x、y 各自每步靠近 1 格），行驶时只按下标走、回程倒着走，不再临时算；两站不在同一层（飞机）就在那一步传送。

const (
	busLoopTime = 300 * time.Millisecond // 预设的每步间隔（rules.busStep 可调；原版 NPC_BUS_LOOPTIME 200 ms）：客户端按收到的节奏连续走，乘客跟得上
	busWaitTime = 180 * time.Second      // NPC_BUS_WAITTIME_DEFAULT
	busArrived  = 3 * time.Second        // mode 3 停 3 秒
)

var busDefaultMsg = map[string]string{
	"msg_gettingon":  "PAON！（你无法於中途加入我们唷！）",
	"msg_notparty":   "PAPAON！！无法以团队加入唷！",
	"msg_overparty":  "PAON！！人数已满。",
	"msg_denieditem": "PAPAON！！我可不要这个道具！",
	"msg_allowitem":  "哇喔~(想要那个道具啊!)",
	"msglevel":       "PAPAON！！你的等级还不够唷！",
	"msg_stone":      "PAPAON！！金钱不足唷！",
	"msg_event":      "PAON！！你无法加入唷！",
	"msg_start":      "哇喔~(出发进行)",
	"msg_end":        "哇喔~(到罗)",
}

type busRoute struct {
	name   string
	points [][3]int // 楼层, x, y（参数写的站点）
	path   [][3]int // 开机算好的逐格路径：从 points[0] 到最后一点，相邻两格不是相邻（换楼层 / 找不到路）就传送
}

type bus struct {
	routes   []busRoute
	cur      int  // 现在走的路线
	step     int  // 现在站在 path 的第几格
	reverse  bool // 回程（NPC_WORK_ROUNDTRIP）
	mode     int  // 0 停站 1 行驶 3 到站
	se       bool // seflg：发车、到站时对附近的人放音效 60（CHAR_sendSEoArroundCharacter）
	at       time.Time
	wait     time.Duration
	oneway   bool
	airplane bool
	deny     []int
	level    int
	stone    int
}

// nopClient 给会走动的 NPC 当队长用：party.go 会对队长送讯息，NPC 没有连线，全部丢掉。
type nopClient struct{}

func (nopClient) Send(proto.Message) {}
func (nopClient) Kick(string)        {}

// newBus 按参数建巴士（NPC_BusInit）；参数不全回 nil（原版 return FALSE，NPC 不建）。
func (w *World) newBus(n *entity) *bus {
	arg := n.npc.Arg
	num := npcArgNum(arg, "routenum")
	if num <= 0 {
		return nil
	}
	b := &bus{airplane: n.npc.Function == "Airplane", wait: busWaitTime}
	for i := 1; i <= num; i++ {
		s, ok := data.ArgField(arg, fmt.Sprintf("routeto%d", i))
		if !ok {
			return nil
		}
		r := busRoute{}
		r.name, _ = data.ArgField(arg, fmt.Sprintf("routename%d", i))
		for _, pt := range strings.Split(s, ";") {
			f := strings.Split(strings.TrimSpace(pt), ",")
			switch {
			case len(f) >= 3: // 飞机：楼层,x,y
				r.points = append(r.points, [3]int{data.Atoi(f[0]), data.Atoi(f[1]), data.Atoi(f[2])})
			case len(f) == 2:
				r.points = append(r.points, [3]int{n.floor, data.Atoi(f[0]), data.Atoi(f[1])})
			}
		}
		if len(r.points) < 2 {
			return nil
		}
		r.path = w.busPath(r.points)
		b.routes = append(b.routes, r)
	}
	if v := npcArgNum(arg, "waittime"); v > 0 {
		b.wait = time.Duration(v) * time.Second
	}
	b.stone = max(0, npcArgNum(arg, "needstone"))
	b.level = max(0, npcArgNum(arg, "needlevel"))
	b.oneway = npcArgNum(arg, "oneway") == 1
	b.se = npcArgNum(arg, "seflg") == 1
	if s, ok := data.ArgField(arg, "denieditem"); ok {
		for _, x := range strings.Split(s, ",") {
			if id := data.Atoi(x); id > 0 {
				b.deny = append(b.deny, id)
			}
		}
	}
	b.cur = w.rnd.IntN(len(b.routes))
	b.step = 0 // 停在路线的起点上
	if npcArgNum(arg, "reverse") == 1 {
		b.reverse = true
		b.step = len(b.routes[b.cur].path) - 1
	}
	b.at = w.now()
	// 停在路线的起点上（.create 的出生点通常就是它）
	p0 := b.routes[b.cur].points[0]
	if b.reverse {
		p0 = b.routes[b.cur].points[len(b.routes[b.cur].points)-1]
	}
	n.floor, n.x, n.y = p0[0], p0[1], p0[2]
	return b
}

func (w *World) busMsg(n *entity, key string) string {
	if s, ok := data.ArgField(n.npc.Arg, key); ok && s != "" {
		return s
	}
	return busDefaultMsg[key]
}

// busSay：巴士对一个人说话（NPC_BusSendMsg）。
func (w *World) busSay(n, p *entity, key string) {
	if p.client != nil {
		w.npcSay(p, n, w.busMsg(n, key))
	}
}

func (w *World) busPassengers(n *entity) []*entity {
	var out []*entity
	if n.party != nil {
		for _, m := range n.party.slots[1:] {
			if m != nil {
				out = append(out, m)
			}
		}
	}
	return out
}

// busTalk 是 NPC_BusTalked：乘客再说话就提早出发；不是乘客的要上车。
func (w *World) busTalk(p, n *entity) {
	b := n.bus
	if b == nil {
		return
	}
	if p.party != nil && p.party.leader() == n {
		if b.mode == 0 {
			w.busStart(n)
		}
		return
	}
	w.busBoard(p, n)
}

// busBoard 是 NPC_BusCheckJoinParty + CHAR_JoinParty_Main。
func (w *World) busBoard(p, n *entity) {
	b := n.bus
	if !inFront(p, n, 1) {
		return
	}
	if b.mode != 0 {
		w.busSay(n, p, "msg_gettingon")
		return
	}
	if p.party != nil {
		w.busSay(n, p, "msg_notparty")
		return
	}
	if n.party != nil && n.party.free() < 0 {
		w.busSay(n, p, "msg_overparty")
		return
	}
	for _, id := range b.deny {
		if w.hasItemWhere(p, func(d *data.ItemDef) bool { return d.ID == id }) {
			w.busSay(n, p, "msg_denieditem")
			return
		}
	}
	if w.hasItemWhere(p, func(d *data.ItemDef) bool { return d.Type == data.ItemWares }) { // _ITEM_CHECKWARES
		w.sysMsg(p, "无法携带货物上车。", colorYellow)
		return
	}
	if b.level > 0 && p.char.Level < b.level {
		w.busSay(n, p, "msglevel")
		return
	}
	if b.stone > 0 {
		if p.char.Gold < b.stone {
			w.busSay(n, p, "msg_stone")
			return
		}
		p.char.Gold -= b.stone
		w.sendStatus(p)
		w.sysMsg(p, fmt.Sprintf("支付了%d Stone！", b.stone), colorYellow)
	}
	w.joinPartyMain(p, n)
	if name := b.routes[b.cur].name; name != "" {
		w.sysMsg(p, "上车了："+name, colorYellow)
	}
}

func (w *World) busStart(n *entity) {
	b := n.bus
	b.mode, b.at = 1, w.now()
	w.busSE(n)
	for _, m := range w.busPassengers(n) {
		w.busSay(n, m, "msg_start")
	}
}

// tickBuses 每 rules.busStep 一次（NPC_BusLoop）。
func (w *World) tickBuses() {
	for _, n := range w.buses {
		w.busLoop(n)
	}
}

func (w *World) busLoop(n *entity) {
	b := n.bus
	now := w.now()
	switch b.mode {
	case 0:
		if now.Sub(b.at) >= b.wait {
			w.busStart(n)
		}
	case 1:
		w.busWalk(n)
	case 3:
		if now.Sub(b.at) < busArrived {
			return
		}
		w.leaveParty(n, false) // 乘客下车（CHAR_DischargeParty）
		if b.oneway {          // 单程：传回起点再等
			p0 := b.routes[b.cur].points[0]
			w.npcWarp(n, p0[0], p0[1], p0[2])
			b.cur = w.rnd.IntN(len(b.routes))
			b.step = 0
		} else {
			b.cur = w.rnd.IntN(len(b.routes))
			b.reverse = !b.reverse
			b.step = 0
			if b.reverse {
				b.step = len(b.routes[b.cur].path) - 1
			}
		}
		b.mode, b.at = 0, now
	}
}

// busWalk 是 NPC_Bus_walk 的固定路径版：沿 path 走一格（回程倒着走），乘客跟着（partyFollow）；走到路径尽头就到站。
// 下一格不在同一层或不相邻（飞机换楼层、开机时没找到路）就传送过去（NPC_BusSetPoint 的 CHAR_warpToSpecificPoint）。
func (w *World) busWalk(n *entity) {
	b := n.bus
	path := b.routes[b.cur].path
	next := b.step + 1
	if b.reverse {
		next = b.step - 1
	}
	if next < 0 || next >= len(path) {
		b.step = max(0, min(len(path)-1, b.step))
		b.mode, b.at = 3, w.now()
		w.busSE(n)
		for _, m := range w.busPassengers(n) {
			w.busSay(n, m, "msg_end")
		}
		return
	}
	t := path[next]
	b.step = next
	if t[0] != n.floor || abs(t[1]-n.x) > 1 || abs(t[2]-n.y) > 1 {
		w.npcWarp(n, t[0], t[1], t[2])
		return
	}
	if t[1] == n.x && t[2] == n.y {
		return
	}
	ox, oy := n.x, n.y
	n.x, n.y, n.dir = t[1], t[2], moveDir(t[1]-ox, t[2]-oy)
	w.broadcastMove(n)
	w.partyFollow(n, ox, oy)
}

// busPath 把路线的站点串成逐格路径：同一层的相邻两站之间直线走（x、y 各自每步靠近 1 格，原版 getDirFromTwoPoint 的走法，
// 不查通行：原版 MAP_walkAble 对巴士一律 TRUE）；换楼层就只接上目标点（行驶时传送）。
func (w *World) busPath(points [][3]int) [][3]int {
	path := [][3]int{points[0]}
	for i := 1; i < len(points); i++ {
		a, t := points[i-1], points[i]
		if a[0] != t[0] {
			path = append(path, t)
			continue
		}
		x, y := a[1], a[2]
		for x != t[1] || y != t[2] {
			x, y = x+sign(t[1]-x), y+sign(t[2]-y)
			path = append(path, [3]int{a[0], x, y})
		}
	}
	return path
}

// npcWarp 把会走动的 NPC（与它的队伍）传到别的楼层。
func (w *World) npcWarp(n *entity, floor, x, y int) {
	f := w.game.Floors[floor]
	if f == nil || !f.In(x, y) {
		return
	}
	w.placeOnFloor(n, floor, x, y, n.dir)
}

// busSE 是 CHAR_sendSEoArroundCharacter：seflg 的巴士 / 飞机发车、到站时，视野内没在战斗的玩家都听到音效 60（lssproto_SE）。
func (w *World) busSE(n *entity) {
	if !n.bus.se {
		return
	}
	msg := &proto.Sound{X: n.x, Y: n.y, Se: 60}
	for _, o := range w.floors[n.floor] {
		if o.client != nil && o.fight == nil && abs(o.x-n.x) <= ViewRadius && abs(o.y-n.y) <= ViewRadius {
			o.client.Send(msg)
		}
	}
}
