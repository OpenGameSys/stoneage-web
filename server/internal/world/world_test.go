package world

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"sa/server/internal/battle"
	"sa/server/internal/data"
	"sa/server/internal/proto"
	"sa/server/internal/store"
)

type fakeClient struct {
	mu   sync.Mutex
	msgs []proto.Message
}

func (c *fakeClient) Send(m proto.Message) { c.mu.Lock(); c.msgs = append(c.msgs, m); c.mu.Unlock() }
func (c *fakeClient) Kick(string)          {}
func (c *fakeClient) take() []proto.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.msgs
	c.msgs = nil
	return m
}

func find[T proto.Message](msgs []proto.Message) (T, bool) {
	for _, m := range msgs {
		if v, ok := m.(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}

var gameOnce struct {
	sync.Once
	g   *data.Game
	err error
}

func setup(t *testing.T) (*World, store.Store, *data.Game) {
	t.Helper()
	const dir = "../../../gamedata"
	if _, err := os.Stat(dir); err != nil {
		t.Skip("没有原版数据")
	}
	gameOnce.Do(func() { gameOnce.g, gameOnce.err = data.Load(dir) })
	if gameOnce.err != nil {
		t.Fatal(gameOnce.err)
	}
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	w := New(gameOnce.g, db, slog.New(slog.NewTextHandler(io.Discard, nil)), 1)
	ctx, cancel := context.WithCancel(context.Background())
	go w.Run(ctx)
	t.Cleanup(func() { cancel(); db.Close() })
	return w, db, gameOnce.g
}

func newChar(t *testing.T, db store.Store, name string, floor, x, y int) store.Character {
	a, err := db.CreateAccount(context.Background(), name, "pw", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := db.CreateCharacter(context.Background(), store.Character{AccountID: a.ID, Name: name, Sprite: 100000, Level: 1, Floor: floor, X: x, Y: y})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// 在 floor 上找一个从 (x,y) 出发、方向 d 可走的格子
func walkableStep(g *data.Game, f *data.Floor, x, y int) (int, bool) {
	for d := 0; d < 8; d++ {
		if g.Tiles.Walkable(f, x+MOVE[d][0], y+MOVE[d][1]) {
			if _, warp := g.WarpAt(f.ID, x+MOVE[d][0], y+MOVE[d][1]); !warp {
				return d, true
			}
		}
	}
	return 0, false
}

func TestEnterSeeMoveChat(t *testing.T) {
	w, db, g := setup(t)
	h := data.Hometowns[0]
	f := g.Floors[h.Floor]
	a, b := newChar(t, db, "alice", h.Floor, h.X, h.Y), newChar(t, db, "bob", h.Floor, h.X, h.Y)
	ca, cb := &fakeClient{}, &fakeClient{}

	w.Enter(a, ca)
	msgs := ca.take()
	if ef, ok := find[*proto.EnterFloor](msgs); !ok || ef.Floor != h.Floor || ef.X != h.X || ef.Name != f.Name {
		t.Fatalf("上线应收到 EnterFloor：%+v", msgs)
	}
	w.Enter(b, cb)
	if ap, ok := find[*proto.ObjAppear](ca.take()); !ok || ap.Objs[0].Name != "bob" {
		t.Fatal("alice 应看到 bob 出现")
	}
	if ap, ok := find[*proto.ObjAppear](cb.take()); !ok {
		t.Fatal("bob 应看到 alice")
	} else {
		names := map[string]bool{}
		for _, o := range ap.Objs {
			names[o.Name] = true
		}
		if !names["alice"] {
			t.Fatalf("bob 的初始视野应包含 alice：%+v", ap.Objs)
		}
	}

	// 合法的一步：bob 收到 ObjMove
	d, ok := walkableStep(g, f, h.X, h.Y)
	if !ok {
		t.Fatal("出生点周围没有可走格")
	}
	w.Move(int(a.ID), h.X, h.Y, d)
	if mv, ok := find[*proto.ObjMove](cb.take()); !ok || mv.X != h.X+MOVE[d][0] || mv.Y != h.Y+MOVE[d][1] {
		t.Fatal("bob 应收到 alice 的移动")
	}
	// 起点不对 → 校正
	w.Move(int(a.ID), h.X+5, h.Y, d)
	if pc, ok := find[*proto.PosCorrect](ca.take()); !ok || pc.X != h.X+MOVE[d][0] {
		t.Fatal("起点不符应收到 PosCorrect")
	}

	w.Chat(int(a.ID), "near", "你好", 0, 3)
	if cm, ok := find[*proto.ChatMsg](cb.take()); !ok || cm.Text != "你好" || cm.From != "alice" {
		t.Fatal("附近的 bob 应收到聊天")
	}

	w.Leave(int(b.ID), cb)
	if _, ok := find[*proto.ObjDisappear](ca.take()); !ok {
		t.Fatal("bob 下线时 alice 应收到 ObjDisappear")
	}
	w.Leave(int(a.ID), &fakeClient{}) // 不是当前连接，不应下线
	if w.Online() != 1 {
		t.Fatal("旧连接的 Leave 不应影响在线角色")
	}
}

func TestStepRateLimit(t *testing.T) {
	w, db, g := setup(t)
	h := data.Hometowns[1]
	f := g.Floors[h.Floor]
	c := newChar(t, db, "speedy", h.Floor, h.X, h.Y)
	cl := &fakeClient{}
	w.Enter(c, cl)
	cl.take()
	clock := time.Now()
	w.Do(func() { w.now = func() time.Time { return clock } })

	x, y := h.X, h.Y
	corrected := 0
	for i := 0; i < 6; i++ { // 同一时刻连走 6 步：只有前 StepBurst 步被接受
		d, ok := walkableStep(g, f, x, y)
		if !ok {
			t.Fatal("无路可走")
		}
		w.Move(int(c.ID), x, y, d)
		if _, bad := find[*proto.PosCorrect](cl.take()); bad {
			corrected++
		} else {
			x, y = x+MOVE[d][0], y+MOVE[d][1]
		}
	}
	if corrected != 6-StepBurst {
		t.Fatalf("超速步数应被拒绝 %d 次，实际 %d", 6-StepBurst, corrected)
	}
}

func TestNpcTalk(t *testing.T) {
	w, db, g := setup(t)
	// 找一个村民（TownPeople），把角色放在它旁边的可走格
	var target *entity
	w.Do(func() {
		for _, es := range w.floors {
			for _, e := range es {
				if e.npc != nil && e.npc.Function == "TownPeople" && strings.Contains(e.npc.Text, ",") {
					target = e
					return
				}
			}
		}
	})
	if target == nil {
		t.Fatal("没有找到村民 NPC")
	}
	f := g.Floors[target.floor]
	sx, sy := -1, -1
	for d := 0; d < 8 && sx < 0; d++ {
		if x, y := target.x+MOVE[d][0], target.y+MOVE[d][1]; g.Tiles.Walkable(f, x, y) {
			sx, sy = x, y
		}
	}
	if sx < 0 {
		t.Skip("村民旁边没有可走格")
	}
	c := newChar(t, db, "talker", target.floor, sx, sy)
	cl := &fakeClient{}
	w.Enter(c, cl)
	cl.take()
	w.Talk(int(c.ID), target.id)
	msgs := cl.take()
	nt, ok := find[*proto.NpcTalk](msgs)
	if !ok || nt.Window || !strings.Contains(target.npc.Text, nt.Text) || nt.Text == "" {
		t.Fatalf("村民应随机说一句台词：%+v", msgs)
	}
	if _, turned := find[*proto.ObjTurn](msgs); !turned {
		t.Log("角色本来就面朝 NPC")
	}
	t.Logf("%s：%s", nt.Name, nt.Text)
}

func TestSignBoard(t *testing.T) {
	w, db, g := setup(t)
	var target *entity
	w.Do(func() {
		for _, es := range w.floors {
			for _, e := range es {
				// 庄园的告示牌显示的是主人家族（manor.go），文字不是 .create 里的；挑编号最小的普通告示牌，免得 map 遍历顺序不同时忽好忽坏
				if e.npc != nil && e.npc.Function == "SignBoard" && e.npc.Text != "" && !strings.Contains(e.npc.Text, "庄园") && (target == nil || e.id < target.id) {
					target = e
				}
			}
		}
	})
	if target == nil {
		t.Fatal("没有找到告示牌")
	}
	f := g.Floors[target.floor]
	place := func(dist int) (int, int, bool) {
		for d := 0; d < 8; d++ {
			x, y := target.x+MOVE[d][0]*dist, target.y+MOVE[d][1]*dist
			if g.Tiles.Walkable(f, x, y) {
				return x, y, true
			}
		}
		return 0, 0, false
	}
	// 距离 2 时不响应（原版 NPC_SignBoardLooked 要求距离不超过 1）
	if x, y, ok := place(2); ok {
		c := newChar(t, db, "farreader", target.floor, x, y)
		cl := &fakeClient{}
		w.Enter(c, cl)
		cl.take()
		w.Talk(int(c.ID), target.id)
		if _, got := find[*proto.NpcTalk](cl.take()); got {
			t.Fatal("距离 2 不应读到告示牌")
		}
	}
	x, y, ok := place(1)
	if !ok {
		t.Skip("告示牌旁边没有可走格")
	}
	c := newChar(t, db, "reader", target.floor, x, y)
	cl := &fakeClient{}
	w.Enter(c, cl)
	cl.take()
	w.Talk(int(c.ID), target.id)
	nt, got := find[*proto.NpcTalk](cl.take())
	if !got || !nt.Window || nt.Text != npcText(target.npc.Text) || strings.Contains(nt.Text, `\n`) {
		t.Fatalf("告示牌应弹窗显示全文（\\n 换成换行）：%+v", nt)
	}
}

func TestWarp(t *testing.T) {
	w, db, g := setup(t)
	// 找一个旁边有可走格的传送点，从旁边走上去
	for floor, warps := range g.Warps {
		f := g.Floors[floor]
		for p, warp := range warps {
			for d := 0; d < 8; d++ {
				sx, sy := p[0]-MOVE[d][0], p[1]-MOVE[d][1]
				if _, isWarp := g.WarpAt(floor, sx, sy); isWarp || !g.Tiles.Walkable(f, sx, sy) || !g.Tiles.Walkable(f, p[0], p[1]) {
					continue
				}
				c := newChar(t, db, "walker", floor, sx, sy)
				cl := &fakeClient{}
				w.Enter(c, cl)
				cl.take()
				w.Move(int(c.ID), sx, sy, d)
				ef, ok := find[*proto.EnterFloor](cl.take())
				if !ok || ef.Floor != warp.ToFloor || ef.X != warp.ToX || ef.Y != warp.ToY {
					t.Fatalf("踩到 (%d,%d,%d) 应传送到 %d(%d,%d)，得到 %+v", floor, p[0], p[1], warp.ToFloor, warp.ToX, warp.ToY, ef)
				}
				t.Logf("传送：%d(%d,%d) → %d %s (%d,%d)", floor, p[0], p[1], ef.Floor, ef.Name, ef.X, ef.Y)
				return
			}
		}
	}
	t.Fatal("没有找到可测试的传送点")
}

func TestStatusOnEnter(t *testing.T) {
	w, db, _ := setup(t)
	h := data.Hometowns[0]
	a, _ := db.CreateAccount(context.Background(), "stat", "pw", "")
	c, err := db.CreateCharacter(context.Background(), store.Character{AccountID: a.ID, Name: "stat", Sprite: 100000, Level: 1,
		Floor: h.Floor, X: h.X, Y: h.Y, Vital: 8, Str: 6, Tgh: 4, Dex: 2, Earth: 5, Fire: 5, Charm: 60})
	if err != nil {
		t.Fatal(err)
	}
	cl := &fakeClient{}
	w.Enter(c, cl)
	st, ok := find[*proto.Status](cl.take())
	if !ok {
		t.Fatal("进入世界后应收到 Status")
	}
	// HP = 4v+s+t+d；1 级升 2 级需 6 经验（exp.txt）；属性按原版 0..100
	if st.MaxHp != 8*4+6+4+2 || st.HP != st.MaxHp || st.NextExp != 6 || st.MaxMp != 100 || st.Attrs[0] != 50 || st.Attrs[2] != 50 {
		t.Fatalf("状态数据不对：%+v", st)
	}
}

func TestChatRange(t *testing.T) {
	e := &entity{x: 10, y: 10, dir: 5} // MOVE[5] = (+1, 0)
	at := func(x, y int) *entity { return &entity{x: x, y: y} }
	// 音量 3（默认）→ chatvol[2] = 7 格
	if h := chatHears(e, "你好", 3); !h(at(17, 10)) || h(at(18, 10)) {
		t.Fatal("音量 3 应听到 7 格内")
	}
	// 句末两个 ! 再加两级 → chatvol[4] = 11 格
	if h := chatHears(e, "喂!!", 3); !h(at(21, 3)) || h(at(22, 10)) {
		t.Fatal("句末 !! 应扩大到 11 格")
	}
	// 悄悄话：只有脚下和面前一格
	if h := chatHears(e, "秘密...", 10); !h(at(11, 10)) || !h(at(10, 10)) || h(at(9, 10)) {
		t.Fatal("悄悄话只传到面前一格")
	}
	if textVolume("abc.") != 0 || textVolume("a!!!") != 3 || textVolume("x...") != -1 {
		t.Fatal("textVolume 与原版不符")
	}
}

func TestReturnHome(t *testing.T) {
	w, db, _ := setup(t)
	h := data.Hometowns[1]
	a, _ := db.CreateAccount(context.Background(), "home", "pw", "")
	c, err := db.CreateCharacter(context.Background(), store.Character{AccountID: a.ID, Name: "home", Sprite: 100000, Level: 1,
		Hometown: 1, Floor: data.Hometowns[0].Floor, X: data.Hometowns[0].X, Y: data.Hometowns[0].Y})
	if err != nil {
		t.Fatal(err)
	}
	cl := &fakeClient{}
	w.Enter(c, cl)
	cl.take()
	w.ReturnHome(int(c.ID))
	if ef, ok := find[*proto.EnterFloor](cl.take()); !ok || ef.Floor != h.Floor || ef.X != h.X || ef.Y != h.Y {
		t.Fatalf("回记录点应回到出生村落 %+v", h)
	}
}

// 在遇敌区域里开一场战斗，一直攻击到结束：应依次收到 BattleStart、BattleTurn、BattleEnd 和更新后的 Status。
func TestBattleFlow(t *testing.T) {
	w, db, g := setup(t)
	f := g.Floors[100]
	a := g.Battle.AreaAt(100, 650, 450)
	if f == nil || a == nil {
		t.Skip("floor 100 没有遇敌区域")
	}
	x, y := -1, -1
	for yy := a.Y1; yy <= a.Y2 && x < 0; yy++ {
		for xx := a.X1; xx <= a.X2; xx++ {
			if g.Tiles.Walkable(f, xx, yy) {
				x, y = xx, yy
				break
			}
		}
	}
	acc, _ := db.CreateAccount(context.Background(), "fighter", "pw", "")
	c, _ := db.CreateCharacter(context.Background(), store.Character{AccountID: acc.ID, Name: "fighter", Sprite: 100000, Level: 1,
		Floor: 100, X: x, Y: y, Vital: 5, Str: 15, Tgh: 0, Dex: 0, Earth: 10, Charm: 60})
	cl := &fakeClient{}
	w.Enter(c, cl)
	cl.take()
	w.Do(func() { w.startBattle(w.players[int(c.ID)], a) })
	st, ok := find[*proto.BattleStart](cl.take())
	if !ok || st.MySlot != 0 || len(st.Fighters) < 2 {
		t.Fatalf("应收到 BattleStart：%+v", st)
	}
	var end *proto.BattleEnd
	for turn := 0; turn < 60 && end == nil; turn++ {
		target := -1
		for _, fi := range st.Fighters {
			if fi.Slot >= 10 {
				target = fi.Slot
			}
		}
		w.BattleCommand(int(c.ID), "attack", target, -1)
		msgs := cl.take()
		if _, ok := find[*proto.BattleTurn](msgs); !ok {
			t.Fatal("下指令后应收到 BattleTurn")
		}
		if e, ok := find[*proto.BattleEnd](msgs); ok {
			end = e
		}
	}
	if end == nil {
		t.Fatal("60 回合内战斗没有结束")
	}
	// 战斗结束后可以走路；打赢则经验增加
	w.Do(func() {
		e := w.players[int(c.ID)]
		if e.fight != nil {
			t.Error("战斗结束后应清掉战斗状态")
		}
		if end.Result == "win" && e.char.Exp != end.Exp {
			t.Errorf("经验应为 %d，实际 %d", end.Exp, e.char.Exp)
		}
	})
}

func TestLevelUpAndStatUp(t *testing.T) {
	w, db, _ := setup(t)
	h := data.Hometowns[1]
	acc, _ := db.CreateAccount(context.Background(), "grow", "pw", "")
	c, _ := db.CreateCharacter(context.Background(), store.Character{AccountID: acc.ID, Name: "grow", Sprite: 100000, Level: 1,
		Floor: h.Floor, X: h.X, Y: h.Y, Vital: 5, Str: 5, Tgh: 5, Dex: 5, Charm: 60})
	cl := &fakeClient{}
	w.Enter(c, cl)
	cl.take()
	var ups int
	w.Do(func() { ups = w.gainExp(&w.players[int(c.ID)].char, 20) }) // exp.txt：2 级 6、3 级 18
	if ups != 2 {
		t.Fatalf("20 经验应从 1 级升到 3 级，升了 %d", ups)
	}
	w.StatUp(int(c.ID), 1)
	st, ok := find[*proto.Status](cl.take())
	if !ok || st.Level != 3 || st.Points != 2*LevelUpPoints-1 || st.Str != 6 || st.Charm != 64 || st.DP != 20+30 {
		t.Fatalf("升级与加点不对：%+v", st)
	}
}

func TestNurseHeals(t *testing.T) {
	w, db, g := setup(t)
	var nurse *data.NPC
	for fl, ns := range g.NPCs {
		for i := range ns {
			if ns[i].Function == "WindowHealer" && g.Floors[fl] != nil {
				nurse = &ns[i]
				break
			}
		}
		if nurse != nil {
			break
		}
	}
	if nurse == nil {
		t.Skip("没有护士")
	}
	var nid, nx, ny int
	w.Do(func() {
		for _, e := range w.floors[nurse.Floor] {
			if e.npc == nurse {
				nid, nx, ny = e.id, e.x, e.y
			}
		}
	})
	acc, _ := db.CreateAccount(context.Background(), "hurt", "pw", "")
	c, _ := db.CreateCharacter(context.Background(), store.Character{AccountID: acc.ID, Name: "hurt", Sprite: 100000, Level: 1,
		Floor: nurse.Floor, X: nx + 1, Y: ny, Vital: 5, Str: 5, Tgh: 5, Dex: 5})
	cl := &fakeClient{}
	w.Enter(c, cl)
	cl.take()
	w.Do(func() {
		e := w.players[int(c.ID)]
		w.placeOnFloor(e, nurse.Floor, nx+1, ny, 0) // 站到护士旁边（出生点不可走时上线会被挪回村里）
		e.char.HP, e.char.MP = 3, 10
		e.char.Gold = 100000 // 有的护士从 1 级就收钱（healerRates）
	})
	// 原版流程：选单 → 选"耐久力·气力回复"（第 4 行）→ 确认"是" → 回复完成
	w.Talk(int(c.ID), nid)
	win, ok := find[*proto.NpcWindow](cl.take())
	if !ok || win.Kind != "select" {
		t.Fatal("和护士说话应打开选单")
	}
	w.NpcWindowReply(int(c.ID), nid, win.Seq, 0, "4")
	confirm, ok := find[*proto.NpcWindow](cl.take())
	if !ok || confirm.Buttons != BtnYesNo {
		t.Fatalf("应出现确认窗：%+v", confirm)
	}
	w.NpcWindowReply(int(c.ID), nid, confirm.Seq, BtnYes, "")
	msgs := cl.take()
	st, ok := find[*proto.Status](msgs)
	if done, _ := find[*proto.NpcWindow](msgs); !ok || st.HP != st.MaxHp || st.Mp != 100 || done.Buttons != BtnOK {
		t.Fatalf("护士应把 HP/MP 补满：%s", fmtMsgs(msgs))
	}
	// 旧窗口编号的回传不处理
	w.NpcWindowReply(int(c.ID), nid, win.Seq, 0, "4")
	if _, again := find[*proto.NpcWindow](cl.take()); again {
		t.Fatal("过期的窗口回传不应处理")
	}
}

// 道具：拿到 → 装备（能力值变化）→ 卸下；回复道具在地图上使用；等级不够不能装。
func TestItemsEquipAndUse(t *testing.T) {
	w, db, _ := setup(t)
	h := data.Hometowns[1]
	acc, _ := db.CreateAccount(context.Background(), "itemer", "pw", "")
	c, _ := db.CreateCharacter(context.Background(), store.Character{AccountID: acc.ID, Name: "itemer", Sprite: 100000, Level: 1,
		Floor: h.Floor, X: h.X, Y: h.Y, Vital: 5, Str: 5, Tgh: 5, Dex: 5, Charm: 60})
	cl := &fakeClient{}
	w.Enter(c, cl)
	if inv, ok := find[*proto.Inventory](cl.take()); !ok || len(inv.Items) != 0 {
		t.Fatal("上线应收到空的道具栏")
	}
	var axeSlot, meatSlot int
	w.Do(func() {
		e := w.players[int(c.ID)]
		axeSlot = w.addItem(e.inv, w.newItem(w.game.Items[0]))     // 小斧头：攻 +9 防 -3 敏 -3
		meatSlot = w.addItem(e.inv, w.newItem(w.game.Items[1234])) // 小块肉：体20
		e.char.HP = 10
	})
	w.Do(func() {}) // 等上一条执行完
	before, _ := find[*proto.Status](append([]proto.Message{}, w.statusOfFor(int(c.ID))))
	w.ItemMove(int(c.ID), axeSlot, -1)
	msgs := cl.take()
	st, ok := find[*proto.Status](msgs)
	if !ok || st.Atk != before.Atk+9 || st.Def != max(0, before.Def-3) {
		t.Fatalf("装上小斧头后攻击应 +9：前 %+v 后 %+v", before, st)
	}
	if inv, _ := find[*proto.Inventory](msgs); len(inv.Items) != 2 || inv.Items[0].Slot != data.SlotArm {
		t.Fatalf("小斧头应在武器栏：%+v", inv)
	}
	// 拿斧头的形象 = 基本形象 + 1（CHAR_eqimagetbl 的 SPR_xxxax）
	if sp, ok := find[*proto.ObjSprite](msgs); !ok || sp.Sprite != 100000+data.ItemAxe {
		t.Fatalf("装上斧头后形象应变为 100001：%+v", sp)
	}
	w.ItemUse(int(c.ID), meatSlot, 0)
	st, _ = find[*proto.Status](cl.take())
	if st.HP < 10+18 || st.HP > 10+22 {
		t.Fatalf("小块肉应回复 18..22 HP：%d", st.HP)
	}
	w.ItemMove(int(c.ID), data.SlotArm, -1) // 卸下
	if inv, _ := find[*proto.Inventory](cl.take()); len(inv.Items) != 1 || inv.Items[0].Slot < data.EquipSlots {
		t.Fatalf("卸下后应回到道具栏：%+v", inv)
	}
}

// 战利品：打倒的敌人身上的道具进道具栏，每场最多 3 件。
func TestBattleDrops(t *testing.T) {
	w, db, _ := setup(t)
	h := data.Hometowns[1]
	acc, _ := db.CreateAccount(context.Background(), "looter", "pw", "")
	c, _ := db.CreateCharacter(context.Background(), store.Character{AccountID: acc.ID, Name: "looter", Sprite: 100000, Level: 1,
		Floor: h.Floor, X: h.X, Y: h.Y, Vital: 5, Str: 5, Tgh: 5, Dex: 5})
	cl := &fakeClient{}
	w.Enter(c, cl)
	cl.take()
	var names []string
	w.Do(func() {
		b := battle.New(w.rnd)
		b.Add(w.playerFighter(w.players[int(c.ID)], 0))
		for i := 0; i < 3; i++ {
			b.Add(&battle.Fighter{Slot: battle.EnemySlot(i), HP: 0, MaxHP: 10, Drops: []int{1234, 1234}, Weapon: -1})
		}
		names = w.giveDrops(w.players[int(c.ID)], b)
	})
	if len(names) != 3 || names[0] != "小块肉" {
		t.Fatalf("应拿到 3 件战利品：%v", names)
	}
}

// 装备让防御力、敏捷变成负数时照原版（ITEM_equipEffect）保留负数，最低 -100；攻击力最低 0。
func TestNegativeDefQuick(t *testing.T) {
	w, db, _ := setup(t)
	c := newChar(t, db, "weak", 1000, 47, 82)
	w.Enter(c, &fakeClient{})
	w.Do(func() {
		e := w.players[int(c.ID)]
		def := &data.ItemDef{ID: -1, Name: "test", Type: data.ItemAxe, Durability: 0}
		it := w.newItem(def)
		it.Mods[data.ModDef], it.Mods[data.ModQuick], it.Mods[data.ModAtk] = -3, -500, -500
		w.game.Items[-1] = def
		defer delete(w.game.Items, -1)
		e.inv[data.SlotArm] = it
		s := w.stats(e)
		if s.Def >= 0 || s.Quick != -100 || s.Atk != 0 {
			t.Errorf("防御 %d（应为负）、敏捷 %d（应为 -100）、攻击 %d（应为 0）", s.Def, s.Quick, s.Atk)
		}
	})
}

// 世界地图的 NPC 清单（NpcListReq）：一层楼全部有名字的 NPC 与位置，不限楼层；没名字的传送点不列；楼层不存在不回。
func TestNpcList(t *testing.T) {
	w, db, g := setup(t)
	c, cl := petOwner(t, w, db, "看地图", 10, 60)
	w.NpcList(int(c.ID), 1000) // 萨姆吉尔村
	m, ok := find[*proto.NpcList](cl.take())
	if !ok || m.Floor != 1000 || len(m.Npcs) == 0 {
		t.Fatalf("应回 1000 层的 NPC 清单：%v %v", ok, m)
	}
	n := len(g.NPCs[1000])
	if len(m.Npcs) > n {
		t.Fatalf("清单 %d 个不应多于这层的 NPC %d 个", len(m.Npcs), n)
	}
	for _, p := range m.Npcs {
		if p.Name == "" || !g.Floors[1000].In(p.X, p.Y) {
			t.Fatalf("NPC 要有名字、在楼层内：%+v", p)
		}
	}
	w.NpcList(int(c.ID), 999999)
	if _, ok := find[*proto.NpcList](cl.take()); ok {
		t.Fatal("不存在的楼层不应回清单")
	}
}
