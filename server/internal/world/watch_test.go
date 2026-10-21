package world

import (
	"testing"

	"sa/server/internal/battle"
	"sa/server/internal/proto"
)

// 观战：面向正在战斗的人按"参战"而加不进去（没开求救）就观战：收到 watch 的 BattleStart、每回合的 BattleTurn，
// 观战中不能走；"离开"或那场打完收到 result "watch" 的 BattleEnd。
func TestWatchBattle(t *testing.T) {
	w, db, g := setup(t)
	a := newChar(t, db, "fighter", 1000, 47, 82)
	b := newChar(t, db, "watcher", 1000, 48, 82)
	cla, clb := &fakeClient{}, &fakeClient{}
	w.Enter(a, cla)
	w.Enter(b, clb)
	ia, ib := int(a.ID), int(b.ID)
	w.Do(func() {
		ea, eb := w.players[ia], w.players[ib]
		for d := range MOVE { // B 面向 A
			if eb.x+MOVE[d][0] == ea.x && eb.y+MOVE[d][1] == ea.y {
				eb.dir = d
			}
		}
		en := enemyFighter(w, petEnemy(t, g, 1), 1, battle.EnemySlot(0))
		en.HP, en.MaxHP, en.Atk = 100000, 100000, 1 // 别被一下打死、也打不死人
		w.startBattleWith(ea, []*battle.Fighter{en}, nil, nil)
		ea.fight.b.F[0].HP, ea.fight.b.F[0].MaxHP = 100000, 100000
	})
	cla.take()
	clb.take()
	w.JoinBattle(ib)
	bs, ok := find[*proto.BattleStart](clb.take())
	if !ok || !bs.Watch || bs.MySlot != -1 || len(bs.Fighters) < 2 {
		t.Fatalf("没开求救应改成观战：%+v", bs)
	}
	w.Move(ib, 49, 82, 6)
	if _, ok := find[*proto.PosCorrect](clb.take()); !ok {
		t.Error("观战中不能走")
	}
	w.BattleCommand(ia, "attack", battle.EnemySlot(0), -1)
	if _, ok := find[*proto.BattleTurn](clb.take()); !ok {
		t.Error("观战者应收到每回合的结果")
	}
	w.BattleCommand(ib, "leave", -1, -1)
	if end, ok := find[*proto.BattleEnd](clb.take()); !ok || end.Result != "watch" {
		t.Fatalf("离开观战应收到 result watch 的 BattleEnd：%+v", end)
	}
	w.Do(func() {
		if w.players[ib].watch != nil || len(w.players[ia].fight.room.watchers) != 0 {
			t.Error("离开后不该还在观战名单里")
		}
	})
	// 再看一次，这次等那场打完
	w.JoinBattle(ib)
	if _, ok := find[*proto.BattleStart](clb.take()); !ok {
		t.Fatal("应能再观战")
	}
	w.Do(func() { w.endBattle(w.players[ia].fight.room, 0) })
	if end, ok := find[*proto.BattleEnd](clb.take()); !ok || end.Result != "watch" {
		t.Fatalf("那场打完观战者应回地图：%+v", end)
	}
	w.Do(func() {
		if w.players[ib].watch != nil {
			t.Error("打完后 watch 应清掉")
		}
	})
}
