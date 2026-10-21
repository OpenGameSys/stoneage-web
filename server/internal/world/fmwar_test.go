package world

import (
	"strings"
	"testing"
	"time"

	"sa/server/internal/data"
	"sa/server/internal/proto"
	"sa/server/internal/store"
)

// foundFamily：让 name 这个角色成立一个家族（30 级、成人礼、手续费），回传角色与连线。
func foundFamily(t *testing.T, w *World, db store.Store, g *data.Game, fmName, name string) (int, *fakeClient) {
	t.Helper()
	c, cl := petOwner(t, w, db, name, 30, 60)
	id := int(c.ID)
	givePet(t, w, id, petEnemy(t, g, 1), 5) // 守护兽
	w.Do(func() {
		w.players[id].char.Gold = 25000
		w.players[id].setEventDone(4)
	})
	w.Family(id, "A|"+fmName+"|0|0|主旨|0")
	if !strings.Contains(fmtMsgs(cl.take()), "恭喜你成立了新的家族") {
		t.Fatalf("%s 应成立家族 %s", name, fmName)
	}
	w.Do(func() {
		defer w.famLock()()
		fm, _, _ := w.fmOf(w.players[id])
		fm.Setup = true // 正式成立（省掉招 10 个人）
	})
	return id, cl
}

// 庄园争夺战：没有庄园的家族族长对有主人的庄园下战书 → 排定明天的时间 → 到时间可进场、开打 → 场上倒下的人送出场、
// 一边没人了另一边赢；客队赢就接管庄园。
func TestManorWar(t *testing.T) {
	w, db, g := setup(t)
	a, cla := foundFamily(t, w, db, g, "甲家族", "甲族长")
	b, clb := foundFamily(t, w, db, g, "乙家族", "乙族长")
	var fmA, fmB *family
	w.Do(func() {
		defer w.famLock()()
		fmA, _, _ = w.fmOf(w.players[a])
		fmB, _, _ = w.fmOf(w.players[b])
		w.claimManor(fmA, 0) // 甲家族占着萨姆吉尔庄园
	})
	n := findNPC(t, w, "ManorSman", "manorid:1")
	w.Do(func() { w.placeOnFloor(w.players[b], n.floor, n.x+1, n.y, 1) })
	cla.take()
	clb.take()
	w.Talk(b, n.id)
	win, ok := find[*proto.NpcWindow](clb.take())
	if !ok || win.Seq != manorAskSeq {
		t.Fatalf("应问挑战资格：%+v", win)
	}
	w.NpcWindowReply(b, n.id, manorAskSeq, BtnYes, "")
	if win, ok = find[*proto.NpcWindow](clb.take()); !ok || win.Seq != manorConfirmSeq {
		t.Fatalf("应确认挑战：%+v", win)
	}
	w.NpcWindowReply(b, n.id, manorConfirmSeq, BtnYes, "")
	if win, ok = find[*proto.NpcWindow](clb.take()); !ok || win.Seq != manorTimeSeq {
		t.Fatalf("应选时间：%+v", win)
	}
	w.NpcWindowReply(b, n.id, manorTimeSeq, 0, "1")
	if !strings.Contains(fmtMsgs(clb.take()), "设定完成") {
		t.Fatal("应回设定完成")
	}
	if !strings.Contains(fmtMsgs(cla.take()), "战书") {
		t.Error("庄园的主人应收到战书的通知")
	}
	var x *fmWar
	w.Do(func() {
		defer w.famLock()()
		x = w.fam.wars[1]
		if x == nil || x.Flag != warScheduled || x.Host != fmA.ID || x.Guest != fmB.ID || x.Manor != 0 {
			t.Fatalf("排程不对：%+v", x)
		}
		if time.Unix(x.At, 0).Hour() != 20 || time.Unix(x.At, 0).Before(w.now()) {
			t.Errorf("应是明天 20:00：%s", time.Unix(x.At, 0))
		}
	})
	// 拨到进场、开打：两边各一个人进对战场
	place := w.warPlaces()[1]
	if place == nil || place.floor == 0 {
		t.Fatal("应有对战场的位置（向导的 WARP1）")
	}
	w.Do(func() {
		w.famLock()()
		x.At = w.now().Unix()
		w.tickWars()
		if x.Flag != warPrepare {
			t.Fatalf("到时间应可进场：%d", x.Flag)
		}
		w.warpTo(w.players[a], place.enter[0][0], place.enter[0][1], place.enter[0][2])
		w.warpTo(w.players[b], place.enter[1][0], place.enter[1][1], place.enter[1][2])
		x.At = w.now().Unix() - int64(x.Prepare+1)*60
		w.tickWars()
		if x.Flag != warFighting || x.Count0 != [2]int{1, 1} {
			t.Fatalf("应开打、记下两边人数：%d %v", x.Flag, x.Count0)
		}
		if !w.warOpponents(w.players[a], w.players[b]) {
			t.Error("开打中两边应能直接决斗")
		}
	})
	if !strings.Contains(fmtMsgs(clb.take()), "开战") {
		t.Error("应收到开战的通知")
	}
	// 乙族长输了（倒下）→ 送出场 → 乙家族没人了 → 甲家族赢，庄园还是甲的
	w.Do(func() {
		w.warLose(w.players[b])
		w.tickWars()
		w.famLock()()
		if w.players[b].floor != place.exit[0] || w.players[b].warOut {
			t.Errorf("输的人应被送到离场点 %v：%d", place.exit, w.players[b].floor)
		}
		if x.Flag != warDone || x.Winner != fmA.ID || w.fam.manors[0].Owner != fmA.ID {
			t.Fatalf("应是甲家族赢、庄园不变：flag %d winner %d owner %d", x.Flag, x.Winner, w.fam.manors[0].Owner)
		}
	})
	if !strings.Contains(fmtMsgs(clb.take()), "战败离场") {
		t.Error("输的人应看到战败离场")
	}
	// 再来一场：只有乙家族的人在场上 → 乙家族赢 → 接管庄园
	w.Do(func() {
		w.famLock()()
		w.resetWar(x)
		x = w.warOf(1)
		*x = fmWar{ID: 1, Manor: 0, Host: fmA.ID, Guest: fmB.ID, HostName: fmA.Name, GuestName: fmB.Name, At: w.now().Unix() - 120, Prepare: 1, Flag: warPrepare}
		w.fam.warHome[1] = w
		w.warpTo(w.players[b], place.enter[1][0], place.enter[1][1], place.enter[1][2])
		w.tickWars() // 开打：Count0 = 0 / 1
		w.tickWars() // 甲没人 → 乙赢
		if x.Flag != warDone || x.Winner != fmB.ID || w.fam.manors[0].Owner != fmB.ID {
			t.Fatalf("乙家族应赢下庄园：flag %d winner %d owner %d", x.Flag, x.Winner, w.fam.manors[0].Owner)
		}
		if w.players[b].floor != place.exit[0] {
			t.Error("打完场上的人应送出场")
		}
	})
}

// 长毛象巴士：面向停着的巴士说话就付车资上车（成为它的队员）；巴士沿路线走，乘客跟着；到站解散。
func TestBusRide(t *testing.T) {
	w, db, _ := setup(t)
	n := findNPC(t, w, "Bus", "routename1")
	if n.bus == nil {
		t.Fatal("巴士应有路线")
	}
	c, cl := besideNPC(t, w, db, "乘客", n)
	id := int(c.ID)
	start := [2]int{n.x, n.y}
	w.Do(func() {
		e := w.players[id]
		e.char.Gold = 1000
		e.dir = 1 // 面向左边的巴士（besideNPC 站在它右边一格）
	})
	w.Talk(id, n.id)
	var gold int
	w.Do(func() {
		e := w.players[id]
		gold = e.char.Gold
		if e.party == nil || e.party.leader() != n {
			t.Fatal("应上车（巴士是队长）")
		}
	})
	if gold != 1000-n.bus.stone {
		t.Errorf("应付车资 %d：剩 %d", n.bus.stone, gold)
	}
	if !strings.Contains(fmtMsgs(cl.take()), "上车了") {
		t.Error("应说上车了")
	}
	w.Do(func() {
		w.busStart(n)
		for i := 0; i < 40; i++ {
			w.busLoop(n)
		}
		if n.x == start[0] && n.y == start[1] {
			t.Fatal("巴士应该动了")
		}
		e := w.players[id]
		if max(abs(e.x-n.x), abs(e.y-n.y)) > 2 {
			t.Errorf("乘客应跟着巴士：巴士 (%d,%d) 乘客 (%d,%d)", n.x, n.y, e.x, e.y)
		}
		// 直接到终点
		r := n.bus.routes[n.bus.cur]
		last := r.points[len(r.points)-1]
		n.x, n.y, n.bus.point = last[1], last[2], len(r.points)-1
		w.busWalk(n)
		if n.bus.mode != 3 {
			t.Fatalf("到站应是 mode 3：%d", n.bus.mode)
		}
		n.bus.at = w.now().Add(-5 * time.Second)
		w.busLoop(n)
		if n.bus.mode != 0 || e.party != nil {
			t.Errorf("到站 3 秒后应解散、等下一趟：mode %d party %v", n.bus.mode, e.party != nil)
		}
	})
	arrived := false
	for _, m := range cl.take() {
		if talk, ok := m.(*proto.NpcTalk); ok && strings.Contains(talk.Text, "到") {
			arrived = true
		}
	}
	if !arrived {
		t.Error("到站应说到了（msg_end）")
	}
}

// 庄园骑宠（Pointpetlist）：占有庄园、守护精灵颜色对的家族成员才能骑蓝暴 / 红暴，形象按人物编号查 PointRideNoList。
func TestManorRide(t *testing.T) {
	w, db, g := setup(t)
	if g.Ride.ManorIndex(100372) != 0 || g.Ride.ManorIndex(100373) != 1 || g.Ride.ManorIndex(100329) != -1 {
		t.Fatalf("Pointpetlist 应是蓝暴 100372、红暴 100373：%v", g.Ride.ManorPets)
	}
	if !g.Ride.ManorOK(0, 1041, 0) || g.Ride.ManorOK(0, 1041, 1) || g.Ride.ManorOK(0, 1000, 0) {
		t.Error("要在庄园楼层、守护精灵 0")
	}
	if g.Ride.ManorSprite(100000, 0) != 101008 || g.Ride.ManorSprite(100000, 1) != 101009 {
		t.Errorf("小矮子骑蓝暴应是 101008：%d", g.Ride.ManorSprite(100000, 0))
	}
	c, _ := petOwner(t, w, db, "庄主", 30, 60)
	id := int(c.ID)
	w.Do(func() {
		e := w.players[id]
		p := &Pet{Sprite: 100372, Level: 1}
		if !strings.HasPrefix(w.rideInfo(e, p), "need:") || !strings.Contains(w.rideInfo(e, p), "庄园") {
			t.Errorf("没有家族应说要庄园家族：%s", w.rideInfo(e, p))
		}
		e.fm = famView{Index: 7, Duty: fmDutyMember, Floor: 1041, Sprite: 1}
		if s := w.rideInfo(e, p); !strings.Contains(s, "守护精灵") {
			t.Errorf("颜色不对应提示：%s", s)
		}
		e.fm.Sprite = 0
		if s := w.mountSprite(e, 100372); s != 101008 {
			t.Errorf("条件都对应能骑：%d", s)
		}
	})
}
