package tests

import (
	"strings"
	"testing"

	"sa/server/internal/proto"
	"sa/server/internal/server"
)

// GM 名单里的名字别人注册不走；密码太短不行；连错 5 次密码会锁住。
func TestReservedGMAndLockout(t *testing.T) {
	url := startServerWith(t, func(s *server.Server) { s.GMs = map[string]bool{"boss": true} })
	c := dial(t, url)
	c.send(&proto.Register{Account: "Boss", Password: testPw, Secret: testSecret})
	if r := c.expect("AuthResult"); r["ok"] != false || !strings.Contains(r["message"].(string), "管理员") {
		t.Fatalf("GM 名应保留：%v", r)
	}
	c.send(&proto.Register{Account: "boss2", Password: "short"}) // 不是 32 位十六进制（客户端没算 MD5）
	if r := c.expect("AuthResult"); r["ok"] != false {
		t.Fatalf("不是 MD5 的密码不应注册成功：%v", r)
	}
	c.send(&proto.Register{Account: "victim", Password: testPw, Secret: testSecret})
	c.expect("AuthResult")
	c.expect("ServerList")
	g := dial(t, url) // 猜密码的人
	for i := 0; i < server.AuthFailLimit; i++ {
		g.send(&proto.Login{Account: "victim", Password: "00000000000000000000000000000000"})
		g.expect("AuthResult")
	}
	g.send(&proto.Login{Account: "victim", Password: testPw})
	if r := g.expect("AuthResult"); r["ok"] != false || !strings.Contains(r["message"].(string), "错误次数太多") {
		t.Fatalf("连错 %d 次后连对的密码也要先锁住：%v", server.AuthFailLimit, r)
	}
}

// 同账号的另一条连线正用着的角色不能删。
func TestCannotDeleteOnlineChar(t *testing.T) {
	url := startServer(t)
	a := dial(t, url)
	a.send(&proto.Register{Account: "twice", Password: testPw, Secret: testSecret})
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
	b.send(&proto.Login{Account: "twice", Password: testPw})
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

// 别的账号送 CharEnter 带着不属于自己的角色编号：不能把那个角色的连线踢下线。
func TestCharEnterOtherAccountCannotKick(t *testing.T) {
	url := startServer(t)
	a := dial(t, url)
	a.send(&proto.Register{Account: "owner1", Password: testPw, Secret: testSecret})
	a.expect("AuthResult")
	a.expect("ServerList")
	a.send(&proto.SelectLine{ID: 1})
	a.expect("LineSelected")
	a.send(validCreate("主人"))
	id := int(a.expect("CharCreateResult")["id"].(float64))
	a.send(&proto.CharEnter{ID: id})
	a.expect("EnterFloor")

	b := dial(t, url)
	b.send(&proto.Register{Account: "intruder", Password: testPw, Secret: testSecret})
	b.expect("AuthResult")
	b.expect("ServerList")
	b.send(&proto.SelectLine{ID: 1})
	b.expect("LineSelected")
	b.send(&proto.CharEnter{ID: id})
	if e := b.expect("Error"); e["code"] != "not_found" {
		t.Fatalf("别人的角色应回 not_found：%v", e)
	}
	// 主人还在线：要任务日志还收得到
	a.send(&proto.QuestReq{})
	a.expect("QuestLog")
}
