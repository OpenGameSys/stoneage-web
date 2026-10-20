package world

import (
	"strings"
	"testing"
	"time"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// npcEnemyWin 把场上的敌人都打倒，下一个指令结算后应以打赢结束。
func npcEnemyWin(t *testing.T, w *World, charID int, cl *fakeClient) []proto.Message {
	t.Helper()
	w.Do(func() {
		for _, f := range w.players[charID].fight.b.F {
			if f != nil && !f.Player {
				f.HP = 0
			}
		}
	})
	w.BattleCommand(charID, "guard", -1, -1)
	msgs := cl.take()
	if end, ok := find[*proto.BattleEnd](msgs); !ok || end.Result != "win" {
		t.Fatalf("敌人全倒应打赢：%+v", end)
	}
	return msgs
}

// talkYes：说话 → 确认窗口（CHAR_WINDOWTYPE_NPCENEMY_START，是/否）→ 按"是"，回传开打的 BattleStart。
func talkYes(t *testing.T, w *World, charID int, cl *fakeClient, n *entity) *proto.BattleStart {
	t.Helper()
	w.Talk(charID, n.id)
	win, ok := find[*proto.NpcWindow](cl.take())
	if !ok || win.Seq != seqNpcEnemyStart || win.Buttons != BtnYesNo {
		t.Fatalf("应先问要不要打：%+v", win)
	}
	w.NpcWindowReply(charID, n.id, win.Seq, BtnYes, "")
	st, ok := find[*proto.BattleStart](cl.take())
	if !ok {
		t.Fatal("按是应开打")
	}
	return st
}

// 大地的守护兽（sb_dbt：askbattlemsg1、dieact:1）：问是/否 → 和 enemyno 的敌人打 → 打赢传送到 warpfl/warpx/warpy。
func TestNpcEnemyWarp(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "NPCEnemy", "sb_dbt.arg")
	c, cl := besideNPC(t, w, db, "guardian", n)
	id := int(c.ID)

	// 按"否"不打
	w.Talk(id, n.id)
	win, ok := find[*proto.NpcWindow](cl.take())
	if !ok {
		t.Fatal("应弹出确认窗口")
	}
	if !strings.Contains(win.Text, "打倒") || strings.Count(win.Text, "\n") != 5 {
		t.Errorf("askbattlemsg1..6 以换行接起来：%q", win.Text)
	}
	w.NpcWindowReply(id, n.id, win.Seq, BtnNo, "")
	if _, ok := find[*proto.BattleStart](cl.take()); ok {
		t.Fatal("按否不应开打")
	}

	st := talkYes(t, w, id, cl, n)
	if st.Surprise != 0 {
		t.Error("NPC 开的战斗不判定偷袭")
	}
	want := map[string]bool{}
	for _, s := range enemyNo(n.npc.Arg, 64) {
		if e := g.Battle.Enemies[data.Atoi(s)]; e != nil {
			want[g.Battle.Temps[e.TempNo].Name] = true
		}
	}
	enemies := 0
	for _, f := range st.Fighters {
		if f.Slot >= 10 {
			enemies++
			if !want[f.Name] {
				t.Errorf("出场的 %s 不在 enemyno 里", f.Name)
			}
		}
	}
	if enemies == 0 || enemies > 10 {
		t.Fatalf("敌人数不对：%d", enemies)
	}
	msgs := npcEnemyWin(t, w, id, cl)
	ef, ok := find[*proto.EnterFloor](msgs)
	if !ok || ef.Floor != 21015 || ef.X != 9 || ef.Y != 17 {
		t.Fatalf("打赢应传送到 21015 (9,17)：%+v", ef)
	}
	if n.hidden() {
		t.Error("dieact:1 的 NPC 不消失")
	}
}

// 宝物袋（sb_mimi：item:2697、dieact:0、time:60）：没有切绳石时说 deniedmsg；有了才问；打赢后消失 60 秒再出现，道具不收走。
func TestNpcEnemyHideAndRevive(t *testing.T) {
	w, db, _ := setup(t)
	n := findNPC(t, w, "NPCEnemy", "sb_mimi.arg")
	c, cl := besideNPC(t, w, db, "bagger", n)
	other, ocl := besideNPC(t, w, db, "watcher", n)
	id := int(c.ID)

	w.Talk(id, n.id)
	msgs := cl.take()
	if _, ok := find[*proto.NpcWindow](msgs); ok {
		t.Fatal("没有道具不应问")
	}
	if nt, ok := find[*proto.NpcTalk](msgs); !ok || !strings.Contains(nt.Text, "袋口") {
		t.Fatalf("应说 deniedmsg：%+v", nt)
	}

	giveItem(w, id, 2697)
	cl.take()
	talkYes(t, w, id, cl, n)
	cl.take()
	ocl.take()
	npcEnemyWin(t, w, id, cl)
	if !n.hidden() {
		t.Fatal("dieact:0 打赢后 NPC 应消失")
	}
	if _, ok := find[*proto.ObjDisappear](ocl.take()); !ok {
		t.Error("旁边的人应看到 NPC 消失")
	}
	w.Do(func() {
		if !w.hasItem(w.players[id], 2697) {
			t.Error("没有 steal 的不收走道具")
		}
	})
	w.Talk(int(other.ID), n.id)
	if m := ocl.take(); len(m) != 0 {
		t.Fatalf("消失时不理人：%+v", m)
	}

	// 时间没到：还在消失；过了 time 秒才回来
	start := time.Now()
	w.Do(func() {
		w.now = func() time.Time { return start.Add(30 * time.Second) }
		w.npcEnemyLoop(n)
	})
	if !n.hidden() {
		t.Fatal("还没到 time 秒不应出现")
	}
	w.Do(func() {
		w.now = func() time.Time { return start.Add(61 * time.Second) }
		w.npcEnemyLoop(n)
	})
	if n.hidden() {
		t.Fatal("过了 time 秒应重新出现")
	}
	if ap, ok := find[*proto.ObjAppear](ocl.take()); !ok || ap.Objs[0].ID != n.id || ap.Objs[0].Sprite != n.npc.Sprite {
		t.Fatalf("应重新出现、换回原来的图号：%+v", ap)
	}
	w.Talk(int(other.ID), n.id)
	if _, ok := find[*proto.NpcTalk](ocl.take()); !ok {
		t.Error("回来后又会理人")
	}
}

// 矿工巴卡斯（sb_bac：item:2535、steal:1）：打赢后收走参加证。
func TestNpcEnemySteal(t *testing.T) {
	w, db, _ := setup(t)
	n := findNPC(t, w, "NPCEnemy", "sb_bac.arg")
	c, cl := besideNPC(t, w, db, "miner", n)
	id := int(c.ID)
	giveItem(w, id, 2535)
	cl.take()
	talkYes(t, w, id, cl, n)
	w.Do(func() {
		if !w.hasItem(w.players[id], 2535) {
			t.Error("steal:1 开打时还不收")
		}
	})
	npcEnemyWin(t, w, id, cl)
	w.Do(func() {
		if w.hasItem(w.players[id], 2535) {
			t.Error("steal:1 打赢后应收走道具")
		}
	})
}

// 道场第 1 个弟子（gym:1，没有 askbattlemsg）：说话直接开打，敌人等级是 gym、第一个用弟子的名字；打赢传到下一个弟子前。
func TestNpcEnemyDoujyou(t *testing.T) {
	w, db, _ := setup(t)
	n := findNPC(t, w, "NPCEnemy", "samdou001.arg")
	c, cl := besideNPC(t, w, db, "pupil", n)
	id := int(c.ID)
	w.Talk(id, n.id)
	msgs := cl.take()
	st, ok := find[*proto.BattleStart](msgs)
	if !ok {
		t.Fatal("道场弟子说话就开打")
	}
	if nt, ok := find[*proto.NpcTalk](msgs); !ok || !strings.Contains(nt.Text, "第1个人") {
		t.Errorf("应说 startmsg：%+v", nt)
	}
	var names []string
	for _, f := range st.Fighters {
		if f.Slot >= 10 {
			names = append(names, f.Name)
			if f.Level != 1 {
				t.Errorf("等级应为 gym（1）：%+v", f)
			}
		}
	}
	if len(names) < 1 || len(names) > 2 || names[0] != n.name {
		t.Fatalf("应是弟子本人加一只宠物：%v", names)
	}
	ef, ok := find[*proto.EnterFloor](npcEnemyWin(t, w, id, cl))
	if !ok || ef.Floor != 1021 || ef.X != 400 || ef.Y != 114 {
		t.Fatalf("打赢应传到 1021 (400,114)：%+v", ef)
	}
}

// 萨登（NEWNPCENEMY、B_evnow:112、onebattle:1）：没有进行中的任务 112 说 deniedmsg；
// 有的话开打，别人这时来说 alreadymsg；打赢后按 OVER 各段的 FREE 选 WARP（等级不够 90 → 第二段 117,180,99）。
func TestNpcEnemyNewEvent(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "NPCEnemy", "sardon.arg")
	c, cl := besideNPC(t, w, db, "hero", n)
	other, ocl := besideNPC(t, w, db, "late", n)
	id := int(c.ID)

	w.Talk(id, n.id)
	if nt, ok := find[*proto.NpcTalk](cl.take()); !ok || !strings.Contains(nt.Text, "有关系") {
		t.Fatalf("没有任务 112 应说 deniedmsg：%+v", nt)
	}
	w.Do(func() {
		w.players[id].setEventNow(112)
		w.players[int(other.ID)].setEventNow(112)
	})
	talkYes(t, w, id, cl, n)

	w.Talk(int(other.ID), n.id)
	if nt, ok := find[*proto.NpcTalk](ocl.take()); !ok || nt.Text != "………" {
		t.Fatalf("onebattle：别人打的时候应说 alreadymsg：%+v", nt)
	}

	msgs := npcEnemyWin(t, w, id, cl)
	if nt, ok := find[*proto.NpcTalk](msgs); !ok || !strings.Contains(nt.Text, "怎么找到我") {
		t.Errorf("应说第二段的 endmsg：%+v", nt)
	}
	if g.Floors[117] != nil {
		if ef, ok := find[*proto.EnterFloor](msgs); !ok || ef.Floor != 117 || ef.X != 180 || ef.Y != 99 {
			t.Fatalf("应传到 117 (180,99)：%+v", ef)
		}
	}
	// 打完了，下一个人可以打
	w.Talk(int(other.ID), n.id)
	if _, ok := find[*proto.NpcWindow](ocl.take()); !ok {
		t.Error("战斗结束后别人可以打")
	}
}

// 逃跑或打输：什么都不发生（WinFunc 只在打赢时调用）。
func TestNpcEnemyLose(t *testing.T) {
	w, db, _ := setup(t)
	n := findNPC(t, w, "NPCEnemy", "sb_dbt.arg")
	c, cl := besideNPC(t, w, db, "loser", n)
	id := int(c.ID)
	talkYes(t, w, id, cl, n)
	w.Do(func() {
		e := w.players[id]
		e.fight.b.F[0].HP = 0
		w.tryResolve(e.fight.room) // 倒下的人不能下指令，直接结算
	})
	msgs := cl.take()
	if end, ok := find[*proto.BattleEnd](msgs); !ok || end.Result != "lose" {
		t.Fatalf("自己倒下应打输：%+v", end)
	}
	if _, ok := find[*proto.EnterFloor](msgs); ok {
		t.Error("打输不传送")
	}
}

// 走到战斗 NPC 身上（EVENY_npcEncount，mode 0）：退回原来的格子，照样问要不要打。
func TestNpcEnemyBump(t *testing.T) {
	w, db, _ := setup(t)
	n := findNPC(t, w, "NPCEnemy", "sb_dbt.arg")
	c, cl := besideNPC(t, w, db, "bumper", n)
	id := int(c.ID)
	w.Do(func() {
		e := w.players[id]
		e.x, e.dir = n.x, 1 // 从 x+1 那格朝 (-1, 0) 走到 NPC 身上
		w.rollEncounter(e)
	})
	msgs := cl.take()
	if pc, ok := find[*proto.PosCorrect](msgs); !ok || pc.X != n.x+1 || pc.Y != n.y {
		t.Fatalf("应退回原来的格子：%+v", pc)
	}
	if win, ok := find[*proto.NpcWindow](msgs); !ok || win.Seq != seqNpcEnemyStart {
		t.Fatalf("碰到 NPC 应问要不要打：%+v", msgs)
	}
}

func TestNpcArgNum(t *testing.T) {
	for _, c := range []struct {
		arg, key string
		want     int
	}{
		{"entype:2|time=300|", "time", 300},
		{"entype:2|dieact:0|", "dieact", 0},
		{"entype:2|", "time", -1},
		{"gym:12|startmsg:我是第12个人", "gym", 12},
	} {
		if got := npcArgNum(c.arg, c.key); got != c.want {
			t.Errorf("npcArgNum(%q, %q) = %d，应为 %d", c.arg, c.key, got, c.want)
		}
	}
}

// 和数据里每一个战斗 NPC 说话、按"是"、能开打的就直接打赢：不应出错。
func TestNpcEnemySweep(t *testing.T) {
	w, db, _ := setup(t)
	var npcs []*entity
	w.Do(func() {
		for _, es := range w.floors {
			for _, e := range es {
				if e.npc != nil && e.npc.Function == "NPCEnemy" {
					npcs = append(npcs, e)
				}
			}
		}
	})
	if len(npcs) < 200 {
		t.Fatalf("战斗 NPC 太少：%d", len(npcs))
	}
	c, cl := besideNPC(t, w, db, "sweeper", npcs[0])
	id := int(c.ID)
	battles := 0
	for _, n := range npcs {
		w.Do(func() {
			e := w.players[id]
			e.char.Level = 80
			w.placeOnFloor(e, n.floor, n.x+1, n.y, 2)
			if n.hidden() { // 前面打倒的同一种 NPC：让它回来
				w.now = func() time.Time { return time.Now().Add(time.Hour) }
				w.npcEnemyLoop(n)
				w.now = time.Now
			}
		})
		cl.take()
		w.Talk(id, n.id)
		if win, ok := find[*proto.NpcWindow](cl.take()); ok {
			w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
		}
		var inFight bool
		w.Do(func() { inFight = w.players[id].fight != nil })
		if inFight {
			battles++
			npcEnemyWin(t, w, id, cl)
		}
	}
	if battles == 0 {
		t.Fatal("应至少开打几场")
	}
	t.Logf("%d 个战斗 NPC，开打 %d 场", len(npcs), battles)
}
