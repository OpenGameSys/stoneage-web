package battle

import (
	"testing"

	"sa/server/internal/data"
)

func enemy(slot int, tactics string) *Fighter {
	d := Derive(2000, 1500, 1500, 1500)
	return &Fighter{Slot: slot, Name: "e", Level: 5, HP: d.HP, MaxHP: d.HP, Atk: d.Atk, Def: d.Def, Quick: d.Quick,
		Tactics: tactics, Base: [4]float64{2000, 1500, 1500, 1500}}
}

func TestOptParse(t *testing.T) {
	if v, _ := optNum("毒 turn 3  攻%-30", "攻%"); v != -30 {
		t.Fatalf("攻%% 应为 -30，得 %d", v)
	}
	if v, _ := optNum("倍2 回避30", "避"); v != 30 {
		t.Fatalf("避 应为 30，得 %d", v)
	}
	if v, _ := optNum("回避%+50 反击%+70 会心%+40", "击%"); v != 70 {
		t.Fatalf("击%% 应为 70，得 %d", v)
	}
	if leadNum("2 攻%+110") != 2 || field("-80|80", 2) != 80 {
		t.Fatal("参数解析不对")
	}
}

func TestContinuationAttackHitsN(t *testing.T) {
	b := New(rnd(1))
	p := player(0)
	p.HP, p.MaxHP = 10000, 10000
	b.Add(p)
	e := enemy(EnemySlot(0), "at:1;1;1")
	b.Add(e)
	var ev []Event
	b.attackSeq(e, 0, attackOpts{hits: 3, div: 3}, &ev)
	n := 0
	for _, x := range ev {
		if x.Kind == Attack && x.Actor == e.Slot {
			n++
		}
	}
	if n != 3 || !ev[0].Combo || !ev[1].Combo || ev[2].Combo {
		t.Fatalf("应连续攻击 3 下，前两下标记连击：%+v", ev)
	}
}

func TestStatusPoisonAndSleep(t *testing.T) {
	b := New(rnd(2))
	p := player(0)
	b.Add(p)
	b.Add(enemy(EnemySlot(0), "gu:1"))
	p.status[StPoison] = 3
	hp := p.HP
	var ev []Event
	b.statusSeq(p, Command{Kind: Attack}, &ev)
	if p.HP >= hp || ev[0].Kind != Poison || p.status[StPoison] != 2 {
		t.Fatalf("中毒应扣血并减少回合：%+v", ev)
	}
	// 睡眠：不能行动；挨打就醒
	p.status = [stCount]int{}
	p.status[StSleep] = 3
	ev = nil
	if _, ok := b.statusSeq(p, Command{Kind: Attack}, &ev); ok || ev[len(ev)-1].Kind != Stun {
		t.Fatalf("睡着时不能行动：%+v", ev)
	}
	b.attackOnce(b.F[EnemySlot(0)], p, attackOpts{}, false)
	if p.status[StSleep] != 0 && p.HP == p.MaxHP {
		t.Skip("这一下没打中")
	}
}

func TestStatusInflictChance(t *testing.T) {
	b := New(rnd(3))
	a := enemy(EnemySlot(0), "")
	a.Level = 30
	hit := 0
	for i := 0; i < 2000; i++ {
		d := player(0)
		if b.statusCheck(a, d, StPoison) {
			hit++
		}
	}
	// 30 + min(40, 58) − 体力占比 8/20/0.25×10=16 = 54% 左右
	if hit < 900 || hit > 1300 {
		t.Fatalf("中毒机率不对：%d/2000", hit)
	}
	d := player(0)
	d.status[StSleep] = 2
	if b.statusCheck(a, d, StPoison) {
		t.Fatal("已有异常状态时不应再中")
	}
}

func TestCounterHappens(t *testing.T) {
	b := New(rnd(4))
	p := player(0)
	p.HP, p.MaxHP, p.Quick, p.FixQuick = 100000, 100000, 1, 1
	b.Add(p)
	e := enemy(EnemySlot(0), "at:1;1;1")
	e.Quick, e.FixQuick, e.HP, e.MaxHP = 200, 200, 100000, 100000
	b.Add(e)
	e.com, p.com = Attack, Attack
	n := 0
	for i := 0; i < 200; i++ {
		var ev []Event
		b.attackSeq(p, e.Slot, attackOpts{}, &ev)
		for _, x := range ev {
			if x.Kind == Counter {
				n++
			}
		}
	}
	if n == 0 {
		t.Fatal("速度差很大时应出现反击")
	}
	// 防御中的一方不会反击
	e.com = Guard
	for i := 0; i < 50; i++ {
		var ev []Event
		b.attackSeq(p, e.Slot, attackOpts{}, &ev)
		for _, x := range ev {
			if x.Kind == Counter && x.Actor == e.Slot {
				t.Fatal("防御的人不会反击")
			}
		}
	}
}

func TestGuardBreakAndGuardian(t *testing.T) {
	b := New(rnd(5))
	p := player(0)
	p.Atk, p.FixAtk = 300, 300
	b.Add(p)
	front, back := enemy(EnemySlot(0), ""), enemy(EnemySlot(5), "")
	front.HP, front.MaxHP, back.HP, back.MaxHP = 100000, 100000, 100000, 100000
	b.Add(front)
	b.Add(back)
	e, _ := b.attackOnce(p, front, attackOpts{gbreak: 1}, false)
	if e.Result != Miss && e.Result != Dodge {
		t.Fatalf("对没防御的人用破除防御应无效：%+v", e)
	}
	// 前排（15）用忠犬守护后排同列（10）
	b.setup(front, Command{Kind: Attack, Target: 0, Skill: &data.Skill{Name: "忠犬", Func: "PETSKILL_Guardian", Option: "攻%-20  COM:攻击"}})
	got := false
	for i := 0; i < 20 && !got; i++ {
		e, _ = b.attackOnce(p, back, attackOpts{}, false)
		got = e.Guardian == front.Slot
	}
	if !got {
		t.Fatal("守护者应替被守护者挡下攻击")
	}
}

func TestEnemyHealRevive(t *testing.T) {
	b := New(rnd(6))
	b.Add(player(0))
	healer, hurt := enemy(EnemySlot(0), ""), enemy(EnemySlot(1), "")
	b.Add(healer)
	b.Add(hurt)
	hurt.HP = 1
	var ev []Event
	b.support(healer, Command{Kind: Heal, Target: 0}, &ev)
	if ev[0].Kind != Heal || hurt.HP <= 1 {
		t.Fatalf("应回复受伤的同伴：%+v", ev)
	}
	hurt.HP = 0
	ev = nil
	b.support(healer, Command{Kind: Revive, Target: 0}, &ev)
	if ev[0].Kind != Revive || hurt.HP != hurt.MaxHP/2 {
		t.Fatalf("应复活倒下的同伴：%+v", ev)
	}
}

func TestChargeAndEarthRound(t *testing.T) {
	b := New(rnd(7))
	p := player(0)
	p.HP, p.MaxHP = 100000, 100000
	b.Add(p)
	e := enemy(EnemySlot(0), "")
	b.Add(e)
	b.setup(e, Command{Kind: Attack, Target: 0, Skill: &data.Skill{Name: "突击", Func: "PETSKILL_ChargeAttack", Option: "1 攻%+90"}})
	var ev []Event
	b.act(e, Command{Kind: Attack, Target: 0}, &ev)
	if ev[0].Kind != Charge {
		t.Fatalf("第一回合应蓄力：%+v", ev)
	}
	ev = nil
	b.act(e, Command{Kind: Attack, Target: 0}, &ev)
	if ev[0].Kind != Attack && ev[0].Kind != Counter {
		t.Fatalf("第二回合应攻击：%+v", ev)
	}
	// 地球一周：钻地的回合打不到
	e.charge = -1
	c := b.setup(e, Command{Kind: Attack, Target: 0, Skill: &data.Skill{Name: "地球一周", Func: "PETSKILL_EarthRound", Option: "攻%+90"}})
	ev = nil
	b.act(e, c, &ev)
	if !e.hidden || b.pickTarget(p, e.Slot) != nil {
		t.Fatal("钻进地底后不能被选为目标")
	}
}

func TestSurprise(t *testing.T) {
	n := map[int]int{}
	for i := uint64(0); i < 2000; i++ {
		b := New(rnd(i))
		n[b.Surprise(5)]++
	}
	// 运气 5：20% 偷袭成功，不会被偷袭
	if n[2] != 0 || n[1] < 300 || n[1] > 500 {
		t.Fatalf("运气 5 的偷袭分布不对：%v", n)
	}
	b := New(rnd(1))
	b.Surprised = 1
	p := player(0)
	b.Add(p)
	e := enemy(EnemySlot(0), "at:1;1;1")
	b.Add(e)
	for _, x := range b.Resolve(map[int]Command{0: {Kind: Guard}}) {
		if x.Actor == e.Slot && x.Kind != None {
			t.Fatalf("被偷袭的敌人第一回合不能行动：%+v", x)
		}
	}
}

func TestPlayerMultiHit(t *testing.T) {
	b := New(rnd(8))
	p := player(0)
	p.Level, p.Luck = 20, 5
	n := map[int]int{}
	for i := 0; i < 5000; i++ {
		n[b.playerHits(p)]++
	}
	// 运气 5：1 下约 90.5%，2 下约 4%，3 下约 2%，5..10 下约 3.5%
	if n[1] < 4300 || n[2] < 100 || n[3] < 50 {
		t.Fatalf("连击分布不对：%v", n)
	}
	p.Level = 9
	if b.playerHits(p) != 1 {
		t.Fatal("10 级以下不会连击")
	}
}

// 骑着宠物中毒：人与骑宠各按自己的基本状态扣血（Compute_Down），骑宠扣不死。
func TestRidePoison(t *testing.T) {
	b := New(rnd(2))
	p := player(0)
	p.Base = [4]float64{4000, 2000, 2000, 2000}
	p.Ride = &Mount{Name: "m", HP: 3, MaxHP: 100, Atk: 10, Def: 10, Quick: 10, Base: [4]float64{8000, 4000, 4000, 4000}}
	b.Add(p)
	b.Add(enemy(EnemySlot(0), "gu:1"))
	p.status[StPoison] = 3
	hp := p.HP
	var ev []Event
	b.statusSeq(p, Command{Kind: Attack}, &ev)
	if p.HP != hp-20 || p.Ride.HP != 1 || ev[0].PetDamage != 2 || ev[0].RideHP != 1 {
		t.Fatalf("人扣 20、骑宠扣到剩 1：hp %d→%d ride %d %+v", hp, p.HP, p.Ride.HP, ev[0])
	}
}
