package world

import (
	"strings"
	"testing"

	"sa/server/internal/battle"
	"sa/server/internal/proto"
)

// 组队：面向队长加入、面向队员也能加入（要在队长身边）；队长走一步队员跟上、队员不能自己走；
// 组队战斗队长在 0、队员依序；队长踢人；队长离开就解散。
func TestParty(t *testing.T) {
	w, db, g := setup(t)
	ca, cla := petOwner(t, w, db, "leader", 10, 60)
	cb, clb := petOwner(t, w, db, "member1", 10, 60)
	cc, clc := petOwner(t, w, db, "member2", 10, 60)
	a, b, c := int(ca.ID), int(cb.ID), int(cc.ID)
	var walkDir int
	w.Do(func() {
		ea, eb, ec := w.players[a], w.players[b], w.players[c]
		f := w.game.Floors[ea.floor]
		walkDir = -1
		for d := 0; d < 8 && walkDir < 0; d++ {
			if w.game.Tiles.Walkable(f, ea.x+MOVE[d][0], ea.y+MOVE[d][1]) {
				walkDir = d
			}
		}
		// B 在 A 后面面向 A；C 在 A 旁边面向 B（B 是队员，要找到 A，C 也在 A 身边）
		back := (walkDir + 4) % 8
		eb.x, eb.y, eb.dir = ea.x+MOVE[back][0], ea.y+MOVE[back][1], walkDir
		side := (back + 1) % 8
		ec.x, ec.y = ea.x+MOVE[side][0], ea.y+MOVE[side][1]
		ec.dir = moveDir(sign(eb.x-ec.x), sign(eb.y-ec.y))
	})
	if walkDir < 0 {
		t.Skip("出生点四周走不通")
	}
	w.PartyRequest(b, 1)
	w.PartyRequest(c, 1)
	w.Do(func() {
		ea, eb, ec := w.players[a], w.players[b], w.players[c]
		if !ea.isLeader() || eb.party != ea.party || ec.party != ea.party || ea.party.slots[1] != eb || ea.party.slots[2] != ec {
			t.Errorf("A 应是队长、B 在 1、C 在 2：%+v %v %v", ea.party, eb.party, ec.party)
		}
	})
	if !strings.Contains(fmtMsgs(clb.take()), "加入团队！") || !strings.Contains(fmtMsgs(cla.take()), "member2 加入团队！") {
		t.Error("加入的讯息不对")
	}
	clc.take()

	// 队员不能自己走；队长走一步，B 走到 A 原来的位置
	var ax, ay int
	w.Do(func() { ax, ay = w.players[a].x, w.players[a].y })
	w.Move(b, 0, 0, walkDir)
	if _, ok := find[*proto.PosCorrect](clb.take()); !ok {
		t.Error("队员自己走应被拉回")
	}
	w.Do(func() { w.players[a].stepTokens = StepBurst })
	w.Move(a, ax, ay, walkDir)
	w.Do(func() {
		if eb := w.players[b]; eb.x != ax || eb.y != ay {
			t.Errorf("B 应走到 A 原来的位置 (%d,%d)：(%d,%d)", ax, ay, eb.x, eb.y)
		}
	})

	// 组队战斗
	w.Do(func() {
		ea := w.players[a]
		en := enemyFighter(w, petEnemy(t, g, 1), 1, battle.EnemySlot(0))
		w.startBattleWith(ea, []*battle.Fighter{en}, nil, nil)
		room := ea.fight.room
		if len(room.members) != 3 || w.players[b].fight.slot != 1 || w.players[c].fight.slot != 2 || room.b.F[1] == nil {
			t.Errorf("队员应在 1、2：%d 人", len(room.members)); return
		}
		w.endBattle(room, 0)
		if ea.fight != nil || w.players[b].fight != nil || ea.party == nil {
			t.Error("打完应都离开战斗、队伍还在")
		}
	})

	w.PartyKick(a, 2)
	w.Do(func() {
		if w.players[c].party != nil || w.players[a].party.slots[2] != nil {
			t.Error("C 应被踢出")
		}
	})
	if !strings.Contains(fmtMsgs(clc.take()), "队长[leader]将你踢除！") {
		t.Error("被踢的人应看到讯息")
	}
	w.PartyRequest(a, 0)
	w.Do(func() {
		if w.players[a].party != nil || w.players[b].party != nil {
			t.Error("队长离开应解散")
		}
	})
	if !strings.Contains(fmtMsgs(clb.take()), "团队已解散！") {
		t.Error("队员应看到团队已解散")
	}
}
