package world

import (
	"fmt"
	"strings"
	"time"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 地上的道具、石币与宠物（char_item.c CHAR_DropItem / CHAR_DropMoney / CHAR_PickUpItem，pet.c _PET_dropPet）。
// 它们是楼层上 kind 为 "item" / "gold" / "pet" 的实体：道具与石币的 sprite 是地图图号（石币按数量 24050 / 24051 / 24052），宠物是它的形象。
// 丢东西不在自己脚下：道具从面前那格起顺时针找第一格能走、没有道具与石币的；石币先面前、再脚下、再顺时针，同一格的石币合起来；
// 宠物先面前、再脚下、再顺时针，找没有任何东西的格子，都没有就放在脚下。
// 捡东西（右键点身边一格，lssproto_PI_recv）先看面前那格、再看脚下，每次只捡一样。setup.cf 的 ITEMUP=1：道具谁都能捡；
// PETUP=0：宠物只有丢的人能捡回来。放在地上的时间：道具 Itemdeletetime 20 秒、石币 Golddeletetime 1800 秒、宠物 Petdeletetime 30 秒，
// 到了就消失（原版在附近有人动作时才检查，这里每 5 秒检查一次）；服务端重开时地上的东西都没了（原版也不存）。
// 原版这台服务端缺 data/ablua/freedrop.lua，FreeDropItem 永远成立，丢道具一律跳出"特殊物品"的确认后直接消失；
// 这里照它本来的设计把道具放到地上（名字以 * 开头的道具才直接消失）。

const (
	groundIDBase   = 2_000_000
	itemDeleteTime = 20 * time.Second
	goldDeleteTime = 1800 * time.Second
	petDeleteTime  = 30 * time.Second
	// 石币的图（setMoneyCharObj）：> 10000 / > 1000 / 其他
	goldBig, goldMid, goldSmall = 24050, 24051, 24052
)

// ground 是地上的东西。
type ground struct {
	item    *Item
	gold    int
	pet     *Pet
	owner   int64 // 丢的人（宠物只有他能捡）
	putTime time.Time
}

func goldSprite(n int) int {
	switch {
	case n > 10000:
		return goldBig
	case n > 1000:
		return goldMid
	}
	return goldSmall
}

// groundAt 是 (x, y) 上的地上物（按放下的先后，CHAR_PickUpItemFXY 照格子上的串列顺序找）。
func (w *World) groundAt(floor, x, y int) []*entity {
	var out []*entity
	for _, o := range w.floors[floor] {
		if o.ground != nil && o.x == x && o.y == y {
			out = append(out, o)
		}
	}
	for i := 1; i < len(out); i++ { // 按 id（放下的顺序）排
		for j := i; j > 0 && out[j].id < out[j-1].id; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// tileFree：能走，而且没有 kinds 里的东西（"item" / "gold" / "pet" / "char"）。
func (w *World) tileFree(floor, x, y int, kinds ...string) bool {
	f := w.game.Floors[floor]
	if f == nil || !w.game.Tiles.Walkable(f, x, y) {
		return false
	}
	for _, o := range w.floors[floor] {
		if o.x != x || o.y != y || o.hidden() {
			continue
		}
		k := o.kind
		if k == "player" || k == "npc" {
			k = "char"
		}
		for _, want := range kinds {
			if k == want {
				return false
			}
		}
	}
	return true
}

// putGround 把地上物放到 (x, y) 并让附近的人看到（CHAR_sendWatchEvent）。
func (w *World) putGround(floor, x, y int, kind, name string, sprite int, g *ground) *entity {
	w.groundSeq++
	g.putTime = w.now()
	o := &entity{id: groundIDBase + w.groundSeq, kind: kind, name: name, sprite: sprite, floor: floor, x: x, y: y, ground: g}
	if g.pet != nil {
		o.level = g.pet.Level
	}
	w.addToFloor(o)
	w.announce(o)
	return o
}

// ItemDrop 是 lssproto_DI_recv → CHAR_DropItem：一次只丢一个（堆叠的只拿出一个）。
func (w *World) ItemDrop(charID, slot int) {
	w.Do(func() {
		if e := w.players[charID]; e != nil {
			w.dropItem(e, slot)
		}
	})
}

func (w *World) dropItem(e *entity, slot int) {
	if e.busy() || slot < 0 || slot >= data.ItemSlots || e.inv[slot] == nil {
		return
	}
	it := e.inv[slot]
	def := w.game.Items[it.ID]
	if def == nil {
		return
	}
	take := func() *Item {
		if it.count() > 1 {
			it.Pile = it.count() - 1
			one := *it
			one.Pile = 0
			return &one
		}
		e.inv[slot] = nil
		return it
	}
	if strings.HasPrefix(def.Name, "*") { // _LOCK_PET_ITEM：特殊物品丢了直接消失
		take()
		w.sysMsg(e, "["+def.Name+"]物品已消失。", colorYellow)
		w.itemsChanged(e)
		return
	}
	x, y, ok := w.dropTile(e, false, "item", "gold")
	if !ok {
		w.sysMsg(e, "周围的地面已经满了。", colorYellow)
		return
	}
	one := take()
	if def.VanishDrop { // ITEM_eventDrop：ITEM_VANISHATDROP
		w.sysMsg(e, def.Name+" 消灭了。", colorWhite)
	} else {
		w.putGround(e.floor, x, y, "item", def.Name, def.Image, &ground{item: one, owner: e.char.ID})
	}
	w.itemsChanged(e)
}

// dropTile 找丢东西的格子：面前那格起顺时针（CHAR_FindAroundUsabilitySpace）；own 时第二个试脚下（丢石币、宠物）。
func (w *World) dropTile(e *entity, own bool, kinds ...string) (int, int, bool) {
	try := func(x, y int) bool { return w.tileFree(e.floor, x, y, kinds...) }
	for k := 0; k < 8; k++ {
		m := MOVE[(e.dir+k)%8]
		if try(e.x+m[0], e.y+m[1]) {
			return e.x + m[0], e.y + m[1], true
		}
		if k == 0 && own && try(e.x, e.y) {
			return e.x, e.y, true
		}
	}
	return 0, 0, false
}

// GoldDrop 是 lssproto_DG_recv → CHAR_DropMoney：同一格已经有石币时合起来（合起来不能超过金钱上限），有道具的格子不放；
// 都放不下时最后硬放在脚下。
func (w *World) GoldDrop(charID, amount int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || e.busy() || amount <= 0 || amount > e.char.Gold {
			return
		}
		limit := maxGold(e.char.Trans)
		put := func(x, y int, force bool) bool {
			f := w.game.Floors[e.floor]
			if f == nil || !w.game.Tiles.Walkable(f, x, y) {
				return false
			}
			for _, o := range w.groundAt(e.floor, x, y) {
				switch o.kind {
				case "gold":
					if o.ground.gold+amount > limit {
						return false
					}
					o.ground.gold += amount
					o.ground.putTime = w.now()
					o.name, o.sprite = fmt.Sprintf("%d Stone", o.ground.gold), goldSprite(o.ground.gold)
					w.regroundView(o)
					return true
				case "item":
					if !force {
						return false
					}
				}
			}
			w.putGround(e.floor, x, y, "gold", fmt.Sprintf("%d Stone", amount), goldSprite(amount), &ground{gold: amount, owner: e.char.ID})
			return true
		}
		ok := false
		for k := 0; k < 8 && !ok; k++ {
			m := MOVE[(e.dir+k)%8]
			ok = put(e.x+m[0], e.y+m[1], false) || k == 0 && put(e.x, e.y, false)
		}
		if !ok && !put(e.x, e.y, true) {
			return
		}
		e.char.Gold -= amount
		w.sendInventory(e)
		w.saveProgress(e)
	})
}

// regroundView：地上物的名字或图变了，看得到的人重新收一次（原版重发 C）。
func (w *World) regroundView(o *entity) {
	for _, p := range w.floors[o.floor] {
		if p.client != nil && p.visible[o.id] {
			p.client.Send(&proto.ObjDisappear{IDs: []int{o.id}})
			p.client.Send(&proto.ObjAppear{Objs: []proto.ObjInfo{o.info()}})
		}
	}
}

// dropPet 是 _PET_dropPet：把第 slot 格的宠物放到地上（骑着的、绑定的不行；出战的改为没有出战）。
func (w *World) dropPet(e *entity, slot int) {
	p := e.pets.Pets[slot]
	switch {
	case e.pets.Ride == slot:
		w.sysMsg(e, "骑乘中的宠物无法丢出！", colorYellow)
		return
	case strings.HasPrefix(p.Name, "*") || strings.HasPrefix(p.FreeName, "*"):
		w.sysMsg(e, "绑定宠物无法丢出！", colorYellow)
		return
	}
	// _DROPCHECK2：周围 21×21 里地上的东西或角色超过 80 个
	things, chars := 0, 0
	for _, o := range w.floors[e.floor] {
		if abs(o.x-e.x) <= 10 && abs(o.y-e.y) <= 10 {
			if o.kind == "item" || o.kind == "gold" {
				things++
			} else {
				chars++
			}
		}
	}
	if things > 80 || chars > 80 {
		w.sysMsg(e, "这里已经太拥挤了，不能再丢了。", colorYellow)
		return
	}
	x, y, ok := w.dropTile(e, true, "item", "gold", "pet", "char")
	if !ok {
		x, y = e.x, e.y
	}
	if e.pets.Battle == slot {
		e.pets.Battle = -1
	}
	e.pets.Pets[slot] = nil
	e.pets.Standby &^= 1 << slot
	w.putGround(e.floor, x, y, "pet", p.displayName(), p.Sprite, &ground{pet: p, owner: e.char.ID})
	w.petsChanged(e)
}

// PickUp 是 lssproto_PI_recv → CHAR_PickUpItem：dir < 0 只看脚下；否则先转向 dir，看面前那格、再看脚下。
func (w *World) PickUp(charID, dir int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || e.busy() || dir >= 8 {
			return
		}
		tiles := [][2]int{{e.x, e.y}}
		if dir >= 0 {
			if dir != e.dir {
				e.dir = dir
				for _, o := range w.floors[e.floor] {
					if o != e && o.client != nil && o.visible[e.id] {
						o.client.Send(&proto.ObjTurn{ID: e.id, Dir: dir})
					}
				}
			}
			m := MOVE[dir]
			tiles = [][2]int{{e.x + m[0], e.y + m[1]}, {e.x, e.y}}
		}
		for _, t := range tiles {
			for _, o := range w.groundAt(e.floor, t[0], t[1]) {
				if w.pickOne(e, o) {
					return
				}
			}
		}
	})
}

// pickOne 是 CHAR_PickUpItemFXY 的一样东西；回传 true 表示这次到此为止（捡到了或不能捡的讯息已经说了）。
func (w *World) pickOne(e *entity, o *entity) bool {
	g := o.ground
	switch o.kind {
	case "item":
		free := -1
		for i := data.EquipSlots; i < data.ItemSlots; i++ {
			if e.inv[i] == nil {
				free = i
				break
			}
		}
		if free < 0 {
			w.sysMsg(e, "道具栏已满。", colorWhite)
			return true
		}
		e.inv[free] = g.item // 捡起来不会和同样的道具叠在一起
		w.removeGround(o)
		w.sysMsg(e, "拾获 "+o.name, colorWhite)
		w.itemsChanged(e)
	case "gold":
		limit := maxGold(e.char.Trans)
		if e.char.Gold < 0 || e.char.Gold > limit {
			w.sysMsg(e, "道具栏已满。", colorWhite)
			return true
		}
		n := min(g.gold, limit-e.char.Gold)
		if n <= 0 {
			w.sysMsg(e, "无法再拾获Stone了。", colorWhite)
			return true
		}
		e.char.Gold += n
		if g.gold -= n; g.gold <= 0 {
			w.removeGround(o)
		} else {
			o.name, o.sprite = fmt.Sprintf("%d Stone", g.gold), goldSprite(g.gold)
			w.regroundView(o)
		}
		w.sysMsg(e, fmt.Sprintf("拾获 %d Stone", n), colorWhite)
		w.sendInventory(e)
		w.saveProgress(e)
	case "pet":
		p := g.pet
		slot := e.pets.free()
		switch {
		case slot < 0:
			w.sysMsg(e, "宠物数已满。", colorWhite)
		case p.Level-e.char.Level > 20 && e.char.Trans <= 0:
			w.sysMsg(e, "无法照顾该宠物。", colorWhite)
		case g.owner != e.char.ID: // PETUP=0：主人的帐号与角色名都要相同
			w.sysMsg(e, "无法拾获该宠物。", colorWhite)
		default:
			e.pets.Pets[slot] = p
			w.removeGround(o)
			w.sysMsg(e, "拾获 "+p.displayName(), colorWhite)
			w.petsChanged(e)
		}
	default:
		return false
	}
	return true
}

func (w *World) removeGround(o *entity) {
	w.removeFromFloor(o)
}

// sweepGround 让放太久的地上物消失（ITEM_DeleteTimeWatched / GOLD_DeleteTimeCheckLoop / PET_CHECKFreePetIsIt）。
func (w *World) sweepGround() {
	now := w.now()
	for _, fl := range w.floors {
		for _, o := range fl {
			g := o.ground
			if g == nil {
				continue
			}
			life := w.Rules.GroundItem
			switch o.kind {
			case "gold":
				life = w.Rules.GroundGold
			case "pet":
				life = w.Rules.GroundPet
			}

			if now.Sub(g.putTime) >= life {
				w.removeGround(o)
			}
		}
	}
}
