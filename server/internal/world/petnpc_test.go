package world

import (
	"strings"
	"testing"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// enemyOfTemp 找一种模板编号为 tempNo 的敌人（不论能不能捕获）。
func enemyOfTemp(t *testing.T, g *data.Game, tempNo int) *data.Enemy {
	t.Helper()
	for _, e := range g.Battle.Enemies {
		if e.TempNo == tempNo {
			return e
		}
	}
	t.Skipf("没有模板 %d 的敌人", tempNo)
	return nil
}

// exFinish 一路按"是"/"确定"把交换的窗口走完（最多 6 个窗口），回传沿途的讯息。
func exFinish(w *World, cl *fakeClient, id int, n *entity, win *proto.NpcWindow) []proto.Message {
	var all []proto.Message
	for i := 0; i < 6 && win != nil; i++ {
		btn := BtnOK
		switch {
		case win.Buttons&BtnYes != 0:
			btn = BtnYes
		case win.Buttons&BtnNext != 0:
			btn = BtnNext
		}
		var msgs []proto.Message
		win, msgs = exReply(w, cl, id, n, win, btn)
		all = append(all, msgs...)
	}
	return all
}

// 任务交换的宠物条件（NPC_PetLvCheck）：模板、等级、只数；EV 只认任务给的宠物。传送员等的 PET 条件（ActionCheckMyPet）。
func TestPetConditions(t *testing.T) {
	w, db, g := setup(t)
	ene := petEnemy(t, g, 1)
	c, _ := petOwner(t, w, db, "petcond", 10, 60)
	id := int(c.ID)
	slot := givePet(t, w, id, ene, 5)
	exChar(w, id, func(p *entity) {
		cases := []struct {
			cond string
			want bool
		}{
			{"PET>0-1", true}, {"PET>4-1", true}, {"PET>5-1", false}, {"PET<6-1", true}, {"PET=5-1", true},
			{"PET>0-1*1", true}, {"PET>0-1*2", false}, {"PET>0-2", false}, {"EVPET>0-1", false},
		}
		for _, tc := range cases {
			if got := w.exBigSmall(p, tc.cond, "", false); got != tc.want {
				t.Errorf("%q = %v，应为 %v", tc.cond, got, tc.want)
			}
		}
		p.pets.Pets[slot].EventPet = true
		if !w.exBigSmall(p, "EVPET>0-1", "", false) || w.exBigSmall(p, "PET>0-1", "", false) {
			t.Error("任务宠物只符合写了 EV 的条件")
		}
		p.pets.Pets[slot].EventPet = false
		if !w.exBigSmall(p, "PET>0-1", p.pets.Pets[slot].Name, true) || w.exBigSmall(p, "PET>0-1", "别的名字", true) {
			t.Error("有 Pet_Name 时名字要对上")
		}
		// 共用的条件（传送员、战斗 NPC）：第一只这个模板的宠物比等级
		if !w.condCheck(p, "PET>4-1") || w.condCheck(p, "PET>5-1") || w.condCheck(p, "PET>0-2") {
			t.Error("ActionCheckMyPet 不对")
		}
	})
}

// 任务交换交出宠物（extra/event/mission.create：ITEM=20003&PET>0-72*1&PET>0-74*1，DelPet 各交一只）。
func TestExchangeDelPet(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "ExChangeMan", "PET>0-72*1&PET>0-74*1")
	a, b := enemyOfTemp(t, g, 72), enemyOfTemp(t, g, 74)
	c, cl := besideNPC(t, w, db, "deliver", n)
	id := int(c.ID)
	exChar(w, id, func(p *entity) {
		w.addPet(p, w.petFromEnemy(a))
		w.addPet(p, w.petFromEnemy(b))
		w.addPet(p, w.petFromEnemy(a)) // 多一只，只交出一只
	})
	giveItem(w, id, 20003)
	cl.take()
	win, _ := exTalk(w, cl, id, n)
	if win == nil {
		t.Fatal("条件都有，应打开交换的窗口")
	}
	msgs := exFinish(w, cl, id, n, win)
	gave := 0
	for _, m := range msgs {
		if cm, ok := m.(*proto.ChatMsg); ok && strings.HasPrefix(cm.Text, "交出") {
			gave++
		}
	}
	exChar(w, id, func(p *entity) {
		left := 0
		for _, pet := range p.pets.Pets {
			if pet != nil {
				left++
			}
		}
		if left != 1 || p.pets.Pets[2] == nil || p.pets.Pets[2].TempNo != 72 {
			t.Errorf("应交出模板 72、74 各一只，剩下 1 只：剩 %d 只", left)
		}
	})
	if gave < 2 {
		t.Errorf("应说两次交出：%d", gave)
	}
}

// 任务交换给宠物（genout/2000npc_m.create：对村民说暗语"楍"的 ACCEPT，GetPet 四选一）；宠物栏满了显示 PetFullMsg、不给。
func TestExchangeGetPet(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "ExChangeMan", "1661,1662,1663,1664")
	ene := petEnemy(t, g, 1)
	c, cl := besideNPC(t, w, db, "receiver", n)
	id := int(c.ID)
	for i := 0; i < PetSlots; i++ {
		givePet(t, w, id, ene, 1)
	}
	exChar(w, id, func(p *entity) { // 面向村民（暗语要对着 NPC 说）
		for d, m := range MOVE {
			if p.x+m[0] == n.x && p.y+m[1] == n.y {
				p.dir = d
			}
		}
	})
	cl.take()
	say := func() *proto.NpcWindow {
		w.Chat(id, "near", "楍", 0, 3)
		win, _ := find[*proto.NpcWindow](cl.take())
		return win
	}
	win := say()
	if win == nil {
		t.Fatal("说暗语应打开交换的窗口")
	}
	full := exFinish(w, cl, id, n, win)
	if !strings.Contains(fmtMsgs(full), "装不下") {
		t.Errorf("宠物栏满了应显示 PetFullMsg：%v", fmtMsgs(full))
	}
	exChar(w, id, func(p *entity) {
		for _, pet := range p.pets.Pets {
			if pet.TempNo != 1 {
				t.Error("宠物栏满了不应拿到宠物")
			}
		}
		p.pets.Pets[4] = nil
	})
	msgs := exFinish(w, cl, id, n, say())
	got, _ := find[*proto.Pets](lastOf(msgs))
	exChar(w, id, func(p *entity) {
		pet := p.pets.Pets[4]
		if pet == nil || pet.EventPet || pet.HP != pet.derive().HP {
			t.Fatalf("应拿到一只一般宠物（满血）：%+v %+v", pet, got)
		}
		en := g.Battle.Enemies[pet.EnemyID]
		if en == nil || pet.Level < en.LvMin || pet.Level > en.LvMax {
			t.Errorf("等级应在敌人的等级范围：%d", pet.Level)
		}
	})
}

// fmtMsgs 把讯息里的窗口与聊天文字接起来（检查显示了什么用）。
func fmtMsgs(msgs []proto.Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		switch v := m.(type) {
		case *proto.NpcWindow:
			sb.WriteString(v.Text + "\n")
		case *proto.ChatMsg:
			sb.WriteString(v.Text + "\n")
		}
	}
	return sb.String()
}
