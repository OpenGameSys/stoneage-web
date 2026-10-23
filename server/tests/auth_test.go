package tests

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"sa/server/internal/proto"
	"sa/server/internal/server"
	"sa/server/internal/store"
)

// 注册时要有安全码；客户端送的密码与安全码都是 32 位十六进制的 MD5，不是的一律拒绝。
const testSecret = "0cc175b9c0f1b6a831c399e269772661" // md5("a")

func register(t *testing.T, c *client, account string) {
	t.Helper()
	c.send(&proto.Register{Account: account, Password: testPw, Secret: testSecret})
	if r := c.expect("AuthResult"); r["ok"] != true {
		t.Fatalf("注册应成功：%v", r)
	}
	c.expect("ServerList")
}

func TestRegisterNeedsSecret(t *testing.T) {
	url := startServer(t)
	c := dial(t, url)
	c.send(&proto.Register{Account: "nosecret", Password: testPw})
	if r := c.expect("AuthResult"); r["ok"] != false || !strings.Contains(r["message"].(string), "安全码") {
		t.Fatalf("没有安全码不应注册成功：%v", r)
	}
	c.send(&proto.Register{Account: "badsecret", Password: testPw, Secret: "1234"})
	if r := c.expect("AuthResult"); r["ok"] != false {
		t.Fatalf("不是 MD5 的安全码不应注册成功：%v", r)
	}
	register(t, c, "withsecret")
}

// 找回密码：安全码对了就改成新密码（旧密码失效、新密码能登录）；回的 AuthResult 不算登录。
func TestResetPassword(t *testing.T) {
	url := startServer(t)
	c := dial(t, url)
	register(t, c, "forgetful")

	const newPw = "5d41402abc4b2a76b9719d911017c592" // md5("hello")
	r := dial(t, url)
	r.send(&proto.ResetPassword{Account: "forgetful", Secret: testSecret, Password: newPw})
	if m := r.expect("AuthResult"); m["ok"] != false || !strings.Contains(m["message"].(string), "已重设") {
		t.Fatalf("应重设成功：%v", m)
	}
	// 重设不等於登录：接着选线路应被拒
	r.send(&proto.SelectLine{ID: 1})
	if e := r.expect("Error"); e["code"] != "not_logged_in" {
		t.Fatalf("重设密码后不应算已登录：%v", e)
	}
	r.send(&proto.Login{Account: "forgetful", Password: testPw})
	if m := r.expect("AuthResult"); m["ok"] != false {
		t.Fatalf("旧密码应失效：%v", m)
	}
	r.send(&proto.Login{Account: "forgetful", Password: newPw})
	if m := r.expect("AuthResult"); m["ok"] != true {
		t.Fatalf("新密码应能登录：%v", m)
	}
}

// 安全码连错 AuthFailLimit 次就锁住，之后连对的安全码也要等。
func TestResetPasswordLockout(t *testing.T) {
	url := startServer(t)
	c := dial(t, url)
	register(t, c, "locked1")
	g := dial(t, url)
	const wrong = "00000000000000000000000000000000"
	for i := 0; i < server.AuthFailLimit; i++ {
		g.send(&proto.ResetPassword{Account: "locked1", Secret: wrong, Password: testPw})
		if m := g.expect("AuthResult"); m["ok"] != false || !strings.Contains(m["message"].(string), "安全码错误") {
			t.Fatalf("错的安全码应被拒：%v", m)
		}
	}
	g.send(&proto.ResetPassword{Account: "locked1", Secret: testSecret, Password: testPw})
	if m := g.expect("AuthResult"); !strings.Contains(m["message"].(string), "次数太多") {
		t.Fatalf("连错 %d 次后连对的安全码也要先锁住：%v", server.AuthFailLimit, m)
	}
	// 被锁的是这个来源 IP 与这个账号：登录本身不受影响
	g.send(&proto.Login{Account: "locked1", Password: testPw})
	if m := g.expect("AuthResult"); m["ok"] != true {
		t.Fatalf("正确的密码仍应能登录：%v", m)
	}
}

// 改版前注册、没有安全码的账号不能找回密码（store 层）。
func TestResetPasswordWithoutSecret(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.CreateAccount(ctx, "old", testPw, ""); err != nil {
		t.Fatal(err)
	}
	if err := db.ResetPassword(ctx, "old", testSecret, testPw); !errors.Is(err, store.ErrNoSecret) {
		t.Fatalf("没有安全码的账号应回 ErrNoSecret：%v", err)
	}
	if _, err := db.CreateAccount(ctx, "new", testPw, testSecret); err != nil {
		t.Fatal(err)
	}
	if err := db.ResetPassword(ctx, "new", "ffffffffffffffffffffffffffffffff", testPw); !errors.Is(err, store.ErrBadLogin) {
		t.Fatalf("安全码不对应回 ErrBadLogin：%v", err)
	}
	if err := db.ResetPassword(ctx, "nobody", testSecret, testPw); !errors.Is(err, store.ErrBadLogin) {
		t.Fatalf("账号不存在应回 ErrBadLogin：%v", err)
	}
	const newPw = "5d41402abc4b2a76b9719d911017c592"
	if err := db.ResetPassword(ctx, "NEW", testSecret, newPw); err != nil { // 账号名不分大小写
		t.Fatal(err)
	}
	if _, err := db.Authenticate(ctx, "new", newPw); err != nil {
		t.Fatalf("重设后应能用新密码验证：%v", err)
	}
}

// 登录后的"个人信息"：要账号资料；改密码 / 改安全码都要先验目前的密码，验错累计到 AuthFailLimit 次就锁住。
func TestAccountInfoAndChange(t *testing.T) {
	url := startServer(t)
	c := dial(t, url)
	register(t, c, "selfcare")
	c.send(&proto.AccountInfoReq{})
	if m := c.expect("AccountInfo"); m["account"] != "selfcare" || m["hasSecret"] != true || num(m, "chars") != 0 {
		t.Fatalf("账号资料不对：%v", m)
	}
	const newPw = "5d41402abc4b2a76b9719d911017c592" // md5("hello")
	c.send(&proto.ChangePassword{Old: "00000000000000000000000000000000", Password: newPw})
	if m := c.expect("AccountResult"); m["ok"] != false {
		t.Fatalf("旧密码不对不应改成功：%v", m)
	}
	c.send(&proto.ChangePassword{Old: testPw, Password: newPw})
	if m := c.expect("AccountResult"); m["ok"] != true {
		t.Fatalf("应改成功：%v", m)
	}
	const newSecret = "81dc9bdb52d04dc20036dbd8313ed055" // md5("1234")
	c.send(&proto.ChangeSecret{Password: newPw, Secret: newSecret})
	if m := c.expect("AccountResult"); m["ok"] != true {
		t.Fatalf("应改安全码成功：%v", m)
	}
	d := dial(t, url)
	d.send(&proto.Login{Account: "selfcare", Password: newPw})
	if m := d.expect("AuthResult"); m["ok"] != true {
		t.Fatalf("新密码应能登录：%v", m)
	}
	d.expect("ServerList")
	r := dial(t, url) // 找回密码是没登录的连线做的
	r.send(&proto.ResetPassword{Account: "selfcare", Secret: newSecret, Password: testPw})
	if m := r.expect("AuthResult"); !strings.Contains(m["message"].(string), "已重设") {
		t.Fatalf("新安全码应能找回密码：%v", m)
	}
	// 连错 AuthFailLimit 次就锁住
	for i := 0; i < server.AuthFailLimit; i++ {
		c.send(&proto.ChangePassword{Old: "00000000000000000000000000000000", Password: newPw})
		c.expect("AccountResult")
	}
	c.send(&proto.ChangePassword{Old: newPw, Password: newPw})
	if m := c.expect("AccountResult"); !strings.Contains(m["message"].(string), "次数太多") {
		t.Fatalf("连错后应锁住：%v", m)
	}
}
