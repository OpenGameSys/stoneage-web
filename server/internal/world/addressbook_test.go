package world

import (
	"strings"
	"testing"
	"time"

	"sa/server/internal/proto"
)

// 名片与邮件：对方没开"交换名片"不行；开了就互相放进名片匣并点头；邮件送到；对方删了我的名片就退回；
// 不在线的人的邮件上线时送到。
func TestAddressBook(t *testing.T) {
	w, db, _ := setup(t)
	ca, cla := petOwner(t, w, db, "alice", 10, 60)
	cb, clb := petOwner(t, w, db, "bob", 10, 60)
	a, b := int(ca.ID), int(cb.ID)
	w.Do(func() {
		ea, eb := w.players[a], w.players[b]
		eb.x, eb.y = ea.x+MOVE[ea.dir][0], ea.y+MOVE[ea.dir][1]
	})
	w.CardExchange(a)
	if !strings.Contains(fmtMsgs(cla.take()), "无法交换名片。") {
		t.Fatal("对方没开交换名片应不行")
	}
	w.SetFlags(b, fsParty|fsCard)
	w.CardExchange(a)
	w.Do(func() {
		ea, eb := w.players[a], w.players[b]
		if ea.book.find(cb.ID) < 0 || eb.book.find(ca.ID) < 0 {
			t.Error("应互相有名片")
		}
	})
	if !strings.Contains(fmtMsgs(cla.take()), "和bob交换名片 。") {
		t.Error("应说交换了名片")
	}
	clb.take()
	w.MailSend(a, 0, "你好")
	if m, ok := find[*proto.MailRecv](clb.take()); !ok || m.Text != "你好" || m.Index != 0 {
		t.Errorf("bob 应收到邮件：%+v", m)
	}
	w.CardDelete(b, 0)
	cla.take()
	w.MailSend(a, 0, "还在吗")
	if !strings.Contains(fmtMsgs(cla.take()), "没有名片，所以信件又被退回来了") {
		t.Error("对方没有我的名片应退回")
	}
	// 不在线：存起来，上线时送到（bob 重新拿到名片后再下线）
	w.CardExchange(a)
	w.Leave(b, clb)
	w.MailSend(a, 0, "上线再看")
	w.Do(func() {})
	time.Sleep(200 * time.Millisecond) // 等存档队列写完 bob 的名片匣
	cl2 := &fakeClient{}
	cb2, _ := db.GetCharacter(t.Context(), cb.AccountID, cb.ID)
	w.Enter(cb2, cl2)
	var got *proto.MailRecv
	for i := 0; i < 100 && got == nil; i++ { // 存档队列是另一个协程
		time.Sleep(20 * time.Millisecond)
		got, _ = find[*proto.MailRecv](cl2.take())
	}
	if got == nil || got.Text != "上线再看" {
		t.Errorf("上线时应收到离线邮件：%+v", got)
	}
}
