package world

import (
	"time"

	"sa/server/internal/battle"
	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 宠物在战斗里的世界层部分：捕获前的检查与捕获后变成宠物、宠物逃走、战斗结束时的经验与忠诚度。

// captureNeeds 是 battle_event.c 的 NeedEnemy（_CAPTURE_FREES，含 _WOLF_TAKE_AXE）：
// 这些敌人模板（CHAR_PETID）要身上同时带着表里的每一种道具才抓得到，抓到后那些道具全部收走。
var captureNeeds = map[int][]int{
	524: {2456}, 961: {20219}, 953: {20223}, 962: {20222}, 777: {20253}, 796: {20247}, 812: {20259},
	1105: {1690, 1691, 1692}, 8: {1810}, 145: {2236}, 146: {2236},
}

// canCapture 是 BATTLE_CaptureItemCheck 加上 PET_createPetFromCharaIndex 的宠物栏空位检查。
func (w *World) canCapture(e *entity, d *battle.Fighter) bool {
	if d.Seed == nil || e.pets.free() < 0 {
		return false
	}
	for _, id := range captureNeeds[d.Seed.TempNo] {
		found := false
		for _, it := range e.inv {
			found = found || it != nil && it.ID == id
		}
		if !found {
			return false
		}
	}
	return true
}

// afterTurn 处理一回合里影响宠物栏的事：
// 捕获成功（BATTLE_Capture）：敌人变成宠物放进宠物栏，忠诚度最多 CHAR_DEFAULTMAXAI，收走必要道具（BATTLE_CaptureItemDelAll）；
// 宠物逃走（BATTLE_LostEscape）：改为休息，主人魅力 −1；
// 换宠物（BATTLE_PetIn / PetOut）：收回的宠物按离开战斗结算（settleRecalled），叫出的宠物成为出战的宠物（CHAR_DEFAULTPET）。
func (w *World) afterTurn(e *entity, ev []battle.Event) {
	ft := e.fight
	pets, items := false, false
	for _, x := range ev {
		switch {
		case x.Kind == battle.PetIn && x.Actor == ft.slot:
			for i, pf := range ft.b.Recalled { // 收回的宠物（组队时别人的也在这里，按位置认自己的）
				if pf.Slot == ft.slot+5 {
					w.settleRecalled(e, ft, pf)
					ft.b.Recalled = append(ft.b.Recalled[:i:i], ft.b.Recalled[i+1:]...)
					break
				}
			}
			ft.pet, e.pets.Battle = -1, -1
			pets = true
		case x.Kind == battle.PetOut && x.Actor == ft.slot:
			ft.pet, e.pets.Battle = ft.swapTo, ft.swapTo
			pets = true
		case x.Kind == battle.Capture && x.OK && x.Actor == ft.slot:
			t := ft.b.F[x.Target]
			p := petFromFighter(t, e.char.ID)
			p.VariableAI = 0
			if ai := battle.DefaultMaxAI - w.petLoyalty(e, p); ai < 0 {
				p.addVariableAI(ai * 100)
			}
			if w.addPet(e, p) < 0 {
				continue
			}
			pets = true
			for _, id := range captureNeeds[t.Seed.TempNo] {
				for i, it := range e.inv {
					if it != nil && it.ID == id {
						e.inv[i], items = nil, true
					}
				}
			}
		case x.Kind == battle.Vary && x.Actor == ft.slot && x.Consume:
			// 战斗中的变身（ITEM_metamo / MAGIC_Metamo）：对自己是变回来，对出战的宠物是变成它 x.Damage 秒
			if x.Target == ft.slot || ft.pet < 0 || e.pets.Pets[ft.pet] == nil {
				e.metamo = nil
			} else {
				e.metamo = &metamoState{sprite: e.pets.Pets[ft.pet].Sprite, until: w.now().Add(time.Duration(x.Damage) * time.Second), body: e.inv[data.SlotBody]}
			}
		case x.Kind == battle.Flee && x.Actor == ft.slot+5:
			// 怯战 / 旅程伙伴 / 狂狮怒吼：宠物被赶回主人身边（BATTLE_PetDefaultExit，CHAR_DEFAULTPET = -1）
			e.pets.Battle = -1
			pets = true
		case x.Kind == battle.Steal && x.OK && x.Target == ft.slot:
			if w.stolen(e, string(x.Result)) {
				items = true
			}
		case x.Kind == battle.Leave && x.Actor == ft.slot+5:
			e.pets.Battle = -1
			e.char.Charm = max(0, min(100, e.char.Charm+battle.CHFixPetEscape))
			pets = true
		}
	}
	if items {
		w.itemsChanged(e)
	}
	if pets {
		w.petsChanged(e)
	}
}

// settlePet 在战斗结束时把出战宠物的状态写回宠物栏（任何结果都要做）：
// HP（倒下的回到 1，BATTLE_Exit）、这场累计的忠诚度变动（打主人的宠物、打倒敌人）、
// 倒下的惩罚（BATTLE_NormalDeadExtra：宠物倒下 AI_FIX_PETDEAD、主人倒下 AI_FIX_PLAYERDEAD，主人 10 级以下减半）。
func (w *World) settlePet(e *entity, ft *fight) {
	pf := ft.b.F[ft.slot+5]
	if ft.pet < 0 || pf == nil {
		return
	}
	p := e.pets.Pets[ft.pet]
	if p == nil {
		return
	}
	div := 1
	if e.char.Level <= 10 {
		div = 2
	}
	// BATTLE_AddExp：每打倒一只，敌人等级比宠物高时 +AI_FIX_PETGOLDWIN，否则 +AI_FIX_PETWIN
	for _, f := range petKills(ft) {
		if f.Level > pf.Level {
			pf.AIDelta += battle.AIFixPetGoldWin
		} else {
			pf.AIDelta += battle.AIFixPetWin
		}
	}
	p.addVariableAI(pf.AIDelta)
	if pf.HP <= 0 && !pf.Escaped && !ft.room.pvp { // 倒下的惩罚只有 P_vs_E
		p.addVariableAI(battle.AIFixPetDead / div)
	}
	if ft.b.F[ft.slot].HP <= 0 && !ft.room.pvp {
		p.addVariableAI(battle.AIFixPlayerDead / div)
	}
	p.HP = max(1, pf.HP)
}

// settleRecalled 是收回宠物时的 BATTLE_Exit：HP 写回（倒下的回到 1）、累计的忠诚度变动与倒下的惩罚；
// 打倒敌人的经验与忠诚度要到战斗结束时还在场才拿得到（BATTLE_AddExp），收回的宠物打倒的敌人不再算给这个位置；
// 经验共享时改算在主人名下（经验还是大家一起拿，忠诚度不给新叫出来的那只）。
func (w *World) settleRecalled(e *entity, ft *fight, pf *battle.Fighter) {
	for _, f := range ft.b.F {
		if f != nil && f.Enemy() && f.KilledBy == pf.Slot {
			f.KilledBy = -1
			if w.Rules.ShareExp {
				f.KilledBy = ft.slot
			}
		}
	}

	if ft.pet < 0 || e.pets.Pets[ft.pet] == nil {
		return
	}
	p := e.pets.Pets[ft.pet]
	p.addVariableAI(pf.AIDelta)
	if pf.HP <= 0 && !pf.Escaped {
		div := 1
		if e.char.Level <= 10 {
			div = 2
		}
		p.addVariableAI(battle.AIFixPetDead / div)
	}
	p.HP = max(1, pf.HP)
}

// petKills 是宠物打倒的敌人（BATTLE_AddExp 的 pBidList 是宠物时）。
func petKills(ft *fight) []*battle.Fighter {
	pf := ft.b.F[ft.slot+5]
	if ft.pet < 0 || pf == nil {
		return nil
	}
	var out []*battle.Fighter
	for _, f := range ft.b.F {
		if f != nil && f.Enemy() && f.HP <= 0 && f.KilledBy == pf.Slot {
			out = append(out, f)
		}
	}
	return out
}

// petKillExp 是 BATTLE_AddExp 里宠物的那一份：打倒的敌人的经验按宠物与敌人的等级差折算。
func (w *World) petKillExp(ft *fight) int {
	exp := 0
	for _, f := range petKills(ft) {
		exp += battle.KillExp(f.Exp, ft.b.F[ft.slot+5].Level, f.Level)
	}
	return exp
}

// petResult：战斗结束时出战宠物拿经验、升级，回传结果窗口的一行（宠物倒下时拿不到）。
func (w *World) petResult(e *entity, ft *fight, exp int) []proto.PetResult {
	pf := ft.b.F[ft.slot+5]
	if ft.pet < 0 || pf == nil || !pf.Alive() || e.pets.Pets[ft.pet] == nil {
		return []proto.PetResult{}
	}
	p := e.pets.Pets[ft.pet]
	from := p.Level
	ups := w.petGainExp(p, exp)
	w.petFeed(e, p, from)
	return []proto.PetResult{{Slot: ft.pet, Name: p.displayName(), Exp: exp, LevelUp: ups, Level: p.Level}}
}

// stolen 是偷窃得手（BATTLE_Steal）：偷钱扣 RAND(8,12)%，偷道具是随机一件没装备的道具（直接消失，偷的人拿不到）；
// 没钱 / 没道具时什么也没偷到。回传道具栏有没有变。
func (w *World) stolen(e *entity, what string) bool {
	if what == "gold" {
		if e.char.Gold <= 0 {
			return false
		}
		lose := e.char.Gold * (8 + w.rnd.IntN(5)) / 100
		e.char.Gold -= lose
		w.sysMsg(e, e.char.Name+"被偷了一些东西。", colorWhite)
		w.sendInventory(e)
		return false
	}
	var bag []int
	for i := data.EquipSlots; i < data.ItemSlots; i++ {
		if e.inv[i] != nil {
			bag = append(bag, i)
		}
	}
	if len(bag) == 0 {
		return false
	}
	e.inv[bag[w.rnd.IntN(len(bag))]] = nil
	w.sysMsg(e, e.char.Name+"被偷了一些东西。", colorWhite)
	return true
}
