package world

import (
	"fmt"
	"strings"

	"sa/server/internal/data"
)

// 第二批地图上的道具（item_event.c）：
//
//	ITEM_MysteriousGift   "N|敌人编号|…"：随机一种敌人变成宠物给你（同捕获）；宠物栏满了不用掉
//	ITEM_Refresh          地图上没有作用（"什麽也没发生。"），整叠用掉
//	ITEM_Luck             凶 / 一般 / 小吉 / 中吉 / 大吉 → 运气 1..5
//	ITEM_Gold             加石币（超过上限的没了）；ITEM_AddFame 加声望
//	ITEM_useSkup          可分配点数 +1
//	ITEM_useNoenemy       驱魔：约 6 分钟不遇敌；ITEM_useEncounter 诱魔：原地每隔几秒遇敌，走动或传送就停
//	ITEM_useDeathcounter  奇怪的石头 / 恶魔宝石：原地遇敌（石头只在参数的楼层有效，宝石有次数）
//	ITEM_useMaxRedSocks / New  圣诞袜：随机给一样礼物
//	ITEM_changePetOwner   原版解除交易来的宠物的改名锁；这里的宠物都是自己的，只说话
// 乌力化（PETSKILL_BecomePig）的地图部分也在这里：characters.pig 是剩下的秒数。

// pigSprite 是乌力的形象（CHAR_BECOMEPIG_BBI 的预设）。
const pigSprite = 100250

func (w *World) useItem2(e *entity, slot int, def *data.ItemDef, pet *Pet) {
	switch def.UseFunc {
	case "ITEM_MysteriousGift":
		w.mysteriousGift(e, slot, def)
	case "ITEM_Refresh":
		w.sysMsg(e, "什麽也没发生。", colorWhite)
		e.inv[slot] = nil // 原版地图上也把整叠用掉
		w.itemsChanged(e)
	case "ITEM_Luck":
		w.useLuck(e, slot, def)
	case "ITEM_Gold":
		n := data.Atoi(def.Argument)
		e.char.Gold = min(maxGold(e.char.Trans), e.char.Gold+n)
		w.sysMsg(e, fmt.Sprintf("你的石币增加%dS", n), colorYellow)
		w.useUp(e, slot)
		w.itemsChanged(e)
	case "ITEM_AddFame":
		n, old := data.Atoi(def.Argument), e.char.Fame
		w.earnFame(e, n*100)
		w.sysMsg(e, fmt.Sprintf("声望点数提升%d,目前你的声望是%d!", n, old/100+n), colorYellow)
		w.useUp(e, slot)
		w.itemsChanged(e)
	case "ITEM_useSkup":
		e.char.Points++ // CHAR_SKILLUPPOINT + 1
		w.sysMsg(e, "你感受到自己的能力被提升了。", colorWhite)
		w.useUp(e, slot)
		w.itemsChanged(e)
	case "ITEM_useNoenemy":
		e.noenemy = 6 // setNoenemy：重设为 6（每分钟减一），不累加
		w.sysMsg(e, "你感受到周边的杀气消失了。", colorWhite)
		w.useUp(e, slot)
		w.itemsChanged(e)
	case "ITEM_useEncounter":
		e.stay = true
		w.sysMsg(e, "你感受到周边突然充满杀气！", colorYellow)
		w.useUp(e, slot)
		w.itemsChanged(e)
	case "ITEM_useDeathcounter":
		w.deathCounter(e, slot, def)
	case "ITEM_useMaxRedSocks":
		w.redSocks(e, slot, def)
	case "ITEM_useMaxRedSocksNew":
		w.redSocksNew(e, slot, def)
	case "ITEM_useRenameItem":
		w.renameList(e, slot, 0) // 先不用掉，改名成功才扣次数
	case "ITEM_WaterWordStatus":
		w.useWaterBreath(e, slot, def)
	case "ITEM_changePetOwner":
		if pet == nil {
			w.sysMsg(e, "这只能用在宠物身上喔。", colorYellow)
		} else {
			w.sysMsg(e, "这只宠物本来就是你的，并不需要使用这个呀。", colorYellow)
		}
	}
}

// tickPig 是 CONNECT_SysEvent_Loop 的乌力化计时：不在战斗中每 10 秒减 1 并报时；每 60 秒（战斗中也算）减 10，
// 到 0 以下时（不在战斗中）变回来："乌力化失效了。"。
func (w *World) tickPig(e *entity, minute bool) {
	c := &e.char
	if c.Pig <= 0 {
		return
	}
	if !minute {
		if e.fight == nil {
			c.Pig--
			w.sysMsg(e, fmt.Sprintf("乌力时间:%d秒", c.Pig), colorWhite)
		}
		return
	}
	if c.Pig-1 <= 0 {
		c.Pig = 0
		if e.fight == nil {
			c.Pig = -1
			w.updateSprite(e)
			w.sysMsg(e, "乌力化失效了。", colorWhite)
			w.saveProgress(e)
		}
		return
	}
	c.Pig -= 10
}

func (w *World) mysteriousGift(e *entity, slot int, def *data.ItemDef) {
	p := strings.Split(def.Argument, "|")
	n := min(20, data.Atoi(p[0]), len(p)-1)
	if n <= 0 || w.game.Battle == nil {
		return
	}
	if e.pets.free() < 0 {
		w.sysMsg(e, "宠物已满！！", colorYellow)
		return
	}
	en := w.game.Battle.Enemies[data.Atoi(p[1+w.rnd.IntN(n)])]
	if en == nil {
		return
	}
	pet := w.petFromEnemy(en)
	w.addPet(e, pet)
	w.sysMsg(e, "拿到"+pet.Name, colorYellow)
	w.useUp(e, slot)
	w.petsChanged(e)
	w.itemsChanged(e)
}

func (w *World) useLuck(e *entity, slot int, def *data.ItemDef) {
	names := []string{"凶", "一般", "小吉", "中吉", "大吉"}
	i := 0 // 原版找不到时越界成 6，这里当作凶
	for k, s := range names {
		if strings.Contains(def.Argument, s) {
			i = k
			break
		}
	}
	e.luck = i + 1
	w.sysMsg(e, "你现在的运气为"+names[i], colorYellow)
	w.useUp(e, slot)
	w.itemsChanged(e)
}

// deathCounter 是 ITEM_UseDeathCounter：奇怪的石头（参数是楼层）只在那些楼层原地遇敌；恶魔宝石（没有参数）有次数（damagebreak）。
func (w *World) deathCounter(e *entity, slot int, def *data.ItemDef) {
	it := e.inv[slot]
	spend := func(gone string) {
		if def.Uses == -1 || def.Uses-it.Used-1 < 1 {
			if gone != "" {
				w.sysMsg(e, gone, colorYellow)
			}
			w.useUp(e, slot)
		} else {
			it.Used++
		}
		w.itemsChanged(e)
	}
	if def.Argument != "" {
		ok := false
		for _, s := range strings.Split(def.Argument, "|") {
			ok = ok || data.Atoi(s) == e.floor
		}
		spend(def.Name + "消失了。")
		if !ok {
			w.sysMsg(e, "没有发生任何事情！", colorYellow)
			return
		}
		e.stay = true
		w.sysMsg(e, "你身边笼罩阴暗气息！", colorYellow)
		return
	}
	if def.Uses == -1 {
		spend("")
		return
	}
	spend("道具 " + def.Name + "消失了。")
	e.stay = true
	w.sysMsg(e, "你感受到周边突然充满杀气！", colorYellow)
}

// redSocks 是 ITEM_useMaxRedSocks：参数是可以用的那一天（unix 时间，可用 24 小时），期间内随机给一样礼物。
func (w *World) redSocks(e *entity, slot int, def *data.ItemDef) {
	start := int64(data.Atoi(def.Argument))
	now := w.now().Unix()
	switch {
	case now < start:
		d := start - now
		w.sysMsg(e, fmt.Sprintf("%s还差%d天%d小时%d分%d秒才可使用!", def.Name, d/86400, d%86400/3600, d%3600/60, d%60), colorYellow)
		return
	case now > start+86400:
		w.sysMsg(e, def.Name+"使用期限已过!", colorYellow)
		return
	}
	present := []int{13061, 13062, 13063, 13064, 13088, 13089, 13090, 13091, 14756, 17256, 13092, 19692, 20594}
	r, k := w.rnd.IntN(100), w.rnd.IntN(8)
	switch {
	case r > 70:
		k = 10 + w.rnd.IntN(3)
	case r > 60:
		k = 8 + w.rnd.IntN(2)
	}
	e.inv[slot] = nil
	w.giveGift(e, present[k])
}

// redSocksNew 是 ITEM_useMaxRedSocksNew："N|道具编号|…"：先用掉整叠，再随机给其中一样。
func (w *World) redSocksNew(e *entity, slot int, def *data.ItemDef) {
	p := strings.Split(def.Argument, "|")
	n := min(20, data.Atoi(p[0]), len(p)-1)
	e.inv[slot] = nil
	if n <= 0 {
		w.itemsChanged(e)
		return
	}
	w.giveGift(e, data.Atoi(p[1+w.rnd.IntN(n)]))
}

// giveGift：礼物（ITEM_makeItemAndRegist + CHAR_addItemSpecificItemIndex）；放不下时袜子照样没了。
func (w *World) giveGift(e *entity, id int) {
	gd := w.game.Items[id]
	switch {
	case gd == nil:
		w.sysMsg(e, "礼物无效!", colorWhite)
	case w.addItem(e.inv, w.newItem(gd)) < 0:
		w.sysMsg(e, "礼物置放栏位错误!", colorWhite)
	default:
		w.sysMsg(e, "拿到"+gd.Name, colorYellow)
	}
	w.itemsChanged(e)
}
