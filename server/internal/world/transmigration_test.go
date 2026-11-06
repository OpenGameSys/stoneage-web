package world

import (
	"strings"
	"testing"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// transNPC 是漆黑的洞窟地下 20 楼（32021）的精灵王。
func transNPC(t *testing.T, w *World) *entity {
	t.Helper()
	n := findNPC(t, w, "Transmigration", "giiru/event/sikkoku/ten.arg")
	if n.floor != 32021 || n.x != 31 || n.y != 28 {
		t.Fatalf("精灵王应在 32021 (31,28)：%d (%d,%d)", n.floor, n.x, n.y)
	}
	return n
}

// enemyPet 用敌人编号做一只宠物（等级、主人另设）。
func enemyPet(t *testing.T, w *World, enemyID, level int, owner int64) *Pet {
	t.Helper()
	en := w.game.Battle.Enemies[enemyID]
	if en == nil {
		t.Fatalf("没有敌人 %d", enemyID)
	}
	var pet *Pet
	w.Do(func() { pet = w.petFromEnemy(en) })
	pet.Level, pet.Owner = level, owner
	return pet
}

func transWin(t *testing.T, cl *fakeClient, want string) *proto.NpcWindow {
	t.Helper()
	win, ok := find[*proto.NpcWindow](cl.take())
	if !ok || !strings.Contains(win.Text, want) {
		t.Fatalf("窗口应含「%s」：%+v", want, win)
	}
	return win
}

// 第 1 转：条件 → startmsg → mainmsg → 转生（数值照 NPC_TransmigrationStatus）→ yesmsg1 → yesmsg2 → 传回出生村。
func TestTransmigrationFirst(t *testing.T) {
	w, db, _ := setup(t)
	n := transNPC(t, w)
	c, cl := besideNPC(t, w, db, "tensei", n)
	id := int(c.ID)
	guardian := enemyPet(t, w, 1373, 50, c.ID) // 转生地：模板 693
	w.Do(func() {
		p := w.players[id]
		p.char.Level, p.char.Exp = 140, 12345
		p.char.Vital, p.char.Str, p.char.Tgh, p.char.Dex = 100, 150, 50, 100
		for _, ev := range []int{39, 40, 42, 46, 1, 2, 300} {
			p.setEventDone(ev)
		}
		p.setEventNow(70)
		w.addPet(p, guardian)
	})
	cl.take()

	w.Talk(id, n.id)
	win := transWin(t, cl, "帖拉所依朵来到我这里")
	w.NpcWindowReply(id, n.id, win.Seq, BtnNext, "")
	win = transWin(t, cl, "下定决心要进行转生")
	if win.Buttons != BtnOK|BtnCancel {
		t.Fatalf("mainmsg 应是确定 / 取消：%+v", win)
	}
	w.NpcWindowReply(id, n.id, win.Seq, BtnOK, "")
	win = transWin(t, cl, "你被一片光芒包围")
	if win.Buttons != BtnNext {
		t.Fatalf("yesmsg1 后面还有 yesmsg2，应是下一页：%+v", win)
	}
	w.Do(func() {
		c := w.players[id].char
		// 任务 2 个（1、2）、等级 130：ans = int(400/12 + 2/4 + (130−85)/4) = 45，按 100:150:50:100 分、四舍五入
		if c.Trans != 1 || c.Level != 1 || c.Exp != 0 || c.Points != 10 {
			t.Errorf("转生后应 1 转、1 级、经验 0、点数 10：%d 转 %d 级 经验 %d 点数 %d", c.Trans, c.Level, c.Exp, c.Points)
		}
		if got := [4]int{c.Vital, c.Str, c.Tgh, c.Dex}; got != [4]int{11, 17, 6, 11} {
			t.Errorf("新的基本状态应为 [11 17 6 11]：%v", got)
		}
		if c.TransEq != 2<<16+130 {
			t.Errorf("TRANSEQUATION 应记任务 2、等级 130：%x", c.TransEq)
		}
		p := w.players[id]
		if p.eventDone(39) || !p.eventDone(48) || !p.eventDone(300) || !p.eventNow(70) {
			t.Errorf("旗标应清掉（256 以下）、补上 48、保留 300 与进行中的 70：%q / %q", c.EndEvents, c.NowEvents)
		}
		var temps []int
		for _, pet := range p.pets.Pets {
			if pet != nil {
				temps = append(temps, pet.TempNo)
			}
		}
		if len(temps) != 1 || temps[0] != w.game.Battle.Enemies[1377].TempNo {
			t.Errorf("应交出守护兽、拿到萨姆吉尔第 1 转送的转生乌力斯坦（敌人 1377）：模板 %v", temps)
		}
	})
	w.NpcWindowReply(id, n.id, win.Seq, BtnNext, "")
	win = transWin(t, cl, "意识越来越模糊")
	if win.Buttons != BtnOK {
		t.Fatalf("最后一页应是确定：%+v", win)
	}
	w.NpcWindowReply(id, n.id, win.Seq, BtnOK, "")
	w.Do(func() {
		p := w.players[id]
		if h := data.Hometowns[0]; p.floor != h.Floor || p.x != h.X || p.y != h.Y {
			t.Errorf("应传回萨姆吉尔的村长家 %v：%d (%d,%d)", h, p.floor, p.x, p.y)
		}
	})
}

// 条件不足说 nonemsg、点数没分配完不转、取消说 nomsg；GM /tensei 不看条件。
func TestTransmigrationRefuse(t *testing.T) {
	w, db, _ := setup(t)
	n := transNPC(t, w)
	c, cl := besideNPC(t, w, db, "notyet", n)
	id := int(c.ID)
	w.Do(func() { w.players[id].char.Level = 79 })
	w.Talk(id, n.id)
	transWin(t, cl, "你好像还没有达成")

	guardian := enemyPet(t, w, 1373, 50, c.ID)
	w.Do(func() {
		p := w.players[id]
		p.char.Level, p.char.Points = 80, 3
		for _, ev := range []int{39, 40, 42, 46} {
			p.setEventDone(ev)
		}
		w.addPet(p, guardian)
	})
	w.Talk(id, n.id)
	win := transWin(t, cl, "来到我这里")
	w.NpcWindowReply(id, n.id, win.Seq, BtnNext, "")
	win = transWin(t, cl, "下定决心")
	w.NpcWindowReply(id, n.id, win.Seq, BtnOK, "")
	transWin(t, cl, "点数没加完")

	w.Do(func() { w.players[id].char.Points = 0 })
	w.Talk(id, n.id)
	win = transWin(t, cl, "来到我这里")
	w.NpcWindowReply(id, n.id, win.Seq, BtnNext, "")
	win = transWin(t, cl, "下定决心")
	w.NpcWindowReply(id, n.id, win.Seq, BtnCancel, "")
	transWin(t, cl, "欢迎随时来我这里")
	w.Do(func() {
		if c := w.players[id].char; c.Trans != 0 || c.Level != 80 {
			t.Errorf("取消了不应转生：%d 转 %d 级", c.Trans, c.Level)
		}
	})

	// 已经 6 转：maxmsg
	w.Do(func() { w.players[id].char.Trans = 6 })
	w.Talk(id, n.id)
	transWin(t, cl, "最强的力量")
}

// 宠物转生：带着自己的 79 级玛蕾菲雅 → 选 80 级以上的宠物 → 按两者的成长重算、变回 1 级、转生 +1，玛蕾菲雅与灵石交出。
func TestPetTransmigration(t *testing.T) {
	w, db, g := setup(t)
	n := transNPC(t, w)
	c, cl := besideNPC(t, w, db, "pettrans", n)
	id := int(c.ID)
	marefia := enemyPet(t, w, 1479, 79, c.ID)
	target := enemyPet(t, w, petEnemy(t, g, 2).ID, 100, c.ID) // 乌力乌力
	target.Alloc = [4]int{22, 23, 11, 23}
	slot := -1
	w.Do(func() {
		p := w.players[id]
		p.char.Level = 80
		for _, ev := range []int{4, 69, 70} {
			p.setEventDone(ev)
		}
		w.addPet(p, marefia)
		slot = w.addPet(p, target)
		p.inv[data.EquipSlots] = &Item{ID: 19625}
	})
	cl.take()

	w.Talk(id, n.id)
	win := transWin(t, cl, "和精灵王重聚")
	w.NpcWindowReply(id, n.id, win.Seq, BtnNext, "")
	win = transWin(t, cl, "准备好要转生")
	w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
	win, _ = find[*proto.NpcWindow](cl.take())
	if win == nil || win.Kind != "petselect" {
		t.Fatalf("应开选宠物的窗口：%+v", win)
	}
	w.NpcWindowReply(id, n.id, win.Seq, 0, "2")
	win = transWin(t, cl, "["+target.Name+"]没错吧")
	w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
	transWin(t, cl, "宠物转生成功了")

	total1 := 0
	for _, a := range marefia.Alloc {
		total1 += max(1, a)
	}
	want := petTransAns(min(150, total1), 79, 100, target.Rank, 0) // 乌力乌力的成长 22+23+11+23 = 79
	w.Do(func() {
		p := w.players[id]
		pet := p.pets.Pets[slot]
		if pet == nil || pet.Level != 1 || pet.Trans != 1 || pet.Exp != 0 || pet.Slot != 7 {
			t.Fatalf("宠物应变回 1 级、转生 1 次、技能栏 7：%+v", pet)
		}
		if sum := pet.Alloc[0] + pet.Alloc[1] + pet.Alloc[2] + pet.Alloc[3]; sum < want-12 || sum > want+8 {
			t.Errorf("新的成长总和应约为 %d（各项 −2..+2、整数除法）：%v", want, pet.Alloc)
		}
		for _, q := range p.pets.Pets {
			if q != nil && q.TempNo == transPetRebel {
				t.Error("玛蕾菲雅应交出")
			}
		}
		if p.inv[data.EquipSlots] != nil || !p.eventNow(71) {
			t.Errorf("灵石应收走、设进行中的 71：%+v %q", p.inv[data.EquipSlots], p.char.NowEvents)
		}
	})
}

// GM /tensei：不看任务与宠物条件，照精灵王的算法转生（要 80 级以上）。
func TestGMTensei(t *testing.T) {
	w, db, _ := setup(t)
	w.GMs = map[string]bool{"gmt": true}
	gm := newChar(t, db, "gmt", 1000, 47, 82)
	cl := &gmClient{account: "gmt"}
	w.Enter(gm, cl)
	id := int(gm.ID)
	w.Do(func() {})
	cl.take()
	run := func(text string) string {
		w.GmCommand(id, text)
		w.Do(func() {})
		m, ok := find[*proto.ChatMsg](cl.take())
		if !ok {
			t.Fatalf("%s 没有回应", text)
		}
		return m.Text
	}
	if r := run("/tensei"); !strings.Contains(r, "80 级以上") {
		t.Errorf("1 级不能转生：%s", r)
	}
	run("/lv 100")
	if r := run("/tensei"); !strings.Contains(r, "转生为 1 转") {
		t.Errorf("/tensei 应转生：%s", r)
	}
	w.Do(func() {
		if c := w.players[id].char; c.Trans != 1 || c.Level != 1 || c.Points != 10 || !w.players[id].eventDone(48) {
			t.Errorf("/tensei 后应 1 转、1 级、点数 10、补上事件 48：%d 转 %d 级 %d 点 %q", c.Trans, c.Level, c.Points, c.EndEvents)
		}
	})
}

// 任务面板的转生提示（jobdaily_h5.txt 9101 / 9102）：80 级起，四个洞窟没做齐列"转生的条件"，做齐了列"漆黑洞窟·找精灵王转生"；6 转之后都不列。
func TestTransQuestHints(t *testing.T) {
	w, db, _ := setup(t)
	n := transNPC(t, w)
	c, _ := besideNPC(t, w, db, "hint", n)
	id := int(c.ID)
	ids := func() map[int]proto.QuestEntry {
		out := map[int]proto.QuestEntry{}
		w.Do(func() {
			for _, q := range w.questLog(w.players[id]) {
				out[q.ID] = q
			}
		})
		return out
	}
	if q := ids(); q[9101].ID != 0 || q[9102].ID != 0 {
		t.Fatalf("1 级不应列转生提示：%v", q)
	}
	w.Do(func() {
		p := w.players[id]
		p.char.Level = 80
		p.setEventDone(39)
		p.setEventDone(40)
	})
	if q := ids(); q[9101].State != "未完成" || q[9102].ID != 0 || !strings.Contains(q[9101].Hint, "碧青") {
		t.Fatalf("80 级缺洞窟应列转生的条件：%v", q)
	}
	w.Do(func() {
		p := w.players[id]
		p.setEventDone(42)
		p.setEventDone(46)
	})
	if q := ids(); q[9101].ID != 0 || q[9102].State != "进行中" || !strings.Contains(q[9102].Hint, "守门人") {
		t.Fatalf("四个洞窟做齐应列找精灵王：%v", q)
	}
	w.Do(func() { w.players[id].char.Trans = 6 })
	if q := ids(); q[9101].ID != 0 || q[9102].ID != 0 {
		t.Fatalf("6 转之后不应再列：%v", q)
	}
}
