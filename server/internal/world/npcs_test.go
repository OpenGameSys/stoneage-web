package world

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"sa/server/internal/data"
	"sa/server/internal/proto"
	"sa/server/internal/store"
)

// findNPC 找功能为 fn、参数（或参数文件名）含 key 的 NPC。
func findNPC(t *testing.T, w *World, fn, key string) *entity {
	t.Helper()
	var found *entity
	w.Do(func() {
		for _, es := range w.floors {
			for _, e := range es {
				// 补进来的 8.0 NPC（gamedata/npc/add80）可能用同名参数文件、内容却不同：测试要的是原数据那个，add80 的只当备胎
				if e.npc != nil && e.npc.Function == fn && (strings.Contains(strings.Join(e.npc.Args, "|"), key) || strings.Contains(e.npc.Arg, key)) {
					if strings.Contains(strings.Join(e.npc.Args, "|"), "add80/") {
						if found == nil {
							found = e
						}
						continue
					}
					found = e
					return
				}
			}
		}
	})
	if found == nil {
		t.Skipf("数据里没有 %s（%s）", fn, key)
	}
	return found
}

// besideNPC 让一个新角色上线并站到 n 右边一格（世界不检查站的格子能不能走，测试只看对话流程）。
func besideNPC(t *testing.T, w *World, db store.Store, name string, n *entity) (store.Character, *fakeClient) {
	t.Helper()
	acc, _ := db.CreateAccount(context.Background(), name, "pw", "")
	c, err := db.CreateCharacter(context.Background(), store.Character{AccountID: acc.ID, Name: name, Sprite: 100000, Level: 1,
		Floor: data.Hometowns[0].Floor, X: data.Hometowns[0].X, Y: data.Hometowns[0].Y, Vital: 5, Str: 5, Tgh: 5, Dex: 5})
	if err != nil {
		t.Fatal(err)
	}
	cl := &fakeClient{}
	w.Enter(c, cl)
	w.Do(func() { w.placeOnFloor(w.players[int(c.ID)], n.floor, n.x+1, n.y, 2) })
	cl.take()
	return c, cl
}

func giveItem(w *World, charID, itemID int) {
	w.Do(func() {
		e := w.players[charID]
		w.addItem(e.inv, w.newItem(w.game.Items[itemID]))
	})
}

// 传送员（wpm_800_34_23：FREE:LV>0）：说话 → FreeMsg 的是/否窗口 → 按"是"传送到 WARP 指定的位置；按"否"不动。
func TestWarpManFree(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "WarpMan", "wpm_800_34_23")
	if g.Floors[811] == nil {
		t.Skip("没有 811 号地图")
	}
	c, cl := besideNPC(t, w, db, "warper", n)
	w.Talk(int(c.ID), n.id)
	win, ok := find[*proto.NpcWindow](cl.take())
	if !ok || win.Buttons != BtnYesNo || !strings.Contains(win.Text, "要从这里到1楼吗") || strings.Contains(win.Text, `\n`) {
		t.Fatalf("应显示 FreeMsg 的是/否窗口：%+v", win)
	}
	w.NpcWindowReply(int(c.ID), n.id, win.Seq, BtnNo, "")
	if _, moved := find[*proto.EnterFloor](cl.take()); moved {
		t.Fatal("按否不应传送")
	}
	w.Talk(int(c.ID), n.id)
	win, _ = find[*proto.NpcWindow](cl.take())
	w.NpcWindowReply(int(c.ID), n.id, win.Seq, BtnYes, "")
	ef, ok := find[*proto.EnterFloor](cl.take())
	if !ok || ef.Floor != 811 || ef.X != 32 || ef.Y != 26 {
		t.Fatalf("应传送到 811(32,26)：%+v", ef)
	}
}

// 斗技场入口（wpm_1000_56_59：FREE:ITEM=2597，没门票时 PayMsg 带 %4d 且 MONEY:-1 → 只有 OK）：
// 没门票只能看说明（伪造"是"也不传送）；有门票 → 是/否 → 交出其它门票（DelItem）后传送，门票 A 留着。
func TestWarpManTicket(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "WarpMan", "wpm_1000_56_59")
	if g.Items[2597] == nil || g.Items[2598] == nil {
		t.Skip("没有门票道具")
	}
	c, cl := besideNPC(t, w, db, "fighter", n)
	w.Talk(int(c.ID), n.id)
	win, ok := find[*proto.NpcWindow](cl.take())
	if !ok || win.Buttons != BtnOK || !strings.Contains(win.Text, "没有门票") {
		t.Fatalf("没门票应只显示说明：%+v", win)
	}
	w.NpcWindowReply(int(c.ID), n.id, win.Seq, BtnYes, "")
	if _, moved := find[*proto.EnterFloor](cl.take()); moved {
		t.Fatal("OK 窗口回传\"是\"不应传送")
	}
	giveItem(w, int(c.ID), 2597)
	giveItem(w, int(c.ID), 2598)
	w.Talk(int(c.ID), n.id)
	win, ok = find[*proto.NpcWindow](cl.take())
	if !ok || win.Buttons != BtnYesNo || !strings.Contains(win.Text, "欢迎来到斗技场") || strings.Contains(win.Text, "%4d") {
		t.Fatalf("有门票应显示 FreeMsg（%%4d 换成人数）：%+v", win)
	}
	w.NpcWindowReply(int(c.ID), n.id, win.Seq, BtnYes, "")
	msgs := cl.take()
	if ef, ok := find[*proto.EnterFloor](msgs); !ok || ef.Floor != 130 || ef.X != 57 || ef.Y != 13 {
		t.Fatalf("应传送到斗技场 130(57,13)：%+v", msgs)
	}
	var a, b int
	w.Do(func() { e := w.players[int(c.ID)]; a, b = w.countItem(e, 2597), w.countItem(e, 2598) })
	if a != 1 || b != 0 {
		t.Fatalf("应交出门票 B、留下门票 A：A=%d B=%d", a, b)
	}
}

// 收费的传送员（wpm_40_12_18：FREE 空、MONEY:300，PayMsg 带 %8d）：钱不够显示 MoneyMsg，够了扣钱传送。
// 道场（wpm_1020_18_12：MONEY:LV*5）的价钱按等级算。
func TestWarpManPay(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "WarpMan", "wpm_40_12_18")
	if g.Floors[31501] == nil {
		t.Skip("没有 31501 号地图")
	}
	c, cl := besideNPC(t, w, db, "payer", n)
	w.Do(func() { w.players[int(c.ID)].char.Gold = 299 })
	w.Talk(int(c.ID), n.id)
	win, ok := find[*proto.NpcWindow](cl.take())
	if !ok || win.Buttons != BtnYesNo || !strings.Contains(win.Text, "要有这个     300s") {
		t.Fatalf("应显示付费的是/否窗口（%%8d 换成价钱）：%+v", win)
	}
	w.NpcWindowReply(int(c.ID), n.id, win.Seq, BtnYes, "")
	msgs := cl.take()
	if _, moved := find[*proto.EnterFloor](msgs); moved {
		t.Fatal("钱不够不应传送")
	}
	if e, ok := find[*proto.NpcWindow](msgs); !ok || e.Buttons != BtnOK || !strings.Contains(e.Text, "你钱不够哦") {
		t.Fatalf("钱不够应显示 MoneyMsg：%+v", msgs)
	}
	w.Do(func() { w.players[int(c.ID)].char.Gold = 305 })
	w.Talk(int(c.ID), n.id)
	win, _ = find[*proto.NpcWindow](cl.take())
	w.NpcWindowReply(int(c.ID), n.id, win.Seq, BtnYes, "")
	msgs = cl.take()
	inv, ok := find[*proto.Inventory](msgs)
	if ef, moved := find[*proto.EnterFloor](msgs); !moved || ef.Floor != 31501 || !ok || inv.Gold != 5 {
		t.Fatalf("付钱后应传送并扣掉 300 石币：%+v", msgs)
	}

	dojo := findNPC(t, w, "WarpMan", "wpm_1020_18_12")
	c2, cl2 := besideNPC(t, w, db, "dojo", dojo)
	w.Do(func() { e := w.players[int(c2.ID)]; e.char.Level = 12 })
	w.Talk(int(c2.ID), dojo.id)
	if win, ok := find[*proto.NpcWindow](cl2.take()); !ok || !strings.Contains(win.Text, "      60个石头") {
		t.Fatalf("道场入场费应为 等级×5 = 60：%+v", win)
	}
}

// 暗语传送员（warp_msg）：点选时随便说一句 nomal_msg；对着它说出暗语才传送。
func TestWarpManPassword(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "WarpMan", "wpm_100_443_127") // 暗语"在卡坦有梦德"、FREE:LV>0
	word, _ := data.ArgField(n.npc.Arg, "warp_msg")
	warp, _ := data.ArgField(n.npc.Arg, "WARP")
	to := strings.Split(warp, ",")
	if g.Floors[data.Atoi(to[0])] == nil {
		t.Skip("暗语传送员的目标地图不存在")
	}
	c, cl := besideNPC(t, w, db, "secret", n)
	w.Talk(int(c.ID), n.id)
	if nt, ok := find[*proto.NpcTalk](cl.take()); !ok || nt.Window {
		t.Fatalf("点选时应说 nomal_msg：%+v", nt)
	}
	w.Chat(int(c.ID), "near", word, 0, 3)
	if ef, ok := find[*proto.EnterFloor](cl.take()); !ok || ef.Floor != data.Atoi(to[0]) {
		t.Fatalf("说出暗语应传送到 %s：%+v", warp, ef)
	}
}

// 万事通（sainasu/manual.conf）：选单第 2 行"关于操作的方法" → 窗口 1000 → 第 2 行"关于移动的方法" → 窗口 1010（下一页）→ 1011。
func TestWindowman(t *testing.T) {
	w, db, _ := setup(t)
	n := findNPC(t, w, "Windowman", "sainasu/manual.conf")
	c, cl := besideNPC(t, w, db, "student", n)
	w.Talk(int(c.ID), n.id)
	win, ok := find[*proto.NpcWindow](cl.take())
	if !ok || win.Kind != "select" || win.Seq != 101 || !strings.HasPrefix(win.Text, "2\n我是这个村子里的万事通啦！") {
		t.Fatalf("应打开第 1 个窗口（选单）：%+v", win)
	}
	w.NpcWindowReply(int(c.ID), n.id, win.Seq, 0, "2")
	win, ok = find[*proto.NpcWindow](cl.take())
	if !ok || win.Seq != 1100 || !strings.Contains(win.Text, "关于操作吗") {
		t.Fatalf("选第 2 行应打开窗口 1000：%+v", win)
	}
	w.NpcWindowReply(int(c.ID), n.id, win.Seq, 0, "2")
	win, ok = find[*proto.NpcWindow](cl.take())
	if !ok || win.Seq != 1110 || win.Kind != "message" || win.Buttons != BtnNext {
		t.Fatalf("应打开窗口 1010（下一页）：%+v", win)
	}
	w.NpcWindowReply(int(c.ID), n.id, win.Seq, BtnNext, "")
	if win, ok = find[*proto.NpcWindow](cl.take()); !ok || win.Seq != 1111 {
		t.Fatalf("下一页应打开窗口 1011：%+v", win)
	}
	// 第 1 行空白对应的 selected 没有去处（gotowin 为空），什么都不发生
	w.Talk(int(c.ID), n.id)
	win, _ = find[*proto.NpcWindow](cl.take())
	w.NpcWindowReply(int(c.ID), n.id, win.Seq, 0, "1")
	if _, again := find[*proto.NpcWindow](cl.take()); again {
		t.Fatal("没有去处的选项不应打开窗口")
	}
}

// 记录点（sp_1000_94_99：ID 4，要一块肉）：没肉时 RequestMsg；有肉 → RealyMsg 是/否 → 收走肉、登记、OkMsg；
// 之后回记录点回到 Born 的位置，再说话直接记录并报开机时间与人数。
func TestSavePoint(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "SavePoint", "sp_1000_94_99")
	if g.Items[1930] == nil {
		t.Skip("没有 1930 号道具")
	}
	c, cl := besideNPC(t, w, db, "saver", n)
	w.Talk(int(c.ID), n.id)
	win, ok := find[*proto.NpcWindow](cl.take())
	if !ok || win.Buttons != BtnOK || !strings.Contains(win.Text, "凯比特的肉") {
		t.Fatalf("没带肉应显示 RequestMsg：%+v", win)
	}
	giveItem(w, int(c.ID), 1930)
	w.Talk(int(c.ID), n.id)
	win, ok = find[*proto.NpcWindow](cl.take())
	if !ok || win.Buttons != BtnYesNo {
		t.Fatalf("带了肉应显示 RealyMsg 的是/否窗口：%+v", win)
	}
	w.NpcWindowReply(int(c.ID), n.id, win.Seq, BtnYes, "")
	if done, ok := find[*proto.NpcWindow](cl.take()); !ok || !strings.Contains(done.Text, "自由进出") {
		t.Fatalf("应显示 OkMsg：%+v", done)
	}
	var meat int
	var ch store.Character
	w.Do(func() { e := w.players[int(c.ID)]; meat, ch = w.countItem(e, 1930), e.char })
	if meat != 0 || ch.Elder != 4 || ch.SavePoints&(1<<4) == 0 {
		t.Fatalf("肉应被收走、记录点设为 4：肉=%d %+v", meat, ch)
	}
	w.ReturnHome(int(c.ID))
	if ef, ok := find[*proto.EnterFloor](cl.take()); !ok || ef.Floor != 1000 || ef.X != 92 || ef.Y != 99 {
		t.Fatalf("回记录点应到 1000(92,99)：%+v", ef)
	}
	w.Do(func() { w.placeOnFloor(w.players[int(c.ID)], n.floor, n.x+1, n.y, 2) })
	cl.take()
	w.Talk(int(c.ID), n.id)
	if nt, ok := find[*proto.NpcTalk](cl.take()); !ok || !strings.Contains(nt.Text, "saver已经记录了") || !strings.Contains(nt.Text, "目前线上人数为 1 人") {
		t.Fatalf("登记过的记录点应直接记录：%+v", nt)
	}
	// 存档里也有
	w.Do(func() {})
	time.Sleep(50 * time.Millisecond)
	if saved, _ := db.GetCharacter(context.Background(), c.AccountID, c.ID); saved.Elder != 4 {
		t.Fatalf("记录点应存档：%+v", saved)
	}
}

// 按时间出现的人：time 的时段外换成 change_no 的图（没写就消失、不能说话），时段内说 main_msg。
func TestTimeMan(t *testing.T) {
	w, db, _ := setup(t)
	n := findNPC(t, w, "TimeMan", "time:ALLNIGHT")
	hourAt := func(h int) time.Time { return time.Unix(saEra+int64(h)*saDay/1024+1, 0) }
	c, cl := besideNPC(t, w, db, "owl", n)
	set := func(h int) {
		w.Do(func() {
			w.now = func() time.Time { return hourAt(h) }
			w.updateTimeMen()
		})
	}
	set(500) // 夜里：出现
	cl.take()
	w.Talk(int(c.ID), n.id)
	if nt, ok := find[*proto.NpcTalk](cl.take()); !ok || nt.Text == "" {
		t.Fatalf("夜里应说 main_msg：%+v", nt)
	}
	set(100) // 白天：ALLNIGHT 的人不在
	if _, ok := find[*proto.ObjDisappear](cl.take()); !ok {
		alt, _ := data.ArgField(n.npc.Arg, "change_no")
		if alt == "" {
			t.Fatal("白天应从视野里消失")
		}
	}
	w.Talk(int(c.ID), n.id)
	if _, ok := find[*proto.NpcTalk](cl.take()); ok && n.hidden() {
		t.Fatal("消失时不应说话")
	}
	set(400)
	if ap, ok := find[*proto.ObjAppear](cl.take()); n.hidden() || (!ok && ap == nil) {
		t.Fatal("夜里应重新出现")
	}
}

// 和数据里每一个传送员、万事通、记录点、按时间出现的人说话，窗口按每种按钮与前几行选项回一次：不应出错。
func TestNpcDialogSweep(t *testing.T) {
	w, db, _ := setup(t)
	var npcs []*entity
	w.Do(func() {
		for _, es := range w.floors {
			for _, e := range es {
				if e.npc != nil && strings.Contains("WarpMan Windowman SavePoint TimeMan Dengon Bankman Charm LuckyMan Janken PoolItemShop Quiz ItemchangeMan Action", e.npc.Function) && e.npc.Function != "" {
					npcs = append(npcs, e)
				}
			}
		}
	})
	c, cl := besideNPC(t, w, db, "sweeper", npcs[0])
	windows := 0
	for _, n := range npcs {
		for _, reply := range []struct {
			button int
			data   string
		}{{BtnYes, ""}, {BtnNo, ""}, {BtnOK, ""}, {BtnNext, ""}, {0, "1"}, {0, "2"}, {0, "3"}} {
			w.Do(func() {
				e := w.players[int(c.ID)]
				e.char.Gold, e.char.Level = 1000, 80
				w.placeOnFloor(e, n.floor, n.x+1, n.y, 2)
			})
			cl.take()
			w.Talk(int(c.ID), n.id)
			if win, ok := find[*proto.NpcWindow](cl.take()); ok {
				windows++
				w.NpcWindowReply(int(c.ID), n.id, win.Seq, reply.button, reply.data)
			}
		}
	}
	if windows == 0 {
		t.Fatal("应至少打开一些窗口")
	}
	t.Logf("%d 个 NPC，打开 %d 次窗口", len(npcs), windows)
}

// 隔着柜台的护士（参数第 4 段 range 2）：站在两格外面向她也能说话；range 1 的护士要相邻。
func TestNurseAcrossCounter(t *testing.T) {
	w, db, _ := setup(t)
	var n *entity
	w.Do(func() {
		for _, es := range w.floors {
			for _, e := range es {
				if e.npc != nil && e.npc.Function == "WindowHealer" && healerRange(e) == 2 {
					n = e
					return
				}
			}
		}
	})
	if n == nil {
		t.Skip("没有 range 2 的护士")
	}
	c, cl := besideNPC(t, w, db, "counter", n)
	id := int(c.ID)
	w.Do(func() { w.placeOnFloor(w.players[id], n.floor, n.x+2, n.y, 2) })
	cl.take()
	w.Talk(id, n.id)
	if win, ok := find[*proto.NpcWindow](cl.take()); !ok || win.Kind != "select" {
		t.Fatal("隔着柜台两格应能和护士说话")
	}
	w.Do(func() { w.placeOnFloor(w.players[id], n.floor, n.x+3, n.y, 2) })
	cl.take()
	w.Talk(id, n.id)
	if _, ok := find[*proto.NpcWindow](cl.take()); ok {
		t.Fatal("三格外不应能说话")
	}
}

// 护士收费（NPC_WindowCostCheck）：等级到参数第 1 段以上时，耐久力回复收 等级 × 第 2 段 的石币；钱不够不回复。
func TestNurseFee(t *testing.T) {
	w, db, _ := setup(t)
	var n *entity
	w.Do(func() {
		for _, es := range w.floors {
			for _, e := range es {
				if e.npc != nil && e.npc.Function == "WindowHealer" && len(e.npc.Args) >= 2 && data.Atoi(e.npc.Args[0]) == 10 {
					n = e
					return
				}
			}
		}
	})
	if n == nil {
		t.Skip("没有收费等级 10 的护士")
	}
	_, hr, _ := healerRates(n)
	want := max(1, int(20*hr))
	c, cl := besideNPC(t, w, db, "patient", n)
	id := int(c.ID)
	w.Do(func() {
		p := w.players[id]
		p.char.Level, p.char.HP, p.char.Gold = 20, 3, want-1
		w.placeOnFloor(p, n.floor, n.x+1, n.y, 2)
	})
	cl.take()
	ask := func() *proto.NpcWindow {
		w.Talk(id, n.id)
		win, _ := find[*proto.NpcWindow](cl.take())
		w.NpcWindowReply(id, n.id, win.Seq, 0, "2")
		win, _ = find[*proto.NpcWindow](cl.take())
		return win
	}
	win := ask()
	if win == nil || !strings.Contains(win.Text, fmt.Sprintf("收您%d的STONE", want)) {
		t.Fatalf("应显示要收 %d 石币：%+v", want, win)
	}
	w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
	if done, _ := find[*proto.NpcWindow](cl.take()); done == nil || !strings.Contains(done.Text, "金钱不够") {
		t.Fatalf("钱不够应拒绝：%+v", done)
	}
	w.Do(func() { w.players[id].char.Gold = want + 5 })
	win = ask()
	w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
	cl.take()
	w.Do(func() {
		p := w.players[id]
		if p.char.HP != -1 || p.char.Gold != 5 {
			t.Errorf("付钱后应回复：HP %d 石币 %d", p.char.HP, p.char.Gold)
		}
	})
}
