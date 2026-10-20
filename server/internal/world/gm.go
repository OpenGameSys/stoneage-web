package world

import (
	"fmt"
	"strings"

	"sa/server/internal/battle"
	"sa/server/internal/proto"
)

// gmCommand 是开发用的调试指令（对应原版 chatmagic.c 的 GM 指令，只在 -gm 时开启）：
//
//	/warp 楼层 x y   传送
//	/encount         在当前位置立即遇敌（不在遇敌区域时什么也不做）
//	/item 编号、/gold 数量、/fame 点数（设定声望）、/pet 敌人编号 [等级]（给一只宠物）、/petai 格位 [变动值]
//	/lv 等级          设定等级
//	/fm fame 点数 | fill 人数 | manor 编号 | gold 数量   家族的测试资料：冒险声望、补满不在线的假族人、直接占有庄园（0 起，-1 放掉）、共同帐户
//
// 返回 true 表示已当作指令处理。
func (w *World) gmCommand(e *entity, text string) bool {
	f := strings.Fields(text)
	reply := func(s string) { e.client.Send(&proto.ChatMsg{Channel: "near", From: "GM", Text: s, Color: 4}) }
	switch f[0] {
	case "/warp":
		var fl, x, y int
		if len(f) != 4 {
			reply("用法：/warp 楼层 x y")
			return true
		}
		fmt.Sscan(f[1], &fl)
		fmt.Sscan(f[2], &x)
		fmt.Sscan(f[3], &y)
		fo := w.game.Floors[fl]
		if fo == nil || !fo.In(x, y) || e.fight != nil {
			reply("无法传送到那里")
			return true
		}
		w.placeOnFloor(e, fl, x, y, e.dir)
		return true
	case "/item":
		var id int
		if len(f) < 2 {
			reply("用法：/item 道具编号")
			return true
		}
		fmt.Sscan(f[1], &id)
		def := w.game.Items[id]
		if def == nil {
			reply("没有这个道具")
			return true
		}
		if w.addItem(e.inv, w.newItem(def)) < 0 {
			reply("道具栏已满")
			return true
		}
		reply("得到了" + def.Name)
		w.itemsChanged(e)
		return true
	case "/gold":
		var n int
		if len(f) == 2 {
			fmt.Sscan(f[1], &n)
		}
		e.char.Gold = max(0, n)
		w.itemsChanged(e)
		return true
	case "/fame":
		var n int
		if len(f) == 2 {
			fmt.Sscan(f[1], &n)
		}
		e.char.Fame = 0
		w.earnFame(e, n*100)
		reply(fmt.Sprintf("声望为 %d", e.char.Fame/100))
		return true
	case "/pet": // /pet 敌人编号 [等级]：照捕获的方式给一只宠物（enemy1.txt 的编号）
		var id, lv int
		if len(f) >= 2 {
			fmt.Sscan(f[1], &id)
		}
		if len(f) >= 3 {
			fmt.Sscan(f[2], &lv)
		}
		en := w.game.Battle.Enemies[id]
		if en == nil {
			reply("用法：/pet 敌人编号 [等级]")
			return true
		}
		if lv <= 0 {
			lv = en.LvMin
		}
		s := battle.EnemySpec{Enemy: en, Temp: w.game.Battle.Temps[en.TempNo], Level: lv}
		f := battle.NewEnemy(s, battle.EnemySlot(0), w.game.Battle.Skills, w.rnd)
		p := petFromFighter(f, e.char.ID)
		if w.addPet(e, p) < 0 {
			reply("宠物栏满了")
			return true
		}
		w.petsChanged(e)
		reply(fmt.Sprintf("得到 %s（%d 级）", p.Name, p.Level))
		return true
	case "/petai": // /petai 格位 [变动值]：设定宠物的忠诚度变动值（CHAR_VARIABLEAI，预设 10000 = 忠诚度 +100）
		slot, v := -1, battle.MaxVariableAI
		if len(f) >= 2 {
			fmt.Sscan(f[1], &slot)
		}
		if len(f) >= 3 {
			fmt.Sscan(f[2], &v)
		}
		p := w.petAt(e, slot)
		if p == nil {
			reply("用法：/petai 格位 [变动值]")
			return true
		}
		p.VariableAI = 0
		p.addVariableAI(v)
		w.petsChanged(e)
		reply(fmt.Sprintf("%s 的忠诚度为 %d", p.displayName(), w.petLoyalty(e, p)))
		return true
	case "/event": // /event 编号 [now|clear]：设定任务旗标（做完 / 进行中 / 清掉）
		if len(f) < 2 {
			reply("用法：/event 编号 [now|clear]")
			return true
		}
		var n int
		fmt.Sscan(f[1], &n)
		switch {
		case len(f) > 2 && f[2] == "now":
			e.setEventNow(n)
		case len(f) > 2 && f[2] == "clear":
			e.clearEvent(n)
		default:
			e.setEventDone(n)
		}
		w.saveProgress(e)
		reply(fmt.Sprintf("做完：%s　进行中：%s", e.char.EndEvents, e.char.NowEvents))
		return true
	case "/lv":
		var n int
		if len(f) == 2 {
			fmt.Sscan(f[1], &n)
		}
		e.char.Level = max(1, min(MaxLevel, n))
		w.sendStatus(e)
		w.saveProgress(e)
		return true
	case "/fm":
		var n int
		if len(f) == 3 {
			fmt.Sscan(f[2], &n)
		}
		if len(f) != 3 {
			reply("用法：/fm fame 点数 | fill 人数 | manor 编号 | gold 数量")
			return true
		}
		reply(w.gmFamily(e, f[1], n))
		return true
	case "/encount":
		a := w.game.Battle.AreaAt(e.floor, e.x, e.y)
		if a == nil || e.fight != nil {
			reply("这里不会遇敌")
			return true
		}
		w.startBattle(e, a)
		return true
	}
	return false
}

// gmFamily 是 /fm：给自己的家族做测试资料。
func (w *World) gmFamily(e *entity, what string, n int) string {
	defer w.famLock()()
	fm, _, _ := w.fmOf(e)
	if fm == nil {
		return "你没有家族"
	}
	switch what {
	case "fame":
		fm.Adv = max(0, fm.Adv+n*100)
	case "fill": // 假族人：不在线的正式族人（ID 用负数，不会和真的角色重复）
		for i := 1; i < fmMaxMember && n > 0; i++ {
			if fm.Members[i] == nil {
				id := -int64(fm.ID*100 + i)
				fm.Members[i] = &fmMember{ID: id, Name: fmt.Sprintf("族人%d", i), Level: 1, Duty: fmDutyMember}
				w.fam.byChar[id] = fm
				n--
			}
		}
		fm.recount()
	case "manor":
		if n < 0 {
			if i := w.fam.manorOf(fm); i >= 0 {
				w.fam.manors[i] = manorState{}
				w.fam.saveKV("manors", w.fam.manors)
				w.resyncFamily(fm)
			}
		} else if n < len(manorTable) {
			w.claimManor(fm, n)
		}
	case "gold":
		fm.Gold = max(0, min(fmMaxGold, n))
	default:
		return "用法：/fm fame 点数 | fill 人数 | manor 编号 | gold 数量"
	}
	w.fam.save(fm)
	return fmt.Sprintf("%s：%d 人（正式 %d），声望 %d，气势 %d，庄园 %d，共同帐户 %d", fm.Name, fm.num(), fm.joinNum(),
		fm.total()/100, fm.momentum()/100, w.fam.manorOf(fm), fm.Gold)
}
