package battle

import "sa/server/internal/data"

// 骑宠在战斗中的规则（battle_event.c）：
//   BATTLE_adjustRidePet3A  攻击力：近战 人×0.8 + 宠×0.8，投掷（弓、回力镖、投掷类）人 + 宠×0.4；防御力 人×0.7 + 宠×0.7；
//                           速度（行动顺序）：近战 人×0.2 + 宠×0.8，投掷 人×0.8 + 宠×0.2。闪避、会心、反击用的还是人自己的敏捷。
//   BATTLE_DamageSub        挨打时按双方防御力分摊：人 = 伤害×宠防/(人防+宠防) + 1，宠 = 伤害 − 人 + 1；
//                           骑宠 HP 到 0 就落马（RIDEPET = -1、换回人的形象、WORKPETFALL）。
// 中毒 / 剧毒每回合（Compute_Down）：人与骑宠各按自己的基本状态扣，都扣不死（剧毒的阵亡只看人）。

// Mount 是骑着的宠物。
type Mount struct {
	Name            string
	Level           int
	HP, MaxHP       int
	Atk, Def, Quick int
	Fell            bool       // 这场战斗里落马了
	Attr            [4]int     // 骑宠的四属性（攻击魔法对骑乘者的属性取两者平均）
	Base            [4]float64 // 骑宠的四项基本状态内部值（中毒扣血用）
}

// riding：还骑着（骑宠 HP 大于 0）。
func (f *Fighter) riding() bool { return f.Ride != nil && f.Ride.HP > 0 && !f.Ride.Fell }

func (f *Fighter) rideHP() int {
	if f.Ride == nil {
		return 0
	}
	return f.Ride.HP
}

// throwing 是 BATTLE_IsThrowWepon：弓、回力镖、投掷类。
func (f *Fighter) throwing() bool {
	switch f.Weapon {
	case data.ItemBow, data.ItemBoomerang, data.ItemBreakThrow, data.ItemBoundThrow:
		return true
	}
	return false
}

func (f *Fighter) rideAtk() float64 {
	if !f.riding() {
		return float64(f.Atk)
	}
	if f.throwing() {
		return float64(f.Atk) + float64(f.Ride.Atk)*0.4
	}
	return float64(f.Atk)*0.8 + float64(f.Ride.Atk)*0.8
}

func (f *Fighter) rideDef() float64 {
	if !f.riding() {
		return float64(f.Def)
	}
	return float64(f.Def)*0.7 + float64(f.Ride.Def)*0.7
}

// rideQuick 是行动顺序用的速度（BATTLE_DexCalc 的 ATTACKSIDE）。
func (f *Fighter) rideQuick() float64 {
	if !f.riding() {
		return float64(f.Quick)
	}
	if f.throwing() {
		return float64(f.Quick)*0.8 + float64(f.Ride.Quick)*0.2
	}
	return float64(f.Quick)*0.2 + float64(f.Ride.Quick)*0.8
}

// rideSplit 是 BATTLE_DamageSub 的分摊：*dmg 改成人分到的伤害，回传骑宠分到的伤害与是否因此落马。
func (f *Fighter) rideSplit(dmg *int) (int, bool) {
	if !f.riding() {
		return 0, false
	}
	rd, pd := max(1, f.Def), max(1, f.Ride.Def)
	player := *dmg*pd/(rd+pd) + 1
	pet := *dmg - player + 1
	*dmg = player
	f.Ride.HP -= pet
	if f.Ride.HP <= 0 {
		f.Ride.HP, f.Ride.Fell = 0, true
		return pet, true
	}
	return pet, false
}
