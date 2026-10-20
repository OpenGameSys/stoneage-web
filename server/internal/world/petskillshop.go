package world

import (
	"fmt"
	"strconv"
	"strings"

	"sa/server/internal/data"
)

// 宠物技能店（npc_petskillshop.c）：参数 pet_skill 列出卖的技能编号，skill_rate 是价格倍率。
// 窗口是 petskillshop（WINDOW_MESSAGETYPE_PETSKILLSHOP），文字照原版的字串：
//   "1|店名|main_msg|技能名|价格|说明|技能名|价格|说明…"（_VERSION_25：每个技能后面没有 "|0"）
// 客户端回传 "第几个技能|第几只宠物|第几格|价格"（都从 1 起，价格服务端会重算）。

const seqPetSkillShop = 260 // CHAR_WINDOWTYPE_WINDOWPETSKILLSHOP

// petSkillShopList：参数 pet_skill 里的技能（找不到的略过，ILLEGAL 的原版不卖）。
func (w *World) petSkillShopList(n *entity) []*data.Skill {
	list, _ := data.ArgField(n.npc.Arg, "pet_skill")
	var out []*data.Skill
	for _, id := range strings.Split(list, ",") {
		if s := w.game.Battle.Skills[data.Atoi(id)]; strings.TrimSpace(id) != "" && s != nil {
			out = append(out, s)
		}
	}
	return out
}

func petSkillRate(n *entity) float64 {
	s, ok := data.ArgField(n.npc.Arg, "skill_rate")
	if !ok {
		return 1.0
	}
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

// petSkillShopOpen 是 NPC_PetSkillShopTalked + NPC_PetSkillMakeStr：面对面 2 格内或相邻。
func (w *World) petSkillShopOpen(p, n *entity) {
	if !faceToFace(p, n, 2) && max(abs(p.x-n.x), abs(p.y-n.y)) > 1 {
		return
	}
	w.petSkillShopWindow(p, n)
}

func (w *World) petSkillShopWindow(p, n *entity) {
	msg, ok := data.ArgField(n.npc.Arg, "main_msg")
	if !ok {
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "1|%s|%s", n.name, msg)
	rate := petSkillRate(n)
	for _, s := range w.petSkillShopList(n) {
		fmt.Fprintf(&sb, "|%s|%d|%s", s.Name, int(float64(s.Price)*rate), s.Comment)
	}
	w.sendWindow(p, n, "petskillshop", 0, seqPetSkillShop, sb.String())
}

// petSkillShopReply 是 NPC_PetSkillShopWindowTalked：换掉宠物的第 slot 格技能，收技能价格 × skill_rate。
// 离 NPC 超过 2 格、战斗中、钱不够都不处理；买完重新显示窗口。
func (w *World) petSkillShopReply(p, n *entity, text string) {
	if max(abs(p.x-n.x), abs(p.y-n.y)) > 2 || p.fight != nil {
		return
	}
	f := strings.Split(text, "|")
	for len(f) < 4 {
		f = append(f, "")
	}
	skill, petNo, slot := data.Atoi(f[0]), data.Atoi(f[1]), data.Atoi(f[2])-1
	list := w.petSkillShopList(n)
	pet := w.petAt(p, petNo-1)
	if skill < 1 || skill > len(list) || pet == nil || slot < 0 || slot >= petSkillMax {
		return
	}
	s := list[skill-1]
	cost := int(float64(s.Price) * petSkillRate(n))
	if p.char.Gold < cost {
		return
	}
	pet.Skills[slot] = s.ID // CHAR_setPetSkill
	p.char.Gold -= cost
	w.petsChanged(p)
	w.sendInventory(p)
	w.petSkillShopWindow(p, n)
}
