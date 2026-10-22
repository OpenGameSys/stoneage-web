package world

import "time"

// Rules 是 config.yaml 的 rules 里可以调的游戏规则（cmd/saserver 在开机时设定，之后只读）。
// 预设值（DefaultRules）除了 ShareExp 都照原版。
type Rules struct {
	ExpRate       float64       // 战斗经验的倍率
	ShareExp      bool          // 经验共享（额外新功能）：战斗结束时还在场的人物、出战宠物、骑宠都拿一样多的经验；false 是原版的谁打倒谁拿
	DropRate      float64       // 战利品机率的倍率（原版是敌人资料里的千分比）
	EncounterRate float64       // 遇敌机率的倍率（0 不会遇敌）
	TurnTime      time.Duration // 每回合选指令的时间（原版 30 秒）
	LevelUpPoints int           // 人物每升一级的加点（原版 3）
	GroundItem    time.Duration // 丢在地上的道具多久消失（原版 20 秒）
	GroundPet     time.Duration // 丢在地上的宠物（原版 30 秒）
	GroundGold    time.Duration // 丢在地上的石币（原版 30 分钟）
	FamilyFee     int           // 成立家族的手续费
	// NoEscapeFloors 是 setup.cf 的 NOTESCAPE（_NOT_ESCAPE）：在这些楼层按逃跑会被"定身法"改成防御
	NoEscapeFloors []int
	// 世界频道（_THE_WORLD_SEND，setup.cf THEWORLDTRANS / THEWORLDLEVEL / THEWORLDSNED）：要几转、几级，
	// 每句扣多少声望（原版内部值，100 = 显示的 1 点）；两句之间至少隔 5 秒
	WorldTrans, WorldLevel, WorldFame int
	// BusStep 是长毛象巴士 / 加美航空每走一步的间隔（原版 NPC_BUS_LOOPTIME 200 ms）；客户端按这个节奏连续播走路动画，乘客跟着走
	BusStep time.Duration
	// VendorTax 是摆摊卖出时抽的税（百分比；setup.cf STREETVENDORPOINT 的第一项是石币摊位的税率，原版 0）
	VendorTax int
}

// DefaultRules 是原版的规则，另外经验共享预设开着。
func DefaultRules() Rules {
	return Rules{ExpRate: 1, ShareExp: true, DropRate: 1, EncounterRate: 1, TurnTime: TurnTime, LevelUpPoints: LevelUpPoints,
		GroundItem: itemDeleteTime, GroundPet: petDeleteTime, GroundGold: goldDeleteTime, FamilyFee: fmFee,
		NoEscapeFloors: []int{12345, 142, 40001, 40002, 40003, 40004, 40005, 40006, 40007, 40008, 40009, 40010, 8252, 8253, 8254, 8256},
		WorldTrans:     5, WorldLevel: MaxLevel, WorldFame: 1, BusStep: busLoopTime}
}

// scaleExp 乘上经验倍率（至少 1，打倒了就有经验）。
func (r Rules) scaleExp(exp int) int {
	if exp <= 0 || r.ExpRate == 1 {
		return exp
	}
	return max(1, int(float64(exp)*r.ExpRate))
}
