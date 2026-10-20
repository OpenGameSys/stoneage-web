package world

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// itemByFunc 找一种 usefunc 是 fn、参数含 sub 的道具（种类要是"其他"或"料理"，地图上才会用）。
func itemByFunc(t *testing.T, g *data.Game, fn, sub string) *data.ItemDef {
	t.Helper()
	var best *data.ItemDef
	for _, d := range g.Items {
		if d.UseFunc == fn && strings.Contains(d.Argument, sub) && (d.Type == data.ItemOther || d.Type == data.ItemDish) &&
			(best == nil || d.ID < best.ID) {
			best = d
		}
	}
	if best == nil {
		t.Skipf("没有 %s（%s）的道具", fn, sub)
	}
	return best
}

// bagSlot 放一个道具进道具栏，回传格位。
func bagSlot(w *World, id int, def *data.ItemDef) int {
	slot := -1
	w.Do(func() {
		e := w.players[id]
		slot = w.addItem(e.inv, w.newItem(def))
	})
	return slot
}

// 回复道具对宠物用（ITEM_useRecovery_Field）："X的耐久力回复N"；只回气力的道具对宠物没有作用、不用掉。
func TestItemRecoveryPet(t *testing.T) {
	w, db, g := setup(t)
	c, cl := petOwner(t, w, db, "feeder", 10, 60)
	id := int(c.ID)
	ps := givePet(t, w, id, petEnemy(t, g, 1), 5)
	w.Do(func() { w.players[id].pets.Pets[ps].HP = 1 })
	def := itemByFunc(t, g, "ITEM_useRecovery", "体")
	slot := bagSlot(w, id, def)
	cl.take()
	w.ItemUse(id, slot, ps+1)
	if !strings.Contains(fmtMsgs(cl.take()), "的耐久力回复") {
		t.Fatal("应说宠物的耐久力回复了多少")
	}
	w.Do(func() {
		if w.players[id].pets.Pets[ps].HP <= 1 {
			t.Error("宠物的 HP 应回复")
		}
	})
	mp := itemByFunc(t, g, "ITEM_useRecovery", "气")
	if strings.Contains(mp.Argument, "体") || strings.Contains(mp.Argument, "全") {
		t.Skip("找不到只回气力的道具")
	}
	s2 := bagSlot(w, id, mp)
	w.ItemUse(id, s2, ps+1)
	w.Do(func() {
		if w.players[id].inv[s2] == nil {
			t.Error("对宠物用只回气力的道具不应用掉")
		}
	})
}

// 技能罐头（ITEM_useSkillCanned）：对人用说"仅限宠物使用"；对宠物用开选技能栏的窗口，选了就学会、道具消失。
func TestSkillCanned(t *testing.T) {
	w, db, g := setup(t)
	c, cl := petOwner(t, w, db, "canner", 10, 60)
	id := int(c.ID)
	ps := givePet(t, w, id, petEnemy(t, g, 1), 5)
	def := itemByFunc(t, g, "ITEM_useSkillCanned", "")
	sk := g.Battle.Skills[data.Atoi(def.Argument)]
	if sk == nil {
		t.Skip("罐头的技能不在 petskill.txt")
	}
	slot := bagSlot(w, id, def)
	cl.take()
	w.ItemUse(id, slot, 0)
	if !strings.Contains(fmtMsgs(cl.take()), "仅限宠物使用") {
		t.Fatal("对人用应说仅限宠物使用")
	}
	w.ItemUse(id, slot, ps+1)
	win, ok := find[*proto.NpcWindow](cl.take())
	if !ok || win.Kind != "petskillslot" || win.Text != strconv.Itoa(ps) {
		t.Fatalf("应开选技能栏的窗口：%+v", win)
	}
	w.NpcWindowReply(id, 0, win.Seq, 0, "1")
	if !strings.Contains(fmtMsgs(cl.take()), "学习"+sk.Name) {
		t.Error("应学会技能")
	}
	w.Do(func() {
		e := w.players[id]
		if e.pets.Pets[ps].Skills[1] != sk.ID || e.inv[slot] != nil {
			t.Errorf("技能应在第 2 栏、道具消失：%v", e.pets.Pets[ps].Skills)
		}
	})
}

// 智慧之果（ITEM_Addexp）：设定百分比与时间、存起来；同样的百分比累加时间；打赢的经验照比例增加。
func TestAddExp(t *testing.T) {
	w, db, g := setup(t)
	c, cl := petOwner(t, w, db, "wise", 10, 60)
	id := int(c.ID)
	def := itemByFunc(t, g, "ITEM_Addexp", "增")
	power, minutes := argNum(def.Argument, "增", 0), argNum(def.Argument, "分", 0)
	slot := bagSlot(w, id, def)
	cl.take()
	w.ItemUse(id, slot, 0)
	if !strings.Contains(fmtMsgs(cl.take()), "学习经验的能力提升了") {
		t.Fatal("应说学习经验的能力提升了")
	}
	w.ItemUse(id, bagSlot(w, id, def), 0)
	w.Do(func() {
		e := w.players[id]
		if e.addExp.power != power || e.addExp.secs != 2*minutes*60 || e.char.AddExp == "" {
			t.Errorf("同样的百分比应累加时间：%+v", e.addExp)
		}
		if got := e.addExp.boost(100); got != 100+power {
			t.Errorf("经验应加 %d%%：%d", power, got)
		}
		// 每 300 秒扣一次
		before := e.addExp.secs
		w.tickAddExp(e)
		if e.addExp.secs != before-300 {
			t.Errorf("每 300 秒应扣 300：%d", e.addExp.secs)
		}
	})
}

// 体质（ITEM_Constitution）：把点数换回可分配点数；点数不够时"物品无效。"、不用掉。
func TestConstitution(t *testing.T) {
	w, db, g := setup(t)
	c, cl := petOwner(t, w, db, "body", 10, 60)
	id := int(c.ID)
	def := itemByFunc(t, g, "ITEM_Constitution", ":")
	slot := bagSlot(w, id, def)
	var before int
	w.Do(func() { e := w.players[id]; before = e.char.Points })
	cl.take()
	w.ItemUse(id, slot, 0)
	msgs := fmtMsgs(cl.take())
	w.Do(func() {
		e := w.players[id]
		if strings.Contains(msgs, "你的身体感到变化。") {
			if e.char.Points <= before {
				t.Error("应多出可分配的点数")
			}
		} else if !strings.Contains(msgs, "物品无效。") {
			t.Errorf("应说身体感到变化或物品无效：%s", msgs)
		}
	})
}

// 传送道具（ITEM_useWarp）：到参数的地点，用掉；有次数的飞行道具（ITEM_useWarpForNum）飞到记录的地点并记下现在的位置。
func TestItemWarp(t *testing.T) {
	w, db, g := setup(t)
	c, cl := petOwner(t, w, db, "flyer", 10, 60)
	id := int(c.ID)
	def := itemByFunc(t, g, "ITEM_useWarp", " ")
	p := strings.Fields(def.Argument)
	floor := data.Atoi(p[1])
	if g.Floors[floor] == nil {
		t.Skipf("没有 %d 号地图", floor)
	}
	slot := bagSlot(w, id, def)
	cl.take()
	w.ItemUse(id, slot, 0)
	w.Do(func() {
		e := w.players[id]
		if e.floor != floor {
			t.Errorf("应传送到 %d：%d", floor, e.floor)
		}
	})

	num := itemByFunc(t, g, "ITEM_useWarpForNum", " ")
	if num.Uses < 2 {
		t.Skip("飞行道具的次数不到 2")
	}
	w.Do(func() { w.placeOnFloor(w.players[id], 100, 600, 450, 0) })
	s2 := bagSlot(w, id, num)
	cl.take()
	w.ItemUse(id, s2, 0)
	if !strings.Contains(fmtMsgs(cl.take()), "飞行纪录点(萨伊那斯,600,450)") {
		t.Error("应记下现在的位置")
	}
	w.Do(func() {
		it := w.players[id].inv[s2]
		if it == nil || it.Uses != num.Uses-1 || !strings.HasPrefix(it.Arg, "0 100 600 450") && !strings.Contains(it.Arg, " 100 600 450") {
			t.Errorf("道具应改写成新的地点、次数减一：%+v", it)
		}
	})
}

// 变身镜（ITEM_metamo）：变成自己的宠物，时间到了变回来；骑着时不能变。
func TestMetamo(t *testing.T) {
	w, db, g := setup(t)
	c, cl := petOwner(t, w, db, "mirror", 10, 60)
	id := int(c.ID)
	ps := givePet(t, w, id, petEnemy(t, g, 1), 5)
	var def *data.ItemDef
	for _, d := range g.Items {
		if d.UseFunc == "ITEM_metamo" && (d.Type == data.ItemOther || d.Type == data.ItemDish) && argNum(" "+d.Argument, " ", 0) > 0 {
			def = d
			break
		}
	}
	if def == nil {
		t.Skip("没有变身镜")
	}
	slot := bagSlot(w, id, def)
	cl.take()
	w.ItemUse(id, slot, ps+1)
	var petSprite int
	w.Do(func() {
		e := w.players[id]
		petSprite = e.pets.Pets[ps].Sprite
		if e.sprite != petSprite {
			t.Fatalf("应变成宠物的样子：%d %d", e.sprite, petSprite)
		}
	})
	if !strings.Contains(fmtMsgs(cl.take()), "变身成") {
		t.Error("应说变身成什么")
	}
	w.Do(func() {
		start := time.Now()
		w.now = func() time.Time { return start.Add(time.Hour) }
		w.tickPlayers()
		if e := w.players[id]; e.sprite == petSprite || e.metamo != nil {
			t.Error("时间到了应变回来")
		}
	})
	if !strings.Contains(fmtMsgs(cl.take()), "变身失效了。") {
		t.Error("应说变身失效了")
	}
}
