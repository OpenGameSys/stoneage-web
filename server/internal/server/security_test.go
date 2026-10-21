package server

import (
	"strings"
	"testing"

	"sa/server/internal/proto"
)

// GM 名单里的名字别人注册不走；密码太短不行；连错 5 次密码会锁住。
func TestReservedGMAndLockout(t *testing.T) {
	url := startServerWith(t, func(s *Server) { s.GMs = map[string]bool{"boss": true} })
	c := dial(t, url)
	c.send(&proto.Register{Account: "Boss", Password: "longenough"})
	if r := c.expect("AuthResult"); r["ok"] != false || !strings.Contains(r["message"].(string), "管理员") {
		t.Fatalf("GM 名应保留：%v", r)
	}
	c.send(&proto.Register{Account: "boss2", Password: "short"})
	if r := c.expect("AuthResult"); r["ok"] != false {
		t.Fatalf("短密码不应注册成功：%v", r)
	}
	c.send(&proto.Register{Account: "victim", Password: "longenough"})
	c.expect("AuthResult")
	c.expect("ServerList")
	g := dial(t, url) // 猜密码的人
	for i := 0; i < authFailLimit; i++ {
		g.send(&proto.Login{Account: "victim", Password: "wrong-guess"})
		g.expect("AuthResult")
	}
	g.send(&proto.Login{Account: "victim", Password: "longenough"})
	if r := g.expect("AuthResult"); r["ok"] != false || !strings.Contains(r["message"].(string), "错误次数太多") {
		t.Fatalf("连错 %d 次后连对的密码也要先锁住：%v", authFailLimit, r)
	}
}

// 同账号的另一条连线正用着的角色不能删。
func TestCannotDeleteOnlineChar(t *testing.T) {
	url := startServer(t)
	a := dial(t, url)
	a.send(&proto.Register{Account: "twice", Password: "longenough"})
	a.expect("AuthResult")
	a.expect("ServerList")
	a.send(&proto.SelectLine{ID: 1})
	a.expect("LineSelected")
	a.send(validCreate("在线的"))
	r := a.expect("CharCreateResult")
	id := int(r["id"].(float64))
	a.send(&proto.CharEnter{ID: id})
	a.expect("EnterFloor")

	b := dial(t, url)
	b.send(&proto.Login{Account: "twice", Password: "longenough"})
	b.expect("AuthResult")
	b.expect("ServerList")
	b.send(&proto.SelectLine{ID: 2})
	b.expect("LineSelected")
	b.send(&proto.CharDelete{ID: id})
	if e := b.expect("Error"); e["code"] != "in_use" {
		t.Fatalf("在线的角色不应能删：%v", e)
	}
	// 名字里不能有 # 或零宽字符
	b.send(validCreate("名#1"))
	if r := b.expect("CharCreateResult"); r["ok"] != false {
		t.Fatalf("带 # 的名字不应建成：%v", r)
	}
}
