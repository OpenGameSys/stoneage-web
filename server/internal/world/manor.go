package world

// 庄园（AC 的 db_fmpoint、family.c FAMILY_SetPoint、npc_fmletter.c、npc_fmhealer.c、npcutil.c addNpcFamilyTax）。
// 原版这一版只开了 2 个庄园（MANOR_MAX_NUM），NPC 却摆了 4 个；这里 4 个都做。
// 庄园争夺战与家族 PK 场在 fmwar.go；这里是申请（只能申请空的，家族解散时放掉）、邀请函、庄园税与家族小护士。

import (
	"fmt"
	"strings"

	"sa/server/internal/data"
)

// manorTable：town 是 fmpointlist 的村（fl），x / y 是申请时核对的座标，home 是族长家（fmfl，族人的 CHAR_WORKFMFLOOR），village 1..4。
var manorTable = [4]struct {
	town, x, y, home, village int
	name                      string
}{
	{100, 607, 563, 1041, 1, "萨姆吉尔庄园"},
	{200, 73, 588, 2031, 2, "玛丽娜丝庄园"},
	{300, 497, 354, 3031, 3, "加加庄园"},
	{400, 274, 532, 4031, 4, "卡鲁它那庄园"},
}

const (
	fmPointFame = 100000 // _FM_POINT_APPLY_FAME：家族声望 1000（内部值）才能申请
	fmPointJoin = 10     // AC：正式族人 10 人才能申请
)

// fmSetPoint 是 FAMILY_SetPoint（P|编号|村|东|南）与 AC 的 ACSetFMPoint：族长申请空的庄园。
func (w *World) fmSetPoint(e *entity, f []string) {
	fm, _, m := w.fmOf(e)
	if fm == nil {
		return
	}
	if m.Duty != fmDutyLeader {
		w.fmMessage(e, "\n你不是族长，所以没有修改的权力唷！")
		return
	}
	if fm.total() < fmPointFame {
		w.fmMessage(e, "\n你家族的声望不足1000声望，无法申请庄园！")
		return
	}
	fam := w.fam
	i := fmInt(f, 1)
	switch {
	case fam.manorOf(fm) >= 0:
		w.fmMessage(e, "\n你已经有家族据点了～不得重复申请！")
	case i < 0 || i >= len(manorTable) || fmInt(f, 2) != manorTable[i].town || fmInt(f, 3) != manorTable[i].x || fmInt(f, 4) != manorTable[i].y:
		w.fmMessage(e, "\n申请家族失败！")
	case fam.manors[i].Owner > 0:
		w.fmMessage(e, "\n家族据点已经有家族在使用中罗！")
	case fm.joinNum() < fmPointJoin:
		w.fmMessage(e, "\n您的家族人数未达申请标准唷！")
	default:
		w.claimManor(fm, i)
		w.fmMessage(e, "\n申请家族据点ＯＫ！")
	}
}

// claimManor 让 fm 占有庄园 i；族人的庄园楼层跟着改（AC 送每个在线族人 ACFMCharLogin）。
func (w *World) claimManor(fm *family, i int) {
	fam := w.fam
	if old := fam.list[fam.manors[i].Owner]; old != nil {
		fam.manors[i] = manorState{}
		w.resyncFamily(old)
	}
	if j := fam.manorOf(fm); j >= 0 {
		fam.manors[j] = manorState{}
	}
	fam.manors[i] = manorState{Owner: fm.ID, Since: w.now().Unix()}
	fam.saveKV("manors", fam.manors)
	w.resyncFamily(fm)
}

func (w *World) resyncFamily(fm *family) {
	for _, m := range fm.Members {
		if m != nil {
			w.fam.resync(m.ID, false)
		}
	}
}

// manorOwner 是村 village（1..4）的庄园现在的主人（锁里用）。
func (w *World) manorOwner(village int) *family {
	for i, mt := range manorTable {
		if mt.village == village {
			return w.fam.list[w.fam.manors[i].Owner]
		}
	}
	return nil
}

// fmLetter 是 L|L|1 / L|L|2（FAMILY_LeaderFunc "L" 的 C 版本）：占有庄园的家族做邀请函（19001..19004）或骑乘同意书（19005..19008）。
func (w *World) fmLetter(e *entity, fm *family, kind int) {
	i := w.fam.manorOf(fm)
	if i < 0 {
		w.fmMessage(e, "\n只有拥有庄园的族长，才能制作唷！")
		return
	}
	id := 19000 + manorTable[i].village
	if kind == 2 {
		id += 4
	} else if kind != 1 {
		return
	}
	w.makeLetter(e, id)
}

func (w *World) makeLetter(e *entity, id int) {
	def := w.game.Items[id]
	if def == nil {
		return
	}
	if w.addItem(e.inv, w.newItem(def)) < 0 {
		w.sysMsg(e, "道具栏已满。", colorWhite)
		return
	}
	w.itemsChanged(e)
	w.sysMsg(e, fmt.Sprintf("制作%s成功。", def.Name), colorWhite)
}

// fmLetterTalk 是 NPC_FmLetterTalked：只有这个庄园的族长能用（秘书的 family= 是村号）。
func (w *World) fmLetterTalk(p, n *entity) {
	defer w.famLock()()
	fm, _, m := w.fmOf(p)
	if owner := w.manorOwner(n.npc.Family); fm == nil || fm != owner || m.Duty != fmDutyLeader {
		w.fmMessage(p, "\n只有本庄园的族长才能制作邀请函及同意书！")
		return
	}
	w.windowmanOpen(p, n, 1)
}

// fmLetterReply 是 NPC_FmLetterWindowTalked：去 5 做邀请函、去 6 做骑乘同意书（设定档的 letterN 是 19000+N / 19004+N）。
func (w *World) fmLetterReply(p, n *entity, seq, button int, sel string) {
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
	village := n.npc.Family
	switch next := def.Procs[proc].Goto; next {
	case 5:
		w.makeLetter(p, 19000+village)
	case 6:
		w.makeLetter(p, 19004+village)
	default:
		w.windowmanOpen(p, n, next)
	}
}

// fmHealerTalk 是 NPC_FmHealerTalked：全员回复（自己、宠物、队伍），不收钱（检查族人的部分原版注解掉了）。
func (w *World) fmHealerTalk(p, n *entity) {
	members := []*entity{p}
	if p.party != nil {
		members = members[:0]
		for _, m := range p.party.slots {
			if m != nil {
				members = append(members, m)
			}
		}
	}
	for _, m := range members {
		w.healAll(m)
	}
	w.sysMsg(p, "已经全部回复。请小心自己的身体唷！", colorWhite)
}

// villageOfFloor 是 addNpcFamilyTax 的 floor/100 → 村（只用到前 4 个村）。
func villageOfFloor(floor int) int {
	switch floor / 100 {
	case 10:
		return 1
	case 20:
		return 2
	case 30:
		return 3
	case 40:
		return 4
	}
	return 0
}

// manorTax 是 addNpcFamilyTax：在庄园所属的村买东西时，庄园的主人另外得到价钱的 10%（买的人不多付）。
func (w *World) manorTax(n *entity, price int) {
	w.addManorGold(villageOfFloor(n.floor), price/10)
}

// addManorGold 把钱加到村 village 的庄园主人的家族共同帐户（骑乘训练师的学费 1/5 也用它）。
func (w *World) addManorGold(village, gold int) {
	if village <= 0 || gold <= 0 {
		return
	}
	defer w.famLock()()
	if fm := w.manorOwner(village); fm != nil {
		fm.Gold = min(fmMaxGold, fm.Gold+gold)
		w.fam.save(fm)
	}
}

// manorText 替换告示牌的 %manorid:n%（npc_signboard.c）：庄园 n（1 起）的主人，没有时是"没有任何家族"。
func (w *World) manorText(s string) string {
	if !strings.Contains(s, "%manorid") {
		return s
	}
	defer w.famLock()()
	for i := range manorTable {
		key := fmt.Sprintf("%%manorid:%d%%", i+1)
		if !strings.Contains(s, key) {
			continue
		}
		name := "没有任何家族"
		if fm := w.fam.list[w.fam.manors[i].Owner]; fm != nil {
			name = fm.Name
		}
		s = strings.ReplaceAll(s, key, name)
	}
	return s
}

// healAll 是 NPC_FmHealerTalked 对一个人做的：HP、MP 回满，宠物也回满。
func (w *World) healAll(e *entity) {
	_, maxMP := w.maxHPMP(e)
	e.char.HP, e.char.MP = -1, maxMP
	w.healPets(e)
	w.sendStatus(e)
	w.saveProgress(e)
}
