package world

import (
	"fmt"
	"strconv"
	"strings"

	"sa/server/internal/data"
)

// 宠物店（npc_petshop.c）：卖掉宠物、寄放宠物、领回寄放的宠物。没有 _NPC_DEPOTPET（宠物仓库）。
// 选宠物用 petselect 窗口（WINDOW_MESSAGETYPE_PETSELECT：客户端列出自己的宠物），回传第几只（从 1 起）。

// 窗口编号（CHAR_WINDOWTYPE_WINDOWPETSHOP_*）
const (
	seqPetShopStart      = 261
	seqPetShopPetSelect  = 262
	seqPetShopMain       = 263
	seqPetShopGoldOver   = 264
	seqPetShopPetSelect2 = 265
	seqPetShopMain2      = 266
	seqPetShopDrawSelect = 267
	seqPetShopAskDraw    = 268
	seqPetShopEnd        = 269
)

const (
	PoolPetMax  = 15 // CHAR_MAXPOOLPETHAVE（_ADD_POOL_ITEM）
	petCostPer  = 10 // PETCOST
	poolPerPage = 5  // NPC_getDrawPet 的 LINENUM
)

// petShopMsgs 是 shopmsg[]：参数里没写时的预设文字。
var petShopMsgs = map[string]string{
	"over_msg": "钱用完罗!没问题吗?", "main_msg": "欢迎", "realy_msg": "真的可以吗?", "thanks_msg": "谢谢!",
	"cost_msg": "钱不够喔!", "pooltanks_msg": "谢谢!", "poolfull_msg": "超过的无法处理喔", "getfull_msg": "你不是有很多宠物吗",
}

// petShopMsg 是 NPC_MaxGoldOver：参数里的那句（没写时用预设），前面空两行。
func petShopMsg(n *entity, key string) string {
	s, ok := data.ArgField(n.npc.Arg, key)
	if !ok {
		s = petShopMsgs[key]
	}
	return "\n\n" + npcText(s)
}

// poolLimit：能寄放的数量是 转生次数×2 + 5（NPC_getDrawPet）。
func poolLimit(e *entity) int { return min(PoolPetMax, e.char.Trans*2+5) }

// poolFree 是 CHAR_getCharPoolPetElement：寄放栏的第一个空位，满了回 -1。
func poolFree(e *entity) int {
	if n := len(e.pets.Pool); n < poolLimit(e) {
		return n
	}
	return -1
}

func petShopPoolFlag(n *entity) bool {
	v, _ := data.ArgField(n.npc.Arg, "pool_flg")
	return data.Atoi(v) == 1
}

// petShopTalk 是 NPC_PetShopTalked：面对面 2 格内或相邻。
func (w *World) petShopTalk(p, n *entity) {
	if !faceToFace(p, n, 2) && max(abs(p.x-n.x), abs(p.y-n.y)) > 1 {
		return
	}
	w.petShopWindow(p, n, 0, -1, 0)
}

// petShopWindow 是 NPC_PetShop_selectWindow 的各个画面。rel 是 CHAR_WORKSHOPRELEVANT（之前选的那只或页数）。
func (w *World) petShopWindow(p, n *entity, num, sel, rel int) {
	pool := petShopPoolFlag(n)
	cost := 0
	switch num {
	case 0: // 选单
		text := "4\n　　　　　　☆宠物商店☆" + petShopMsg(n, "main_msg")
		if pool {
			text += "\n            《 寄放宠物 》\n            《 领取宠物 》\n　　　　　　《 卖掉宠物 》\n              《 离开 》"
		} else {
			text += "\n\n　　　　　 《 卖掉宠物 》\n\n             《 离开 》"
		}
		w.sendWindow(p, n, "select", 0, seqPetShopStart, text)
		return
	case 1: // 选要卖的宠物
		w.sendWindow(p, n, "petselect", BtnCancel, seqPetShopPetSelect, "")
		return
	case 2: // NPC_PetDate：这只卖多少
		pet := w.petAt(p, sel-1)
		if pet == nil {
			return
		}
		cost = w.petSellCost(p, n, pet)
		text := fmt.Sprintf("\n是[%s]没错吧？%s\n\n    买卖价格         %d STONE\n", pet.displayName(), petShopMsg(n, "realy_msg"), cost)
		w.sendWindow(p, n, "message", BtnYesNo, seqPetShopMain, text)
		rel = sel
	case 3: // 卖：超过持有上限时先问
		pet := w.petAt(p, rel-1)
		if pet == nil {
			return
		}
		if p.char.Gold+w.petSellCost(p, n, pet) > maxGold(p.char.Trans) {
			w.sendWindow(p, n, "message", BtnYesNo, seqPetShopGoldOver, petShopMsg(n, "over_msg"))
			break
		}
		w.sendWindow(p, n, "message", BtnOK, seqPetShopEnd, w.petSell(p, n, rel-1))
	case 4:
		w.sendWindow(p, n, "message", BtnOK, seqPetShopEnd, w.petSell(p, n, rel-1))
	case 5: // 选要寄放的宠物
		if !pool {
			return
		}
		w.sendWindow(p, n, "petselect", BtnCancel, seqPetShopPetSelect2, "")
		return
	case 6: // NPC_PetDate2：寄放的价钱
		if p.char.Gold < petPoolCost(p) {
			w.sendWindow(p, n, "message", BtnOK, seqPetShopMain2, petShopMsg(n, "cost_msg"))
			break
		}
		pet := w.petAt(p, sel-1)
		if pet == nil {
			return
		}
		text := fmt.Sprintf("\n寄放[%s]\n\n    寄放价格            %d stone\n", pet.displayName(), petPoolCost(p))
		w.sendWindow(p, n, "message", BtnYesNo, seqPetShopMain2, text)
		rel = sel
	case 7: // NPC_PetDel2：寄放
		if !pool {
			return
		}
		text := petShopMsg(n, "cost_msg")
		if p.char.Gold >= petPoolCost(p) {
			text = w.petPool(p, n, rel-1)
		}
		w.sendWindow(p, n, "message", BtnOK, seqPetShopEnd, text)
	case 8:
		w.sendWindow(p, n, "message", BtnOK, seqPetShopEnd, petShopMsg(n, "poolfull_msg"))
	case 9: // NPC_getDrawPet：寄放中的宠物，一页 5 只
		if !pool {
			return
		}
		page := rel + sel
		buttons, text := w.drawPetList(p, page)
		w.sendWindow(p, n, "select", buttons, seqPetShopDrawSelect, text)
		rel = page
	case 10: // NPC_PetDate3：领回哪一只
		idx := rel*poolPerPage + sel - 1
		if idx < 0 || idx >= len(p.pets.Pool) {
			return
		}
		w.sendWindow(p, n, "message", BtnYesNo, seqPetShopAskDraw, fmt.Sprintf("\n领回[%s]", p.pets.Pool[idx].displayName()))
		rel = idx
	case 11: // NPC_PetDel3：领回
		if !pool {
			return
		}
		w.sendWindow(p, n, "message", BtnOK, seqPetShopEnd, w.petDraw(p, n, rel))
	case 12:
		w.sendWindow(p, n, "message", BtnOK, seqPetShopEnd, petShopMsg(n, "getfull_msg"))
	}
	if p.window != nil {
		p.window.event, p.window.cost = rel, cost
	}
}

// petShopReply 是 NPC_PetShopWindowTalked。
func (w *World) petShopReply(p, n *entity, win *npcWindow, button int, text string) {
	sel := data.Atoi(text)
	pool := petShopPoolFlag(n)
	switch win.seq {
	case seqPetShopStart:
		switch {
		case sel == 3 && pool || sel == 2 && !pool: // 卖掉宠物
			w.petShopWindow(p, n, 1, -1, 0)
		case sel == 1 && pool: // 寄放宠物
			if poolFree(p) >= 0 {
				w.petShopWindow(p, n, 5, -1, 0)
			} else {
				w.petShopWindow(p, n, 8, -1, 0)
			}
		case sel == 2 && pool: // 领取宠物
			if p.pets.free() >= 0 {
				w.petShopWindow(p, n, 9, 0, 0)
			} else {
				w.petShopWindow(p, n, 12, -1, 0)
			}
		}
	case seqPetShopPetSelect:
		if button == BtnCancel {
			w.petShopWindow(p, n, 0, -1, 0)
		} else {
			w.petShopWindow(p, n, 2, sel, 0)
		}
	case seqPetShopPetSelect2:
		if button == BtnCancel {
			w.petShopWindow(p, n, 0, -1, 0)
		} else {
			w.petShopWindow(p, n, 6, sel, 0)
		}
	case seqPetShopMain:
		if button == BtnYes {
			w.petShopWindow(p, n, 3, -1, win.event)
		} else {
			w.petShopWindow(p, n, 1, -1, 0)
		}
	case seqPetShopMain2:
		if button == BtnYes {
			w.petShopWindow(p, n, 7, -1, win.event)
		} else {
			w.petShopWindow(p, n, 5, -1, 0)
		}
	case seqPetShopAskDraw:
		switch {
		case button != BtnYes:
			w.petShopWindow(p, n, 9, 0, 0)
		case p.pets.free() >= 0:
			w.petShopWindow(p, n, 11, -1, win.event)
		default:
			w.petShopWindow(p, n, 12, -1, 0)
		}
	case seqPetShopGoldOver:
		if button == BtnYes {
			w.petShopWindow(p, n, 4, -1, win.event)
		} else {
			w.petShopWindow(p, n, 0, -1, 0)
		}
	case seqPetShopDrawSelect:
		switch {
		case button == BtnPrev:
			w.petShopWindow(p, n, 9, -1, win.event)
		case button == BtnNext:
			w.petShopWindow(p, n, 9, 1, win.event)
		case button == BtnCancel:
			w.petShopWindow(p, n, 0, -1, 0)
		case p.pets.free() >= 0:
			w.petShopWindow(p, n, 10, sel, win.event)
		default:
			w.petShopWindow(p, n, 12, -1, 0)
		}
	case seqPetShopEnd:
		if button == BtnOK {
			w.petShopWindow(p, n, 0, -1, 0)
		}
	}
}

// petAt 是宠物栏第 i 格（越界或空的回 nil）。
func (w *World) petAt(e *entity, i int) *Pet {
	if i < 0 || i >= PetSlots {
		return nil
	}
	return e.pets.Pets[i]
}

// petPoolCost 是 NPC_GETPOOLCOST：寄放一只要 50 + 等级 × 4（参数的 pool_cost 原版没有用到）。
func petPoolCost(e *entity) int { return 50 + e.char.Level*4 }

// petSellCost 是 NPC_GetCost：((等级² − 捕获时等级²) × 10 + 等级 × 10) × 稀有度倍率（0→1、1→5、2→8），
// 转生过的宠物捕获等级那一项固定为 10；再乘店的倍率（special_pet 列出的形象用 special_rate），
// 最后乘 (主人魅力 + 宠物忠诚度) / 2 %（至少 10%），最多 1000000。
func (w *World) petSellCost(e *entity, n *entity, p *Pet) int {
	get := max(1, p.GetLevel)
	levelCost, getCost := p.Level*p.Level*10, get*get*10
	if p.Trans > 0 {
		getCost = 10
	}
	rare := p.Rare
	switch rare {
	case 0:
		rare = 1
	case 1:
		rare = 5
	case 2:
		rare = 8
	}
	cost := ((levelCost - getCost) + p.Level*petCostPer) * rare
	rate := argRate(n, "nomal_rate", 1.0)
	if specialPet(n, p.Sprite) {
		rate = argRate(n, "special_rate", 1.2)
	}
	charm := w.stats(e).Charm + w.petLoyalty(e, p)
	charm = max(20, charm) / 2
	cost = int(float64(cost) * rate)
	cost = int(float64(cost) * float64(charm) / 100)
	return min(cost, 1000000)
}

// argRate 是 NPC_PetShopInit 读倍率：atof 后取到千分位（×1000 存成整数）。
func argRate(n *entity, key string, def float64) float64 {
	s, ok := data.ArgField(n.npc.Arg, key)
	if !ok {
		return def
	}
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return float64(int(f*1000)) / 1000
}

// specialPet：special_pet 列出的形象编号（或 a-b 的范围）。
func specialPet(n *entity, sprite int) bool {
	s, _ := data.ArgField(n.npc.Arg, "special_pet")
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if a, b, ok := strings.Cut(part, "-"); ok {
			lo, hi := data.Atoi(a), data.Atoi(b)
			if lo > hi {
				lo, hi = hi, lo
			}
			if sprite >= lo && sprite <= hi {
				return true
			}
		} else if data.Atoi(part) == sprite {
			return true
		}
	}
	return false
}

// petSell 是 NPC_PetDel：卖掉第 i 格的宠物，出战中的改为没有出战。回传窗口文字。
func (w *World) petSell(e *entity, n *entity, i int) string {
	pet := w.petAt(e, i)
	if pet == nil || e.fight != nil {
		return ""
	}
	if e.pets.Ride == i {
		return "\n\n骑乘中的宠物无法贩卖喔！"
	}
	if pet.Away {
		return "\n\n送信中的宠物无法贩卖喔！"
	}
	cost := w.petSellCost(e, n, pet)
	w.removePet(e, i)
	e.char.Gold = min(maxGold(e.char.Trans), e.char.Gold+cost) // CHAR_AddGold
	w.petsChanged(e)
	w.sendInventory(e)
	return petShopMsg(n, "thanks_msg")
}

// petPool 是 NPC_PetDel2：把第 i 格的宠物寄放，收 poolCost。
func (w *World) petPool(e *entity, n *entity, i int) string {
	pet := w.petAt(e, i)
	if pet == nil || poolFree(e) < 0 {
		return ""
	}
	if e.pets.Ride == i {
		return "\n\n骑乘中的宠物无法寄放喔！"
	}
	if pet.Away {
		return "\n\n送信中的宠物无法寄放喔！"
	}
	if pet.Guard {
		return "\n\n家族守护兽无法存入仓库喔！"
	}
	w.removePet(e, i)
	e.pets.Pool = append(e.pets.Pool, pet)
	e.char.Gold -= petPoolCost(e)
	w.petsChanged(e)
	w.sendInventory(e)
	return petShopMsg(n, "pooltanks_msg")
}

// petDraw 是 NPC_PetDel3：领回寄放的第 idx 只，后面的往前补。
func (w *World) petDraw(e *entity, n *entity, idx int) string {
	if idx < 0 || idx >= len(e.pets.Pool) || e.pets.free() < 0 {
		return ""
	}
	w.addPet(e, e.pets.Pool[idx])
	e.pets.Pool = append(e.pets.Pool[:idx], e.pets.Pool[idx+1:]...)
	w.petsChanged(e)
	return petShopMsg(n, "pooltanks_msg")
}

// removePet 把宠物从宠物栏拿掉（出战的改为没有，待命位元清掉）。
func (w *World) removePet(e *entity, i int) {
	e.pets.Pets[i] = nil
	if e.pets.Battle == i {
		e.pets.Battle = -1
	}
	e.pets.Standby &^= 1 << i
}

// drawPetList 是 NPC_getDrawPet：第 page 页的寄放宠物（LV、MaxHP、名字），按钮按页数加上一页 / 下一页。
func (w *World) drawPetList(e *entity, page int) (int, string) {
	limit := poolLimit(e)
	end := (page + 1) * poolPerPage
	start := end - poolPerPage
	buttons := BtnCancel
	switch {
	case end >= limit:
		end = limit
		if page != 0 {
			buttons |= BtnPrev
		}
	case end <= poolPerPage:
		end = poolPerPage
		buttons |= BtnNext
	default:
		buttons |= BtnPrev | BtnNext
	}
	var sb strings.Builder
	sb.WriteString("2\n　　　　请选择宠物　　　　\n\n")
	for i := start; i < end; i++ {
		if i < len(e.pets.Pool) {
			p := e.pets.Pool[i]
			fmt.Fprintf(&sb, "LV.%3d MaxHP %3d %s\n", p.Level, w.petMaxHP(p), p.displayName())
		} else {
			sb.WriteString("\n")
		}
	}
	if end != limit && end >= len(e.pets.Pool) {
		buttons &^= BtnNext // 下一页没有宠物就不给"下一页"
	}
	return buttons, sb.String()
}
