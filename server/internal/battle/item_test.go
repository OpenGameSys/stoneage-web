package battle

import (
	"testing"

	"sa/server/internal/data"
)

// 参数照原版的位移读：turn 后面要隔一个字，"turn3" 读成 0；成 后面同样。
func TestItemArgNum(t *testing.T) {
	cases := []struct {
		arg, key string
		want     int
	}{{"毒 turn 10 成 300", "turn", 10}, {"醉 turn3 成 60", "turn", 0}, {"毒 turn 10 成 300", "成", 300}, {"眠", "成", 15}}
	for _, c := range cases {
		if got := itemArgNum(c.arg, c.key, 1, 15*boolInt(c.key == "成")); got != c.want {
			t.Errorf("%q 的 %s = %d，应为 %d", c.arg, c.key, got, c.want)
		}
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func itemBattle() (*Battle, *Fighter, *Fighter) {
	b := New(rnd(3))
	p := player(0)
	b.Add(p)
	e := foe(EnemySlot(0))
	e.HP, e.MaxHP = 3000, 3000
	b.Add(e)
	return b, p, e
}

// 捕获之果加在捕获机率上、捕捉一次后归零；咒 / 恩 / 祝在每次行动前生效；光镜守的道具不能对敌人用。
func TestBattleItems(t *testing.T) {
	b, p, e := itemBattle()
	var ev []Event
	b.useItem(p, Command{Kind: UseItem, ItemFunc: "ITEM_useCaptureUp", ItemArg: "50", ItemName: "捕获之果", Target: 0}, &ev)
	if p.CaptureUp < 45 || p.CaptureUp > 55 || !ev[0].Consume {
		t.Fatalf("捕获机率应加约 50 并用掉：%d %+v", p.CaptureUp, ev)
	}
	b.capture(p, e.Slot, &ev)
	if p.CaptureUp != 0 {
		t.Error("捕捉一次后应归零")
	}

	ev = nil
	p.HP = 10
	b.useItem(p, Command{Kind: UseItem, ItemFunc: "ITEM_useImprecate", ItemArg: "恩:10x2", ItemName: "恩惠", Target: 0}, &ev)
	if !ev[0].ConsumeAll || p.imprecate[1] != [2]int{10, 2} {
		t.Fatalf("恩应设定 10 × 2 回合、整叠用掉：%v %+v", p.imprecate, ev)
	}
	ev = nil
	b.statusSeq(p, Command{Kind: Guard}, &ev)
	if p.HP != 20 || ev[0].Kind != Regen || ev[0].Damage != 10 {
		t.Errorf("行动前应回 10 HP：%d %+v", p.HP, ev)
	}

	ev = nil
	m := &data.Magic{Name: "光的精灵", Func: "MAGIC_MagicDef", Option: "吸 turn 1"}
	b.useItem(p, Command{Kind: UseItem, ItemFunc: "ITEM_useMRecovery", ItemArg: "单:3", ItemName: "光", ItemMagic: m, Target: e.Slot}, &ev)
	if e.Absorb != 0 || ev[0].Consume {
		t.Error("光镜守的道具不能对敌人用、不用掉")
	}
	ev = nil
	b.useItem(p, Command{Kind: UseItem, ItemFunc: "ITEM_useMRecovery", ItemArg: "单:3", ItemName: "光", ItemMagic: m, Target: 0}, &ev)
	if p.Absorb != 3 || !ev[0].Consume {
		t.Errorf("应吸收 3 次：%d", p.Absorb)
	}

	// 回复道具：只用 体 或 气 的一种；气 的数值是 1 时补满
	ev = nil
	p.MP = 0
	b.useItem(p, Command{Kind: UseItem, ItemFunc: "ITEM_useRecovery", ItemArg: "气1", ItemName: "气", Target: 0}, &ev)
	if p.MP != p.MaxMP {
		t.Errorf("气 1 应补满 MP：%d", p.MP)
	}
	ev = nil
	b.useItem(p, Command{Kind: UseItem, ItemFunc: "ITEM_useRecovery", ItemArg: "魅10", ItemName: "魅", Target: 0}, &ev)
	if ev[0].Consume {
		t.Error("魅力道具在战斗中没有作用、不用掉")
	}
}
