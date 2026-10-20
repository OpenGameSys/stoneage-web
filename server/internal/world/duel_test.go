package world

import (
	"strings"
	"testing"

	"sa/server/internal/battle"
	"sa/server/internal/proto"
)

// 决斗：对方没开"决斗"不行；开了就两边各在一边、没有求救；打倒对方：对方扣 10% DP、自己加；打完"决斗"开关关掉。
func TestDuel(t *testing.T) {
	w, db, _ := setup(t)
	ca, cla := petOwner(t, w, db, "duelist", 10, 60)
	cb, clb := petOwner(t, w, db, "target", 10, 60)
	a, b := int(ca.ID), int(cb.ID)
	w.Do(func() {
		ea, eb := w.players[a], w.players[b]
		eb.x, eb.y = ea.x+MOVE[ea.dir][0], ea.y+MOVE[ea.dir][1]
		ea.char.DP, eb.char.DP = 1000, 2000
		ea.pets.Battle, eb.pets.Battle = -1, -1
	})
	w.Duel(a)
	if !strings.Contains(fmtMsgs(cla.take()), "无人可以对战。") {
		t.Fatal("对方没开决斗应不行")
	}
	w.SetFlags(b, fsParty|fsDuel)
	w.Duel(a)
	w.Do(func() {
		ea, eb := w.players[a], w.players[b]
		if ea.fight == nil || eb.fight == nil || !ea.fight.room.pvp || eb.fight.slot != battle.SideOffset || !ea.fight.b.PvP {
			t.Errorf("应开打 PvP：%+v %+v", ea.fight, eb.fight)
			return
		}
		room := ea.fight.room
		// 打倒对方
		f := room.b.F[eb.fight.slot]
		f.HP, f.KilledBy = 0, ea.fight.slot
		w.duelDeaths(room)
		if ea.fight.dp != 200 || eb.fight.dp != -200 {
			t.Errorf("对方扣 10%%、自己加一样多：%d %d", ea.fight.dp, eb.fight.dp)
		}
		w.endBattle(room, 0)
		if ea.char.DP != 1200 || eb.char.DP != 1800 || eb.fs&fsDuel != 0 {
			t.Errorf("DP 应结算、决斗开关关掉：%d %d %d", ea.char.DP, eb.char.DP, eb.fs)
		}
	})
	if m, ok := find[*proto.DuelResult](cla.take()); !ok || m.Delta != 200 || m.DP != 1200 {
		t.Errorf("应送 DP 结果：%+v", m)
	}
	if m, ok := find[*proto.BattleEnd](clb.take()); !ok || m.Result != "lose" {
		t.Errorf("对方应是输：%+v", m)
	}
}
