package world

import (
	"context"
	"strings"
	"testing"

	"sa/server/internal/battle"
	"sa/server/internal/data"
	"sa/server/internal/proto"
	"sa/server/internal/store"
)

// petEnemy 找一种可以捕获、模板编号为 tempNo 的敌人（乌力是 1）。
func petEnemy(t *testing.T, g *data.Game, tempNo int) *data.Enemy {
	t.Helper()
	for _, e := range g.Battle.Enemies {
		if e.TempNo == tempNo && e.PetFlag != 0 {
			return e
		}
	}
	t.Skipf("没有模板 %d 的可捕获敌人", tempNo)
	return nil
}

func enemyFighter(w *World, e *data.Enemy, level, slot int) *battle.Fighter {
	s := battle.EnemySpec{Enemy: e, Temp: w.game.Battle.Temps[e.TempNo], Level: level}
	return battle.NewEnemy(s, slot, w.game.Battle.Skills, w.rnd)
}

// petOwner 建一个在村子里的角色并上线。
func petOwner(t *testing.T, w *World, db store.Store, name string, level, charm int) (store.Character, *fakeClient) {
	t.Helper()
	acc, _ := db.CreateAccount(context.Background(), name, "pw", "")
	c, err := db.CreateCharacter(context.Background(), store.Character{AccountID: acc.ID, Name: name, Sprite: 100000, Level: level,
		Floor: 1000, X: 47, Y: 82, Vital: 20, Str: 20, Tgh: 10, Dex: 10, Earth: 10, Charm: charm})
	if err != nil {
		t.Fatal(err)
	}
	cl := &fakeClient{}
	w.Enter(c, cl)
	cl.take()
	return c, cl
}

// givePet 直接把一只宠物放进宠物栏（照捕获的方式产生）。
func givePet(t *testing.T, w *World, charID int, e *data.Enemy, level int) int {
	slot := -1
	w.Do(func() {
		p := w.players[charID]
		pet := petFromFighter(enemyFighter(w, e, level, 15), p.char.ID)
		slot = w.addPet(p, pet)
		w.petsChanged(p)
	})
	if slot < 0 {
		t.Fatal("宠物栏满了")
	}
	return slot
}

func TestCapture(t *testing.T) {
	w, db, g := setup(t)
	ene := petEnemy(t, g, 1)
	c, cl := petOwner(t, w, db, "catcher", 20, 100)
	id := int(c.ID)
	var target *battle.Fighter
	w.Do(func() {
		target = enemyFighter(w, ene, 1, battle.EnemySlot(0))
		target.HP = 1 // HP 越少越好抓
		w.startBattleWith(w.players[id], []*battle.Fighter{target}, nil, nil)
	})
	if _, ok := find[*proto.BattleStart](cl.take()); !ok {
		t.Fatal("应开始战斗")
	}
	var got *proto.Pets
	for turn := 0; turn < 30 && got == nil; turn++ {
		w.Do(func() {
			// 敌人不动、不会被打死，只看捕获
			w.players[id].fight.b.F[target.Slot].Tactics = "gu:1"
		})
		w.BattleCommand(id, "capture", target.Slot, -1)
		msgs := cl.take()
		tu, ok := find[*proto.BattleTurn](msgs)
		if !ok {
			t.Fatal("应收到 BattleTurn")
		}
		for _, ev := range tu.Events {
			if ev.Kind == "capture" && ev.OK {
				if p, ok := find[*proto.Pets](msgs); ok {
					got = p
				}
			}
		}
		if end, ok := find[*proto.BattleEnd](msgs); ok && got == nil {
			t.Fatalf("没抓到就结束了：%+v", end)
		}
	}
	if got == nil || len(got.Pets) != 1 {
		t.Fatalf("应抓到一只宠物：%+v", got)
	}
	pi := got.Pets[0]
	if pi.Name != target.Name || pi.Level != 1 || pi.HP != 1 || pi.Loyalty > battle.DefaultMaxAI || !pi.CanRename {
		t.Errorf("宠物应照抄敌人当时的数值、忠诚度最多 60：%+v", pi)
	}
	// 敌人被抓走了，战斗结束；宠物存进资料库
	w.Do(func() {
		if w.players[id].fight != nil {
			t.Error("唯一的敌人被抓走后战斗应结束")
		}
	})
	w.Leave(id, cl)
	flushSaves(w)
	saved, _ := db.GetCharacter(context.Background(), c.AccountID, c.ID)
	if b := parsePets(saved.Pets); b.Pets[0] == nil || b.Pets[0].TempNo != 1 || b.Pets[0].Owner != c.ID {
		t.Errorf("宠物应存档：%s", saved.Pets)
	}
}

func TestCaptureRules(t *testing.T) {
	w, db, g := setup(t)
	ene := petEnemy(t, g, 1)
	c, _ := petOwner(t, w, db, "rules", 1, 60)
	id := int(c.ID)
	w.Do(func() {
		p := w.players[id]
		a := w.playerFighter(p, 0)
		d := enemyFighter(w, ene, 7, 15)
		if battle.CaptureRate(a, d) != 0 {
			t.Error("比自己高 5 级以上的抓不到")
		}
		d = enemyFighter(w, ene, 1, 15)
		if battle.CaptureRate(a, d) <= 0 {
			t.Error("同等级的可以抓")
		}
		// 需要道具的敌人（NeedEnemy 按模板编号）
		d.Seed.TempNo = 524
		if w.canCapture(p, d) {
			t.Error("没带 2456 时抓不到模板 524")
		}
		w.addItem(p.inv, w.newItem(w.game.Items[2456]))
		if !w.canCapture(p, d) {
			t.Error("带了 2456 就可以抓")
		}
		// 宠物栏满了抓不到
		d.Seed.TempNo = 1
		for i := 0; i < PetSlots; i++ {
			p.pets.Pets[i] = petFromFighter(enemyFighter(w, ene, 1, 15), p.char.ID)
		}
		if w.canCapture(p, d) {
			t.Error("宠物栏满了抓不到")
		}
	})
}

// TestPetBattle 用原版的经验分配（谁打倒谁拿）；TestPetBattleSharedExp 是预设的经验共享。
func TestPetBattle(t *testing.T)          { petBattle(t, false) }
func TestPetBattleSharedExp(t *testing.T) { petBattle(t, true) }

func petBattle(t *testing.T, share bool) {
	w, db, g := setup(t)
	w.Rules.ShareExp = share
	ene := petEnemy(t, g, 1)
	c, cl := petOwner(t, w, db, "tamer", 30, 100)
	id := int(c.ID)
	slot := givePet(t, w, id, ene, 20)
	w.PetState(id, slot, "battle")
	if p, ok := find[*proto.Pets](lastOf(cl.take())); !ok || p.Battle != slot || p.Pets[0].State != "battle" {
		t.Fatalf("应改为出战：%+v", p)
	}
	before := 0
	w.Do(func() {
		p := w.players[id].pets.Pets[slot]
		p.VariableAI = 100 * 100 // 忠诚度满，一定听话
		before = p.Exp
		w.startBattleWith(w.players[id], []*battle.Fighter{enemyFighter(w, ene, 1, battle.EnemySlot(0))}, nil, nil)
	})
	st, ok := find[*proto.BattleStart](cl.take())
	if !ok {
		t.Fatal("应开始战斗")
	}
	hasPet := false
	for _, f := range st.Fighters {
		hasPet = hasPet || f.Slot == 5
	}
	if !hasPet {
		t.Fatalf("宠物应站在 5 号位：%+v", st.Fighters)
	}
	// 人下了指令还要等宠物
	w.BattleCommand(id, "guard", -1, -1)
	if _, ok := find[*proto.BattleTurn](cl.take()); ok {
		t.Fatal("宠物还没下指令，不应结算")
	}
	attack := -1
	for i, s := range w.game.Battle.Temps[1].Skills {
		if sk := w.game.Battle.Skills[s]; s >= 0 && sk != nil && sk.Func == "PETSKILL_NormalAttack" {
			attack = i
		}
	}
	if attack < 0 {
		t.Skip("乌力没有攻击技能")
	}
	var end *proto.BattleEnd
	for turn := 0; turn < 40 && end == nil; turn++ {
		if turn > 0 {
			w.BattleCommand(id, "guard", -1, -1)
		}
		w.BattlePetCommand(id, attack, 15)
		msgs := cl.take()
		if _, ok := find[*proto.BattleTurn](msgs); !ok {
			t.Fatal("人和宠物都下了指令应结算")
		}
		end, _ = find[*proto.BattleEnd](msgs)
	}
	if end == nil || end.Result != "win" {
		t.Fatalf("宠物应打赢：%+v", end)
	}
	if share {
		// 经验共享：人与宠物拿一样多（按两者里较低的等级 20 折算）
		if len(end.Pets) != 1 || end.Pets[0].Exp <= 0 || end.Exp != end.Pets[0].Exp {
			t.Errorf("人与宠物应拿一样多的经验：%+v", end)
		}
	} else if end.Exp != 0 || len(end.Pets) != 1 || end.Pets[0].Exp <= 0 {
		// 原版：敌人是宠物打倒的，经验给宠物，人没有
		t.Errorf("经验应给打倒敌人的宠物：%+v", end)
	}

	w.Do(func() {
		if p := w.players[id].pets.Pets[slot]; p.Exp != before+end.Pets[0].Exp {
			t.Errorf("宠物经验应为 %d，实际 %d", before+end.Pets[0].Exp, p.Exp)
		}
	})
}

func TestPetLevelUp(t *testing.T) {
	w, db, g := setup(t)
	ene := petEnemy(t, g, 1)
	c, _ := petOwner(t, w, db, "grower", 30, 100)
	id := int(c.ID)
	slot := givePet(t, w, id, ene, 1)
	w.Do(func() {
		p := w.players[id].pets.Pets[slot]
		old := *p
		ups := w.petGainExp(p, w.game.Battle.NextLevelExp(4))
		if ups != 4 || p.Level != 5 {
			t.Fatalf("应升到 5 级：升了 %d 级，现在 %d 级", ups, p.Level)
		}
		if p.Vital <= old.Vital || p.Str <= old.Str || p.Tgh <= old.Tgh || p.Dex <= old.Dex {
			t.Errorf("升级应按成长率加四项：%+v → %+v", old, *p)
		}
		if p.VariableAI != old.VariableAI+4*battle.AIFixPetLevelUp {
			t.Errorf("每升一级忠诚度变动 +500：%d → %d", old.VariableAI, p.VariableAI)
		}
		// 成长：每级加 成长率 × 4.5..6.0（档次决定范围）
		rank := rankRand[p.Rank]
		lo := float64(old.Alloc[0]) * float64(rank[0]) / 100 * 4
		hi := (float64(old.Alloc[0]) + 10) * float64(rank[1]) / 100 * 4
		if gain := float64(p.Vital - old.Vital); gain < lo-4 || gain > hi {
			t.Errorf("体力成长 %v 不在 [%v, %v]", gain, lo, hi)
		}
	})
}

func TestPetLoyalty(t *testing.T) {
	w, db, g := setup(t)
	ene := petEnemy(t, g, 1)
	c, _ := petOwner(t, w, db, "loyal", 10, 50)
	id := int(c.ID)
	slot := givePet(t, w, id, ene, 10)
	w.Do(func() {
		e := w.players[id]
		p := e.pets.Pets[slot]
		modai := p.ModAI
		if modai <= 0 {
			modai = 100
		}
		want := min(100, int(float64(10*w.stats(e).Charm)*1.1/float64(10*modai)*100))
		if got := w.petLoyalty(e, p); got != want {
			t.Errorf("忠诚度应为 %d，实际 %d", want, got)
		}
		p.VariableAI = -100 * 100
		if got := w.petLoyalty(e, p); got != 0 {
			t.Errorf("变动值很低时忠诚度为 0：%d", got)
		}
	})
}

func TestPetWindow(t *testing.T) {
	w, db, g := setup(t)
	ene := petEnemy(t, g, 1)
	c, cl := petOwner(t, w, db, "keeper", 10, 60)
	id := int(c.ID)
	var slots []int
	for i := 0; i < PetSlots; i++ {
		slots = append(slots, givePet(t, w, id, ene, 1))
	}
	cl.take()
	// 待命最多 4 只
	for _, s := range slots {
		w.PetState(id, s, "standby")
	}
	p, _ := find[*proto.Pets](lastOf(cl.take()))
	n := 0
	for _, pi := range p.Pets {
		if pi.State == "standby" {
			n++
		}
	}
	if n != maxStandby {
		t.Errorf("待命最多 %d 只，实际 %d", maxStandby, n)
	}
	// 倒下（HP 0）的不能出战
	w.Do(func() { w.players[id].pets.Pets[slots[0]].HP = 0 })
	w.PetState(id, slots[0], "battle")
	if p, _ := find[*proto.Pets](lastOf(cl.take())); p.Battle != -1 {
		t.Error("HP 0 的宠物不能出战")
	}
	// 改名：16 个字节以内；别人抓的不能改
	w.PetRename(id, slots[1], "小乌")
	if p, _ := find[*proto.Pets](lastOf(cl.take())); p.Pets[1].FreeName != "小乌" {
		t.Errorf("应改名：%+v", p.Pets[1])
	}
	w.PetRename(id, slots[1], "一二三四五六七八九")
	if p, _ := find[*proto.Pets](lastOf(cl.take())); p != nil {
		t.Error("超过 16 个字节的名字不改")
	}
	w.Do(func() { w.players[id].pets.Pets[slots[2]].Owner = 999999 })
	w.PetRename(id, slots[2], "abc")
	if p, _ := find[*proto.Pets](lastOf(cl.take())); p != nil {
		t.Error("别人抓的宠物不能改名")
	}
	// 丢出（_PET_dropPet）：出战中的也能丢，丢了改为没有出战
	w.Do(func() { w.players[id].pets.Pets[slots[3]].HP = 10 })
	w.PetState(id, slots[3], "battle")
	w.PetDrop(id, slots[3])
	if p, _ := find[*proto.Pets](lastOf(cl.take())); p.Battle != -1 || len(p.Pets) != PetSlots-1 {
		t.Error("出战中的宠物丢出后应改为没有出战")
	}
	w.PetDrop(id, slots[4])
	if p, _ := find[*proto.Pets](lastOf(cl.take())); len(p.Pets) != PetSlots-2 {
		t.Error("应再丢出一只")
	}
}

// lastOf 只留最后一则宠物栏讯息（连发时取最新的）。
func lastOf(msgs []proto.Message) []proto.Message {
	for i := len(msgs) - 1; i >= 0; i-- {
		if _, ok := msgs[i].(*proto.Pets); ok {
			return msgs[i:]
		}
	}
	return nil
}

// flushSaves 等存档队列把之前的存档写完。
func flushSaves(w *World) {
	done := make(chan struct{})
	w.Do(func() { w.saves <- func() { close(done) } })
	<-done
}

// 护士的"宠物回复"（NPC_WindowHealerAllHeal）：宠物 HP 补满；宠物没受伤时说不必回复。
func TestNursePetHeal(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "WindowHealer", "")
	ene := petEnemy(t, g, 1)
	c, cl := besideNPC(t, w, db, "petnurse", n)
	id := int(c.ID)
	slot := givePet(t, w, id, ene, 5)
	w.Do(func() { w.players[id].pets.Pets[slot].HP = 1 })
	cl.take()
	w.Talk(id, n.id)
	win, _ := find[*proto.NpcWindow](cl.take())
	if win == nil {
		t.Fatal("应打开护士的选单")
	}
	w.NpcWindowReply(id, n.id, win.Seq, 0, "6")
	msgs := cl.take()
	p, _ := find[*proto.Pets](msgs)
	if done, _ := find[*proto.NpcWindow](msgs); p == nil || p.Pets[0].HP != p.Pets[0].MaxHp || done == nil || !strings.Contains(done.Text, "已经没问题了") {
		t.Fatalf("宠物应回复：%+v", msgs)
	}
}
