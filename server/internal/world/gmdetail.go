package world

import (
	"fmt"
	"strings"

	"sa/server/internal/battle"
	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// itemTypeNames 是道具种类（ITEM_*）的名字，GM 清单的详情用。
var itemTypeNames = map[int]string{
	data.ItemFist: "拳套", data.ItemAxe: "斧", data.ItemClub: "棍棒", data.ItemSpear: "枪", data.ItemBow: "弓",
	data.ItemShield: "盾", data.ItemHelm: "头盔", data.ItemArmour: "铠甲", data.ItemBracelet: "手环", data.ItemMusic: "乐器",
	data.ItemNecklace: "项链", data.ItemRing: "戒指", data.ItemBelt: "腰带", data.ItemEarring: "耳环", data.ItemNosering: "鼻环",
	data.ItemAmulet: "护身符", data.ItemOther: "其他", data.ItemBoomerang: "回力镖", data.ItemBoundThrow: "投掷斧",
	data.ItemBreakThrow: "投掷石", data.ItemDish: "料理", data.ItemMetal: "金属", data.ItemJewel: "宝石",
	data.ItemWBelt: "腰带", data.ItemWShield: "盾", data.ItemWShoes: "鞋", data.ItemWGlove: "手套",
}

// GmDetailReq 是 GM 窗口的清单里滑鼠停着的那一项的详情（图与几行文字）。只回给 GM。
func (w *World) GmDetailReq(charID int, kind string, id int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || !e.gm {
			return
		}
		out := &proto.GmDetail{Kind: kind, ID: id, Lines: []string{}}
		add := func(format string, a ...any) { out.Lines = append(out.Lines, fmt.Sprintf(format, a...)) }
		switch kind {
		case "item":
			d := w.game.Items[id]
			if d == nil {
				return
			}
			out.Image = d.Image
			add("%s（编号 %d）", d.Name, id)
			tn := itemTypeNames[d.Type]
			if tn == "" {
				tn = fmt.Sprintf("种类 %d", d.Type)
			}
			add("%s　等级 %d　价格 %d", tn, d.Level, d.Cost)
			names := [data.ModCount]string{"攻", "防", "敏", "体", "气", "运", "魅", "回避", "必杀"}
			var mods []string
			for i, r := range d.Mods {
				switch {
				case r[0] == 0 && r[1] == 0:
				case r[0] == r[1]:
					mods = append(mods, fmt.Sprintf("%s%+d", names[i], r[0]))
				default:
					mods = append(mods, fmt.Sprintf("%s%+d~%+d", names[i], r[0], r[1]))
				}
			}
			if d.Attrib >= 1 && d.Attrib <= 4 && d.AttribValue != 0 {
				mods = append(mods, fmt.Sprintf("%s%+d", []string{"地", "水", "火", "风"}[d.Attrib-1], d.AttribValue/10))
			}
			if len(mods) > 0 {
				add("能力 %s", strings.Join(mods, " "))
			}
			if d.AttackMax > 0 {
				add("攻击次数 %d~%d", d.AttackMin, d.AttackMax)
			}
			dur := "不会损坏"
			if d.Durability > 0 {
				dur = fmt.Sprintf("耐久 %d", d.Durability/1000)
			}
			pile := ""
			if d.MaxPile > 1 {
				pile = fmt.Sprintf("　可堆叠 %d", d.MaxPile)
			}
			add("%s%s", dur, pile)
			if d.UseFunc != "" {
				add("使用 %s %s", d.UseFunc, d.Argument)
			}
			if m := w.game.Battle.Magics[d.MagicID]; d.MagicID >= 0 && m != nil {
				add("咒术 %s", m.Name)
			}
			if memo := strings.TrimSpace(d.Memo); memo != "" {
				add("%s", memo)
			}
		case "enemy":
			en := w.game.Battle.Enemies[id]
			if en == nil {
				return
			}
			t := w.game.Battle.Temps[en.TempNo]
			if t == nil {
				add("%s（敌人编号 %d，没有模板）", en.Name, id)
				break
			}
			out.Image = t.Image
			add("%s（敌人编号 %d，模板 %d）", t.Name, id, en.TempNo)
			capt := "不可捕获"
			if en.PetFlag != 0 {
				capt = fmt.Sprintf("可捕获（难度 %d）", t.Get)
			}
			add("等级 %d~%d　%s", en.LvMin, en.LvMax, capt)
			add("属性 地%d 水%d 火%d 风%d", t.Attr[0]/10, t.Attr[1]/10, t.Attr[2]/10, t.Attr[3]/10)
			add("成长率 体%d 腕%d 耐%d 敏%d　档次 %d", t.Base[0], t.Base[1], t.Base[2], t.Base[3], data.EnemyRank(t))
			k := float64(t.InitNum)
			lv1 := battle.Derive(k*(float64(t.Base[0])+2.5), k*(float64(t.Base[1])+2.5), k*(float64(t.Base[2])+2.5), k*(float64(t.Base[3])+2.5))
			add("1 级约 攻%d 防%d 敏%d 血%d", lv1.Atk, lv1.Def, lv1.Quick, lv1.HP)
			add("抗性 毒%d 麻%d 眠%d 石%d 醉%d 乱%d", t.Status[0], t.Status[1], t.Status[2], t.Status[3], t.Status[4], t.Status[5])
			var sk []string
			for _, s := range t.Skills {
				if x := w.game.Battle.Skills[s]; s >= 0 && x != nil {
					sk = append(sk, x.Name)
				}
			}
			if len(sk) > 0 {
				add("技能 %s", strings.Join(sk, "、"))
			}
			var drops []string
			for i, it := range en.Items {
				if d := w.game.Items[it]; it > 0 && d != nil && en.ItemProbs[i] > 0 {
					drops = append(drops, fmt.Sprintf("%s %d‰", d.Name, en.ItemProbs[i]))
				}
			}
			if len(drops) > 0 {
				add("掉落 %s", strings.Join(drops, "、"))
			}
		case "skill":
			s := w.game.Battle.Skills[id]
			if s == nil {
				return
			}
			field := []string{"战斗与地图", "只在战斗", "只在地图"}
			f := ""
			if s.Field >= 0 && s.Field < len(field) {
				f = field[s.Field]
			}
			add("%s（技能编号 %d）", s.Name, id)
			add("%s　%s", f, s.Func)
			if s.Option != "" {
				add("参数 %s", s.Option)
			}
			if c := strings.TrimSpace(s.Comment); c != "" {
				add("%s", c)
			}
		case "player":
			t := w.players[id]
			if t == nil {
				return
			}
			out.Image = t.sprite
			c := &t.char
			maxHP, _ := w.maxHPMP(t)
			hp := c.HP
			if hp < 0 {
				hp = maxHP
			}
			add("%s　Lv%d　%d 转", t.name, c.Level, c.Trans)
			add("账号 %s", t.account)
			if w.now().Before(t.muteUntil) {
				add("禁言中，还有 %d 分钟", int(t.muteUntil.Sub(w.now()).Minutes())+1)
			}
			add("楼层 %d (%d,%d)", t.floor, t.x, t.y)
			add("HP %d/%d　DP %d", hp, maxHP, c.DP)
			add("石币 %d　声望 %d", c.Gold, c.Fame/100)
			for _, p := range t.pets.Pets {
				if p != nil {
					add("宠物 %s Lv%d", p.displayName(), p.Level)
				}
			}
			if t.fight != nil {
				add("战斗中")
			}
		default:
			return
		}
		e.client.Send(out)
	})
}
