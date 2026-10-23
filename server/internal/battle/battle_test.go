package battle

import (
	"math/rand/v2"
	"os"
	"testing"

	"sa/server/internal/data"
)

func rnd(seed uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, 7)) }

func player(slot int) *Fighter {
	// 建角 8/6/4/2 点：HP 44、攻 7、防 5、速 2
	d := Derive(800, 600, 400, 200)
	return &Fighter{Slot: slot, Name: "p", Level: 1, HP: d.HP, MaxHP: d.HP, MP: 100, MaxMP: 100,
		Atk: d.Atk, Def: d.Def, Quick: d.Quick, Attr: [4]int{50, 50, 0, 0}, Luck: 1, Player: true,
		Base: [4]float64{800, 600, 400, 200}}
}

func TestDerive(t *testing.T) {
	d := Derive(800, 600, 400, 200)
	if d.HP != 44 || d.Atk != 7 || d.Def != 5 || d.Quick != 2 {
		t.Fatalf("换算不对：%+v", d)
	}
}

func TestAttrAdjust(t *testing.T) {
	none := [4]int{}
	// 无属性打无属性：AJ_SAME，伤害不变
	if got := attrAdjust(none, none, 100); got != 100 {
		t.Fatalf("无打无应为 100，得 %d", got)
	}
	// 纯火打纯风：1.5 倍；纯火打纯水：0.6 倍
	fire, wind, water := [4]int{0, 0, 100, 0}, [4]int{0, 0, 0, 100}, [4]int{0, 100, 0, 0}
	if got := attrAdjust(fire, wind, 100); got != 150 {
		t.Fatalf("火打风应为 150，得 %d", got)
	}
	if got := attrAdjust(fire, water, 100); got != 60 {
		t.Fatalf("火打水应为 60，得 %d", got)
	}
}

func TestGuardReducesDamage(t *testing.T) {
	b := New(rnd(1))
	a := &Fighter{Slot: 10, Level: 5, HP: 100, MaxHP: 100, Atk: 60, Def: 10, Quick: 10}
	d := player(0)
	d.HP, d.MaxHP = 1000, 1000
	d.guard = true
	sum := 0
	for i := 0; i < 200; i++ {
		e, _ := b.attackOnce(a, d, attackOpts{}, false)
		if e.Result == Dodge {
			t.Fatal("防御中不应闪避")
		}
		sum += e.Damage
	}
	b2 := New(rnd(1))
	d.guard, d.HP = false, 100000
	sum2 := 0
	for i := 0; i < 200; i++ {
		e, _ := b2.attackOnce(a, d, attackOpts{}, false)
		sum2 += e.Damage
	}
	if sum*2 > sum2 {
		t.Fatalf("防御后伤害应明显减少：防御 %d，不防御 %d", sum, sum2)
	}
}

func TestTurnAndOutcome(t *testing.T) {
	b := New(rnd(3))
	p := player(0)
	p.Atk = 200 // 一击必杀
	b.Add(p)
	e := &Fighter{Slot: EnemySlot(0), Name: "e", Level: 1, HP: 5, MaxHP: 5, Atk: 1, Def: 1, Quick: 1, Tactics: "at:1;1;1"}
	b.Add(e)
	var ev []Event
	for i := 0; i < 10; i++ {
		ev = b.Resolve(map[int]Command{0: {Kind: Attack, Target: e.Slot}})
		if over, _ := b.Over(); over {
			break
		}
	}
	over, win := b.Over()
	if !over || win != 0 {
		t.Fatalf("应打赢：%v %d %+v", over, win, ev)
	}
}

func TestEscapeAndTimeout(t *testing.T) {
	b := New(rnd(5))
	p := player(0)
	p.Luck = 5 // 运气 5：95% 逃跑
	b.Add(p)
	b.Add(&Fighter{Slot: EnemySlot(0), Level: 1, HP: 50, MaxHP: 50, Atk: 1, Def: 1, Quick: 1, Tactics: "gu:1"})
	ok := false
	for i := 0; i < 5 && !ok; i++ {
		for _, e := range b.Resolve(map[int]Command{0: {Kind: Escape}}) {
			if e.Actor == 0 && e.Kind == Escape && e.OK {
				ok = true
			}
		}
	}
	if !ok || !p.Escaped {
		t.Fatal("运气 5 应能逃掉")
	}
	if over, win := b.Over(); !over || win != 1 {
		t.Fatal("玩家逃掉后战斗结束（己方没赢）")
	}
	// 没给指令的玩家当作 None
	b2 := New(rnd(6))
	b2.Add(player(0))
	b2.Add(&Fighter{Slot: EnemySlot(0), Level: 1, HP: 50, MaxHP: 50, Tactics: "gu:1"})
	if ev := b2.Resolve(nil); len(ev) == 0 || ev[0].Kind != None && ev[1].Kind != None {
		t.Fatalf("超时应为 None：%+v", ev)
	}
}

func TestEnemyAITargetsPlayer(t *testing.T) {
	b := New(rnd(9))
	b.Add(player(0))
	e := &Fighter{Slot: EnemySlot(0), Tactics: "at:1;2;3|gu:0|es:0", HP: 1}
	b.Add(e)
	for i := 0; i < 20; i++ {
		if c := b.enemyAI(e); c.Kind != Attack || c.Target != 0 {
			t.Fatalf("应攻击玩家：%+v", c)
		}
	}
}

func TestKillExp(t *testing.T) {
	if KillExp(30, 10, 8) != 30 || KillExp(30, 20, 5) != 30*5/15 || KillExp(30, 40, 5) != 1 {
		t.Fatal("经验递减不符合原版")
	}
}

func TestPickEnemiesFromRealData(t *testing.T) {
	const dir = "../../../gamedata"
	if _, err := os.Stat(dir); err != nil {
		t.Skip("没有原版数据")
	}
	bd, err := data.LoadBattle(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(bd.Areas) < 1000 || len(bd.Groups) < 600 || len(bd.Enemies) < 2000 || len(bd.Temps) < 1000 {
		t.Fatalf("数据数量不对：区域 %d 队伍 %d 敌人 %d 模板 %d", len(bd.Areas), len(bd.Groups), len(bd.Enemies), len(bd.Temps))
	}
	if bd.NextLevelExp(1) != 2 || bd.NextLevelExp(2) != 6 || bd.NextLevelExp(0) != 0 {
		t.Fatalf("exp.txt：1 级升 2 级应需 2（第 2 行）、2 升 3 要 6，得 %d", bd.NextLevelExp(1))
	}
	r := rnd(11)
	tried := 0
	for i := range bd.Areas {
		a := &bd.Areas[i]
		if a.ZOrder <= 0 {
			continue
		}
		specs := PickEnemies(bd, a, r)
		if len(specs) == 0 {
			continue
		}
		tried++
		if len(specs) > a.EnemyMax {
			t.Fatalf("区域 %d 出了 %d 只，超过上限 %d", a.Index, len(specs), a.EnemyMax)
		}
		for j, s := range specs {
			if j >= 5 && s.Temp.Size == 1 {
				t.Fatalf("区域 %d：大型敌人排在第 %d 只（应只站前排）", a.Index, j+1)
			}
			f := NewEnemy(s, EnemySlot(j), bd.Skills, r)
			if f.HP <= 0 || f.Sprite < 100000 || f.Level < s.Enemy.LvMin || f.Level > s.Enemy.LvMax || f.Exp < 0 {
				t.Fatalf("敌人数据不对：%+v", f)
			}
		}
	}
	if tried < 500 {
		t.Fatalf("能出敌人的区域太少：%d", tried)
	}
	// 萨姆吉尔村外（floor 100）应该有遇敌区域，敌人是乌力一类的低级怪
	if a := bd.AreaAt(100, 650, 450); a == nil {
		t.Fatal("floor 100 (650,450) 应在遇敌区域内")
	}
}

// 击飞（BATTLE_DamageSub 的 IsUltimate）：一下 ≥ 最大 HP × 1.2 + 20 直接击飞（2）、累计到了也击飞（1）、雷尔不会。
func TestUltimate(t *testing.T) {
	b := New(rnd(2))
	a := &Fighter{Slot: 10, Level: 50, HP: 1000, MaxHP: 1000, Atk: 100000, Quick: 10}
	d := player(0)
	d.HP, d.MaxHP = 500, 500
	e, _ := b.attackOnce(a, d, attackOpts{}, false)
	if e.Fly != 2 || !e.Dead || !d.Flown || d.HP != 0 {
		t.Fatalf("一下超过 1.2 倍最大 HP 应直接击飞：%+v flown=%v", e, d.Flown)
	}
	a.Atk = 150
	d = player(0)
	d.HP, d.MaxHP, d.Def = 100000, 1000, 0
	fly := 0
	for i := 0; i < 60 && fly == 0; i++ {
		e, _ := b.attackOnce(a, d, attackOpts{}, false)
		fly = e.Fly
	}
	if fly != 1 || !d.Flown || d.HP != 0 {
		t.Fatalf("累计伤害到了应击飞（1）：fly=%d flown=%v hp=%d ultimate=%d", fly, d.Flown, d.HP, d.ultimate)
	}
	ler := player(0)
	ler.Sprite, ler.HP, ler.MaxHP = 101813, 500, 500
	a.Atk = 100000
	if e, _ := b.attackOnce(a, ler, attackOpts{}, false); e.Fly != 0 || ler.Flown {
		t.Fatalf("雷尔不该被击飞：%+v", e)
	}
}

// rules.enemyGuardRate：敌人选防御的权重乘上倍率；NoEnemyGuard 完全不防御。
func TestEnemyGuardScale(t *testing.T) {
	count := func(scale float64, none bool) int {
		b := New(rnd(9))
		b.GuardScale, b.NoEnemyGuard = scale, none
		f := &Fighter{Slot: EnemySlot(0), Tactics: "at:10;1;1,gu:10"}
		b.Add(player(0))
		n := 0
		for i := 0; i < 2000; i++ {
			if b.enemyAI(f).Kind == Guard {
				n++
			}
		}
		return n
	}
	full, half, none := count(1, false), count(0.5, false), count(0, true)
	if none != 0 || half*10 > full*8 || half*10 < full*4 {
		t.Fatalf("防御次数：原版 %d、0.5 倍 %d、不防御 %d", full, half, none)
	}
}
