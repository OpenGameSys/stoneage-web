package world

import (
	"strings"
	"testing"

	"sa/server/internal/battle"
	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 摆摊：店家设定两项（一件道具、一只宠物）开张 → 买家面向他按摆摊钮看到清单 → 买下两项 → 钱、道具、宠物都换手，清单清空 → 收摊。
func TestStreetVendor(t *testing.T) {
	w, db, g := setup(t)
	s, cls := petOwner(t, w, db, "店家", 10, 60)
	b, clb := petOwner(t, w, db, "买家", 10, 60)
	sid, bid := int(s.ID), int(b.ID)
	itemID := 0
	for id, def := range g.Items {
		if !def.VanishDrop && def.MaxPile <= 1 && def.Type == data.ItemOther && def.Name != "" {
			itemID = id
			break
		}
	}
	if itemID == 0 {
		t.Fatal("找不到能卖的道具")
	}
	petSlot := givePet(t, w, sid, petEnemy(t, g, 1), 5)
	var slot int
	w.Do(func() {
		se, be := w.players[sid], w.players[bid]
		slot = w.addItem(se.inv, &Item{ID: itemID})
		se.char.Gold, be.char.Gold = 0, 1000
		w.placeOnFloor(be, se.floor, se.x+1, se.y, 1) // 买家站在店家 +x 那一格、面向店家（MOVE[1] = (-1, 0)）
	})
	cls.take()
	clb.take()
	w.Vendor(sid, "open", "", nil, nil, nil)
	if _, ok := find[*proto.VendorSetup](cls.take()); !ok {
		t.Fatal("面前没人时应打开设定窗口")
	}
	w.Vendor(sid, "set", "测试摊", []int{0, 1}, []int{slot, petSlot}, []int{100, 200})
	var stall *vendorStall
	var markV string
	w.Do(func() { se := w.players[sid]; stall = se.stall; markV = se.mark().Vendor })
	if stall == nil || len(stall.entries) != 2 || stall.title != "[石]测试摊" {
		t.Fatalf("应开张两项：%+v（%s）", stall, fmtMsgs(cls.take()))
	}
	if markV != "[石]测试摊" {
		t.Errorf("头上应有摊位名：%s", markV)
	}
	if mk, ok := find[*proto.ObjMark](clb.take()); !ok || mk.Vendor == "" {
		t.Error("附近的人应收到摊位名的记号")
	}
	w.Vendor(bid, "open", "", nil, nil, nil)
	list, ok := find[*proto.VendorList](clb.take())
	if !ok || len(list.Entries) != 2 || list.Seller != "店家" || list.Entries[1].Kind != 1 || list.Entries[0].Price != 100 {
		t.Fatalf("买家应看到两项：%+v", list)
	}
	w.Vendor(bid, "detail", "", nil, []int{1}, nil)
	if d, ok := find[*proto.VendorDetail](clb.take()); !ok || d.Kind != 1 || d.Pet.Level <= 0 {
		t.Errorf("应收到宠物的详细资料：%+v", d)
	}
	w.Vendor(bid, "buy", "", nil, []int{0, 1}, nil)
	msgs := clb.take()
	list, ok = find[*proto.VendorList](msgs)
	if !ok || len(list.Entries) != 0 {
		t.Errorf("买完后清单应是空的：%+v", list)
	}
	if !strings.Contains(fmtMsgs(msgs), "买下了") {
		t.Error("买家应收到买下的讯息")
	}
	w.Do(func() {
		se, be := w.players[sid], w.players[bid]
		if be.char.Gold != 700 || se.char.Gold != 300 {
			t.Errorf("钱应换手：买家 %d 店家 %d", be.char.Gold, se.char.Gold)
		}
		if se.inv[slot] != nil || se.pets.Pets[petSlot] != nil {
			t.Error("店家的道具与宠物应已卖出")
		}
		got := false
		for _, it := range be.inv {
			if it != nil && it.ID == itemID {
				got = true
			}
		}
		if !got || be.pets.freeCount() != PetSlots-1 {
			t.Errorf("买家应拿到道具与宠物：%v %d", got, be.pets.freeCount())
		}
		if !w.vendorSoldOut(se) {
			t.Error("应卖完")
		}
	})
	if !strings.Contains(fmtMsgs(cls.take()), "卖完") {
		t.Error("店家应收到卖完的讯息")
	}
	w.Vendor(sid, "close", "", nil, nil, nil)
	w.Do(func() {
		if w.players[sid].stall != nil || w.players[bid].buying != nil {
			t.Error("收摊后应清掉双方的状态")
		}
	})
	if _, ok := find[*proto.VendorClose](clb.take()); !ok {
		t.Error("买家的窗口应关掉")
	}
}

// 摆摊的限制：登出消失的道具与守护兽不能卖、摆着时不能走、钱不够买不了。
func TestStreetVendorLimits(t *testing.T) {
	w, db, g := setup(t)
	s, cls := petOwner(t, w, db, "摊主", 10, 60)
	sid := int(s.ID)
	vanish := 0
	for id, def := range g.Items {
		if def.VanishDrop {
			vanish = id
			break
		}
	}
	givePet(t, w, sid, petEnemy(t, g, 1), 5)
	var slot int
	w.Do(func() {
		se := w.players[sid]
		slot = w.addItem(se.inv, &Item{ID: vanish})
		for _, p := range se.pets.Pets {
			if p != nil {
				p.Guard = true
			}
		}
	})
	cls.take()
	w.Vendor(sid, "set", "", []int{0, 1}, []int{slot, 0}, []int{10, 10})
	msgs := fmtMsgs(cls.take())
	w.Do(func() {
		if w.players[sid].stall != nil {
			t.Error("两项都不能卖时不应开张")
		}
	})
	if !strings.Contains(msgs, "会消失的道具") || !strings.Contains(msgs, "守护兽") {
		t.Errorf("应说明为什麽取消：%s", msgs)
	}
	w.Do(func() {
		se := w.players[sid]
		for _, p := range se.pets.Pets {
			if p != nil {
				p.Guard = false
			}
		}
	})
	w.Vendor(sid, "set", strings.Repeat("长", 9), []int{1}, []int{0}, []int{10})
	if !strings.Contains(fmtMsgs(cls.take()), "标题过长") {
		t.Error("17 个字节以上的标题应被拒绝")
	}
	w.Vendor(sid, "set", "", []int{1}, []int{0}, []int{10})
	var x, y int
	w.Do(func() {
		se := w.players[sid]
		if se.stall == nil || se.stall.title != "[石]欢迎光临!!" {
			t.Fatalf("空标题应是欢迎光临：%+v", se.stall)
		}
		x, y = se.x, se.y
	})
	cls.take()
	w.Move(sid, x, y, 0)
	if _, ok := find[*proto.PosCorrect](cls.take()); !ok {
		t.Error("摆着摊时不能走动")
	}
	w.Do(func() {
		w.placeOnFloor(w.players[sid], w.players[sid].floor, x, y, 0)
		if w.players[sid].stall != nil {
			t.Error("被传送时应收摊")
		}
	})
}

// NPC 参数里的组队 / 时间 / DP 条件（NPC_ActionFreeIfCheck）。
func TestNpcCondPartyTimeDP(t *testing.T) {
	w, db, _ := setup(t)
	a, _ := petOwner(t, w, db, "甲", 10, 60)
	b, _ := petOwner(t, w, db, "乙", 10, 60)
	w.Do(func() {
		pa, pb := w.players[int(a.ID)], w.players[int(b.ID)]
		pa.char.DP = 50
		pa.char.Sprite, pb.char.Sprite = 100000, 100120 // 小男孩、女生
		check := func(cond string, want bool) {
			t.Helper()
			if w.passCheck(pa, cond) != want {
				t.Errorf("%s 应是 %v", cond, want)
			}
		}
		check("PARTY=0", true)
		check("PARTY>0", false)
		check("DP>49", true)
		check("DP<50", false)
		check("MANCOUNT=1", true)
		check("WOMANCOUNT=0", true)
		check("GTIME=FREE", true)
		check("GTIME=NOSUCH", false)
		sec := lsSection(w.saHour())
		check("TIME="+string(rune('0'+sec)), true)
		// 组队后：两个人、一男一女
		pa.party = &party{}
		pa.party.slots[0], pa.party.slots[1] = pa, pb
		pb.party = pa.party
		check("PARTY=2", true)
		check("PARTYCOUNT>1", true)
		check("MANCOUNT=1&WOMANCOUNT=1", true)
	})
}

// 道场的人形敌人（jyajyadou001 的 enemyno 564..580 是门下生，ENEMY_RandomChange 按敌人编号判断）：随机外形、属性、武器与技能（gymRandomize）。
func TestGymRandomize(t *testing.T) {
	w, _, g := setup(t)
	n := findNPC(t, w, "NPCEnemy", "enemyno:564,565")
	if e := g.Battle.Enemies[564]; e == nil || g.Battle.Temps[e.TempNo] == nil {
		t.Fatalf("应有敌人 564（门下生）：%+v", e)
	}
	sprites := map[int]bool{}
	for i := 0; i < 20; i++ {
		w.Do(func() {
			out := w.doujyouEnemies(n, 3)
			if len(out) == 0 {
				t.Fatal("应有敌人")
			}
			// 第一个用 NPC 的形象；再做一个不盖形象的看随机外形
			f := battle.NewEnemy(battle.EnemySpec{Enemy: g.Battle.Enemies[564], Temp: g.Battle.Temps[g.Battle.Enemies[564].TempNo], Level: 3}, 0, g.Battle.Skills, w.rnd)
			w.gymRandomize(f, 564)
			sprites[f.Sprite] = true
			if f.Sprite < 100000 || f.Sprite >= 100240 || (f.Sprite-100000)%5 != 0 {
				t.Errorf("人形敌人应是玩家的外形：%d", f.Sprite)
			}
			if f.Attr[0]+f.Attr[1]+f.Attr[2]+f.Attr[3] != 100 || (f.Attr[0] > 0 && f.Attr[2] > 0) || (f.Attr[1] > 0 && f.Attr[3] > 0) {
				t.Errorf("属性应地火相抵、水风相抵、合计 100：%v", f.Attr)
			}
			if f.Skills[0] == nil || f.Skills[1] == nil {
				t.Errorf("前两格技能应有：%v", f.Skills[:2])
			}
			switch f.Weapon {
			case data.ItemBow, data.ItemBoomerang, data.ItemBoundThrow, data.ItemBreakThrow:
				if f.Skills[0].ID != 0 {
					t.Errorf("远程武器只会普通攻击：%d", f.Skills[0].ID)
				}
			}
			if out[0].Sprite != n.sprite || out[0].Name != n.name {
				t.Error("第一个敌人应用 NPC 的形象与名字")
			}
		})
	}
	if len(sprites) < 3 {
		t.Errorf("外形应随机：%v", sprites)
	}
}
