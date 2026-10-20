package world

// 家族（char/family.c、saac/acfamily.c）。原版的家族资料放在帐号服务器（AC），各个 gmsv 用 saacproto 来回；
// 这里所有线路共用一个 Families（锁保护），直接改资料、写进 families 表，在线的族人用 post 送到他所在的线路。
// _FMVER21：职位 族长 3 / 长老 4 / 申请中 2 / 一般成员 1；_PERSONAL_FAME 个人声望；_NEW_MANOR_LAW 个人与家族的气势。
//
// 锁的规矩：玩家的指令、NPC 对话等入口拿锁（famLock），里面的函数假定锁已经拿着；送到别的线路的 post 函数自己再拿锁。
// 实体上的 fm（家族名、职位、庄园楼层……）是锁外可读的快照，由 famSync 更新。
//
// 与原版不同：
//   - 成立手续费照家族管理员的条件收 10000（原版 FAMILY_Add 只收 1000，失败却退 10000000），失败退回同样的钱；
//   - 家族徽章（_FAMILYBADGE_）的图不在资源里，没有做；
//   - 只有一个"星球"，族人名单的星球一栏都是"无"；上线时直接重算，不需要 ACFMCharLogin 的来回。

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"sa/server/internal/proto"
	"sa/server/internal/store"
)

const (
	fmDutyNone   = -1 // FMMEMBER_NONE
	fmDutyMember = 1
	fmDutyApply  = 2
	fmDutyLeader = 3
	fmDutyElder  = 4

	fmMaxMember     = 50            // FAMILY_MAX_MEMBER（gmsv 的阵列大小；AC 与客户端写 100）
	fmMaxNum        = 1000          // FAMILY_MAXNUM
	fmMaxElder      = 5             // FMELDERNUM
	fmSetupNum      = 5             // MIN_MEMBERNUM：正式族人到这个数才算成立
	fmFee           = 10000         // 成立手续费（见文件开头）
	fmLeaderLv      = 30            // FMLEADERLV
	fmMemoMax       = 35            // DENGONFILELINENUM
	fmsMemoMax      = 140           // FMSDENGONFILELINENUM
	fmMaxGold       = 1_000_000_000 // CHAR_MAXFMBANKGOLDHAVE：家族共同帐户
	fmMaxBank       = 100_000_000   // CHAR_MAXBANKGOLDHAVE：家族银行个人帐户
	maxMomentum     = 10_000_000    // MAX_PERSONAL_MOMENTUM
	fmChannelLeader = 5             // FAMILY_MAXCHANNEL：族长广播
)

// fmDutyName 是 AC 的 MEMBERKIND_INTDATA（"%s族长已经将你的家族职等改为%s！"等用）。
var fmDutyName = map[int]string{fmDutyMember: "一般成员", fmDutyApply: "申请中", fmDutyLeader: "族长", fmDutyElder: "长老"}

// fmMember 是 AC 的 MEMBERDATA（slot 0 永远是族长）。
type fmMember struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Level    int    `json:"lv"`
	Duty     int    `json:"duty"`
	Fame     int    `json:"fame"` // popular：个人声望（内部值 ×100）
	Momentum int    `json:"mom"`  // 个人气势（×100）
	Event    bool   `json:"ev"`   // eventflag：上次上线时 CheckLeaderQ 通过（可以当长老、族长）
}

func (m *fmMember) formal() bool { return m.Duty != fmDutyApply }

// family 是 AC 的 struct FAMILY。声望都是内部值（×100），显示时 ÷100。
type family struct {
	ID         int                     `json:"id"` // fmindex 1..1000
	Name       string                  `json:"name"`
	LeaderFace int                     `json:"face"`
	PetName    string                  `json:"pet"`
	PetAttr    string                  `json:"petattr"` // "图号 攻 防 敏"
	Accept     bool                    `json:"accept"`
	Setup      bool                    `json:"setup"`
	Rule       string                  `json:"rule"`
	Sprite     int                     `json:"sprite"` // 守护精灵：0 光明、1 黑暗
	Adv        int                     `json:"adv"`    // 冒险（FM_FIX_FMADV）
	Feed       int                     `json:"feed"`   // 饲育（FM_FIX_FMFEED）
	PK         int                     `json:"pk"`     // 家族战（没有做，永远 0）
	Gold       int                     `json:"gold"`   // 家族共同帐户 fmgold
	Memos      []string                `json:"memos,omitempty"`
	Created    int64                   `json:"created"`
	Members    [fmMaxMember]*fmMember  `json:"members"`
}

func (f *family) leader() *fmMember { return f.Members[0] }

func (f *family) member(id int64) (int, *fmMember) {
	for i, m := range f.Members {
		if m != nil && m.ID == id {
			return i, m
		}
	}
	return -1, nil
}

// num 是 fmnum（含申请中的人），joinNum 是 fmjoinnum（正式族人）。
func (f *family) num() (n int) {
	for _, m := range f.Members {
		if m != nil {
			n++
		}
	}
	return
}

func (f *family) joinNum() (n int) {
	for _, m := range f.Members {
		if m != nil && m.formal() {
			n++
		}
	}
	return
}

func (f *family) elders() (n int) {
	for _, m := range f.Members {
		if m != nil && m.Duty == fmDutyElder {
			n++
		}
	}
	return
}

// popular 是 fmpopular，total 是 fmtotalfame（加上正式族人的个人声望），momentum 是 fmmomentum（acfamily.c 的公式）。
func (f *family) popular() int { return f.Adv + f.Feed + f.PK }

func (f *family) total() int {
	t := f.popular()
	for _, m := range f.Members {
		if m != nil && m.formal() {
			t += m.Fame
		}
	}
	return t
}

func (f *family) momentum() int {
	j := f.joinNum()
	t := (j*j/10 + 1) * 100
	for _, m := range f.Members {
		if m != nil && m.formal() {
			t += m.Momentum
		}
	}
	return min(t, 2_000_000_000)
}

// recount 是 AC 在族人变动后做的：正式族人到 fmSetupNum 成立，满了就不再招人。
func (f *family) recount() {
	j := f.joinNum()
	f.Setup = j >= fmSetupNum
	if f.num() >= fmMaxMember {
		f.Accept = false
	}
}

// Families 是所有线路共用的家族资料（原版 AC 的 family 与 fmpoint，以及 gmsv 的 familyMemberIndex）。
type Families struct {
	mu      sync.Mutex
	db      store.Store
	log     *slog.Logger
	list    map[int]*family
	byChar  map[int64]*family
	manors  [len(manorTable)]manorState
	fmsMemo []string           // 家族之间留言板
	online  map[int64]*World   // 在线的角色 → 所在的线路
	change  map[int64]int64    // 族长让位：被问的人 → 族长（CHAR_WORKLEADERCHANGE）
	saves   chan func()
	ranking []*family          // 综合声望排行（按 total 由高到低），famRank 时重排
}

type manorState struct {
	Owner int   `json:"owner"` // 占有的家族 fmindex，0 没有
	Since int64 `json:"since"` // 占有的时间（fmpointtime）
}

// NewFamilies 读出全部家族（开机时 AC 读 data/family）。
func NewFamilies(db store.Store, log *slog.Logger) *Families {
	f := &Families{db: db, log: log, list: map[int]*family{}, byChar: map[int64]*family{},
		online: map[int64]*World{}, change: map[int64]int64{}, saves: make(chan func(), 4096)}
	go func() {
		for s := range f.saves {
			s()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := db.LoadFamilies(ctx)
	if err != nil {
		log.Error("读取家族失败", "err", err)
	}
	for id, s := range rows {
		fm := &family{}
		if err := json.Unmarshal([]byte(s), fm); err != nil || fm.leader() == nil {
			log.Error("家族资料有误", "id", id, "err", err)
			continue
		}
		fm.ID = id
		f.list[id] = fm
		for _, m := range fm.Members {
			if m != nil {
				f.byChar[m.ID] = fm
			}
		}
	}
	if s, _ := db.GetKV(ctx, "manors"); s != "" {
		json.Unmarshal([]byte(s), &f.manors)
	}
	for i := range f.manors { // 占有的家族已经不在了
		if f.list[f.manors[i].Owner] == nil {
			f.manors[i] = manorState{}
		}
	}
	if s, _ := db.GetKV(ctx, "fmsmemo"); s != "" {
		json.Unmarshal([]byte(s), &f.fmsMemo)
	}
	return f
}

// save 在锁里把资料编好，交给存档协程写。
func (f *Families) save(fm *family) {
	b, _ := json.Marshal(fm)
	id, s := fm.ID, string(b)
	f.saves <- func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.db.SaveFamily(ctx, id, s); err != nil {
			f.log.Error("保存家族失败", "id", id, "err", err)
		}
	}
}

func (f *Families) remove(fm *family) {
	id := fm.ID
	delete(f.list, id)
	f.saves <- func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.db.DeleteFamily(ctx, id); err != nil {
			f.log.Error("删除家族失败", "id", id, "err", err)
		}
	}
}

func (f *Families) saveKV(key string, v any) {
	b, _ := json.Marshal(v)
	s := string(b)
	f.saves <- func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.db.PutKV(ctx, key, s); err != nil {
			f.log.Error("保存失败", "key", key, "err", err)
		}
	}
}

// tell 把 fn 送到 id 所在的线路去做（不在线就不做）。fn 在那条线路的协程里执行，要用家族资料时自己拿锁。
func (f *Families) tell(id int64, fn func(w *World, e *entity)) {
	w := f.online[id]
	if w == nil {
		return
	}
	w.post(func() {
		if e := w.players[int(id)]; e != nil {
			fn(w, e)
		}
	})
}

// say 是 AC 送给族人的公告（ACFMAnnounce kind 3 等，红字）；msg 不在线的人收不到。
func (f *Families) say(id int64, text string, color int) {
	f.tell(id, func(w *World, e *entity) { w.sysMsg(e, text, color) })
}

// window 送一个没有 NPC 的文字窗口（lssproto_WN_send(..., -1, -1, ...)）。
func (f *Families) window(id int64, text string) {
	f.tell(id, func(w *World, e *entity) { w.fmMessage(e, text) })
}

// resync 让 id 重新读家族资料（换了职位、被踢、家族解散……）；leave 为真时同时做 SetFMVarInit。
func (f *Families) resync(id int64, leave bool) {
	f.tell(id, func(w *World, e *entity) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if leave {
			w.fmVarInit(e)
		}
		w.famSync(e)
	})
}

// newID 是最小的空号（ACAddFM）。
func (f *Families) newID() int {
	for i := 1; i <= fmMaxNum; i++ {
		if f.list[i] == nil {
			return i
		}
	}
	return -1
}

func (f *Families) byName(name string) *family {
	for _, fm := range f.list {
		if fm.Name == name {
			return fm
		}
	}
	return nil
}

// sorted 是按 fmindex 排的家族列表（familyListBuf）。
func (f *Families) sorted() []*family {
	out := make([]*family, 0, len(f.list))
	for _, fm := range f.list {
		out = append(out, fm)
	}
	slices.SortFunc(out, func(a, b *family) int { return a.ID - b.ID })
	return out
}

// rank 按 key 由高到低排（AC 的 SortFMTop；同分按编号）。
func (f *Families) rank(key func(*family) int) []*family {
	out := f.sorted()
	sort.SliceStable(out, func(i, j int) bool { return key(out[i]) > key(out[j]) })
	return out
}

// manorOf 是家族占有的庄园（0 起），没有为 -1。
func (f *Families) manorOf(fm *family) int {
	for i, m := range f.manors {
		if m.Owner == fm.ID {
			return i
		}
	}
	return -1
}

// ---------------- 世界这边 ----------------

// fmChar 是存在角色上的家族资料（characters.family）：个人气势 CHAR_MOMENTUM、家族银行个人帐户 CHAR_BANKGOLD。
type fmChar struct {
	Momentum int `json:"mom,omitempty"`
	Bank     int `json:"bank,omitempty"`
}

func parseFmChar(s string) fmChar {
	var c fmChar
	if s != "" {
		json.Unmarshal([]byte(s), &c)
	}
	return c
}

func (c fmChar) String() string {
	b, _ := json.Marshal(c)
	return string(b)
}

// famView 是实体上的家族快照（CHAR_FMINDEX、CHAR_FMNAME、CHAR_FMLEADERFLAG、CHAR_FMSPRITE、CHAR_WORKFMFLOOR、CHAR_WORKFMCHANNEL）。
type famView struct {
	Index   int // 0 没有家族
	Name    string
	Duty    int
	Sprite  int
	Floor   int // 占有的庄园的族长家（1041 / 2031 / 3031 / 4031），没有为 0
	Manor   int // 占有的庄园（0 起），没有为 -1
	Channel int // 0 家族频道，5 族长广播，没有家族为 -1
}

func (v famView) formal() bool { return v.Index > 0 && v.Duty > 0 && v.Duty != fmDutyApply }

// famLock 拿家族资料的锁，回传放锁的函数。
func (w *World) famLock() func() {
	w.fam.mu.Lock()
	return w.fam.mu.Unlock
}

// fmOf 是 e 所在的家族与他的那一格（锁里用）。
func (w *World) fmOf(e *entity) (*family, int, *fmMember) {
	fm := w.fam.byChar[e.char.ID]
	if fm == nil {
		return nil, -1, nil
	}
	i, m := fm.member(e.char.ID)
	if m == nil {
		return nil, -1, nil
	}
	return fm, i, m
}

// checkLeaderQ 是 CheckLeaderQ：30 级或转生过，而且做过成人礼（事件 4）。
func checkLeaderQ(e *entity) int {
	if e.char.Level < fmLeaderLv && e.char.Trans == 0 {
		return -1
	}
	if !e.eventDone(4) {
		return -2
	}
	return 0
}

// famSync 按家族资料更新 e 的快照，发 F 状态，家族名变了就告诉看得到的人（CHAR_sendCToArroundCharacter）。
func (w *World) famSync(e *entity) {
	old := e.fm.Name
	v := famView{Duty: fmDutyNone, Channel: -1, Manor: -1}
	if fm, _, m := w.fmOf(e); fm != nil {
		v = famView{Index: fm.ID, Name: fm.Name, Duty: m.Duty, Sprite: fm.Sprite, Manor: w.fam.manorOf(fm), Channel: max(e.fm.Channel, 0)}
		if v.Duty != fmDutyLeader && v.Channel == fmChannelLeader {
			v.Channel = 0
		}
		if v.Manor >= 0 && v.formal() {
			v.Floor = manorTable[v.Manor].home
		}
	}
	// 加入或离开家族时开关家族频道（客户端在 F 状态的职位是 −1 时关掉 PC_ETCFLAG_CHAT_FM，有家族时上线就开着）
	if had, has := old != "", v.Index > 0; had != has {
		if has {
			e.fs |= fsFM
		} else {
			e.fs &^= fsFM
		}
		e.client.Send(&proto.Flags{Flags: e.fs})
	}
	e.fm = v
	e.client.Send(&proto.FamilyStatus{Index: v.Index, Name: v.Name, Duty: v.Duty, Channel: v.Channel, Sprite: v.Sprite})
	if v.Name != old {
		for _, o := range w.floors[e.floor] {
			if o.client != nil && o != e && o.visible[e.id] {
				o.client.Send(&proto.ObjFamily{ID: e.id, Family: v.Name})
			}
		}
	}
}

// fmVarInit 是 SetFMVarInit：离开家族时清掉守护兽的记号、下骑宠（家族骑宠要族人才能骑）。
func (w *World) fmVarInit(e *entity) {
	w.clearGuard(e)
	if e.pets.Ride >= 0 {
		w.dismount(e)
		w.petsChanged(e)
	}
}

// clearGuard 是 SetFMPetVarInit：身上和寄放的宠物都不再是守护兽。
func (w *World) clearGuard(e *entity) {
	changed := false
	for _, p := range e.pets.Pets {
		if p != nil && p.Guard {
			p.Guard, changed = false, true
		}
	}
	for _, p := range e.pets.Pool {
		if p != nil && p.Guard {
			p.Guard, changed = false, true
		}
	}
	if changed {
		w.petsChanged(e)
	}
}

// resetMomentum：成立、加入、退出家族时个人气势归零（"成立家族个人气势归零"等，黄字）。
func (w *World) resetMomentum(e *entity, text string) {
	e.fmc.Momentum = 0
	e.char.Family = e.fmc.String()
	w.saveProgress(e)
	w.sysMsg(e, text, colorYellow)
}

// famLogin 是上线时的 ACFMCharLogin（char.c 1794）：更新名单上的等级、声望与能否当族长，非族长的守护兽清掉，
// 族长没有守护兽时提醒。
func (w *World) famLogin(e *entity) {
	defer w.famLock()()
	e.fm.Channel = 0
	fm, _, m := w.fmOf(e)
	if fm == nil {
		w.fmVarInitQuiet(e)
		w.famSync(e)
		return
	}
	w.fam.online[e.char.ID] = w
	m.Name, m.Level, m.Fame, m.Momentum, m.Event = e.char.Name, e.char.Level, e.char.Fame, e.fmc.Momentum, checkLeaderQ(e) == 0
	if m.Duty == fmDutyLeader {
		fm.LeaderFace = e.char.Face
	}
	w.fam.save(fm)
	if m.Duty != fmDutyLeader {
		w.clearGuard(e)
	} else if !hasGuard(e) {
		w.sysMsg(e, "\n家族守护兽消失了！\n请立刻再选定一只守护兽，\n否则家族在七天之後会消失唷！\n", colorRed)
	}
	w.famSync(e)
}

// fmVarInitQuiet：上线时没有家族，把旧的守护兽记号清掉（不下骑宠，那由骑宠本身的条件决定）。
func (w *World) fmVarInitQuiet(e *entity) { w.clearGuard(e) }

func hasGuard(e *entity) bool {
	for _, p := range e.pets.Pets {
		if p != nil && p.Guard {
			return true
		}
	}
	for _, p := range e.pets.Pool {
		if p != nil && p.Guard {
			return true
		}
	}
	return false
}

// famLogout 是 ACFMCharLogout。
func (w *World) famLogout(e *entity) {
	defer w.famLock()()
	f := w.fam
	if f.online[e.char.ID] == w {
		delete(f.online, e.char.ID)
	}
	delete(f.change, e.char.ID)
	if fm, _, m := w.fmOf(e); fm != nil {
		m.Level, m.Fame, m.Momentum = e.char.Level, e.char.Fame, e.fmc.Momentum
		f.save(fm)
	}
}

// famFame 在个人声望或气势变了之后更新名单（AC 的 FM_FIX_FMFEED / FMADV / FAME 里 data2 是新的个人声望）。
func (w *World) famFame(e *entity, adv, feed int) {
	defer w.famLock()()
	fm, _, m := w.fmOf(e)
	if fm == nil {
		return
	}
	m.Fame, m.Momentum = e.char.Fame, e.fmc.Momentum
	if m.formal() { // 申请中的只更新个人声望（FM_FIX_FAME）
		fm.Adv += adv
		fm.Feed += feed
	}
	w.fam.save(fm)
}

// fmAdvTbl 是 npcutil.c 的 FMAdvTbl：第一次做完某个事件时得到的声望（家族冒险）。
var fmAdvTbl = []int{0, 1, 1, 0, 10, 5, 0, 0, 30, 0, 0, 0, 68, 68, 0, 15, 120, 90, 0, 120,
	0, 0, 60, 0, 0, 0, 60, 0, 0, 0, 0, 60, 0, 0, 30, 225, 0, 0, 60, 135,
	600, 0, 360, 0, 0, 480, 840, 0, 600, 600, 600, 600, 600, 600, 720, 0, 0, 0, 0, 0,
	0, 0, 0, 120, 900, 0, 0, 0, 0, 0, 0, 15000, 20000, 0, 0, 0, 0, 0, 0, 0,
	0, 1200, 3000, 0, 1500, 0, 0, 100, 0, 400, 0, 0, 0, 0, 0, 0, 0, 100, 0, 0,
	0, 0, 0, 0, 0, 600, 0, 100, 0, 0}

// addFMAdv 是 AddFMAdv：任何人都得到个人声望，正式族人另外加到家族的冒险声望。
func (w *World) addFMAdv(e *entity, bit int) {
	if bit < 0 || bit >= len(fmAdvTbl) || fmAdvTbl[bit] <= 0 {
		return
	}
	v := fmAdvTbl[bit]
	w.earnFame(e, v)
	w.famFame(e, v, 0)
}

// fmMessage 送一个没有 NPC 的文字窗口（OK 按钮，回传不处理）。
func (w *World) fmMessage(e *entity, text string) {
	e.window = &npcWindow{npc: 0, seq: seqNone, kind: "message", buttons: BtnOK}
	e.client.Send(&proto.NpcWindow{ID: 0, Kind: "message", Buttons: BtnOK, Seq: seqNone, Text: text})
}

// fmWindow 送家族的窗口（_FM_MODIFY：布告栏等都不带 NPC，npc 用 0）。
func (w *World) fmWindow(e *entity, kind string, buttons, seq int, text string) {
	e.window = &npcWindow{npc: 0, seq: seq, kind: kind, buttons: buttons}
	e.client.Send(&proto.NpcWindow{ID: 0, Kind: kind, Buttons: buttons, Seq: seq, Text: text})
}

// fmEscape / fmUnescape 是 makeEscapeString / makeStringFromEscaped（FM 字串用 | 分开，字串里的 | 与 \ 要跳脱）。
var (
	fmEscaper   = strings.NewReplacer(`\`, `\\`, "|", `\|`)
	fmUnescaper = strings.NewReplacer(`\\`, `\`, `\|`, "|")
)

func fmEscape(s string) string   { return fmEscaper.Replace(s) }
func fmUnescape(s string) string { return fmUnescaper.Replace(s) }

// fmSplit 按没有跳脱的 | 切开。
func fmSplit(s string) []string {
	var out []string
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s):
			b.WriteByte(s[i])
			b.WriteByte(s[i+1])
			i++
		case s[i] == '|':
			out = append(out, b.String())
			b.Reset()
		default:
			b.WriteByte(s[i])
		}
	}
	return append(out, b.String())
}

// fmField 取第 i 段（没有为空字串）并解开跳脱。
func fmField(f []string, i int) string {
	if i < len(f) {
		return fmUnescape(f[i])
	}
	return ""
}


