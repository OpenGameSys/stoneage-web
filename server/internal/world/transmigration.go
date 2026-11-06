package world

import (
	"fmt"
	"strings"

	"sa/server/internal/data"
)

// 转生（原版 npc/npc_transmigration.c，模板 Transmigration；这台服务端开着 _TRANS_6、_PET_TRANS、_PET_2TRANS、
// _FIX_SUPERPERSON_STRANS、_PROSK99、_NO_CLS_EVENT_255）。漆黑的洞窟地下 20 楼（32021）的精灵王：
//
//	人物转生  80 级以上、做完事件 39 / 40 / 42 / 46，按这次是第几转带着指定的宠物（NPC_TransmigrationCheck）：
//	          1..4 转各要一只守护兽（模板 693 地 / 694 水 / 695 火 / 696 风，按顺序），5 转四只都要，
//	          6 转要一只转生过 2 次的长老宠（乌力乌力 2 / 凯比 112 / 克克尔 102 / 威伯 34）、事件 63 / 136 / 175 / 176 与 15 个魔界之门的碎片（20613）。
//	          对话：startmsg → mainmsg（确定 / 取消）→ 转生 → yesmsg1 → yesmsg2 → 传回出生村（NPC_Transmigration_selectWindow 0..5）。
//	宠物转生  身上带着自己的 79 级玛蕾菲雅（模板 718，二转用 401）时找精灵王是宠物转生（NPC_PetTransMan_selectWindow）：
//	          80 级以上、事件 4 / 69 / 70，选一只 80 级以上、转生不到 2 次的宠物，按玛蕾菲雅与它的成长算新的成长、变回 1 级（GetNewPet）。
//
// 参数档 giiru/event/sikkoku/ten.arg：四个村子各一行（samugiru / marinasu / jaja / karutarna：Present 是第 n 转送的宠物、Warp 是传回的地点）、
// maxmsg、PETTRANS 段与 TENSEI:1..5 段，段与段之间用 END 分开。官方的这份没有 TENSEI:6（原版第 6 转的对话取不到就不回应），
// 这里第 6 转改用最后一段（TENSEI:5）的台词。

// 窗口编号（CHAR_WINDOWTYPE_TRANSMIGRATION_* / PETTRANS_*）
const (
	seqTransStart = iota + 1
	seqTransMain
	seqTransEnd
	seqTransNone
	seqPetTransStart
	seqPetTransSelect
	seqPetTransSelPet
	seqPetTransMain1
	seqPetTransEnd
)

const (
	transMax      = 6     // _TRANS_6
	transMinLevel = 80    // NPC_TransmigrationCheck / NPC_PetTransManCheck
	transPetRebel = 718   // 玛蕾菲雅（一转用）
	transPetRebl2 = 401   // 玛蕾菲雅（_PET_2TRANS：二转用）
	transShard    = 20613 // 魔界之门的碎片
	transSixPet   = 2546  // 第 6 转送的宠物（六转机械人龙）
)

var (
	transElders     = []string{"samugiru", "marinasu", "jaja", "karutarna"} // 出生村（CHAR_SAVEPOINT 的位元 0..3）
	transGuardians  = [4]int{693, 694, 695, 696}                            // 守护兽的模板
	transElderPets  = []int{2, 112, 102, 34}                                // 第 6 转要的二转长老宠
	transNeedEvents = []int{39, 40, 42, 46}
	transSixEvents  = []int{63, 136, 175, 176}
	// questTbl：转生时数做完了几个，算进新的基本状态（NPC_TransmigrationQuestCheck）
	transQuests = []int{1, 2, 4, 5, 8, 12, 14, 15, 16, 17, 19, 22, 27, 30, 31, 34, 35, 38, 44, 45}
	// NPC_TransmigrationFlg_CLS：清掉旗标时保留的（_PROSK99）与每转补上的
	transKeepEnd = []int{69, 70, 71, 72, 109, 118, 122, 131, 127, 158, 159, 181}
	transKeepNow = []int{69, 70, 71, 72}
	transSetEnd  = []int{48, 49, 50, 51, 52, 179}
	// 宠物转生后收走的道具（地 / 水 / 火 / 风灵项链与灵石，NPC_PetTransManStatus 的 DelItem）
	petTransDelItems = [][2]int{{19629, 19632}, {19672, 19687}, {19625, 19628}}
)

// ---------------- 参数 ----------------

// transSegment 是第 n 转的那一段（"TENSEI:n"，段与段之间是 END）；找不到时用最后一个 TENSEI 段（见文件开头）。
func transSegment(arg string, n int) string {
	key, last := fmt.Sprintf("TENSEI:%d", n), ""
	for _, seg := range strings.Split(arg, "END") {
		if strings.Contains(seg, key) {
			return seg
		}
		if strings.Contains(seg, "TENSEI:") {
			last = seg
		}
	}
	return last
}

func petTransSegment(arg string) string {
	for _, seg := range strings.Split(arg, "END") {
		if strings.Contains(seg, "PETTRANS:") {
			return seg
		}
	}
	return ""
}

func transMsg(seg, key string) string {
	s, _ := data.ArgField(seg, key)
	return npcText(s)
}

// transVillage 是 NPC_StartpointCheck：CHAR_SAVEPOINT 里第一个登记过的村子（0..3），出生村也算；都没有回 -1。
func transVillage(p *entity) int {
	bits := p.char.SavePoints
	if h := p.char.Hometown; h >= 0 && h < 4 {
		bits |= 1 << h
	}
	for i := 0; i < 4; i++ {
		if bits&(1<<i) != 0 {
			return i
		}
	}
	return -1
}

// transVillageArg 是村子那一行的 "Item=4,Pet=3,Present=…,Warp=…" 里某一项的值。
func transVillageArg(arg string, village int, key string) string {
	line, _ := data.ArgField(arg, transElders[village])
	for _, kv := range strings.Split(line, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ---------------- 对话 ----------------

// transTalk 是 NPC_TransmigrationTalked：身上有自己的 79 级玛蕾菲雅就是宠物转生，否则是人物转生。
func (w *World) transTalk(p, n *entity) {
	marefia := 0
	for i, pet := range p.pets.Pets {
		if pet == nil || (pet.TempNo != transPetRebel && pet.TempNo != transPetRebl2) {
			continue
		}
		switch {
		case p.char.Level < transMinLevel:
			w.sysMsg(p, "由于你的等级小于80,没能力进行转宠！", colorWhite)
			return
		case pet.Level > 79: // 原版把超过 79 级的玛蕾菲雅当作非法的清掉
			if p.pets.Ride == i {
				w.dismount(p)
			}
			w.removePet(p, i)
			w.petsChanged(p)
			w.sysMsg(p, "清除你身上非法玛雷菲雅", colorRed)
			return
		case pet.Level == 79:
			if pet.Owner != p.char.ID {
				w.sysMsg(p, "你身上的玛雷菲雅，并不是你的啊！", colorWhite)
				return
			}
			marefia++
		}
	}
	switch {
	case marefia > 1:
		w.sysMsg(p, "你怎会有那麽多玛雷菲雅呢？", colorWhite)
	case marefia == 1:
		w.sendWindow(p, n, "message", BtnNext, seqPetTransStart, transMsg(petTransSegment(n.npc.Arg), "mainmsg"))
	default:
		w.transOpen(p, n)
	}
}

// transOpen 是 NPC_Transmigration_selectWindow(0)：条件达成说 startmsg，已经 6 转说 maxmsg，否则 nonemsg。
func (w *World) transOpen(p, n *entity) {
	next := p.char.Trans + 1
	seg := transSegment(n.npc.Arg, next)
	switch ten := w.transCheck(p); {
	case ten >= 0 && ten < transMax:
		w.sendWindow(p, n, "message", BtnNext, seqTransStart, transMsg(seg, "startmsg"))
	case ten == transMax:
		w.sendWindow(p, n, "message", BtnOK, seqTransStart, transMsg(n.npc.Arg, "maxmsg"))
	default:
		w.sendWindow(p, n, "message", BtnOK, seqTransStart, transMsg(seg, "nonemsg"))
	}
	p.window.event = next
}

// transCheck 是 NPC_TransmigrationCheck：回传现在的转生次数（条件都达成），已经 6 转回 6，条件不足回 -1。
func (w *World) transCheck(p *entity) int {
	num := p.char.Trans
	if num >= transMax {
		return transMax
	}
	if p.char.Level < transMinLevel {
		return -1
	}
	for _, ev := range transNeedEvents {
		if !p.eventDone(ev) {
			return -1
		}
	}
	switch {
	case num < 4:
		if transFindPet(p, func(pet *Pet) bool { return pet.TempNo == transGuardians[num] }) < 0 {
			return -1
		}
	case num == 4:
		for _, t := range transGuardians {
			if transFindPet(p, func(pet *Pet) bool { return pet.TempNo == t }) < 0 {
				return -1
			}
		}
	default: // 第 6 转
		if transFindPet(p, transSixPetOK) < 0 {
			w.sysMsg(p, "你没有带2转的宠物哦!", colorWhite)
			return -1
		}
		for _, ev := range transSixEvents {
			if !p.eventDone(ev) {
				return -1
			}
		}
		if w.countItem(p, transShard) < 15 {
			w.sysMsg(p, "你没有带15个魔界之门的碎片哦!", colorWhite)
			return -1
		}
	}
	return num
}

func transSixPetOK(pet *Pet) bool {
	if pet.Trans != 2 {
		return false
	}
	for _, t := range transElderPets {
		if pet.TempNo == t {
			return true
		}
	}
	return false
}

// transFindPet：第一只合条件、不在送信中的宠物的格位，没有回 -1。
func transFindPet(p *entity, ok func(*Pet) bool) int {
	for i, pet := range p.pets.Pets {
		if pet != nil && !pet.Away && ok(pet) {
			return i
		}
	}
	return -1
}

// transReply 是 NPC_TransmigrationWindowTalked。
func (w *World) transReply(p, n *entity, win *npcWindow, button int, input string) {
	switch win.seq {
	case seqTransStart:
		if button == BtnNext {
			w.sendWindow(p, n, "message", BtnOK|BtnCancel, seqTransMain, transMsg(transSegment(n.npc.Arg, win.event), "mainmsg"))
			p.window.event = win.event
		}
	case seqTransMain:
		seg := transSegment(n.npc.Arg, win.event)
		if button != BtnOK {
			w.sendWindow(p, n, "message", BtnOK, seqTransNone, transMsg(seg, "nomsg"))
			return
		}
		// 原版这一步不再检查（只靠窗口的顺序）；这里重新检查一次，中途把宠物交出去了就不做
		if win.event != p.char.Trans+1 || w.transCheck(p) != p.char.Trans {
			return
		}
		village := transVillage(p)
		if village < 0 {
			return
		}
		if p.char.Points > 0 {
			w.sendWindow(p, n, "message", BtnCancel, seqTransNone, "你身上的点数没加完，所以我不能为你转生！")
			return
		}
		if !w.transmigrate(p, n.npc.Arg, village) {
			return
		}
		w.transYesMsg(p, n, win.event, 1)
	case seqTransEnd:
		switch button {
		case BtnNext:
			w.transYesMsg(p, n, win.event, win.cost)
		case BtnOK:
			// 最后一页按确定：传回出生村（NPC_TransmigratiomWarp）
			if v := transVillage(p); v >= 0 {
				if pt := strings.Split(transVillageArg(n.npc.Arg, v, "Warp"), "."); len(pt) == 3 {
					w.warpTo(p, data.Atoi(pt[0]), data.Atoi(pt[1]), data.Atoi(pt[2]))
				}
			}
		}
	case seqPetTransStart, seqPetTransSelect, seqPetTransSelPet, seqPetTransMain1:
		w.petTransReply(p, n, win, button, input)
	}
}

// transYesMsg 送 yesmsg<k>；还有下一页就是"下一页"钮，否则是"确定"（按了传回出生村）。
func (w *World) transYesMsg(p, n *entity, tensei, k int) {
	seg := transSegment(n.npc.Arg, tensei)
	text := transMsg(seg, fmt.Sprintf("yesmsg%d", k))
	if text == "" {
		return
	}
	buttons := BtnOK
	if strings.Contains(seg, fmt.Sprintf("yesmsg%d", k+1)) {
		buttons = BtnNext
	}
	w.sendWindow(p, n, "message", buttons, seqTransEnd, text)
	p.window.event, p.window.cost = tensei, k+1
}

// ---------------- 人物转生 ----------------

// transmigrate 是 NPC_TransmigrationMain：先交出宠物（与第 6 转的碎片），再重算基本状态、清旗标、变回 1 级、送宠物。
// 原版先改数值再收宠物，收不到时已经改了一半；这里先收（条件刚检查过），收不到就整个不做。
func (w *World) transmigrate(p *entity, arg string, village int) bool {
	c := &p.char
	next := c.Trans + 1
	present := 0
	if list := strings.Split(transVillageArg(arg, village, "Present"), "."); next-1 < len(list) {
		present = data.Atoi(list[next-1])
	}
	if next == transMax {
		present = transSixPet
	}
	if !w.transDelPets(p, c.Trans) {
		return false
	}
	if next == transMax {
		w.takeItems(p, []itemNeed{{ID: transShard, All: true}}, false)
	}
	c.Trans = next
	w.transReset(p)
	if en := w.game.Battle.Enemies[present]; en != nil && p.pets.free() >= 0 {
		pet := w.petFromEnemy(en) // NPC_TransmigrationAddPet：ENEMY_createPetFromEnemyIndex
		w.addPet(p, pet)
		w.sysMsg(p, "拿到"+pet.Name+"。", colorWhite)
	}
	c.Elder = village // CHAR_LASTTALKELDER
	w.petsChanged(p)
	w.itemsChanged(p)
	w.flushQuests(p)
	w.sendStatus(p)
	w.saveProgress(p)
	return true
}

// transDelPets 是 NPC_TransmigrationDelPet（trans 是转生前的次数）。
func (w *World) transDelPets(p *entity, trans int) bool {
	var slots []int
	switch {
	case trans < 4:
		slots = append(slots, transFindPet(p, func(pet *Pet) bool { return pet.TempNo == transGuardians[trans] }))
	case trans == 4:
		for _, t := range transGuardians {
			slots = append(slots, transFindPet(p, func(pet *Pet) bool { return pet.TempNo == t }))
		}
	default:
		slots = append(slots, transFindPet(p, transSixPetOK))
	}
	for _, s := range slots {
		if s < 0 {
			return false
		}
	}
	for _, s := range slots {
		w.giveUpPet(p, s)
	}
	return true
}

// transReset 是转生的数值部分（转生次数已经加过）：NPC_TransmigrationStatus 重算基本状态、NPC_TransmigrationFlg_CLS 清旗标、
// 变回 1 级、经验 0、可分配点数 = 转生次数 × 10、下骑宠、变回原来的样子、HP 补满。人物转生与 GM /tensei 共用。
func (w *World) transReset(p *entity) {
	c := &p.char
	w.transStatus(p)
	w.transFlags(p)
	c.Level, c.Exp = 1, 0
	c.Points = c.Trans * 10
	if p.pets.Ride >= 0 {
		w.dismount(p)
	}
	if p.metamo != nil {
		w.metamo(p, nil, 0)
	}
	c.HP = -1
}

// transStatus 是 NPC_TransmigrationStatus + NPC_TransCalculation：
//
//	累计 CHAR_TRANSEQUATION（做完的 questTbl 任务数、这次的等级（最多 130））；
//	新的四项总和 ans = 旧的总和(点数)/12 + 累计任务数/4 + (累计等级 − 转生次数 × 85)/4（第 6 转旧的总和固定当作 620）；
//	按旧的比例分给四项，每项是 Rounding(值, 1)：原版加 0.5 后乘 100 存成内部值（不去尾），这里存点数，就是四舍五入。
//
// 原版的内部值会留下不到 1 点的零头（状态窗口看不到），这里没有保留。
func (w *World) transStatus(p *entity) {
	c := &p.char
	quest := 0
	for _, ev := range transQuests {
		if p.eventDone(ev) {
			quest++
		}
	}
	tQuest := (c.TransEq>>16)&0xFFFF + quest
	tLevel := c.TransEq&0xFFFF + min(c.Level, 130)
	c.TransEq = tQuest<<16 + tLevel
	total := c.Vital + c.Str + c.Tgh + c.Dex
	old := float64(total)
	if c.Trans == transMax {
		old = 620
	}
	ans := float64(int(old/12 + float64(tQuest)/4 + float64(tLevel-c.Trans*85)/4))
	if total <= 0 {
		return
	}
	share := func(v int) int { return max(0, int(float64(v)/float64(total)*ans+0.5)) }
	c.Vital, c.Str, c.Tgh, c.Dex = share(c.Vital), share(c.Str), share(c.Tgh), share(c.Dex)
}

// transFlags 是 NPC_TransmigrationFlg_CLS：清掉 0..255 号旗标（_NO_CLS_EVENT_255：256 号以后的不清），保留几个宠物转生的旗标，
// 再按转生次数补上做完的 48..52、179（第 n 转补前 n 个，各给一次家族冒险声望 AddFMAdv），第 6 转另外补 63 / 136 / 175 / 176。
func (w *World) transFlags(p *entity) {
	c := &p.char
	var keepEnd, keepNow []int
	for _, ev := range transKeepEnd {
		if p.eventDone(ev) {
			keepEnd = append(keepEnd, ev)
		}
	}
	for _, ev := range transKeepNow {
		if p.eventNow(ev) {
			keepNow = append(keepNow, ev)
		}
	}
	high := func(s string) string {
		var out []int
		for _, ev := range parseFlags(s) {
			if ev >= 256 {
				out = append(out, ev)
			}
		}
		return flagsString(out)
	}
	c.EndEvents, c.NowEvents = high(c.EndEvents), high(c.NowEvents)
	for _, ev := range keepEnd {
		p.setEventDone(ev)
	}
	for _, ev := range keepNow {
		p.setEventNow(ev)
	}
	for i := 0; i < c.Trans && i < len(transSetEnd); i++ {
		p.setEventDone(transSetEnd[i])
		w.addFMAdv(p, transSetEnd[i])
	}
	if c.Trans == transMax {
		for _, ev := range transSixEvents {
			p.setEventDone(ev)
		}
	}
	p.questDirty = true
}

// ---------------- 宠物转生 ----------------

func (w *World) petTransReply(p, n *entity, win *npcWindow, button int, input string) {
	seg := petTransSegment(n.npc.Arg)
	cancel := func() { w.sendWindow(p, n, "message", BtnOK, seqPetTransEnd, transMsg(seg, "cancelmsg")) }
	switch win.seq {
	case seqPetTransStart:
		if button != BtnNext {
			return
		}
		if w.marefia(p, 0) == nil || !w.petTransCheck(p) {
			w.sendWindow(p, n, "message", BtnOK, seqPetTransEnd, transMsg(seg, "failmsg"))
			return
		}
		w.sendWindow(p, n, "message", BtnYesNo, seqPetTransSelect, transMsg(seg, "selectmsg"))
	case seqPetTransSelect:
		if button != BtnYes {
			cancel()
			return
		}
		w.sendWindow(p, n, "petselect", BtnCancel, seqPetTransSelPet, transMsg(seg, "setpetmsg"))
	case seqPetTransSelPet:
		i := data.Atoi(strings.TrimSpace(input)) - 1
		if button == BtnCancel || i < 0 || i >= PetSlots || p.pets.Pets[i] == nil {
			cancel()
			return
		}
		text := fmt.Sprintf("\n是[%s]没错吧？ \n\n %s", p.pets.Pets[i].displayName(), transMsg(seg, "transmsg"))
		w.sendWindow(p, n, "message", BtnYesNo, seqPetTransMain1, text)
		p.window.pet = i
	case seqPetTransMain1:
		if button != BtnYes {
			cancel()
			return
		}
		w.sendWindow(p, n, "message", BtnOK, seqPetTransEnd, w.petTransmigrate(p, win.pet, seg))
	}
}

// petTransCheck 是 NPC_PetTransManCheck：80 级以上、事件 4 / 69 / 70 做完、只带着一只玛蕾菲雅。
func (w *World) petTransCheck(p *entity) bool {
	if p.char.Level < transMinLevel {
		return false
	}
	for _, ev := range []int{4, 69, 70} {
		if !p.eventDone(ev) {
			return false
		}
	}
	n := 0
	for _, pet := range p.pets.Pets {
		if pet != nil && (pet.TempNo == transPetRebel || pet.TempNo == transPetRebl2) {
			if pet.Level > 79 {
				return false
			}
			n++
		}
	}
	return n == 1
}

// marefia 是 NPC_PetTransManStatus 找的那只：自己的 79 级玛蕾菲雅（要转的宠物还没转过时 718 或 401，转过一次时只认 401）。
func (w *World) marefia(p *entity, tran int) *Pet {
	for _, pet := range p.pets.Pets {
		if pet == nil || pet.Level != 79 || pet.Owner != p.char.ID {
			continue
		}
		if pet.TempNo == transPetRebl2 || (tran == 0 && pet.TempNo == transPetRebel) {
			return pet
		}
	}
	return nil
}

// petTransmigrate 是 NPC_PetTransMan_selectWindow(4) + NPC_PetTransManStatus + GetNewPet，回传要显示的话。
func (w *World) petTransmigrate(p *entity, slot int, seg string) string {
	pet := p.pets.Pets[slot]
	switch {
	case pet == nil || pet.Away:
		return transMsg(seg, "failmsg")
	case p.pets.Ride == slot:
		return "\n\n骑乘中的宠物不能转生喔！"
	case pet.Level < 80:
		return "\n\n接受转生祝福的宠物必需要等级80以上喔！"
	case pet.TempNo == transPetRebel || pet.TempNo == transPetRebl2:
		return "\n\n这只宠物不能转生喔！"
	case pet.Trans > 1:
		return "\n\n这只宠物已经接受过祝福了喔！"
	}
	m := w.marefia(p, pet.Trans)
	temp := w.game.Battle.Temps[pet.TempNo]
	if m == nil || temp == nil || !w.petTransCheck(p) {
		return transMsg(seg, "failmsg") // 原版这里不回窗口
	}
	alloc := func(a [4]int) (v [4]int, sum int) {
		for i := range a {
			v[i] = max(1, a[i])
			sum += v[i]
		}
		return
	}
	ma, total1 := alloc(m.Alloc)
	total1 = max(0, min(150, total1))
	pa, total2 := alloc(pet.Alloc)
	ans := petTransAns(total1, total2, pet.Level, pet.Rank, pet.Trans)
	total := total1 + total2*4
	var work [4]int
	for i := range work {
		work[i] = ans * (ma[i] + pa[i]*4) / total
	}
	// 交出玛蕾菲雅（同模板的都交出）
	mtemp := m.TempNo
	for i, q := range p.pets.Pets {
		if q != nil && q.TempNo == mtemp {
			w.giveUpPet(p, i)
		}
	}
	w.petReborn(p, pet, temp, work)
	w.sysMsg(p, pet.displayName()+" 接受了精灵王的祝福！", colorWhite)
	for i := data.EquipSlots; i < data.ItemSlots; i++ {
		if it := p.inv[i]; it != nil {
			for _, r := range petTransDelItems {
				if it.ID >= r[0] && it.ID <= r[1] {
					p.inv[i] = nil
				}
			}
		}
	}
	p.setEventNow(71)
	w.petsChanged(p)
	w.itemsChanged(p)
	w.flushQuests(p)
	return transMsg(seg, "main1msg")
}

// petTransAns 是 NPC_PetTransManGetAns（_PET_2TRANS）：新的成长总和 = (玛蕾菲雅的成长/100)^5 × 1.3（不到 1 当 0）+ 宠物的成长
// + (宠物等级（最多 130，_ATTESTAION_ID 1）− 100) / Fx，Fx = int((5 − 档次) × 1.2) + 5；一转最多 150、二转最多 200
// （_PET_TRANS_ABILITY 的 PETTRANSABILITY1 / 2 的预设值；那台服务端的 setup.cf 把一转调成 155，没照抄）。
func petTransAns(total1, total2, lv, rank, tran int) int {
	lv = min(lv, 130)
	t := float64(total1) / 100
	t = t * t * t * t * t
	if t < 1 {
		t = 0
	} else {
		t *= 1.3
	}
	fx := int(float64(5-rank)*1.2) + 5
	ans := int(t) + total2 + (lv-100)/fx
	if tran == 0 {
		return min(ans, 150)
	}
	return min(ans, 200)
}

// petReborn 是 GetNewPet：成长率各加 −2..+2 当作新的 ALLOCPOINT，按总和定档次，再随机加 10 点，
// 体腕耐敏 = 模板的初始成长次数 × 成长；变回 1 级、经验 0、技能栏 7 格、HP 补满、主人是自己、转生次数 +1。名字、技能照旧。
func (w *World) petReborn(p *entity, pet *Pet, temp *data.EnemyTemp, work [4]int) {
	for i := range work {
		work[i] = max(0, work[i]+w.rnd.IntN(5)-2)
	}
	pet.Alloc = work
	sum := work[0] + work[1] + work[2] + work[3]
	pet.Rank = 5
	for i, lo := range []int{130, 100, 95, 85, 80, 0} {
		if sum >= lo {
			pet.Rank = i
			break
		}
	}
	for i := 0; i < 10; i++ {
		work[w.rnd.IntN(4)]++
	}
	pet.Vital, pet.Str, pet.Tgh, pet.Dex = temp.InitNum*work[0], temp.InitNum*work[1], temp.InitNum*work[2], temp.InitNum*work[3]
	pet.Born = &[4]int{pet.Vital, pet.Str, pet.Tgh, pet.Dex}
	pet.Level, pet.Exp, pet.Slot = 1, 0, 7
	pet.Owner = p.char.ID
	pet.Trans++
	pet.HP = w.petMaxHP(pet)
}
