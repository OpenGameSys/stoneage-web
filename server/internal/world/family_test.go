package world

import (
	"strings"
	"testing"

	"sa/server/internal/proto"
)

// 家族：成立、申请加入与审核、家族频道、银行两种帐户、留言板、踢人、庄园申请与条件、解散。
func TestFamily(t *testing.T) {
	w, db, g := setup(t)
	cl, cll := petOwner(t, w, db, "族长甲", 30, 60)
	cm, clm := petOwner(t, w, db, "族员乙", 10, 60)
	l, m := int(cl.ID), int(cm.ID)
	givePet(t, w, l, petEnemy(t, g, 1), 5)
	w.Do(func() {
		w.players[l].char.Gold = 25000
		w.players[m].char.Gold = 1000
	})
	cll.take()
	// 没做成人礼不能成立
	w.Family(l, "A|测试家族|0|0|主旨|0")
	if !strings.Contains(fmtMsgs(cll.take()), "成人礼") {
		t.Fatal("没做成人礼应被拒绝")
	}
	w.Do(func() { w.players[l].setEventDone(4) })
	w.Family(l, "A|测试 家族|0|0|主旨|0")
	if !strings.Contains(fmtMsgs(cll.take()), "请勿输入空格") {
		t.Fatal("名字有空格应被拒绝")
	}
	w.Family(l, "A|测试家族|0|0|主旨|0")
	msgs := cll.take()
	if !strings.Contains(fmtMsgs(msgs), "恭喜你成立了新的家族") {
		t.Fatalf("应成立家族：%s", fmtMsgs(msgs))
	}
	if st, ok := find[*proto.FamilyStatus](msgs); !ok || st.Name != "测试家族" || st.Duty != fmDutyLeader {
		t.Fatalf("F 状态不对：%+v", st)
	}
	w.Do(func() {
		e := w.players[l]
		if e.char.Gold != 25000-fmFee || !e.pets.Pets[0].Guard || e.fs&fsFM == 0 {
			t.Errorf("应扣手续费、设守护兽、开家族频道：%d %v %d", e.char.Gold, e.pets.Pets[0].Guard, e.fs)
		}
	})
	w.Family(l, "A|再一个|0|0|主旨|0")
	if !strings.Contains(fmtMsgs(cll.take()), "无法再成立家族") {
		t.Error("已经有家族不能再成立")
	}

	// 列表与申请加入
	w.Family(m, "S|F|1|0")
	if fm, ok := find[*proto.FamilyMsg](clm.take()); !ok || !strings.HasPrefix(fm.Text, "S|F|1|1|1|1 测试家族 族长甲 ") {
		t.Fatalf("家族列表不对：%+v", fm)
	}
	w.Family(m, "J|0|1|测试家族|0")
	if !strings.Contains(fmtMsgs(clm.take()), "谢谢你的加入申请") {
		t.Fatal("应收到申请")
	}
	if !strings.Contains(fmtMsgs(cll.take()), "(族员乙 lv:10)正要求加入您的家族喔！") {
		t.Error("族长应收到申请的通知")
	}
	// 申请中的人不能用家族的布告栏成员列表、族长审核
	w.Family(m, "M|族长甲|0|-1")
	if !strings.Contains(fmtMsgs(clm.take()), "你不是族长") {
		t.Error("申请中的人不能改别人")
	}
	w.Family(l, "M|族员乙|1|1")
	msgs = clm.take()
	if !strings.Contains(fmtMsgs(msgs), "恭喜你！族长甲已经审核完毕您的加入申请！") {
		t.Fatalf("应审核通过：%s", fmtMsgs(msgs))
	}
	if st, ok := find[*proto.FamilyStatus](msgs); !ok || st.Duty != fmDutyMember {
		t.Errorf("审核后应是一般成员：%+v", st)
	}
	// 等级不够当不了长老
	w.Family(l, "M|族员乙|1|4")
	w.Do(func() {
		_, mm := w.fam.list[1].member(cm.ID)
		if mm.Duty != fmDutyMember {
			t.Error("没过 CheckLeaderQ 的不能当长老")
		}
	})

	// 家族频道
	cll.take()
	w.Chat(m, "family", "大家好", 0, 3)
	if c, ok := find[*proto.ChatMsg](cll.take()); !ok || c.Channel != "family" || c.Text != "大家好" {
		t.Errorf("族长应听到家族频道：%+v", c)
	}
	// 族长广播：只送一般族员与长老
	w.Family(l, "C|J|5")
	cll.take()
	w.Chat(l, "family", "集合", 0, 3)
	if !strings.Contains(fmtMsgs(clm.take()), "[族长广播]族长甲: 集合") {
		t.Error("族员应收到族长广播")
	}
	w.Family(l, "C|J|-1")

	// 家族银行：个人帐户与共同帐户
	w.Family(m, "B|G|300")
	w.Family(m, "B|T|200")
	w.Family(m, "B|T|-100") // 一般成员不能领共同帐户
	w.Do(func() {
		e := w.players[m]
		if e.fmc.Bank != 300 || e.char.Gold != 500 || w.fam.list[1].Gold != 200 {
			t.Errorf("存款不对：个人 %d 现金 %d 共同 %d", e.fmc.Bank, e.char.Gold, w.fam.list[1].Gold)
		}
	})
	w.Family(m, "B|G|-400")
	if !strings.Contains(fmtMsgs(clm.take()), "没有这么多存款") {
		t.Error("领太多应被拒绝")
	}

	// 家族留言板（布告栏 → 家族留言）
	w.Family(m, "D")
	if win, ok := find[*proto.NpcWindow](clm.take()); !ok || win.Seq != seqFmSelect {
		t.Fatal("应打开布告栏")
	}
	w.NpcWindowReply(m, 0, seqFmSelect, 0, "2")
	if win, ok := find[*proto.NpcWindow](clm.take()); !ok || win.Kind != "fmboard" {
		t.Fatal("应打开家族留言")
	}
	w.NpcWindowReply(m, 0, seqFmDengon, BtnOK, "6|有人吗")
	if win, ok := find[*proto.NpcWindow](clm.take()); !ok || !strings.HasPrefix(win.Text, "有人吗|") || !strings.Contains(win.Text, "族员乙\n") {
		t.Errorf("留言不对：%+v", win)
	}

	// 庄园：人数、声望不够不能申请；补够后申请，族人能过庄园的门
	w.Family(l, "P|0|100|607|563")
	if !strings.Contains(fmtMsgs(cll.take()), "声望不足") {
		t.Error("声望不够应被拒绝")
	}
	w.Do(func() { w.gmFamily(w.players[l], "fame", 1000) })
	w.Family(l, "P|0|100|607|563")
	if !strings.Contains(fmtMsgs(cll.take()), "人数未达申请标准") {
		t.Error("人数不够应被拒绝")
	}
	w.Do(func() { w.gmFamily(w.players[l], "fill", 8) })
	w.Family(l, "P|0|100|607|563")
	if !strings.Contains(fmtMsgs(cll.take()), "申请家族据点ＯＫ") {
		t.Fatal("应申请到庄园")
	}
	w.Do(func() {
		e := w.players[m]
		if e.fm.Floor != 1041 || !w.condCheck(e, "LV>0&MANOR=0") || w.condCheck(e, "MANOR=1") {
			t.Errorf("族人应能进庄园：%+v", e.fm)
		}
	})
	// 邀请函、庄园的店的税
	w.Family(l, "L|L|1")
	if !strings.Contains(fmtMsgs(cll.take()), "成功") {
		t.Error("应做出邀请函")
	}
	w.Do(func() {
		w.manorTax(&entity{floor: 1046}, 1000)
		if w.fam.list[1].Gold != 300 {
			t.Errorf("庄园税应进共同帐户：%d", w.fam.list[1].Gold)
		}
	})

	// 踢人
	w.Family(l, "M|族员乙|1|-1")
	msgs = clm.take()
	if !strings.Contains(fmtMsgs(msgs), "族长甲已经将你踢出家族了！") {
		t.Fatalf("应被踢出：%s", fmtMsgs(msgs))
	}
	if st, ok := findLast[*proto.FamilyStatus](msgs); !ok || st.Index != 0 {
		t.Errorf("被踢后应没有家族：%+v", st)
	}
	// 解散：庄园放掉
	w.Family(l, "E|1")
	if !strings.Contains(fmtMsgs(cll.take()), "家族已经解散了") {
		t.Fatal("应解散")
	}
	w.Do(func() {
		if len(w.fam.list) != 0 || w.fam.manors[0].Owner != 0 || w.players[l].pets.Pets[0].Guard {
			t.Error("解散后家族、庄园、守护兽都应清掉")
		}
	})
}

// 族长让位：列出在线的人、问对方、对方答应后换人，守护兽清掉。
func TestFamilyLeaderChange(t *testing.T) {
	w, db, g := setup(t)
	cl, cll := petOwner(t, w, db, "老族长", 30, 60)
	cm, clm := petOwner(t, w, db, "新族长", 30, 60)
	l, m := int(cl.ID), int(cm.ID)
	givePet(t, w, l, petEnemy(t, g, 1), 5)
	w.Do(func() {
		w.players[l].char.Gold = 20000
		w.players[l].setEventDone(4)
		w.players[m].setEventDone(4)
	})
	w.Family(l, "A|让位家族|0|1|主旨|0")
	w.Family(m, "J|0|1|让位家族|1")
	w.Family(l, "M|新族长|1|1")
	cll.take()
	clm.take()
	w.Family(l, "L|CHANGE|L")
	if win, ok := find[*proto.NpcWindow](cll.take()); !ok || win.Kind != "fmleaders" || win.Text != "L|CHANGE|L|1|1|新族长" {
		t.Fatalf("候选名单不对：%+v", win)
	}
	w.Family(l, "L|CHANGE|Q|1|新族长")
	if win, ok := find[*proto.NpcWindow](clm.take()); !ok || win.Kind != "fmleaderask" || win.Text != "老族长|0" {
		t.Fatalf("对方应被问：%+v", win)
	}
	w.Family(m, "L|CHANGE|A|1|老族长|0")
	if !strings.Contains(fmtMsgs(clm.take()), "你已经是新任的族长了") {
		t.Fatal("应成为新族长")
	}
	if !strings.Contains(fmtMsgs(cll.take()), "你已经将族长的位子交给新族长了") {
		t.Error("老族长应收到通知")
	}
	w.Do(func() {
		fm := w.fam.list[1]
		if fm.leader().ID != cm.ID || fm.PetName != "" || w.players[l].pets.Pets[0].Guard || w.players[l].fm.Duty != fmDutyMember {
			t.Errorf("让位后不对：%+v", fm.leader())
		}
	})
}

func findLast[T proto.Message](msgs []proto.Message) (T, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if v, ok := msgs[i].(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}
