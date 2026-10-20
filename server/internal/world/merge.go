package world

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 宠物的地图技能（lssproto_PS_recv → PETSKILL_Use）里和道具有关的几种：
//
//	PETSKILL_Merge    加工（200）、料理（201）：ITEM_mergeItem_merge；精炼（PETSKILL_Alchemist，202）在原版的函数表里也指到 PETSKILL_Merge
//	PETSKILL_Fixitem  修复（540）：PETSKILL_ITEM_FixItem，用同材料的道具把装备的耐久补满
//	PETSKILL_Inslay   镶宝石（572）：PETSKILL_ITEM_inslay，把宝石的能力加到精工装备上（_EXPANSION_ITEM_INSLAY：最多 max(3, min(选项, 25)) 颗）
//
// data 是客户端选好的道具栏格位，"|" 分开（原客户端右键标记"合成 / 料理 / 精炼 / 精工 / 修复"的那几格）。
// 结果回 PetSkillResult（lssproto_PS_send），结果为 0 时客户端说"失败！"。
//
// 加工的规则（item_gen.c ITEM_mergeItem）：
//  1. 材料都要能加工（CANMERGEFROM），料理与道具不能混在一起（"非法的合成方法"，材料照样用掉）；一秒内连续合成时随机还你一样材料。
//  2. 把材料的成分（itematom.txt 里有的素材）按素材加总：同一种由小到大排，后一个加上前一个 × oddstable × 档次倍率（ITEM_simplify_atoms），
//     最多 1000。
//  3. 每种素材的数值随机浮动：宠物模板对这种素材有修正（E_T_ATOMFIX*）就加上 base、用它的千分比缩放范围，否则加工按档次
//     （ItemRandTableForItem）在 0.87 / 档次倍率 .. 1.05 × 档次倍率之间，料理在 0.9 .. 1.1 之间。
//  4. 找成分对得上的道具（ITEM_merge_with_retry）：先随机决定要几种素材都对上（越多越可能），所有能做出来的道具（CANMERGETO）里
//     成分全部落在范围内（加工：数值 / 倍率 .. 数值 × 倍率、最多 2500；料理：0.7 .. 1.3 倍）且成分数正好等于那个数目的、
//     又不是材料本身的，随机挑一个；找 5 次都没有就随机还你一样材料。做出来的道具标上 ITEM_MERGEFLG（名字黄色、魔术笔能改名）。
//
// 没做的部分：家族守护兽（CHAR_PETFAMILY）与家族料理、合成经验（_COMPOUND_EXP 的 CHAR_MATERIAL，只给私服的评定能力用）；
// _MAX_MERGE_LEVEL 的上限 MAXMERGELEVEL 在 setup.cf 里没有设（= 0，原版会让所有加工都失败），这里当作不限。

// randRow 是 ItemRandTableForItem 的一档（ITEM_initRandTable）。
type randRow struct {
	minnum, maxnum int
	rate           float64 // maxnum / num
}

var mergeRandTable = func() []randRow {
	nums := []int{10, 30, 65, 125, 205, 305, 425, 565, 725, 905, 1125, 1354, 1594, 1825, 2105, 2405, 2725, 3065, 3425, 3805}
	t := make([]randRow, len(nums))
	for i, n := range nums {
		if i > 0 {
			t[i].minnum = t[i-1].maxnum + 1
		}
		if i+1 < len(nums) {
			t[i].maxnum = int(float64(n) + float64(nums[i+1]-n)*0.7) // ITEM_GEN_RATE
		} else {
			t[i].maxnum = 4000
		}
		t[i].rate = float64(t[i].maxnum) / float64(n)
	}
	return t
}()

// mergeTableNum 是 ITEM_getTableNum：数值落在哪一档。
func mergeTableNum(v int) int {
	for i, r := range mergeRandTable {
		if v <= r.maxnum {
			return i
		}
	}
	return len(mergeRandTable) - 1
}

// mergeOdds 是 ITEM_simplify_atoms 的 oddstable。
var mergeOdds = []float64{0.1, 0.25, 0.35, 0.4, 0.42, 0.44, 0.46, 0.47, 0.48, 0.49, 0.5, 0.51, 0.52, 0.53}

// PetSkillUse 是 lssproto_PS_recv：地图上用宠物 petSlot 的第 skill 个技能。战斗中不理（原版直接 return）。
func (w *World) PetSkillUse(charID, petSlot, skill int, arg string) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || e.busy() || petSlot < 0 || petSlot >= PetSlots || e.pets.Pets[petSlot] == nil {
			return
		}
		p := e.pets.Pets[petSlot]
		ret := 0
		if skill >= 0 && skill < petSkillMax && w.game.Battle != nil {
			if s := w.game.Battle.Skills[p.Skills[skill]]; p.Skills[skill] >= 0 && s != nil {
				slots := mergeSlots(arg)
				switch s.Func {
				case "PETSKILL_Merge", "PETSKILL_Alchemist":
					ret = w.petMerge(e, p, slots)
				case "PETSKILL_Fixitem":
					ret = w.petFixItem(e, slots)
				case "PETSKILL_Inslay":
					ret = w.petInslay(e, s, slots)
				}
			}
		}
		e.client.Send(&proto.PetSkillResult{OK: ret != 0, Pet: petSlot, Skill: skill})
	})
}

// mergeSlots 拆开 "格位|格位|…"（getStringFromIndexWithDelim + atoi，最多 15 个）；只认道具栏的格位，其他的当作没有（-1）。
func mergeSlots(arg string) []int {
	var out []int
	for i, s := range strings.Split(arg, "|") {
		if i >= data.BagSlots {
			break
		}
		n := data.Atoi(s)
		if n < data.EquipSlots || n >= data.ItemSlots {
			n = -1
		}
		out = append(out, n)
	}
	return out
}

// petMerge 是 ITEM_mergeItem_merge：回传 1 成功、0 失败、-1 道具栏没有空格。
func (w *World) petMerge(e *entity, p *Pet, slots []int) int {
	free := false
	for i := data.EquipSlots; i < data.ItemSlots; i++ {
		free = free || e.inv[i] == nil
	}
	if !free {
		w.sysMsg(e, "合成时，最少需空一格物品栏位！", colorYellow)
		return -1
	}
	var defs []*data.ItemDef
	var used []int
	for _, s := range slots {
		if s < 0 || e.inv[s] == nil || w.game.Items[e.inv[s].ID] == nil {
			continue
		}
		def := w.game.Items[e.inv[s].ID]
		if !def.CanMergeFrom {
			w.sysMsg(e, fmt.Sprintf("似乎对%s没有兴趣。", def.Name), colorWhite)
			continue
		}
		for _, u := range used {
			if u == s {
				return 0 // 同一格选了两次（Merge Collision）
			}
		}
		defs, used = append(defs, def), append(used, s)
	}
	if len(defs) < 2 {
		return 0
	}
	created := w.mergeItem(e, defs, p)
	if created == -10 {
		w.sysMsg(e, "非法的合成方法", colorWhite)
	}
	for _, s := range used { // 材料不论成败都用掉（堆叠的一次一个，_ITEM_PILENUMS）
		w.useUp(e, s)
	}
	ret := 0
	if def := w.game.Items[created]; created >= 0 && def != nil {
		it := w.newItem(def) // ITEM_makeItemAndRegist：能力照样随机
		it.Merged = true
		if w.addItem(e.inv, it) >= 0 {
			ret = 1
		}
	}
	w.itemsChanged(e)
	return ret
}

// mergeItem 是 ITEM_mergeItem：回传做出来的道具编号；-10 是料理与道具混在一起，-1 是材料没有成分。
func (w *World) mergeItem(e *entity, defs []*data.ItemDef, p *Pet) int {
	giveBack := func() int { return defs[w.rnd.IntN(len(defs))].ID }
	now := w.now().Unix()
	if now-e.lastMerge < 1 {
		e.lastMerge = now
		w.sysMsg(e, "合成料理过於频繁，休息一下比较好哟。", colorRed)
		return giveBack()
	}
	e.lastMerge = now
	dish := defs[0].Type == data.ItemDish // searchtable：料理用第二组范围
	var names []string
	var raw [][]float64
	for i, d := range defs {
		if (d.Type == data.ItemDish) != (defs[0].Type == data.ItemDish) && i > 0 {
			return -10
		}
		for _, ing := range d.Ings {
			if !w.game.Atoms[ing.Name] {
				continue
			}
			k := 0
			for k < len(names) && names[k] != ing.Name {
				k++
			}
			if k == len(names) {
				names, raw = append(names, ing.Name), append(raw, nil)
			}
			raw[k] = append(raw[k], float64(ing.Value))
		}
	}
	// ITEM_simplify_atoms
	vals := make([]int, len(names))
	for i, v := range raw {
		sort.Float64s(v)
		for j := 1; j < len(v); j++ {
			rate := mergeRandTable[mergeTableNum(int(v[j-1]))].rate / mergeRandTable[0].rate
			v[j] += v[j-1] * mergeOdds[min(j-1, len(mergeOdds)-1)] * rate
		}
		vals[i] = min(int(v[len(v)-1]), 1000) // ITEM_ATOMIND_MAX
	}
	// 宠物对素材的修正（ITEM_merge_getPetFix）：范围是千分比，负的当 1000
	var fixes []data.AtomFix
	if t := w.game.Battle.Temps[p.TempNo]; t != nil {
		for _, f := range t.AtomFix {
			if !w.game.Atoms[f.Name] {
				continue
			}
			if f.Min > f.Max {
				f.Min, f.Max = f.Max, f.Min
			}
			if f.Min < 0 {
				f.Min = 1000
			}
			if f.Max < 0 {
				f.Max = 1000
			}
			fixes = append(fixes, f)
		}
	}
	for i := range vals {
		rate := mergeRandTable[0].rate
		if !dish {
			rate = mergeRandTable[mergeTableNum(vals[i])].rate
		}
		var fix *data.AtomFix
		for k := range fixes {
			if fixes[k].Name == names[i] {
				fix = &fixes[k]
				break
			}
		}
		switch {
		case fix != nil && !dish:
			lo, hi := float64(max(0, fix.Min))/1000, float64(max(0, fix.Max))/1000
			vals[i] = w.mergeRange(vals[i]+fix.Base, int(1/rate*0.87*lo*1000), int(rate*1.05*hi*1000))
		case fix != nil:
			lo := float64(max(0, fix.Min)) / 1000 // 原版上下限都乘 fixedmin
			vals[i] = w.mergeRange(vals[i]+fix.Base, int(900*lo), int(1100*lo))
		case !dish:
			vals[i] = w.mergeRange(vals[i], int(1/rate*0.87*1000), int(rate*1.05*1000))
		default:
			vals[i] = w.mergeRange(vals[i], 900, 1100)
		}
	}
	if len(names) == 0 {
		return -1
	}
	for try := 0; try < 5; try++ {
		if id := w.mergeSearch(defs, names, vals, dish); id >= 0 {
			return id
		}
	}
	return giveBack()
}

// mergeRange 是 ITEM_randRange：base × [lo, hi] / 1000 之间随机（rint）。
func (w *World) mergeRange(base, lo, hi int) int {
	if lo > hi {
		lo, hi = hi, lo
	}
	minN := int(math.RoundToEven(float64(base) / 1000 * float64(lo)))
	maxN := int(math.RoundToEven(float64(base) / 1000 * float64(hi)))
	switch r := maxN - minN; {
	case lo == 0 && hi == 0:
		return 0
	case r == 0:
		return base
	case r < 0:
		return 0
	default:
		return minN + w.rnd.IntN(r+1)
	}
}

// mergeTarget 是能做出来的道具（ingcache：CANMERGETO、有 itematom.txt 里的成分）。
type mergeTarget struct {
	id   int
	ings []data.Ingredient
}

// mergeTargets 按编号排好（原版按编号走 icache），第一次用到时建立。
func (w *World) mergeTargets() []mergeTarget {
	if w.mergeCache != nil {
		return w.mergeCache
	}
	w.mergeCache = []mergeTarget{}
	for _, d := range w.game.Items {
		if !d.CanMergeTo {
			continue
		}
		t := mergeTarget{id: d.ID}
		for _, ing := range d.Ings {
			if w.game.Atoms[ing.Name] {
				t.ings = append(t.ings, ing)
			}
		}
		if len(t.ings) > 0 {
			w.mergeCache = append(w.mergeCache, t)
		}
	}
	sort.Slice(w.mergeCache, func(i, j int) bool { return w.mergeCache[i].id < w.mergeCache[j].id })
	return w.mergeCache
}

// mergeSearch 是 ITEM_merge_with_retry：回传找到的道具编号，没有回 -1。vals 在料理时会被压到上限（同原版）。
func (w *World) mergeSearch(defs []*data.ItemDef, names []string, vals []int, dish bool) int {
	idealTbl := [5][]int{{0}, {250, 0}, {400, 150, 0}, {700, 260, 70, 0}, {740, 500, 200, 40, 0}}
	ideal := min(len(names), 5)
	var done [5]bool
	var hits []int
	targets := w.mergeTargets()
	for extracted := 0; extracted < ideal; {
		r := w.rnd.IntN(1000)
		k := 0
		for k < ideal-1 && r < idealTbl[ideal-1][k] {
			k++
		}
		if done[k] {
			continue
		}
		done[k] = true
		extracted++
		need := ideal - k // 要几种素材都对上
		if hits == nil {
			hits = make([]int, len(targets))
			for ti, t := range targets {
				for _, ing := range t.ings {
					for n, name := range names {
						if ing.Name != name {
							continue
						}
						v := float64(ing.Value)
						if !dish {
							rate := mergeRandTable[mergeTableNum(vals[n])].rate
							top := min(float64(vals[n])*rate, 2500)
							if v <= top && v >= float64(vals[n])/rate {
								hits[ti]++
								break
							}
							continue
						}
						if capv := float64(mergeRandTable[9].maxnum) / 1.3; float64(vals[n]) > capv {
							vals[n] = int(capv)
						}
						if v <= float64(vals[n])*1.3 && v >= float64(vals[n])*0.7 {
							hits[ti]++
							break
						}
					}
				}
			}
		}
		var match []int
		for ti, t := range targets {
			if hits[ti] != len(t.ings) || hits[ti] != need {
				continue
			}
			self := false
			for _, d := range defs {
				self = self || d.ID == t.id // ITEM_merge_checkitem：不能做出材料本身
			}
			if !self {
				match = append(match, t.id)
			}
		}
		if len(match) > 0 {
			return match[w.rnd.IntN(len(match))]
		}
	}
	return -1
}

// fixableType 是 PETSKILL_Fixitem 认得的武器或防具（宠物装备在 _TAKE_ITEMDAMAGE_FOR_PET 下不算）。
func fixableType(t int) bool {
	switch {
	case t >= data.ItemFist && t <= data.ItemAmulet, t == data.ItemBoomerang, t == data.ItemBoundThrow, t == data.ItemBreakThrow:
		return true
	case t == data.ItemWBelt, t == data.ItemWShield, t == data.ItemWShoes, t == data.ItemWGlove:
		return true
	}
	return false
}

// petFixItem 是 PETSKILL_Fixitem + PETSKILL_ITEM_FixItem：一件武器或防具加一件材料（第一个成分要是装备的成分之一，
// 或参数是 FIXITEMALL 的修复之石）；耐久不到 80%、最大耐久 500 以上才能修，修好后耐久补满、损坏等级归零，材料用掉。
func (w *World) petFixItem(e *entity, slots []int) int {
	fix := -1
	var picked []int
	for i, s := range slots {
		if s < 0 || e.inv[s] == nil || w.game.Items[e.inv[s].ID] == nil {
			continue
		}
		if i >= 2 {
			w.sysMsg(e, "修复每次只能选择两个物品。", colorYellow)
			return 0
		}
		for _, x := range picked {
			if x == s {
				return 0
			}
		}
		def := w.game.Items[e.inv[s].ID]
		if def.Type == data.ItemDish {
			w.sysMsg(e, "料理不能做修复。", colorYellow)
			return 0
		}
		if fixableType(def.Type) {
			if fix >= 0 {
				w.sysMsg(e, "修复每次只能选择一个武器或防具。", colorYellow)
				return 0
			}
			fix = s
		}
		picked = append(picked, s)
	}
	if fix < 0 {
		w.sysMsg(e, "修复必须选择一个武器或防具。", colorYellow)
		return 0
	}
	other := -1
	for _, s := range picked {
		if s != fix {
			other = s
			break
		}
	}
	if other < 0 {
		return 0
	}
	fit, fdef, odef := e.inv[fix], w.game.Items[e.inv[fix].ID], w.game.Items[e.inv[other].ID]
	match := false
	for _, ing := range fdef.Ings {
		match = match || len(odef.Ings) > 0 && ing.Name == odef.Ings[0].Name
	}
	if !match && odef.Argument != "FIXITEMALL" { // _ITEM_FIXALLBASE：修复之石什么都能修
		w.sysMsg(e, "材料不符", colorYellow)
		return 0
	}
	cur := itemDur(fdef, fit)
	switch {
	case float64(cur) >= float64(fdef.Durability)*0.80:
		w.sysMsg(e, fdef.Name+"并没有损坏到需要修复。", colorYellow)
		return 0
	case fdef.Durability < 500:
		w.sysMsg(e, "此物品已不能修复", colorYellow)
		return 0
	case cur <= 0:
		return 0
	}
	fit.Dur, fit.Crush = fdef.Durability, 0
	if i := strings.Index(fit.Name, "("); i > 0 { // 修正名称：去掉 "(" 之后的部分
		fit.Name = fit.Name[:i]
	}
	for _, s := range picked {
		if s != fix {
			e.inv[s] = nil
		}
	}
	w.itemsChanged(e)
	w.sysMsg(e, "修复完成。", colorYellow)
	return 1
}

// petInslay 是 PETSKILL_Inslay：选一件精工装备（TYPECODE 含 INSLAY）与几颗宝石（有 TYPECODE 的），一颗一颗镶上去，
// 镶好的宝石用掉；中途镶不下就停（之前镶上的照算）。
func (w *World) petInslay(e *entity, s *data.Skill, slots []int) int {
	base := -1
	var picked []int
	for _, x := range slots {
		if x < 0 || e.inv[x] == nil || w.game.Items[e.inv[x].ID] == nil {
			continue
		}
		for _, y := range picked {
			if y == x {
				return 0
			}
		}
		tc := w.game.Items[e.inv[x].ID].TypeCode
		if tc == "" || tc == "NULL" {
			w.sysMsg(e, "这样物品不适合精工。", colorYellow)
			return 0
		}
		if strings.Contains(tc, "INSLAY") {
			if base >= 0 {
				w.sysMsg(e, "精工每次只能选择一个武器或防具。", colorYellow)
				return 0
			}
			base = x
		}
		picked = append(picked, x)
	}
	if base < 0 {
		w.sysMsg(e, "精工必须选择一个武器或防具。", colorYellow)
		return 0
	}
	most := max(3, min(data.Atoi(s.Option), 25))
	ret := 1
	for _, x := range picked {
		if x == base {
			continue
		}
		if !w.inslay(e, base, x, most) {
			ret = 0
			break
		}
		e.inv[x] = nil // CHAR_DelItem
	}
	w.itemsChanged(e)
	return ret
}

// inslay 是 PETSKILL_ITEM_inslay：宝石的代码放进第一个空位（"NULL"），攻防敏 HP MP 运加上宝石的，宝石带的咒术换上去，
// 说明改成"攻+n 防+n 敏+n HP+n MP+n 伤+n 吸+n 咒术名"。宝石的功能与参数原版也照抄过去，这份数据的宝石都没有，略过；
// 伤 / 吸（ITEM_OTHERDAMAGE / OTHERDEFC）这里的道具没有，一律是 0。
func (w *World) inslay(e *entity, base, gem, most int) bool {
	bit, git := e.inv[base], e.inv[gem]
	bdef, gdef := w.game.Items[bit.ID], w.game.Items[git.ID]
	if gdef.TypeCode == "NULL" {
		return false
	}
	codes := make([]string, 25)
	for i := range codes {
		codes[i] = "NULL"
	}
	inlay := bit.Inlay
	if inlay == "" {
		inlay = bdef.InlayCode
	}
	for i, c := range strings.Split(inlay, "|") {
		if i < most && c != "" {
			codes[i] = c
		}
	}
	at := 0
	for at < most && codes[at] != "NULL" {
		at++
	}
	if at == most {
		w.sysMsg(e, fmt.Sprintf("武器或防具已经镶满%d颗或%d颗以上。", most, most), colorYellow)
		return false
	}
	codes[at] = gdef.TypeCode
	bit.Inlay = strings.Join(codes, "|")
	for _, k := range []int{data.ModAtk, data.ModDef, data.ModQuick, data.ModHP, data.ModMP, data.ModLuck} {
		bit.Mods[k] += git.Mods[k]
	}
	if gdef.MagicID > 0 {
		bit.Magic = &[2]int{gdef.MagicID, gdef.MagicMP}
	}
	magic := ""
	id := bdef.MagicID
	if bit.Magic != nil {
		id = bit.Magic[0]
	}
	if m := w.game.Battle.Magics[id]; id > 0 && m != nil {
		magic = m.Name
	}
	sign := func(v int) string {
		if v >= 0 {
			return "+" + strconv.Itoa(v)
		}
		return strconv.Itoa(v)
	}
	bit.Memo = fmt.Sprintf("攻%s 防%s 敏%s HP%s MP%s 伤+0 吸+0 %s", sign(bit.Mods[data.ModAtk]), sign(bit.Mods[data.ModDef]),
		sign(bit.Mods[data.ModQuick]), sign(bit.Mods[data.ModHP]), sign(bit.Mods[data.ModMP]), magic)
	return true
}
