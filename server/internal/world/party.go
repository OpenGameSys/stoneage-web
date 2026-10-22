package world

import (
	"fmt"
	"strings"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 组队（char_party.c、callfromcli.c lssproto_PR_recv / KTEAM_recv）：
//
//	加入      面前那一格的玩家（他是队员时找他的队长）：要站在队长身边 1 格内、队长没在战斗、开着"组队"、队伍没满（5 人）；
//	          面前有好几个可以加入的队伍时开选单"和谁组成团队呢？"。面前的人变成队长，自己排在第一个空位（1..4，空位不往前补）。
//	离开      队长离开就解散（"团队已解散！"，不会交给别人）；队员离开"脱离团队！"，其他人"<名字> 脱离团队！"；队长可以踢人（_TEAM_KICKPARTY）。
//	走路      队员不能自己走（只能转身），队长走一步，每个队员走进前一个人刚离开的那一格；只有队长会遇敌；队长传送时全队一起传送。
//	战斗      队长在 0，队员依序 1..4（BATTLE_PartyNewEntry），经验不分享（给打倒的人）；逃走的队长解散队伍、逃走的队员离队；倒下不会离队。
//	"N" 字串  PartyMember：每个位置的物件编号、等级、HP、MP、名字（CHAR_sendStatusString "N"）。
//	开关      "组队"预设开（_ALWAYS_TEAM_OPEN），交易与名片预设关；FS 的位元见 fs*。
//
// 与原版不同：队员自己被传送走（NPC、道具）时离队（原版留在队伍里但跟不上）；随机目的地的传送全队用同一个结果。

const partyMax = 5 // CHAR_PARTYMAX

// FS 的位元（CHAR_FS_*）
const (
	fsParty     = 1 << 0
	fsDuel      = 1 << 1
	fsPartyChat = 1 << 2
	fsCard      = 1 << 3
	fsTrade     = 1 << 4
	fsFM        = 1 << 6 // CHAR_ISFM：家族频道（PC_ETCFLAG_CHAT_FM）
)

// partySelectSeq 是选队伍的窗口（CHAR_WINDOWTYPE_SELECTPARTY），用 NPC 编号 0。
const partySelectSeq = 20

// party 是一支队伍：slots[0] 是队长，队员在 1..4，离开的人留下空位。
type party struct {
	slots [partyMax]*entity
}

func (p *party) leader() *entity { return p.slots[0] }

// isFollower：e 在队伍里而且不是队长。
func (e *entity) isFollower() bool { return e.party != nil && e.party.leader() != e }

// isLeader：e 是队长。
func (e *entity) isLeader() bool { return e.party != nil && e.party.leader() == e }

// noPartyFloors 是 lssproto_PR_recv 不能组队的楼层（还有时间票地图，这里没有）。
func noPartyFloor(floor int) bool {
	switch {
	case floor == 31706, floor == 10204, floor >= 10601 && floor <= 10605, floor == 10919, floor == 10920,
		floor == 20711, floor == 20712, floor == 1008, floor == 1021, floor == 3008, floor == 3021,
		floor >= 8200 && floor <= 8213, floor >= 30017 && floor <= 30021, floor == 17003, floor == 17005:
		return true
	}
	return false
}

func chebyshev(a, b *entity) int {
	if a.floor != b.floor {
		return 1 << 30
	}
	return max(abs(a.x-b.x), abs(a.y-b.y))
}

// SetFlags 是 lssproto_FS_recv：设定开关，回传设好的值。
func (w *World) SetFlags(charID, flags int) {
	w.Do(func() {
		if e := w.players[charID]; e != nil {
			e.fs = flags & 0x1fff // 原样回传（频道开关等服务端不管的位元也留着）
			e.client.Send(&proto.Flags{Flags: e.fs})
		}
	})
}

// PartyRequest 是 lssproto_PR_recv：1 加入面前的队伍，0 离开。战斗中、交易中不理。
func (w *World) PartyRequest(charID, request int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || e.fight != nil || e.watch != nil || e.trade != nil {
			return
		}
		if request == 0 {
			w.leaveParty(e, true)
			return
		}
		if noPartyFloor(e.floor) {
			return
		}
		w.joinParty(e)
	})
}

// partyCandidate：o 是面前那一格的玩家时，回传可以加入的队长（CHAR_JoinParty 的检查）。
func (w *World) partyCandidate(e, o *entity) *entity {
	leader := o
	if o.party != nil {
		leader = o.party.leader()
	}
	if chebyshev(e, leader) > 1 || leader.fight != nil || leader.watch != nil || leader.fs&fsParty == 0 || leader.trade != nil {
		return nil
	}
	if leader.party != nil && leader.party.free() < 0 {
		return nil
	}
	return leader
}

// free 是第一个空位（CHAR_getEmptyPartyArray），满了回 -1。
func (p *party) free() int {
	for i := 1; i < partyMax; i++ {
		if p.slots[i] == nil {
			return i
		}
	}
	return -1
}

// joinParty 是 CHAR_JoinParty。
func (w *World) joinParty(e *entity) {
	if e.party != nil {
		e.client.Send(&proto.PartyResult{Request: 1, OK: false})
		return
	}
	fx, fy := e.x+MOVE[e.dir][0], e.y+MOVE[e.dir][1]
	var cands []*entity
	found := false
	for _, o := range w.floors[e.floor] {
		if o.bus != nil && o.x == fx && o.y == fy { // 面前是巴士 / 飞机：按"组队"钮也算上车（原版要对它说话；两种都行）
			w.busBoard(e, o)
			e.client.Send(&proto.PartyResult{Request: 1, OK: e.party != nil})
			return
		}
		if o == e || o.kind != "player" || o.x != fx || o.y != fy {
			continue
		}
		found = true
		if l := w.partyCandidate(e, o); l != nil && !containsEntity(cands, l) {
			cands = append(cands, l)
		}
	}
	switch len(cands) {
	case 0:
		if found {
			w.sysMsg(e, "无法加入团队。", colorYellow)
		}
		e.client.Send(&proto.PartyResult{Request: 1, OK: false})
	case 1:
		w.joinPartyMain(e, cands[0])
		e.client.Send(&proto.PartyResult{Request: 1, OK: true})
	default:
		var sb strings.Builder
		sb.WriteString("1\n和谁组成团队呢？\n")
		ids := make([]int, len(cands))
		for i, c := range cands {
			sb.WriteString(c.name + "\n")
			ids[i] = c.id
		}
		e.window = &npcWindow{npc: 0, seq: partySelectSeq, kind: "select", buttons: BtnCancel, ids: ids}
		e.client.Send(&proto.NpcWindow{ID: 0, Kind: "select", Buttons: BtnCancel, Seq: partySelectSeq, Text: sb.String()})
	}
}

func containsEntity(list []*entity, e *entity) bool {
	for _, x := range list {
		if x == e {
			return true
		}
	}
	return false
}

// partySelectReply 是 CHAR_JoinParty_WindowResult：再检查一次，失败或取消时"无法加入团队。"。
func (w *World) partySelectReply(e *entity, win *npcWindow, button int, arg string) {
	ok := false
	if n := data.Atoi(arg) - 1; button == 0 && n >= 0 && n < len(win.ids) && e.party == nil {
		if o := w.players[win.ids[n]]; o != nil && w.partyCandidate(e, o) == o {
			w.joinPartyMain(e, o)
			ok = true
		}
	}
	if !ok {
		w.sysMsg(e, "无法加入团队。", colorYellow)
	}
	e.client.Send(&proto.PartyResult{Request: 1, OK: ok})
}

// joinPartyMain 是 CHAR_JoinParty_Main：没有队伍的人变成队长；新队员拿到每个位置的资料，其他人拿到新队员那一格。
func (w *World) joinPartyMain(e, leader *entity) {
	p := leader.party
	first := p == nil
	if first {
		p = &party{}
		p.slots[0] = leader
		leader.party = p
		w.broadcastMark(leader)
	}
	slot := p.free()
	if slot < 0 {
		return
	}
	p.slots[slot] = e
	e.party = p
	if first {
		leader.client.Send(w.partyMember(p, 0))
	}
	for i, m := range p.slots {
		if m != nil {
			e.client.Send(w.partyMember(p, i))
		}
	}
	for _, m := range p.slots {
		if m != nil && m != e {
			m.client.Send(w.partyMember(p, slot))
			w.sysMsg(m, e.name+" 加入团队！", colorYellow)
		}
	}
	w.sysMsg(e, "加入团队！", colorYellow)
}

// partyMember 是 "N<k>" 字串。
func (w *World) partyMember(p *party, slot int) *proto.PartyMember {
	m := p.slots[slot]
	if m == nil {
		return &proto.PartyMember{Slot: slot}
	}
	if m.kind != "player" { // 巴士 / 飞机当队长（bus.go）
		return &proto.PartyMember{Slot: slot, Used: true, ID: m.id, Level: 1, MaxHp: 1, HP: 1, Mp: 0, Name: m.name}
	}
	st := w.statusOf(m)
	return &proto.PartyMember{Slot: slot, Used: true, ID: m.id, Level: st.Level, MaxHp: st.MaxHp, HP: st.HP, Mp: st.Mp, Name: m.name}
}

// partyUpdate 是 CHAR_PartyUpdate：e 的等级、HP、MP 变了，告诉其他队员。
func (w *World) partyUpdate(e *entity) {
	p := e.party
	if p == nil {
		return
	}
	for i, m := range p.slots {
		if m == e {
			msg := w.partyMember(p, i)
			for _, o := range p.slots {
				if o != nil && o != e {
					o.client.Send(msg)
				}
			}
		}
	}
}

// sendStatus 送 e 自己的状态，顺便更新队员看到的 HP、MP。
func (w *World) sendStatus(e *entity) {
	e.client.Send(w.statusOf(e))
	w.partyUpdate(e)
}

// leaveParty 是 CHAR_DischargeParty：msg 为 false 时不说话（CHAR_DischargePartyNoMsg）。
func (w *World) leaveParty(e *entity, msg bool) {
	p := e.party
	if p == nil {
		return
	}
	if p.leader() == e {
		for i, m := range p.slots {
			if m == nil {
				continue
			}
			p.slots[i], m.party = nil, nil
			if msg {
				w.sysMsg(m, "团队已解散！", colorYellow)
			}
			m.client.Send(&proto.PartyResult{Request: 0, OK: true})
		}
		w.broadcastMark(e)
		return
	}
	slot := 0
	for i, m := range p.slots {
		if m == e {
			slot = i
		}
	}
	p.slots[slot], e.party = nil, nil
	if msg {
		w.sysMsg(e, "脱离团队！", colorYellow)
	}
	e.client.Send(&proto.PartyResult{Request: 0, OK: true})
	alone := true
	for _, m := range p.slots {
		if m == nil {
			continue
		}
		if msg {
			w.sysMsg(m, e.name+" 脱离团队！", colorYellow)
		}
		m.client.Send(&proto.PartyMember{Slot: slot})
		alone = alone && m == p.leader()
	}
	if alone { // 只剩队长：队伍没了
		l := p.leader()
		p.slots[0], l.party = nil, nil
		w.broadcastMark(l)
	}
}

// PartyKick 是 lssproto_KTEAM_recv：队长把 slot 的队员踢出去。
func (w *World) PartyKick(charID, slot int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || !e.isLeader() || slot < 1 || slot >= partyMax || e.party.slots[slot] == nil {
			return
		}
		m := e.party.slots[slot]
		w.leaveParty(m, true)
		w.sysMsg(m, fmt.Sprintf("队长[%s]将你踢除！", e.name), colorYellow)
		w.sysMsg(e, fmt.Sprintf("将[%s]踢除出团队！", m.name), colorYellow)
	})
}

// partyFollow 是 CHAR_walkcall 里队员跟着走：每个人走进前一个人刚离开的那一格（ox, oy 是队长走之前的位置）。
func (w *World) partyFollow(e *entity, ox, oy int) {
	if !e.isLeader() {
		return
	}
	ex, ey := ox, oy
	for _, m := range e.party.slots[1:] {
		if m == nil || m.fight != nil || m.floor != e.floor {
			continue
		}
		sx, sy := m.x, m.y
		if sx == ex && sy == ey {
			continue
		}
		dx, dy := sign(ex-sx), sign(ey-sy)
		d := moveDir(dx, dy)
		m.x, m.y, m.dir = sx+dx, sy+dy, d
		m.clearAction()
		ex, ey = sx, sy
		w.broadcastMove(m)
		m.client.Send(&proto.ObjMove{ID: m.id, X: m.x, Y: m.y, Dir: m.dir}) // 队员自己的画面跟着走
	}
}

// moveDir 是 MOVE 里 (dx, dy) 的方向。
func moveDir(dx, dy int) int {
	for d, m := range MOVE {
		if m[0] == dx && m[1] == dy {
			return d
		}
	}
	return 0
}

// battleMembers 是一起开打的玩家（BATTLE_PartyNewEntry）：队长与在同一楼层、没在战斗的队员，按位置排；没有组队时只有自己。
func (w *World) battleMembers(e *entity) []*entity {
	if !e.isLeader() {
		return []*entity{e}
	}
	var out []*entity
	for _, m := range e.party.slots {
		if m != nil && (m == e || m.fight == nil && m.floor == e.floor) {
			out = append(out, m)
		}
	}
	return out
}

// broadcastMark 告诉看得到 e 的人（与 e 自己）它头上的记号变了（CA 的队长 21、战斗 20、观战 22）。
func (w *World) broadcastMark(e *entity) {
	msg := e.mark()
	e.client.Send(msg)
	for _, o := range w.floors[e.floor] {
		if o != e && o.client != nil && o.visible[e.id] {
			o.client.Send(msg)
		}
	}
}

// mark 是 e 头上的记号：队长、战斗 / 求救、交易中（CHAR_ACTTRADE：交易与摆摊的买卖双方）、摊位名（CHAR_STREETVENDOR_OPEN）。
func (e *entity) mark() *proto.ObjMark {
	m := &proto.ObjMark{ID: e.id, Leader: e.isLeader(), Trade: e.trade != nil || e.buying != nil || (e.stall != nil && e.stall.who != nil)}
	if e.stall != nil {
		m.Vendor = e.stall.title
	}
	if e.watch != nil {
		m.Battle = e.watch.no + 1 // 观战的人头上也有战斗记号（CHAR_sendBattleWatch）
	} else if e.fight != nil {
		m.Battle = e.fight.room.no + 1
		m.Help = e.fight.room.help
	}
	return m
}

// partyAt 是 CHAR_getPartyIndex：e 的队伍第 slot 格的人（0 是队长，可以是自己），没组队或空位回 nil。
func (w *World) partyAt(e *entity, slot int) *entity {
	if e.party == nil || slot < 0 || slot >= partyMax {
		return nil
	}
	return e.party.slots[slot]
}
