package battle

import (
	"testing"

	"sa/server/internal/data"
)

// petUse 让 5 号位的宠物这回合用技能 s 打 target（人在 0 号位防御，敌人只防御）。
func petUse(b *Battle, s *data.Skill, target int) []Event {
	return b.Resolve(map[int]Command{0: {Kind: Guard}, 5: {Kind: Attack, Target: target, Skill: s}})
}

func skillBattle(seed uint64, foes ...*Fighter) (*Battle, *Fighter) {
	b := New(rnd(seed))
	p := player(0)
	p.HP, p.MaxHP = 9999, 9999
	b.Add(p)
	pt := pet(5, 100)
	pt.Atk, pt.FixAtk = 200, 0
	pt.fixed = false
	b.Add(pt)
	for _, f := range foes {
		b.Add(f)
	}
	return b, pt
}

func bigFoe(slot int) *Fighter {
	f := foe(slot)
	f.HP, f.MaxHP = 5000, 5000
	return f
}

// 技能攻击（BATTLE_S_AttackDamage 那一类）不会引起反击；手下留情打不死对方；没有登记的技能什么也不做。
func TestSkillNoCounterMercyUnregistered(t *testing.T) {
	for seed := uint64(1); seed < 40; seed++ {
		e := bigFoe(EnemySlot(0))
		e.Tactics = "at:1"
		b, _ := skillBattle(seed, e)
		ev := petUse(b, &data.Skill{Name: "撕裂", Func: "PETSKILL_DamageToHp", Option: "30|50"}, e.Slot)
		for _, x := range ev {
			if x.Kind == Counter {
				t.Fatalf("嗜血技不应引起反击：%+v", ev)
			}
		}
	}
	e := foe(EnemySlot(0))
	e.HP = 1
	b, _ := skillBattle(3, e)
	for i := 0; i < 5; i++ {
		petUse(b, &data.Skill{Name: "手下留情", Func: "PETSKILL_ShowMercy"}, e.Slot)
	}
	if e.HP != 1 {
		t.Errorf("手下留情不应打死对方：%d", e.HP)
	}
	e2 := foe(EnemySlot(1))
	b, _ = skillBattle(3, e2)
	ev := petUse(b, &data.Skill{Name: "自爆", Func: "PETSKILL_SelfExplodeAttack"}, e2.Slot)
	for _, x := range ev {
		if x.Actor == 5 && x.Kind != None {
			t.Errorf("没有登记的技能应什么也不做：%+v", x)
		}
	}
}

// 回旋攻击打目标那一整排；撕裂伤口打满血的对手没有伤害。
func TestGyrateAndTear(t *testing.T) {
	a, c := foe(15), foe(16)
	a.HP, c.HP = 500, 500
	a.MaxHP, c.MaxHP = 500, 500
	b, pt := skillBattle(5, a, c, foe(10))
	ev := petUse(b, &data.Skill{Name: "回旋攻击", Func: "PETSKILL_Gyrate", Option: "攻%-50"}, 15)
	hit := map[int]bool{}
	for _, x := range ev {
		if x.Actor == pt.Slot && x.Kind == Attack {
			hit[x.Target] = true
		}
	}
	if !hit[15] || !hit[16] || hit[10] {
		t.Fatalf("应打 15..19 那一排每人一下：%v", hit)
	}

	full := bigFoe(EnemySlot(0))
	b, pt = skillBattle(7, full)
	pt.Player = false
	tear := &data.Skill{Name: "撕裂伤口", Func: "PETSKILL_BattleTearDamage", Option: "50"}
	for _, x := range petUse(b, tear, full.Slot) {
		if x.Actor == pt.Slot && x.Kind == Attack && x.Result != Dodge && x.Damage != 0 {
			t.Fatalf("对手满血时撕裂伤口不应有伤害：%+v", x)
		}
	}
	full.HP = 1000
	got := false
	for i := 0; i < 5 && !got; i++ {
		for _, x := range petUse(b, tear, full.Slot) {
			if x.Actor == pt.Slot && x.Kind == Attack && x.Result != Dodge && x.Damage >= 2000 {
				got = true
			}
		}
	}
	if !got {
		t.Error("对手缺 4000 HP 时应再加上 50%")
	}
}

// 落马术打中骑着的人时可能落马；闪避术让自己 3 回合多半闪开。
func TestFallGroundAndSetDuck(t *testing.T) {
	fell := false
	for seed := uint64(1); seed < 30 && !fell; seed++ {
		b := New(rnd(seed))
		p := player(0)
		p.HP, p.MaxHP = 9999, 9999
		p.Ride = &Mount{Name: "m", HP: 9999, MaxHP: 9999, Atk: 10, Def: 10, Quick: 10}
		b.Add(p)
		e := enemy(EnemySlot(0), "gu:1")
		e.Atk = 300
		b.Add(e)
		var ev []Event
		c := b.setup(e, Command{Kind: Attack, Target: 0, Skill: &data.Skill{Name: "落马术", Func: "PETSKILL_FallGround", Option: "攻%-30"}})
		b.skillAttack(e, c, &ev)
		fell = p.Ride.Fell
	}
	if !fell {
		t.Error("落马术应能打下骑宠")
	}

	b, pt := skillBattle(9, bigFoe(EnemySlot(0)))
	petUse(b, &data.Skill{Name: "闪避术", Func: "PETSKILL_SetDuck", Option: "3|60"}, pt.Slot)
	if pt.skillDuck != 3 || pt.duckP != 60 {
		t.Fatalf("闪避术应设 3 回合 60：%d %d", pt.skillDuck, pt.duckP)
	}
	att := enemy(EnemySlot(1), "gu:1")
	b.Add(att)
	dodged := 0
	for i := 0; i < 200; i++ {
		if b.duck(att, pt, 0) {
			dodged++
		}
	}
	if dodged < 100 {
		t.Errorf("闪避术中应多半闪开：%d/200", dodged)
	}
}

// 旅程伙伴：对宠物写了忠诚度时，忠诚度低于它就带走；自己一定离开。偷窃只偷玩家，得手后离开。
func TestAbductAndSteal(t *testing.T) {
	b := New(rnd(2))
	p := player(0)
	b.Add(p)
	pt := pet(5, 30)
	b.Add(pt)
	e := enemy(EnemySlot(0), "gu:1")
	b.Add(e)
	var ev []Event
	b.abduct(e, Command{Target: 5, Skill: &data.Skill{Name: "旅程伙伴2", Func: "PETSKILL_Abduct", Option: "60"}}, &ev)
	if !pt.Escaped || !e.Escaped {
		t.Fatalf("忠诚度 30 < 60 应被带走，自己也离开：%+v", ev)
	}
	b2 := New(rnd(2))
	b2.NoAbduct = true
	p2, pt2 := player(0), pet(5, 30)
	b2.Add(p2)
	b2.Add(pt2)
	e2 := enemy(EnemySlot(0), "gu:1")
	b2.Add(e2)
	ev = nil
	b2.abduct(e2, Command{Target: 5, Skill: &data.Skill{Name: "旅程伙伴2", Func: "PETSKILL_Abduct", Option: "60"}}, &ev)
	if pt2.Escaped || !e2.Escaped {
		t.Error("NPC 战斗带不走人，但自己照样离开")
	}

	ok := false
	for seed := uint64(1); seed < 20 && !ok; seed++ {
		b := New(rnd(seed))
		p := player(0)
		b.Add(p)
		th := enemy(EnemySlot(0), "gu:1")
		b.Add(th)
		var ev []Event
		b.steal(th, Command{Target: 0, Skill: &data.Skill{Name: "偷窃", Func: "PETSKILL_Steal"}}, &ev)
		if ev[0].OK {
			ok = true
			if ev[0].Result != "gold" && ev[0].Result != "item" || !th.Escaped {
				t.Fatalf("偷到东西应说明偷了什么并离开：%+v", ev)
			}
		}
	}
	if !ok {
		t.Error("偷窃应有机会得手")
	}
}

// 群蜂乱舞每个敌人至少一下；憾甲一击打宠物时连主人一起打。
func TestBattleModelAndRegret(t *testing.T) {
	foes := []*Fighter{bigFoe(10), bigFoe(11), bigFoe(15)}
	b, pt := skillBattle(4, foes...)
	ev := petUse(b, &data.Skill{Name: "群蜂乱舞", Func: "PETSKILL_BattleModel", Option: "5|4|麻|1|30|攻%-30|101867"}, 10)
	hit := map[int]int{}
	for _, x := range ev {
		if x.Actor == pt.Slot && x.Kind == Attack {
			hit[x.Target]++
		}
	}
	if hit[10] < 1 || hit[11] < 1 || hit[15] < 1 || hit[10]+hit[11]+hit[15] != 4 {
		t.Fatalf("4 个物件应分给 3 个敌人：%v", hit)
	}

	b = New(rnd(6))
	owner, mine := player(0), pet(5, 100)
	owner.HP, owner.MaxHP, mine.HP, mine.MaxHP = 9999, 9999, 9999, 9999
	b.Add(owner)
	b.Add(mine)
	e := enemy(EnemySlot(0), "gu:1")
	b.Add(e)
	ev = nil
	c := b.setup(e, Command{Kind: Attack, Target: 5, Skill: &data.Skill{Name: "憾甲一击", Func: "PETSKILL_Regret", Option: "命%20 攻%30 防%-50"}})
	b.skillAttack(e, c, &ev)
	targets := map[int]bool{}
	for _, x := range ev {
		targets[x.Target] = true
	}
	if !targets[5] || !targets[0] {
		t.Fatalf("憾甲一击应打宠物与主人：%+v", ev)
	}
}

// 暗月变身：6 次行动（含变身那次）后变回来；剧毒经由咒术附加，回合数是参数 + 2。
func TestVaryAndDeeppoison(t *testing.T) {
	b, pt := skillBattle(8, bigFoe(EnemySlot(0)))
	base := pt.Sprite
	petUse(b, &data.Skill{Name: "暗月变身", Func: "PETSKILL_Vary", Option: "攻%+30 敏%+30 魔防%-50"}, pt.Slot)
	if pt.Sprite != 101428 || pt.vary != 5 {
		t.Fatalf("应变成狼人：%d %d", pt.Sprite, pt.vary)
	}
	var back bool
	for i := 0; i < 5; i++ {
		for _, x := range b.Resolve(map[int]Command{0: {Kind: Guard}, 5: {Kind: Guard}}) {
			back = back || x.Kind == Vary && x.Actor == pt.Slot
		}
	}
	if !back || pt.Sprite != base {
		t.Errorf("6 次行动后应变回来：%d", pt.Sprite)
	}

	e := bigFoe(EnemySlot(0))
	e.Level = 1
	b, pt = skillBattle(1, e)
	pt.Level = 60
	m, ok := b.skillMagic(pt, &data.Skill{Name: "剧毒", Func: "PETSKILL_Deeppoison", Option: "剧 turn 5 成 50", Target: 6}, e.Slot)
	if !ok || m.Magic.Func != "MAGIC_StatusChange" || magicNum(m.Magic.Option, "turn", 0) != 7 {
		t.Fatalf("剧毒应转成 7 回合的咒术：%+v", m.Magic)
	}
}

// 地球一周：钻地的下一回合打的是下指令时选的目标（不是随机），而且带技能名。
func TestEarthRoundKeepsTarget(t *testing.T) {
	er := &data.Skill{Name: "地球一周", Func: "PETSKILL_EarthRound", Option: "攻%+90", Field: 1, Target: 6}
	for seed := uint64(1); seed <= 20; seed++ {
		b, _ := skillBattle(seed, bigFoe(15), bigFoe(16), bigFoe(17))
		petUse(b, er, 16)
		hit := false
		for _, e := range petUse(b, er, 15) { // 第二回合的指令不算数
			if e.Actor == 5 && e.Kind == Attack {
				hit = true
				if e.Target != 16 || e.Skill != "地球一周" {
					t.Fatalf("seed %d：钻出来应打 16 号并显示技能名：%+v", seed, e)
				}
			}
		}
		if !hit {
			t.Fatalf("seed %d：第二回合没有攻击", seed)
		}
	}
}
