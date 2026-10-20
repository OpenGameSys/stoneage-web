package world

import (
	"strings"
	"testing"

	"sa/server/internal/battle"
	"sa/server/internal/data"
)

// 宠物身上的道具（CHAR_movePetItem）：忠诚变动值不够、种类不对、等级不够都装不上；装上后能力加到宠物身上；
// 宠物身上的不能丢；战斗中挨打会磨损。
func TestPetItem(t *testing.T) {
	w, db, g := setup(t)
	c, cl := petOwner(t, w, db, "petitem", 10, 60)
	id := int(c.ID)
	ps := givePet(t, w, id, petEnemy(t, g, 1), 1)
	var head, claw *data.ItemDef
	for _, d := range g.Items {
		if d.Level < 1 || d.Durability <= 0 {
			continue
		}
		switch data.PetEquipSlot(d.Type) {
		case 0:
			if head == nil || d.ID < head.ID {
				head = d
			}
		case data.PetSlotClaw:
			if claw == nil || d.ID < claw.ID {
				claw = d
			}
		}
	}
	if head == nil {
		t.Skip("没有宠物头部装备")
	}
	bag := data.EquipSlots
	var p *Pet
	w.Do(func() {
		e := w.players[id]
		p = e.pets.Pets[ps]
		e.inv[bag] = w.newItem(head)
		p.VariableAI = 0
	})
	cl.take()
	w.PetItemEquip(id, ps, bag, -1)
	w.Do(func() {})
	if msgs := fmtMsgs(cl.take()); !strings.Contains(msgs, "无法训服宠物装备该物品。") {
		t.Fatalf("忠诚变动值不到 100 不能装：%s", msgs)
	}
	w.Do(func() { p.VariableAI = 100 })
	w.PetItemEquip(id, ps, bag, -1)
	w.Do(func() {})
	if msgs := fmtMsgs(cl.take()); !strings.Contains(msgs, "宠物等级尚未到达装备该项物品！") {
		t.Fatalf("等级不够不能装：%s", msgs)
	}
	var before int
	w.Do(func() {
		p.Level = head.Level
		d, _ := w.petDerive(p)
		before = d.Atk + d.Def + d.Quick + d.HP
	})
	w.PetItemEquip(id, ps, bag, 1) // 头部装备放到翼那一格
	w.Do(func() {})
	if msgs := fmtMsgs(cl.take()); !strings.Contains(msgs, "无法装备该项物品。") {
		t.Fatalf("放错格应装不上：%s", msgs)
	}
	w.PetItemEquip(id, ps, bag, -1)
	w.Do(func() {
		e := w.players[id]
		if p.item(0) == nil || e.inv[bag] != nil {
			t.Fatalf("应装到头部：%+v", p.Items)
		}
		d, _ := w.petDerive(p)
		mods := 0
		for i := 0; i < 4; i++ {
			mods += p.item(0).Mods[[]int{data.ModAtk, data.ModDef, data.ModQuick, data.ModHP}[i]]
		}
		if !strings.ContainsAny(head.Argument, "攻防敏体") && d.Atk+d.Def+d.Quick+d.HP != before+mods {
			t.Errorf("能力应加上道具：%d + %d ≠ %d", before, mods, d.Atk+d.Def+d.Quick+d.HP)
		}
		if pi := w.petInfo(e, ps); len(pi.Items) != 1 || pi.Items[0].Slot != 0 {
			t.Errorf("宠物资料应带身上的道具：%+v", pi.Items)
		}
	})
	if msgs := fmtMsgs(cl.take()); !strings.Contains(msgs, "装备"+head.Name) {
		t.Errorf("应说装备了：%s", msgs)
	}
	w.PetItemEquip(id, ps, 0, -2)
	w.Do(func() {})
	if msgs := fmtMsgs(cl.take()); !strings.Contains(msgs, "无法丢弃宠物身上的物品。") {
		t.Errorf("宠物身上的不能丢：%s", msgs)
	}

	// 战斗中挨打磨损身上的道具
	w.Do(func() {
		e := w.players[id]
		e.pets.Battle = ps
		en := enemyFighter(w, petEnemy(t, g, 1), 1, battle.EnemySlot(0))
		w.startBattleWith(e, []*battle.Fighter{en}, nil, nil)
		ft := e.fight
		dur := itemDur(head, p.item(0))
		w.battleCrush(e, ft, en, ft.b.F[ft.slot+5], 10)
		if got := itemDur(head, p.item(0)); got >= dur {
			t.Errorf("宠物挨打应磨损头部装备：%d → %d", dur, got)
		}
	})

	// 拿下来
	w.Do(func() { w.players[id].fight = nil })
	w.PetItemEquip(id, ps, 0, bag+1)
	w.Do(func() {
		if p.Items != nil || w.players[id].inv[bag+1] == nil {
			t.Errorf("应拿回道具栏：%+v", p.Items)
		}
	})
	if msgs := fmtMsgs(cl.take()); !strings.Contains(msgs, "卸下"+head.Name) {
		t.Errorf("应说卸下：%s", msgs)
	}
}
