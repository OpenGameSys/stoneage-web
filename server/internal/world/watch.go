package world

import (
	"strings"

	"sa/server/internal/proto"
)

// 观战（battle.c BATTLE_WatchTry / BATTLE_WatchEntry / BATTLE_CreateForWatcher）：
// 面向正在战斗的人按"参战"，而那场不能加入（没开求救、决斗、只能观战的楼层）时，改为观战：原版给观战者开一场
// BATTLE_TYPE_WATCH 的战斗挂在被看的那场后面（WatchLink），每回合的战斗字串一并送给它（BATTLE_WatchBroadCast）；
// 这里直接把观战者记在 battleRoom.watchers，BattleStart（watch = true、mySlot = -1）、每回合的 BattleTurn、
// 中途加入的 BattleJoin 都照送，被看的那场打完或观战者按"离开"（BattleCommand kind "leave"）就送 BattleEnd（result "watch"）。
// 观战中和战斗中一样不能走动、说话给 NPC、组队、交易；头上也有战斗记号。组队时由队长按，全队一起看（BATTLE_WatchPartyNewEntry）；
// 队员按了是"无法观战。"。面前有好几场时开选单挑一场（CHAR_WINDOWTYPE_SELECTBATTLEWATCH）。

const watchSelectSeq = 24 // CHAR_WINDOWTYPE_SELECTBATTLEWATCH（原版 22，这里 22 / 23 已给名片与决斗）

// watchTry 是 BATTLE_WatchTry：面前一格里正在战斗的玩家（不含观战的人）；found 是那里有没有人。
func (w *World) watchTry(e *entity, found bool) {
	if e.isFollower() {
		w.sysMsg(e, "无法观战。", colorYellow)
		return
	}
	fx, fy := e.x+MOVE[e.dir][0], e.y+MOVE[e.dir][1]
	var cands []*entity
	for _, o := range w.floors[e.floor] {
		if o.kind == "player" && o != e && o.x == fx && o.y == fy && o.fight != nil && !containsEntity(cands, o) {
			cands = append(cands, o)
		}
	}
	switch len(cands) {
	case 0:
		if !found {
			w.sysMsg(e, "那里没有任何人。", colorYellow)
		} else {
			w.sysMsg(e, "无法观战。", colorYellow)
		}
	case 1:
		w.watchEntry(e, cands[0])
	default:
		var sb strings.Builder
		sb.WriteString("1\n观看谁的战斗呢？\n")
		ids := make([]int, len(cands))
		for i, c := range cands {
			sb.WriteString(c.name + "\n")
			ids[i] = c.id
		}
		e.window = &npcWindow{npc: 0, seq: watchSelectSeq, kind: "select", buttons: BtnCancel, ids: ids}
		e.client.Send(&proto.NpcWindow{ID: 0, Kind: "select", Buttons: BtnCancel, Seq: watchSelectSeq, Text: sb.String()})
	}
}

// watchSelectReply：选单里挑了一场（再查一次对方还在打、还在身边）。
func (w *World) watchSelectReply(e *entity, win *npcWindow, button int, arg string) {
	n := atoiSafe(arg) - 1
	if button != 0 || n < 0 || n >= len(win.ids) || e.fight != nil || e.watch != nil {
		return
	}
	if o := w.players[win.ids[n]]; o != nil && o.fight != nil && chebyshev(e, o) <= 1 {
		w.watchEntry(e, o)
	} else {
		w.sysMsg(e, "无法观战。", colorYellow)
	}
}

// watchEntry 是 BATTLE_WatchEntry：e（队长时连同全队）开始看 target 那一场。
func (w *World) watchEntry(e, target *entity) {
	room := target.fight.room
	if len(room.members) == 0 {
		w.sysMsg(e, "无法观战。", colorYellow)
		return
	}
	for _, m := range w.battleMembers(e) {
		if m.fight == nil && m.watch == nil && m.floor == e.floor {
			w.startWatch(m, room)
		}
	}
}

func (w *World) startWatch(e *entity, room *battleRoom) {
	w.cancelTrade(e)
	e.clearAction()
	e.window = nil
	e.watch = room
	room.watchers = append(room.watchers, e)
	msg := &proto.BattleStart{Field: room.field, MySlot: -1, TurnTime: 0, Pvp: room.pvp, Event: room.npc != nil, Watch: true, Speed: w.battleSpeedPct()}
	for _, f := range room.b.F {
		if f != nil {
			msg.Fighters = append(msg.Fighters, w.fighterInfo(room, f))
		}
	}
	e.client.Send(msg)
	w.broadcastMark(e) // CHAR_sendBattleWatch(ON)
}

// stopWatch：观战结束（自己离开、被看的那场打完、下线、被传送）。notify 时送 BattleEnd（result "watch"）让客户端回到地图。
func (w *World) stopWatch(e *entity, notify bool) {
	room := e.watch
	if room == nil {
		return
	}
	e.watch = nil
	for i, x := range room.watchers {
		if x == e {
			room.watchers = append(room.watchers[:i:i], room.watchers[i+1:]...)
			break
		}
	}
	if notify && e.client != nil {
		e.client.Send(&proto.BattleEnd{Result: "watch", Items: []string{}})
	}
	if e.client != nil {
		w.broadcastMark(e)
	}
}

// endWatchers：被看的那场没有人了 / 打完了，观战的人全部回地图。
func (w *World) endWatchers(room *battleRoom) {
	for _, x := range append([]*entity(nil), room.watchers...) {
		w.stopWatch(x, true)
	}
}

// sendWatchers 把一则讯息送给这场的观战者。
func (w *World) sendWatchers(room *battleRoom, msg proto.Message) {
	for _, x := range room.watchers {
		if x.client != nil {
			x.client.Send(msg)
		}
	}
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
		if n > 1<<20 {
			break
		}
	}
	return n
}
