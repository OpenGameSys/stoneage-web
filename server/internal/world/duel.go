package world

import (
	"context"
	"fmt"
	"strings"
	"time"

	"sa/server/internal/battle"
	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 决斗（callfromcli.c lssproto_DU_recv、battle.c BATTLE_CreateVsPlayer / BATTLE_AddDuelPoint / BATTLE_EscapeDpSend）：
//
//	挑战      面前那一格开着"决斗"、没在战斗的玩家（他是队员时找他的队长）；自己是队员不行。12345、140 号地图"该地图禁止私下PK！"；
//	          几个人时开选单"要和谁战斗？"（"名字 [团体|单独]"）。两边的队伍都进来（我方 0..4、对方 10..14，宠物 +5），不能求救
//	规则      逃跑一定成功、捕获不了、复活不能用在玩家身上（battle.Battle.PvP）；没有经验、石币、道具，改算 DP
//	DP        有玩家倒下：扣他 DP 的 10%，打倒他的那边那个人加上同样多（自己人打倒的就由对方还站着的人平分）；
//	          逃跑：第一回合还没人倒下时不扣，否则扣 20%、对方还站着的人平分 10%；结束时 DP 增减至少 1，范围 0..1 亿（RD 字串）
//	之后      "决斗"开关关掉（每次都要再开）；在庄园竞技场（1042、2032、3032、4032）打过的 120 秒内不能 PK
//	强者表    Duelranking NPC：看前十名或自己附近的名次，每页 10 人（原版在 AC 的 db_duel，这里直接查资料库）
//
// 观战在 watch.go。没做：胜败连胜的计数（DUELBATTLECOUNT 等，强者表也没用到）、乱舞 PK（_AUTO_PK 的 NPC 没有摆）。

const (
	duelSelectSeq = 23  // CHAR_WINDOWTYPE_SELECTDUEL
	duelRankStart = 250 // CHAR_WINDOWTYPE_DUELRANKING_START
	duelRankList  = 251 // CHAR_WINDOWTYPE_DUELRANKING_TOPRANKING
	maxDuelPoint  = 100_000_000
	duelCoolDown  = 120 * time.Second // _BATTLE_OVER_STAR_TIME
)

// Duel 是 lssproto_DU_recv。
func (w *World) Duel(charID int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || e.fight != nil || e.isFollower() || e.trade != nil {
			return
		}
		fx, fy := e.x+MOVE[e.dir][0], e.y+MOVE[e.dir][1]
		var cands []*entity
		found := false
		for _, o := range w.floors[e.floor] {
			if o == e || o.kind != "player" || o.x != fx || o.y != fy {
				continue
			}
			found = true
			if o.fight != nil {
				continue
			}
			if e.floor == 12345 || e.floor == 140 { // freevsplayer.lua 的 nopkfloor
				w.sysMsg(e, "该地图禁止私下PK！", colorYellow)
				return
			}
			if o.fs&fsDuel == 0 && !w.warOpponents(e, o) { // 家族对战场上开打中的两边不用开决斗开关（NPC_FMBATTLESET 的 CHAR_ISDUEL）
				continue
			}
			cands = append(cands, o)
		}
		switch len(cands) {
		case 0:
			if found {
				w.sysMsg(e, "无人可以对战。", colorYellow)
			} else {
				w.sysMsg(e, "那里没有任何人。", colorYellow)
			}
		case 1:
			w.startDuel(e, partyHead(cands[0]))
		default:
			var sb strings.Builder
			sb.WriteString("1\n要和谁战斗？\n")
			ids := make([]int, len(cands))
			for i, c := range cands {
				kind := "单独"
				if c.party != nil {
					kind = "团体"
				}
				fmt.Fprintf(&sb, "%s [%s]\n", c.name, kind)
				ids[i] = c.id
			}
			e.window = &npcWindow{npc: 0, seq: duelSelectSeq, kind: "select", buttons: BtnCancel, ids: ids}
			e.client.Send(&proto.NpcWindow{ID: 0, Kind: "select", Buttons: BtnCancel, Seq: duelSelectSeq, Text: sb.String()})
		}
	})
}

// partyHead 是 e 的队长（没组队就是自己）。
func partyHead(e *entity) *entity {
	if e.party != nil {
		return e.party.leader()
	}
	return e
}

// duelSelectReply 是 CHAR_JoinDuel_WindowResult：再检查一次（没在战斗、开着决斗、在身边）。
func (w *World) duelSelectReply(e *entity, win *npcWindow, button int, arg string) {
	n := data.Atoi(arg) - 1
	if button != 0 || n < 0 || n >= len(win.ids) || e.isFollower() {
		return
	}
	if o := w.players[win.ids[n]]; o != nil && o.fight == nil && o.fs&fsDuel != 0 && chebyshev(e, o) <= 1 {
		if !w.startDuel(e, partyHead(o)) {
			w.sysMsg(e, "遭遇失败！", colorYellow)
		}
	}
}

// startDuel 是 BATTLE_CreateVsPlayer：c0 的队伍在 0 边、c1 的队伍在 1 边。
func (w *World) startDuel(c0, c1 *entity) bool {
	if c0.fight != nil || c1.fight != nil {
		w.sysMsg(c0, "二重遭遇。", colorYellow)
		w.sysMsg(c1, "二重遭遇。", colorYellow)
		return false
	}
	now := w.now()
	if now.Before(c0.duelCool) {
		w.sysMsg(c0, fmt.Sprintf("你当前需冷却%d后才能进行PK", int(c0.duelCool.Sub(now).Seconds())+1), colorYellow)
		return false
	}
	if now.Before(c1.duelCool) {
		left := int(c1.duelCool.Sub(now).Seconds()) + 1
		w.sysMsg(c0, fmt.Sprintf("对方需冷却%d后才能进行PK", left), colorYellow)
		w.sysMsg(c1, fmt.Sprintf("你当前需冷却%d后才能进行PK", left), colorYellow)
		return false
	}
	if c0.party != nil && c0.party == c1.party {
		return false // BATTLE_ERR_SAMEPARTY
	}
	b := battle.New(w.rnd)
	b.PvP = true
	w.battleSeq++
	room := &battleRoom{b: b, started: now, no: w.battleSeq, pvp: true}
	for i, m := range w.battleMembers(c0) {
		w.joinRoom(room, m, i)
	}
	for i, m := range w.battleMembers(c1) {
		w.joinRoom(room, m, battle.SideOffset+i)
	}
	b.CanCapture = func(a, d *battle.Fighter) bool { return false }
	b.OnHit = func(a, d *battle.Fighter, dmg int) {
		for _, m := range room.members {
			if m.fight != nil {
				w.battleCrush(m, m.fight, a, d, dmg)
			}
		}
	}
	b.AttMagics, b.Magics = w.game.Battle.AttMagics, w.game.Battle.Magics
	b.Notice = func(slot int, text string) {
		if m := room.member(slot); m != nil && slot == m.fight.slot {
			w.sysMsg(m, text, colorRed)
		}
	}
	room.field = w.battleField(c0) // 挑战的人脚下的背景
	for _, m := range room.members {
		w.sendBattleStart(m, room.field, 0)
		w.broadcastMark(m)
	}
	w.armTurnTimer(room, 0)
	return true
}

// duelDeaths 是 BATTLE_AddDuelPoint：这一回合新倒下的玩家扣 DP、打倒他的人加。
func (w *World) duelDeaths(room *battleRoom) {
	for _, m := range room.members {
		ft := m.fight
		f := room.b.F[ft.slot]
		if ft.dead || f.HP > 0 || f.Escaped {
			continue
		}
		ft.dead, room.charaLost = true, true
		lose := m.char.DP / 10 // DUELPOINT_RATE
		ft.dp -= lose
		killer := room.member(f.KilledBy)
		if killer != nil && killer.fight.slot/battle.SideOffset != ft.slot/battle.SideOffset {
			killer.fight.dp += max(1, lose)
			continue
		}
		// 自己人打倒的（或不知道是谁）：对方还站着的人平分
		var alive []*entity
		for _, o := range room.members {
			if o.fight.slot/battle.SideOffset != ft.slot/battle.SideOffset && room.b.F[o.fight.slot].Alive() {
				alive = append(alive, o)
			}
		}
		for _, o := range alive {
			o.fight.dp += max(1, lose/len(alive))
		}
	}
}

// duelEscape 是 BATTLE_EscapeDpSend：第一回合还没人倒下时逃跑不扣；否则扣 20%，对方还站着的人平分 10%。
func (w *World) duelEscape(room *battleRoom, m *entity) {
	ft := m.fight
	if room.turn <= 1 && !room.charaLost {
		ft.dp = 0
		return
	}
	room.charaLost = true
	var alive []*entity
	for _, o := range room.members {
		if o.fight.slot/battle.SideOffset != ft.slot/battle.SideOffset && room.b.F[o.fight.slot].Alive() {
			alive = append(alive, o)
		}
	}
	if len(alive) == 0 {
		return
	}
	lose := max(1, m.char.DP/10)
	ft.dp = -2 * lose
	for _, o := range alive {
		o.fight.dp += max(1, lose/len(alive))
	}
}

// duelResult 是 BATTLE_GetDuelPoint：DP 加上这场的增减（至少 ±1），送 RD；"决斗"开关关掉（battle.c _BATTLE_Exit）。
func (w *World) duelResult(e *entity, ft *fight) {
	d := ft.dp
	switch {
	case d < 0:
		d = min(-1, d)
	case d > 0:
		d = max(1, d)
	}
	e.char.DP = max(0, min(maxDuelPoint, e.char.DP+d))
	e.client.Send(&proto.DuelResult{Delta: d, DP: e.char.DP})
	e.fs &^= fsDuel
	e.client.Send(&proto.Flags{Flags: e.fs})
	if isFamilyWarFloor(e.floor) { // _BATTLE_OVER_STAR_TIME：庄园竞技场打完要冷却
		e.duelCool = w.now().Add(duelCoolDown)
	}
}

// ---------------- 强者表 ----------------

// duelRankingOpen 是 NPC_DuelrankingLooked：要看哪一个？
func (w *World) duelRankingOpen(p, n *entity) {
	if chebyshev(p, n) > 1 {
		return
	}
	text := "1\n             要看哪一个？              \n\n\n             ＜强者们＞              \n\n             ＜ 自己 ＞              \n\n             ＜都不看＞              \n\n"
	w.sendWindow(p, n, "select", 0, duelRankStart, text)
}

// duelRankingReply 处理强者表的两种窗口。
func (w *World) duelRankingReply(p, n *entity, seq, button int, sel string) {
	if chebyshev(p, n) > 1 || w.dbBusy(p) {
		return
	}
	switch seq {
	case duelRankStart:
		switch data.Atoi(sel) {
		case 3:
			w.duelRankingPage(p, n, 0)
		case 5:
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if pos, err := w.db.DuelRank(ctx, p.char.ID); err == nil {
				w.duelRankingPage(p, n, max(0, pos-5))
			}
		}
	case duelRankList:
		start := p.rankStart
		switch {
		case button&BtnNext != 0:
			w.duelRankingPage(p, n, start+10)
		case button&BtnPrev != 0:
			w.duelRankingPage(p, n, max(0, start-10))
		}
	}
}

// duelRankingPage 是 NPC_Duelranking_PrintRanking（自己那一行前面有 ★）。
func (w *World) duelRankingPage(p, n *entity, start int) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, err := w.db.DuelRanking(ctx, start, 10)
	if err != nil {
		return
	}
	p.rankStart = start
	if len(rows) == 0 {
		p.rankStart = -1
		w.sendWindow(p, n, "widemessage", BtnPrev|BtnOK, duelRankList, "\n      没有其他的资料了。")
		return
	}
	var sb strings.Builder
	sb.WriteString("                        世界的强者们\n\n      Rank       DuelPoint      Lv     Name\n\n")
	suffix := []string{"st", "nd", "rd", "th"}
	for _, r := range rows {
		mark := "　"
		if r.ID == p.char.ID {
			mark = "★"
		}
		fmt.Fprintf(&sb, "%s %5d%s      %10d     %3d     %s\n", mark, r.Rank+1, suffix[min(r.Rank, 3)], r.DP, r.Level, r.Name)
	}
	buttons := BtnOK
	switch {
	case len(rows) < 10:
		if start >= 10 {
			buttons |= BtnPrev
		}
	case start == 0:
		buttons |= BtnNext
	default:
		buttons |= BtnNext | BtnPrev
	}
	w.sendWindow(p, n, "widemessage", buttons, duelRankList, sb.String())
}
