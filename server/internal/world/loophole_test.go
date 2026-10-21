package world

import (
	"strings"
	"testing"

	"sa/server/internal/battle"
	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// NPC_PARTY_CHAECK：组队中不能用传送员（这个 NPC 的 PartyMsg 是"不好意思、可以个别的来吗？"）。队长问时全队都收到 PartyMsg，队员问时只有自己；解散后照常。
func TestWarpManParty(t *testing.T) {
	w, db, _ := setup(t)
	n := findNPC(t, w, "WarpMan", "wpm_800_34_23")
	ca, cla := besideNPC(t, w, db, "pleader", n)
	cb, clb := besideNPC(t, w, db, "pmember", n)
	a, b := int(ca.ID), int(cb.ID)
	w.Do(func() {
		pt := &party{}
		pt.slots[0], pt.slots[1] = w.players[a], w.players[b]
		w.players[a].party, w.players[b].party = pt, pt
	})
	w.Talk(a, n.id)
	for who, cl := range map[string]*fakeClient{"队长": cla, "队员": clb} {
		win, ok := find[*proto.NpcWindow](cl.take())
		if !ok || strings.Contains(win.Text, "要从这里到1楼吗") || win.Buttons != BtnOK || win.Seq != seqWarpManErr {
			t.Errorf("队长问时%s应收到组队不能传送的窗口：%+v", who, win)
		}
	}
	w.Talk(b, n.id)
	if _, ok := find[*proto.NpcWindow](clb.take()); !ok {
		t.Error("队员问时自己应收到窗口")
	}
	if _, ok := find[*proto.NpcWindow](cla.take()); ok {
		t.Error("队员问时队长不应收到")
	}
	// 打开窗口之后才组队：按"是"时再挡一次
	w.Do(func() { w.players[a].party, w.players[b].party = nil, nil })
	w.Talk(a, n.id)
	win, _ := find[*proto.NpcWindow](cla.take())
	w.Do(func() {
		pt := &party{}
		pt.slots[0], pt.slots[1] = w.players[a], w.players[b]
		w.players[a].party, w.players[b].party = pt, pt
	})
	w.NpcWindowReply(a, n.id, win.Seq, BtnYes, "")
	if _, moved := find[*proto.EnterFloor](cla.take()); moved {
		t.Error("按是的时候在组队中不应传送")
	}
}

// CHAR_logout → BATTLE_EscapeDpSend：决斗中下线要扣 DP（2 成），对方还站着的人分到 1 成。
func TestDuelDisconnectDP(t *testing.T) {
	w, db, _ := setup(t)
	ca, _ := petOwner(t, w, db, "stayer", 10, 60)
	cb, _ := petOwner(t, w, db, "quitter", 10, 60)
	a, b := int(ca.ID), int(cb.ID)
	w.Do(func() {
		ea, eb := w.players[a], w.players[b]
		eb.x, eb.y = ea.x+MOVE[ea.dir][0], ea.y+MOVE[ea.dir][1]
		ea.char.DP, eb.char.DP = 1000, 2000
		ea.pets.Battle, eb.pets.Battle = -1, -1
	})
	w.SetFlags(b, fsParty|fsDuel)
	w.Duel(a)
	w.Do(func() {
		ea, eb := w.players[a], w.players[b]
		if ea.fight == nil || eb.fight == nil {
			t.Fatal("应开打")
		}
		ea.fight.room.turn = 2 // 第一回合还没人倒下时逃走不扣（BATTLE_FLG_FREEDP）
		w.leave(eb)
		if eb.char.DP != 1600 {
			t.Errorf("下线的人应扣 400 DP：%d", eb.char.DP)
		}
		if ea.fight == nil || ea.fight.dp != 200 {
			t.Errorf("留下的人应分到 200：%+v", ea.fight)
		}
	})
}

// CHAR_setLuck：同一个 SA 日不重抽，换了日才抽。
func TestLuckOncePerSADay(t *testing.T) {
	w, db, _ := setup(t)
	c := newChar(t, db, "lucky", 1000, 47, 82)
	w.Enter(c, &fakeClient{})
	w.Do(func() {
		e := w.players[int(c.ID)]
		if e.char.Luck < 1 || e.char.Luck > 5 || e.char.LuckTime == 0 {
			t.Fatalf("上线应抽运气并记下时间：%d %d", e.char.Luck, e.char.LuckTime)
		}
		now := w.now().Unix()
		e.char.Luck, e.char.LuckTime = 3, now
		w.setLuck(e)
		if e.char.Luck != 3 || e.char.LuckTime != now {
			t.Errorf("同一个 SA 日不应重抽：%d %d", e.char.Luck, e.char.LuckTime)
		}
		e.char.LuckTime = now - 2*saDay
		w.setLuck(e)
		if e.char.LuckTime != now {
			t.Errorf("换了 SA 日应重抽：%d", e.char.LuckTime)
		}
	})
}

// 登出消失的道具（ITEM_DROPATLOGOUT）：带着不能回记录点，下线就消失。
func TestDropAtLogout(t *testing.T) {
	w, db, g := setup(t)
	id := -1
	for i, d := range g.Items {
		if d.DropAtLogout && d.Type != data.ItemWares {
			id = i
			break
		}
	}
	if id < 0 {
		t.Skip("没有登出消失的道具")
	}
	c := newChar(t, db, "carrier", 1000, 47, 82)
	cl := &fakeClient{}
	w.Enter(c, cl)
	giveItem(w, int(c.ID), id)
	cl.take()
	w.ReturnHome(int(c.ID))
	if !strings.Contains(fmtMsgs(cl.take()), "携带的物品使你无法回纪录点！") {
		t.Error("带着登出消失的道具不能回记录点")
	}
	w.Do(func() {
		e := w.players[int(c.ID)]
		w.leave(e)
		if strings.Contains(e.char.Items, `"id":`+itoa(id)+`,`) || strings.Contains(e.char.Items, `"id":`+itoa(id)+`}`) {
			t.Errorf("下线后道具应消失：%s", e.char.Items)
		}
	})
}

// _CHAR_PROFESSION：要职业的装备（没有职业系统）装不上；上线时已经装着的拿下来，也没有效果。
func TestProfessionEquip(t *testing.T) {
	w, db, g := setup(t)
	id := -1
	for i, d := range g.Items {
		if d.NeedProfession > 0 && data.EquipSlot(d.Type) >= 0 {
			id = i
			break
		}
	}
	if id < 0 {
		t.Skip("没有要职业的装备")
	}
	c := newChar(t, db, "noclass", 1000, 47, 82)
	w.Enter(c, &fakeClient{})
	w.Do(func() {
		e := w.players[int(c.ID)]
		e.char.Level = 140
		it := w.newItem(g.Items[id])
		slot := data.EquipSlot(g.Items[id].Type)
		if err := w.canEquip(e, it, slot); err == nil {
			t.Error("要职业的装备应装不上")
		}
		e.inv[slot] = it
		w.unequipProfession(e)
		if e.inv[slot] != nil {
			t.Error("上线时应把要职业的装备拿下来")
		}
	})
}

// _NOT_ESCAPE：NOTESCAPE 的楼层按逃跑改成防御。
func TestNoEscapeFloor(t *testing.T) {
	w, db, g := setup(t)
	ene := petEnemy(t, g, 1)
	c, cl := petOwner(t, w, db, "stuck", 10, 60)
	id := int(c.ID)
	w.Do(func() {
		e := w.players[id]
		e.floor = 142
		e.pets.Battle = -1
		w.startBattleWith(e, []*battle.Fighter{enemyFighter(w, ene, 1, battle.EnemySlot(0))}, nil, nil)
	})
	cl.take()
	var kind battle.Kind
	w.BattleCommand(id, "escape", -1, -1)
	w.Do(func() {
		if f := w.players[id].fight; f != nil && f.cmd != nil {
			kind = f.cmd.Kind
		}
	})
	msgs := fmtMsgs(cl.take())
	if !strings.Contains(msgs, "定身法") {
		t.Errorf("应说逃不掉：%s", msgs)
	}
	if kind != battle.Guard && kind != "" {
		t.Errorf("逃跑应改成防御：%q", kind)
	}
}

// _THE_WORLD_SEND：世界频道要 5 转 140 级、每句扣声望、两句之间隔 5 秒以上。
func TestWorldChannelLimits(t *testing.T) {
	w, db, _ := setup(t)
	ca := newChar(t, db, "shouter", 1000, 47, 82)
	cb := newChar(t, db, "listener", 1000, 47, 82)
	cla, clb := &fakeClient{}, &fakeClient{}
	w.Enter(ca, cla)
	w.Enter(cb, clb)
	a := int(ca.ID)
	w.Do(func() {})
	cla.take()
	clb.take()
	heard := func() bool {
		w.Do(func() {})
		for _, m := range clb.take() {
			if c, ok := m.(*proto.ChatMsg); ok && c.Channel == "world" && c.Text == "hi" {
				return true
			}
		}
		return false
	}
	w.Chat(a, "world", "hi", 0, 3)
	if heard() || !strings.Contains(fmtMsgs(cla.take()), "转生等级未达到要求") {
		t.Error("等级不够不能用世界频道")
	}
	w.Do(func() { e := w.players[a]; e.char.Trans, e.char.Level, e.char.Fame = 5, 140, 100 })
	w.Chat(a, "world", "hi", 0, 3)
	if !heard() {
		t.Fatal("够条件应说得出去")
	}
	w.Do(func() {
		if f := w.players[a].char.Fame; f != 99 {
			t.Errorf("每句扣 1 声望（内部值）：%d", f)
		}
	})
	cla.take()
	w.Chat(a, "world", "hi", 0, 3)
	if heard() || !strings.Contains(fmtMsgs(cla.take()), "间隔五秒") {
		t.Error("5 秒内第二句不应说出去")
	}
}
