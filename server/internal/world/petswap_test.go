package world

import (
	"testing"

	"sa/server/internal/battle"
	"sa/server/internal/proto"
)

// 战斗中换宠物（"S|n"，BATTLE_PetIn / PetOut）：收回在场的宠物、叫出另一只，新宠物下回合才行动；"S|-1" 只收回；倒下的叫不出来。
func TestPetSwap(t *testing.T) {
	w, db, g := setup(t)
	ene := petEnemy(t, g, 1)
	c, cl := petOwner(t, w, db, "swapper", 20, 100)
	id := int(c.ID)
	a := givePet(t, w, id, ene, 10)
	b := givePet(t, w, id, ene, 12)
	dead := givePet(t, w, id, ene, 5)
	w.PetState(id, a, "battle")
	w.Do(func() {
		e := w.players[id]
		for _, p := range e.pets.Pets {
			if p != nil {
				p.VariableAI = 100 * 100
			}
		}
		e.pets.Pets[dead].HP = 0
		en := enemyFighter(w, ene, 1, battle.EnemySlot(0))
		en.HP, en.MaxHP, en.Tactics = 9999, 9999, "gu:1"
		w.startBattleWith(e, []*battle.Fighter{en}, nil, nil)
		e.fight.b.F[5].HP = 3 // 收回时 HP 要写回
	})
	cl.take()
	turn := func() *proto.BattleTurn {
		t.Helper()
		tu, ok := find[*proto.BattleTurn](cl.take())
		if !ok {
			t.Fatal("应结算")
		}
		return tu
	}
	w.BattleCommand(id, "pet", -1, b)
	w.BattlePetCommand(id, -1, -1)
	tu := turn()
	var in, out *proto.BattleEvent
	for i, ev := range tu.Events {
		switch ev.Kind {
		case "petin":
			in = &tu.Events[i]
		case "petout":
			out = &tu.Events[i]
		case "none", "guard", "attack":
			if ev.Actor == 5 && out != nil {
				t.Error("新叫出的宠物这回合不应行动")
			}
		}
	}
	if in == nil || out == nil || out.Target != 5 || out.Level != 12 || out.Sprite == 0 || out.MaxHp <= 0 {
		t.Fatalf("应收回再叫出：%+v %+v", in, out)
	}
	w.Do(func() {
		e := w.players[id]
		if e.fight.pet != b || e.pets.Battle != b || e.fight.b.F[5].Level != 12 {
			t.Errorf("出战的宠物应换成第 %d 格：%d %d", b, e.fight.pet, e.pets.Battle)
		}
		if e.pets.Pets[a].HP != 3 {
			t.Errorf("收回的宠物 HP 应写回：%d", e.pets.Pets[a].HP)
		}
	})
	// 新宠物下回合要下指令才结算
	w.BattleCommand(id, "guard", -1, -1)
	if _, ok := find[*proto.BattleTurn](cl.take()); ok {
		t.Fatal("新宠物还没下指令，不应结算")
	}
	w.BattlePetCommand(id, -1, -1)
	turn()
	// 倒下的宠物叫不出来：只收回
	w.BattleCommand(id, "pet", -1, dead)
	w.BattlePetCommand(id, -1, -1)
	tu = turn()
	for _, ev := range tu.Events {
		if ev.Kind == "petout" {
			t.Error("倒下的宠物不应叫出来")
		}
	}
	w.Do(func() {
		e := w.players[id]
		if e.fight.pet != -1 || e.pets.Battle != -1 || e.fight.b.F[5] != nil {
			t.Errorf("应只收回：%d %d", e.fight.pet, e.pets.Battle)
		}
	})
	// 没有宠物时只等人的指令
	w.BattleCommand(id, "guard", -1, -1)
	turn()
}
