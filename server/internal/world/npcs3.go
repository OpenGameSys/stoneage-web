package world

import (
	"fmt"
	"strings"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 道具兑换（npc_itemchange.c，_ITEM_NPCCHANGE）、麦克风（npc_mic.c）、看动作的人（npc_action.c）。

// 窗口编号（NPC_ITEMCHANGE_*）
const (
	seqItemChangeSelect  = 446
	seqItemChangeMessage = 447
	seqItemChangeEnd     = 448
)

// itemChangeOpen 是 NPC_ItemchangeManTalked：START_MSG 的是 / 否。
func (w *World) itemChangeOpen(p, n *entity) {
	if !faceToFace(n, p, 2) && !inFront(p, n, 1) {
		return
	}
	if msg, ok := data.ArgField(n.npc.Arg, "START_MSG"); ok {
		w.sendWindow(p, n, "message", BtnYesNo, seqItemChangeSelect, npcText(msg))
	}
}

// itemChangeBlocks：参数用 "}" 分成几段，每段是一种兑换（CHANGEITEM / NeedItem / FREE / DelItem / AddItem …）。
func itemChangeBlocks(arg string) []string { return strings.Split(arg, "}") }

// itemChangeReply 是 NPC_ItemchangeManWindowTalked：是 → 选单（MenuHead + 每段的 CHANGEITEM 名称或 CHANGEMSG）→
// 选一项 → 需要的东西（NeedHead + NeedItem 名称，或 NeedMsg；DelGold 的石币）是 / 否 → 是：FREE 条件成立就照段里的动作兑换。
func (w *World) itemChangeReply(p, n *entity, win *npcWindow, button int, sel string) {
	if button == BtnNo || button == BtnCancel || !faceToFace(n, p, 2) && !inFront(p, n, 1) {
		return
	}
	arg := n.npc.Arg
	blocks := itemChangeBlocks(arg)
	switch win.seq {
	case seqItemChangeSelect: // ItemchangeMan_GetMenuStr
		head, ok := data.ArgField(arg, "MenuHead")
		if !ok {
			return
		}
		var sb strings.Builder
		sb.WriteString(npcText(head) + "\n")
		found := false
		for _, b := range blocks {
			if id, ok := data.ArgField(b, "CHANGEITEM"); ok {
				name := "未知的物品"
				if def := w.game.Items[data.Atoi(id)]; def != nil {
					name = def.Name
				}
				sb.WriteString("   " + name + "\n")
			} else if msg, ok := data.ArgField(b, "CHANGEMSG"); ok {
				sb.WriteString(npcText(msg) + "\n")
			} else {
				continue
			}
			found = true
		}
		if found {
			w.sendWindow(p, n, "select", BtnCancel, seqItemChangeMessage, sb.String())
		}
	case seqItemChangeMessage: // ItemchangeMan_GetNeedStr
		k := data.Atoi(sel)
		head, ok := data.ArgField(arg, "NeedHead")
		if !ok || k < 1 || k > len(blocks) {
			return
		}
		b := blocks[k-1]
		text := npcText(head) + "\n"
		if items, ok := data.ArgField(b, "NeedItem"); ok {
			for i, id := range strings.Split(items, ",") {
				name := "未知的物品"
				if def := w.game.Items[data.Atoi(id)]; def != nil {
					name = def.Name
				}
				text += name + "  "
				if i%3 == 0 {
					text += "\n"
				}
			}
		} else if msg, ok := data.ArgField(b, "NeedMsg"); ok {
			text = npcText(msg) + "  "
		} else {
			return
		}
		// 原版在 NeedItem 的字串里找 DelGold（永远找不到），这里照本意列出要付的石币
		if g, ok := data.ArgField(b, "DelGold"); ok && data.Atoi(g) > 0 {
			text += fmt.Sprintf("石币:%d \n", data.Atoi(g))
		}
		w.sendWindow(p, n, "message", BtnYesNo, seqItemChangeEnd, text)
		p.window.event = k // CHAR_WORKSHOPRELEVANT：选的是第几项
	case seqItemChangeEnd: // ActionNpc_CheckMenuFree
		k := win.event
		if k < 1 || k > len(blocks) || button != BtnYes {
			return
		}
		b := blocks[k-1]
		free, ok := data.ArgField(b, "FREE")
		if !ok {
			free = "LV>0"
		}
		if !w.passCheck(p, free) {
			w.npcSay(p, n, "条件不足！")
			return
		}
		if !w.runEventAction(p, b) {
			if msg, ok := data.ArgField(arg, "FAIL_MSG"); ok {
				w.npcSay(p, n, msg)
			}
		}
	}
}

// micTalk 是 NPC_MicTalked：对着麦克风（参数有 FREE 时不必面对）说的话，用黄字传给同一楼层（参数写了范围时只限范围内）的所有玩家。
func (w *World) micTalk(p, n *entity, text string) {
	arg := n.npc.Arg
	if !strings.Contains(arg, "FREE") && !inFront(p, n, 1) {
		return
	}
	floor, x1, y1, x2, y2, whole := n.floor, 0, 0, 0, 0, true
	if strings.Contains(arg, "|") {
		v := strings.Split(arg, "|")
		for len(v) < 8 {
			v = append(v, "")
		}
		floor, whole = data.Atoi(v[0]), false
		x1, x2 = min(data.Atoi(v[1]), data.Atoi(v[3])), max(data.Atoi(v[1]), data.Atoi(v[3]))
		y1, y2 = min(data.Atoi(v[2]), data.Atoi(v[4])), max(data.Atoi(v[2]), data.Atoi(v[4]))
	}
	msg := &proto.ChatMsg{Channel: "near", FromID: p.id, From: p.char.Name, Text: text, Color: colorYellow}
	for _, o := range w.floors[floor] {
		if o.client == nil || !whole && (o.x < x1 || o.x > x2 || o.y < y1 || o.y > y2) {
			continue
		}
		o.client.Send(msg)
		if strings.Contains(arg, "WIND") && o.fight == nil { // 还用窗口显示一次（CHAR_WINDOWTYPE_MICMESSAGE）
			o.client.Send(&proto.NpcTalk{ID: n.id, Name: n.name, Text: p.char.Name + "\n" + text, Window: true})
		}
	}
}

// actionTalk 是 NPC_ActionTalked：面对着说话回 normal 那句（对动作的回话在 action.go 的 actionWatch）。
func (w *World) actionTalk(p, n *entity) {
	if !inFront(p, n, 1) {
		return
	}
	if msg, ok := data.ArgField(n.npc.Arg, "normal"); ok {
		w.npcSay(p, n, msg)
	}
}
