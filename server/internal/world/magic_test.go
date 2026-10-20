package world

import (
	"strings"
	"testing"

	"sa/server/internal/battle"
	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// magicItem 找一件带着某种咒术的装备（道具的 magicid）。
func magicItem(t *testing.T, g *data.Game, fn string) (*data.ItemDef, *data.Magic) {
	t.Helper()
	for _, def := range g.Items {
		if def.MagicID < 0 || def.MagicMP <= 0 || data.EquipSlot(def.Type) < 0 {
			continue
		}
		if m := g.Battle.Magics[def.MagicID]; m != nil && m.Func == fn {
			return def, m
		}
	}
	t.Skipf("没有带 %s 的道具", fn)
	return nil, nil
}

// equipMagicItem 把道具直接装上，回传装备位。
func equipMagicItem(w *World, id int, def *data.ItemDef) int {
	slot := data.EquipSlot(def.Type)
	w.Do(func() {
		e := w.players[id]
		e.inv[slot] = w.newItem(def)
		w.sendInventory(e)
	})
	return slot
}

// 地图上的滋润精灵（MAGIC_OtherRecovery_Field）：回复宠物，扣道具上写的 MP；道具栏里带着咒术的资料（精灵窗口用）。
func TestMagicFieldRecovery(t *testing.T) {
	w, db, g := setup(t)
	def, m := magicItem(t, g, "MAGIC_OtherRecovery")
	c, cl := petOwner(t, w, db, "healer", 10, 60)
	id := int(c.ID)
	slot := givePet(t, w, id, petEnemy(t, g, 1), 5)
	es := equipMagicItem(w, id, def)
	inv, _ := find[*proto.Inventory](cl.take())
	listed := false
	for _, it := range inv.Items {
		listed = listed || (it.Slot == es && it.Magic == m.Name && it.MagicMp == def.MagicMP)
	}
	if !listed {
		t.Fatalf("装备着的道具应带着咒术的名字与 MP：%+v", inv.Items)
	}
	var mp, maxHP int
	w.Do(func() {
		e := w.players[id]
		e.char.MP = 100
		e.pets.Pets[slot].HP = 1
		maxHP = e.pets.Pets[slot].derive().HP
		mp = e.char.MP
	})
	w.MagicUse(id, es, slot+1)
	msgs := cl.take()
	if !strings.Contains(fmtMsgs(msgs), "的耐久力回复") {
		t.Errorf("应说宠物回复了多少：%s", fmtMsgs(msgs))
	}
	w.Do(func() {
		e := w.players[id]
		p := data.Atoi(strings.Fields(m.Option)[0])
		if hp := e.pets.Pets[slot].HP; hp != maxHP && (hp-1 < p*9/10 || hp-1 > p*11/10) {
			t.Errorf("宠物 HP 应回复 %d 的 0.9..1.1 倍：%d", p, hp-1)
		}
		if e.char.MP != mp-def.MagicMP {
			t.Errorf("MP 应扣 %d：%d → %d", def.MagicMP, mp, e.char.MP)
		}
		e.char.MP = 0
	})
	// MP 不够就不能用
	w.MagicUse(id, es, 0)
	if strings.Contains(fmtMsgs(cl.take()), "耐久力回复") {
		t.Error("MP 不够不应回复")
	}
}

// 战斗中的咒术（"J|装备位|目标"）：目标要合咒术的种类（MAGIC_isTargetValid），施法扣 MP，攻击魔法打到敌人。
func TestMagicBattle(t *testing.T) {
	w, db, g := setup(t)
	def, m := magicItem(t, g, "MAGIC_AttMagic")
	c, cl := petOwner(t, w, db, "caster", 30, 60)
	id := int(c.ID)
	es := equipMagicItem(w, id, def)
	ene := petEnemy(t, g, 1)
	var target *battle.Fighter
	w.Do(func() {
		w.players[id].char.MP = 100
		target = enemyFighter(w, ene, 1, battle.EnemySlot(0))
		w.startBattleWith(w.players[id], []*battle.Fighter{target}, nil, nil)
		w.players[id].fight.b.F[target.Slot].Tactics = "gu:1"
	})
	cl.take()
	if !magicTargetValid(m, 3) || magicTargetValid(m, 22) != (m.Target == data.MagicTargetAll) || magicTargetValid(m, 27) {
		t.Fatal("MAGIC_isTargetValid 不对")
	}
	to := target.Slot
	switch m.Target {
	case data.MagicTargetWholeOtherSide, data.MagicTargetAllRows:
		to = 21
	case data.MagicTargetAll:
		to = 22
	case data.MagicTargetOneRow:
		to = 23
	}
	w.BattleCommand(id, "magic", to, es)
	tu, ok := find[*proto.BattleTurn](cl.take())
	if !ok {
		t.Fatal("应结算")
	}
	cast := false
	for _, ev := range tu.Events {
		if ev.Kind == "magic" && ev.Actor == 0 {
			cast = true
			if ev.Skill != m.Name {
				t.Errorf("事件应带咒术名：%+v", ev)
			}
		}
	}
	if !cast {
		t.Fatalf("应有施法的事件：%+v", tu.Events)
	}
	w.Do(func() {
		f := w.players[id].fight
		if f == nil {
			return
		}
		if p := f.b.F[0]; p.MP != 100-def.MagicMP {
			t.Errorf("施法应扣 %d MP：%d", def.MagicMP, p.MP)
		}
	})
}
