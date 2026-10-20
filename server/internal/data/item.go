package data

import (
	"path/filepath"
	"strings"
)

// 道具（gmsv/item/item.c ITEM_readItemConfFile，setup.cf itemset6file=itemset6.txt）。
// 栏位顺序见 ITEM_itemconfentries，按这台服务端的构建宏（_ITEM_INSLAY、_Item_ReLifeAct、_ITEM_MAXUSERNUM、
// _ITEMSET4_TXT、_ADD_DEAMGEDEFC、_SUIT_ITEM、_ITEMSET5_TXT、_ITEMSET6_TXT）展开；数据里 neguard 之后还多了
// 10 栏（_VERSION_GF 的 unknown1..10），所以异常状态从第 74 栏开始。攻击、防御等"随机值"栏位占两栏（最小、最大）。

// 道具种类（ITEM_CATEGORY）
const (
	ItemFist       = 0
	ItemAxe        = 1
	ItemClub       = 2
	ItemSpear      = 3
	ItemBow        = 4
	ItemShield     = 5
	ItemHelm       = 6
	ItemArmour     = 7
	ItemBracelet   = 8
	ItemMusic      = 9
	ItemNecklace   = 10
	ItemRing       = 11
	ItemBelt       = 12
	ItemEarring    = 13
	ItemNosering   = 14
	ItemAmulet     = 15
	ItemOther      = 16
	ItemBoomerang  = 17
	ItemBoundThrow = 18
	ItemBreakThrow = 19
	ItemDish       = 20
	ItemMetal      = 21 // _ITEM_INSLAY：金属
	ItemJewel      = 22 // 宝石
	ItemWBelt      = 24 // _ITEM_INSLAY（金属、宝石）与 _ITEM_CHECKWARES（货物）之后
	ItemWShield    = 25
	ItemWShoes     = 26
	ItemWGlove     = 27
)

// 道具能力加减（ITEM_MODIFY*）的下标
const (
	ModAtk = iota
	ModDef
	ModQuick
	ModHP
	ModMP
	ModLuck
	ModCharm
	ModAvoid
	ModCritical
	ModCount
)

// ItemDef 是 itemset6.txt 的一行。
type ItemDef struct {
	ID          int
	Name        string
	SecretName  string
	Memo        string // effectstring：道具说明
	Argument    string // 道具参数，如回复道具的 "体20"
	UseFunc     string // usefunc，如 ITEM_useRecovery
	Image       int
	Cost        int
	Type        int
	Field       int // 0 都能用、1 只在战斗、2 只在地图（ITEM_FIELDTYPE）
	Target      int // 0 自己、1 其他人、2 我方全体、3 对方全体、4 全体（ITEM_TARGETTYPE）
	Level       int
	MaxPile     int // 堆叠上限（upinums）
	Durability  int // damcrushe / maxdmce
	AttackMin   int // 攻击次数
	AttackMax   int
	Mods        [ModCount][2]int // 各项加减的最小、最大值
	Attrib      int              // 1..4 地水火风
	AttribValue int
	MagicID     int       // 装备着就能用的咒术（magic.txt 编号，-1 没有）
	MagicMP     int       // 用这个咒术要的 MP（magicusemp）
	Resist      [6][2]int // 毒、麻、眠、石、醉、乱抗性
	VanishDrop  bool
	Uses        int // damagebreak：有次数的道具（ITEM_useWarpForNum）能用几次
	DurInit     int // damagecrushe：产生时的耐久（Durability 是最大耐久 maxdamagecrushe）
	// 不能寄放（npc_poolitemshop.c：登出消失、丢弃消失、不能宠物邮寄的都不收）
	DropAtLogout, CanPetMail bool
	// 加工 / 料理（item_gen.c）：能当材料（ITEM_CANMERGEFROM）、能做出来（ITEM_CANMERGETO）、成分（ITEM_INGNAME0..4 / INGVALUE0..4）
	CanMergeFrom, CanMergeTo bool
	Ings                     []Ingredient
	// 镶宝石（_ITEM_INSLAY）：TYPECODE（宝石的代码，精工装备是 "INSLAY"）与 INLAYCODE（已镶的代码，"|" 分开）
	TypeCode, InlayCode string
}

// Ingredient 是道具的一项成分（素材名与数值）。
type Ingredient struct {
	Name  string
	Value int
}

// LoadItems 读取 gamedata/item/itemset6.txt。
func LoadItems(dataDir string) (map[int]*ItemDef, error) {
	lines, err := readLines(filepath.Join(dataDir, "item", "itemset6.txt"))
	if err != nil {
		return nil, err
	}
	out := map[int]*ItemDef{}
	for _, l := range lines {
		c := cols(l)
		if len(c) < 94 {
			continue
		}
		v := func(col int) int { return num(c[col-1], 0) } // 栏位从 1 起，和 getStringFromIndexWithDelim 一致
		pair := func(col int) [2]int {
			a, b := v(col), v(col+1)
			return [2]int{min(a, b), max(a, b)}
		}
		d := &ItemDef{Name: c[0], SecretName: c[1], Memo: c[2], Argument: c[3], UseFunc: c[10],
			ID: v(17), Image: v(18), Cost: v(19), Type: v(20), Field: v(21), Target: v(22), Level: v(23),
			MaxPile: v(25), Durability: v(32), AttackMin: v(36), AttackMax: v(37),
			Attrib: v(54), AttribValue: v(55), MagicID: num(c[55], -1), MagicMP: v(58), VanishDrop: v(90) == 1, Uses: v(24), DurInit: v(31),
			DropAtLogout: v(89) == 1, CanPetMail: v(92) == 1}
		for i := 0; i < 8; i++ { // 攻 防 敏 体 气 运 魅 回避：38..53
			d.Mods[i] = pair(38 + i*2)
		}
		for i := 0; i < 6; i++ { // 毒 麻 眠 石 醉 乱：74..85
			d.Resist[i] = pair(74 + i*2)
		}
		d.Mods[ModCritical] = pair(86)
		d.TypeCode, d.InlayCode = c[4], c[5]
		d.CanMergeFrom, d.CanMergeTo = v(93) == 1, v(94) == 1
		for i := 0; i < 5 && 95+i*2 < len(c); i++ { // 成分：95..104，名字与数值交替
			if name := strings.TrimSpace(c[94+i*2]); name != "" {
				d.Ings = append(d.Ings, Ingredient{name, v(96 + i*2)})
			}
		}
		if d.AttackMin == 0 {
			d.AttackMin = d.AttackMax
		}
		d.AttackMin, d.AttackMax = min(d.AttackMin, d.AttackMax), max(d.AttackMin, d.AttackMax)
		if d.SecretName == "" {
			d.SecretName = d.Name
		}
		if _, dup := out[d.ID]; !dup { // 原版：重复编号以第一个为准
			out[d.ID] = d
		}
	}
	return out, nil
}

// EquipSlot 是 ITEM_getEquipPlace：道具能装备的位置，-1 为不能装备。
// 饰品回传饰品 1，饰品 2 由玩家拖到那一格时也可以放。
func EquipSlot(itemType int) int {
	switch itemType {
	case ItemFist, ItemSpear, ItemAxe, ItemClub, ItemBoomerang, ItemBreakThrow, ItemBoundThrow, ItemBow:
		return SlotArm
	case ItemHelm:
		return SlotHead
	case ItemArmour:
		return SlotBody
	case ItemBracelet, ItemMusic, ItemNecklace, ItemRing, ItemBelt, ItemEarring, ItemNosering, ItemAmulet:
		return SlotDeco1
	case ItemWBelt:
		return SlotBelt
	case ItemWShield, ItemShield:
		return SlotShield
	case ItemWShoes:
		return SlotShoes
	case ItemWGlove:
		return SlotGlove
	}
	return -1
}

// 宠物装备（_PET_ITEM）：数据里是 29..37 九种（头、翼、牙、身体、背、爪、双足、四足、鳍，原客户端 pc.h 的 ITEM_PET_*），
// 宠物身上 7 格按原客户端的 PET_HEAD..PET_FOOT 排（三种脚共用最后一格）。原服务端的 ITEM_PET_* 只有 7 种、编号也对不上这份数据，
// 所以照客户端与数据的对应。
const (
	ItemPetHead  = 29
	ItemPetFin   = 37
	PetItemSlots = 7 // CHAR_PETITEMNUM
	PetSlotClaw  = 5 // 爪：宠物的"武器"（原服务端排在 CHAR_ARM 那一格，攻击时磨损它）
)

// PetEquipSlot 是宠物装备该放的那一格（原客户端 PET_*），不是宠物装备回 -1。
func PetEquipSlot(itemType int) int {
	switch {
	case itemType < ItemPetHead || itemType > ItemPetFin:
		return -1
	case itemType >= ItemPetHead+6:
		return 6
	}
	return itemType - ItemPetHead
}

// 装备位置（CHAR_EQUIPPLACE，_ITEM_EQUITSPACE + _EQUIT_NEWGLOVE）与道具栏（_NEW_ITEM_：3 页 × 15 格）。
const (
	SlotHead = iota
	SlotBody
	SlotArm
	SlotDeco1
	SlotDeco2
	SlotBelt
	SlotShield
	SlotShoes
	SlotGlove
	EquipSlots // 9
	BagSlots   = 45
	ItemSlots  = EquipSlots + BagSlots
)

// LoadAtoms 读取 gamedata/item/itematom.txt（"名字,魔法旗标,..."，ITEM_initItemAtom 只用第一栏当素材名）。
func LoadAtoms(dataDir string) (map[string]bool, error) {
	lines, err := readLines(filepath.Join(dataDir, "item", "itematom.txt"))
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, l := range lines {
		out[strings.TrimSpace(cols(l)[0])] = true
	}
	return out, nil
}
