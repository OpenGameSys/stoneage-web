package battle

import (
	"math/rand/v2"

	"sa/server/internal/data"
)

// EnemySpec 是一只要出场的敌人。
type EnemySpec struct {
	Enemy *data.Enemy
	Temp  *data.EnemyTemp
	Level int
}

// wpick 是原版按权重挑一项的写法：r = RAND(0, 总和-1)，依次累加，权重不为 0 且 r 落在累计值以内就选它，否则落到最后一项。
func wpick(rnd *rand.Rand, w []int) int {
	sum := 0
	for _, v := range w {
		sum += v
	}
	if len(w) == 0 {
		return -1
	}
	r := 0
	if sum > 0 {
		r = rnd.IntN(sum)
	}
	acc := 0
	for i := 0; i < len(w)-1; i++ {
		acc += w[i]
		if w[i] != 0 && r < acc {
			return i
		}
	}
	return len(w) - 1
}

// PickEnemies 是 ENEMY_getEnemy：先按权重选一支队伍，再决定出场数量（1..区域上限，且不超过队伍里各敌人上限之和），
// 然后按权重逐只抽敌人，同种敌人不超过它的 CreateMax。需要道具才出现的队伍不会被选到（还没有道具）。
func PickEnemies(bd *data.BattleData, area *data.EncountArea, rnd *rand.Rand) []EnemySpec {
	var groups []*data.Group
	var gw []int
	for i, gid := range area.Groups {
		g := bd.Groups[gid]
		if g == nil || g.AppearItem != -1 {
			continue
		}
		groups = append(groups, g)
		gw = append(gw, area.GroupProbs[i])
	}
	if len(groups) == 0 {
		return nil
	}
	g := groups[wpick(rnd, gw)]

	var es []*data.Enemy
	var ew []int
	total := 0
	for i, id := range g.Enemies {
		if alt, ok := randomEnemies[id]; ok {
			id = alt[rnd.IntN(len(alt))] // ENEMY_RandomEnemyArray：占位编号换成表里随机一种
		}
		e := bd.Enemies[id]
		if e == nil {
			continue
		}
		es = append(es, e)
		ew = append(ew, g.Probs[i])
		total += e.CreateMax
	}
	n := min(area.EnemyMax, total)
	if n < 1 {
		return nil
	}
	n = 1 + rnd.IntN(n)
	count := map[int]int{}
	var out []EnemySpec
	big := 0
	for loop := 0; len(out) < n && loop < 100; loop++ {
		e := es[wpick(rnd, ew)]
		same := 0
		for _, x := range es {
			if x == e {
				same++
			}
		}
		if count[e.ID] >= e.CreateMax*same {
			continue
		}
		t := bd.Temps[e.TempNo]
		spec := EnemySpec{Enemy: e, Temp: t, Level: e.LvMin + rnd.IntN(e.LvMax-e.LvMin+1)}
		if t.Size == 1 {
			// 大型敌人最多 5 只，而且只站前排：已经排到第 6 只以后时，和前五只里的一只普通敌人对调
			if big >= 5 {
				n--
				continue
			}
			if len(out) > 4 {
				j := -1
				for k := 0; k < 5; k++ {
					if out[k].Temp.Size != 1 {
						j = k
						break
					}
				}
				if j < 0 {
					continue
				}
				out = append(out, out[j])
				out[j] = spec
			} else {
				out = append(out, spec)
			}
			big++
		} else {
			out = append(out, spec)
		}
		count[e.ID]++
	}
	return out
}

// randomEnemies 是 enemy.c RandomEnemyTbl：这些敌人编号只是占位，出场时随机换成表里的一种。
var randomEnemies = func() map[int][]int {
	human := map[int][]int{945: {739, 742, 745, 748}, 947: {740, 743, 746, 749}, 949: {741, 744, 747, 750},
		951: {895, 898, 901, 904}, 953: {896, 899, 902, 905}, 955: {897, 900, 903, 906}}
	pa := []int{859, 860, 861, 868, 869, 870, 881, 882, 883, 884}
	pb := []int{864, 871, 872, 873, 885, 886, 889, 890, 891, 892, 893, 894}
	pc := []int{862, 863, 865, 866, 867, 874, 875, 876, 877, 878, 879, 880}
	pd := []int{912, 913, 914, 915, 916, 917, 918, 919, 920, 928}
	pe := []int{929, 930, 931, 932, 933, 934, 935, 936, 937, 938, 939, 940}
	pf := []int{907, 908, 909, 910, 911, 921, 922, 923, 924, 925, 926, 927}
	m := map[int][]int{946: pa, 964: pa, 948: pb, 965: pb, 950: pc, 966: pc, 952: pd, 967: pd, 954: pe, 968: pe, 956: pf, 969: pf}
	for k, v := range human {
		m[k] = v
	}
	return m
}()

// EnemySlot 把第 i 只出场的敌人放到位置上：原版先依次放进 Entry 0..9，再把前后两排对调，
// 所以前五只站前排（15..19），之后的站后排（10..14）。
func EnemySlot(i int) int {
	if i < 5 {
		return SideOffset + 5 + i
	}
	return SideOffset + i - 5
}

// NewEnemy 是 ENEMY_createEnemy + CHAR_complianceParameter：
// 成长率各自 ±2 随机，再随机加 10 点；基本状态 = ((等级-1) × 每级成长 + 初始成长) × 成长率（原版内部单位 ×100）。
func NewEnemy(s EnemySpec, slot int, skills map[int]*data.Skill, rnd *rand.Rand) *Fighter {
	t := s.Temp
	base := t.Base
	for k := range base {
		base[k] += rnd.IntN(5) - 2
	}
	alloc := base // CHAR_ALLOCPOINT 记的是加 10 点之前的成长率
	for i := 0; i < 10; i++ {
		base[rnd.IntN(4)]++
	}
	grow := float64((s.Level-1)*t.LvUpPoint + t.InitNum)
	vit, str, tgh, dex := grow*float64(base[0]), grow*float64(base[1]), grow*float64(base[2]), grow*float64(base[3])
	d := Derive(vit, str, tgh, dex)
	f := &Fighter{Slot: slot, Name: t.Name, Sprite: t.Image, Level: s.Level, HP: d.HP, MaxHP: d.HP,
		Atk: d.Atk, Def: d.Def, Quick: d.Quick, Attr: t.Attr, Tactics: s.Enemy.Tactics, Rare: max(0, t.Rare),
		Exp: data.EnemyExp(s.Enemy, t, s.Level), Base: [4]float64{vit, str, tgh, dex}, Big: t.Size == 1}
	for i := 1; i <= len(t.Status); i++ {
		f.Resist[i] = t.Status[i-1] // 模板的毒/麻/眠/石/醉/乱（E_T_POISON..）就是抗性（CHAR_WORKMODPOISON..）
	}
	for i, id := range t.Skills {
		if id >= 0 {
			f.Skills[i] = skills[id]
		}
	}
	f.Seed = &PetSeed{TempNo: t.TempNo, EnemyID: s.Enemy.ID, Alloc: alloc, Rank: data.EnemyRank(t), ModAI: t.ModAI,
		Slot: t.Slot, LimitLevel: t.LimitLevel, Skills: t.Skills, Critical: t.Critical, Counter: t.Counter,
		Capture: s.Enemy.PetFlag != 0, Get: t.Get}
	// ENEMY_createEnemy：身上的道具按千分比机率产生（_FIX_ITEMPROB：RAND(0,999) < 机率）
	for i, id := range s.Enemy.Items {
		if p := s.Enemy.ItemProbs[i]; p > 0 && id > 0 && rnd.IntN(1000) < p {
			f.Drops = append(f.Drops, id)
		}
	}
	f.Weapon = -1
	return f
}

// Stats 是由基本状态换算出的工作值。
type Stats struct{ HP, Atk, Def, Quick int }

// Derive 是 CHAR_initcharWorkInt：参数是原版内部单位（基本状态点数 × 100）。
//
//	最大 HP = (体力×4 + 腕力 + 耐力 + 敏捷) × 0.01
//	攻击力  = 腕力 + 耐力×0.1 + 体力×0.1 + 敏捷×0.05（× 0.01）
//	防御力  = 耐力 + 腕力×0.1 + 体力×0.1 + 敏捷×0.05（× 0.01）
//	速度    = 敏捷 × 0.01
func Derive(vit, str, tgh, dex float64) Stats {
	return Stats{
		HP:    int((vit*4 + str + tgh + dex) * 0.01),
		Atk:   int(str*0.01 + tgh*0.01*0.1 + vit*0.01*0.1 + dex*0.01*0.05),
		Def:   int(tgh*0.01 + str*0.01*0.1 + vit*0.01*0.1 + dex*0.01*0.05),
		Quick: int(dex * 0.01),
	}
}

// RollLuck 是 CHAR_setLuck：每个游戏日重抽一次运气，1（40%）2（30%）3（20%）4（7%）5（3%）。
func RollLuck(rnd *rand.Rand) int {
	r := rnd.IntN(100)
	for i, p := range []int{60, 30, 10, 3, 0} {
		if r >= p {
			return i + 1
		}
	}
	return 1
}

// KillExp 是打倒一只敌人得到的经验（battle.c 的经验分配）：
// 自己比敌人高 5 级以内拿全额；再高就按 (5 + 15 − 等级差) / 15 递减，至少 1。
func KillExp(exp, myLevel, enemyLevel int) int {
	const maxLevel, div = 5, 15 // EXPGET_MAXLEVEL / EXPGET_DIV
	b := myLevel - enemyLevel
	if b <= maxLevel {
		return exp
	}
	b = maxLevel + div - b
	if b > div {
		b = div
	}
	if b <= 0 {
		return 1
	}
	return max(1, exp*b/div)
}
