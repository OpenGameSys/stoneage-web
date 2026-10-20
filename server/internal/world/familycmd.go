package world

// 家族的指令（lssproto_FM_recv → CHAR_Family，family.c）与家族管理员（npc_familyman.c）。
// 回传的字串照原版：FM 的 "S|F|…"、"S|D|…"、"B|G|…"、"B|T|…"、"C|J|…" 用 FamilyMsg 送，其余是 NPC 窗口（npc 0）。

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 家族管理员的窗口编号（CHAR_WINDOWTYPE_FAMILYMAN_*）
const (
	seqFamilyAdd = 321
	seqFamilyOut = 323
)

// Family 是 lssproto_FM_recv → CHAR_Family：按第一段的字母分派。
func (w *World) Family(charID int, text string) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || text == "" || len(text) > 2048 || !utf8.ValidString(text) {
			return
		}
		f := fmSplit(text)
		defer w.famLock()()
		switch strings.ToLower(f[0]) {
		case "a":
			w.fmAdd(e, f)
		case "j":
			w.fmJoin(e, f)
		case "e":
			w.fmLeave(e)
		case "m":
			w.fmCheckMember(e, f)
		case "s":
			w.fmDetail(e, f)
		case "c":
			w.fmChannel(e, f)
		case "b":
			w.fmBank(e, f)
		case "p":
			w.fmSetPoint(e, f)
		case "t":
			w.fmSetAccept(e, f)
		case "x":
			w.fmFixRule(e, f)
		case "l":
			w.fmLeaderFunc(e, f)
		case "d":
			w.fmDengonMenu(e)
		}
	})
}

func fmInt(f []string, i int) int {
	if i < len(f) {
		if n, err := strconv.Atoi(strings.TrimSpace(f[i])); err == nil {
			return n
		}
	}
	return 0
}

// fmName 是家族名的规矩：不能有空格（FAMILY_Add），另外限制 16 个字节（客户端 CHAR_NAME_LEN）、不能有 | 与 \。
func fmNameOK(name string) bool {
	return name != "" && gbkLen(name) <= 16 && !strings.ContainsAny(name, "|\\\n\r\t")
}

func fmRule(s string) string {
	s = strings.NewReplacer("\n", " ", "\r", " ").Replace(strings.TrimSpace(s))
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120])
	}
	return s
}

// setGuard 把 slot 的宠物设为守护兽（CHAR_PETFAMILY），家族记下它的名字与"图号 攻 防 敏"。
func (w *World) setGuard(e *entity, fm *family, slot int) {
	w.clearGuard(e)
	p := e.pets.Pets[slot]
	p.Guard = true
	d, _ := w.petDerive(p)
	fm.PetName = p.displayName()
	fm.PetAttr = fmt.Sprintf("%d %d %d %d", p.Sprite, d.Atk, d.Def, d.Quick)
	w.petsChanged(e)
}

// fmAdd 是 FAMILY_Add：A|名字|宠物格位|守护精灵|主旨|徽章。
func (w *World) fmAdd(e *entity, f []string) {
	if e.fight != nil {
		return
	}
	if fm, _, _ := w.fmOf(e); fm != nil {
		w.fmMessage(e, "\n你已经加入家族罗～无法再成立家族！")
		return
	}
	switch checkLeaderQ(e) {
	case -1:
		w.fmMessage(e, "\n很抱歉喔！你的等级不足！")
		return
	case -2:
		w.fmMessage(e, "\n很抱歉喔！你必须先完成成人礼才行！")
		return
	}
	if e.char.Gold < fmFee {
		w.fmMessage(e, fmt.Sprintf("\n很抱歉喔！成立家族需要%d石币的手续费！", fmFee))
		return
	}
	name, slot, sprite, rule := fmField(f, 1), fmInt(f, 2), fmInt(f, 3), fmRule(fmField(f, 4))
	if name == "" || strings.ContainsAny(name, " 　") {
		w.fmMessage(e, "\n家族的名称请勿输入空格！")
		return
	}
	if !fmNameOK(name) || sprite < 0 || sprite > 1 {
		w.fmMessage(e, "\n申请成立家族失败！")
		return
	}
	if slot < 0 || slot >= PetSlots || e.pets.Pets[slot] == nil || e.pets.Pets[slot].Away {
		w.fmMessage(e, "\n请选择一只宠物作为家族守护兽！")
		return
	}
	if rule == "" {
		rule = "无"
	}
	fam := w.fam
	if fam.byName(name) != nil {
		w.fmMessage(e, "\n已经有相同名字的家族成立了！")
		return
	}
	id := fam.newID()
	if id < 0 {
		w.fmMessage(e, "\n申请成立家族失败！")
		return
	}
	e.char.Gold -= fmFee
	w.sendStatus(e)
	fm := &family{ID: id, Name: name, LeaderFace: e.char.Face, Accept: true, Rule: rule, Sprite: sprite, Created: w.now().Unix()}
	fm.Members[0] = &fmMember{ID: e.char.ID, Name: e.char.Name, Level: e.char.Level, Duty: fmDutyLeader, Fame: e.char.Fame, Event: true}
	w.setGuard(e, fm, slot)
	fm.recount()
	fam.list[id], fam.byChar[e.char.ID], fam.online[e.char.ID] = fm, fm, w
	w.resetMomentum(e, "成立家族个人气势归零")
	fam.save(fm)
	w.fmMessage(e, "\n恭喜你成立了新的家族！但请在７天之内召集到１０名族人加入，不然会取消家族资格喔。")
	w.famSync(e)
}

// fmJoin 是 FAMILY_Join：J|格位|fmindex|名字|守护精灵。加入后是"申请中"，等族长或长老审核。
func (w *World) fmJoin(e *entity, f []string) {
	if e.party != nil || e.fight != nil {
		return
	}
	if fm, _, _ := w.fmOf(e); fm != nil {
		w.fmMessage(e, "\n你已经加入其他家族了喔！")
		return
	}
	fm := w.fam.list[fmInt(f, 2)]
	if fm == nil || fm.Name != fmField(f, 3) {
		w.fmMessage(e, "\n申请加入家族失败！")
		return
	}
	if fm.num() >= fmMaxMember {
		w.fmMessage(e, "\n此家族目前无法招收成员，家族成员人数已到达上限！")
		return
	}
	if !fm.Accept {
		w.fmMessage(e, "\n此家族目前不愿意招收成员！")
		return
	}
	slot := -1
	for i := 1; i < fmMaxMember; i++ {
		if fm.Members[i] == nil {
			slot = i
			break
		}
	}
	if slot < 0 {
		w.fmMessage(e, "\n申请加入家族失败！")
		return
	}
	fm.Members[slot] = &fmMember{ID: e.char.ID, Name: e.char.Name, Level: e.char.Level, Duty: fmDutyApply, Fame: e.char.Fame, Event: checkLeaderQ(e) == 0}
	fm.recount()
	w.fam.byChar[e.char.ID], w.fam.online[e.char.ID] = fm, w
	w.fam.save(fm)
	w.fmMessage(e, "\n谢谢你的加入申请！请先等族长对你的审核通过之後，才算正式加入。")
	w.resetMomentum(e, "加入家族个人气势归零")
	w.fam.say(fm.leader().ID, fmt.Sprintf("(%s lv:%d)正要求加入您的家族喔！目前家族人数：%4d人", e.char.Name, e.char.Level, fm.num()), colorRed)
	w.famSync(e)
}

// fmLeave 是 FAMILY_Leave（E|1）：族长是解散家族，其他人退出。
func (w *World) fmLeave(e *entity) {
	if e.fight != nil {
		return
	}
	fm, i, m := w.fmOf(e)
	if fm == nil {
		w.fmMessage(e, "\n你并没有加入家族喔！")
		return
	}
	if m.Duty == fmDutyLeader {
		w.fmDismiss(e, fm)
		return
	}
	fm.Members[i] = nil
	delete(w.fam.byChar, e.char.ID)
	fm.recount()
	w.fam.save(fm)
	w.fam.say(fm.leader().ID, fmt.Sprintf("(%s)已经离开您的家族了！目前家族人数：%4d人", e.char.Name, fm.num()), colorRed)
	w.fmVarInit(e)
	w.resetMomentum(e, "退出家族个人气势归零")
	w.fmMessage(e, "\n申请退出家族ＯＫ！")
	w.famSync(e)
}

// fmDismiss 是 ACDelFM：放掉庄园，每个族人都退出（"你已经退出家族或家族已经不存在了！"）。
func (w *World) fmDismiss(e *entity, fm *family) {
	fam := w.fam
	if i := fam.manorOf(fm); i >= 0 {
		fam.manors[i] = manorState{}
		fam.saveKV("manors", fam.manors)
	}
	for _, m := range fm.Members {
		if m == nil {
			continue
		}
		delete(fam.byChar, m.ID)
		delete(fam.change, m.ID)
		if m.ID != e.char.ID {
			fam.say(m.ID, "你已经退出家族或家族已经不存在了！", colorYellow)
			fam.resync(m.ID, true)
		}
	}
	fam.remove(fm)
	w.fmVarInit(e)
	w.fmMessage(e, "\n家族已经解散了！")
	w.famSync(e)
}

// fmCheckMember 是 FAMILY_CheckMember：M|名字|格位|职位（1 一般族员 / −1 退出 / 4 长老），族长或长老才能改。
// 名字原版没有跳脱，取第一段与最后两段之间的全部。
func (w *World) fmCheckMember(e *entity, f []string) {
	fm, _, me := w.fmOf(e)
	if fm == nil || len(f) < 4 {
		return
	}
	if me.Duty != fmDutyLeader && me.Duty != fmDutyElder {
		w.fmMessage(e, "\n你不是族长，所以没有修改的权力唷！")
		return
	}
	name := fmUnescape(strings.Join(f[1:len(f)-2], "|"))
	slot, duty := fmInt(f, len(f)-2), fmInt(f, len(f)-1)
	if slot < 0 || slot >= fmMaxMember {
		return
	}
	t := fm.Members[slot]
	if t == nil || t.Name != name || t.ID == e.char.ID || t.Duty == fmDutyLeader {
		return
	}
	if me.Duty == fmDutyElder && t.Duty == fmDutyElder { // 客户端也不让长老改长老
		return
	}
	fam := w.fam
	switch duty {
	case fmDutyMember:
		if t.Duty == fmDutyMember {
			return
		}
		was := t.Duty
		t.Duty = fmDutyMember
		fm.recount()
		fam.save(fm)
		if was == fmDutyApply {
			fam.say(t.ID, fmt.Sprintf("恭喜你！%s已经审核完毕您的加入申请！", e.char.Name), colorRed)
		} else {
			fam.say(t.ID, fmt.Sprintf("%s族长已经将你的家族职等改为%s！", e.char.Name, fmDutyName[fmDutyMember]), colorRed)
		}
		fam.resync(t.ID, false)
	case fmDutyNone:
		fm.Members[slot] = nil
		delete(fam.byChar, t.ID)
		delete(fam.change, t.ID)
		fm.recount()
		fam.save(fm)
		fam.say(t.ID, fmt.Sprintf("%s已经将你踢出家族了！", e.char.Name), colorRed)
		fam.say(t.ID, "你已经退出家族或家族已经不存在了！", colorYellow)
		fam.resync(t.ID, true)
	case fmDutyElder:
		if me.Duty != fmDutyLeader || t.Duty == fmDutyApply || !t.Event {
			return
		}
		if fm.elders() >= fmMaxElder {
			w.fmMessage(e, fmt.Sprintf("\n只能指派%d位家族成员成为%s喔！", fmMaxElder, fmDutyName[fmDutyElder]))
			return
		}
		t.Duty = fmDutyElder
		fam.save(fm)
		w.fmMessage(e, fmt.Sprintf("\n你已经将%s的职等改为%s了！", t.Name, fmDutyName[fmDutyElder]))
		fam.say(t.ID, fmt.Sprintf("%s族长已经将你的家族职等改为%s！", e.char.Name, fmDutyName[fmDutyElder]), colorRed)
		fam.resync(t.ID, false)
	}
}

// fmSetAccept 是 FAMILY_SetAcceptFlag：T|0 / T|1。
func (w *World) fmSetAccept(e *entity, f []string) {
	fm, _, me := w.fmOf(e)
	if fm == nil || me.Duty != fmDutyLeader && me.Duty != fmDutyElder {
		return
	}
	fm.Accept = fmInt(f, 1) == 1
	w.fam.save(fm)
}

// fmDetail 是 FAMILY_Detail：S|F|页（家族列表）、S|D|名字|fmindex|格位（家族资料）、S|D2（自己家族的资料）、S|P（个人声望）。
func (w *World) fmDetail(e *entity, f []string) {
	fam := w.fam
	switch strings.ToUpper(fmField(f, 1)) {
	case "F":
		list := fam.sorted()
		pages := max(1, (len(list)+9)/10)
		page := min(max(fmInt(f, 2), 1), pages)
		var sb strings.Builder
		n := 0
		for _, fm := range list[min((page-1)*10, len(list)):min(page*10, len(list))] {
			fmt.Fprintf(&sb, "|%s", fmEscape(fmt.Sprintf("%d %s %s %d %d %d %d %d", fm.ID, fm.Name, fm.leader().Name, fm.total(),
				fm.joinNum(), fm.ID-1, b2i(fm.Setup), b2i(fm.Accept))))
			n++
		}
		e.client.Send(&proto.FamilyMsg{Text: fmt.Sprintf("S|F|%d|%d|%d%s", len(list), page, n, sb.String())})
	case "D":
		fm := fam.list[fmInt(f, 3)]
		if fm == nil {
			return
		}
		e.client.Send(&proto.FamilyMsg{Text: strings.Join([]string{"S", "D", strconv.Itoa(fmInt(f, 4)), strconv.Itoa(fm.ID),
			fmEscape(fm.Name), fmEscape(fm.leader().Name), strconv.Itoa(fm.LeaderFace), fmEscape(fm.PetName), fm.PetAttr,
			strconv.Itoa(fm.joinNum()), strconv.Itoa(b2i(fm.Accept)), fmEscape(fm.Rule), strconv.Itoa(fm.Sprite), "0"}, "|")})
	case "D2":
		fm, _, m := w.fmOf(e)
		if fm == nil {
			return
		}
		rank := 0
		for i, x := range fam.rank((*family).total) {
			if x == fm {
				rank = i + 1
			}
		}
		text := strings.Join([]string{fmEscape(fm.Name), strconv.Itoa(fm.joinNum()), fmEscape(fm.leader().Name), strconv.Itoa(rank),
			strconv.Itoa(fm.total() / 100), strconv.Itoa(e.char.Fame / 100), strconv.Itoa(m.Duty), strconv.Itoa(fm.Sprite), "无挑战排程",
			strconv.Itoa(fm.momentum() / 100), strconv.Itoa(e.fmc.Momentum / 100), "0"}, "|")
		w.fmWindow(e, "fmdetail", BtnOK, seqNone, text)
	case "P":
		// FreeDataInfoShow 是 Lua；照 C 的版本说两行
		w.sysMsg(e, fmt.Sprintf("你目前的个人声望点数为：%d", e.char.Fame/100), colorWhite)
		w.sysMsg(e, fmt.Sprintf("你目前的个人气势点数为：%d", e.fmc.Momentum/100), colorWhite)
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// fmChannel 是 FAMILY_Channel：C|J|5 族长广播、C|J|-1 关掉。
func (w *World) fmChannel(e *entity, f []string) {
	fm, _, m := w.fmOf(e)
	if fm == nil {
		w.fmMessage(e, "\n你还没有加入任何家族！")
		return
	}
	if strings.ToUpper(fmField(f, 1)) != "J" {
		return
	}
	switch fmInt(f, 2) {
	case fmChannelLeader:
		if m.Duty != fmDutyLeader {
			return
		}
		e.fm.Channel = fmChannelLeader
		w.sysMsg(e, "启动族长广播。", colorWhite)
	case -1:
		e.fm.Channel = 0
		w.sysMsg(e, "关闭族长广播。", colorWhite)
	default:
		return
	}
	e.client.Send(&proto.FamilyMsg{Text: fmt.Sprintf("C|J|%d", e.fm.Channel)})
	w.famSync(e)
}

// fmBank 是 FAMILY_Bank：B|G|存入（负数是取出，家族银行个人帐户 CHAR_BANKGOLD）、B|T|存入（家族共同帐户 fmgold）。
func (w *World) fmBank(e *entity, f []string) {
	fm, _, m := w.fmOf(e)
	amount := fmInt(f, 2)
	switch strings.ToUpper(fmField(f, 1)) {
	case "G":
		bank := e.fmc.Bank
		if fm == nil && bank < 1 {
			w.sysMsg(e, "你必须先加入家族。", colorWhite)
			return
		}
		cash := e.char.Gold - amount
		if amount == 0 || cash < 0 || cash > maxGold(e.char.Trans) {
			return
		}
		if amount > 0 {
			if fm == nil {
				w.sysMsg(e, "抱歉！你没有加入任何家族，所以仅能领取存款", colorWhite)
				return
			}
			if bank+amount > fmMaxBank {
				return
			}
		} else if bank+amount < 0 {
			w.sysMsg(e, "抱歉！你的家族银行个人帐户没有这么多存款", colorWhite)
			return
		}
		e.char.Gold, e.fmc.Bank = cash, bank+amount
		e.char.Family = e.fmc.String()
		w.sendStatus(e)
		w.saveProgress(e)
		if amount > 0 {
			w.sysMsg(e, fmt.Sprintf("存入%d到家族银行个人帐户。", amount), colorWhite)
		} else {
			w.sysMsg(e, fmt.Sprintf("从家族银行个人帐户取出%d。", -amount), colorWhite)
		}
	case "T":
		if fm == nil || amount == 0 {
			return
		}
		if amount > 0 {
			if e.char.Gold < amount || fm.Gold+amount > fmMaxGold {
				return
			}
			w.sysMsg(e, "家族银行存款处理中....", colorYellow)
		} else {
			if m.Duty != fmDutyLeader && m.Duty != fmDutyElder || fm.Gold+amount < 0 || e.char.Gold-amount > maxGold(e.char.Trans) {
				return
			}
			w.sysMsg(e, "家族银行取款处理中....", colorYellow)
		}
		e.char.Gold -= amount
		fm.Gold += amount
		w.fam.save(fm)
		w.sendStatus(e)
		w.saveProgress(e)
	}
}

// fmFixRule 是 FAMILY_FixRule（族长才能改）：X|R|主旨、X|P|宠物格位（换守护兽）；X|B（徽章）没有做。
func (w *World) fmFixRule(e *entity, f []string) {
	fm, _, m := w.fmOf(e)
	if fm == nil {
		return
	}
	if m.Duty != fmDutyLeader {
		w.fmMessage(e, "\n你不是族长，所以没有修改的权力唷！")
		return
	}
	switch strings.ToUpper(fmField(f, 1)) {
	case "R":
		rule := fmRule(fmField(f, 2))
		if rule == "" {
			w.fmMessage(e, "\n家族主旨不可为空白唷！")
			return
		}
		fm.Rule = rule
		w.fam.save(fm)
		w.sysMsg(e, "家族主旨修改ＯＫ！", colorWhite)
	case "P":
		slot := fmInt(f, 2)
		if slot < 0 || slot >= PetSlots || e.pets.Pets[slot] == nil || e.pets.Pets[slot].Away {
			return
		}
		w.setGuard(e, fm, slot)
		w.fam.save(fm)
	}
}

// fmLeaderFunc 是 FAMILY_LeaderFunc：L|F|1 成员管理、L|L|1/2 邀请函 / 骑乘同意书、L|CHANGE|… 族长让位。
func (w *World) fmLeaderFunc(e *entity, f []string) {
	fm, slot, m := w.fmOf(e)
	if fm == nil {
		return
	}
	switch strings.ToUpper(fmField(f, 1)) {
	case "F":
		if m.Duty != fmDutyLeader && m.Duty != fmDutyElder {
			return
		}
		w.fmWindow(e, "message", BtnOK, seqFmMessage2, "               『族 长 需 知』\n请小心处理族员的资料，一经修改後就无法回复原态，敬请小心。")
	case "L":
		if m.Duty != fmDutyLeader && m.Duty != fmDutyElder {
			return
		}
		w.fmLetter(e, fm, fmInt(f, 2))
	case "CHANGE":
		w.fmLeaderChange(e, fm, slot, m, f)
	}
}

// fmLeaderChange 是 FAMILY_LeaderFunc 的 "CHANGE"：
//
//	L：族长列出在线、可以当族长的正式族人（WN 32）；Q|格位|名字：问那个人（WN 34）；A|0/1|…：被问的人回答。
//
// 原版的编号是在线名单的格位，这里用家族名单的格位。
func (w *World) fmLeaderChange(e *entity, fm *family, slot int, m *fmMember, f []string) {
	fam := w.fam
	switch strings.ToUpper(fmField(f, 2)) {
	case "L":
		if m.Duty != fmDutyLeader {
			return
		}
		var sb strings.Builder
		n := 0
		for i, x := range fm.Members {
			if x != nil && i != slot && x.formal() && x.Event && fam.online[x.ID] != nil {
				fmt.Fprintf(&sb, "|%d|%s", i, fmEscape(x.Name))
				n++
			}
		}
		w.fmWindow(e, "fmleaders", 0, seqNone, fmt.Sprintf("L|CHANGE|L|%d%s", n, sb.String()))
	case "Q":
		if m.Duty != fmDutyLeader {
			return
		}
		j := fmInt(f, 3)
		if j <= 0 || j >= fmMaxMember {
			return
		}
		t := fm.Members[j]
		if t == nil || t.Name != fmField(f, 4) || !t.formal() || !t.Event || fam.online[t.ID] == nil {
			return
		}
		fam.change[t.ID] = e.char.ID
		text := fmEscape(e.char.Name) + "|" + strconv.Itoa(slot)
		fam.tell(t.ID, func(w2 *World, o *entity) { w2.fmWindow(o, "fmleaderask", 0, seqNone, text) })
	case "A":
		leaderID, ok := fam.change[e.char.ID]
		delete(fam.change, e.char.ID)
		if !ok {
			return
		}
		ld := fm.leader()
		if ld.ID != leaderID {
			return
		}
		if fmInt(f, 3) != 1 {
			fam.window(ld.ID, "\n对不起！对方不愿意接受！")
			return
		}
		if !m.formal() || slot <= 0 {
			fam.say(ld.ID, "族长让位失败！", colorYellow)
			w.sysMsg(e, "族长让位失败！", colorYellow)
			return
		}
		// AC：族长与对方换格位、守护兽清掉（新族长要再到家族管理员那里选）
		fm.Members[0], fm.Members[slot] = m, ld
		m.Duty, ld.Duty = fmDutyLeader, fmDutyMember
		fm.LeaderFace, fm.PetName, fm.PetAttr = e.char.Face, "", ""
		fam.save(fm)
		w.clearGuard(e)
		w.fmMessage(e, "\n恭喜你！你已经是新任的族长了。\n请好好的努力吧！\n对了～记得请先到村长家的家族管理员选择\n新的家族守护兽，否则家族将会被解散唷！")
		w.famSync(e)
		fam.window(ld.ID, fmt.Sprintf("\n辛苦你了！你已经将族长的位子交给%s了。", e.char.Name))
		fam.tell(ld.ID, func(w2 *World, o *entity) {
			defer w2.famLock()()
			w2.clearGuard(o)
			w2.famSync(o)
		})
	}
}

// familyChat 是 CHAR_Talk 的家族频道（_CHANNEL_MODIFY 的 /FM）：族长开着广播时送"[族长广播]名字: 话"给一般族员与长老，
// 否则送给开着家族频道（CHAR_ISFM）的族人。
func (w *World) familyChat(e *entity, msg *proto.ChatMsg) {
	defer w.famLock()()
	fm, _, m := w.fmOf(e)
	if fm == nil {
		return
	}
	fam := w.fam
	if e.fm.Channel == fmChannelLeader && m.Duty == fmDutyLeader {
		text := fmt.Sprintf("[族长广播]%s: %s", e.char.Name, msg.Text)
		w.sysMsg(e, text, msg.Color)
		for _, x := range fm.Members {
			if x != nil && (x.Duty == fmDutyMember || x.Duty == fmDutyElder) {
				fam.say(x.ID, text, msg.Color)
			}
		}
		return
	}
	out := *msg
	out.Channel = "family"
	e.client.Send(&out)
	for _, x := range fm.Members {
		if x == nil || x.ID == e.char.ID {
			continue
		}
		fam.tell(x.ID, func(_ *World, o *entity) {
			if o.fs&fsFM != 0 {
				o.client.Send(&out)
			}
		})
	}
}

// ---------------- 家族管理员 ----------------

// familymanTalk 是 NPC_FamilymanLooked：面前一格才理会。
func (w *World) familymanTalk(p, n *entity) {
	if max(abs(n.x-p.x), abs(n.y-p.y)) > 1 {
		return
	}
	w.windowmanOpen(p, n, 1)
}

// familymanReply 是 NPC_FamilymanWindowTalked：去 5 成立（检查后开 WN 14）、6 家族列表、7 退出或解散，其余照设定档翻窗口。
func (w *World) familymanReply(p, n *entity, seq, button int, sel string) {
	if max(abs(n.x-p.x), abs(n.y-p.y)) > 1 {
		return
	}
	if seq == seqFamilyOut {
		if button == BtnYes {
			defer w.famLock()()
			w.fmLeave(p)
		}
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
	defer w.famLock()()
	fm, _, m := w.fmOf(p)
	switch next {
	case 5:
		switch {
		case fm != nil:
			w.fmMessage(p, "\n很抱歉喔！你已经加入家族了！")
		case p.char.Trans == 0 && p.char.Level < fmLeaderLv:
			w.fmMessage(p, "\n很抱歉喔！你的等级不足！")
		case !p.eventDone(4):
			w.fmMessage(p, "\n很抱歉喔！你必须先完成成人礼才行！")
		case p.char.Gold < fmFee:
			w.fmMessage(p, "\n很抱歉喔！你的申请手续费不足！")
		default:
			w.sendWindow(p, n, "familyadd", 0, seqFamilyAdd, "Hello!!")
		}
	case 6:
		w.fmDetail(p, []string{"S", "F", "1", "0"})
	case 7:
		switch {
		case fm == nil:
			w.fmMessage(p, "\n很抱歉喔！你还没有加入任何家族呀！")
		case m.Duty == fmDutyLeader:
			w.sendWindow(p, n, "message", BtnYesNo, seqFamilyOut, "\n您现在是这个家族的族长喔...\n\n家族解散了就无法再救回唷！～\n\n确定要解散家族吗？")
		default:
			w.fmLeave(p)
		}
	default:
		w.windowmanOpen(p, n, next)
	}
}
