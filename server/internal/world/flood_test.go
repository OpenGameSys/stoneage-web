package world

import (
	"testing"
	"time"

	"sa/server/internal/proto"
)

// 刷屏：一口气说 chatBurst 句之后，再说的要等令牌回来，不然丢掉并提示。
func TestChatFlood(t *testing.T) {
	w, db, _ := setup(t)
	a := newChar(t, db, "talker", 1000, 47, 82)
	b := newChar(t, db, "hearer", 1000, 47, 82)
	cla, clb := &fakeClient{}, &fakeClient{}
	w.Enter(a, cla)
	w.Enter(b, clb)
	base := time.Unix(2_000_000, 0)
	now := base
	w.now = func() time.Time { return now }
	w.Do(func() {})
	cla.take()
	clb.take()
	heard := func() (n int) {
		w.Do(func() {})
		for _, m := range clb.take() {
			if c, ok := m.(*proto.ChatMsg); ok && c.Text == "spam" {
				n++
			}
		}
		return n
	}
	for i := 0; i < 10; i++ {
		w.Chat(int(a.ID), "near", "spam", 0, 3)
	}
	if n := heard(); n != int(chatBurst) {
		t.Fatalf("连说 10 句应只传出 %v 句：%d", chatBurst, n)
	}
	w.Do(func() {})
	cla.take()
	now = now.Add(2 * time.Second) // 回 3 个令牌
	for i := 0; i < 5; i++ {
		w.Chat(int(a.ID), "near", "spam", 0, 3)
	}
	if n := heard(); n != 3 {
		t.Fatalf("2 秒后应再传出 3 句：%d", n)
	}
}

// Flush 等队列里的存档写完才回来。
func TestFlush(t *testing.T) {
	w, _, _ := setup(t)
	done := false
	w.saves <- func() { time.Sleep(50 * time.Millisecond); done = true }
	if !w.Flush(5*time.Second) || !done {
		t.Fatal("Flush 应等前面的存档做完")
	}
}
