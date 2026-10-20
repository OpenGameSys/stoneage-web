package battle

import (
	"testing"

	"sa/server/internal/data"
)

func pet(slot, loyalty int) *Fighter {
	d := Derive(1000, 1000, 1000, 1000)
	f := &Fighter{Slot: slot, Name: "pet", Level: 1, HP: d.HP, MaxHP: d.HP, Atk: d.Atk, Def: d.Def, Quick: d.Quick,
		Pet: true, Loyalty: loyalty, Base: [4]float64{1000, 1000, 1000, 1000}}
	f.Skills[0] = &data.Skill{ID: 1, Name: "攻击", Func: "PETSKILL_NormalAttack", Field: 1, Target: 6}
	return f
}

func foe(slot int) *Fighter {
	d := Derive(500, 100, 100, 100)
	return &Fighter{Slot: slot, Name: "e", Level: 1, HP: d.HP, MaxHP: d.HP, Atk: 1, Def: d.Def, Quick: d.Quick,
		Tactics: "gu:1", Base: [4]float64{500, 100, 100, 100}}
}

// 忠诚度 80 以上一定听话；不到 10 的宠物会打主人或逃走。
func TestPetLoyalCheck(t *testing.T) {
	b := New(rnd(1))
	b.Add(player(0))
	b.Add(pet(5, 100))
	b.Add(foe(15))
	for i := 0; i < 50; i++ {
		var ev []Event
		c, ok := b.loyalCheck(b.F[5], Command{Kind: Attack, Target: 15}, &ev)
		if !ok || len(ev) != 0 || c.Target != 15 {
			t.Fatalf("忠诚度 100 应听话：%+v %+v", c, ev)
		}
	}
	b.F[5].Loyalty = 0
	owner, leave := 0, 0
	for i := 0; i < 200; i++ {
		b.F[5].Escaped = false
		var ev []Event
		c, ok := b.loyalCheck(b.F[5], Command{Kind: Attack, Target: 15}, &ev)
		if len(ev) == 0 || ev[0].Kind != Disobey {
			t.Fatal("不听话时先送出 Disobey")
		}
		switch {
		case !ok:
			leave++
			if !b.F[5].Escaped || ev[len(ev)-1].Kind != Leave {
				t.Fatal("逃走的宠物离开战场")
			}
		case c.Kind == Attack && c.Target == 0:
			owner++
		default:
			t.Fatalf("忠诚度 0 只会打主人或逃走：%+v", c)
		}
	}
	// 60% 打主人、40% 逃走
	if owner < 90 || leave < 50 {
		t.Errorf("打主人 %d 次、逃走 %d 次", owner, leave)
	}
	// 没有技能的宠物不听话时什么也不做
	b.F[5].Escaped = false
	b.F[5].Skills = [7]*data.Skill{}
	b.F[5].Loyalty = 0
	var ev []Event
	if c, ok := b.loyalCheck(b.F[5], Command{Kind: Attack, Target: 15}, &ev); !ok || c.Kind != None {
		t.Errorf("没有技能的宠物不听话时不行动：%+v", c)
	}
}

// 主人逃走时宠物跟着离开；打自己的宠物忠诚度下降。
func TestPetEscapeWithOwner(t *testing.T) {
	for seed := uint64(1); seed < 50; seed++ {
		b := New(rnd(seed))
		p := player(0)
		p.Luck = 5 // 运气 5 的逃跑机率 95%
		b.Add(p)
		b.Add(pet(5, 100))
		b.Add(foe(15))
		b.Resolve(map[int]Command{0: {Kind: Escape}, 5: {Kind: Guard}})
		if p.Escaped {
			if b.F[5].Alive() {
				t.Fatal("主人逃走时宠物也要离开")
			}
			return
		}
	}
	t.Fatal("一直逃不掉")
}

func TestSekkan(t *testing.T) {
	b := New(rnd(3))
	b.Add(player(0))
	b.Add(pet(5, 100))
	b.Add(foe(15))
	b.attackOnce(b.F[0], b.F[5], attackOpts{}, false)
	if b.F[5].AIDelta != aiFixSekkan {
		t.Errorf("主人打自己的宠物，忠诚度变动 %d", b.F[5].AIDelta)
	}
}

// 捕获机率（BATTLE_CaptureCheck）：HP 越少越好抓，最多 99%。
func TestCaptureRate(t *testing.T) {
	a := player(0)
	a.Charm, a.Level, a.Luck = 60, 10, 1
	d := foe(15)
	d.Level, d.Seed = 5, &PetSeed{Capture: true, Get: 11}
	New(rnd(1)).Add(a)
	full := CaptureRate(a, d)
	d.HP = 1
	low := CaptureRate(a, d)
	if low <= full || low > 99 {
		t.Errorf("HP 越少越好抓：满血 %v、剩 1 %v", full, low)
	}
	// (10 − 1/MaxHP + 10/2 − 5/2 + 敏捷差/15 + 11 + 1) × 60 / 50
	want := (10 - 1.0/float64(d.MaxHP) + 5 - 2.5 + float64(a.FixQuick)/15 - float64(d.FixQuick)/15 + 12) * 60 / 50
	if low != want {
		t.Errorf("机率应为 %v，实际 %v", want, low)
	}
	d.Seed.Capture = false
	if CaptureRate(a, d) != 0 {
		t.Error("不能捕获的敌人机率为 0")
	}
}
