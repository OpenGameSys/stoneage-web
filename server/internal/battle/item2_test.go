package battle

import (
	"testing"

	"sa/server/internal/data"
)

// 战场属性道具：原版找 "turn"，数据写"回合"，所以一律 3 回合；鞭炮吓跑名单里的宠物模板，对自己人不用掉。
func TestFieldChangeAndFirecracker(t *testing.T) {
	b, p, e := itemBattle()
	var ev []Event
	b.useItem(p, Command{Kind: UseItem, ItemFunc: "ITEM_useFieldChange", ItemArg: "火 50 回合 6", ItemName: "葡萄", Target: 0}, &ev)
	if b.fieldAttr != 3 || b.fieldPow != 50 || b.fieldTurns != 3 || !ev[0].Consume {
		t.Fatalf("应是火 50、3 回合：%d %d %d", b.fieldAttr, b.fieldPow, b.fieldTurns)
	}
	e.Seed = &PetSeed{TempNo: 901}
	ev = nil
	b.useItem(p, Command{Kind: UseItem, ItemFunc: "ITEM_firecracker", ItemArg: "KPET:900_901", ItemName: "鞭炮", Target: e.Slot}, &ev)
	if !e.Escaped {
		t.Fatalf("名单里的敌人应被吓跑：%+v", ev)
	}
	b2, p2, _ := itemBattle()
	pet2 := pet(5, 100)
	b2.Add(pet2)
	ev = nil
	b2.useItem(p2, Command{Kind: UseItem, ItemFunc: "ITEM_firecracker", ItemArg: "KPET:901", ItemName: "鞭炮", Target: 5}, &ev)
	if ev[0].Consume {
		t.Error("对自己人用鞭炮不应用掉")
	}
}

// 媚惑术：打中可以捕获的敌人可能变成小狐狸，3 回合后变回来；变身中只能攻击 / 防御 / 待机。
func TestBecomeFox(t *testing.T) {
	fox := false
	for seed := uint64(1); seed < 60 && !fox; seed++ {
		e := bigFoe(EnemySlot(0))
		e.Seed = &PetSeed{Capture: true}
		e.Sprite = 100500
		b, _ := skillBattle(seed, e)
		petUse(b, &data.Skill{Name: "媚惑术", Func: "PETSKILL_BecomeFox"}, e.Slot)
		if e.fox > 0 {
			fox = true
			if e.Sprite != foxSprite {
				t.Fatalf("应变成小狐狸：%d", e.Sprite)
			}
			c := b.transformLimit(e, Command{Kind: Attack, Skill: &data.Skill{Func: "PETSKILL_Mighty"}})
			if c.Kind != None {
				t.Error("小狐狸不能用技能")
			}
			for i := 0; i < 4; i++ {
				b.Resolve(map[int]Command{0: {Kind: Guard}, 5: {Kind: Guard}})
			}
			if e.fox != 0 || e.Sprite != 100500 {
				t.Errorf("3 回合后应变回来：%d %d", e.fox, e.Sprite)
			}
		}
	}
	if !fox {
		t.Error("媚惑术应有机会成功")
	}
}

// 乌力化：只对对方的玩家；乌力不能用咒术（改为防御）。
func TestBecomePig(t *testing.T) {
	pigged := false
	for seed := uint64(1); seed < 60 && !pigged; seed++ {
		b := New(rnd(seed))
		p := player(0)
		p.HP, p.MaxHP = 9999, 9999
		b.Add(p)
		e := enemy(EnemySlot(0), "gu:1")
		b.Add(e)
		var ev []Event
		c := b.setup(e, Command{Kind: Attack, Target: 0, Skill: &data.Skill{Name: "乌力化", Func: "PETSKILL_BecomePig"}})
		b.skillAttack(e, c, &ev)
		if p.Pig > 0 {
			pigged = true
			if p.Sprite != pigSprite || p.Pig != 60 {
				t.Fatalf("应变成乌力 60 秒：%d %d", p.Sprite, p.Pig)
			}
			if c := b.transformLimit(p, Command{Kind: Magic}); c.Kind != Guard {
				t.Error("乌力不能用咒术")
			}
		}
	}
	if !pigged {
		t.Error("乌力化应有机会成功")
	}
}

// 火线猎杀：打一下再对整排放火魔法。
func TestFirekill(t *testing.T) {
	a, c := bigFoe(15), bigFoe(16)
	b, pt := skillBattle(2, a, c)
	ev := petUse(b, &data.Skill{Name: "火线猎杀", Func: "PETSKILL_Firekill"}, 15)
	fire := map[int]bool{}
	for _, x := range ev {
		if x.Actor == pt.Slot && x.Kind == Magic {
			fire[x.Target] = true
		}
	}
	if !fire[15] || !fire[16] {
		t.Fatalf("火魔法应打整排：%v", fire)
	}
}
