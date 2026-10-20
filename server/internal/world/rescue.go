package world

import (
	"strings"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 求救与参战（callfromcli.c lssproto_HL_recv / JB_recv，battle.c BATTLE_RescueTry / BATTLE_RescueEntry）：
//
//	求救     战斗中按"求救"切换我方的 BSIDE_FLG_HELP_OK，我方每个玩家看到"<名字> 在求救！" / "<名字> 决定拒绝帮助。"，地图上的战斗记号多一个求救图
//	参战     没组队的玩家面向开着求救的战斗里的人按"参战"，站进我方第一个空位（0..4），从下一回合开始下指令；
//	         面前有好几场可以加入时开选单"加入谁的战斗呢？"
//
// 观战（BATTLE_WatchTry，组队时参战会改成观战）还没做：该观战的地方说"无法观战。"。

const battleSelectSeq = 21 // CHAR_WINDOWTYPE_SELECTBATTLE

// noHelpFloor 是不能求救的楼层（_LOCKHELP_OK、_NO_HELP_MAP）。
func noHelpFloor(floor int) bool {
	return floor >= 8200 && floor <= 8213 || floor >= 30017 && floor <= 30021 || floor == 20000 || floor >= 33001 && floor <= 33101
}

// BattleHelp 是 lssproto_HL_recv。
func (w *World) BattleHelp(charID int, on bool) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || e.fight == nil || e.fight.room.pvp || noHelpFloor(e.floor) {
			return
		}
		room := e.fight.room
		room.help = on
		msg := e.name + " 在求救！"
		if !on {
			msg = e.name + " 决定拒绝帮助。"
		}
		for _, m := range room.members {
			m.client.Send(&proto.HelpFlag{On: on})
			w.sysMsg(m, msg, colorYellow)
			w.broadcastMark(m)
		}
	})
}

// JoinBattle 是 lssproto_JB_recv → BATTLE_RescueTry。
func (w *World) JoinBattle(charID int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || e.trade != nil {
			return
		}
		if e.fight != nil {
			w.sysMsg(e, " 重复加入战斗! ", colorRed)
			return
		}
		switch e.floor {
		case 1007, 2007, 3007, 4007, 130, 20000: // 只能观战的地方
			w.sysMsg(e, "无法观战。", colorYellow)
			return
		}
		if e.party != nil {
			w.sysMsg(e, "无法观战。", colorYellow) // 组队时参战改成观战
			return
		}
		fx, fy := e.x+MOVE[e.dir][0], e.y+MOVE[e.dir][1]
		var cands []*entity
		found := false
		for _, o := range w.floors[e.floor] {
			if o.kind != "player" || o == e || o.x != fx || o.y != fy || o.fight == nil {
				continue
			}
			found = true
			if o.fight.room.help && !o.fight.room.pvp && !containsEntity(cands, o) {
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
			w.rescueEntry(e, cands[0])
		default:
			var sb strings.Builder
			sb.WriteString("1\n加入谁的战斗呢？\n")
			ids := make([]int, len(cands))
			for i, c := range cands {
				sb.WriteString(c.name + "\n")
				ids[i] = c.id
			}
			e.window = &npcWindow{npc: 0, seq: battleSelectSeq, kind: "select", buttons: BtnCancel, ids: ids}
			e.client.Send(&proto.NpcWindow{ID: 0, Kind: "select", Buttons: BtnCancel, Seq: battleSelectSeq, Text: sb.String()})
		}
	})
}

// joinBattleReply 是 CHAR_JoinBattle_WindowResult：再检查一次（没组队、对方开着求救、在身边）。
func (w *World) joinBattleReply(e *entity, win *npcWindow, button int, arg string) {
	n := data.Atoi(arg) - 1
	if button != 0 || n < 0 || n >= len(win.ids) || e.party != nil || e.fight != nil {
		return
	}
	if o := w.players[win.ids[n]]; o != nil && o.fight != nil && o.fight.room.help && chebyshev(e, o) <= 1 {
		w.rescueEntry(e, o)
	}
}

// rescueEntry 是 BATTLE_RescueEntry：站进对方那一边的第一个空位。
func (w *World) rescueEntry(e, target *entity) {
	room := target.fight.room
	slot := -1
	for i := 0; i < partyMax; i++ {
		if room.b.F[i] == nil {
			slot = i
			break
		}
	}
	if slot < 0 || len(room.members) == 0 {
		w.sysMsg(e, "无法参战。", colorYellow)
		return
	}
	w.joinRoom(room, e, slot)
	w.sendBattleStart(e, room.field, 0)
	e.client.Send(&proto.HelpFlag{On: room.help})
	join := &proto.BattleJoin{}
	for _, s := range []int{slot, slot + 5} {
		if f := room.b.F[s]; f != nil {
			join.Fighters = append(join.Fighters, w.fighterInfo(room, f))
		}
	}
	for _, m := range room.members {
		if m != e {
			m.client.Send(join)
		}
	}
	w.broadcastMark(e)
}
