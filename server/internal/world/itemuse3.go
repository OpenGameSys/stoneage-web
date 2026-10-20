package world

import (
	"fmt"
	"strconv"
	"strings"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 第三批地图上的道具（item_event.c）：
//
//	ITEM_useRenameItem     魔术笔：给加工 / 料理做出来的道具改名（ITEM_useRenameItem_PrintWindow / _WindowResult）。
//	                       选单列出道具栏 0..19 格（含装备位）每页 5 个，能改的才显示名字；参数是剩下的次数（空的或 0 是无限）
//	ITEM_WaterWordStatus   鱼鳃草："time:60"：水中呼吸 N 分钟（不能累积）；在水中的地图上没有水中呼吸时每分钟扣血（CHAR_CheckWaterStatus）

// 道具开的窗口的 seq（ITEM_WINDOWTYPE_*）
const (
	renameSelectSeq = 10 // + 页数 0..3
	renameInputSeq  = 14
	renameErrorSeq  = 15
)

// renamable：能改名的道具（ITEM_MERGEFLG 合成出来的、不是料理、没有损坏）。
func (w *World) renamable(it *Item) bool {
	if it == nil || !it.Merged || it.Crush != 0 {
		return false
	}
	def := w.game.Items[it.ID]
	return def != nil && def.Type != data.ItemDish
}

// renameList 是改名的选单（第 page 页）。
func (w *World) renameList(e *entity, pen, page int) {
	var sb strings.Builder
	sb.WriteString("2\n   要变更那个项目的名称呢\n")
	sb.WriteString(fmt.Sprintf("                               Page:%d\n", page+1))
	for i := page * 5; i < page*5+5; i++ {
		if it := e.inv[i]; w.renamable(it) {
			sb.WriteString(w.itemName(it))
		}
		sb.WriteString("\n")
	}
	buttons := BtnCancel
	switch page {
	case 0:
		buttons |= BtnNext
	case 3:
		buttons |= BtnPrev
	default:
		buttons |= BtnPrev | BtnNext
	}
	e.window = &npcWindow{npc: 0, seq: renameSelectSeq + page, kind: "select", buttons: buttons, slot: pen, pet: page}
	e.client.Send(&proto.NpcWindow{ID: 0, Kind: "select", Buttons: buttons, Seq: renameSelectSeq + page, Text: sb.String()})
}

// itemName 是道具显示的名字（改过名的用改过的，ITEM_SECRETNAME）。
func (w *World) itemName(it *Item) string {
	if it.Name != "" {
		return it.Name
	}
	if def := w.game.Items[it.ID]; def != nil {
		return def.Name
	}
	return ""
}

func (w *World) renameInput(e *entity, pen, target int) {
	text := fmt.Sprintf("%s 名字要更改\n请输入姓名\n全形13个字, 半形26个字", w.itemName(e.inv[target]))
	e.window = &npcWindow{npc: 0, seq: renameInputSeq, kind: "input", buttons: BtnOK | BtnCancel, slot: pen, pet: target}
	e.client.Send(&proto.NpcWindow{ID: 0, Kind: "input", Buttons: BtnOK | BtnCancel, Seq: renameInputSeq, Text: text})
}

// renameReply 处理改名的三种窗口。
func (w *World) renameReply(e *entity, win *npcWindow, button int, text string) {
	pen := e.inv[win.slot]
	if pen == nil || w.game.Items[pen.ID] == nil || w.game.Items[pen.ID].UseFunc != "ITEM_useRenameItem" {
		return
	}
	switch {
	case win.seq >= renameSelectSeq && win.seq < renameSelectSeq+4:
		page := win.pet
		switch {
		case button&BtnNext != 0 && page < 3:
			w.renameList(e, win.slot, page+1)
		case button&BtnPrev != 0 && page > 0:
			w.renameList(e, win.slot, page-1)
		case button == 0:
			row, _ := strconv.Atoi(text)
			if t := page*5 + row - 1; row >= 1 && row <= 5 && w.renamable(e.inv[t]) {
				w.renameInput(e, win.slot, t)
			}
		}
	case win.seq == renameInputSeq:
		if button&BtnOK == 0 {
			return
		}
		msg := ""
		switch n := gbkLen(text); {
		case n > 26:
			msg = "超过文字限制"
		case n < 1:
			msg = "请输入一个文字以上"
		case strings.ContainsAny(text, " 　"):
			msg = "不可空白"
		case strings.Contains(text, "|"):
			msg = "不可有特殊符号"
		}
		target := e.inv[win.pet]
		if target == nil {
			return
		}
		if msg != "" {
			e.window = &npcWindow{npc: 0, seq: renameErrorSeq, kind: "message", buttons: BtnOK, slot: win.slot, pet: win.pet}
			e.client.Send(&proto.NpcWindow{ID: 0, Kind: "message", Buttons: BtnOK, Seq: renameErrorSeq, Text: msg})
			return
		}
		old := w.itemName(target)
		target.Name = text
		w.sysMsg(e, fmt.Sprintf("把 %s 命名成 %s ", old, text), colorYellow)
		// 笔的次数：参数是剩下几次，0 或空的是无限
		def := w.game.Items[pen.ID]
		left := data.Atoi(def.Argument)
		if pen.Arg != "" {
			left = data.Atoi(pen.Arg)
		}
		if left != 0 {
			if left--; left <= 0 {
				w.sysMsg(e, def.Name+" 消失了", colorYellow)
				e.inv[win.slot] = nil
			} else {
				pen.Arg = strconv.Itoa(left)
			}
		}
		w.itemsChanged(e)
	case win.seq == renameErrorSeq:
		w.renameInput(e, win.slot, win.pet)
	}
}

// ---------------- 水中呼吸 ----------------

// waterFloors 是 MAP_getMapFloorType 的水中地图（readmap.c）。
var waterFloors = map[int]bool{8101: true, 8015: true, 8029: true, 8028: true, 8027: true, 817: true, 8007: true, 8100: true, 151: true, 160: true, 161: true}

func (w *World) useWaterBreath(e *entity, slot int, def *data.ItemDef) {
	v, ok := data.ArgField(def.Argument, "time")
	minutes := data.Atoi(strings.TrimLeft(v, ":"))
	if !ok || minutes <= 0 {
		w.sysMsg(e, "什麽事也没发生。", colorYellow)
		return
	}
	if e.water > 0 {
		w.sysMsg(e, "水中呼吸时间不得累积。", colorYellow)
		return
	}
	e.water = minutes
	e.inv[slot] = nil // 整叠用掉
	w.sysMsg(e, fmt.Sprintf("水中呼吸时间增加%d分，总计%d分。", minutes, e.water), colorYellow)
	w.itemsChanged(e)
}

// tickWater 是 CHAR_CheckWaterStatus（每分钟）：水中呼吸减一（每 10 分钟提醒）；在水中的地图上没有水中呼吸时扣 3.3% HP，
// 快死时回到 1 并被送上岸。
func (w *World) tickWater(e *entity) {
	if e.water > 0 {
		if e.water--; e.water != 0 && e.water%10 == 0 {
			w.sysMsg(e, fmt.Sprintf("水中呼吸时间剩余%d分。", e.water), colorYellow)
		}
	}
	if e.fight != nil || !waterFloors[e.floor] || e.water > 0 {
		return
	}
	c := &e.char
	maxHP, _ := w.maxHPMP(e)
	hp := c.HP
	if hp < 0 {
		hp = maxHP
	}
	down := max(1, int(float64(maxHP)*0.033))
	if hp-down <= 0 {
		c.HP = 1
		if e.floor == 151 || e.floor == 160 || e.floor == 161 {
			w.placeOnFloor(e, 702, 213, 27, e.dir)
		} else {
			w.placeOnFloor(e, 200, 102, 1021, e.dir)
		}
	} else {
		c.HP = hp - down
		w.sysMsg(e, fmt.Sprintf("因无法呼吸，扣%d HP。", down), colorYellow)
	}
	w.sendStatus(e)
	w.saveProgress(e)
}

// ---------------- 拼图 ----------------
//
// ITEM_Jigsaw（这台服务端的源码里没有，取自编好的 gmsvjt：item_event.c 的 ITEM_Jigsaw 与 JigsawOK 表）：
// 小底板 Ａ..Ｉ（图 25151..25159）各有 9 块拼图（25160 起每块底板 9 张图），大底板（25150）的 9 块就是拼满的小底板；
// 底板的参数是 9 个 '0' / '1'（哪几块放进去了）。客户端打开底板后用拼图：ItemUse 的 target 是底板的格位。
// 放得进去（这块对得上这张底板、那一格还空着；小底板要拼满 "111111111" 才能放进大底板）就把那一格设成 '1'、
// 底板改名成"名字(放了几块)"、拼图用掉；放不进去什么也不说。拼满了也没有奖励（数据里没有用到完成的底板）。

const (
	jigsawBigBoard  = 25150 // 大底板的图
	jigsawLastPiece = 25240 // 最后一块拼图的图
)

// jigsawRow 是 JigsawOK 的一行：底板的图与 9 块的图。
func jigsawRow(board int) []int {
	if board == jigsawBigBoard {
		return []int{25151, 25152, 25153, 25154, 25155, 25156, 25157, 25158, 25159}
	}
	if board < 25151 || board > 25159 {
		return nil
	}
	row := make([]int, 9)
	for j := range row {
		row[j] = 25160 + 9*(board-25151) + j
	}
	return row
}

// jigsawArg 是底板现在的参数（GetJigsawString）。
func jigsawArg(it *Item, def *data.ItemDef) string {
	if def.Image < jigsawBigBoard || def.Image > 25159 {
		return ""
	}
	if it.Arg != "" {
		return it.Arg
	}
	return def.Argument
}

func (w *World) jigsaw(e *entity, boardSlot, pieceSlot int) {
	if boardSlot < data.EquipSlots || boardSlot >= data.ItemSlots || boardSlot == pieceSlot || e.inv[boardSlot] == nil {
		return
	}
	board, piece := e.inv[boardSlot], e.inv[pieceSlot]
	bdef, pdef := w.game.Items[board.ID], w.game.Items[piece.ID]
	if bdef == nil || pdef == nil {
		return
	}
	arg := []byte(jigsawArg(board, bdef))
	if len(arg) < 9 {
		return
	}
	at := -1
	for j, img := range jigsawRow(bdef.Image) {
		if img == pdef.Image && arg[j] == '0' {
			at = j
			break
		}
	}
	if at < 0 {
		return
	}
	if jigsawRow(pdef.Image) != nil && jigsawArg(piece, pdef) != "111111111" {
		return // 小底板要拼满才能放进大底板
	}
	arg[at] = '1'
	name := w.itemName(board)
	if i := strings.Index(name, "("); i >= 0 {
		name = name[:i]
	}
	board.Name = fmt.Sprintf("%s(%d)", name, strings.Count(string(arg), "1"))
	board.Arg = string(arg)
	e.inv[pieceSlot] = nil
	w.itemsChanged(e)
}
