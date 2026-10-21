package world

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// Item 是一件道具的实例。能力值在产生时从道具定义的 [最小, 最大] 里随机取（ITEM_getRandomValue）。
type Item struct {
	ID   int                `json:"id"`
	Mods [data.ModCount]int `json:"m"`
	Pile int                `json:"n,omitempty"` // 堆叠数（0 视为 1）
	// 有次数的飞行道具（ITEM_useWarpForNum）改写过的参数与剩下的次数（ITEM_ARGUMENT / ITEM_DAMAGEBREAK）；空的用道具定义的
	Arg  string `json:"arg,omitempty"`
	Uses int    `json:"uses,omitempty"`
	// Used 是有次数的施法道具（ITEM_useMagic）用过几次
	Used int `json:"used,omitempty"`
	// Dur 是目前的耐久（ITEM_DAMAGECRUSHE，0 是还没算过 = 产生时的值），Crush 是损坏等级（ITEM_CRUSHLEVEL：0 完好 1 受损 2 毁坏 3 不堪使用）
	Dur   int `json:"dur,omitempty"`
	Crush int `json:"crush,omitempty"`
	// Merged 是加工 / 料理做出来的（ITEM_MERGEFLG，魔术笔只能改这种）；Name 是改过的名字（ITEM_SECRETNAME）
	Merged bool   `json:"merged,omitempty"`
	Name   string `json:"name,omitempty"`
	// 镶过宝石的精工装备（PETSKILL_ITEM_inslay）：已镶的代码（ITEM_INLAYCODE）、改写的说明（ITEM_EFFECTSTRING）、
	// 宝石带来的咒术（ITEM_MAGICID / MAGICUSEMP：编号、MP）
	Inlay string  `json:"inlay,omitempty"`
	Memo  string  `json:"memo,omitempty"`
	Magic *[2]int `json:"magic,omitempty"`
}

func (it *Item) count() int { return max(1, it.Pile) }

// inventory 是 9 个装备位 + 45 格道具栏（_NEW_ITEM_ 三页），下标见 data.Slot*。
type inventory [data.ItemSlots]*Item

func parseInventory(s string) *inventory {
	inv := &inventory{}
	if s == "" {
		return inv
	}
	var list []*Item
	if err := json.Unmarshal([]byte(s), &list); err != nil {
		return inv
	}
	copy(inv[:], list)
	return inv
}

func (inv *inventory) String() string {
	b, _ := json.Marshal(inv[:])
	return string(b)
}

// newItem 是 ITEM_makeItem：各项能力在最小、最大值之间随机。
func (w *World) newItem(def *data.ItemDef) *Item {
	it := &Item{ID: def.ID, Pile: 1, Dur: durStart(def)}
	for i, r := range def.Mods {
		it.Mods[i] = r[0]
		if r[1] > r[0] {
			it.Mods[i] += w.rnd.IntN(r[1] - r[0] + 1)
		}
	}
	return it
}

// addItem 放进第一个空格（同种可堆叠的先叠上去），返回格位，满了回 -1（CHAR_addItemSpecificItemIndex）。
func (w *World) addItem(inv *inventory, it *Item) int {
	def := w.game.Items[it.ID]
	if def != nil && def.MaxPile > 1 {
		for i := data.EquipSlots; i < data.ItemSlots; i++ {
			if x := inv[i]; x != nil && x.ID == it.ID && x.count()+it.count() <= def.MaxPile {
				x.Pile = x.count() + it.count()
				return i
			}
		}
	}
	for i := data.EquipSlots; i < data.ItemSlots; i++ {
		if inv[i] == nil {
			inv[i] = it
			return i
		}
	}
	return -1
}

// equipMods 是 ITEM_equipEffect：所有装备的能力加总。参数里写了"攻""防""敏""体""气""运""魅""回"的，那一项是百分比。
// items 是装备着的那几格（角色的装备位或宠物身上的道具），base 是不含装备时的攻、防、敏、HP、MP（用于百分比）。
func (w *World) equipMods(items []*Item, base [5]int) (mods [data.ModCount]int, attrib [4]int) {
	keys := []string{"攻", "防", "敏", "体", "气", "运", "魅", "回"}
	for _, it := range items {
		if it == nil {
			continue
		}
		def := w.game.Items[it.ID]
		if def == nil || def.NeedProfession > 0 { // 职业不合的装备没有效果（CHAR_complianceParameter 前先拿下，见 unequipProfession）
			continue
		}
		for i := range mods {
			v := it.Mods[i]
			if i < len(keys) && strings.Contains(def.Argument, keys[i]) && i < len(base) {
				v = base[i] * v / 100
			}
			mods[i] += v
		}
		if def.Attrib >= 1 && def.Attrib <= 4 {
			attrib[def.Attrib-1] += def.AttribValue
		}
	}
	return
}

// equipSprite 是 CHAR_getNewImagenumberFromEquip（char_data.c CHAR_eqimagetbl）：每个形象的每种颜色有 5 张连续的图，
// 依次是空手（拳套）、斧、棍、枪、弓；显示的形象 = 基本形象 + 武器种类（0..4），其他武器（回力镖、投掷类）用基本形象。
// equipSprite 是现在的形象：骑着宠物时是骑乘形象（ride.go），否则是拿着武器的形象。
func (w *World) equipSprite(e *entity) int {
	if e.char.Pig > 0 {
		return pigSprite // 乌力化中（_CHAR_complianceParameter 保留乌力的形象）
	}
	if e.metamo != nil {
		return e.metamo.sprite // 变身中（_CHAR_complianceParameter 保留变身的形象）
	}
	if s := w.rideSprite(e); s > 0 {
		return s
	}
	return w.weaponSprite(e)
}

// weaponSprite 是 CHAR_getNewImagenumberFromEquip：人物原形象 + 武器种类。
func (w *World) weaponSprite(e *entity) int {
	base := e.char.Sprite
	if base < 100000 || base >= 100000+20*12 { // 只有玩家形象（SPR_001em 起的 48 组）有持武器的图
		return base
	}
	cat := data.ItemFist
	if wp := w.weapon(e.inv); wp != nil {
		cat = wp.Type
	}
	if cat < 0 || cat > data.ItemBow {
		return base
	}
	return base + cat
}

// weapon 回传装备的武器（没有时 nil）。
func (w *World) weapon(inv *inventory) *data.ItemDef {
	if it := inv[data.SlotArm]; it != nil {
		return w.game.Items[it.ID]
	}
	return nil
}

func (w *World) sendInventory(e *entity) {
	msg := &proto.Inventory{Gold: e.char.Gold, Items: []proto.InvItem{}}
	for i, it := range e.inv {
		if it == nil {
			continue
		}
		def := w.game.Items[it.ID]
		if def == nil {
			continue
		}
		msg.Items = append(msg.Items, invItem(i, it, def))
		// 装备着的道具带的咒术（CHAR_makeStatusString 的 "J"：精灵窗口列出的内容）
		if m, mp := w.equipMagic(e, i); m != nil {
			x := &msg.Items[len(msg.Items)-1]
			x.Magic, x.MagicMemo, x.MagicMp, x.MagicField, x.MagicTarget, x.MagicDead, x.MagicFunc, x.MagicOption = m.Name, m.Comment, mp, m.Field, m.Target, m.Dead, m.Func, m.Option
		}
	}
	e.client.Send(msg)
}

// invItem 是 ITEM_makeItemStatusString 的内容（道具栏与宠物身上的道具共用）。
// flags 是原版的 flg：1 能宠物邮寄、2 能加工（CANMERGEFROM）、4 料理、8 金属、16 宝石、32 耐久不满（能修复）；
// alch 是精炼用的素材名（只有一种成分时是那一种，否则"杂"）；merged 是加工出来的（名字画黄色）。
func invItem(slot int, it *Item, def *data.ItemDef) proto.InvItem {
	x := proto.InvItem{Slot: slot, ID: it.ID, Name: def.Name, Image: def.Image, Memo: def.Memo,
		Level: def.Level, Pile: it.count(), Type: def.Type, Field: def.Field, Target: def.Target,
		Usable: def.UseFunc != "" && (def.Type == data.ItemOther || def.Type == data.ItemDish), Equip: data.EquipSlot(def.Type),
		Params: itemParams(def, it), Damage: itemDamage(def, it), Merged: it.Merged, Alch: "杂", Jigsaw: jigsawArg(it, def)}
	if it.Name != "" {
		x.Name = it.Name
	}
	if it.Memo != "" {
		x.Memo = it.Memo
	}
	for _, f := range []struct {
		on  bool
		bit int
	}{{def.CanPetMail, 1}, {def.CanMergeFrom, 2}, {def.Type == data.ItemDish, 4}, {def.Type == data.ItemMetal, 8},
		{def.Type == data.ItemJewel, 16}, {def.Durability > 0 && itemDur(def, it) != def.Durability, 32}} {
		if f.on {
			x.Flags |= f.bit
		}
	}
	if len(def.Ings) == 1 {
		x.Alch = def.Ings[0].Name
	}
	return x
}

// itemParams 是 ITEM_makeItemStatusString 里（原版注释掉的）paramshow：这件道具实际的能力值。
// 道具产生时各项在 [最小, 最大] 之间随机，说明文字（effectstring）写的是固定值，这里列出真正的数值；
// 参数写了"攻""防"等字的那一项是百分比（ITEM_equipEffect），加上 %。没有任何加减的道具回传空字串。
func itemParams(def *data.ItemDef, it *Item) string {
	names := [data.ModCount]string{"攻", "防", "敏", "体", "气", "运", "魅", "回避", "必杀"}
	keys := []string{"攻", "防", "敏", "体", "气", "运", "魅", "回"}
	var parts []string
	for i, v := range it.Mods {
		if v == 0 {
			continue
		}
		s := fmt.Sprintf("%s%+d", names[i], v)
		if i < len(keys) && strings.Contains(def.Argument, keys[i]) {
			s += "%"
		}
		parts = append(parts, s)
	}
	if def.Attrib >= 1 && def.Attrib <= 4 && def.AttribValue != 0 {
		parts = append(parts, fmt.Sprintf("%s%+d", []string{"地", "水", "火", "风"}[def.Attrib-1], def.AttribValue/10))
	}
	return strings.Join(parts, " ")
}

// itemDamage 是 _ADD_SHOW_ITEMDAMAGE 的耐久度文字：没有耐久的"不会损坏"，否则是剩下的百分比。
// 这台服务端没定义 _TAKE_ITEMDAMAGE，装备不会按耐久磨损（只有极低机率的 BATTLE_ItemCrush 降能力，还没做），所以一律是 100%。
// itemDamage 是 _ADD_SHOW_ITEMDAMAGE 的耐久度显示（item.c）：(目前/1000)×100/(最大/1000) 的百分比，最大耐久小于 1 的"不会损坏"。
func itemDamage(def *data.ItemDef, it *Item) string {
	if def.Durability < 1 {
		return "不会损坏"
	}
	max := def.Durability / 1000
	if max < 1 {
		return "100%"
	}
	return fmt.Sprintf("%d%%", itemDur(def, it)/1000*100/max)
}

// durStart 是道具产生时的耐久（ITEM_makeItem：damagecrushe，没写时用最大值）。
func durStart(def *data.ItemDef) int {
	if def.DurInit > 0 {
		return min(def.DurInit, max(def.Durability, def.DurInit))
	}
	return def.Durability
}

// itemDur 是目前的耐久（旧存档没有记的当作产生时的值）。
func itemDur(def *data.ItemDef, it *Item) int {
	if it.Dur <= 0 {
		return durStart(def)
	}
	return it.Dur
}

// itemsChanged：道具或装备变了，存档并通知（装备会影响能力值）。
func (w *World) itemsChanged(e *entity) {
	e.char.Items = e.inv.String()
	w.checkMetamo(e) // 脱下衣服就变回来
	w.updateSprite(e)
	w.sendInventory(e)
	w.sendStatus(e)
	w.saveProgress(e)
}

// updateSprite：形象变了就告诉看得到的人（CHAR_sendCToArroundCharacter）。
func (w *World) updateSprite(e *entity) {
	if s := w.equipSprite(e); s != e.sprite {
		e.sprite = s
		for _, o := range w.floors[e.floor] {
			if o.client != nil && (o == e || o.visible[e.id]) {
				o.client.Send(&proto.ObjSprite{ID: e.id, Sprite: s})
			}
		}
	}
}

func (w *World) itemMsg(e *entity, text string) {
	e.client.Send(&proto.ChatMsg{Channel: "near", From: "", Text: text, Color: 0})
}

// canEquip 检查道具能不能放到某个装备位（ITEM_getEquipPlace + 等级限制）。
func (w *World) canEquip(e *entity, it *Item, slot int) error {
	def := w.game.Items[it.ID]
	if def == nil {
		return fmt.Errorf("不明的道具")
	}
	place := data.EquipSlot(def.Type)
	if place < 0 {
		return fmt.Errorf("这个道具不能装备")
	}
	if e.metamo != nil && (def.Type == data.ItemBow || def.Type == data.ItemBoundThrow || def.Type == data.ItemBreakThrow || def.Type == data.ItemBoomerang) {
		return fmt.Errorf("变身无法使用远距离武器！")
	}
	if place != slot && !(place == data.SlotDeco1 && slot == data.SlotDeco2) {
		return fmt.Errorf("不能装备在那里")
	}
	if e.char.Trans == 0 && def.Level > e.char.Level {
		return fmt.Errorf("等级不够，无法装备")
	}
	// CHAR_EquipItem（_ITEMSET4_TXT）：腕力、敏捷、转生次数不够，或要职业的道具（没有职业系统，人物都是无职业）都不能装备
	if def.NeedProfession != 0 {
		return fmt.Errorf("职业不符，无法装备")
	}
	if e.char.Str*100 < def.NeedStr || e.char.Dex*100 < def.NeedDex || e.char.Trans < def.NeedTrans {
		return fmt.Errorf("能力不够，无法装备")
	}
	// _ITEM_EQUITSPACE：拿弓时不能装盾（ITEM_getEquipPlace 对弓回 -1）
	if def.Type == data.ItemBow && e.inv[data.SlotShield] != nil && slot == data.SlotArm {
		return fmt.Errorf("装备盾牌时不能使用弓")
	}
	return nil
}

// unequipProfession 是 _CHAR_PROFESSION 上线时的修正（char.c）：与人物职业不合的装备拿下放进道具栏（道具栏满了就留在身上，但没有效果）。
func (w *World) unequipProfession(e *entity) {
	for i := 0; i < data.EquipSlots; i++ {
		if d := w.itemDef(e.inv[i]); d != nil && d.NeedProfession > 0 {
			it := e.inv[i]
			e.inv[i] = nil
			if w.addItem(e.inv, it) < 0 {
				e.inv[i] = it
			}
		}

	}
	e.char.Items = e.inv.String()
}

// ItemMove 移动道具：道具栏之间交换或叠加；拖到装备位就是装备，从装备位拖回道具栏就是卸下。
// to 为 -1 时自动处理：道具栏里的装备品装上去，装备位上的卸到第一个空格。
func (w *World) ItemMove(charID, from, to int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || e.busy() || from < 0 || from >= data.ItemSlots || to < -1 || to >= data.ItemSlots || from == to {
			return
		}
		inv := e.inv
		it := inv[from]
		if it == nil {
			return
		}
		if to == -1 {
			if from < data.EquipSlots {
				to = -1
				for i := data.EquipSlots; i < data.ItemSlots; i++ {
					if inv[i] == nil {
						to = i
						break
					}
				}
				if to < 0 {
					w.itemMsg(e, "道具栏已满。")
					return
				}
			} else {
				def := w.game.Items[it.ID]
				if def == nil || data.EquipSlot(def.Type) < 0 {
					return
				}
				to = data.EquipSlot(def.Type)
				if to == data.SlotDeco1 && inv[to] != nil && inv[data.SlotDeco2] == nil {
					to = data.SlotDeco2
				}
			}
		}
		other := inv[to]
		if to < data.EquipSlots {
			if err := w.canEquip(e, it, to); err != nil {
				w.itemMsg(e, err.Error())
				return
			}
		}
		if from < data.EquipSlots && other != nil {
			if err := w.canEquip(e, other, from); err != nil {
				w.itemMsg(e, err.Error())
				return
			}
		}
		// 同种可堆叠的道具叠在一起
		if other != nil && other.ID == it.ID && from >= data.EquipSlots && to >= data.EquipSlots {
			if def := w.game.Items[it.ID]; def != nil && def.MaxPile > 1 {
				n := min(def.MaxPile-other.count(), it.count())
				if n > 0 {
					other.Pile = other.count() + n
					it.Pile = it.count() - n
					if it.Pile <= 0 {
						inv[from] = nil
					}
					w.itemsChanged(e)
					return
				}
			}
		}
		inv[from], inv[to] = other, it
		w.itemsChanged(e)
	})
}

// recovery 解析回复道具的参数（ITEM_useRecovery_Field）："体N" 回 HP RAND(N×0.9, N×1.1)，"气N" 回 MP，"魅N" 加魅力，"全" 全回复。
func (w *World) recovery(arg string) (hp, mp, charm int, full bool) {
	if strings.Contains(arg, "全") {
		return 10000000, 100, 0, true
	}
	roll := func(key string) int {
		i := strings.Index(arg, key)
		if i < 0 {
			return 0
		}
		s := arg[i+len(key):]
		end := 0
		for end < len(s) && s[end] >= '0' && s[end] <= '9' {
			end++
		}
		n, err := strconv.Atoi(s[:end])
		if err != nil {
			return 1
		}
		lo, hi := int(float64(n)*0.9), int(float64(n)*1.1)
		return lo + w.rnd.IntN(max(1, hi-lo+1))
	}
	return roll("体"), roll("气"), roll("魅"), false
}

// ItemMoveLocked 是在世界协程内的自动装备（给 ItemUse 双击装备品用）。
func (w *World) ItemMoveLocked(e *entity, from int) {
	it := e.inv[from]
	def := w.game.Items[it.ID]
	to := data.EquipSlot(def.Type)
	if to == data.SlotDeco1 && e.inv[to] != nil && e.inv[data.SlotDeco2] == nil {
		to = data.SlotDeco2
	}
	if err := w.canEquip(e, it, to); err != nil {
		w.itemMsg(e, err.Error())
		return
	}
	e.inv[from], e.inv[to] = e.inv[to], it
	w.itemsChanged(e)
}
