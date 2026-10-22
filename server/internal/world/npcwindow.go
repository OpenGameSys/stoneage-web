package world

import (
	"fmt"
	"strconv"
	"strings"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// NPC 对话窗口（原版 lssproto_WN_send / WN_recv）：服务端发一个窗口（文字、可选行、按钮），
// 玩家点选后回传按钮与选择，服务端按窗口编号（seq）继续流程。
const (
	BtnOK     = 1 << 0 // WINDOW_BUTTONTYPE_OK
	BtnCancel = 1 << 1
	BtnYes    = 1 << 2
	BtnNo     = 1 << 3
	BtnPrev   = 1 << 4
	BtnNext   = 1 << 5
	BtnYesNo  = BtnYes | BtnNo
)

// 护士窗口编号（CHAR_WINDOWTYPE_WINDOWHEALER_*）
const (
	healStart = iota + 1
	healHP
	healMP
	healAll
	healOKHP
	healOKMP
	healOKAll
)

// npcWindow 记下玩家当前打开的 NPC 窗口，回传时核对。cost、event 是传送员记的价钱（-1 免费）与 TALKEVENT 段号
// （原版 CHAR_WORKSHOPRELEVANTTRD / CHAR_WORKSHOPRELEVANT）。
type npcWindow struct {
	npc, seq    int
	kind        string
	buttons     int
	cost, event int
	slot, pet   int   // 道具开的窗口（npc 为 0）：道具栏格位、宠物栏格位
	ids         []int // 选人的窗口（选队伍、选要加入的战斗）列出的玩家
}

func (w *World) sendWindow(p, n *entity, kind string, buttons, seq int, text string) {
	p.window = &npcWindow{npc: n.id, seq: seq, kind: kind, buttons: buttons}
	p.client.Send(&proto.NpcWindow{ID: n.id, Kind: kind, Buttons: buttons, Seq: seq, Text: text})
}

// NpcWindowReply 处理窗口的回传（NPC_WindowHealerWindowTalked、NPC_WarpManWindowTalked 等）。离 NPC 超过 范围+4 格不处理。
func (w *World) NpcWindowReply(charID, npcID, seq, button int, data string) {
	w.Do(func() {
		p := w.players[charID]
		if p == nil || p.window == nil || p.window.npc != npcID || p.window.seq != seq || p.fight != nil || p.trade != nil || p.stall != nil {
			return
		}
		win := p.window
		p.window = nil
		if win.npc == 0 {
			w.itemWindowReply(p, win, button, data)
			return
		}
		// 文字窗口只认窗口上有的按钮（原版不检查，OK 窗口回传"是"也会照"是"处理）
		if (win.kind == "message" || win.kind == "wideinput" || win.kind == "input") && button&^win.buttons != 0 {
			return
		}
		n := w.floors[p.floor][npcID]
		if n == nil || n.npc == nil || max(abs(n.x-p.x), abs(n.y-p.y)) > 1+4 {
			return
		}
		switch n.npc.Function {
		case "WindowHealer", "Healer":
			w.healerReply(p, n, seq, button, data)
		case "WarpMan":
			w.warpmanReply(p, n, win, button)
		case "Windowman":
			w.windowmanReply(p, n, seq, button, data)
		case "SavePoint":
			w.savePointReply(p, n, seq, button)
		case "ItemShop":
			w.shopReply(p, n, seq, button, data)
		case "PoolItemShop":
			w.poolShopReply(p, n, seq, data)
		case "Dengon":
			w.dengonReply(p, n, win, button, data)
		case "Bankman":
			w.bankmanReply(p, n, seq, button, data)
		case "Charm":
			w.charmReply(p, n, seq, button, data)
		case "LuckyMan":
			w.luckyReply(p, n, button)
		case "Duelranking":
			w.duelRankingReply(p, n, seq, button, data)
		case "Janken":
			w.jankenReply(p, n, seq, button, data)
		case "ExChangeMan":
			w.exchangeReply(p, n, win, button, data)
		case "NPCEnemy":
			w.npcEnemyReply(p, n, win, button, data)
		case "Quiz":
			w.quizReply(p, n, win, button, data)
		case "ItemchangeMan":
			w.itemChangeReply(p, n, win, button, data)
		case "Riderman":
			w.ridermanReply(p, n, seq, button, data)
		case "Familyman":
			w.familymanReply(p, n, seq, button, data)
		case "FmLetter":
			w.fmLetterReply(p, n, seq, button, data)
		case "ManorSman":
			w.manorSmanReply(p, n, win, button, data)
		case "Scheduleman":
			w.schedulemanReply(p, n, win, button, data)
		case "FMWarpMan":
			w.fmWarpManReply(p, n, win, button)
		case "FMPKMan":
			w.fmPkManReply(p, n, win, button, data)
		case "FMPKCallMan":
			w.fmPkCallManReply(p, n, win, button, data)
		case "PetShop":
			w.petShopReply(p, n, win, button, data)
		case "PetSkillShop":
			w.petSkillShopReply(p, n, data)
		}
	})
}

// 护士（npc_windowhealer.c）的原版文字。
const (
	healerStartText = " 　　　哎呀！你受伤了吗？　 \n\n " +
		"　　　　　　 ＜ 耐久力回复＞　　　　　 \n" +
		" 　　　　　　＜  气力回复 ＞　　　　　 \n" +
		" 　　　　 ＜ 耐久力·气力回复 ＞　　 \n\n" +
		" 　　　　　 ＜宠物回复(免费)＞　　　　 "
	healerNoNeed = "\n\n\n\n　　　　似乎没有必要回复唷！　"
	healerFree   = "\n\n　现在的等级可以免费帮你回复唷！"
)

// healerRange 是 NPC_WindowHealerInit 的 CHAR_WORK_RANGE：参数第 4 段（没写或 0 时为 1）。
// 原版的普通护士（npc_healer.c NPC_HealerTalked）固定 2 格。隔着柜台的护士参数多半写 2。
func healerRange(n *entity) int {
	if n.npc.Function == "Healer" {
		return 2
	}
	if len(n.npc.Args) >= 4 {
		if r := data.Atoi(n.npc.Args[3]); r > 0 {
			return r
		}
	}
	return 1
}

// healerRates 是 NPC_WindowHealerInit：参数第 1 段是开始收费的等级，第 2、3 段是耐久力、气力每级的价钱（atof，0 时为 0.5 / 2.0；
// 原版存成 ×1000 的整数）。普通护士（Healer）不收钱。
func healerRates(n *entity) (level int, hp, mp float64) {
	if n.npc.Function != "WindowHealer" {
		return 1 << 30, 0, 0
	}
	a := n.npc.Args
	rate := func(i int, def float64) float64 {
		if i >= len(a) {
			return def
		}
		f, _ := strconv.ParseFloat(strings.TrimSpace(a[i]), 64)
		if f == 0 {
			return def
		}
		return float64(int(f*1000)) / 1000
	}
	if len(a) > 0 {
		level = data.Atoi(a[0])
	}
	return level, rate(1, 0.5), rate(2, 2.0)
}

// healerCost 是 NPC_WindowCostCheck / NPC_WindowCostCheckMp：等级 × 每级的价钱，最少 1；等级不到收费等级时免费（回传 0）。
func healerCost(p, n *entity, hp, mp bool) int {
	level, hr, mr := healerRates(n)
	if p.char.Level < level {
		return 0
	}
	cost := 0
	if hp {
		cost += max(1, int(float64(p.char.Level)*hr))
	}
	if mp {
		cost += max(1, int(float64(p.char.Level)*mr))
	}
	return cost
}

// healerOpen 是 NPC_WindowHealer_selectWindow(0)：开始的选单。
func (w *World) healerOpen(p, n *entity) {
	w.sendWindow(p, n, "select", BtnCancel, healStart, healerStartText)
}

// healerReply：选单第 2 行耐久力、第 3 行气力、第 4 行两者、第 6 行宠物（免费）；
// 确认窗显示要收的石币（等级到护士参数的收费等级以上时，NPC_WindowCostCheck），按"是"付钱回复（钱不够显示"金钱不够"），
// "否"或回复完的 OK 回到选单。回复时宠物一起回复（NPC_WindowHealerAllHeal）。
func (w *World) healerReply(p, n *entity, seq, button int, data string) {
	c := &p.char
	maxHP, maxMP := w.maxHPMP(p)
	hpFull := c.HP < 0 || c.HP >= maxHP
	mpFull := c.MP >= maxMP
	// 收费等级以下的说法 / 要收钱的说法
	price := func(cost int, free string) string {
		if cost == 0 {
			return free + healerFree
		}
		return free + fmt.Sprintf("\n\n　　　　　收您%d的STONE 。", cost)
	}
	switch seq {
	case healStart:
		if button == BtnCancel {
			return
		}
		// 自己不必回复但宠物受伤时（NPC_PetHealerCheck）：先帮宠物回复
		noNeed := func(title string) string {
			if !w.petsHurt(p) {
				return title + healerNoNeed
			}
			w.healPets(p)
			return title + "\n\n　　　　      似乎没有必要回复唷！　\n\n　　　因为宠物好像也受伤了！\n                            先帮他回复吧！"
		}
		switch data {
		case "2":
			if hpFull {
				w.sendWindow(p, n, "message", BtnOK, healHP, noNeed("　　　　　　 ＜　耐久力回复　＞"))
			} else {
				w.sendWindow(p, n, "message", BtnYesNo, healHP, price(healerCost(p, n, true, false), "　　　　　　 ＜　耐久力回复　＞\n\n\n　　　　　　　是要回复耐久力没错吧！　　　 "))
			}
		case "3":
			if mpFull {
				w.sendWindow(p, n, "message", BtnOK, healMP, noNeed("　　　　　　  ＜　气力回复　＞"))
			} else {
				w.sendWindow(p, n, "message", BtnYesNo, healMP, price(healerCost(p, n, false, true), "　　　　　　  ＜ 气力回复＞\n\n　　　　　　　是要回复气力没错吧！　　　　 "))
			}
		case "4":
			if hpFull && mpFull {
				w.sendWindow(p, n, "message", BtnOK, healAll, noNeed("　　　　  ＜　耐久力气力回复　＞"))
			} else {
				w.sendWindow(p, n, "message", BtnYesNo, healAll, price(healerCost(p, n, !hpFull, !mpFull), "　　　　  ＜　耐久力气力回复　＞\n\n\n　　　　 　是要回复耐久力气力没错吧！"))
			}
		case "6": // 宠物回复（免费）
			if w.petsHurt(p) {
				w.healPets(p)
				w.sendWindow(p, n, "message", BtnOK, healOKHP, "　　　　　　　＜宠物回复＞　　　　　　\n\n\n　　　　　　　已经没问题了啦！　　　　　\n\n　　但是太勉强的话也不行唷！　　")
			} else {
				w.sendWindow(p, n, "message", BtnOK, healOKHP, "　　　　　　　＜宠物回复＞　　　　　　\n\n\n　 似乎没有必要回复宠物的样子。　\n\n　　但是太勉强的话也不行唷！　　")
			}
		}
	case healHP, healMP, healAll:
		switch button {
		case BtnYes:
			// NPC_WindowMoneyCheck：收钱（两者时只算没满的那项）
			cost := healerCost(p, n, seq != healMP && !hpFull, seq != healHP && !mpFull)
			if c.Gold < cost {
				w.sendWindow(p, n, "message", BtnOK, healOKHP, "\n\n\n\n 　　真可惜似乎所带的金钱不够唷！")
				return
			}
			text := ""
			switch seq {
			case healHP:
				c.HP = -1
				text = "\n　　　　　　＜耐久力已回复＞\n　　  ＜宠物也已回复一般状态＞\n\n\n　　　　　  这样子就没问题了！　　　　"
				seq = healOKHP
			case healMP:
				c.MP = maxMP
				text = "\n　　　　　　＜气力已回复＞\n　　 ＜宠物也已回复一般状态＞\n\n\n　　　　　 这样子就没问题了！ 　　　"
				seq = healOKMP
			default:
				c.HP, c.MP = -1, maxMP
				text = "　　　　 ＜耐久力气力已回复＞\n　　  ＜宠物也已回复一般状态＞\n\n\n　　　　这样一来就回复健康了！"
				seq = healOKAll
			}
			c.Gold -= cost
			w.healPets(p)
			w.sendStatus(p)
			if cost > 0 {
				w.sendInventory(p)
			}
			w.saveProgress(p)
			w.sendWindow(p, n, "message", BtnOK, seq, text)
		default:
			w.healerOpen(p, n)
		}
	case healOKHP, healOKMP, healOKAll:
		if button == BtnOK {
			w.healerOpen(p, n)
		}
	}
}
