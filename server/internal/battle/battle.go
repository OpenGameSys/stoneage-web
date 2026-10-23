// Package battle 是回合制战斗的规则核心，不涉及网络与存储，按原版 gmsv/battle 的语义实现：
//
//	行动顺序      battle.c BATTLE_DexCalc / EntrySort
//	普通攻击      battle_event.c BATTLE_AttackSeq：闪避 → 守护 → 会心 → 伤害 → 防御减伤
//	伤害          BATTLE_DamageCalc（_BATTLE_NEWPOWER：防御力 × 0.70；_NPCENEMY_ADDPOWER；属性相克；setup.cf dammagecalc）
//	闪避 / 会心   BATTLE_DuckCheck / BATTLE_CriticalCheckPlayer
//	防御          BATTLE_GuardAdjust
//	连击          battle.c：10 级以上空手的玩家按运气 1/2/3/5..10 下
//	反击          BATTLE_Counter：攻击结束后双方最多轮流反击 5 次，伤害 × 0.75
//	异常状态      BATTLE_StatusAttackCheck（附加机率）与行动前的状态处理（中毒扣血、麻痹/睡眠/石化不能动、混乱乱打）
//	逃跑          BATTLE_EscapeCheck
//	偷袭          BATTLE_SurpriseCheck
//	敌人行动      battle_ai.c BATTLE_ai_normal（at/gu/es/wa 权重），技能见 skill.go
//
// 一场战斗最多 20 个位置：0..4 己方人物、5..9 己方宠物、10..14 敌方后排、15..19 敌方前排（与客户端 p_party 下标一致）。
package battle

import (
	"math"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"

	"sa/server/internal/data"
)

const (
	Slots      = 20
	SideOffset = 10 // 敌方位置从 10 开始（原版 SIDE_OFFSET）

	// DamageCalcRate 是 setup.cf 的 dammagecalc（_DAMMAGE_CALC：最终伤害 × rate / 100）。
	DamageCalcRate = 70
	duckMaxRate    = 75   // KAWASHI_MAX_RATE
	kawashiPara    = 0.02 // gKawashiPara
	criticalPara   = 0.09 // gCriticalPara
	counterPara    = 0.08 // gCounterPara
)

// 异常状态编号（battle_event.c aszStatus / StatusTbl）。
const (
	StNone = iota
	StPoison
	StParalysis
	StSleep
	StStone
	StDrunk
	StConfusion
	StWeaken     // 虚弱（WORKWEAKEN）：攻防速 ×0.8
	StDeepPoison // 剧毒（WORKDEEPPOISON）：像中毒一样扣血，回合数到了就阵亡
	StBarrier    // 魔障（WORKBARRIER）：不能行动、不能守护
	StNocast     // 沉默（WORKNOCAST）：不能吟唱咒术
	stCount
)

// statusChar 是 battle_event.c aszStatus 的顺序（全 毒 麻 眠 石 醉 乱 虚 剧 障 默），下标 0 的"全"表示全部
var statusChar = []string{"全", "毒", "麻", "眠", "石", "醉", "乱", "虚", "剧", "障", "默"}

// statusOf 找出参数里写的是哪一种状态（最先出现的那个字），找不到回 -1。
func statusOf(opt string) int {
	best, at := -1, len(opt)+1
	for i, c := range statusChar {
		if k := strings.Index(opt, c); k >= 0 && k < at {
			best, at = i, k
		}
	}
	return best
}

// Fighter 是战斗中的一个角色。数值都是原版"工作值"（CHAR_WORK*），已从基本状态换算好。
type Fighter struct {
	Slot   int
	Name   string
	Sprite int // 战斗形象（动画编号）
	Level  int
	HP     int
	MaxHP  int
	MP     int
	MaxMP  int
	Atk    int    // CHAR_WORKATTACKPOWER；每回合从 FixAtk 重置，技能可临时改动
	Def    int    // CHAR_WORKDEFENCEPOWER
	Quick  int    // CHAR_WORKQUICK
	Attr   [4]int // 地水火风 0..100
	Luck   int    // 玩家的运气 1..5（敌人为 0）
	Player bool
	Pet    bool // 玩家带出场的宠物（CHAR_TYPEPET），站在主人位置 +5；Player 与 Pet 都不是的就是敌人
	CharID int  // 玩家的角色 ID
	Charm  int  // 玩家的魅力（CHAR_WORKFIXCHARM，捕获机率用）
	// Loyalty 是宠物的忠诚度 0..100（CHAR_WORKFIXAI），决定会不会听指令（BATTLE_PetLoyalCheck）
	Loyalty int
	// AIDelta 累计这场战斗里宠物忠诚度变动值（CHAR_PetAddVariableAi 的点数，×100 为 1 点），战斗结束时由世界层加到宠物上
	AIDelta int
	// Seed 是敌人变成宠物时要带走的资料（敌人才有）；Captured 表示已被捕获（离场，不给经验）
	Seed     *PetSeed
	Captured bool
	// KilledBy 是打倒这个人的位置（-1 为还没倒下），经验给打倒它的人（BATTLE_AddExp 的 pBidList）；
	// KilledRide 表示打倒的人那时骑着宠物（骑宠分到 6 成经验）
	KilledBy   int
	KilledRide bool
	// Ride 是玩家骑着的宠物（BATTLE_getRidePet）：不占位置，攻防速并进骑乘者，挨打时两边分摊伤害（ride.go）
	Ride *Mount
	// Mag 是魔法熟练度与抗魔（玩家与宠物，指向世界层的资料，战斗中直接改；敌人为 nil，按等级算）
	Mag *MagicSkill
	// 魔法状态（MAGIC_MagicStatusChange：魔抗 / 铁壁…）：种类、剩下的回合、强度 %
	magStatus, magTurns, magN int
	recovered                 bool // 这场已经因被回复加过忠诚度（BATTLEFLG_RECOVERY）
	// Base 是四项基本状态的原版内部值（点数 × 100），异常状态的抗性要用体力占比。
	Base [4]float64
	// Resist 是异常状态抗性（敌人模板的毒/麻/眠/石/醉/乱，下标同 St*；玩家为 0）。
	Resist [stCount]int
	// Absorb / Reflect / Vanish 是光、镜、守的精灵（MAGIC_MagicDef）剩下的次数：受到物理攻击时吸收、反射、无效
	Absorb, Reflect, Vanish int
	// reversed 是彩虹 / 极光的精灵（MAGIC_AttReverse，BATTLEFLG_REVERSE）：地↔火、水↔风对调
	reversed bool

	// 敌人专用
	Tactics string // 行动方针（enemy.txt 第 2 栏）
	Skills  [7]*data.Skill
	Rare    int
	Exp     int // 打倒后给的经验
	Big     bool
	Drops   []int // 身上的道具（打倒后成为战利品），产生敌人时按机率决定

	// 装备的武器（data.Item* 种类，-1 为空手）与攻击次数（ITEM_ATTACKNUM_MIN/MAX）
	Weapon               int
	AttackMin, AttackMax int

	Escaped bool

	FixAtk, FixDef, FixQuick int // 不含临时加成的工作值（WORKFIXSTR / FIXTOUGH / FIXDEX）
	fixed                    bool

	escapes int  // 逃跑失败次数（原版 Entry.escape）
	guard   bool // 本回合防御中
	com     Kind // 本回合的行动（反击只在攻击或不防守时发生）
	status  [stCount]int
	duckMod int // 本回合被攻击时的闪避加成（不防守战法）
	ctrMod  int // 本回合的反击加成（不防守战法）
	charge  int // 蓄力剩余回合；-1 表示没在蓄力
	chargeP int // 蓄力完成后的攻击力加成 %
	hidden  bool
	earthP  int // 地球一周：下回合的伤害加成 %
	// CaptureUp 是捕获机率的加成（ITEM_useCaptureUp，WORKMODCAPTURE），捕捉一次后归零
	CaptureUp int
	// TempNo 是宠物的模板编号（CHAR_PETID，鞭炮用；敌人的在 Seed 里）；Pig 是乌力化剩下的秒数（玩家，世界层带进带出）；
	// BaseSprite 是没变身时的形象（变回自己用）
	TempNo, Pig, BaseSprite int
	fox, foxSave            int       // 媚惑术：变成小狐狸的回合（0 没有）、原来的形象
	imprecate               [3][2]int // 咒 / 恩 / 祝：每回合的量、剩下的次数（item.go）
	// skill2.go 的持续效果
	dizzy            int    // 晕眩（憾甲一击，CHAR_WORKDIZZY）：剩下的行动次数，> 0 不能行动
	skillDuck, duckP int    // 闪避术（MYSKILLDUCK / DUCKPOWER）：剩下的回合、自动闪避的机率
	dexTurns, dexP   int    // 束缚 / 风之吹息（MYSKILLDEX）：剩下的回合、敏捷加减（耐力的 %）
	vary             int    // 暗月变身：变身中剩下的行动次数
	varySave         [4]int // 变身前的 FixAtk / FixDef / FixQuick / Sprite
}

func (f *Fighter) Side() int { return f.Slot / SideOffset }

// Enemy：敌人（CHAR_TYPEENEMY），既不是玩家也不是宠物。
func (f *Fighter) Enemy() bool { return !f.Player && !f.Pet }

// PetSeed 是 ENEMY_createEnemy 存在敌人身上、捕获时 PET_createPetFromCharaIndex 照抄给宠物的资料。
type PetSeed struct {
	TempNo, EnemyID int    // CHAR_PETID（模板编号）与 CHAR_PETENEMYID
	Alloc           [4]int // CHAR_ALLOCPOINT：成长率 ±2 之后、随机加 10 点之前的体腕耐敏（宠物升级用）
	Rank            int    // CHAR_PETRANK（ENEMY_getRank）
	ModAI           int    // CHAR_MODAI
	Slot            int    // CHAR_SLOT：技能栏数
	LimitLevel      int    // CHAR_LIMITLEVEL
	Skills          [7]int // 技能编号（-1 为空）
	Critical        int
	Counter         int
	Capture         bool // ENEMY_PETFLG：可以捕获
	Get             int  // E_T_GET（CHAR_WORKMODCAPTUREDEFAULT）
}

// Alive：还在场上（没倒下也没逃走）。
func (f *Fighter) Alive() bool { return f != nil && f.HP > 0 && !f.Escaped }

// Status 返回当前的异常状态（同时只会有一种，原版附加时已有状态就不会再附加）。
func (f *Fighter) Status() int {
	for i := 1; i < stCount; i++ {
		if f.status[i] > 0 {
			return i
		}
	}
	return StNone
}

func (f *Fighter) canMove() bool {
	return f.status[StParalysis] <= 0 && f.status[StStone] <= 0 && f.status[StSleep] <= 0 && f.status[StBarrier] <= 0 && f.dizzy <= 0
}

// attrs 是计算属性相克用的四属性：属性反转时地↔火、水↔风对调。
func (f *Fighter) attrs() [4]int {
	a := f.Attr
	if f.reversed {
		a[0], a[2], a[1], a[3] = a[2], a[0], a[3], a[1]
	}
	return a
}

// Kind 是指令种类。
type Kind string

const (
	Attack  Kind = "attack"
	Guard   Kind = "guard"
	Escape  Kind = "escape"
	None    Kind = "none" // 时间到、无法行动或待机
	Counter Kind = "counter"
	Charge  Kind = "charge" // 蓄力中
	Hide    Kind = "hide"   // 地球一周：钻进地底
	Heal    Kind = "heal"
	Revive  Kind = "revive"
	Poison  Kind = "poison" // 中毒扣血
	Cure    Kind = "cure"   // 异常状态解除
	Stun    Kind = "stun"   // 因麻痹/睡眠/石化不能行动
	NoGuard Kind = "noguard"
	UseItem Kind = "item"
	Capture Kind = "capture" // 捕获（BATTLE_Capture），OK 表示成功
	Disobey Kind = "disobey" // 宠物不听指令（BATTLE_PetLoyalCheck 的 "BX"），之后照改过的行动做
	Leave   Kind = "leave"   // 宠物忠诚度太低逃走（BATTLE_LostEscape），回到主人身边休息
	Magic   Kind = "magic"   // 咒术（magic.go）：一个目标一个事件，同一次施法的事件 Combo 相连
	// FieldAttr 是战场属性改变（"BV"）：Status 是新的属性 0 无 1 地 2 水 3 火 4 风；Actor -1 表示回合数到了回到无属性
	FieldAttr Kind = "field"
	// 换宠物（lssproto_B "S|n"，BATTLE_COM_PETIN / PETOUT）：玩家的行动。PetIn 收回 Target 位置的宠物，
	// PetOut 叫出新的宠物到 Target（事件带着它的名字、图、等级、HP），新宠物这回合不行动
	PetSwap Kind = "pet" // 玩家的指令
	PetIn   Kind = "petin"
	PetOut  Kind = "petout"
)

type Command struct {
	Kind   Kind
	Target int
	Skill  *data.Skill // 敌人使用的技能（nil 为普通行动）

	// 使用道具（item.go）：道具的 usefunc、参数、名字、在道具栏的格位，以及道具带的咒术（光镜守的种类）
	ItemFunc, ItemArg, ItemName string
	ItemSlot                    int
	ItemMagic                   *data.Magic
	FromItem                    bool // 道具借用咒术的执行（不做 _PREVENT_TEAMATTACK 的检查）

	// 咒术（Kind 为 Magic，magic.go）：用哪一个、要扣的 MP（敌人与宠物技能为 0）
	Magic *data.Magic
	MP    int

	// 换宠物（Kind 为 PetSwap）：叫出的宠物（世界层准备好，nil 为只收回）
	Swap *Fighter
}

// Result 是一次攻击的结果。
type Result string

const (
	Normal   Result = "normal"
	Critical Result = "critical"
	Dodge    Result = "dodge"
	Miss     Result = "miss"
	AllGuard Result = "guard" // 防御把伤害全挡掉
	// 守 / 光 / 镜的精灵：攻击无效、伤害变成回复、伤害反射给攻击者（Damage 是被反射的伤害，ActorHP 是攻击者剩下的 HP）
	Vanished  Result = "vanish"
	Absorbed  Result = "absorb"
	Reflected Result = "reflect"
)

// Event 是回合中按顺序发生的一个行动，客户端照此播放动画。
type Event struct {
	Actor    int
	Kind     Kind
	Target   int
	Result   Result
	Damage   int
	HP       int    // 目标（无目标时为本人）行动后的 HP
	Dead     bool   // 目标因此倒下
	OK       bool   // 逃跑是否成功
	Skill    string // 使用的技能名
	Status   int    // 目标（无目标时为本人）行动后的异常状态
	Guardian int    // 替目标挡下攻击的守护者位置，-1 为无
	Combo    bool   // 同一个人接下来还有一下（连击），不用走回原位
	ActorHP  int    // 行动者行动后的 HP（吸血）
	MPDamage int
	Guarded  bool // 目标处于防御（客户端的命中特效用蓝色）
	// 目标骑着宠物时：骑宠分到的伤害、骑宠剩下的 HP、骑宠因此倒下而退出战斗（落马）
	PetDamage int
	RideHP    int
	Fall      bool
	// 咒术：目标身上的效果动画、施法者剩下的 MP
	Effect  int
	ActorMP int
	// 道具用掉了（item.go，不送给客户端）：Consume 一个、ConsumeAll 整叠
	Consume, ConsumeAll bool `json:"-"`
	// 攻击魔法放动画的位置（attmagic.bin）："攻击方式|时间差|显示方式|x|y"
	Att string
	// 叫出宠物（PetOut）：新宠物的资料
	Name   string
	Sprite int
	Level  int
	MaxHP  int
}

// Battle 是一场战斗的状态。
type Battle struct {
	F    [Slots]*Fighter
	Turn int
	Rnd  *rand.Rand
	// PvP 是玩家对玩家（BATTLE_TYPE_P_vs_P）：逃跑一定成功、复活不能用在玩家身上、可以回复对方与对自己人附加状态、爆裂攻击生效
	PvP bool
	// CanCapture 是捕获前世界层的检查（BATTLE_CaptureItemCheck 的必要道具、宠物栏有空位），nil 为都可以
	CanCapture func(a, d *Fighter) bool
	// Surprised 是被偷袭的一方（第一回合不能行动），-1 为无（BATTLE_SurpriseCheck）
	Surprised int
	guardian  map[int]int // 被守护者位置 → 守护者位置（本回合）
	// 战场属性（MAGIC_FieldAttChange）：fieldAttr 0 无、1 地、2 水、3 火、4 风，fieldPow 强度，fieldTurns 剩下的回合
	fieldAttr, fieldPow, fieldTurns int
	// AttMagics 是攻击魔法的范围表（attmagic.bin）；Notice 把讯息送给某个位置的玩家（魔法熟练度、沉默中无法吟唱…）
	AttMagics []data.AttMagic
	Magics    map[int]*data.Magic // magic.txt（技能用的咒术）
	Notice    func(slot int, text string)
	// NoAbduct：NPC 开的战斗，旅程伙伴带不走人（skill2.go abduct）
	NoAbduct bool
	// OnHit 是每一下有伤害的攻击（BATTLE_ItemCrushSeq：装备磨损由世界层做）；OnTooth 是 E啮齿术打中玩家（BATTLE_S_ToothCrushe）
	OnHit   func(a, d *Fighter, dmg int)
	OnTooth func(a, d *Fighter)
	// Recalled 是换宠物时收回的宠物（petSwap），世界层在回合结束后结算
	Recalled []*Fighter
}

func New(rnd *rand.Rand) *Battle { return &Battle{Rnd: rnd, Surprised: -1, guardian: map[int]int{}} }

func (b *Battle) Add(f *Fighter) {
	if !f.fixed {
		f.FixAtk, f.FixDef, f.FixQuick, f.fixed = f.Atk, f.Def, f.Quick, true
	}
	f.charge = -1
	f.KilledBy = -1
	b.F[f.Slot] = f
}

// rand 是原版的 RAND(x, y) 宏：x + (int)((y - x + 1) × [0,1))，参数可以是小数。
func (b *Battle) rand(x, y float64) float64 {
	if y < x {
		return x
	}
	return x + float64(int((y-x+1)*b.Rnd.Float64()))
}

func (b *Battle) randi(x, y int) int { return int(b.rand(float64(x), float64(y))) }

// alive 返回某一方还在场上的位置。
func (b *Battle) alive(side int) []int {
	var out []int
	for i := side * SideOffset; i < (side+1)*SideOffset; i++ {
		if b.F[i].Alive() {
			out = append(out, i)
		}
	}
	return out
}

// targetable：还在场上、且没有钻进地底。
func (b *Battle) targetable(side int) []int {
	var out []int
	for _, i := range b.alive(side) {
		if !b.F[i].hidden {
			out = append(out, i)
		}
	}
	return out
}

// Surprise 是 BATTLE_SurpriseCheck：按己方第一个人的运气，1 己方偷袭成功（敌人第一回合不动），2 被敌人偷袭。
func (b *Battle) Surprise(luck int) int {
	a, c := 0, 7
	switch luck {
	case 5:
		a, c = 20, 0
	case 4:
		a, c = 15, 2
	case 3:
		a, c = 10, 3
	case 2:
		a, c = 5, 5
	}
	r := b.randi(1, 100)
	switch {
	case r <= a:
		b.Surprised = 1
		return 1
	case r < a+c:
		b.Surprised = 0
		return 2
	}
	return 0
}

// Over 返回战斗是否结束及胜方（0 己方、1 敌方；双方都没人时视作己方没赢，返回 1）。
func (b *Battle) Over() (bool, int) {
	if len(b.alive(1)) == 0 {
		if len(b.alive(0)) > 0 {
			return true, 0
		}
		return true, 1
	}
	if len(b.alive(0)) == 0 {
		return true, 1
	}
	return false, -1
}

// Resolve 执行一回合：玩家的指令由 cmds 给出（没有的当作 None），敌人按行动方针决定，然后按速度依次行动。
func (b *Battle) Resolve(cmds map[int]Command) []Event {
	b.Turn++
	b.guardian = map[int]int{}
	all := map[int]Command{}
	for i, f := range b.F {
		if !f.Alive() {
			continue
		}
		// 每回合把工作值恢复成基本值（技能的加减只在当回合有效）；酒醉时速度减半
		f.Atk, f.Def, f.Quick = f.FixAtk, f.FixDef, f.FixQuick
		if f.status[StWeaken] > 0 { // 虚弱：攻防速 ×0.8（Other_DefcharWorkInt）
			f.Atk, f.Def, f.Quick = f.Atk*8/10, f.Def*8/10, f.Quick*8/10
		}
		if f.status[StDrunk] > 0 {
			f.Quick /= 2
		}
		f.guard, f.duckMod, f.ctrMod = false, 0, 0
		var c Command
		switch {
		case b.Turn == 1 && f.Side() == b.Surprised:
			c = Command{Kind: None}
		case f.charge >= 0 || f.hidden:
			c = Command{Kind: Attack, Target: -1} // 蓄力中 / 地底：不重新选指令（BATTLE_IsCharge）
		case f.Player || f.Pet:
			var ok bool
			if c, ok = cmds[i]; !ok {
				c = Command{Kind: None}
			}
		default:
			c = b.enemyAI(f)
		}
		all[i] = b.setup(f, c)
	}
	// 行动顺序：(速度 + 20) − RAND(0, 10%)，大的先动
	type ord struct {
		slot, dex int
		f         *Fighter // 下指令的人（换宠物后同一个位置换了人，新来的这回合不行动）
	}
	var order []ord
	for i, c := range all {
		w := b.F[i].rideQuick() + 20
		dex := w - b.rand(0, w*0.1)
		if c.Kind == UseItem {
			dex += w * 0.15 // BATTLE_DexCalc：用道具比较快
		}
		if c.Skill != nil {
			switch c.Skill.Func {
			case "PETSKILL_SpeedyAttack":
				dex = w * 1.3 // BATTLE_DexCalc：疾速攻击固定 ×1.3（不扣随机）
			case "PETSKILL_DamageToHp2":
				dex = w * 1.2
			}
		}
		order = append(order, ord{i, int(dex), b.F[i]})
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].dex != order[j].dex {
			return order[i].dex > order[j].dex
		}
		return order[i].slot < order[j].slot
	})

	var ev []Event
	for _, o := range order {
		f := b.F[o.slot]
		if f != o.f || !f.Alive() {
			continue
		}
		if done, _ := b.Over(); done {
			break
		}
		c, ok := b.statusSeq(f, all[o.slot], &ev)
		if !ok || !f.Alive() {
			continue
		}
		if f.Pet && !(b.Turn == 1 && b.Surprised == 1-f.Side()) { // 偷袭成功的那一方第一回合不判定（BSIDE_FLG_SURPRISE）
			if c, ok = b.loyalCheck(f, c, &ev); !ok {
				continue
			}
		}
		b.act(f, c, &ev)
		b.afterAct(f, &ev)
	}
	b.tickMagicStatus()
	// 战场属性的回合数：到了就回到无属性（"BV|15|0|"）
	if b.fieldTurns > 0 {
		if b.fieldTurns--; b.fieldTurns == 0 {
			b.fieldAttr = 0
			ev = append(ev, Event{Actor: -1, Kind: FieldAttr, Target: -1, Guardian: -1})
		}
	}
	return ev
}

// statusSeq 是行动前的状态处理（battle.c）：不能动的改为无行动；各状态回合数 −1，到 0 解除；
// 中毒扣血（不会扣死）；混乱时 80% 改成随机攻击任意一方的一个人。返回 false 表示这次不行动。
func (b *Battle) statusSeq(f *Fighter, c Command, ev *[]Event) (Command, bool) {
	b.tickImprecate(f, ev)
	stunned := !f.canMove()
	stunStatus := f.Status()
	if f.dizzy > 0 {
		f.dizzy--
	}
	// BATTLE_StatusSeq 最后：闪避术、技能加减的回合数
	if f.skillDuck > 0 {
		f.skillDuck--
	}
	if f.dexTurns > 0 {
		if f.dexTurns--; f.dexTurns == 0 {
			f.dexP = 0
		}
	}
	for i := 1; i < stCount; i++ {
		if f.status[i] <= 0 {
			continue
		}
		f.status[i]--
		// 剧毒（battle.c）：回合数剩 1 以下或 HP 只剩 1 就阵亡（"身中剧毒未解而阵亡了!!"）
		if i == StDeepPoison && (f.status[i] <= 1 || f.HP <= 1) {
			f.status = [stCount]int{}
			f.HP, f.charge = 0, -1
			*ev = append(*ev, Event{Actor: f.Slot, Kind: Poison, Target: -1, HP: 0, Dead: true, Status: StNone, Guardian: -1, Skill: "剧毒"})
			return c, false
		}
		if f.status[i] <= 0 {
			*ev = append(*ev, Event{Actor: f.Slot, Kind: Cure, Target: -1, HP: f.HP, Status: f.Status(), Guardian: -1})
			continue
		}
		switch i {
		case StPoison, StDeepPoison:
			// Compute_Down：(四项基本状态和 / 100 − 20) / 4，至少 1，最多扣到剩 1；骑着宠物时骑宠按自己的状态另外扣（也扣不死）
			down := poisonDown(f.Base, f.HP)
			pet := 0
			if f.riding() {
				pet = poisonDown(f.Ride.Base, f.Ride.HP)
				f.Ride.HP -= pet
			}
			if down > 0 || pet > 0 {
				f.HP -= down
				*ev = append(*ev, Event{Actor: f.Slot, Kind: Poison, Target: -1, Damage: down, PetDamage: pet, HP: f.HP, Status: f.Status(),
					Guardian: -1, RideHP: f.rideHP()})
			}
		case StConfusion:
			if !stunned && b.randi(1, 100) <= 80 {
				side, pos := b.randi(0, 1), b.randi(0, 9)
				c = Command{Kind: Attack, Target: -1}
				for k := 0; k < SideOffset; k++ {
					if pos++; pos >= SideOffset {
						pos = 0
					}
					t := side*SideOffset + pos
					if t != f.Slot && b.F[t].Alive() && !b.F[t].hidden {
						c.Target = t
						break
					}
				}
			}
		}
	}
	if stunned {
		f.charge = -1
		*ev = append(*ev, Event{Actor: f.Slot, Kind: Stun, Target: -1, HP: f.HP, Status: stunStatus, Guardian: -1})
		return c, false
	}
	return c, true
}

// act 执行一个人这回合的行动。
func (b *Battle) act(f *Fighter, c Command, ev *[]Event) {
	name := ""
	if c.Skill != nil {
		name = c.Skill.Name
	}
	base := Event{Actor: f.Slot, Target: -1, HP: f.HP, Status: f.Status(), Guardian: -1, Skill: name}
	switch {
	case f.charge > 0:
		f.charge--
		e := base
		e.Kind = Charge
		*ev = append(*ev, e)
		return
	case f.charge == 0:
		f.charge = -1
		f.Atk = f.FixAtk + int(float64(f.FixAtk)*float64(f.chargeP)*0.01)
		b.attackSeq(f, c.Target, attackOpts{name: "突击"}, ev)
		return
	case f.hidden:
		f.hidden = false
		b.attackSeq(f, c.Target, attackOpts{name: name, dmgMod: 1 + 0.01*float64(f.earthP)}, ev)
		return
	}
	switch c.Kind {
	case Attack:
		b.skillAttack(f, c, ev)
	case Guard:
		e := base
		e.Kind = Guard
		*ev = append(*ev, e)
	case Escape:
		ok := b.escapeCheck(f)
		if ok {
			f.Escaped = true
			// BATTLE_Exit：玩家逃走时出场的宠物跟着离开
			if f.Player && f.Slot%SideOffset < 5 && b.F[f.Slot+5].Alive() {
				b.F[f.Slot+5].Escaped = true
			}
		} else {
			f.escapes++
		}
		e := base
		e.Kind, e.OK = Escape, ok
		*ev = append(*ev, e)
	case NoGuard:
		e := base
		e.Kind = NoGuard
		*ev = append(*ev, e)
	case Hide:
		f.hidden = true
		e := base
		e.Kind = Hide
		*ev = append(*ev, e)
	case Heal, Revive:
		b.support(f, c, ev)
	case UseItem:
		b.useItem(f, c, ev)
	case Magic:
		if c.Magic != nil {
			b.castMagic(f, c, ev)
		}
	case PetSwap:
		b.petSwap(f, c, ev)
	case Capture:
		b.capture(f, c.Target, ev)
	default:
		e := base
		e.Kind = None
		*ev = append(*ev, e)
	}
}

type attackOpts struct {
	name     string
	hits     int     // 攻击次数（0 视为 1）
	div      float64 // 每下伤害除以这个数（连续攻击、狂暴攻击）
	dmgMod   float64 // 伤害倍率（一击必杀、地球一周），0 视为 1
	duckMod  int     // 目标闪避加成（一击必杀、狂暴攻击）
	status   int     // 附加的异常状态
	turns    int
	gbreak   int // 1 破除防御，2 破除防御之2
	drain    int // 吸血：伤害的百分比回到自己身上
	mpDamage int // MP 攻击：目标 MP 的百分比
	// skill2.go 的技能
	noCounter  bool    // 不会引起反击（只有普通攻击那一组 case 有反击，BATTLE_S_AttackDamage 等没有）
	criMod     float64 // 会心机率倍率（浴血狂袭 ×1.3），0 视为 1
	mercy      bool    // 手下留情：打不死对方（剩 1）
	tear       int     // 撕裂伤口：再加上对方缺的 HP 的 %
	modifyElem int     // 强化攻击：对方有这个属性时伤害 +20%（地水火风 1..4，0 为没有）
	mdfyElem   int     // 属性转换：攻方属性当作只有这一种 100（1..4，0 为没有）
	fallRide   bool    // 落马术
	randomNext bool    // 狂乱暴走：第二下起打对方随机的人
	lightTake  string  // 穿透术 / 破镜重圆：对方的光镜守正好是这一种时，自己也得到一次
	noBarrier  bool    // 火线猎杀：不看光镜守（BATTLE_DamageSub_FIREKILL）
	explode    bool    // 爆裂攻击（PvP）
}

// attackSeq 是一次完整的攻击：可能连击几下，之后双方轮流反击。
func (b *Battle) attackSeq(a *Fighter, target int, o attackOpts, ev *[]Event) {
	hits := max(1, o.hits)
	t := b.pickTarget(a, target)
	if t == nil {
		*ev = append(*ev, Event{Actor: a.Slot, Kind: None, Target: -1, HP: a.HP, Status: a.Status(), Guardian: -1})
		return
	}
	first := len(*ev)
	cont := true
	var d *Fighter
	for n := 0; n < hits; n++ {
		if n > 0 {
			next := t.Slot
			if o.randomNext {
				next = b.crazedTarget(t.Side())
			}
			if t = b.pickTarget(a, next); t == nil {
				break
			}
		}
		var e Event
		e, cont = b.attackOnce(a, t, o, false)
		d = b.F[e.Target]
		if g := e.Guardian; g >= 0 {
			d = b.F[g]
		}
		*ev = append(*ev, e)
		if over, _ := b.Over(); over || !a.Alive() {
			cont = false
			break
		}
	}
	for i := first; i < len(*ev)-1; i++ {
		(*ev)[i].Combo = true
	}
	// 反击：守方先反击攻方，然后攻方再反击，最多 5 次
	for k := 0; k < 5 && cont && d != nil && !o.noCounter; k++ {
		x, y := d, a
		if k%2 == 1 {
			x, y = a, d
		}
		var e Event
		var ok bool
		if e, ok, cont = b.counter(x, y); !ok {
			break
		}
		*ev = append(*ev, e)
	}
}

// pickTarget：指定的目标还在就打它，否则改打对方随机一个（BATTLE_TargetAdjust / BATTLE_DefaultAttacker）。
func (b *Battle) pickTarget(a *Fighter, slot int) *Fighter {
	if slot >= 0 && slot < Slots && b.F[slot].Alive() && !b.F[slot].hidden && slot != a.Slot {
		return b.F[slot]
	}
	cand := b.targetable(1 - a.Side())
	if len(cand) == 0 {
		return nil
	}
	return b.F[cand[b.randi(0, len(cand)-1)]]
}

// attackOnce 是 BATTLE_Attack 的一下：闪避判定 → 守护者替挡 → 会心 / 普通伤害 → 防御 → 扣血、叫醒、附加异常状态。
// 返回事件与"之后能否反击"（会心、对方防御、对方倒下时不能）。
func (b *Battle) attackOnce(a, d *Fighter, o attackOpts, isCounter bool) (Event, bool) {
	e := Event{Actor: a.Slot, Kind: Attack, Target: d.Slot, Guardian: -1, Skill: o.name}
	if isCounter {
		e.Kind = Counter
	}
	cont := true
	if d.Pet && d.Slot-5 == a.Slot {
		d.AIDelta += aiFixSekkan // 主人打自己的宠物
	}
	if b.duck(a, d, o.duckMod) {
		e.Result, e.HP, e.Status, e.ActorHP = Dodge, d.HP, d.Status(), a.HP
		return e, cont
	}
	if !isCounter {
		if g, ok := b.guardian[d.Slot]; ok {
			gf := b.F[g]
			if g != d.Slot && gf.Alive() && gf.canMove() && gf.status[StConfusion] <= 0 && gf != a {
				d = gf
				e.Guardian = g
			}
		}
	}
	var dmg int
	if o.mdfyElem > 0 {
		// BATTLE_AttrAdjust 的 BCF_MODIFY：攻方属性只剩选的那一种（满 100）
		save := a.Attr
		a.Attr = [4]int{}
		a.Attr[o.mdfyElem-1] = 100
		defer func() { a.Attr = save }()
	}
	criPer := b.criticalPer(a, d)
	if o.criMod > 0 {
		criPer = int(float64(criPer) * o.criMod)
	}
	if b.Rnd.IntN(10000)+1 < criPer {
		if a.Weapon == data.ItemBow {
			dmg = b.damage(a, d) // 弓的会心一击没有额外伤害
		} else {
			dmg = b.criDamage(a, d)
		}
		e.Result = Critical
		cont = false
	} else {
		dmg = b.damage(a, d)
		e.Result = Normal
	}
	guarding := d.guard && d.status[StConfusion] <= 0
	switch {
	case o.gbreak == 1:
		if !guarding {
			dmg, e.Result = 0, Miss // 破除防御只对防御中的对手有效
		}
	case o.gbreak == 2:
		if guarding {
			dmg = int(float64(dmg) * 1.3)
		} else {
			dmg = int(float64(dmg) * 0.7)
		}
	case guarding:
		dmg = b.guardAdjust(dmg)
	}
	if guarding {
		cont = false
		e.Guarded = true
	}
	if dmg < 1 && o.gbreak != 1 {
		dmg = b.randi(0, 1)
	}
	if dmg == 0 {
		e.Result = Miss
		switch {
		case e.Guardian >= 0:
			e.Result, dmg = Normal, 1
		case guarding && o.gbreak != 1:
			e.Result = AllGuard
		}
	}
	if o.dmgMod > 0 {
		dmg = int(float64(dmg) * o.dmgMod)
	}
	if isCounter && dmg > 0 {
		dmg = max(1, int(float64(dmg)*0.75))
	}
	if o.div > 1 && dmg > 0 {
		dmg = max(1, int(float64(dmg)/o.div))
	}
	barrier := d.Vanish > 0 || d.Absorb > 0 || d.Reflect > 0
	if o.tear > 0 && d.Side() != a.Side() && !barrier {
		// 撕裂伤口（BATTLE_S_AttackDamage）：加上对方（与骑宠）缺的 HP 的 %；对方满血时整下没有伤害
		lost := d.MaxHP - d.HP
		if d.riding() {
			lost += d.Ride.MaxHP - d.Ride.HP
		}
		if bonus := lost * o.tear / 100; bonus <= 0 {
			dmg = 0
		} else {
			dmg += bonus
			if e.Result == Miss || e.Result == AllGuard {
				e.Result = Normal
			}
		}
	}
	if o.modifyElem > 0 && dmg > 0 && !barrier {
		// BATTLE_S_Modifyattack：对方有这个属性时 +20%（第二项是整数除法，ModNum ≥ 96 时才可能多加一倍）
		if n := d.Attr[o.modifyElem-1]; n > 0 {
			dmg += int(float64(dmg) * (0.20 + float64(b.Rnd.IntN(n+5)/100)))
		}
	}
	if o.mercy && dmg >= d.HP {
		dmg = max(0, d.HP-1) // BATTLE_DamageSub 的 SHOWMERCY
	}
	if dmg > 0 && d.status[StSleep] > 0 {
		d.status[StSleep] = 0 // BATTLE_DamageWakeUp：挨打就醒
	}
	if o.lightTake != "" && dmg > 0 {
		b.lightTake(a, d, o.lightTake)
	}
	// 守 / 光 / 镜的精灵（BATTLE_DamageSub 的 react）：无效 > 吸收 > 反射，各用掉一次；投掷类武器不会被反射
	switch {
	case o.noBarrier:
	case dmg > 0 && d.Vanish > 0:
		d.Vanish--
		dmg, e.Result = 0, Vanished
	case dmg > 0 && d.Absorb > 0:
		d.Absorb--
		d.HP = min(d.MaxHP, d.HP+dmg)
		e.Result, e.Damage, e.HP, e.Status, e.ActorHP = Absorbed, dmg, d.HP, d.Status(), a.HP
		return e, false
	case dmg > 0 && d.Reflect > 0 && !a.throwing():
		d.Reflect--
		a.HP -= dmg
		if a.HP <= 0 {
			a.HP, a.KilledBy = 0, d.Slot
		}
		e.Result, e.Damage, e.HP, e.Status, e.ActorHP = Reflected, dmg, d.HP, d.Status(), a.HP
		return e, false
	}
	if dmg > 0 && b.OnHit != nil {
		b.OnHit(a, d, dmg)
	}
	explode := o.explode && dmg > 0 && d.Side() != a.Side()
	if explode {
		// BATTLE_S_Explode（PvP）：打中就扣对方一半的 HP、自己剩 1，骑着的一定落马
		dmg = max(1, d.HP/2)
		a.HP = 1
		e.ActorHP = a.HP
		if d.riding() {
			d.Ride.Fell, e.Fall = true, true
		}
	}
	if dmg > 0 && !explode {
		e.PetDamage, e.Fall = d.rideSplit(&dmg)
	}
	// 落马术（BATTLE_S_FallGround）：打中、没有光镜守、不是自己人时 RAND(0,100) > 50 就落马
	if o.fallRide && dmg > 0 && d.Side() != a.Side() && d.riding() && b.randi(0, 100) > 50 {
		d.Ride.Fell, e.Fall = true, true
	}
	d.HP -= dmg
	if d.HP <= 0 {
		d.HP, e.Dead, cont = 0, true, false
		d.KilledBy, d.KilledRide = a.Slot, a.riding()
	}
	if dmg > 0 && o.status > 0 && d.HP > 0 && b.statusCheck(a, d, o.status) {
		d.status[o.status] = o.turns + 1
	}
	if dmg > 0 && o.drain > 0 {
		a.HP = min(a.MaxHP, a.HP+dmg*o.drain/100)
	}
	if dmg > 0 && o.mpDamage > 0 && d.Player && d.MP > 0 {
		e.MPDamage = d.MP * o.mpDamage / 100
		d.MP -= e.MPDamage
	}
	e.Damage, e.HP, e.Status, e.ActorHP = dmg, d.HP, d.Status(), a.HP
	e.RideHP = d.rideHP()
	return e, cont
}

// counter 是 BATTLE_Counter：x 反击 y。x 这回合必须是攻击或不防守战法；没击中就结束反击。
// 返回事件、是否发生、之后能否继续反击。
func (b *Battle) counter(x, y *Fighter) (Event, bool, bool) {
	if !x.Alive() || !y.Alive() || (x.com != Attack && x.com != NoGuard) {
		return Event{}, false, false
	}
	if !b.counterCheck(x, y) {
		return Event{}, false, false
	}
	e, cont := b.attackOnce(x, y, attackOpts{}, true)
	if e.Result == Miss {
		return Event{}, false, false
	}
	return e, true, cont
}

// counterCheck 是 BATTLE_CounterCheckPlayer / Pet 的机率。
func (b *Battle) counterCheck(a, d *Fighter) bool {
	at, df := float64(a.Quick), float64(d.Quick)
	div, root := counterPara, true
	switch {
	case a.Enemy() && d.Pet:
		div, root = 10, false
	case a.Pet && d.Enemy():
		df *= 0.8
	case !a.Player && d.Player:
		div, root = 10, false
	case a.Player && !d.Player:
		df *= 0.6
	}
	big, small, wari := at, df, 1.0
	if df > at {
		big, small = df, at
		if big <= 0 {
			wari = 0
		} else {
			wari = small / big
		}
	}
	work := math.Max(0, (big-small)/div)
	per := work
	if root {
		per = math.Sqrt(work)
	}
	per *= wari
	if a.Player {
		tbl := counterTable(a.Weapon, d.Weapon)
		if tbl == 0 {
			return false // 投掷类武器不会反击，也不会被反击
		}
		per = per*float64(tbl)*0.1 + float64(a.Luck)
		if per*100 <= 0 {
			return false
		}
		return float64(b.Rnd.IntN(10000)+1) < per*100
	}
	per = math.Min(100, per+float64(a.ctrMod))
	if per <= 0 {
		per = 0.01
	}
	return float64(b.Rnd.IntN(10000)+1) <= per*100
}

// statusCheck 是 BATTLE_StatusAttackCheck：已有异常状态就不会再中；麻痹固定 20% − 抗性；
// 其余为 30 + 等级差×2（±40 以内）+ 攻方运气 − 抗性 − 体力占比/0.25×10，最多 80%。
func (b *Battle) statusCheck(a, d *Fighter, st int) bool { return b.statusChance(a, d, st, 30, 40, 2) }

// statusChance 是 BATTLE_StatusAttackCheck(攻, 守, 状态, PerOffset, Range, Bai)：
// 物理攻击附带的是 (30, 40, 2)，咒术、道具与沉默 / 魔障 / 虚弱等技能是 (成, 30, 1.0)。
func (b *Battle) statusChance(a, d *Fighter, st int, offset, rng int, bai float64) bool {
	if d.Status() != StNone {
		return false
	}
	var per float64
	if st == StParalysis {
		per = math.Max(0, float64(20-d.Resist[st]))
	} else {
		sum := d.Base[0] + d.Base[1] + d.Base[2] + d.Base[3]
		vit := 0.0
		if sum > 0 {
			vit = d.Base[0] / sum / 0.25 * 10
		}
		lv := float64(a.Level-d.Level) * bai
		lv = math.Max(-float64(rng), math.Min(float64(rng), lv))
		luck := 0
		if a.Player {
			luck = a.Luck
		}
		per = math.Min(80, float64(offset)+lv+float64(luck)-float64(max(0, d.Resist[st]))-vit)
	}
	return float64(b.randi(1, 100)) < per
}

// duck 是 BATTLE_DuckCheck：防御中、不能动时不会闪避；敌人打玩家时攻方敏捷 × 0.6，玩家打敌人时守方敏捷 × 0.6。
// mod 是攻方技能的加成（一击必杀等降低命中），攻方酒醉时再加 20..30。
func (b *Battle) duck(a, d *Fighter, mod int) bool {
	if d.guard || !d.canMove() {
		return false
	}
	// 闪避术（BATTLE_CheckMySkillDuck）：rand()%100 <= 机率 就闪开
	if d.skillDuck > 0 && b.Rnd.IntN(100) <= d.duckP {
		return true
	}
	at, df := float64(a.Quick+a.dexBonus()), float64(d.Quick+d.dexBonus())
	luck := 0.0
	if d.Player {
		luck = float64(d.Luck)
	}
	switch {
	case a.Enemy() && d.Pet:
		at *= 0.8
	case !a.Enemy() && d.Pet:
		df *= 0.8
	case !a.Player && d.Player:
		at *= 0.6
	case a.Player && !d.Player:
		df *= 0.6
	}
	big, small, wari := df, at, 1.0
	if at > df {
		big, small = at, df
		if big <= 0 {
			wari = 0
		} else {
			wari = small / big
		}
	}
	work := math.Max(0, (big-small)/kawashiPara)
	per := math.Sqrt(work)*wari + luck + float64(mod)
	if a.Weapon == data.ItemBow {
		per += 20 // 弓箭容易被闪开
	}
	if a.status[StDrunk] > 0 {
		per += b.rand(20, 30)
	}
	per += float64(d.duckMod)
	per = math.Min(per*100, duckMaxRate*100)
	if per <= 0 {
		per = 1
	}
	return float64(b.Rnd.IntN(10000)+1) <= per
}

// criticalPer 是 BATTLE_CriticalCheckPlayer 的机率（万分比）。没有装备，At_Soubi 为 0。
func (b *Battle) criticalPer(a, d *Fighter) int {
	at, df := float64(a.Quick), float64(d.Quick)
	luck := 0.0
	if a.Player {
		luck = float64(a.Luck)
	}
	div, root := criticalPara, true
	switch {
	case a.Pet && d.Enemy():
		df *= 0.8
	case a.Enemy() && d.Pet:
		div, root = 10.0, false
	case !a.Player && d.Player:
		div, root = 10.0, false
	case a.Player && !d.Player:
		df *= 0.6
	}
	big, small, wari := at, df, 1.0
	if df > at {
		big, small = df, at
		if big <= 0 {
			wari = 0
		} else {
			wari = small / big
		}
	}
	work := math.Max(0, (big-small)/div)
	per := work
	if root {
		per = math.Sqrt(work)
	}
	per = (per*wari + luck) * 100
	if per < 0 {
		per = 1
	}
	return int(math.Min(per, 10000))
}

// damage 是 BATTLE_DamageCalc。石化的目标防御力 ×2。
func (b *Battle) damage(a, d *Fighter) int {
	attack := a.rideAtk()
	defense := d.rideDef() * 0.70    // _BATTLE_NEWPOWER
	if d.magStatus == magSuperWall { // 铁壁（_MAGIC_SUPERWALL）：防御 +(n + rand%20)%
		defense += defense * float64(d.magN+b.Rnd.IntN(20)) / 100
	}
	if d.Enemy() {
		defense += (defense*float64(b.Rnd.IntN(10)) + 2) / 100 // _NPCENEMY_ADDPOWER
	}
	if a.Enemy() {
		attack += (attack*float64(b.Rnd.IntN(10)) + 2) / 100
	}
	if d.status[StStone] > 0 {
		defense *= 2
	}
	var dmg int
	switch {
	case defense <= attack && attack < defense*8.0/7.0:
		dmg = int(b.rand(0, attack/16))
	case defense > attack:
		dmg = int(b.rand(0, 1))
	default:
		k0 := b.rand(0, attack/8) - attack/16
		dmg = int((attack-defense)*2.0 + k0)
	}
	dmg = attrAdjust(a.attrs(), d.attrs(), dmg)
	dmg = int(float64(dmg) * b.fieldPower(a.attrs()) / b.fieldPower(d.attrs()))
	return dmg * DamageCalcRate / 100
}

// fieldPower 是 BATTLE_FieldAttAdjust：有战场属性时，那一种属性越高越有利（0.5 + 属性 × 强度 / 10000 × 0.5），没有时 0.5。
func (b *Battle) fieldPower(attr [4]int) float64 {
	if b.fieldAttr < 1 || b.fieldAttr > 4 {
		return 0.5
	}
	return 0.5 + float64(attr[b.fieldAttr-1])*float64(b.fieldPow)*0.01*0.01*0.5
}

// criDamage 是 BATTLE_CriDamageCalc：普通伤害 + 守方防御力 × 攻方等级 / 守方等级 × 0.5。
func (b *Battle) criDamage(a, d *Fighter) int {
	lv := 1.0
	if d.Level > 0 {
		lv = float64(a.Level) / float64(d.Level)
	}
	return b.damage(a, d) + int(float64(d.Def)*lv*0.5)
}

// guardAdjust 是 BATTLE_GuardAdjust：原版防御时伤害随机减为 0%..50%（25% 的机会完全挡掉）。
// H5 改成只减伤、不会完全格挡：原版 0% 的那一档改成 10%，而且至少 1 点（AllGuard 只剩破除防御等特殊情况会出现）。
func (b *Battle) guardAdjust(dmg int) int {
	r := b.randi(1, 100)
	rate := 0.5
	switch {
	case r <= 25:
		rate = 0.1
	case r <= 50:
		rate = 0.1
	case r <= 70:
		rate = 0.2
	case r <= 85:
		rate = 0.3
	case r <= 95:
		rate = 0.4
	}
	return max(1, int(float64(dmg)*rate))
}

// attrAdjust 是 BATTLE_AttrAdjust + BATTLE_AttrCalc：地水火风与"无"属性（100 − 四属性之和）互相克制。
// 克制关系：火克风、风克地、地克水、水克火（AJ_UP 1.5 / AJ_SAME 1.0 / AJ_DOWN 0.6）；无属性打任何属性都弱。
// 战场属性没有实现，攻守两方的场地修正相同（0.5 / 0.5）。
func attrAdjust(at, df [4]int, dmg int) int {
	none := func(a [4]int) int {
		n := 100
		for _, v := range a {
			n -= max(0, v)
		}
		return max(0, n)
	}
	return attrAdjustN(at, none(at), df, none(df), dmg)
}

// attrAdjustN 是 BATTLE_AttrCalc：四属性与"无"属性另外给（攻击魔法的牵引属性，"无"仍是施法者原本的）。
func attrAdjustN(at [4]int, atNone int, df [4]int, dfNone int, dmg int) int {
	pow := func(a [4]int, n int) (earth, water, fire, wind, none float64) {
		return float64(max(0, a[0])), float64(max(0, a[1])), float64(max(0, a[2])), float64(max(0, a[3])), float64(max(0, n))
	}
	const up, same, down = 1.5, 1.0, 0.6
	ae, aw, af, ai, an := pow(at, atNone)
	de, dw, dfi, di, dn := pow(df, dfNone)
	d := float64(dmg)
	ae, aw, af, ai, an = ae*d, aw*d, af*d, ai*d, an*d
	fire := af*dn*up + af*dfi*same + af*dw*down + af*de*same + af*di*up
	water := aw*dn*up + aw*dfi*up + aw*dw*same + aw*de*down + aw*di*same
	earth := ae*dn*up + ae*dfi*same + ae*dw*up + ae*de*same + ae*di*down
	wind := ai*dn*up + ai*dfi*down + ai*dw*same + ai*de*up + ai*di*same
	none := an*dn*same + an*dfi*down + an*dw*down + an*de*down + an*di*down
	return int((fire + water + earth + wind + none) / (100 * 100))
}

// escapeCheck 是 BATTLE_EscapeCheck：运气决定基础机率，敌人平均等级比自己高越多越难逃。
func (b *Battle) escapeCheck(f *Fighter) bool {
	if b.PvP {
		return true // BATTLE_EscapeCheck：P_vs_P 一定逃得掉
	}
	luck := 1
	if f.Player {
		luck = min(5, max(1, f.Luck))
	} else {
		switch f.Rare {
		case 0:
			luck = 1
		case 1:
			luck = 3
		default:
			luck = 5
		}
	}
	cnt := f.escapes + 1
	lv, n := 0, 0
	for _, i := range b.alive(1 - f.Side()) {
		lv += b.F[i].Level
		n++
	}
	esc := 100
	if n > 0 {
		lv /= n
		diff := 2 * (lv - f.Level)
		switch {
		case luck >= 5:
			esc = 95 * cnt
		case luck >= 4:
			esc = 60*cnt - diff
		case luck >= 3:
			esc = 50*cnt - diff
		case luck >= 2:
			esc = 40*cnt - diff
		default:
			esc = 30*cnt - diff
		}
	}
	esc = max(1, esc)
	return b.randi(1, 100) < esc
}

// tactic 读出行动方针里某一项的数值，如 "at:10;1;1" 的 at → [10 1 1]。
func tactic(opt, key string, n int) []int {
	out := make([]int, n)
	for _, part := range strings.Split(opt, "|") {
		k, v, ok := strings.Cut(part, ":")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		for i, s := range strings.Split(v, ";") {
			if i >= n {
				break
			}
			out[i], _ = strconv.Atoi(strings.TrimSpace(s))
		}
	}
	return out
}

// enemyAI 是 BATTLE_ai_normal：按 at/gu/ma/es/wa 的权重随机决定攻击、防御、逃跑或用第几个技能。
// 攻击与技能的目标：at 第二项 1 全部 / 2 只打人物 / 3 只打宠物（找不到时改为全部）；第三项 1 随机 / 2 HP 最高 / 3 HP 最低。
// 魔法（ma）还没有实现，抽到时改为普通攻击。
func (b *Battle) enemyAI(f *Fighter) Command {
	at := tactic(f.Tactics, "at", 3)
	gu := tactic(f.Tactics, "gu", 1)
	ma := tactic(f.Tactics, "ma", 1)
	es := tactic(f.Tactics, "es", 1)
	wa := tactic(f.Tactics, "wa", 7)
	total := at[0] + gu[0] + ma[0] + es[0]
	for _, w := range wa {
		total += w
	}
	kind, skill := Attack, -1
	if total > 0 {
		r := b.randi(0, total-1)
		switch {
		case at[0] != 0 && r < at[0]:
		case gu[0] != 0 && r < at[0]+gu[0]:
			kind = Guard
		case ma[0] != 0 && r < at[0]+gu[0]+ma[0]:
		case es[0] != 0 && r < at[0]+gu[0]+ma[0]+es[0]:
			kind = Escape
		default:
			acc := at[0] + gu[0] + ma[0] + es[0]
			for i, w := range wa {
				acc += w
				if w != 0 && r < acc {
					skill = i
					break
				}
			}
		}
	}
	if kind != Attack {
		return Command{Kind: kind}
	}
	targets := func(filter int) []int {
		var out []int
		for _, i := range b.targetable(1 - f.Side()) {
			p := b.F[i].Player && i%SideOffset < 5
			if filter == 2 && !p || filter == 3 && p {
				continue
			}
			out = append(out, i)
		}
		return out
	}
	cand := targets(at[1])
	if len(cand) == 0 {
		cand = targets(1)
	}
	if len(cand) == 0 {
		return Command{Kind: None}
	}
	t := cand[0]
	switch at[2] {
	case 2, 3:
		for _, i := range cand[1:] {
			if at[2] == 2 && b.F[i].HP > b.F[t].HP || at[2] == 3 && b.F[i].HP < b.F[t].HP {
				t = i
			}
		}
	default:
		t = cand[b.randi(0, len(cand)-1)]
	}
	c := Command{Kind: Attack, Target: t}
	if skill >= 0 {
		c.Skill = f.Skills[skill]
	}
	return c
}

// poisonDown 是 Compute_Down 的一份：(四项基本状态和 / 100 − 20) / 4，至少 1，最多扣到剩 1。
func poisonDown(base [4]float64, hp int) int {
	down := max(1, int((base[0]+base[1]+base[2]+base[3])/100-20)/4)
	return max(0, min(down, hp-1))
}
