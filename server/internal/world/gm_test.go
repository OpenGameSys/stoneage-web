package world

import (
	"fmt"
	"strings"
	"testing"

	"sa/server/internal/proto"
	"sa/server/internal/store"
)

// gmClient 是带账号名的连线（server.session 也有 Account()）。
type gmClient struct {
	fakeClient
	account string
}

func (c *gmClient) Account() string { return c.account }

func TestGMOnlyForConfiguredAccounts(t *testing.T) {
	w, db, _ := setup(t)
	w.GMs = map[string]bool{"boss": true}
	enter := func(name string) (store.Character, *gmClient) {
		c := newChar(t, db, name, 1000, 47, 82)
		cl := &gmClient{account: name}
		w.Enter(c, cl)
		return c, cl
	}
	gm, gcl := enter("Boss") // 账号名不分大小写
	pl, pcl := enter("player")
	w.Do(func() {})
	if m, ok := find[*proto.GmMenu](gcl.take()); !ok || len(m.Items) == 0 {
		t.Fatal("GM 上线应收到 GM 指令选单")
	}
	if _, ok := find[*proto.GmMenu](pcl.take()); ok {
		t.Fatal("一般玩家不应收到 GM 指令选单")
	}
	// 一般玩家送 GM 指令、在聊天里打指令都不执行
	w.GmCommand(int(pl.ID), "/gold 12345")
	w.Chat(int(pl.ID), "near", "/gold 12345", 0, 3)
	w.Do(func() {
		if g := w.players[int(pl.ID)].char.Gold; g == 12345 {
			t.Error("一般玩家的 GM 指令不应执行")
		}
	})
	// GM 从选单送、在聊天里打都可以
	w.GmCommand(int(gm.ID), "/gold 777")
	w.Do(func() {
		if g := w.players[int(gm.ID)].char.Gold; g != 777 {
			t.Errorf("GM 的 /gold 没有执行：%d", g)
		}
	})
	w.Chat(int(gm.ID), "near", "/lv 33", 0, 3)
	w.Do(func() {
		if lv := w.players[int(gm.ID)].char.Level; lv != 33 {
			t.Errorf("GM 在聊天里打的 /lv 没有执行：%d", lv)
		}
	})
	// /call 把玩家叫到身边
	w.GmCommand(int(gm.ID), "/warp 1000 50 82")
	w.GmCommand(int(gm.ID), "/call player")
	w.Do(func() {
		g, p := w.players[int(gm.ID)], w.players[int(pl.ID)]
		if p.floor != g.floor || p.x != g.x || p.y != g.y {
			t.Errorf("/call 后应在 GM 身边：GM (%d,%d) 玩家 (%d,%d)", g.x, g.y, p.x, p.y)
		}
	})
}

func TestPetExt(t *testing.T) {
	w, db, g := setup(t)
	ene := petEnemy(t, g, 1)
	c, _ := petOwner(t, w, db, "ext", 30, 100)
	id := int(c.ID)
	slot := givePet(t, w, id, ene, 1)
	w.Do(func() {
		e := w.players[id]
		p := e.pets.Pets[slot]
		x := w.petExt(p)
		if len(x.Grow) != 4 || len(x.Std) != 4 || len(x.Init) != 4 || len(x.Real) != 0 {
			t.Fatalf("扩展资料不完整：%+v", x)
		}
		// 1 级抓的：1 级的推算值就是现在的数值（没有道具）
		d, _ := w.petDerive(p)
		if x.Init[0] != d.Atk || x.Init[1] != d.Def || x.Init[2] != d.Quick || x.Init[3] != d.HP {
			t.Errorf("1 级的推算值 %v 应等于现在的 攻%d 防%d 敏%d HP%d", x.Init, d.Atk, d.Def, d.Quick, d.HP)
		}
		w.petGainExp(p, w.game.Battle.NextLevelExp(10))
		x = w.petExt(p)
		if len(x.Real) != 4 || x.Real[0] <= 0 || x.Real[3] <= 0 {
			t.Fatalf("升级后应有实际成长：%+v", x.Real)
		}
		// 实际成长应在预计值附近（随机 10 点与档次倍率的浮动）
		for i := range x.Real {
			if lo, hi := x.Grow[i]/2, x.Grow[i]*2; x.Real[i] < lo || x.Real[i] > hi {
				t.Errorf("第 %d 项实际成长 %d 离预计 %d 太远", i, x.Real[i], x.Grow[i])
			}
		}
		if info := w.petInfo(e, slot); info.Ext.GetLevel != 1 {
			t.Errorf("宠物资料应带扩展资料：%+v", info.Ext)
		}
	})
}

// 新的 GM 指令都要回一句话；清单只给 GM。
func TestGMCommandsReplyAndLists(t *testing.T) {
	w, db, g := setup(t)
	w.GMs = map[string]bool{"gmx": true}
	gm := newChar(t, db, "gmx", 1000, 47, 82)
	cl := &gmClient{account: "gmx"}
	w.Enter(gm, cl)
	pl := newChar(t, db, "plx", 1000, 47, 82)
	pcl := &gmClient{account: "plx"}
	w.Enter(pl, pcl)
	id := int(gm.ID)
	w.Do(func() {})
	cl.take()
	run := func(text string) string {
		w.GmCommand(id, text)
		w.Do(func() {})
		m, ok := find[*proto.ChatMsg](cl.take())
		if !ok || m.From != "GM" {
			t.Fatalf("%s 没有回应", text)
		}
		return m.Text
	}
	run("/gold 5000")
	run("/points 12")
	run("/stat 30 20 10 5")
	if r := run("/attr 6 6 0 0"); r != "四项属性加起来不能超过 10" {
		t.Errorf("属性超过 10 应该拒绝：%s", r)
	}
	run("/attr 5 5 0 0")
	if r := run("/stat 1 2"); !strings.HasPrefix(r, "用法：") {
		t.Errorf("参数不够应回用法：%s", r)
	}
	run("/item 1 3")
	ene := petEnemy(t, g, 1)
	run(fmt.Sprintf("/pet %d 1", ene.ID))
	run("/petlv 0 10")
	w.Do(func() {
		c := w.players[id].char
		if c.Gold != 5000 || c.Points != 12 || c.Vital != 30 || c.Str != 20 || c.Tgh != 10 || c.Dex != 5 || c.Earth != 5 || c.Water != 5 {
			t.Errorf("角色资料没改对：%+v", c)
		}
		if p := w.players[id].pets.Pets[0]; p == nil || p.Level != 10 {
			t.Errorf("宠物应升到 10 级：%+v", p)
		}
	})
	w.GmListReq(id, "item")
	w.GmListReq(id, "player")
	w.Do(func() {})
	n := 0
	for _, m := range cl.take() {
		if l, ok := m.(*proto.GmList); ok {
			n++
			if len(l.Entries) == 0 {
				t.Errorf("%s 清单是空的", l.Kind)
			}
		}
	}
	if n != 2 {
		t.Errorf("GM 应收到 2 份清单，收到 %d", n)
	}
	w.GmListReq(int(pl.ID), "item")
	w.Do(func() {})
	if _, ok := find[*proto.GmList](pcl.take()); ok {
		t.Error("一般玩家不应收到清单")
	}
}

func TestGMMoreCommands(t *testing.T) {
	w, db, g := setup(t)
	w.GMs = map[string]bool{"gmy": true}
	gm := newChar(t, db, "gmy", 1000, 47, 82)
	cl := &gmClient{account: "gmy"}
	w.Enter(gm, cl)
	pl := newChar(t, db, "ply", 1000, 47, 82)
	pcl := &gmClient{account: "ply"}
	w.Enter(pl, pcl)
	id, pid := int(gm.ID), int(pl.ID)
	w.Do(func() {})
	cl.take()
	pcl.take()
	run := func(text string) string {
		w.GmCommand(id, text)
		w.Do(func() {})
		m, ok := find[*proto.ChatMsg](cl.take())
		if !ok {
			t.Fatalf("%s 没有回应", text)
		}
		return m.Text
	}
	if r := run("/where"); !strings.Contains(r, "楼层 1000") {
		t.Errorf("/where：%s", r)
	}
	run("/give ply 1 2")
	if m, ok := find[*proto.ChatMsg](pcl.take()); !ok || !strings.HasPrefix(m.Text, "GM 给了你") {
		t.Error("收到道具的人应该收到通知")
	}
	if r := run("/look ply"); !strings.HasPrefix(r, "ply：Lv1") {
		t.Errorf("/look：%s", r)
	}
	run("/mute ply 5")
	pcl.take()
	w.Chat(pid, "near", "hello", 0, 3)
	w.Do(func() {})
	for _, m := range cl.take() {
		if c, ok := m.(*proto.ChatMsg); ok && c.Text == "hello" {
			t.Error("禁言中的话不应传出去")
		}
	}
	run("/mute ply 0")
	ene := petEnemy(t, g, 1)
	run(fmt.Sprintf("/pet %d 1", ene.ID))
	var skill int
	for sid := range w.game.Battle.Skills {
		skill = sid
		break
	}
	run(fmt.Sprintf("/petskill 0 7 %d", skill))
	w.Do(func() {
		p := w.players[id].pets.Pets[0]
		if p.Skills[6] != skill || p.Slot < 7 {
			t.Errorf("第 7 栏应学会技能 %d：%+v / 栏数 %d", skill, p.Skills, p.Slot)
		}
	})
	run("/petdel 0")
	run("/item 1 3")
	if r := run("/clearbag 确定"); !strings.Contains(r, "丢掉 3 件") {
		t.Errorf("/clearbag：%s", r)
	}
	w.Do(func() {
		if w.players[id].pets.Pets[0] != nil {
			t.Error("/petdel 后宠物应该没了")
		}
	})
}

func TestGMDetailAndMore(t *testing.T) {
	w, db, g := setup(t)
	w.GMs = map[string]bool{"gmz": true}
	gm := newChar(t, db, "gmz", 1000, 47, 82)
	cl := &gmClient{account: "gmz"}
	w.Enter(gm, cl)
	id := int(gm.ID)
	w.Do(func() {})
	cl.take()
	run := func(text string) string {
		w.GmCommand(id, text)
		w.Do(func() {})
		m, ok := find[*proto.ChatMsg](cl.take())
		if !ok {
			t.Fatalf("%s 没有回应", text)
		}
		return m.Text
	}
	ene := petEnemy(t, g, 1)
	w.GmDetailReq(id, "enemy", ene.ID)
	w.GmDetailReq(id, "item", 1)
	w.Do(func() {})
	n := 0
	for _, m := range cl.take() {
		if d, ok := m.(*proto.GmDetail); ok {
			n++
			if len(d.Lines) < 2 || d.Image == 0 {
				t.Errorf("%s 详情不完整：%+v", d.Kind, d)
			}
		}
	}
	if n != 2 {
		t.Fatalf("应收到 2 份详情，收到 %d", n)
	}
	run("/stat 20 20 20 20")
	run("/hp 1")
	run("/town 加加")
	run(fmt.Sprintf("/pet %d 1", ene.ID))
	run("/petrank 0 5")
	run("/petname 0 小乌")
	run("/bank 5000")
	run("/repair")
	w.Do(func() {
		e := w.players[id]
		if e.char.HP != 1 || e.floor != 3006 || e.fmc.Bank != 5000 {
			t.Errorf("hp %d 楼层 %d 银行 %d", e.char.HP, e.floor, e.fmc.Bank)
		}
		if p := e.pets.Pets[0]; p.Rank != 5 || p.FreeName != "小乌" {
			t.Errorf("宠物档次 / 名字没改：%d %q", p.Rank, p.FreeName)
		}
	})
}
