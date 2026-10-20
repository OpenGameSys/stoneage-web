package world

import (
	"encoding/json"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 寄放道具的店（npc_poolitemshop.c，_NPC_DEPOTITEM 未定义）：选单（寄放 / 取回 / 离开）→ 寄放的清单（身上的道具，每件收 cost 石币）
// 或取回的清单（寄放中的道具）。寄放中的道具存在 characters.pool（最多 CHAR_MAXPOOLITEMHAVE 件）。
// 回传照原版：寄放是"格位 + 1"，取回是"第几件"，"0" 回选单。

// 窗口编号（CHAR_WINDOWTYPE_POOLITEMSHOP_*）
const (
	seqPoolStart     = 290
	seqPoolPool      = 291
	seqPoolDraw      = 292
	seqPoolFull      = 293
	seqPoolItemFull  = 294
	maxPoolItems     = 30  // _ADD_POOL_ITEM
	defaultPoolCost  = 200 // NPC_POOLITEMSHOP_DEFAULT_COST
	poolShopMenuText = "3\n\n            欢迎光临\n\n          ＜寄放道具＞\n          ＜取回道具＞\n\n\n          ＜  离开  ＞"
)

func parsePool(s string) []*Item {
	var list []*Item
	if s != "" {
		json.Unmarshal([]byte(s), &list)
	}
	return list
}

func poolString(list []*Item) string {
	if len(list) == 0 {
		return ""
	}
	b, _ := json.Marshal(list)
	return string(b)
}

// poolMsg 是 NPC_PoolItemShop_getMsg_noarg：参数没写时用原版的预设文字。
func poolMsg(arg, key, def string) string {
	if v, ok := data.ArgField(arg, key); ok {
		return npcText(v)
	}
	return def
}

func poolCost(arg string) int {
	if v, ok := data.ArgField(arg, "cost"); ok {
		return data.Atoi(v)
	}
	return defaultPoolCost
}

// poolShopOpen 是 NPC_PoolItemShopTalked：面对面 2 格内或站在旁边，显示选单。
func (w *World) poolShopOpen(p, n *entity) {
	if !faceToFace(n, p, 2) && max(abs(n.x-p.x), abs(n.y-p.y)) > 1 {
		return
	}
	w.sendWindow(p, n, "select", 0, seqPoolStart, poolShopMenuText)
}

// poolable：登出消失、丢弃消失、不能宠物邮寄的道具不能寄放（poolflg）。
func poolable(def *data.ItemDef) bool {
	return def != nil && !def.DropAtLogout && !def.VanishDrop && def.CanPetMail
}

func (w *World) freeBagSlots(p *entity) int {
	n := 0
	for i := data.EquipSlots; i < data.ItemSlots; i++ {
		if p.inv[i] == nil {
			n++
		}
	}
	return n
}

// poolList 是 printWindow_Pool（寄放）/ printWindow_Draw（取回）。
func (w *World) poolList(p, n *entity, draw bool) {
	arg := n.npc.Arg
	pool := parsePool(p.char.Pool)
	m := &proto.ShopWindow{Seq: seqPoolPool, Mode: "pool", Title: n.name, Msg: poolMsg(arg, "main_msg", "欢迎"),
		Main: poolMsg(arg, "pool_main", "要寄放什麽呢"), Full: poolMsg(arg, "poolfull_msg", "超过的无法处理喔"),
		Realy: poolMsg(arg, "realy_msg", "真的要那个吗"), Rest: maxPoolItems - len(pool)}
	if draw {
		m.Seq, m.Mode, m.Main = seqPoolDraw, "draw", poolMsg(arg, "draw_main", "要领养什麽呢")
		m.Full, m.Rest = poolMsg(arg, "itemfull_msg", "项目有很多"), w.freeBagSlots(p)
		for i, it := range pool {
			def := w.game.Items[it.ID]
			if def == nil {
				continue
			}
			m.Items = append(m.Items, proto.ShopItem{Name: def.Name, Level: def.Level, Price: def.Cost, Image: def.Image,
				Info: def.Memo, Slot: i + 1, Pile: it.count(), Fame: -1})
		}
	} else {
		cost := poolCost(arg)
		for i := data.EquipSlots; i < data.ItemSlots; i++ {
			it := p.inv[i]
			if it == nil {
				continue
			}
			def := w.game.Items[it.ID]
			if def == nil {
				continue
			}
			m.Items = append(m.Items, proto.ShopItem{Name: def.Name, Level: def.Level, Price: cost, Image: def.Image,
				Info: def.Memo, Sealed: !poolable(def), Slot: i + 1, Pile: it.count(), Fame: -1})
		}
	}
	w.sendShop(p, n, m)
}

// poolShopReply 是 NPC_PoolItemShopWindowTalked。
func (w *World) poolShopReply(p, n *entity, seq int, sel string) {
	if max(abs(n.x-p.x), abs(n.y-p.y)) > 3 {
		return
	}
	pool := parsePool(p.char.Pool)
	switch seq {
	case seqPoolStart:
		switch data.Atoi(sel) {
		case 1:
			if len(pool) < maxPoolItems {
				w.poolList(p, n, false)
			} else {
				w.sendWindow(p, n, "message", BtnOK, seqPoolFull, "\n\n    超过的无法处理喔")
			}
		case 2:
			if w.freeBagSlots(p) > 0 {
				w.poolList(p, n, true)
			} else {
				w.sendWindow(p, n, "message", BtnOK, seqPoolItemFull, "\n\n    道具不是已经满了吗")
			}
		}
	case seqPoolPool, seqPoolDraw:
		k := data.Atoi(sel)
		if k == 0 {
			w.sendWindow(p, n, "select", 0, seqPoolStart, poolShopMenuText)
			return
		}
		ok := false
		if seq == seqPoolPool {
			ok = w.poolItem(p, n, k-1, pool)
		} else {
			ok = w.drawItem(p, k-1, pool)
		}
		if ok { // 原版不重发清单（客户端自己删掉那一项）；这里以服务端的结果重发
			w.poolList(p, n, seq == seqPoolDraw)
		}
	case seqPoolFull, seqPoolItemFull:
		w.sendWindow(p, n, "select", 0, seqPoolStart, poolShopMenuText)
	}
}

// poolItem 是 NPC_PoolItemShop_PoolItem：把道具栏 slot 的道具放进寄放，收 cost 石币。
// 原版不检查身上的钱（客户端挡），这里钱不够就不收。
func (w *World) poolItem(p, n *entity, slot int, pool []*Item) bool {
	if len(pool) >= maxPoolItems || slot < data.EquipSlots || slot >= data.ItemSlots || p.inv[slot] == nil {
		return false
	}
	cost := poolCost(n.npc.Arg)
	if !poolable(w.game.Items[p.inv[slot].ID]) || p.char.Gold < cost {
		return false
	}
	p.char.Gold -= cost
	p.char.Pool = poolString(append(pool, p.inv[slot]))
	p.inv[slot] = nil
	w.itemsChanged(p)
	return true
}

// drawItem 是 NPC_PoolItemShop_DrawItem：取回第 k 件放进第一个空格，后面的往前补。
func (w *World) drawItem(p *entity, k int, pool []*Item) bool {
	if k < 0 || k >= len(pool) {
		return false
	}
	for i := data.EquipSlots; i < data.ItemSlots; i++ {
		if p.inv[i] == nil {
			p.inv[i] = pool[k]
			p.char.Pool = poolString(append(pool[:k], pool[k+1:]...))
			w.itemsChanged(p)
			return true
		}
	}
	return false
}
