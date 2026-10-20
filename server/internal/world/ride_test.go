package world

import (
	"strings"
	"testing"

	"sa/server/internal/battle"
	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// petByTemp 找模板编号为 tempNo 的可捕获敌人。
func petByTemp(t *testing.T, g *data.Game, tempNo int) *data.Enemy {
	return petEnemy(t, g, tempNo)
}

// 格鲁西斯（红虎 100329）：形象 100000 的人物不必证书就能骑，骑乘形象 101004（CommonRideNoList[0][0]）。
func TestRideTiger(t *testing.T) {
	w, db, g := setup(t)
	tiger := petByTemp(t, g, 193)
	c, cl := petOwner(t, w, db, "rider", 20, 100)
	id := int(c.ID)
	slot := givePet(t, w, id, tiger, 1)
	cl.take()

	// 忠诚度不到 100 不能骑
	w.Do(func() { w.players[id].pets.Pets[slot].VariableAI = -100 * 100 })
	w.PetRide(id, slot)
	if m, _ := find[*proto.ChatMsg](cl.take()); m == nil || !strings.Contains(m.Text, "忠小于100") {
		t.Fatalf("忠诚度不够应拒绝：%+v", m)
	}
	w.Do(func() { w.players[id].pets.Pets[slot].VariableAI = 100 * 100 })
	w.PetRide(id, slot)
	msgs := cl.take()
	p, _ := find[*proto.Pets](msgs)
	sp, _ := find[*proto.ObjSprite](msgs)
	if p == nil || p.Ride != slot || p.Pets[0].State != "ride" || !p.Pets[0].Rideable || sp == nil || sp.Sprite != 101004 {
		t.Fatalf("应骑上老虎、形象 101004：%+v %+v", p, sp)
	}

	// 骑着打仗：骑宠并进人物，挨打时分摊伤害
	var f *battle.Fighter
	w.Do(func() {
		e := w.players[id]
		en := enemyFighter(w, tiger, 1, battle.EnemySlot(0))
		w.startBattleWith(e, []*battle.Fighter{en}, nil, nil)
		f = e.fight.b.F[0]
	})
	st, _ := find[*proto.BattleStart](cl.take())
	if f.Ride == nil || st == nil || st.Fighters[0].Sprite != 101004 || st.Fighters[0].RideMaxHp <= 0 || st.Fighters[0].FallSprite != 100000 {
		t.Fatalf("战斗中应骑着：%+v", st)
	}
	split := false
	for turn := 0; turn < 40 && !split; turn++ {
		w.BattleCommand(id, "guard", -1, -1)
		msgs := cl.take()
		if tu, ok := find[*proto.BattleTurn](msgs); ok {
			for _, ev := range tu.Events {
				split = split || ev.Target == 0 && ev.PetDamage > 0
			}
		}
		if _, ok := find[*proto.BattleEnd](msgs); ok {
			break
		}
	}
	if !split {
		t.Error("骑着挨打应由骑宠分摊伤害")
	}
}

// 落马：骑宠 HP 到 0 就退出战斗，人换回原来的形象，战斗结束后下来。
func TestRideFall(t *testing.T) {
	w, db, g := setup(t)
	tiger := petByTemp(t, g, 193)
	c, cl := petOwner(t, w, db, "faller", 20, 100)
	id := int(c.ID)
	slot := givePet(t, w, id, tiger, 1)
	w.Do(func() { w.players[id].pets.Pets[slot].VariableAI = 100 * 100 })
	w.PetRide(id, slot)
	w.Do(func() {
		e := w.players[id]
		e.pets.Pets[slot].HP = 1
		en := enemyFighter(w, tiger, 30, battle.EnemySlot(0))
		en.Atk, en.FixAtk = 500, 500
		w.startBattleWith(e, []*battle.Fighter{en}, nil, nil)
	})
	cl.take()
	fell := false
	for turn := 0; turn < 20 && !fell; turn++ {
		w.BattleCommand(id, "guard", -1, -1)
		if tu, ok := find[*proto.BattleTurn](cl.take()); ok {
			for _, ev := range tu.Events {
				fell = fell || ev.Fall
			}
		}
	}
	if !fell {
		t.Fatal("骑宠 HP 1 应很快落马")
	}
	w.Do(func() {
		e := w.players[id]
		if e.fight != nil {
			w.abandonBattle(e)
		}
		if e.pets.Ride != -1 || e.sprite != w.weaponSprite(e) || e.pets.Pets[slot].HP != 1 {
			t.Errorf("落马后应下来：ride %d sprite %d hp %d", e.pets.Ride, e.sprite, e.pets.Pets[slot].HP)
		}
	})
}

// 骑宠证书（ITEM_useLearnRideCode）与证书骑宠：帖拉所伊朵（机暴 100374）要 RIDE_PET0 才能骑。
func TestRideLicence(t *testing.T) {
	w, db, g := setup(t)
	robo := petByTemp(t, g, 304)
	c, cl := petOwner(t, w, db, "licensee", 20, 100)
	id := int(c.ID)
	slot := givePet(t, w, id, robo, 1)
	w.Do(func() { w.players[id].pets.Pets[slot].VariableAI = 100 * 100 })
	w.PetRide(id, slot)
	if p, _ := find[*proto.Pets](lastOf(cl.take())); p != nil && p.Ride == slot {
		t.Fatal("没有证书不能骑机暴")
	}
	book := 0
	for itemID, d := range g.Items {
		if d.UseFunc == "ITEM_useLearnRideCode" && strings.HasPrefix(d.Argument, "RIDE_PET0|") {
			book = itemID
		}
	}
	if book == 0 {
		t.Skip("没有 RIDE_PET0 的证书")
	}
	giveItem(w, id, book)
	var bag int
	w.Do(func() {
		for i, it := range w.players[id].inv {
			if it != nil && it.ID == book {
				bag = i
			}
		}
	})
	w.ItemUse(id, bag, 0)
	if m, _ := find[*proto.ChatMsg](cl.take()); m == nil || !strings.Contains(m.Text, "学习了新的骑宠") {
		t.Fatalf("用了证书应学会：%+v", m)
	}
	w.PetRide(id, slot)
	if p, _ := find[*proto.Pets](lastOf(cl.take())); p == nil || p.Ride != slot {
		t.Fatalf("有证书应能骑：%+v", p)
	}
	w.Do(func() {
		if s := w.players[id].sprite; s != 101305 {
			t.Errorf("形象 100000 骑机暴是 101305，实际 %d", s)
		}
	})
}

// 骑乘训练师：特级班要先学过高级班（新角色是 140，已经超过），付 20000 石币后能骑所有等级。
func TestRiderman(t *testing.T) {
	w, db, _ := setup(t)
	n := findNPC(t, w, "Riderman", "riderman.conf")
	c, cl := besideNPC(t, w, db, "student", n)
	id := int(c.ID)
	w.Do(func() { w.players[id].char.Gold = 30000 })
	w.Talk(id, n.id)
	win, _ := find[*proto.NpcWindow](cl.take())
	if win == nil || !strings.Contains(win.Text, "骑乘训练师") {
		t.Fatalf("应打开训练师选单：%+v", win)
	}
	w.NpcWindowReply(id, n.id, win.Seq, 0, "4") // 学习骑乘宠物
	win, _ = find[*proto.NpcWindow](cl.take())
	w.NpcWindowReply(id, n.id, win.Seq, 0, "4") // 特级班
	win, _ = find[*proto.NpcWindow](cl.take())
	if win == nil || !strings.Contains(win.Text, "20000") {
		t.Fatalf("应问特级班学费：%+v", win)
	}
	w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
	win, _ = find[*proto.NpcWindow](cl.take())
	if win == nil || !strings.Contains(win.Text, "完成骑乘特级班") {
		t.Fatalf("应学会特级班：%+v", win)
	}
	w.Do(func() {
		e := w.players[id]
		if learnRide(e) != 200 || e.char.Gold != 10000 {
			t.Errorf("骑乘等级 %d、石币 %d", learnRide(e), e.char.Gold)
		}
	})
}
