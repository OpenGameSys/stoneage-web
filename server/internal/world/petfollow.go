package world

import "sa/server/internal/data"

// 溜宠（原版 item_event.c ITEM_petFollow、pet.c PET_dropPetFollow、char_walk.c 的 Robin petFollow、char_item.c CHAR_pickupFollowPet；
// 这台服务端没开 _PETFOLLOW_NEW_，一次只能溜一只）。
// 对宠物用宠物项圈（19011..19014）或玛蕾菲雅喜欢的项链（19624）等 ITEM_petFollow 道具：道具不会用掉，参数是能带的最高等级；
// 宠物离开宠物栏、放到身边的地上跟着主人走（主人每走一步它朝主人旁边那格走一步，走不过去就停着），主人被传送时传到同一格；
// 不像丢在地上的宠物会在 30 秒后消失。主人右键点它（捡起来）就收回，下线时也收回；跟着走了 1000 步自动收回。
// 跟它说话同地上的宠物（pettalk.go）。

// petFollowMaxSteps 是 CHAR_WORKPETFOLLOWCOUNT 的上限（没开 _SYSTEM_SPEAD 是 1000）：走超过就自动收回。
const petFollowMaxSteps = 1000

// usePetFollow 是 ITEM_petFollow：对第 petSlot 格的宠物用溜宠的道具。
func (w *World) usePetFollow(e *entity, def *data.ItemDef, petSlot int) {
	if o := e.follow; o != nil {
		if w.floors[o.floor][o.id] == o {
			w.sysMsg(e, "请先收回放出的宠物！", colorWhite)
			return
		}
		e.follow = nil
	}
	pet := e.pets.Pets[petSlot]
	if pet == nil {
		return
	}
	if pet.Level > argNum(" "+def.Argument, " ", 0) {
		w.sysMsg(e, "道具的等级不足！", colorWhite)
		return
	}
	if !w.dropPetFollow(e, petSlot) {
		w.sysMsg(e, "宠物跟随失败！", colorWhite)
	}
}

// dropPetFollow 是 PET_dropPetFollow：放在面前、脚下、再顺时针第一格能放的地方（都不行就脚下），设成跟随。
func (w *World) dropPetFollow(e *entity, slot int) bool {
	p := e.pets.Pets[slot]
	switch {
	case e.fight != nil:
		return false
	case p.Guard:
		w.sysMsg(e, "家族守护兽无法丢出！", colorYellow)
		return false
	case e.pets.Ride == slot:
		w.sysMsg(e, "骑乘中的宠物无法跟随！", colorYellow)
		return false
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
	e.follow = w.putGround(e.floor, x, y, "pet", p.displayName(), p.Sprite, &ground{pet: p, owner: e.char.ID, follow: true})
	w.petsChanged(e)
	return true
}

// petFollowStep 是主人走了一步之后（char_walk.c）：宠物朝"主人的位置往宠物那边退一格"走一步。
func (w *World) petFollowStep(e *entity) {
	o := e.follow
	if o == nil {
		return
	}
	if w.floors[o.floor][o.id] != o {
		e.follow = nil
		return
	}
	if o.floor != e.floor {
		return
	}
	ex, ey := e.x-sign(e.x-o.x), e.y-sign(e.y-o.y)
	dx, dy := sign(ex-o.x), sign(ey-o.y)
	if dx == 0 && dy == 0 {
		return
	}
	nx, ny := o.x+dx, o.y+dy
	if !w.game.Tiles.Walkable(w.game.Floors[o.floor], nx, ny) {
		return
	}
	o.x, o.y, o.dir = nx, ny, moveDir(dx, dy)
	w.broadcastMove(o)
	if o.ground.steps++; o.ground.steps > petFollowMaxSteps {
		o.ground.steps = 0
		w.pickupFollowPet(e, o)
	}
}

// petFollowWarp 是 CHAR_warpToSpecificPoint 里的 Robin petFollow：主人传送时跟着的宠物传到同一格。
func (w *World) petFollowWarp(e *entity) {
	o := e.follow
	if o == nil {
		return
	}
	if w.floors[o.floor][o.id] != o {
		e.follow = nil
		return
	}
	w.removeFromFloor(o)
	o.floor, o.x, o.y = e.floor, e.x, e.y
	w.addToFloor(o)
	w.announce(o)
}

// pickupFollowPet 是 CHAR_pickupFollowPet：收回跟着的宠物（主人右键点它、下线、走太久）。
func (w *World) pickupFollowPet(e, o *entity) bool {
	if e.follow == o {
		e.follow = nil
	}
	slot := e.pets.free()
	if slot < 0 {
		w.sysMsg(e, "宠物栏已满！无法拾回！", colorYellow)
		return false
	}
	if o.ground.owner != e.char.ID {
		return false
	}
	p := o.ground.pet
	w.removeGround(o)
	e.pets.Pets[slot] = p
	w.sysMsg(e, "拾回 "+p.displayName(), colorYellow)
	w.petsChanged(e)
	return true
}

// followPetOf 回传 e 现在跟着的宠物（不在地图上了就清掉）。
func (w *World) followPetOf(e *entity) *entity {
	if o := e.follow; o != nil && w.floors[o.floor][o.id] == o {
		return o
	}
	e.follow = nil
	return nil
}
