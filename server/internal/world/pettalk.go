package world

import (
	"strings"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 跟宠物说话（原版 char/pet_event.c PET_Talkfunc，_PET_TALK + _PET_TALKPRO；资料 npc/pettalk，data.LoadPetTalk）。
// 主人对着放在地上的宠物说话（或点它）时，宠物按自己的对话档回一个窗口：对话档按 "OVER" 分段，
// 宠物等级没到上限（CHAR_LIMITLEVEL）时用含 "TALKRUN" 的段，到了上限用含 "EVENTRUN" 的段，含 "BOTH" 的段两种都用；
// 每段要 FREE（玩家的条件，同传送员的 FREE）成立，有 FLOOR（玩家所在楼层，"!楼层" 是不在那层）、PET（宠物自己的条件）的也要成立。
// 成立的段里随机挑一段：做 AddItem 等动作（Action_RunDoEventAction）、RandItem（1/11 的机会拿到）、LimitLevel 改宠物的等级上限，
// 窗口显示 TalkMsg。都不成立时从 OTHER 段的 TalkMsg（以 "," 分开）随机挑一句。
// 玛蕾菲雅（718 / 401）的等级上限就是这样一段一段提高的：到了上限，在指定的地方跟她说话，上限加 5，最后在 40 号 75 级提到 79。

// petTalkTypes 是 TalkType[]：等级没到上限 / 到了上限 / 两种都用。
var petTalkTypes = [3]string{"TALKRUN", "EVENTRUN", "BOTH"}

// petTalk 是 PET_Talkfunc：p 对地上的宠物 o 说话（点选时 text 为空）。
func (w *World) petTalk(p, o *entity, text string) {
	g := o.ground
	if g == nil || g.pet == nil || p.fight != nil {
		return
	}
	// NPC_Util_isFaceToFace(宠物, 玩家, 2) 或玩家面向宠物 1 格
	if !faceToFace(o, p, 2) && !inFront(p, o, 1) {
		return
	}
	pet := g.pet
	talk, ok := w.game.PetTalk[pet.TempNo]
	if !ok {
		return
	}
	if g.owner != p.char.ID { // 设定为非主人不得与 PET 互动
		msg, ok := data.ArgField(talk, "NoPlayerMsg")
		if !ok {
			msg = "陌生人？我不认识你呀！你是坏人！"
		}
		p.client.Send(&proto.NpcTalk{ID: o.id, Name: o.name, Text: msg})
		return
	}
	typ := 0
	if pet.Level >= pet.LimitLevel { // 上限是 0（不限）的宠物也算到了上限，照原版
		typ = 1
	}
	segs := strings.Split(talk, "OVER")
	var picks []string
	for _, seg := range segs {
		if !strings.Contains(seg, petTalkTypes[typ]) && !strings.Contains(seg, petTalkTypes[2]) {
			continue
		}
		if fl, ok := data.ArgField(seg, "FLOOR"); ok && !petTalkFloor(p.floor, fl) {
			continue
		}
		if cond, ok := data.ArgField(seg, "PET"); ok && !w.petTalkCheck(pet, cond) {
			continue
		}
		free, ok := data.ArgField(seg, "FREE")
		if !ok || !w.passCheck(p, free) {
			continue
		}
		w.petTalkFlags(p, seg) // PetTalk_CheckPetEvent：挑选时每个成立的段都会设旗标（资料里没有用到）
		picks = append(picks, seg)
		if len(picks) >= 8 { // PETTALK_MAXID
			break
		}
	}
	if len(picks) == 0 {
		for _, seg := range segs {
			if !strings.Contains(seg, "OTHER") {
				continue
			}
			if msg, ok := data.ArgField(seg, "TalkMsg"); ok {
				picks = strings.Split(msg, ",")
				break
			}
		}
	}
	if len(picks) == 0 {
		return
	}
	seg := picks[w.rnd.IntN(len(picks))]
	if !w.petTalkRun(p, o, pet, seg) {
		p.client.Send(&proto.NpcTalk{ID: o.id, Name: o.name, Text: "．．．．！"})
		return
	}
	msg, ok := data.ArgField(seg, "TalkMsg")
	if !ok {
		msg = seg // OTHER 挑到的就是那句话本身
	}
	w.sendWindow(p, o, "message", BtnOK, 0, npcText(msg))
}

// petTalkFloor 是 PetTalk_CheckMyFloor："!楼层" 是不在那层，否则要在那层。
func petTalkFloor(floor int, spec string) bool {
	if _, not, ok := strings.Cut(spec, "!"); ok {
		return floor != data.Atoi(not)
	}
	return floor == data.Atoi(spec)
}

// petTalkCheck 是 PetTalk_CheckFree(宠物, 宠物, 条件)：以 "," 分组、组内 "&" 全部成立，比的是宠物自己的等级、HP（最大 HP 的百分比）等。
func (w *World) petTalkCheck(pet *Pet, cond string) bool {
	for _, group := range strings.Split(cond, ",") {
		ok := true
		for _, c := range strings.Split(group, "&") {
			if !w.petTalkCond(pet, c) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// petTalkCond 是 PetTalk_BSCheck + PetTalk_FreeIfCheck 对宠物本身：LV、TRANS、HP 比较；宠物没有道具与旗标，那些一律不成立。
// "PET=" 的 flg 是 3，原版的比较没有这一种，永远不成立。
func (w *World) petTalkCond(pet *Pet, c string) bool {
	if l, _, ok := strings.Cut(c, "-"); ok {
		c = l
	}
	var name string
	var n, flg int
	switch {
	case strings.Contains(c, "<"):
		l, r, _ := strings.Cut(c, "<")
		name, n, flg = l, data.Atoi(r), cmpLess
	case strings.Contains(c, ">"):
		l, r, _ := strings.Cut(c, ">")
		name, n, flg = l, data.Atoi(r), cmpGreater
	case strings.Contains(c, "!"):
		l, r, _ := strings.Cut(c, "!=")
		return !w.petTalkIf(pet, l, data.Atoi(r), cmpEq)
	case strings.Contains(c, "="):
		l, r, _ := strings.Cut(c, "=")
		if strings.Contains(c, "PET") || strings.Contains(c, "*") {
			return false
		}
		name, n, flg = l, data.Atoi(r), cmpEq
	default:
		return false
	}
	return w.petTalkIf(pet, name, n, flg)
}

func (w *World) petTalkIf(pet *Pet, name string, n, flg int) bool {
	switch name {
	case "LV":
		return compare(n, pet.Level, flg)
	case "TRANS":
		return compare(n, pet.Trans, flg)
	case "HP": // PetTalk_CheckMyType：最大 HP × n / 100
		return compare(w.petMaxHP(pet)*n/100, pet.HP, flg)
	case "BBI":
		return compare(n, pet.Sprite, flg)
	}
	return false
}

// petTalkFlags 是 PetTalk_CheckPetEvent：段里的 EndSetFlg / NowSetFlg 设给玩家。
func (w *World) petTalkFlags(p *entity, seg string) {
	changed := false
	if v, ok := data.ArgField(seg, "EndSetFlg"); ok {
		for _, s := range strings.Split(v, ",") {
			p.setEventDone(data.Atoi(s))
			changed = true
		}
	}
	if v, ok := data.ArgField(seg, "NowSetFlg"); ok {
		for _, s := range strings.Split(v, ",") {
			p.setEventNow(data.Atoi(s))
			changed = true
		}
	}
	if changed {
		w.flushQuests(p)
		w.saveProgress(p)
	}
}

// petTalkRun 是 PetTalk_RunEvent：Action_RunDoEventAction（AddItem 等）、RandItem（RAND(0,10) > 9 才给）、LimitLevel。
func (w *World) petTalkRun(p, o *entity, pet *Pet, seg string) bool {
	if !w.runEventAction(p, seg) {
		return false
	}
	if v, ok := data.ArgField(seg, "RandItem"); ok {
		if w.rnd.IntN(11) <= 9 {
			return false
		}
		if !w.petTalkAddItems(p, o, v) {
			return true // PetTalk_AddItem 失败也照样往下（原版不看它的回传）
		}
	}
	if v, ok := data.ArgField(seg, "LimitLevel"); ok {
		pet.LimitLevel = data.Atoi(v)
	}
	return true
}

// petTalkAddItems 是 PetTalk_AddItem：先看空格够不够（不够宠物说"主人，你的物品栏已经满了！！"），再一件一件给。
func (w *World) petTalkAddItems(p, o *entity, spec string) bool {
	var defs []*data.ItemDef
	for _, s := range strings.Split(spec, ",") {
		if def := w.game.Items[data.Atoi(s)]; def != nil {
			defs = append(defs, def)
		}
	}
	if len(strings.Split(spec, ",")) > w.freeBagSlots(p) {
		p.client.Send(&proto.NpcTalk{ID: o.id, Name: o.name, Text: "主人，你的物品栏已经满了！！"})
		return false
	}
	for _, def := range defs {
		w.addItem(p.inv, w.newItem(def))
		w.sysMsg(p, "拿到"+def.Name, colorWhite)
	}
	w.itemsChanged(p)
	return true
}
