// Package server 负责 WebSocket 连接与会话：鉴权 → 选择线路 → 角色管理 → 把游戏指令转给对应线路的 world。
// 流程与原客户端一致：输入账号密码（idPasswordProc）→ 选大区与线路（titleProc）→ 角色选择/创建 → 进入游戏。
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"

	"sa/server/internal/data"
	"sa/server/internal/proto"
	"sa/server/internal/store"
	"sa/server/internal/world"
)

const (
	Version       = "h5-m1"
	MaxChars      = 2 // 原版每个账号 2 个角色位（MAXCHARACTER）
	maxMsgBytes   = 16 << 10
	sendQueue     = 256
	msgsPerSecond = 40
	idleTimeout   = 90 * time.Second
	charNameLen   = 8 // 原客户端 CHAR_NAME_LEN 为 16 字节，即 8 个汉字
)

var accountRe = regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`)

// Line 是一条线路（原版的一台 gmsv）。同一大区的线路共用账号与角色，但世界互相独立。
type Line struct {
	ID    int
	Group string
	Name  string
	World *world.World
}

type Server struct {
	Lines   []*Line
	Store   store.Store
	Log     *slog.Logger
	Origins []string // 允许的页面来源；为空时只允许同源
	// MaxChars 是每个账号的角色位（config.yaml maxChars），0 用原版的 MaxChars
	MaxChars int

	mu     sync.Mutex
	online map[int64]*session // 角色 ID → 当前在线的会话（跨线路唯一）
}

func (s *Server) maxChars() int {
	if s.MaxChars > 0 {
		return s.MaxChars
	}
	return MaxChars
}

func (s *Server) line(id int) *Line {
	for _, l := range s.Lines {
		if l.ID == id {
			return l
		}
	}
	return nil
}

// ServeHTTP 处理 /ws 的 WebSocket 升级。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: s.Origins})
	if err != nil {
		return
	}
	conn.SetReadLimit(maxMsgBytes)
	sess := &session{srv: s, conn: conn, out: make(chan []byte, sendQueue), log: s.Log.With("remote", r.RemoteAddr)}
	sess.run(r.Context())
}

type session struct {
	srv     *Server
	conn    *websocket.Conn
	out     chan []byte
	log     *slog.Logger
	account store.Account
	line    *Line            // 已选择的线路
	char    *store.Character // 进入世界后非空
	closeMu sync.Once
	cancel  context.CancelFunc
}

// Account 是登录的账号名（世界按它判断是不是 GM，World.GMs）。
func (s *session) Account() string { return s.account.Name }

// Send 实现 world.Client。队列满说明客户端太慢，直接断开，避免拖慢世界协程。
func (s *session) Send(m proto.Message) {
	b, err := proto.Encode(m)
	if err != nil {
		s.log.Error("编码失败", "err", err)
		return
	}
	select {
	case s.out <- b:
	default:
		s.Kick("发送队列已满")
	}
}

func (s *session) Kick(reason string) {
	s.closeMu.Do(func() {
		s.log.Info("断开", "reason", reason)
		if s.cancel != nil {
			s.cancel()
		}
		go s.conn.Close(websocket.StatusPolicyViolation, reason)
	})
}

func (s *session) run(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	defer cancel()
	go s.writeLoop(ctx)
	s.Send(&proto.Hello{Version: Version, ServerTime: int(time.Now().Unix())})

	tokens, last := float64(msgsPerSecond), time.Now()
	for {
		rctx, rcancel := context.WithTimeout(ctx, idleTimeout)
		typ, b, err := s.conn.Read(rctx)
		rcancel()
		if err != nil {
			break
		}
		if typ != websocket.MessageText {
			s.Kick("只接受文本消息")
			break
		}
		now := time.Now()
		tokens = min(msgsPerSecond, tokens+now.Sub(last).Seconds()*msgsPerSecond)
		last = now
		if tokens < 1 {
			s.Send(&proto.Error{Code: "rate", Message: "发送过快"})
			continue
		}
		tokens--
		m, err := proto.DecodeClient(b)
		if err != nil {
			s.Send(&proto.Error{Code: "bad_message", Message: err.Error()})
			continue
		}
		s.handle(ctx, m)
	}
	s.leaveWorld()
	s.conn.CloseNow()
}

func (s *session) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case b := <-s.out:
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := s.conn.Write(wctx, websocket.MessageText, b)
			cancel()
			if err != nil {
				s.Kick("写入失败")
				return
			}
		}
	}
}

func (s *session) fail(code, msg string) { s.Send(&proto.Error{Code: code, Message: msg}) }

// handle 按会话阶段分派：未登录 → 已登录未选线路 → 选好线路（角色管理）→ 在世界中。
func (s *session) handle(ctx context.Context, m proto.Message) {
	if p, ok := m.(*proto.Ping); ok {
		s.Send(&proto.Pong{Seq: p.Seq, ServerTime: int(time.Now().Unix())})
		return
	}
	switch {
	case s.account.ID == 0:
		s.handleAuth(ctx, m)
	case s.char == nil:
		s.handleLobby(ctx, m)
	default:
		s.handleWorld(m)
	}
}

func (s *session) handleAuth(ctx context.Context, m proto.Message) {
	st := s.srv.Store
	switch m := m.(type) {
	case *proto.Register:
		if !accountRe.MatchString(m.Account) || len(m.Password) < 4 || len(m.Password) > 64 {
			s.Send(&proto.AuthResult{OK: false, Message: "账号为 3–16 位字母数字下划线，密码 4–64 位"})
			return
		}
		a, err := st.CreateAccount(ctx, m.Account, m.Password)
		if errors.Is(err, store.ErrExists) {
			s.Send(&proto.AuthResult{OK: false, Message: "账号已存在"})
			return
		} else if err != nil {
			s.log.Error("注册失败", "err", err)
			s.Send(&proto.AuthResult{OK: false, Message: "服务器错误"})
			return
		}
		s.account = a
		s.Send(&proto.AuthResult{OK: true, Message: "注册成功"})
		s.sendServerList()
	case *proto.Login:
		a, err := st.Authenticate(ctx, m.Account, m.Password)
		if err != nil {
			s.Send(&proto.AuthResult{OK: false, Message: "账号或密码错误"})
			return
		}
		s.account = a
		s.Send(&proto.AuthResult{OK: true, Message: "登录成功"})
		s.sendServerList()
	default:
		s.fail("not_logged_in", "请先登录")
	}
}

// sendServerList 对应原客户端的大区/线路选择（selectGroup / selectServer）。
func (s *session) sendServerList() {
	out := &proto.ServerList{Groups: []proto.ServerGroup{}}
	for _, l := range s.srv.Lines {
		if len(out.Groups) == 0 || out.Groups[len(out.Groups)-1].Name != l.Group {
			out.Groups = append(out.Groups, proto.ServerGroup{Name: l.Group, Lines: []proto.LineInfo{}})
		}
		g := &out.Groups[len(out.Groups)-1]
		g.Lines = append(g.Lines, proto.LineInfo{ID: l.ID, Name: l.Name, Online: l.World.Online()})
	}
	s.Send(out)
}

func (s *session) handleLobby(ctx context.Context, m proto.Message) {
	st := s.srv.Store
	if sel, ok := m.(*proto.SelectLine); ok {
		l := s.srv.line(sel.ID)
		if l == nil {
			s.fail("no_line", "线路不存在")
			return
		}
		s.line = l
		s.Send(&proto.LineSelected{ID: l.ID, Name: l.Group + " " + l.Name})
		return
	}
	if s.line == nil {
		s.fail("no_line", "请先选择线路")
		return
	}
	switch m := m.(type) {
	case *proto.CharList:
		s.sendCharList(ctx)
	case *proto.CharCreate:
		s.createChar(ctx, m)
	case *proto.CharDelete:
		if err := st.DeleteCharacter(ctx, s.account.ID, int64(m.ID)); err != nil {
			s.fail("not_found", "角色不存在")
		}
		s.sendCharList(ctx)
	case *proto.CharEnter:
		c, err := st.GetCharacter(ctx, s.account.ID, int64(m.ID))
		if err != nil {
			s.fail("not_found", "角色不存在")
			return
		}
		s.srv.mu.Lock()
		if s.srv.online == nil {
			s.srv.online = map[int64]*session{}
		}
		old := s.srv.online[c.ID]
		s.srv.online[c.ID] = s
		s.srv.mu.Unlock()
		if old != nil && old != s {
			old.Kick("角色在别处登录") // 可能在另一条线路，各线路的世界互不知道
			old.leaveWorld()
		}
		st.AddLogin(ctx, c.ID)
		s.char = &c
		s.line.World.Enter(c, s)
	default:
		s.fail("no_character", "请先选择角色")
	}
}

func (s *session) handleWorld(m proto.Message) {
	w, id := s.line.World, int(s.char.ID)
	switch m := m.(type) {
	case *proto.Move:
		w.Move(id, m.X, m.Y, m.Dir)
	case *proto.Turn:
		w.Turn(id, m.Dir)
	case *proto.MapReq:
		w.MapReq(id, m.Floor, m.X1, m.Y1, m.X2, m.Y2)
	case *proto.Talk:
		w.Talk(id, m.ID)
	case *proto.Chat:
		w.Chat(id, m.Channel, strings.TrimSpace(m.Text), m.Color, m.Area)
	case *proto.GmDetailReq:
		w.GmDetailReq(id, m.Kind, m.ID)
	case *proto.GmListReq:
		w.GmListReq(id, m.Kind)
	case *proto.GmCommand:
		w.GmCommand(id, m.Text)
	case *proto.ReturnHome:
		w.ReturnHome(id)
	case *proto.BattleCommand:
		w.BattleCommand(id, m.Kind, m.Target, m.Item)
	case *proto.BattlePetCommand:
		w.BattlePetCommand(id, m.Skill, m.Target)
	case *proto.PetState:
		w.PetState(id, m.Slot, m.State)
	case *proto.PetRename:
		w.PetRename(id, m.Slot, m.Name)
	case *proto.PetDrop:
		w.PetDrop(id, m.Slot)
	case *proto.PetRide:
		w.PetRide(id, m.Slot)
	case *proto.ItemMove:
		w.ItemMove(id, m.From, m.To)
	case *proto.Trade:
		w.Trade(id, m.Cmd, m.Slot, m.Amount)
	case *proto.Action:
		w.Action(id, m.X, m.Y, m.Action)
	case *proto.CardExchange:
		w.CardExchange(id)
	case *proto.CardDelete:
		w.CardDelete(id, m.Index)
	case *proto.MailSend:
		w.MailSend(id, m.Index, m.Text)
	case *proto.PetMail:
		w.PetMail(id, m.Index, m.Pet, m.Item, m.Text)
	case *proto.Duel:
		w.Duel(id)
	case *proto.Family:
		w.Family(id, m.Text)
	case *proto.SetFlags:
		w.SetFlags(id, m.Flags)
	case *proto.Party:
		w.PartyRequest(id, m.Request)
	case *proto.PartyKick:
		w.PartyKick(id, m.Slot)
	case *proto.BattleHelp:
		w.BattleHelp(id, m.On)
	case *proto.JoinBattle:
		w.JoinBattle(id)
	case *proto.PetSkillUse:
		w.PetSkillUse(id, m.Pet, m.Skill, m.Data)
	case *proto.PetItemEquip:
		w.PetItemEquip(id, m.Pet, m.From, m.To)
	case *proto.ItemUse:
		w.ItemUse(id, m.Slot, m.Target)
	case *proto.MagicUse:
		w.MagicUse(id, m.Slot, m.Target)
	case *proto.ItemDrop:
		w.ItemDrop(id, m.Slot)
	case *proto.GoldDrop:
		w.GoldDrop(id, m.Amount)
	case *proto.PickUp:
		w.PickUp(id, m.Dir)
	case *proto.StatUp:
		w.StatUp(id, m.Stat)
	case *proto.NpcWindowReply:
		w.NpcWindowReply(id, m.ID, m.Seq, m.Button, m.Data)
	case *proto.Logout:
		s.leaveWorld()
		s.sendCharList(context.Background())
	default:
		s.fail("in_world", "游戏中不能这样做")
	}
}

// leaveWorld 让当前角色下线（可重复调用）。
func (s *session) leaveWorld() {
	if s.char == nil {
		return
	}
	c := s.char
	s.char = nil
	s.line.World.Leave(int(c.ID), s)
	s.srv.mu.Lock()
	if s.srv.online[c.ID] == s {
		delete(s.srv.online, c.ID)
	}
	s.srv.mu.Unlock()
}

func summary(c store.Character) proto.CharSummary {
	d := data.DeriveStats(c.Vital, c.Str, c.Tgh, c.Dex)
	return proto.CharSummary{ID: int(c.ID), Name: c.Name, Sprite: c.Sprite, Face: c.Face, Level: c.Level,
		HP: d.HP, Atk: d.Atk, Def: d.Def, Agi: d.Agi, Charm: c.Charm, DP: 0,
		Attrs: []int{c.Earth, c.Water, c.Fire, c.Wind}, Logins: c.Logins, Hometown: c.Hometown}
}

func (s *session) sendCharList(ctx context.Context) {
	list, err := s.srv.Store.ListCharacters(ctx, s.account.ID)
	if err != nil {
		s.fail("server", "服务器错误")
		return
	}
	out := proto.CharListResult{Chars: []proto.CharSummary{}, MaxChars: s.srv.maxChars()}
	for _, c := range list {
		out.Chars = append(out.Chars, summary(c))
	}
	s.Send(&out)
}

func (s *session) createChar(ctx context.Context, m *proto.CharCreate) {
	st := s.srv.Store
	reply := func(msg string) { s.Send(&proto.CharCreateResult{OK: false, Message: msg}) }
	name := strings.TrimSpace(m.Name)
	opt := data.NewCharOption{Sprite: m.Sprite, Face: m.Face, Vital: m.Vital, Str: m.Str, Tgh: m.Tgh, Dex: m.Dex,
		Earth: m.Earth, Water: m.Water, Fire: m.Fire, Wind: m.Wind}
	if n := utf8.RuneCountInString(name); n < 1 || n > charNameLen || strings.ContainsAny(name, "|\\,\n\t ") {
		reply("名字为 1–8 个字，不能含空格和 | \\ ,")
		return
	}
	if err := opt.Validate(); err != nil {
		reply(err.Error())
		return
	}
	if m.Hometown < 0 || m.Hometown >= len(data.Hometowns) {
		reply("出生地不存在")
		return
	}
	if list, err := st.ListCharacters(ctx, s.account.ID); err != nil || len(list) >= s.srv.maxChars() {

		reply("角色位已满")
		return
	}
	h := data.Hometowns[m.Hometown]
	c, err := st.CreateCharacter(ctx, store.Character{AccountID: s.account.ID, Name: name, Sprite: m.Sprite, Level: 1,
		Hometown: m.Hometown, Floor: h.Floor, X: h.X, Y: h.Y, Face: m.Face,
		Vital: m.Vital, Str: m.Str, Tgh: m.Tgh, Dex: m.Dex, Earth: m.Earth, Water: m.Water, Fire: m.Fire, Wind: m.Wind,
		Charm: data.InitialCharm})
	if errors.Is(err, store.ErrExists) {
		reply("名字已被使用")
		return
	} else if err != nil {
		reply("服务器错误")
		return
	}
	s.Send(&proto.CharCreateResult{OK: true, Message: "创建成功", ID: int(c.ID)})
	s.sendCharList(ctx)
}
