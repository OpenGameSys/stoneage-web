package world

import (
	"sort"
	"strings"
	"testing"

	"sa/server/internal/data"
)

// mergeSetup：一个有宠物（第 0 格技能是 skill）的角色，回传角色编号与宠物格位。
func mergeSetup(t *testing.T, name string, skill int) (*World, *data.Game, int, int, *fakeClient) {
	w, db, g := setup(t)
	c, cl := petOwner(t, w, db, name, 10, 60)
	id := int(c.ID)
	ps := givePet(t, w, id, petEnemy(t, g, 1), 1)
	w.Do(func() { w.players[id].pets.Pets[ps].Skills[0] = skill })
	cl.take()
	return w, g, id, ps, cl
}

// sortedItems 按编号列出符合条件的道具。
func sortedItems(g *data.Game, ok func(*data.ItemDef) bool) []*data.ItemDef {
	var out []*data.ItemDef
	for _, d := range g.Items {
		if ok(d) {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// 加工：材料用掉，做出一件标了 MERGEFLG 的道具；多试几次应该做得出材料以外的东西。料理与道具混在一起是非法的。
func TestPetMerge(t *testing.T) {
	w, g, id, ps, cl := mergeSetup(t, "merger", 200)
	mats := sortedItems(g, func(d *data.ItemDef) bool {
		return d.CanMergeFrom && d.Type != data.ItemDish && len(d.Ings) > 0 && w.game.Atoms[d.Ings[0].Name]
	})
	if len(mats) < 2 {
		t.Skip("没有材料")
	}
	bag := data.EquipSlots
	fresh := 0
	for n := 0; n < 30; n++ {
		a, b := mats[n%len(mats)], mats[(n*7+1)%len(mats)]
		if a.ID == b.ID {
			continue
		}
		w.Do(func() {
			e := w.players[id]
			for i := bag; i < data.ItemSlots; i++ {
				e.inv[i] = nil
			}
			e.inv[bag], e.inv[bag+1] = w.newItem(a), w.newItem(b)
			e.lastMerge = 0
		})
		w.PetSkillUse(id, ps, 0, "9|10")
		w.Do(func() {
			e := w.players[id]
			var got []*Item
			for i := bag; i < data.ItemSlots; i++ {
				if e.inv[i] != nil {
					got = append(got, e.inv[i])
				}
			}
			if len(got) != 1 || !got[0].Merged {
				t.Fatalf("两样材料应换成一件加工品：%+v", got)
			}
			if got[0].ID != a.ID && got[0].ID != b.ID {
				fresh++
			}
		})
	}
	if fresh == 0 {
		t.Error("30 次加工都只还回材料")
	}
	cl.take()

	dishes := sortedItems(g, func(d *data.ItemDef) bool { return d.CanMergeFrom && d.Type == data.ItemDish })
	if len(dishes) == 0 {
		return
	}
	w.Do(func() {
		e := w.players[id]
		e.inv[bag], e.inv[bag+1], e.lastMerge = w.newItem(dishes[0]), w.newItem(mats[0]), 0
	})
	w.PetSkillUse(id, ps, 0, "9|10")
	w.Do(func() {})
	if msgs := fmtMsgs(cl.take()); !strings.Contains(msgs, "非法的合成方法") {
		t.Errorf("料理与道具混合应是非法的：%s", msgs)
	}
}

// 修复：装备与同成分的材料，耐久不到 80% 时补满，材料用掉。
func TestPetFixItem(t *testing.T) {
	w, g, id, ps, cl := mergeSetup(t, "fixer", 540)
	var arm, mat *data.ItemDef
	for _, d := range sortedItems(g, func(d *data.ItemDef) bool { return fixableType(d.Type) && d.Durability >= 500 && len(d.Ings) > 0 }) {
		for _, m := range sortedItems(g, func(m *data.ItemDef) bool { return len(m.Ings) > 0 && !fixableType(m.Type) && m.Type != data.ItemDish }) {
			if m.Ings[0].Name == d.Ings[0].Name {
				arm, mat = d, m
				break
			}
		}
		if arm != nil {
			break
		}
	}
	if arm == nil {
		t.Skip("没有可修的装备与材料")
	}
	bag := data.EquipSlots
	w.Do(func() {
		e := w.players[id]
		e.inv[bag], e.inv[bag+1] = w.newItem(arm), w.newItem(mat)
	})
	w.PetSkillUse(id, ps, 0, "9|10")
	w.Do(func() {})
	if msgs := fmtMsgs(cl.take()); !strings.Contains(msgs, "并没有损坏到需要修复") {
		t.Fatalf("满耐久不用修：%s", msgs)
	}
	w.Do(func() { w.players[id].inv[bag].Dur, w.players[id].inv[bag].Crush = arm.Durability/2, 2 })
	w.PetSkillUse(id, ps, 0, "9|10")
	w.Do(func() {
		e := w.players[id]
		if it := e.inv[bag]; it.Dur != arm.Durability || it.Crush != 0 || e.inv[bag+1] != nil {
			t.Errorf("应补满耐久并用掉材料：%+v %+v", it, e.inv[bag+1])
		}
	})
	if msgs := fmtMsgs(cl.take()); !strings.Contains(msgs, "修复完成。") {
		t.Errorf("应说修复完成：%s", msgs)
	}
}

// 镶宝石：宝石的能力加到精工装备上、说明改写、宝石用掉；最多 3 颗。
func TestPetInslay(t *testing.T) {
	w, g, id, ps, cl := mergeSetup(t, "inslayer", 572)
	bases := sortedItems(g, func(d *data.ItemDef) bool { return strings.Contains(d.TypeCode, "INSLAY") })
	ruby := g.Items[20288]
	if len(bases) == 0 || ruby == nil {
		t.Skip("没有精工装备或红宝石")
	}
	bag := data.EquipSlots
	var atk int
	w.Do(func() {
		e := w.players[id]
		e.inv[bag] = w.newItem(bases[0])
		atk = e.inv[bag].Mods[data.ModAtk]
		for i := 1; i <= 4; i++ {
			e.inv[bag+i] = w.newItem(ruby)
		}
	})
	w.PetSkillUse(id, ps, 0, "9|10|11|12|13")
	w.Do(func() {
		e := w.players[id]
		it := e.inv[bag]
		if it.Mods[data.ModAtk] != atk+3*ruby.Mods[data.ModAtk][0] || !strings.HasPrefix(it.Memo, "攻+") {
			t.Errorf("三颗红宝石的攻击应加上去：%d → %+v", atk, it)
		}
		if it.Inlay == "" || strings.Count(it.Inlay, ruby.TypeCode) != 3 || e.inv[bag+4] == nil {
			t.Errorf("镶了 3 颗、第 4 颗留着：%q %+v", it.Inlay, e.inv[bag+4])
		}
	})
	if msgs := fmtMsgs(cl.take()); !strings.Contains(msgs, "已经镶满3颗") {
		t.Errorf("第 4 颗应镶不下：%s", msgs)
	}
}
