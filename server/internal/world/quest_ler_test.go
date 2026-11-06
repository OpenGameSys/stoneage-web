//go:build questler

package world

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 雷尔任务（事件 175、176，六转的条件之一）与它的前置地精灵任务（事件 171..174）从头走到尾的模拟。
// 只在 -tags questler 时编译：go test -tags questler -run 'TestQuest(EarthSpirit|Ler)' -count=1 ./internal/world/

// lerNPC 按参数档名找 NPC（不论 functionset 有没有对上；对不上的 Function 是空的），找不到就失败。
func lerNPC(t *testing.T, w *World, key string) *entity {
	t.Helper()
	var found *entity
	w.Do(func() {
		for _, es := range w.floors {
			for _, e := range es {
				if e.npc != nil && strings.Contains(strings.Join(e.npc.Args, "|"), key) {
					found = e
				}
			}
		}
	})
	if found == nil {
		t.Fatalf("数据里没有 NPC %s", key)
	}
	return found
}

// lerReach：在 floor 上从 (sx,sy) 走八方向、只踩能走而且不是传送点的格子，能不能走到 (tx,ty) 的 r 格内。
func lerReach(g *data.Game, floor, sx, sy, tx, ty, r int) bool {
	f := g.Floors[floor]
	if f == nil {
		return false
	}
	type pt struct{ x, y int }
	seen := map[pt]bool{{sx, sy}: true}
	q := []pt{{sx, sy}}
	for len(q) > 0 {
		c := q[0]
		q = q[1:]
		if max(abs(c.x-tx), abs(c.y-ty)) <= r {
			return true
		}
		for d := 0; d < 8; d++ {
			n := pt{c.x + MOVE[d][0], c.y + MOVE[d][1]}
			if seen[n] || !g.Tiles.Walkable(f, n.x, n.y) {
				continue
			}
			seen[n] = true
			if _, warp := g.WarpAt(floor, n.x, n.y); warp {
				continue
			}
			q = append(q, n)
		}
	}
	return false
}

// lerExits：从 (sx,sy) 走得到的传送点及其目的地。
func lerExits(g *data.Game, floor, sx, sy int) []string {
	f := g.Floors[floor]
	type pt struct{ x, y int }
	seen := map[pt]bool{{sx, sy}: true}
	q := []pt{{sx, sy}}
	var out []string
	for len(q) > 0 {
		c := q[0]
		q = q[1:]
		for d := 0; d < 8; d++ {
			n := pt{c.x + MOVE[d][0], c.y + MOVE[d][1]}
			if seen[n] || !g.Tiles.Walkable(f, n.x, n.y) {
				continue
			}
			seen[n] = true
			if wp, warp := g.WarpAt(floor, n.x, n.y); warp {
				out = append(out, fmt.Sprintf("(%d,%d)→%d(%d,%d)", n.x, n.y, wp.ToFloor, wp.ToX, wp.ToY))
				continue
			}
			q = append(q, n)
		}
	}
	return out
}

// lerLoadFix：把 files（目的相对路径 → 来源档案）复制到暂存目录、再写一份 .create，用 data.LoadNPCs 读出来。
// 用来模拟"补上缺的 NPC / 模板之后"的情形，不动 gamedata。
func lerLoadFix(t *testing.T, files map[string]string, create string) []data.NPC {
	t.Helper()
	dir := t.TempDir()
	for dst, src := range files {
		b, err := os.ReadFile(src)
		if err != nil {
			t.Skipf("没有 %s：%v", src, err)
		}
		p := filepath.Join(dir, filepath.FromSlash(dst))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "fix.create"), []byte(create), 0o644); err != nil {
		t.Fatal(err)
	}
	ns, err := data.LoadNPCs(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ns
}

const lerSA = "../../../.." // C:\Users\Administrator\sa

type lerRig struct {
	t  *testing.T
	w  *World
	g  *data.Game
	cl *fakeClient
	id int
}

// talk：站到 NPC 旁边说话，窗口一路按"是"/"下一页"/"确定"走完（最多 20 个窗口），回传沿途的讯息。
func (r *lerRig) talk(n *entity) []proto.Message {
	r.t.Helper()
	r.w.Do(func() {
		p := r.w.players[r.id]
		p.window = nil
		r.w.placeOnFloor(p, n.floor, n.x+1, n.y, 1)
	})
	r.cl.take()
	win, all := exTalk(r.w, r.cl, r.id, n)
	for i := 0; i < 20 && win != nil; i++ {
		btn := BtnOK
		switch {
		case win.Buttons&BtnYes != 0:
			btn = BtnYes
		case win.Buttons&BtnNext != 0:
			btn = BtnNext
		}
		var msgs []proto.Message
		win, msgs = exReply(r.w, r.cl, r.id, n, win, btn)
		all = append(all, msgs...)
	}
	r.w.Do(func() {})
	return append(all, r.cl.take()...)
}

// warpman：和传送员说话按"是"，应传到 floor（-1 表示不该传送）。
func (r *lerRig) warpman(n *entity, floor int) *proto.EnterFloor {
	r.t.Helper()
	msgs := r.talk(n)
	ef, ok := find[*proto.EnterFloor](msgs)
	if floor < 0 {
		if ok {
			r.t.Fatalf("%s 不应传送：%+v", n.name, ef)
		}
		return nil
	}
	if !ok || ef.Floor != floor {
		r.t.Fatalf("%s 应传到 %d：%s", n.name, floor, fmtMsgs(msgs))
	}
	return ef
}

// step：踩地图上的传送点，应传到 floor。
func (r *lerRig) step(floor, x, y, want int) *proto.EnterFloor {
	r.t.Helper()
	ef, ok := find[*proto.EnterFloor](questStepOnto(r.t, r.w, r.g, r.cl, r.id, floor, x, y))
	if !ok || ef.Floor != want {
		r.t.Fatalf("%d (%d,%d) 的传送点应到 %d：%+v", floor, x, y, want, ef)
	}
	return ef
}

// fight：和战斗 NPC 说话、按"是"开打、打赢，应传到 want（0 表示不检查）。
func (r *lerRig) fight(n *entity, want int) []proto.Message {
	r.t.Helper()
	r.w.Do(func() {
		p := r.w.players[r.id]
		p.window = nil
		r.w.placeOnFloor(p, n.floor, n.x+1, n.y, 1)
	})
	r.cl.take()
	r.w.Talk(r.id, n.id)
	win, _ := find[*proto.NpcWindow](r.cl.take())
	if win == nil {
		r.t.Fatalf("%s（%s）应问要不要打", n.name, n.npc.Function)
	}
	r.w.NpcWindowReply(r.id, n.id, win.Seq, BtnYes, "")
	r.cl.take()
	msgs := npcEnemyWin(r.t, r.w, r.id, r.cl)
	r.w.Do(func() {})
	msgs = append(msgs, r.cl.take()...)
	if want != 0 {
		if ef, ok := find[*proto.EnterFloor](msgs); !ok || ef.Floor != want {
			r.t.Fatalf("打赢%s应传到 %d：%s", n.name, want, fmtMsgs(msgs))
		}
	}
	return msgs
}

func (r *lerRig) has(item, n int) {
	r.t.Helper()
	if got := exCount(r.w, r.id, item); got != n {
		r.t.Fatalf("道具 %d 应有 %d 个：%d", item, n, got)
	}
}

// flags：检查做完 / 进行中的旗标。done / now 里负数表示"不应有"。
func (r *lerRig) flags(step string, done, now []int) {
	r.t.Helper()
	r.w.Do(func() {
		p := r.w.players[r.id]
		for _, ev := range done {
			if ev > 0 && !p.eventDone(ev) || ev < 0 && p.eventDone(-ev) {
				r.t.Errorf("%s：ENDEV %d 不对（END=%q NOW=%q）", step, ev, p.char.EndEvents, p.char.NowEvents)
			}
		}
		for _, ev := range now {
			if ev > 0 && !p.eventNow(ev) || ev < 0 && p.eventNow(-ev) {
				r.t.Errorf("%s：NOWEV %d 不对（END=%q NOW=%q）", step, ev, p.char.EndEvents, p.char.NowEvents)
			}
		}
	})
	if r.t.Failed() {
		r.t.FailNow()
	}
}

// reach：断言地图上 from 走得到 NPC 旁边（1 格内）。
func (r *lerRig) reach(floor, sx, sy int, n *entity) {
	r.t.Helper()
	if n.floor != floor {
		r.t.Fatalf("%s 在 %d，不在 %d", n.name, n.floor, floor)
	}
	if !lerReach(r.g, floor, sx, sy, n.x, n.y, 1) {
		r.t.Errorf("%d：从 (%d,%d) 走不到 %s (%d,%d) 旁边", floor, sx, sy, n.name, n.x, n.y)
	}
}

// puzzle：在 60101 的九座神秘巨石像（第一 10_41 无石→真地石 19555，第二 10_39 真地→真水 19556，第四 10_35 真水→真火 19557，
// 第八 10_27 真火→真风 19558）换出真风石。
func (r *lerRig) puzzle() {
	r.t.Helper()
	for _, s := range []struct {
		key       string
		from, got int
	}{{"earth60101_10_41", 0, 19555}, {"earth60101_10_39", 19555, 19556}, {"earth60101_10_35", 19556, 19557}, {"earth60101_10_27", 19557, 19558}} {
		n := lerNPC(r.t, r.w, s.key)
		r.reach(60101, 6, 31, n)
		r.talk(n)
		if s.from != 0 {
			r.has(s.from, 0)
		}
		r.has(s.got, 1)
	}
}

func (r *lerRig) setTime(section int) {
	// saHour = (秒 mod 5400)·1024/5400；时段 1（早）取 SA 时 800，时段 0（夜）取 500
	hour := map[int]int{0: 500, 1: 800, 2: 50, 3: 250}[section]
	secs := int64(hour*saDay/1024 + 2)
	r.w.Do(func() {
		r.w.now = func() time.Time { return time.Unix(saEra+secs, 0) }
		if got := lsSection(r.w.saHour()); got != section {
			r.t.Fatalf("时段应为 %d：%d", section, got)
		}
	})
}

func lerSetup(t *testing.T, name string, level int, done []int) *lerRig {
	w, db, g := setup(t)
	for _, f := range []int{700, 708, 7000, 7021, 7107, 7129, 7133, 60040, 60045, 60080, 60082, 60101, 41, 2000,
		8513, 8514, 8515, 8516, 8518, 8519, 8520, 7217, 60215, 60337, 60313} {
		if g.Floors[f] == nil {
			t.Fatalf("没有 %d 号地图", f)
		}
	}
	for _, it := range []int{19550, 19551, 19555, 19556, 19557, 19558, 19559, 19560, 19561, 20191,
		20601, 20602, 20603, 20604, 20605, 20606, 20607, 20608, 20609, 20611, 20177, 20180, 20185} {
		if g.Items[it] == nil {
			t.Errorf("没有道具 %d", it)
		}
	}
	for _, en := range []int{1372, 2505, 2511, 2515, 2535, 1982, 1921} {
		if g.Battle.Enemies[en] == nil {
			t.Errorf("没有敌人 %d", en)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	c, cl := besideNPC(t, w, db, name, lerNPC(t, w, "earth7107_12_13"))
	r := &lerRig{t: t, w: w, g: g, cl: cl, id: int(c.ID)}
	w.Do(func() {
		p := w.players[r.id]
		p.char.Level = level
		p.char.Trans = 5
		for _, ev := range done {
			p.setEventDone(ev)
		}
	})
	return r
}

// 地精灵任务（eden3/earth、eden2/fight、eden1/zoo2、seimu/event，事件 171..174）。
// 没在这里验证、直接设旗标的前置：4、81（伊甸任务）、83（席格/拉斯基）、86、87、105（战士团）、32、33、34（梦见的仙人）。
func TestQuestEarthSpirit(t *testing.T) {
	r := lerSetup(t, "earthspirit", 100, []int{4, 81, 83, 86, 87, 105, 32, 33, 34})
	w, g := r.w, r.g

	// 171：沙尔特的助手（7107）要一只 Lv82 以上的白色玛恩摩洛斯（模板 264）。
	// 这只宠物来自吉鲁岛（300 号地图）的遇敌：encount.txt 第 571..574 行用 group 732（gil_w_26all_82/84），其中敌人 1372 gil_w_264_82/84。
	assistant := lerNPC(t, w, "earth7107_12_13")
	r.talk(assistant)
	r.flags("助手 REQUEST 171", nil, []int{171})
	if e := g.Battle.Enemies[1372]; e.TempNo != 264 || e.LvMin != 82 || e.PetFlag == 0 {
		t.Fatalf("敌人 1372 应是可捕获的 Lv82 模板 264：%+v", e)
	}
	givePet(t, w, r.id, g.Battle.Enemies[1372], 83)
	r.talk(assistant)
	r.flags("助手 ACCEPT 171", []int{171}, []int{-171})
	w.Do(func() {
		for _, pet := range w.players[r.id].pets.Pets {
			if pet != nil {
				t.Fatalf("大象应被收走：%+v", pet.Name)
			}
		}
	})

	// 172 开始：汉斯（7129）REQUEST 172，给启动石 19550。
	hans := lerNPC(t, w, "eden2/fight/fight-hans")
	r.talk(hans)
	r.flags("汉斯 REQUEST 172", nil, []int{172})
	r.has(19550, 1)

	// 173 开始：梦见的仙人（41）看到启动石，REQUEST 173。
	sage := lerNPC(t, w, "seimu/event/event01_2")
	r.talk(sage)
	r.flags("仙人 REQUEST 173", nil, []int{172, 173})

	// 708 的神秘巨石像：有 NOW172、NOW173、启动石 → 60101 (6,31)，收走启动石。
	statue := lerNPC(t, w, "earth708_229_253")
	if ef := r.warpman(statue, 60101); ef.X != 6 || ef.Y != 31 {
		t.Fatalf("巨石像应传到 60101 (6,31)：%+v", ef)
	}
	r.has(19550, 0)
	r.puzzle()
	// 传送机关（22,5）收真风石 → 60101 (15,32)；也看另一个机关（18,54）走不走得到
	gate := lerNPC(t, w, "earth60101_22_5")
	r.reach(60101, 6, 31, gate)
	if gate2 := lerNPC(t, w, "earth60101_36_55"); !lerReach(g, 60101, 6, 31, gate2.x, gate2.y, 1) {
		t.Logf("注意：从 (6,31) 走不到第二个传送机关 (%d,%d)", gate2.x, gate2.y)
	}
	if ef := r.warpman(gate, 60101); ef.X != 15 || ef.Y != 32 {
		t.Fatalf("传送机关应传到 60101 (15,32)：%+v", ef)
	}
	r.has(19558, 0)
	elf := lerNPC(t, w, "earth60101_47_32")
	r.reach(60101, 15, 32, elf)
	if lerReach(g, 60101, 6, 31, elf.x, elf.y, 1) {
		t.Logf("注意：60101 从 (6,31) 不经机关也走得到大地精灵")
	}
	// 174 开始：大地精灵 REQUEST 174（要 NOW172、NOW173）
	r.talk(elf)
	r.flags("大地精灵 REQUEST 174", []int{171}, []int{172, 173, 174})

	// 英嘉（7133）ACCEPT 172：给文件 19561，172 做完
	engua := lerNPC(t, w, "eden2/fight/fight-engua")
	r.talk(engua)
	r.flags("英嘉 ACCEPT 172", []int{172}, []int{-172, 173, 174})
	r.has(19561, 1)

	// 少年（60040）有 ENDEV=83&NOWEV=174 → 60045 (1,6)，席格 ACCEPT 174：收文件，174 做完
	boy := lerNPC(t, w, "zoo2/event83_17")
	if ef := r.warpman(boy, 60045); ef.X != 1 || ef.Y != 6 {
		t.Fatalf("少年应传到 60045 (1,6)：%+v", ef)
	}
	sig := lerNPC(t, w, "zoo2/event83_19")
	r.reach(60045, 1, 6, sig)
	r.talk(sig)
	r.flags("席格 ACCEPT 174", []int{172, 174}, []int{173, -174})
	r.has(19561, 0)

	// 汉斯（700 (103,127)）：END172、NOW173、END174，早上（TIME=1）→ 监牢 60080 (6,20)
	r.setTime(1)
	jailHans := lerNPC(t, w, "earth700_103_127")
	if ef := r.warpman(jailHans, 60080); ef.X != 6 || ef.Y != 20 {
		t.Fatalf("汉斯应传到 60080 (6,20)：%+v", ef)
	}
	guard := lerNPC(t, w, "earth60080_20_5")
	r.reach(60080, 6, 20, guard)
	if guard.npc.Function != "NPCEnemy" {
		t.Errorf("阻断：监牢警卫（add80/eden3/earth/earth.create，enemy=devilpk）没有 functionset（h5 缺 devilpk 模板，Function=%q），"+
			"跟它说话没反应，进不了 60082 / 60083，事件 173 做不完", guard.npc.Function)
		// 模拟修正：补上 8.0 的 party/devil/devil.template（devilpk → NPCEnemy）后再读这两个警卫
		ns := lerLoadFix(t, map[string]string{
			"devil.template":                    lerSA + "/serv/data-8.0/npc/party/devil/devil.template",
			"add80/eden3/earth/earth60080_20_5": "../../../gamedata/npc/add80/eden3/earth/earth60080_20_5",
		}, "NPCCREATE\n{\nname=guard\nfloorid=60080\nborncenter=20,5\ndir=6\ntime=0\nignoreinvincible=1\ncreatenum=1\n"+
			"enemy=devilpk|file:add80/eden3/earth/earth60080_20_5\ngraphicname=100439\n}\n")
		if len(ns) != 1 || ns[0].Function != "NPCEnemy" {
			t.Fatalf("补模板后应是 NPCEnemy：%+v", ns)
		}
		w.Do(func() { guard.npc = &ns[0] })
	}
	r.fight(guard, 60082)
	// 团长亚克（60082）ACCEPT 173：随机给一件战士团装备，173 做完；再说话给威灵石 19560
	chief := lerNPC(t, w, "earth60082_13_10")
	r.reach(60082, 6, 20, chief)
	r.talk(chief)
	r.flags("团长亚克 ACCEPT 173", []int{171, 172, 173, 174}, []int{-171, -172, -173, -174})
	r.talk(chief)
	r.has(19560, 1)

	// 收尾：汉斯给启动石 → 巨石像 → 机关谜题 → 大地精灵收威灵石给灵石（2282..2284 之一）
	r.talk(hans)
	r.has(19550, 1)
	r.warpman(statue, 60101)
	r.puzzle()
	r.warpman(gate, 60101)
	r.talk(elf)
	r.has(19560, 0)
	if exCount(w, r.id, 2282)+exCount(w, r.id, 2283)+exCount(w, r.id, 2284) != 1 {
		t.Fatal("大地精灵应给 2282..2284 之一")
	}
}

// 雷尔任务（sa75/ler，事件 175、176）。
// 直接设旗标的前置：4、81、174（上面 TestQuestEarthSpirit 验证）、171..173、83、86、87、105（重进地精灵结界要的）、
// 116（水精灵）、110、111（火精灵）、112..115（风精灵；115 由 TestQuestLumberjack 验证）、63、136、52（五转）。
func TestQuestLer(t *testing.T) {
	r := lerSetup(t, "ler", 120, []int{4, 81, 83, 86, 87, 105, 171, 172, 173, 174, 116, 110, 111, 112, 113, 114, 115, 63, 136, 52})
	w, g := r.w, r.g
	giveItem(w, r.id, 20180) // 116 的奖励水魔戒（没有的话阿葵丽雅先补发戒指）
	giveItem(w, r.id, 20177) // 111 的奖励火魔法戒
	giveItem(w, r.id, 20185) // 115 的奖励风魔四戒

	// 伊甸园 7000 (137,97) 的传送点：没旗标 → 7021 雷尔的屋子
	ef := r.step(7000, 137, 97, 7021)
	ler := lerNPC(t, w, "sa75/ler/ler1.arg")
	r.reach(7021, ef.X, ef.Y, ler)
	r.talk(ler)
	r.flags("雷尔 REQUEST 175", nil, []int{175})

	// 四大精灵各给一半的力量
	hans := lerNPC(t, w, "eden2/fight/fight-hans")
	statue := lerNPC(t, w, "earth708_229_253")
	gate := lerNPC(t, w, "earth60101_22_5")
	elf := lerNPC(t, w, "earth60101_47_32")
	r.talk(hans)
	r.has(19550, 1)
	r.warpman(statue, 60101)
	r.puzzle()
	if ef := r.warpman(gate, 60101); ef.X != 15 || ef.Y != 32 {
		t.Fatalf("NOW175 时传送机关应到 60101 (15,32)：%+v", ef)
	}
	r.talk(elf)
	r.has(20601, 1)

	ef = r.step(7217, 15, 13, 60215)
	aqua := lerNPC(t, w, "eden3/princess/aqua")
	r.reach(60215, ef.X, ef.Y, aqua)
	r.talk(aqua)
	r.has(20602, 1)

	fireGuard := lerNPC(t, w, "eden3/warrior/guard")
	ef = r.warpman(fireGuard, 60313)
	sara := lerNPC(t, w, "eden3/warrior/saramanda")
	r.reach(60313, ef.X, ef.Y, sara)
	r.talk(sara)
	r.has(20603, 1)

	windy := lerNPC(t, w, "eden3/lumberjack/windy")
	r.talk(windy)
	r.has(20604, 1)
	r.flags("四种力量", nil, []int{175, -176})

	// 带着四种力量：7000 (137,97) → 8518，雷尔收下力量 → 8513 地牢 (23,9)
	ef = r.step(7000, 137, 97, 8518)
	ler2 := lerNPC(t, w, "sa75/ler/lerwarp1.arg")
	r.reach(8518, ef.X, ef.Y, ler2)
	if ef := r.warpman(ler2, 8513); ef.X != 23 || ef.Y != 9 {
		t.Fatalf("雷尔应传到 8513 (23,9)：%+v", ef)
	}
	for _, it := range []int{20601, 20602, 20603, 20604} {
		r.has(it, 0)
	}
	// 亚巴 REQUEST 176：给加料大福 20611
	aba := lerNPC(t, w, "sa75/ler/ler2.arg")
	r.reach(8513, 23, 9, aba)
	r.talk(aba)
	r.flags("亚巴 REQUEST 176", nil, []int{175, 176})
	r.has(20611, 1)
	// 老鼠：拿大福换钥匙 20609
	rat := lerNPC(t, w, "sa75/ler/lerwarp3.arg")
	r.reach(8513, 23, 9, rat)
	r.warpman(rat, -1)
	r.has(20611, 0)
	r.has(20609, 1)
	// 牢门 → (23,18)
	door := lerNPC(t, w, "sa75/ler/lerwarp2.arg")
	r.reach(8513, 23, 9, door)
	if ef := r.warpman(door, 8513); ef.X != 23 || ef.Y != 18 {
		t.Fatalf("牢门应传到 8513 (23,18)：%+v", ef)
	}
	t.Logf("8513 出牢门后走得到的传送点：%v", lerExits(g, 8513, 23, 18))

	// 四个黑暗使者：地（60101 机关 → 8514）
	r.talk(hans)
	r.has(19550, 1)
	r.warpman(statue, 60101)
	r.puzzle()
	if ef := r.warpman(gate, 8514); ef.X != 15 || ef.Y != 32 {
		t.Fatalf("NOW175、NOW176、没有地骨头时传送机关应到 8514 (15,32)：%+v", ef)
	}
	pk1 := lerNPC(t, w, "sa75/ler/lerpk1.arg")
	r.reach(8514, 15, 32, pk1)
	r.fight(pk1, 60101)
	r.has(20605, 1)
	// 水（7217 的传送点 → 8515）
	ef = r.step(7217, 15, 13, 8515)
	pk2 := lerNPC(t, w, "sa75/ler/lerpk2.arg")
	r.reach(8515, ef.X, ef.Y, pk2)
	r.fight(pk2, 60215)
	r.has(20606, 1)
	// 火（柯亚 → 8516）
	ef = r.warpman(fireGuard, 8516)
	pk3 := lerNPC(t, w, "sa75/ler/lerpk3.arg")
	r.reach(8516, ef.X, ef.Y, pk3)
	r.fight(pk3, 60313)
	r.has(20607, 1)
	// 风：8.0 的 ler.create 在玛丽娜丝渔村 2000 (48,71) 放了"风精灵"（sb_dou，lerpk4.arg：打赢给风骨头 20608、传到 2000 (48,72)）
	var pk4 *entity
	w.Do(func() {
		for _, e := range w.floors[2000] {
			if e.npc != nil && e.npc.Function == "NPCEnemy" && strings.Contains(e.npc.Arg, "20608") {
				pk4 = e
			}
		}
	})
	if pk4 == nil {
		t.Errorf("阻断：没有风之黑暗使者（8.0 sa75/ler/ler.create 的 风精灵 2000 (48,71) sb_dou|file:sa75/ler/lerpk4.arg 没补进来），" +
			"风骨头 20608 拿不到，四种骨头凑不齐，事件 175 做不完")
		// 模拟修正：照 8.0 的那一块补上（sb_dou 用 h5 doujyou/dou.template 的 NPCEnemy）
		ns := lerLoadFix(t, map[string]string{
			"add80/sa75/ler/lerpk4.arg": lerSA + "/serv/data-8.0/npc/sa75/ler/lerpk4.arg",
			"dou.template":              "../../../gamedata/npc/doujyou/dou.template",
		}, "NPCCREATE\n{\nname=wind\nfloorid=2000\nborncenter=48,71\ndir=4\ntime=1\nignoreinvincible=1\ncreatenum=1\n"+
			"enemy=sb_dou|file:add80/sa75/ler/lerpk4.arg\ngraphicname=16950\n}\n")
		if len(ns) != 1 || ns[0].Function != "NPCEnemy" {
			t.Fatalf("模拟补上的风之黑暗使者读不出来：%+v", ns)
		}
		w.Do(func() {
			maxID := 0
			for _, es := range w.floors {
				for id := range es {
					maxID = max(maxID, id)
				}
			}
			n := ns[0]
			pk4 = &entity{id: maxID + 1000, kind: "npc", name: n.Name, sprite: n.Sprite, floor: 2000, x: n.X, y: n.Y, dir: n.Dir, npc: &n}
			w.addToFloor(pk4)
		})
	}
	r.reach(2000, windy.x+1, windy.y, pk4)
	r.fight(pk4, 2000)
	r.has(20608, 1)

	// 四种骨头交给弗斯温蒂（地 / 水 / 火精灵也收）：给四精灵戒指 20191，175 做完
	r.talk(windy)
	for _, it := range []int{20605, 20606, 20607, 20608} {
		r.has(it, 0)
	}
	r.has(20191, 1)
	r.flags("交四种骨头", []int{175}, []int{-175, 176})

	// 7000 (137,97)：END175、NOW176 → 8519，打赢雷尔 → 8520 (30,25)
	ef = r.step(7000, 137, 97, 8519)
	pk5 := lerNPC(t, w, "sa75/ler/lerpk5.arg")
	r.reach(8519, ef.X, ef.Y, pk5)
	r.fight(pk5, 8520)
	// 魔界之门 ACCEPT 176：给哈利恩斯（2515），176 做完
	gateOfHell := lerNPC(t, w, "sa75/ler/ler7.arg")
	r.reach(8520, 30, 25, gateOfHell)
	r.talk(gateOfHell)
	r.flags("魔界之门 ACCEPT 176", []int{175, 176}, []int{-175, -176})
	got := false
	w.Do(func() {
		for _, pet := range w.players[r.id].pets.Pets {
			if pet != nil {
				got = true
			}
		}
	})
	if !got {
		t.Fatal("魔界之门应给宠物 2515")
	}
	// 之后 7000 (137,97) → 8520
	r.step(7000, 137, 97, 8520)
}
