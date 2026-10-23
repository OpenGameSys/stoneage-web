// Package world 是游戏世界：楼层、实体、移动校验、视野同步、传送与聊天。
// 所有状态只在 World.run 这一个协程里读写，外部通过 World.Do 投递指令，因此不需要锁。
package world

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"sa/server/internal/battle"
	"sa/server/internal/data"
	"sa/server/internal/proto"
	"sa/server/internal/store"
)

const (
	ViewRadius   = 16                     // 视野半径（格）。原客户端一屏约 ±13 格，多留几格避免边缘闪现
	MinStepGap   = 200 * time.Millisecond // 两步之间的最短间隔（客户端每步约 260ms）
	StepBurst    = 3                      // 允许网络抖动造成的连续快步数
	MaxMapReq    = 64                     // 一次 MapReq 的最大边长
	MaxChatRunes = 120
	// chatBurst / chatPerSecond：一个人连说几句之后限到每秒几句（防刷屏）
	chatBurst     = 4.0
	chatPerSecond = 1.5
	// worldChatGap：世界频道两句之间要超过这么久（CHAR_WORKWORLDSENDTIME，原版 > 5 秒）

	worldChatGap = 5 * time.Second
	SaveInterval = 30 * time.Second
	// 多久按 SA 时间检查一次 TimeMan 的图（原版是周围有人动作时 NPC_TimeManWatch 检查；一个 SA 时约 5.3 秒）
	timeManInterval = 5 * time.Second
	npcIDBase       = 1_000_000
)

// Client 是世界向一个在线玩家发送消息的出口（由网络层实现）。
type Client interface {
	Send(m proto.Message)
	Kick(reason string)
}

// MOVE 为客户端方向 0..7 的格子位移（原客户端 map.cpp moveAddTbl）。
var MOVE = [8][2]int{{-1, 1}, {-1, 0}, {-1, -1}, {0, -1}, {1, -1}, {1, 0}, {1, 1}, {0, 1}}

type entity struct {
	id               int
	kind             string // "player" / "npc"
	name             string
	sprite           int
	floor, x, y, dir int
	client           Client       // 玩家才有
	party            *party       // 组队（party.go）
	fs               int          // FS 开关（fsParty 等）
	trade            *trade       // 交易中（trade.go）
	act              int          // 身上的动作（action.go）：0 没有，否则动作编号 + 1
	book             *addressBook // 名片匣（addressbook.go）
	petMails         int          // 在外面的邮件宠物（CHAR_PETMAILNUMS）
	petMailAt        time.Time    // 下次可以寄宠物邮件的时间
	duelCool         time.Time    // 这之前不能 PK（_BATTLE_OVER_STAR_TIME）
	rankStart        int          // 强者表现在的第一行（CHAR_WORKSHOPRELEVANT）
	fm               famView      // 家族的快照（family.go）
	fmc              fmChar       // 存在角色上的家族资料（个人气势、家族银行个人帐户）
	char             store.Character
	visible          map[int]bool // 玩家当前能看到的实体
	stepTokens       float64
	lastStep         time.Time
	npc              *data.NPC    // NPC 才有：对话行为与内容
	cep              int          // 遇敌值（原版 CONNECT_get_CEP）
	watch            *battleRoom  // 正在观战的那场（watch.go），不能走动
	bus              *bus         // 会走动的 NPC（bus.go）：巴士 / 飞机的路线与状态
	stall            *vendorStall // 摆着的摊位（vendor.go，CHAR_WORKSTREETVENDOR 1 / 2）
	buying           *entity      // 正在看谁的摊位（CHAR_WORKSTREETVENDOR 3）
	warOut           bool         // 家族对战里倒下 / 逃跑过，等着被送出场（fmwar.go）
	gm               bool         // 账号在 World.GMs 里（上线时定）
	account          string       // 登录账号名（GM 的 /setpw、/setsecret 用）
	questDirty       bool         // 任务旗标改过、任务日志还没重送（quest.go）
	questSig         string       // 上次送出的任务日志签名（道具栏变动后比对，quest.go refreshQuests）
	muteUntil        time.Time    // GM 的 /mute：这之前不能说话（不存档）
	mind             int          // 留在头上的心情图示（CHAR_MIND_NUM：睡觉 / 咖啡 / 强调）
	worldSentAt      time.Time    // 上次在世界频道说话（CHAR_WORKWORLDSENDTIME）
	chatTokens       float64      // 说话的令牌桶（chatBurst 满、每秒回 chatPerSecond），刷屏的句子直接丢掉
	chatAt           time.Time
	dbAt             time.Time // 上次在世界协程里替它同步查库（传言板、强者表）的时间：刷太快就不理，免得卡住所有人

	fight     *fight             // 战斗中
	window    *npcWindow         // 打开中的 NPC 窗口
	inv       *inventory         // 道具与装备
	pets      *petBag            // 宠物栏（pet.go）
	magic     *battle.MagicSkill // 魔法熟练度与抗魔（characters.magic）
	noenemy   int                // 驱魔（ITEM_useNoenemy）：剩下的分钟数，> 0 不遇敌（原版记在连线上，不存档）
	stay      bool               // 原地遇敌（ITEM_useEncounter / UseDeathCounter），走动或传送就取消
	water     int                // 水中呼吸剩下的分钟数（鱼鳃草，CHAR_WORKSTATUSWATER，不存档）
	lastMerge int64              // 上次加工 / 料理的时间（CHAR_WORKLASTMERGETIME，unix 秒）
	ground    *ground            // 地上的道具 / 石币 / 宠物才有（ground.go）
	addExp    addExp             // 智慧之果（itemuse.go）
	metamo    *metamoState       // 变身中（itemuse.go）
	level     int                // 地上的宠物的等级（滑鼠停着时显示）
}

// hidden：按时间出现的人不在的时候（图号 9999）看不到。
func (e *entity) hidden() bool { return e.npc != nil && e.sprite == timeManHidden }

func (e *entity) info() proto.ObjInfo {
	m := e.mark() // 头上的队长 / 战斗记号（CHAR_sendCharaAtWalk 补送的 CA）
	return proto.ObjInfo{ID: e.id, Kind: e.kind, Name: e.name, Sprite: e.sprite, X: e.x, Y: e.y, Dir: e.dir, Level: e.level, Mind: e.mind,
		Leader: m.Leader, Battle: m.Battle, Help: m.Help, Action: e.act - 1, Family: e.fm.Name, Trade: m.Trade, Vendor: m.Vendor}
}

type World struct {
	Name       string // 线路名（大区 + 线路，星球频道的 [星球X] 用）
	game       *data.Game
	db         store.Store
	log        *slog.Logger
	cmds       chan func()
	floors     map[int]map[int]*entity // 楼层 → 实体
	players    map[int]*entity         // 角色 ID → 实体
	mergeCache []mergeTarget           // 能加工出来的道具（merge.go，第一次加工时建立）
	towns      []gmTown                // GM /town2 的村庄清单（记录点 / 长老的位置，开机建）
	now        func() time.Time
	rnd        *rand.Rand
	// groundSeq 编地上物的 id（groundIDBase 起）；ticks 是时钟跳了几次（5 秒一次）
	groundSeq, ticks int
	partyWarping     bool // 正在把队员跟着队长传送
	battleSeq        int  // 战斗的编号（地图上的战斗记号 CG_VS_MARK 按它轮换）
	// GMs 是 config.yaml 的 gms：这些账号（登录用的账号名，不分大小写）上线的角色是 GM，
	// 有 GM 指令选单、能用 GM 指令（gm.go）；别的玩家收不到选单，送来的指令也不执行
	GMs map[string]bool
	// Rules 是 config.yaml 的 rules（rules.go），开机时设定，之后只读
	Rules Rules
	// panics 是 safe 拦下的 panic 次数
	panics    atomic.Int64
	buses     []*entity         // 会走动的 NPC（bus.go tickBuses）
	warPlace  map[int]*warPlace // 对战场的位置（fmwar.go warPlaces，算一次）
	qhints    map[int]string    // 事件编号 → 相关 NPC 的提示（quest.go questHints，算一次）
	floorVers map[int]string    // 楼层资料的版本（floorVer，客户端拿它决定本机存的地图还能不能用）

	// saves 是存档队列：由一个协程按顺序写库，保证同一角色的新存档不会被旧存档盖掉。
	// enqueue 按 key 合并：同一个 key 还没写时只换掉要写的内容，不再排一笔（道具栏一秒动 40 次也只写一次），
	// 队列里顶多每个在线角色各一笔，不会被刷爆而卡住世界协程
	saves      chan func()
	saveMu     sync.Mutex
	saveLatest map[string]func()
	started    time.Time          // 开机时间（记录点报"本星球已经启动了…"）
	timeMen    []*entity          // 按时间换图的 NPC（TimeMan）
	quizzes    map[int]*quizState // 角色 ID → 正在进行的猜谜（quiz.go）
	fam        *Families          // 家族（所有线路共用，family.go）
	// inbox 是别的线路送来的事（post），世界协程在 Do 之后与 wake 时做
	inboxMu sync.Mutex
	inbox   []func()
	wake    chan struct{}
}

// New 创建一条线路的世界。seed 决定 NPC 的随机出生位置（不同线路的 NPC 站位可以不同）。
func New(game *data.Game, db store.Store, log *slog.Logger, seed int64) *World {
	w := &World{game: game, db: db, log: log, cmds: make(chan func(), 1024),
		floors: map[int]map[int]*entity{}, players: map[int]*entity{}, now: time.Now,
		rnd: rand.New(rand.NewPCG(uint64(seed), 1)), saves: make(chan func(), 4096), started: time.Now(),
		quizzes: map[int]*quizState{}, wake: make(chan struct{}, 1), Rules: DefaultRules()}

	w.fam = NewFamilies(db, log)
	go func() {
		for f := range w.saves {
			f()
		}
	}()
	id := npcIDBase
	floors := make([]int, 0, len(game.NPCs))
	for f := range game.NPCs {
		floors = append(floors, f)
	}
	slices.Sort(floors) // 固定遍历顺序，同一 seed 得到同样的站位
	for _, floor := range floors {
		for i := range game.NPCs[floor] {
			n := &game.NPCs[floor][i]
			x, y := w.bornPoint(n)
			id++
			e := &entity{id: id, kind: "npc", name: n.Name, sprite: n.Sprite, floor: floor, x: x, y: y, dir: n.Dir, npc: n}
			if n.Function == "Bus" || n.Function == "Airplane" {
				if b := w.newBus(e); b != nil { // 巴士是乘客的队长：要有一个（什么都不送的）连线与视野表，party.go 才能照常用
					e.bus, e.client, e.visible = b, nopClient{}, map[int]bool{}
					w.buses = append(w.buses, e)
				}
			}
			w.addToFloor(e)
			if n.Function == "TimeMan" {
				w.timeMen = append(w.timeMen, e)
			}
		}
	}
	w.updateTimeMen()
	w.towns = gmTowns(game)
	return w
}

// bornPoint 在出生矩形内随机挑一格可走的位置（原版 npcgen 在 borncenter 范围内随机出生）；找不到时用中心点。
func (w *World) bornPoint(n *data.NPC) (int, int) {
	f := w.game.Floors[n.Floor]
	if n.X2 > n.X1 || n.Y2 > n.Y1 {
		for try := 0; try < 20; try++ {
			x := n.X1 + w.rnd.IntN(n.X2-n.X1+1)
			y := n.Y1 + w.rnd.IntN(n.Y2-n.Y1+1)
			if w.game.Tiles.Walkable(f, x, y) {
				return x, y
			}
		}
	}
	return n.X, n.Y
}

// Run 处理指令直到 ctx 结束；退出前保存所有在线角色的位置。
func (w *World) Run(ctx context.Context) {
	save := time.NewTicker(SaveInterval)
	defer save.Stop()
	clock := time.NewTicker(timeManInterval)
	defer clock.Stop()
	busStep := w.Rules.BusStep
	if busStep <= 0 {
		busStep = busLoopTime
	}
	busTick := time.NewTicker(busStep)
	defer busTick.Stop()
	for {
		select {
		case <-ctx.Done():
			for _, e := range w.players {
				w.save(e)
				w.saveProgress(e)
			}
			// 等存档队列写完再退出
			done := make(chan struct{})
			w.saves <- func() { close(done) }
			select {
			case <-done:
			case <-time.After(10 * time.Second):
			}
			return
		case f := <-w.cmds:
			f() // Do 包好的，里面已经 safe
		case <-w.wake:
			w.drain()
		case <-save.C:
			w.safe(w.saveAll)
		case <-busTick.C:
			w.safe(w.tickBuses)
		case <-clock.C:
			w.safe(w.updateTimeMen)
			w.safe(w.sweepGround)
			w.safe(w.tickPlayers)
			w.safe(w.tickWars)
		}
	}
}

// safe 执行 f；f panic 时记下错误与堆栈、计一次数（Panics），世界协程照常运行：
// 玩家送来的怪资料（或我们漏掉的边界检查）只能弄坏那一次指令，不能拖垮整台服务器。
func (w *World) safe(f func()) {
	defer func() {
		if r := recover(); r != nil {
			w.panics.Add(1)
			w.log.Error("世界协程出错（已拦下）", "err", r, "stack", string(debug.Stack()))
		}
	}()
	f()
}

// Panics 是 safe 拦下过几次 panic（测试与监控用）。
func (w *World) Panics() int64 { return w.panics.Load() }

// Do 把 f 投递到世界协程执行，并等待它完成（f panic 时也一定放行，不会让调用的人卡住）。
func (w *World) Do(f func()) {
	done := make(chan struct{})
	w.cmds <- func() {
		defer close(done)
		w.safe(f)
		w.drain()
	}
	<-done
}

// post 把 f 交给世界协程稍后执行，不等待、不会阻塞（别的线路、家族资料送来的事用它，避免互相等待）。
func (w *World) post(f func()) {
	w.inboxMu.Lock()
	w.inbox = append(w.inbox, f)
	w.inboxMu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *World) drain() {
	for {
		w.inboxMu.Lock()
		fs := w.inbox
		w.inbox = nil
		w.inboxMu.Unlock()
		if len(fs) == 0 {
			return
		}
		for _, f := range fs {
			w.safe(f)
		}
	}
}

// Families 回传这条线路用的家族资料；ShareFamilies 让几条线路共用同一份（cmd/saserver 在 Run 之前设定）。
func (w *World) Families() *Families { return w.fam }
func (w *World) ShareFamilies(f *Families) {
	w.fam = f
	f.mu.Lock()
	f.worlds = append(f.worlds, w)
	f.mu.Unlock()
}

// ---------------- 进入与离开 ----------------

// Enter 让角色上线；同一角色已在线时踢掉旧连接。返回实体 ID。
func (w *World) Enter(c store.Character, cl Client) {
	w.Do(func() {
		if old := w.players[int(c.ID)]; old != nil {
			old.client.Kick("角色在别处登录")
			w.leave(old)
		}
		f := w.game.Floors[c.Floor]
		if f == nil || !w.game.Tiles.Walkable(f, c.X, c.Y) {
			h := data.Hometowns[max(0, min(c.Hometown, len(data.Hometowns)-1))]
			c.Floor, c.X, c.Y = h.Floor, h.X, h.Y
		}
		e := &entity{id: int(c.ID), kind: "player", name: c.Name, sprite: c.Sprite, floor: c.Floor, x: c.X, y: c.Y,
			dir: c.Dir, client: cl, char: c, visible: map[int]bool{}, stepTokens: StepBurst}
		w.players[e.id] = e
		w.setLuck(e)
		if a, ok := cl.(interface{ Account() string }); ok {
			e.account = a.Account()
			e.gm = w.GMs[strings.ToLower(e.account)]
		}
		e.inv = parseInventory(c.Items)
		w.unequipProfession(e)

		e.pets = parsePets(c.Pets)
		e.magic = parseMagic(c.Magic)
		e.addExp = parseAddExp(c.AddExp)
		e.book = parseAddressBook(c.AddressBook)
		e.fmc = parseFmChar(c.Family)
		e.sprite = w.equipSprite(e)
		e.fs = fsParty | fsAllServ // 上线时组队开着（_ALWAYS_TEAM_OPEN），交易、名片关着（char.c CHAR_ISTRADE / ISTRADECARD = 0）；星球频道收得到
		cl.Send(&proto.Self{ID: e.id, Name: e.name, Sprite: e.sprite})
		w.famLogin(e) // ACFMCharLogin：F 状态、家族频道
		cl.Send(&proto.Flags{Flags: e.fs})
		cl.Send(w.statusOf(e))
		w.sendInventory(e)
		w.sendPets(e)
		w.sendQuests(e)
		w.placeOnFloor(e, c.Floor, c.X, c.Y, c.Dir)
		w.notifyLoginLogout(e, true) // 名片上的在线
		w.deliverStoredMails(e)      // MessageFlush
		w.returnAwayPets(e)
		if e.gm {
			cl.Send(&proto.GmMenu{Items: w.gmMenuItems()})
		}
		w.log.Info("上线", "char", e.name, "floor", e.floor, "x", e.x, "y", e.y, "gm", e.gm)
	})
}

// hasItemWhere：身上（装备与道具栏）有没有符合条件的道具。
func (w *World) hasItemWhere(e *entity, ok func(*data.ItemDef) bool) bool {
	for _, it := range e.inv {
		if d := w.itemDef(it); d != nil && ok(d) {
			return true
		}
	}
	return false
}

// itemDef 是道具实例的定义（nil 安全）。
func (w *World) itemDef(it *Item) *data.ItemDef {
	if it == nil {
		return nil
	}
	return w.game.Items[it.ID]
}

// dropItemsAtLogout 是 CHAR_dropItemAtLogout：下线时，身上（装备与道具栏）登出消失的道具（ITEM_DROPATLOGOUT）都消失。
func (w *World) dropItemsAtLogout(e *entity) {
	gone := false
	for i, it := range e.inv {
		if d := w.itemDef(it); d != nil && d.DropAtLogout {
			e.inv[i] = nil
			gone = true
		}
	}
	if gone {
		e.char.Items = e.inv.String()
	}
}

// setLuck 是 CHAR_setLuck（上线时）：上次抽运气不是同一个 SA 日（RealTimeToLSTime 的年、日不同）才重抽，
// 运气与抽的时间都存档，所以重新登录不会换运气。
func (w *World) setLuck(e *entity) {
	now := w.now().Unix()
	if e.char.Luck >= 1 && e.char.LuckTime > 0 && saDayOf(e.char.LuckTime) == saDayOf(now) {
		return
	}
	e.char.Luck, e.char.LuckTime = battle.RollLuck(w.rnd), now
	w.saveProgress(e)
}

// saDayOf 是 Unix 秒所在的 SA 日（一天 5400 秒，从 saEra 起算）。
func saDayOf(t int64) int64 {
	d := t - saEra
	if d < 0 {
		return (d - saDay + 1) / saDay
	}
	return d / saDay
}

// Leave 让角色下线并存档。只有当前在线的正是这个连接时才生效：
// 被顶号的旧连接断开时，不能把新连接的角色也带下线。
func (w *World) Leave(charID int, cl Client) {
	w.Do(func() {
		if e := w.players[charID]; e != nil && e.client == cl {
			w.leave(e)
		}
	})
}

func (w *World) leave(e *entity) {
	w.abandonBattle(e)
	w.cancelTrade(e)
	w.vendorClear(e)
	w.dropItemsAtLogout(e)
	w.leaveParty(e, true) // _CHAR_logout：CHAR_DischargeParty
	w.removeFromFloor(e)
	delete(w.players, e.id)
	w.notifyLoginLogout(e, false)
	w.famLogout(e)
	w.save(e)
	w.saveProgress(e) // 下线时把数值也存一次：登出消失的道具（上面刚拿掉）与还没来得及存的变化都写进去
	w.log.Info("下线", "char", e.name)
}

// ---------------- 玩家指令 ----------------

func (w *World) Move(charID, x, y, dir int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil {
			return
		}
		if e.fight != nil || e.watch != nil || e.isFollower() || e.trade != nil || e.stall != nil || e.buying != nil {
			// 战斗中（含观战）不能走；队员不能自己走（CHAR_walk_init 只收转身）；交易中不能走；摆摊中、看别人的摊位时也不能走（iOnStreetVendor）
			e.client.Send(&proto.PosCorrect{X: e.x, Y: e.y, Dir: e.dir})
			return
		}
		f := w.game.Floors[e.floor]
		ok := dir >= 0 && dir < 8 && x == e.x && y == e.y && w.takeStepToken(e)
		nx, ny := e.x, e.y
		if ok {
			nx, ny = e.x+MOVE[dir][0], e.y+MOVE[dir][1]
			ok = w.game.Tiles.Walkable(f, nx, ny)
		}
		if !ok {
			e.client.Send(&proto.PosCorrect{X: e.x, Y: e.y, Dir: e.dir})
			return
		}
		ox, oy := e.x, e.y
		e.x, e.y, e.dir = nx, ny, dir
		e.clearAction()
		if warp, found := w.game.WarpAt(e.floor, nx, ny); found {
			w.placeOnFloor(e, warp.ToFloor, warp.ToX, warp.ToY, e.dir)
			return
		}
		w.broadcastMove(e)
		w.partyFollow(e, ox, oy)
		e.stay = false // 走一步就不再原地遇敌（clearStayEncount）
		w.rollEncounter(e)
	})
}

// walkSpeed 是 rules.walkSpeed（没设或不合理就 1）。
func (w *World) walkSpeed() float64 {
	if w.Rules.WalkSpeed <= 0 {
		return 1
	}
	return w.Rules.WalkSpeed
}

// battleSpeedPct 是 rules.battleSpeed 的百分比（BattleStart.speed，客户端 battle.ts 用它加速演出）。
func (w *World) battleSpeedPct() int {
	if w.Rules.BattleSpeed <= 0 {
		return 100
	}
	return int(w.Rules.BattleSpeed*100 + 0.5)
}

// clientStepMs 是客户端自己走一格要用的毫秒数（EnterFloor.step）：原版 260 ms 除以速度倍数。
func (w *World) clientStepMs() int { return int(260/w.walkSpeed() + 0.5) }

// takeStepToken 是简单的令牌桶：平均不快于 MinStepGap / walkSpeed 一步，允许 StepBurst 步的突发。
func (w *World) takeStepToken(e *entity) bool {
	now := w.now()
	if !e.lastStep.IsZero() {
		gap := time.Duration(float64(MinStepGap) / w.walkSpeed())
		e.stepTokens = min(StepBurst, e.stepTokens+float64(now.Sub(e.lastStep))/float64(gap))
	}
	e.lastStep = now
	if e.stepTokens < 1 {
		return false
	}
	e.stepTokens--
	return true
}

func (w *World) Turn(charID, dir int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || dir < 0 || dir >= 8 {
			return
		}
		e.dir = dir
		for _, o := range w.floors[e.floor] {
			if o != e && o.client != nil && o.visible[e.id] {
				o.client.Send(&proto.ObjTurn{ID: e.id, Dir: dir})
			}
		}
	})
}

// Talk 与 NPC 对话。规则来自原版：
//   - TownPeople（npc_townpeople.c）：玩家在 NPC 前方 3 格内并面朝它，随机回一句（台词以逗号分隔）
//   - SignBoard（npc_signboard.c）：玩家与告示牌距离不超过 1，弹窗显示全文
//
// 原版要求玩家先面朝 NPC；这里由服务端替玩家转身并广播，省去客户端额外的转向请求。
func (w *World) Talk(charID, npcID int) {
	w.Do(func() {
		p := w.players[charID]
		if p == nil {
			return
		}
		n := w.floors[p.floor][npcID]
		if n == nil || n.npc == nil || n.npc.Function == "" || n.hidden() || p.busy() { // 交易、摆摊中不能找 NPC（CHAR_WORKTRADEMODE）
			return
		}
		dist := max(abs(n.x-p.x), abs(n.y-p.y))
		switch n.npc.Function {
		case "TownPeople":
			if dist > 3 {
				return
			}
			w.face(p, n)
			if n.npc.Text == "" {
				return
			}
			lines := strings.Split(n.npc.Text, ",")
			p.client.Send(&proto.NpcTalk{ID: n.id, Name: n.name, Text: strings.TrimSpace(lines[w.rnd.IntN(len(lines))])})
		case "SignBoard":
			if dist > 1 || n.npc.Text == "" {
				return
			}
			w.face(p, n)
			p.client.Send(&proto.NpcTalk{ID: n.id, Name: n.name, Text: npcText(w.manorText(n.npc.Text)), Window: true}) // 告示牌文字里写成两个字的 \n 是换行
		case "WindowHealer", "Healer":
			if dist > healerRange(n) {
				return
			}
			w.face(p, n)
			w.healerOpen(p, n)
		case "WarpMan":
			if dist > 2 {
				return
			}
			w.face(p, n)
			w.warpmanTalk(p, n, "")
		case "Windowman":
			if dist > 1 {
				return
			}
			w.face(p, n)
			w.windowmanOpen(p, n, 1)
		case "SavePoint":
			if dist > 2 {
				return
			}
			w.face(p, n)
			w.savePointTalk(p, n)
		case "TimeMan":
			if dist > 3 {
				return
			}
			w.face(p, n)
			w.timeManTalk(p, n)
		case "ItemShop":
			if dist > 2 {
				return
			}
			w.face(p, n)
			w.shopTalk(p, n, "")
		case "PoolItemShop":
			if dist > 2 {
				return
			}
			w.face(p, n)
			w.poolShopOpen(p, n)
		case "Dengon":
			if dist > 3 { // DENGONDISTANCE
				return
			}
			w.face(p, n)
			w.dengonOpen(p, n)
		case "Bankman":
			if dist > 1 {
				return
			}
			w.face(p, n)
			w.windowmanOpen(p, n, 1) // NPC_Bankman_selectWindow 与万事通相同
		case "Charm":
			if dist > 2 {
				return
			}
			w.face(p, n)
			w.charmOpen(p, n)
		case "LuckyMan":
			if dist > 2 {
				return
			}
			w.face(p, n)
			w.luckyOpen(p, n)
		case "Duelranking": // NPC_DuelrankingLooked（原版是"看"不是说话，这里点它就算）
			w.duelRankingOpen(p, n)
		case "Janken":
			if dist > 1 {
				return
			}
			w.face(p, n)
			w.jankenOpen(p, n)
		case "ExChangeMan":
			if dist > 2 {
				return
			}
			w.face(p, n)
			w.exchangeTalk(p, n, "")
		case "NPCEnemy":
			if dist > 2 {
				return
			}
			w.face(p, n)
			w.npcEnemyTalk(p, n)
		case "Quiz":
			if dist > 2 {
				return
			}
			w.face(p, n)
			w.quizTalk(p, n)
		case "ItemchangeMan":
			if dist > 2 {
				return
			}
			w.face(p, n)
			w.itemChangeOpen(p, n)
		case "Riderman":
			if dist > 1 {
				return
			}
			w.face(p, n)
			w.windowmanOpen(p, n, 1) // NPC_Riderman_selectWindow(1)
		case "Bus", "Airplane":
			w.busTalk(p, n)
		case "ManorSman":
			w.manorSmanTalk(p, n)
		case "Scheduleman":
			w.schedulemanTalk(p, n)
		case "FMWarpMan":
			w.fmWarpManTalk(p, n)
		case "FMPKMan":
			w.fmPkManTalk(p, n)
		case "FMPKCallMan":
			w.fmPkCallManTalk(p, n)
		case "Familyman": // NPC_FamilymanLooked
			w.face(p, n)
			w.familymanTalk(p, n)
		case "FmDengon": // _FM_MODIFY 的 NPC_FmDengonLooked 不检查距离；这里照传言板 3 格
			if dist > 3 {
				return
			}
			w.face(p, n)
			w.fmDengonTalk(p)
		case "FmLetter":
			if dist > 1 {
				return
			}
			w.face(p, n)
			w.fmLetterTalk(p, n)
		case "FmHealer":
			if dist > 1 {
				return
			}
			w.face(p, n)
			w.fmHealerTalk(p, n)
		case "PetShop":
			if dist > 2 {
				return
			}
			w.face(p, n)
			w.petShopTalk(p, n)
		case "PetSkillShop":
			if dist > 2 {
				return
			}
			w.face(p, n)
			w.petSkillShopOpen(p, n)
		case "Action":
			if dist > 1 {
				return
			}
			w.face(p, n)
			w.actionTalk(p, n)
		}
	})
}

// face 让 p 转向 t（八方向中最接近的一个）并通知周围玩家。
func (w *World) face(p, t *entity) {
	dx, dy := sign(t.x-p.x), sign(t.y-p.y)
	for d, m := range MOVE {
		if m[0] == dx && m[1] == dy && p.dir != d {
			p.dir = d
			for _, o := range w.floors[p.floor] {
				if o.client != nil && (o == p || o.visible[p.id]) {
					o.client.Send(&proto.ObjTurn{ID: p.id, Dir: d})
				}
			}
		}
	}
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// NpcList 是世界地图要的一层楼全部 NPC 的位置（H5 新增：原版自动地图只画视野内的人）。不限楼层（NPC 站哪里不是秘密），
// 没名字的（传送点 npcgen_warp 等）与按时间消失的不列；巴士 / 飞机列当下的位置。
func (w *World) NpcList(charID, floor int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || w.game.Floors[floor] == nil {
			return
		}
		m := &proto.NpcList{Floor: floor, Npcs: []proto.NpcPos{}}
		for _, o := range w.floors[floor] {
			if o.kind == "npc" && o.name != "" && !o.hidden() {
				m.Npcs = append(m.Npcs, proto.NpcPos{Name: o.name, X: o.x, Y: o.y})
			}
		}
		slices.SortFunc(m.Npcs, func(a, b proto.NpcPos) int { return strings.Compare(a.Name, b.Name) })
		e.client.Send(m)
	})
}

func (w *World) MapReq(charID, floor, x1, y1, x2, y2 int) {
	w.Do(func() {
		e := w.players[charID]
		f := w.game.Floors[floor]
		// 先把座标限制在楼层附近再相减：极端值（±MaxInt）相减会溢出，绕过边长检查后 make 负长度就 panic
		// 不限楼层：世界地图看去过的别层时会补要那一层看过的格子（地图资料不是秘密，一次最多 64×64）
		if e == nil || f == nil || x1 < -MaxMapReq || y1 < -MaxMapReq || x2 > f.W+MaxMapReq || y2 > f.H+MaxMapReq ||
			x2 <= x1 || y2 <= y1 || x2-x1 > MaxMapReq || y2-y1 > MaxMapReq {
			return
		}
		n := (x2 - x1) * (y2 - y1)
		md := &proto.MapData{Floor: floor, X1: x1, Y1: y1, X2: x2, Y2: y2, Tiles: make([]int, n), Objs: make([]int, n)}
		// walk：每格一个字符，'1' 可走、'0' 不可走、'2' 传送点。由服务端按 mapset 规则算好，客户端预判与服务端一致
		walk := make([]byte, n)
		i := 0
		for y := y1; y < y2; y++ {
			for x := x1; x < x2; x++ {
				walk[i] = '0'
				if f.In(x, y) {
					md.Tiles[i], md.Objs[i] = int(f.Tiles[y*f.W+x]), int(f.Objs[y*f.W+x])
					if _, ok := w.game.WarpAt(floor, x, y); ok {
						walk[i] = '2'
					} else if w.game.Tiles.Walkable(f, x, y) {
						walk[i] = '1'
					}
				}
				i++
			}
		}
		md.Walk = string(walk)
		md.Ver = w.floorVer(floor)
		e.client.Send(md)
	})
}

// Chat 发言。color 为原版文字调色 0..9（FONT_PAL_*）；area 为聊天设定里的"音量"1..10（原版 NowMaxVoice）。
// 附近频道按原版 CHAR_Talk 决定听得到的范围：音量 = area-1+句末'!'个数，范围查 chatvol 表；
// 以"..."结尾是悄悄话，只有脚下和面前一格的人听得到。
func (w *World) Chat(charID int, channel, text string, color, area int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || text == "" || !utf8.ValidString(text) {
			return
		}
		if utf8.RuneCountInString(text) > MaxChatRunes {
			text = string([]rune(text)[:MaxChatRunes])
		}
		// 发言限速：一个人刷屏会把看得到他的每个人的发送队列塞满（慢的那个会被踢），所以每秒最多几句
		now := w.now()
		if e.chatAt.IsZero() {
			e.chatTokens = chatBurst
		} else {
			e.chatTokens = min(chatBurst, e.chatTokens+now.Sub(e.chatAt).Seconds()*chatPerSecond)
		}
		e.chatAt = now
		if e.chatTokens < 1 {
			w.sysMsg(e, "说话太快了，慢一点。", colorRed)
			return
		}
		e.chatTokens--
		if e.gm && strings.HasPrefix(text, "/") && w.gmCommand(e, text) {
			return
		}
		if w.now().Before(e.muteUntil) {
			w.sysMsg(e, fmt.Sprintf("你被禁言了，还有 %d 分钟。", int(e.muteUntil.Sub(w.now()).Minutes())+1), colorRed)
			return
		}

		if color < 0 || color >= FontPalCount {
			color = 0
		}
		if strings.HasPrefix(text, "/tell ") { // 密语（char_talk.c OneByOneTkChannel，_TELLCHANNEL）
			w.tell(e, strings.TrimSpace(text[len("/tell "):]), color)
			return
		}
		msg := &proto.ChatMsg{Channel: channel, FromID: e.id, From: e.name, Text: text, Color: color}
		switch channel {
		case "world":
			// char_talk.c _THE_WORLD_SEND：间隔 5 秒以上、转生与等级够、声望够（每句扣掉）才说得出去
			now := w.now()
			r := w.Rules
			switch {
			case now.Sub(e.worldSentAt) <= worldChatGap:
				w.sysMsg(e, "每次使用世界频道需间隔五秒才能使用世界频道!", colorRed)
				return
			case e.char.Trans < r.WorldTrans || e.char.Level < r.WorldLevel:
				w.sysMsg(e, "您的转生等级未达到要求,暂不能使用世界频道!", colorRed)
				return
			case e.char.Fame < r.WorldFame:
				w.sysMsg(e, "您的声望不足,暂无法使用世界频道!", colorRed)
				return
			}
			e.char.Fame -= r.WorldFame
			e.worldSentAt = now
			if r.WorldFame > 0 {
				w.saveProgress(e)
			}
			for _, p := range w.players {
				p.client.Send(msg)
			}
		case "allserv":
			// 星球频道（char_talk.c _ALL_SERV_SEND：转生、等级、声望门槛同世界频道的写法；原版经 AC 广播到所有 gmsv，这里是同一进程的所有线路）
			r := w.Rules
			switch {
			case e.char.Trans < r.AllServTrans || e.char.Level < r.AllServLevel:
				w.sysMsg(e, "您的转生等级未达到要求,暂不能使用星球频道!", colorRed)
				return
			case e.char.Fame < r.AllServFame:
				w.sysMsg(e, fmt.Sprintf("您的声望不足%d，无法使用星球频道功能！", r.AllServFame), colorRed)
				return
			}
			e.char.Fame -= r.AllServFame
			if r.AllServFame > 0 {
				w.saveProgress(e)
			}
			// saacproto_AllServSend_recv：所有线路的所有人都收到，颜色随机（rand() % CHAR_COLORNUM）
			msg := &proto.ChatMsg{Channel: "allserv", Text: fmt.Sprintf("[星球%s] %s 说：%s", w.Name, e.name, text), Color: w.rnd.IntN(FontPalCount)}
			w.fam.each(func(o *World) {
				for _, p := range o.players {
					p.client.Send(msg)
				}
			})
		case "family": // 家族频道（客户端的 /FM）
			w.familyChat(e, msg)
		case "party":
			// 队伍频道（CHAR_ISPARTYCHAT）：只有同一队的人听得到，"[队]" 开头（_CHANNEL_MODIFY 的 P|T|）
			if e.party == nil {
				return
			}
			msg.Text = "[队]" + text
			for _, m := range e.party.slots {
				if m != nil {
					m.client.Send(msg)
				}
			}
		default:
			msg.Channel = "near"
			hear := chatHears(e, text, area)
			for _, o := range w.floors[e.floor] {
				if o.client != nil && (o == e || (o.visible[e.id] && hear(o))) {
					o.client.Send(msg)
				}
			}
			w.talkAction(e, text)
			if e.party != nil { // CHAR_Talk：队员不管多远都听得到
				for _, m := range e.party.slots {
					if m != nil && m != e && !(m.floor == e.floor && m.visible[e.id] && hear(m)) {
						m.client.Send(msg)
					}
				}
			}
			// 原版 CHAR_Talk 会把话转给听得到的 NPC（Talked）；用得到说的内容的是带暗语（warp_msg）的传送员和道具店（buy_msg 等）
			if e.fight == nil {
				for _, o := range w.floors[e.floor] {
					if o.npc == nil || !hear(o) || e.window != nil && e.window.kind == "shop" {
						continue
					}
					switch {
					case o.npc.Function == "WarpMan" && strings.Contains(o.npc.Arg, "warp_msg"):
						w.warpmanTalk(e, o, text)
					case o.npc.Function == "ItemShop":
						w.shopTalk(e, o, text)
					case o.npc.Function == "ExChangeMan" && strings.Contains(o.npc.Arg, "KeyWord"):
						w.exchangeTalk(e, o, text)
					case o.npc.Function == "Mic":
						w.micTalk(e, o, text)
					}
				}
			}
		}
	})
}

// FontPalCount 原版 FONT_PAL_NUM：可选的文字颜色数。
const FontPalCount = 10

// chatVolumeRange 原版 char_talk.c CHAR_getRangeFromVolume 的 chatvol 表。
var chatVolumeRange = []int{3, 5, 7, 9, 11, 13, 15, 17, 19, 21, 23, 25, 27, 29, 31}

// textVolume 原版 CHAR_getVolume：句末每个'!'加大一级音量，"..."结尾返回 -1（悄悄话）。
func textVolume(s string) int {
	switch {
	case len(s) >= 3 && strings.HasSuffix(s, "..."):
		return -1
	case strings.HasSuffix(s, "."):
		return 0
	}
	return len(s) - len(strings.TrimRight(s, "!"))
}

// chatHears 返回判断某个实体能否听到 e 这句话的函数。
func chatHears(e *entity, text string, area int) func(o *entity) bool {
	if area <= 0 {
		area = 3 // 原版：area 为 0 时按 3
	}
	area = min(area, 10) // 聊天设定的音量 1..10（NowMaxVoice）；再大就是乱送的，下面的下标相加会溢出
	v := textVolume(text)

	if v < 0 {
		fx, fy := e.x+MOVE[e.dir][0], e.y+MOVE[e.dir][1]
		return func(o *entity) bool { return (o.x == e.x && o.y == e.y) || (o.x == fx && o.y == fy) }
	}
	r := chatVolumeRange[min(area-1+v, len(chatVolumeRange)-1)]
	return func(o *entity) bool { return abs(o.x-e.x) <= r && abs(o.y-e.y) <= r }
}

// ReturnHome 回记录点（原版 _CHAR_NEWLOGOUT 的 CharLogout(1)：不下线，传回 CHAR_LASTTALKELDER 的长老位置）。
// 没在记录点登记过时是出生村落的村长家门口。
func (w *World) ReturnHome(charID int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil {
			return
		}
		// lssproto_CharLogout_recv（flg 1 回记录点，_CHAR_NEWLOGOUT）的检查，照原版的顺序
		h := w.elderPos(e)
		msg := ""
		switch {
		case e.party != nil && h.Floor == 30691: // _NO_TEAMWARP_SKYLAND
			msg = "团队中无法回天空之岛！"
		case e.fight != nil || e.watch != nil:
			msg = "战斗中无法回纪录点！"
		case w.hasItemWhere(e, func(d *data.ItemDef) bool { return d.Type == data.ItemWares }): // _ITEM_CHECKWARES
			msg = "携带货物无法使用。"
		case e.isFollower(): // 队员不能回记录点，队长回就全队一起
			msg = "团队中无法回纪录点！"
		case w.hasItemWhere(e, func(d *data.ItemDef) bool { return d.DropAtLogout }): // _ITEM_CHECKDROPATLOGOUT
			msg = "携带的物品使你无法回纪录点！"
		}
		if msg != "" {
			w.sysMsg(e, msg, colorYellow)
			return
		}
		w.placeOnFloor(e, h.Floor, h.X, h.Y, e.dir)
		w.save(e)
	})
}

// tell 是 OneByOneTkChannel（_CHANNEL_MODIFY）："/tell 名字 内容" 只说给这条线路上叫那个名字的人；对方关了密语频道回"X 关闭了此频道"，
// 双方各看到"你告诉X：…" / "X告诉你：…"（P|M|）。找不到人时的那一句是 H5 加的（原版不回话）。
func (w *World) tell(e *entity, rest string, color int) {
	name, text, ok := strings.Cut(rest, " ")
	text = strings.TrimSpace(text)
	if !ok || name == "" || text == "" {
		w.sysMsg(e, "用法：/tell 名字 内容", colorYellow)
		return
	}
	var to *entity
	for _, p := range w.players {
		if p.name == name {
			to = p
			break
		}
	}
	switch {
	case to == nil:
		w.sysMsg(e, fmt.Sprintf("找不到叫 %s 的人。", name), colorYellow)
	case to == e:
	case to.fs&fsTell == 0:
		w.sysMsg(e, fmt.Sprintf("%s 关闭了此频道", name), color)
	default:
		w.sysMsg(e, fmt.Sprintf("你告诉%s：%s", name, text), color)
		w.sysMsg(to, fmt.Sprintf("%s告诉你：%s", e.name, text), color)
	}
}

// ---------------- 楼层与视野 ----------------

func (w *World) addToFloor(e *entity) {
	if w.floors[e.floor] == nil {
		w.floors[e.floor] = map[int]*entity{}
	}
	w.floors[e.floor][e.id] = e
}

// placeOnFloor 把玩家放到某楼层的某格（上线或传送），发 EnterFloor 并重建视野。
func (w *World) placeOnFloor(e *entity, floor, x, y, dir int) {
	w.stopWatch(e, true) // 被传送走就不看了
	e.stay = false
	e.clearAction()
	w.cancelTrade(e)
	w.vendorClear(e)
	w.removeFromFloor(e)
	e.floor, e.x, e.y, e.dir = floor, x, y, dir
	w.addToFloor(e)
	f := w.game.Floors[floor]
	e.client.Send(&proto.EnterFloor{Floor: floor, Name: f.Name, Palette: f.Palette, Width: f.W, Height: f.H, X: x, Y: y, Dir: dir, Step: w.clientStepMs(), MapVer: w.floorVer(floor)})
	e.visible = map[int]bool{}
	w.refreshView(e)
	w.announce(e)
	switch {
	case e.isLeader() && !w.partyWarping:
		// 队长传送时队员传到同一格（EVENT_onWarpNPC、MAPPOINT_MapWarpHandle、回记录点都是全队一起）
		w.partyWarping = true
		for _, m := range e.party.slots[1:] {
			if m != nil && m.fight == nil {
				w.placeOnFloor(m, floor, x, y, dir)
			}
		}
		w.partyWarping = false
	case e.isFollower() && !w.partyWarping:
		w.leaveParty(e, true) // 队员自己被传走就离队（原版留在队伍里但跟不上）
	}
}

// removeFromFloor 从当前楼层移除，并让看得到它的玩家删掉它。
func (w *World) removeFromFloor(e *entity) {
	for _, o := range w.floors[e.floor] {
		if o != e && o.client != nil && o.visible[e.id] {
			delete(o.visible, e.id)
			o.client.Send(&proto.ObjDisappear{IDs: []int{e.id}})
		}
	}
	delete(w.floors[e.floor], e.id)
}

func near(a, b *entity) bool {
	return a.floor == b.floor && !a.hidden() && !b.hidden() && abs(a.x-b.x) <= ViewRadius && abs(a.y-b.y) <= ViewRadius
}

// refreshView 重新计算 e 能看到的实体，发出增减。
func (w *World) refreshView(e *entity) {
	appear := []proto.ObjInfo{}
	gone := []int{}
	for _, o := range w.floors[e.floor] {
		if o == e {
			continue
		}
		in := near(e, o)
		if in && !e.visible[o.id] {
			e.visible[o.id] = true
			appear = append(appear, o.info())
		} else if !in && e.visible[o.id] {
			delete(e.visible, o.id)
			gone = append(gone, o.id)
		}
	}
	for id := range e.visible {
		if o := w.floors[e.floor][id]; o == nil {
			delete(e.visible, id)
			gone = append(gone, id)
		}
	}
	if len(appear) > 0 {
		e.client.Send(&proto.ObjAppear{Objs: appear})
	}
	if len(gone) > 0 {
		e.client.Send(&proto.ObjDisappear{IDs: gone})
	}
}

// announce 让附近的其他玩家看到 e（新出现）。
func (w *World) announce(e *entity) {
	for _, o := range w.floors[e.floor] {
		if o != e && o.client != nil && near(o, e) && !o.visible[e.id] {
			o.visible[e.id] = true
			o.client.Send(&proto.ObjAppear{Objs: []proto.ObjInfo{e.info()}})
		}
	}
}

// broadcastMove 在 e 走了一步之后：更新它自己的视野，并通知其他玩家（移动 / 出现 / 消失）。
func (w *World) broadcastMove(e *entity) {
	w.refreshView(e)
	for _, o := range w.floors[e.floor] {
		if o == e || o.client == nil {
			continue
		}
		in := near(o, e)
		switch {
		case in && o.visible[e.id]:
			o.client.Send(&proto.ObjMove{ID: e.id, X: e.x, Y: e.y, Dir: e.dir})
		case in:
			o.visible[e.id] = true
			o.client.Send(&proto.ObjAppear{Objs: []proto.ObjInfo{e.info()}})
		case o.visible[e.id]:
			delete(o.visible, e.id)
			o.client.Send(&proto.ObjDisappear{IDs: []int{e.id}})
		}
	}
}

// ---------------- 存档 ----------------

// save 在后台写库，不阻塞世界协程。
func (w *World) save(e *entity) {
	id, fl, x, y, d := e.char.ID, e.floor, e.x, e.y, e.dir
	w.enqueue(fmt.Sprintf("pos:%d", id), func() { w.write(id, fl, x, y, d) })
}

// enqueue 把一笔写库排进存档队列；同一个 key 还在排队时只换成最新的 job（合并），所以每个 key 在队列里顶多一笔。
func (w *World) enqueue(key string, job func()) {
	w.saveMu.Lock()
	if w.saveLatest == nil {
		w.saveLatest = map[string]func(){}
	}
	_, queued := w.saveLatest[key]
	w.saveLatest[key] = job
	w.saveMu.Unlock()
	if queued {
		return
	}
	w.saves <- func() {
		w.saveMu.Lock()
		j := w.saveLatest[key]
		delete(w.saveLatest, key)
		w.saveMu.Unlock()
		if j != nil {
			j()
		}
	}
}

func (w *World) write(id int64, fl, x, y, d int) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.db.SavePosition(ctx, id, fl, x, y, d); err != nil {
		w.log.Error("存档失败", "char", id, "err", err)
	}
}

func (w *World) saveAll() {
	for _, e := range w.players {
		w.save(e)
	}
}

// Online 返回在线人数（调试用）。
// Flush 等存档队列里排在前面的写入都做完（换线路重登前要等旧线路存完，不然读到旧存档）。
func (w *World) Flush(timeout time.Duration) bool {
	done := make(chan struct{})
	select {
	case w.saves <- func() { close(done) }:
	case <-time.After(timeout):
		return false
	}
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (w *World) Online() (n int) {
	w.Do(func() { n = len(w.players) })
	return
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
