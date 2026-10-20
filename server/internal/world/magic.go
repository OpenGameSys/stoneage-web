package world

import (
	"fmt"
	"strings"

	"sa/server/internal/battle"
	"sa/server/internal/data"
)

// 咒术（精灵）：装备着的道具带的咒术（道具的 magicid / magicusemp），战斗中用 battle.Magic，地图上用 MagicUse（magic.c MAGIC_Recovery 等的地图部分）。

// equipMagic 是 MAGIC_GetArrayNo：装备位 slot 的道具带的咒术与要的 MP。
func (w *World) equipMagic(e *entity, slot int) (*data.Magic, int) {
	if slot < 0 || slot >= data.EquipSlots || e.inv == nil || e.inv[slot] == nil {
		return nil, 0
	}
	it := e.inv[slot]
	def := w.game.Items[it.ID]
	if def == nil || w.game.Battle == nil {
		return nil, 0
	}
	id, mp := def.MagicID, def.MagicMP
	if it.Magic != nil { // 镶上去的宝石带的咒术
		id, mp = it.Magic[0], it.Magic[1]
	}
	m := w.game.Battle.Magics[id]
	if id < 0 || m == nil {
		return nil, 0
	}
	return m, mp
}

// magicTargetValid 是 MAGIC_isTargetValid（_ATTACK_MAGIC）：0..19 单人都行；20 / 21 一边要"点哪边就是哪边"或"整排"的咒术，
// 22 全体要 MAGIC_TARGET_ALL，23..26 一排要 MAGIC_TARGET_ONE_ROW。
func magicTargetValid(m *data.Magic, to int) bool {
	switch {
	case to >= 0 && to <= 19:
		return true
	case to == 20 || to == 21:
		return m.Target == data.MagicTargetWholeOtherSide || m.Target == data.MagicTargetAllRows
	case to == 22:
		return m.Target == data.MagicTargetAll
	case to >= 23 && to <= 26:
		return m.Target == data.MagicTargetOneRow
	}
	return false
}

// MagicUse 是 lssproto_MU_recv → MAGIC_Use：在地图上用装备位 slot 的咒术，target 0 为自己、1..5 为第几只宠物（Callfromcli_Util_getTargetCharaindex）。
// 地图上能用的只有 MAGIC_Recovery（回复自己，MAGIC_Recovery_Field）与 MAGIC_OtherRecovery（回复自己或宠物，MAGIC_OtherRecovery_Field）；
// 回复量 RAND(0.9p, 1.1p)（_MAGIC_REHPAI 开着，不乘 GetRecoveryRate）；MAGIC_Metamo（变幻的精灵）同变身镜（itemuse.go metamo）。
func (w *World) MagicUse(charID, slot, target int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || e.busy() {
			return
		}
		m, mp := w.equipMagic(e, slot)
		if m == nil || m.Field == 1 || target < 0 || target >= 6+partyMax {
			return
		}
		var pet *Pet
		who := e // 6..10 是队伍的第几格
		switch {
		case target >= 6:
			if who = w.partyAt(e, target-6); who == nil || who.fight != nil {
				return
			}
		case target > 0:
			if pet = e.pets.Pets[target-1]; pet == nil {
				return
			}
		}
		c := &e.char
		switch m.Func {
		case "MAGIC_Recovery":
			pet, who = nil, e // 只回复自己
		case "MAGIC_OtherRecovery":
		case "MAGIC_Metamo":
			if who != e {
				return
			}
			// 变幻的精灵（MAGIC_Metamo）：变成自己的宠物 N 秒，对自己用是变回来；条件同变身镜，成立才扣 MP
			if c.MP >= mp && w.metamo(e, pet, data.Atoi(m.Option)) {
				c.MP -= mp
				w.sendStatus(e)
				w.saveProgress(e)
			}
			return
		default:
			return
		}
		if c.MP < mp {
			return
		}
		c.MP -= mp
		if maxHP, _ := w.maxHPMP(e); c.HP < 0 {
			c.HP = maxHP // -1 是满血
		}
		p := float64(data.Atoi(strings.Fields(m.Option + " 0")[0]))
		heal := int(p*0.9 + w.rnd.Float64()*(p*1.1-p*0.9))
		if pet != nil {
			before := pet.HP
			pet.HP = min(w.petMaxHP(pet), pet.HP+heal)
			w.sysMsg(e, fmt.Sprintf("%s的耐久力回复%d", pet.displayName(), pet.HP-before), colorWhite)
			w.petsChanged(e)
		} else {
			tc := &who.char
			maxHP, _ := w.maxHPMP(who)
			if tc.HP < 0 {
				tc.HP = maxHP
			}
			before := tc.HP
			tc.HP = min(maxHP, tc.HP+heal)
			w.sysMsg(e, fmt.Sprintf("耐久力回复%d", tc.HP-before), colorWhite)
		}
		w.sendStatus(e)
		w.saveProgress(e)
		if who != e {
			w.sendStatus(who)
			w.saveProgress(who)
		}
	})
}

// itemTargetValid 是 ITEM_isTargetValid：0..19 都行；20 是"我方全体"（自己在 0 边）或"对方全体"（自己在 1 边），21 相反；22 要"全体"。
// 道具的目标栏 100 以上表示可以对倒下的人用，种类看 %100。
func itemTargetValid(def *data.ItemDef, to, mySlot int) bool {
	t, side := def.Target%100, mySlot/10
	switch {
	case to >= 0 && to <= 19:
		return true
	case to == 20:
		return t == 2 && side == 0 || t == 3 && side == 1
	case to == 21:
		return t == 2 && side == 1 || t == 3 && side == 0
	case to == 22:
		return t == 4
	}
	return false
}

// attSkill 是 MAGIC_AttSkill（_ITEM_ATTSKILLMAGIC）：咒术参数 "PETSKILL_GuardBreak;3" 是一个宠物技能，人照宠物的方式用它；
// 下指令时就扣 MP（目标种类是"点哪边就是哪边"的不检查 MP），MP 不够或不是单人目标时这回合什么也不做。
func (w *World) attSkill(e *entity, m *data.Magic, mp, target int) battle.Command {
	none := battle.Command{Kind: battle.None}
	_, id, ok := strings.Cut(m.Option, ";")
	sk := w.game.Battle.Skills[data.Atoi(id)]
	pf := e.fight.b.F[e.fight.slot]
	if !ok || sk == nil || target < 0 || target > 19 {
		return none
	}
	if pf.MP < mp && m.Target != data.MagicTargetWholeOtherSide {
		return none
	}
	pf.MP = max(0, pf.MP-mp)
	return battle.Command{Kind: battle.Attack, Target: target, Skill: sk}
}
