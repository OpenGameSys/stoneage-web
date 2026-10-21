package world

import (
	"fmt"
	"strings"

	"sa/server/internal/battle"
	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 骑宠（family.c FAMILY_RidePet + ablua familyridefunction.lua，item_event.c ITEM_useLearnRideCode）。
// 原客户端从宠物窗口的骑乘按钮送 lssproto_FM "R|P|格位"，再按一次（或 "R|P|-1"）就下来。
// setup.cf：RIDEPETLEVEL=140（新角色能骑的宠物等级上限）、RIDELEVEL=10（宠物最多比人高 10 级）、RIDETRANS=1。

const (
	RidePetLevel = 140 // getRidePetLevel：CHAR_LEARNRIDE 的初始值
	RideLevel    = 10  // getRideLevel
	RideTrans    = 1   // getRideTrans：宠物转生次数上限
)

func learnRide(e *entity) int {
	if e.pets.LearnRide > 0 {
		return e.pets.LearnRide
	}
	return RidePetLevel
}

// rideSprite 是骑着时的形象（FamilyRideFunction 回传的 rideGraNo），没在骑时为 0。
func (w *World) rideSprite(e *entity) int {
	if e.pets == nil || e.pets.Ride < 0 || w.game.Ride == nil {
		return 0
	}
	p := e.pets.Pets[e.pets.Ride]
	if p == nil {
		return 0
	}
	return w.mountSprite(e, p.Sprite)
}

// mountSprite 是 FamilyRideFunction 的全部：先查证书 / 老虎 / 固定配对（data.Ride.Sprite），再查庄园骑宠（Pointpetlist：
// 家族占有庄园、守护精灵颜色对、不是申请中的人）；骑不了回 0。
func (w *World) mountSprite(e *entity, petSprite int) int {
	if s := w.game.Ride.Sprite(e.char.Sprite, w.weaponSprite(e), petSprite, e.pets.Licence[0]); s != 0 {
		return s
	}
	if i := w.game.Ride.ManorIndex(petSprite); i >= 0 && e.fm.Index > 0 && e.fm.Duty != fmDutyApply && w.game.Ride.ManorOK(i, e.fm.Floor, e.fm.Sprite) {
		return w.game.Ride.ManorSprite(e.char.Sprite, i)
	}
	return 0
}

// PetRide 是 FAMILY_RidePet：slot 为 -1 或已经骑着时下来；否则依次检查后骑上第 slot 格的宠物。
// 原版没挡骑出战中的宠物（那行注释掉了），这里骑上去时把它从出战改掉，免得同一只既出战又被骑。
func (w *World) PetRide(charID, slot int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil {
			return
		}
		if e.fight != nil {
			w.sysMsg(e, "战斗中不可骑宠！", colorYellow)
			return
		}
		if e.char.Pig > 0 && slot >= 0 && e.pets.Ride < 0 {
			w.sysMsg(e, "目前你处于乌力化状态，不能骑乘宠物。", colorYellow) // family.c FAMILY_RidePet
			return
		}
		if slot < 0 || e.pets.Ride >= 0 {
			if e.pets.Ride >= 0 {
				w.dismount(e)
				w.petsChanged(e)
			}
			return
		}
		p := w.petAt(e, slot)
		if p == nil || p.Away {
			return
		}
		if lr := learnRide(e); lr < p.Level {
			w.sysMsg(e, fmt.Sprintf("你目前只能骑乘等级小于%d级的宠。", lr), colorYellow)
			return
		}
		if w.petLoyalty(e, p) < 100 {
			w.sysMsg(e, "该骑宠的忠小于100", colorYellow)
			return
		}
		if e.char.Level+RideLevel < p.Level {
			w.sysMsg(e, fmt.Sprintf("你最高只能骑宠等级比你大%d级的宠。", RideLevel), colorYellow)
			return
		}
		if p.Trans > RideTrans {
			return
		}
		if p.Trans > 0 { // FamilyRideFunction：转生过的宠物不能骑
			w.sysMsg(e, "【系统提示】由于["+p.displayName()+"]已经转生，所以无法骑乘。", colorRed)
			return
		}
		if w.mountSprite(e, p.Sprite) == 0 {
			if w.game.Ride.ManorIndex(p.Sprite) >= 0 {
				w.sysMsg(e, "只有占有庄园、守护精灵颜色相符的家族成员才能骑这只。", colorYellow)
			}
			return
		}
		e.pets.Ride = slot
		if e.pets.Battle == slot {
			e.pets.Battle = -1
		}
		e.pets.Standby &^= 1 << slot // lssproto_SPET：骑着的宠物不待命
		w.updateSprite(e)
		w.petsChanged(e)
	})
}

// rideInfo 是宠物窗口详情里的骑乘说明（额外新功能）："no" 这种宠物不能骑；"ok" 现在就能骑；
// 否则 "need:" 加上还差的条件（照 PetRide / FAMILY_RidePet 的检查顺序）。
func (w *World) rideInfo(e *entity, p *Pet) string {
	if !w.game.Ride.Rideable(p.Sprite) {
		return "no"
	}
	var need []string
	if lr := learnRide(e); lr < p.Level {
		need = append(need, fmt.Sprintf("骑乘课程（现在只能骑 %d 级以下）", lr))
	}
	if ai := w.petLoyalty(e, p); ai < 100 {
		need = append(need, fmt.Sprintf("忠诚度 100（现在 %d）", ai))
	}
	if e.char.Level+RideLevel < p.Level {
		need = append(need, fmt.Sprintf("宠物最多比你高 %d 级（你 %d 级）", RideLevel, e.char.Level))
	}
	if p.Trans > 0 {
		need = append(need, "转生过的宠物不能骑")
	}
	if w.mountSprite(e, p.Sprite) == 0 {
		if mi := w.game.Ride.ManorIndex(p.Sprite); mi >= 0 {
			switch {
			case e.fm.Index <= 0 || e.fm.Duty == fmDutyApply:
				need = append(need, "加入占有庄园的家族（庄园骑宠）")
			case e.fm.Floor == 0:
				need = append(need, "家族要先占有庄园")
			case w.game.Ride.ManorKind[mi] != e.fm.Sprite:
				need = append(need, "家族的守护精灵颜色不对（蓝暴要光明、红暴要黑暗）")
			default:
				need = append(need, "你的人物形象骑不了这只")
			}
		} else if i := w.game.Ride.LicenceIndex(p.Sprite); i >= 0 && e.pets.Licence[0]&(1<<i) == 0 {
			need = append(need, "先用这种宠物的骑乘书学会")
		} else {
			need = append(need, "你的人物形象骑不了这只（颜色不合）")
		}
	}
	if len(need) == 0 {
		return "ok"
	}
	return "need:" + strings.Join(need, "、")
}

// dismount：下来（RIDEPET = -1，形象换回拿武器的样子）。
func (w *World) dismount(e *entity) {
	e.pets.Ride = -1
	w.updateSprite(e)
}

// learnRideItem 是 ITEM_useLearnRideCode：道具参数 "RIDE_PETn|宠物名[|转生次数]"，把证书第 n 位打开（n 每 32 个换下一组），
// 转生次数不够不能学；学会后道具消失。
func (w *World) learnRideItem(e *entity, slot int, def *data.ItemDef) {
	f := strings.Split(def.Argument, "|")
	code, ok := strings.CutPrefix(f[0], "RIDE_PET")
	n := data.Atoi(code)
	if !ok || code == "" || n < 0 || n >= 96 {
		return
	}
	name := ""
	if len(f) > 1 {
		name = f[1]
	}
	if len(f) > 2 && e.char.Trans < data.Atoi(f[2]) {
		if name != "" {
			w.sysMsg(e, fmt.Sprintf("必须%d转人以上才能学习骑%s。", data.Atoi(f[2]), name), colorYellow)
		}
		return
	}
	e.pets.Licence[n/32] |= 1 << (n % 32)
	if name != "" { // 原版只有参数写了宠物名时才说、才收走道具
		w.sysMsg(e, fmt.Sprintf("学习了新的骑宠 (%s)。", name), colorYellow)
		e.inv[slot] = nil
		w.itemsChanged(e)
	}
	w.petsChanged(e)
}

// mount 把骑着的宠物换成战斗数值（BATTLE_getRidePet）。
func (w *World) mount(e *entity) *battle.Mount {
	if e.pets.Ride < 0 {
		return nil
	}
	p := e.pets.Pets[e.pets.Ride]
	if p == nil {
		return nil
	}
	d, _ := w.petDerive(p)
	return &battle.Mount{Name: p.displayName(), Level: p.Level, HP: max(0, min(p.HP, d.HP)), MaxHP: d.HP,
		Atk: d.Atk, Def: d.Def, Quick: d.Quick, Attr: p.Attr, Base: [4]float64{float64(p.Vital), float64(p.Str), float64(p.Tgh), float64(p.Dex)}}
}

// rideResult：战斗结束时骑宠拿经验（exp 由 battleExp 算好）、升级，回传结果窗口的一行（落马了就没有）。
func (w *World) rideResult(e *entity, ft *fight, exp int) []proto.PetResult {
	f := ft.b.F[ft.slot]
	if f.Ride == nil || f.Ride.Fell || e.pets.Ride < 0 {
		return nil
	}
	p := e.pets.Pets[e.pets.Ride]
	if p == nil {
		return nil
	}
	from := p.Level

	ups := w.petGainExp(p, exp)
	w.petFeed(e, p, from)
	return []proto.PetResult{{Slot: e.pets.Ride, Name: p.displayName(), Exp: exp, LevelUp: ups, Level: p.Level}}
}

// settleRide 把骑宠这场的 HP 写回（倒下的回到 1，BATTLE_Exit）；落马了就下来（WORKPETFALL → RIDEPET -1）。
func (w *World) settleRide(e *entity, ft *fight) {
	f := ft.b.F[ft.slot]
	if f.Ride == nil || e.pets.Ride < 0 {
		return
	}
	if p := e.pets.Pets[e.pets.Ride]; p != nil {
		p.HP = max(1, f.Ride.HP)
	}
	if f.Ride.Fell {
		w.dismount(e)
	}
	e.char.Pets = e.pets.String()
}

// ---------------- 骑乘训练师（npc_riderman.c） ----------------

// rideLessons 是 NPC_RidermanWindowTalked 的特别窗口 6..9：学会后能骑的等级、要先学过的等级、课程名。
var rideLessons = map[int]struct {
	level, need int
	name, ok    string
}{
	6: {40, 0, "初级班", "可以骑乘 Lv40 以下的宠物，"},
	7: {80, 40, "中级班", "可以骑乘 Lv80 以下的宠物，"},
	8: {120, 80, "高级班", "可以骑乘 Lv120 以下的宠物，"},
	9: {200, 120, "特级班", "可以骑乘所有等级的宠物，"},
}

// ridermanReply 是 NPC_RidermanWindowTalked：与万事通相同的设定档流程，gotowin 5 显示能骑的宠物，6..9 是骑乘课程
// （学费是按下"是"的那个窗口的 takegold）。原版把学费的 1/5 交给这个村子的庄园家族，还没有家族，略过。
func (w *World) ridermanReply(p, n *entity, seq, button int, sel string) {
	if max(abs(n.x-p.x), abs(n.y-p.y)) > 1 {
		return
	}
	def := n.npc.Windows[seq-100]
	if def == nil {
		return
	}
	proc := -1
	if def.Type == data.WinTypeSelect {
		proc = data.Atoi(sel) + data.WinProcSelect - 1
	} else {
		for i, b := range []int{BtnOK, BtnCancel, BtnYes, BtnNo, BtnPrev, BtnNext} {
			if button&b != 0 {
				proc = i
				break
			}
		}
	}
	if proc < 0 || proc >= data.WinProcCount || def.Procs[proc] == nil {
		return
	}
	next := def.Procs[proc].Goto
	msg := func(text string) { w.sendWindow(p, n, "message", BtnOK, 0, text) }
	if next == 5 {
		// WINDOW_MESSAGETYPE_SHOWRIDEPET：原客户端从自己的骑宠表列出这个人物能骑的宠物，这里由服务端列名字
		var names []string
		for i, pet := range w.game.Ride.RidePets {
			if i < len(w.game.Ride.PetNames) && w.mountSprite(p, pet) != 0 {
				names = append(names, w.game.Ride.PetNames[i])
			}
		}
		if len(names) == 0 {
			msg("\n\n你现在没有可以骑乘的宠物。")
		} else {
			msg("\n你可以骑乘的宠物：\n\n" + strings.Join(names, "、"))
		}
		return
	}
	if l, ok := rideLessons[next]; ok {
		lr := learnRide(p)
		switch {
		case next == 9 && lr > 200, next != 9 && lr >= l.level:
			msg("\n\n你已经学会" + l.name + "了呀。\n不需要再学了。")
		case lr < l.need:
			prev := rideLessons[next-1].name
			msg("\n\n你必须先学过" + prev + "才能来" + l.name + "唷。")
		case p.char.Gold < def.TakeGold:
			msg(fmt.Sprintf("\n很抱歉喔！你的学费不足！\n学习骑乘宠物%s需要%d石币。", l.name, def.TakeGold))
		default:
			p.char.Gold -= def.TakeGold
			w.addManorGold(n.npc.Family, def.TakeGold/5) // 学费的 1/5 给庄园的家族（ACFixFMData FM_FIX_FMGOLD）
			p.pets.LearnRide = l.level
			w.petsChanged(p)
			w.sendInventory(p)
			msg("\n\n恭喜你！你已经完成骑乘" + l.name + "了。\n" + l.ok + "\n请小心骑乘喔！")
		}
		return
	}
	w.windowmanOpen(p, n, next)
}
