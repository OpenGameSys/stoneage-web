package server

import (
	"testing"
	"time"
)

// 连错 authFailLimit 次锁 1 分钟，再连错锁 2 分钟（每次加倍，最多 1 小时）；成功就清掉失败次数。
func TestAuthLimiterBackoff(t *testing.T) {
	l := newAuthLimiter()
	now := time.Unix(1_000_000, 0)
	l.now = func() time.Time { return now }
	for i := 0; i < authFailLimit-1; i++ {
		if d := l.fail("acc:x"); d != 0 {
			t.Fatalf("第 %d 次失败不应锁：%v", i+1, d)
		}
	}
	if d := l.fail("acc:x"); d != authLockBase {
		t.Fatalf("第 %d 次失败应锁 %v：%v", authFailLimit, authLockBase, d)
	}
	if l.locked("acc:x") == 0 {
		t.Fatal("应在锁定中")
	}
	now = now.Add(authLockBase + time.Second)
	if l.locked("acc:x") != 0 {
		t.Fatal("时间到应解锁")
	}
	for i := 0; i < authFailLimit-1; i++ {
		l.fail("acc:x")
	}
	if d := l.fail("acc:x"); d != 2*authLockBase {
		t.Fatalf("第二次锁应加倍：%v", d)
	}
	l.ok("acc:y")
	l.fail("acc:y")
	l.ok("acc:y")
	for i := 0; i < authFailLimit-1; i++ {
		l.fail("acc:y")
	}
	if d := l.fail("acc:y"); d != authLockBase {
		t.Fatalf("成功后应从头数：%v", d)
	}
	// 注册计数：一小时内第 n 次
	for i := 1; i <= authRegPerHour+1; i++ {
		if n := l.count("reg:ip"); n != i {
			t.Fatalf("注册计数应为 %d：%d", i, n)
		}
	}
	now = now.Add(2 * time.Hour)
	if n := l.count("reg:ip"); n != 1 {
		t.Fatalf("过了一小时应重新数：%d", n)
	}
}

func TestValidCharName(t *testing.T) {
	ok := []string{"测试勇者", "Bob01", "乌力乌力", "ａｂｃ"}
	bad := []string{"a b", "x#1", "名字​", "tab\t", "pipe|x", "name\x00", "星★", "名字。"}
	for _, n := range ok {
		if !validCharName(n) {
			t.Errorf("%q 应该可以", n)
		}
	}
	for _, n := range bad {
		if validCharName(n) {
			t.Errorf("%q 应该不行", n)
		}
	}
	if remoteIP("1.2.3.4:5678") != "1.2.3.4" || remoteIP("[::1]:80") != "::1" {
		t.Error("remoteIP 去端口不对")
	}
}
