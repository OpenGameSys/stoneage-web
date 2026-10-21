package world

import (
	"math"

	"sa/server/internal/battle"
	"sa/server/internal/proto"
)

// petExt 是宠物窗口的扩展资料（额外新功能，原版客户端看不到这些；类似外挂的"算档"）：
//
//   - 档次（CHAR_PETRANK，决定升级时成长倍率的范围 rankRand）、成长率（CHAR_ALLOCPOINT）与模板的标准成长率（E_T 的四项）；
//   - 预计每级成长：CHAR_PetLevelUp 每级加 (成长率 + 平均 2.5 点随机) × 档次倍率的中间值，换算成攻防敏与 HP；
//   - 实际每级成长：捕获以后（GetLevel → 现在）平均每级涨了多少；
//   - 1 级时的数值：ENEMY_createEnemy 的 初始成长次数 × 成长率（捕获前的部分按等级倒推）。
//
// 成长的数值都是 ×100 的整数（客户端显示两位小数），顺序是攻、防、敏、HP；都不含身上道具的加成。
func (w *World) petExt(p *Pet) proto.PetExt {
	x := proto.PetExt{Rank: p.Rank, GetLevel: p.GetLevel, EnemyID: p.EnemyID, Alloc: p.Alloc[:], Std: []int{},
		Grow: []int{}, Real: []int{}, Init: []int{}, Resist: p.Resist[:], Critical: p.Critical, Counter: p.Counter,
		Rare: p.Rare, Vai: p.VariableAI / 100, Magic: []int{}}
	if p.Mag != nil {
		x.Magic = append(append(x.Magic, p.Mag.Lv[:]...), p.Mag.Res[:]...)
	}
	rank := p.Rank
	if rank < 0 || rank > 5 {
		rank = 0
	}
	f := float64(rankRand[rank][0]+rankRand[rank][1]) / 2 * 0.01
	var per [4]float64
	for k := range per {
		per[k] = (float64(p.Alloc[k]) + 2.5) * f
	}
	x.Grow = derive100(per)

	if w.game.Battle == nil {
		return x
	}
	t := w.game.Battle.Temps[p.TempNo]
	if t == nil {
		return x
	}
	x.Std = t.Base[:]
	// 捕获时的体腕耐敏：新抓的宠物记在 Born；旧存档没有，按成长率 + 平均 2.5 点随机推算
	getLv := max(1, p.GetLevel)
	grow0 := float64((getLv-1)*t.LvUpPoint + t.InitNum)
	var born [4]float64
	for k := range born {
		if p.Born != nil {
			born[k] = float64(p.Born[k])
		} else {
			born[k] = grow0 * (float64(p.Alloc[k]) + 2.5)
		}
	}
	if grow0 > 0 {
		k := float64(t.InitNum) / grow0
		d := battle.Derive(born[0]*k, born[1]*k, born[2]*k, born[3]*k)
		x.Init = []int{d.Atk, d.Def, d.Quick, d.HP}
	}
	if n := p.Level - getLv; n > 0 {
		var gain [4]float64
		now := [4]int{p.Vital, p.Str, p.Tgh, p.Dex}
		for k := range gain {
			gain[k] = (float64(now[k]) - born[k]) / float64(n)
		}
		x.Real = derive100(gain)
	}
	return x
}

// derive100 是 battle.Derive 不取整的版本（×100），给每级成长用：攻、防、敏、HP。
func derive100(v [4]float64) []int {
	vit, str, tgh, dex := v[0], v[1], v[2], v[3]
	r := func(f float64) int { return int(math.Round(f)) }
	return []int{
		r(str + tgh*0.1 + vit*0.1 + dex*0.05),
		r(tgh + str*0.1 + vit*0.1 + dex*0.05),
		r(dex),
		r(vit*4 + str + tgh + dex),
	}
}
