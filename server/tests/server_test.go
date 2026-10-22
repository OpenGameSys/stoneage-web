package tests

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"sa/server/internal/data"
	"sa/server/internal/proto"
	"sa/server/internal/server"
	"sa/server/internal/store"
	"sa/server/internal/world"
)

// 客户端送的是密码的 MD5（32 位十六进制）
const testPw = "5f4dcc3b5aa765d61d8327deb882cf99"

// 端到端：真实的 WebSocket 连接 → 会话 → 世界，数据用仓库里的原版数据。

type client struct {
	t    *testing.T
	conn *websocket.Conn
	in   chan map[string]any
}

func startServer(t *testing.T) string { return startServerWith(t, nil) }

// startServerWith 起一台测试服务端，configure 可以先改 Server 的设定（GM 名单等）。
func startServerWith(t *testing.T, configure func(*server.Server)) string {
	t.Helper()
	const dir = "../../gamedata"
	if _, err := os.Stat(dir); err != nil {
		t.Skip("没有原版数据")
	}
	game, err := data.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "e2e.db"))
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	var lines []*server.Line
	for i, name := range []string{"一线", "二线"} {
		w := world.New(game, db, log, int64(i+1))
		// 世界频道的门槛（5 转 140 级、扣声望）在 world 的测试里测，这里只测转送
		w.Rules.WorldTrans, w.Rules.WorldLevel, w.Rules.WorldFame = 0, 0, 0
		go w.Run(ctx)
		lines = append(lines, &server.Line{ID: i + 1, Group: "测试区", Name: name, World: w})
	}
	sv := &server.Server{Lines: lines, Store: db, Log: log, Origins: []string{"*"}}
	if configure != nil {
		configure(sv)
	}
	srv := httptest.NewServer(sv)
	t.Cleanup(func() { srv.Close(); cancel(); db.Close() })
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func dial(t *testing.T, url string) *client {
	t.Helper()
	conn, _, err := websocket.Dial(context.Background(), url, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, conn: conn, in: make(chan map[string]any, 1024)}
	go func() {
		for {
			_, b, err := conn.Read(context.Background())
			if err != nil {
				close(c.in)
				return
			}
			var m map[string]any
			json.Unmarshal(b, &m)
			c.in <- m
		}
	}()
	c.expect("Hello")
	return c
}

func (c *client) send(m proto.Message) {
	b, _ := proto.Encode(m)
	if err := c.conn.Write(context.Background(), websocket.MessageText, b); err != nil {
		c.t.Fatal(err)
	}
}

// expect 等待指定类型的消息，跳过其他消息。
func (c *client) expect(t string, match ...func(map[string]any) bool) map[string]any {
	c.t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case m, ok := <-c.in:
			if !ok {
				c.t.Fatalf("等待 %s 时连接已关闭", t)
			}
			if m["t"] == t && (len(match) == 0 || match[0](m)) {
				return m
			}
		case <-timeout:
			c.t.Fatalf("等待 %s 超时", t)
		}
	}
}

func (c *client) closed() bool {
	timeout := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-c.in:
			if !ok {
				return true
			}
		case <-timeout:
			return false
		}
	}
}

// validCreate 是一组符合原版规则的建角参数：形象 100000、颜色 0、眼 1 嘴 2，体力 10 腕力 10，地 10。
func validCreate(name string) *proto.CharCreate {
	return &proto.CharCreate{Name: name, Sprite: 100000, Face: data.FaceNumber(0, 0, 1, 2),
		Vital: 10, Str: 10, Earth: 10, Hometown: 0}
}

// login 注册账号、选线路、创建角色、进入世界，返回角色 ID。
func (c *client) login(account, name string, line int) int {
	c.send(&proto.Register{Account: account, Password: testPw, Secret: testSecret})
	if r := c.expect("AuthResult"); r["ok"] != true {
		c.t.Fatalf("注册失败：%v", r)
	}
	c.expect("ServerList")
	c.send(&proto.SelectLine{ID: line})
	c.expect("LineSelected")
	c.send(validCreate(name))
	if r := c.expect("CharCreateResult"); r["ok"] != true {
		c.t.Fatalf("建角失败：%v", r)
	}
	list := c.expect("CharListResult")
	chars := list["chars"].([]any)
	id := int(chars[0].(map[string]any)["id"].(float64))
	c.send(&proto.CharEnter{ID: id})
	c.expect("EnterFloor")
	return id
}

func num(m map[string]any, k string) int { return int(m[k].(float64)) }

func TestTwoPlayers(t *testing.T) {
	url := startServer(t)
	a, b := dial(t, url), dial(t, url)
	idA := a.login("alice", "爱丽丝", 1)
	idB := b.login("bob01", "鲍勃", 1)

	// 互相可见
	a.expect("ObjAppear", func(m map[string]any) bool { return strings.Contains(toJSON(m), "鲍勃") })
	sawA := b.expect("ObjAppear", func(m map[string]any) bool { return strings.Contains(toJSON(m), "爱丽丝") })
	_ = sawA

	// a 请求地图并找一步可走的路
	h := data.Hometowns[0]
	a.send(&proto.MapReq{Floor: h.Floor, X1: h.X - 1, Y1: h.Y - 1, X2: h.X + 2, Y2: h.Y + 2})
	md := a.expect("MapData")
	walk := md["walk"].(string)
	dir := -1
	for d := 0; d < 8; d++ {
		x, y := 1+world.MOVE[d][0], 1+world.MOVE[d][1]
		if walk[y*3+x] == '1' {
			dir = d
			break
		}
	}
	if dir < 0 {
		t.Fatal("出生点周围没有可走格")
	}
	a.send(&proto.Move{X: h.X, Y: h.Y, Dir: dir})
	mv := b.expect("ObjMove")
	if num(mv, "id") != idA || num(mv, "x") != h.X+world.MOVE[dir][0] {
		t.Fatalf("b 收到的移动不对：%v", mv)
	}

	// 附近聊天与世界聊天
	a.send(&proto.Chat{Channel: "near", Text: "你好，鲍勃"})
	if cm := b.expect("ChatMsg"); cm["text"] != "你好，鲍勃" || cm["channel"] != "near" {
		t.Fatalf("附近聊天：%v", cm)
	}
	b.send(&proto.Chat{Channel: "world", Text: "大家好"})
	// a 会先收到自己那句附近聊天的回显，按频道过滤
	if cm := a.expect("ChatMsg", func(m map[string]any) bool { return m["channel"] == "world" }); cm["text"] != "大家好" || cm["channel"] != "world" || num(cm, "fromId") != idB {
		t.Fatalf("世界聊天：%v", cm)
	}

	// 同一角色从另一条线路登录：旧连接被踢（跨线路），新连接接管；一线的 a 看到鲍勃离开
	b2 := dial(t, url)
	b2.send(&proto.Login{Account: "bob01", Password: testPw})
	b2.expect("AuthResult")
	b2.send(&proto.SelectLine{ID: 2})
	b2.expect("LineSelected")
	b2.send(&proto.CharEnter{ID: idB})
	b2.expect("EnterFloor")
	if !b.closed() {
		t.Fatal("旧连接应被断开")
	}

	// 鲍勃已在二线：一线的 a 收到消失，且听不到二线的附近聊天
	gone := a.expect("ObjDisappear", func(m map[string]any) bool { return strings.Contains(toJSON(m), itoa(idB)) })
	_ = gone
	b2.send(&proto.Chat{Channel: "near", Text: "二线的人听得到"})
	b2.expect("ChatMsg") // 自己的回显
	a.send(&proto.Chat{Channel: "near", Text: "一线"})
	if cm := a.expect("ChatMsg"); cm["text"] != "一线" {
		t.Fatalf("a 不应收到二线的附近聊天：%v", cm)
	}
}

func TestCreateRules(t *testing.T) {
	url := startServer(t)
	c := dial(t, url)
	c.send(&proto.Register{Account: "maker", Password: testPw, Secret: testSecret})
	c.expect("AuthResult")
	c.expect("ServerList")
	c.send(validCreate("没选线路"))
	if e := c.expect("Error"); e["code"] != "no_line" {
		t.Fatalf("未选线路不能建角：%v", e)
	}
	c.send(&proto.SelectLine{ID: 1})
	c.expect("LineSelected")

	bad := []struct {
		why string
		m   func(*proto.CharCreate)
	}{
		{"头像与形象不匹配", func(m *proto.CharCreate) { m.Face = data.FaceNumber(1, 0, 0, 0) }},
		{"基本状态超过 20", func(m *proto.CharCreate) { m.Tgh = 5 }},
		{"属性不足 10", func(m *proto.CharCreate) { m.Earth = 9 }},
		{"三种属性", func(m *proto.CharCreate) { m.Earth, m.Water, m.Wind = 4, 3, 3 }},
		{"地火相克", func(m *proto.CharCreate) { m.Earth, m.Fire = 5, 5 }},
		{"不可选形象", func(m *proto.CharCreate) { m.Sprite = 100001 }},
	}
	for _, b := range bad {
		m := validCreate("测试")
		b.m(m)
		c.send(m)
		if r := c.expect("CharCreateResult"); r["ok"] != false {
			t.Errorf("%s 应被拒绝", b.why)
		}
	}
	c.send(validCreate("合法角色"))
	if r := c.expect("CharCreateResult"); r["ok"] != true {
		t.Fatalf("合法参数应成功：%v", r)
	}
	list := c.expect("CharListResult")
	ch := list["chars"].([]any)[0].(map[string]any)
	// 体力 10 腕力 10：HP = 4×10+10 = 50，攻 = 10+1 = 11，防 = 0+1+1 = 2，魅力 60
	if num(ch, "hp") != 50 || num(ch, "atk") != 11 || num(ch, "def") != 2 || num(ch, "charm") != 60 || num(ch, "face") != data.FaceNumber(0, 0, 1, 2) {
		t.Fatalf("角色摘要数值不对：%v", ch)
	}
}

func TestRejectsBeforeLogin(t *testing.T) {
	url := startServer(t)
	c := dial(t, url)
	c.send(&proto.CharEnter{ID: 1})
	if e := c.expect("Error"); e["code"] != "not_logged_in" {
		t.Fatalf("未登录应被拒绝：%v", e)
	}
	c.send(&proto.Login{Account: "nobody", Password: "0123456789abcdef0123456789abcdef"})
	if r := c.expect("AuthResult"); r["ok"] != false {
		t.Fatal("不存在的账号不应登录成功")
	}
	c.conn.Write(context.Background(), websocket.MessageText, []byte(`{"t":"Hack"}`))
	if e := c.expect("Error"); e["code"] != "bad_message" {
		t.Fatalf("未知消息应报错：%v", e)
	}
}

func toJSON(m map[string]any) string { b, _ := json.Marshal(m); return string(b) }
func itoa(i int) string              { b, _ := json.Marshal(i); return string(b) }
