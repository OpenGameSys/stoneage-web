package world

import (
	"strings"
	"testing"

	"sa/server/internal/proto"
)

// 宝石提炼师（eden3/process/maker1.arg）：选钻石 → 需要三个矿石与 150 石币 → 条件（ENDEV=4&ENDEV=81&…）成立才兑换。
func TestItemChange(t *testing.T) {
	w, db, g := setup(t)
	n := findNPC(t, w, "ItemchangeMan", "maker1.arg")
	if g.Items[20236] == nil || g.Items[20287] == nil {
		t.Skip("没有矿石或宝石")
	}
	c, cl := besideNPC(t, w, db, "jeweler", n)
	id := int(c.ID)
	open := func() *proto.NpcWindow {
		w.Talk(id, n.id)
		win, ok := find[*proto.NpcWindow](cl.take())
		if !ok || win.Buttons != BtnYesNo || !strings.Contains(win.Text, "提炼宝石") {
			t.Fatalf("应显示 START_MSG：%+v", win)
		}
		w.NpcWindowReply(id, n.id, win.Seq, BtnYes, "")
		menu, ok := find[*proto.NpcWindow](cl.take())
		if !ok || menu.Kind != "select" || !strings.Contains(menu.Text, g.Items[20287].Name) {
			t.Fatalf("应显示兑换选单：%+v", menu)
		}
		w.NpcWindowReply(id, n.id, menu.Seq, 0, "1")
		need, ok := find[*proto.NpcWindow](cl.take())
		if !ok || need.Buttons != BtnYesNo || !strings.Contains(need.Text, g.Items[20236].Name) || !strings.Contains(need.Text, "石币:150") {
			t.Fatalf("应列出需要的东西：%+v", need)
		}
		return need
	}
	need := open()
	w.NpcWindowReply(id, n.id, need.Seq, BtnYes, "")
	if nt, _ := find[*proto.NpcTalk](cl.take()); nt == nil || nt.Text != "条件不足！" {
		t.Fatalf("没做过事件应条件不足：%+v", nt)
	}
	for range 3 {
		giveItem(w, id, 20236)
	}
	w.Do(func() { e := w.players[id]; e.char.Gold = 200; e.setEventDone(4); e.setEventDone(81) })
	need = open()
	w.NpcWindowReply(id, n.id, need.Seq, BtnYes, "")
	cl.take()
	var ore, gem, gold int
	w.Do(func() { e := w.players[id]; ore, gem, gold = w.countItem(e, 20236), w.countItem(e, 20287), e.char.Gold })
	if ore != 0 || gem != 1 || gold != 50 {
		t.Fatalf("应收走 3 个矿石与 150 石币、拿到钻石：矿石 %d 钻石 %d 金钱 %d", ore, gem, gold)
	}
}

// 麦克风（mic_3007_50_49：3007 楼 0,0–80,80）：对着它说的话用黄字传给范围内的人。
func TestMic(t *testing.T) {
	w, db, _ := setup(t)
	n := findNPC(t, w, "Mic", "mic_3007_50_49")
	c, cl := besideNPC(t, w, db, "speaker", n)
	c2, cl2 := besideNPC(t, w, db, "listener", n)
	w.Do(func() { e := w.players[int(c2.ID)]; w.placeOnFloor(e, n.floor, 5, 5, 0) })
	cl2.take()
	w.Do(func() { e := w.players[int(c.ID)]; e.dir = 1 }) // 站在右边，面向左（MOVE[1] = (−1, 0)，朝着麦克风）
	cl.take()
	w.Chat(int(c.ID), "near", "大家好", 0, 3)
	var got bool
	for _, m := range cl2.take() {
		if cm, ok := m.(*proto.ChatMsg); ok && cm.Text == "大家好" && cm.Color == colorYellow && cm.From == "speaker" {
			got = true
		}
	}
	if !got {
		t.Fatal("范围内的人应收到麦克风广播")
	}
}

// 看动作的人：面对着说话回 normal。
func TestActionNpc(t *testing.T) {
	w, db, _ := setup(t)
	n := findFn(t, w, "Action")
	c, cl := besideNPC(t, w, db, "watcher", n)
	w.Talk(int(c.ID), n.id)
	if nt, ok := find[*proto.NpcTalk](cl.take()); !ok || !strings.Contains(n.npc.Arg, nt.Text) {
		t.Fatalf("应说 normal：%+v", nt)
	}
}
