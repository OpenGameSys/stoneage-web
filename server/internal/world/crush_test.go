package world

import (
	"strings"
	"testing"

	"sa/server/internal/battle"
	"sa/server/internal/data"
)

// 装备的耐久：挨打时磨损防具（BATTLE_ItemCrush）；耐久显示百分比；耐久到 0 时消失；E啮齿术把装备咬到 70%。
func TestItemCrush(t *testing.T) {
	w, db, g := setup(t)
	c, cl := petOwner(t, w, db, "worn", 10, 60)
	id := int(c.ID)
	var helm *data.ItemDef
	for _, d := range g.Items {
		if data.EquipSlot(d.Type) == data.SlotHead && d.Durability > 0 && d.Level <= 1 && (helm == nil || d.ID < helm.ID) {
			helm = d
		}
	}
	if helm == nil {
		t.Skip("没有头盔")
	}
	var ft *fight
	w.Do(func() {
		e := w.players[id]
		e.inv[data.SlotHead] = w.newItem(helm)
		en := enemyFighter(w, petEnemy(t, g, 1), 1, battle.EnemySlot(0))
		w.startBattleWith(e, []*battle.Fighter{en}, nil, nil)
		ft = e.fight
		before := itemDur(helm, e.inv[data.SlotHead])
		w.battleCrush(e, ft, en, ft.b.F[ft.slot], 10)
		if got := itemDur(helm, e.inv[data.SlotHead]); got >= before || got < before-14 {
			t.Errorf("伤害 10 应磨掉 9..13：%d → %d", before, got)
		}
		if s := itemDamage(helm, e.inv[data.SlotHead]); !strings.HasSuffix(s, "%") {
			t.Errorf("耐久度应显示百分比：%s", s)
		}
		e.inv[data.SlotHead].Dur = helm.Durability // 满耐久时咬到 70%
		w.battleTooth(e, ft)
		if it := e.inv[data.SlotHead]; it.Crush != 1 || it.Dur != int(float64(helm.Durability)*0.7) {
			t.Errorf("啮齿术应咬到 70%%、受损：%+v", it)
		}
		w.setDur(e, ft, e.inv[:], data.SlotHead, helm, 0, false)
		if e.inv[data.SlotHead] != nil {
			t.Error("耐久到 0 应消失")
		}
	})
	if !strings.Contains(fmtMsgs(cl.take()), "因过度损坏而消失") {
		t.Error("应说因过度损坏而消失")
	}
}
