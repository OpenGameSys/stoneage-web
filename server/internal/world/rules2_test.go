package world

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"sa/server/internal/battle"
	"sa/server/internal/data"
	"sa/server/internal/proto"
	"sa/server/internal/store"
)

func chatHas(msgs []proto.Message, sub string) bool {
	for _, m := range msgs {
		if c, ok := m.(*proto.ChatMsg); ok && strings.Contains(c.Text, sub) {
			return true
		}
	}
	return false
}

// 密语（/tell）：只有对方听得到；对方关了密语频道就回"关闭了此频道"。
func TestTellChannel(t *testing.T) {
	w, db, _ := setup(t)
	c1, cl1 := petOwner(t, w, db, "teller", 10, 60)
	c2, cl2 := petOwner(t, w, db, "hearer", 10, 60)
	id1, id2 := int(c1.ID), int(c2.ID)
	w.SetFlags(id2, fsTell)
	w.Chat(id1, "near", "/tell hearer 你好吗", 0, 0)
	m1, m2 := cl1.take(), cl2.take()
	if !chatHas(m1, "你告诉hearer：你好吗") || !chatHas(m2, "teller告诉你：你好吗") {
		t.Fatalf("密语没送到：%v / %v", m1, m2)
	}
	w.SetFlags(id2, 0)
	w.Chat(id1, "near", "/tell hearer 还在吗", 0, 0)
	m1, m2 = cl1.take(), cl2.take()
	if !chatHas(m1, "hearer 关闭了此频道") || chatHas(m2, "还在吗") {
		t.Fatalf("关了密语频道应回绝：%v / %v", m1, m2)
	}
	w.Chat(id1, "near", "/tell nobody 喂", 0, 0)
	if !chatHas(cl1.take(), "找不到") {
		t.Fatal("找不到人应提示")
	}
}

// 两个饰品位不能装同一种类的饰品（CANNOTEQUIP2）。
func TestSameDecorationType(t *testing.T) {
	w, db, g := setup(t)
	c, _ := petOwner(t, w, db, "ringer", 50, 60)
	id := int(c.ID)
	var rings []int
	for _, d := range g.Items {
		if d.Type == data.ItemRing && d.Level <= 1 && d.NeedProfession == 0 && d.NeedTrans == 0 {
			rings = append(rings, d.ID)
			if len(rings) == 2 {
				break
			}
		}
	}
	if len(rings) < 2 {
		t.Skip("没有两个戒指")
	}
	giveItem(w, id, rings[0])
	giveItem(w, id, rings[1])
	slotOf := func(itemID int) int {
		s := -1
		w.Do(func() {
			for i, it := range w.players[id].inv {
				if it != nil && it.ID == itemID && i >= data.EquipSlots {
					s = i
				}
			}
		})
		return s
	}
	w.ItemMove(id, slotOf(rings[0]), data.SlotDeco1)
	w.ItemMove(id, slotOf(rings[1]), data.SlotDeco2)
	w.Do(func() {
		inv := w.players[id].inv
		if inv[data.SlotDeco1] == nil || inv[data.SlotDeco1].ID != rings[0] {
			t.Fatalf("第一个戒指应装上：%+v", inv[data.SlotDeco1])
		}
		if inv[data.SlotDeco2] != nil {
			t.Fatalf("同种类的第二个戒指不该装上：%+v", inv[data.SlotDeco2])
		}
	})
}

// 击飞：一下伤害 ≥ 最大 HP × 1.2 + 20 的玩家离开战斗、传回记录点、扣魅力。
func TestUltimateFliesHome(t *testing.T) {
	w, db, g := setup(t)
	ene := petEnemy(t, g, 1)
	c, cl := petOwner(t, w, db, "flyer", 30, 60)
	id := int(c.ID)
	w.Do(func() {
		e := w.players[id]
		w.placeOnFloor(e, 1000, 50, 50, 0) // 离记录点远一点
		f := enemyFighter(w, ene, 1, battle.EnemySlot(0))
		f.Atk, f.MaxHP, f.HP = 100000, 100000, 100000
		w.startBattleWith(e, []*battle.Fighter{f}, nil, nil)
	})
	cl.take()
	// 敌人的行动方针有机率待机，多下几回合防御直到它出手
	var msgs []proto.Message
	flown := false
	for round := 0; round < 20 && !flown; round++ {
		w.BattleCommand(id, "guard", -1, 0)
		time.Sleep(30 * time.Millisecond)
		msgs = cl.take()
		turn, ok := find[*proto.BattleTurn](msgs)
		if !ok {
			t.Fatal("应有回合结果")
		}
		for _, ev := range turn.Events {
			if ev.Target == 0 && ev.Fly > 0 && ev.Dead {
				flown = true
			}
		}
	}
	if !flown {
		t.Fatal("20 回合都没被击飞")
	}
	if _, ok := find[*proto.BattleEnd](msgs); !ok {
		t.Fatal("被击飞应离开战斗")
	}
	w.Do(func() {
		e := w.players[id]
		h := w.elderPos(e)
		if e.fight != nil || e.floor != h.Floor || e.x != h.X || e.y != h.Y {
			t.Fatalf("应传回记录点 %+v：fight=%v 楼层 %d (%d,%d)", h, e.fight != nil, e.floor, e.x, e.y)
		}
		if e.char.Charm != 60+battle.CHFixPlayerUltimate {
			t.Fatalf("魅力应扣 4：%d", e.char.Charm)
		}
	})
}

// 战斗 60 分钟后还没下指令的玩家被请出战斗（BATTLE_TimeOutCheck）。
func TestBattleTimeLimit(t *testing.T) {
	w, db, g := setup(t)
	ene := petEnemy(t, g, 1)
	c, cl := petOwner(t, w, db, "idler", 30, 60)
	id := int(c.ID)
	w.Do(func() {
		e := w.players[id]
		w.startBattleWith(e, []*battle.Fighter{enemyFighter(w, ene, 1, battle.EnemySlot(0))}, nil, nil)
		e.fight.room.started = w.now().Add(-BattleTimeLimit - time.Minute)
		w.battleTimeout(e.fight.room)
	})
	msgs := cl.take()
	if _, ok := find[*proto.BattleEnd](msgs); !ok || !chatHas(msgs, "时间到，结束战斗。") {
		t.Fatalf("时间到应结束战斗并提示：%v", msgs)
	}
	w.Do(func() {
		if w.players[id].fight != nil {
			t.Fatal("应已离开战斗")
		}
	})
}

// 旧存档的经验换成原版语义：人物与宠物都减掉旧语义下这一级的起点，做过一次就不再做。
func TestMigrateExp(t *testing.T) {
	_, db, g := setup(t)
	ctx := context.Background()
	acc, err := db.CreateAccount(ctx, "mig", "pw", "")
	if err != nil {
		t.Fatal(err)
	}
	bag := &petBag{Battle: -1, Ride: -1}
	bag.Pets[0] = &Pet{Name: "a", Level: 2, Exp: 7}
	bag.Pool = []*Pet{{Name: "b", Level: 1, Exp: 1}}
	c, err := db.CreateCharacter(ctx, store.Character{AccountID: acc.ID, Name: "mig", Sprite: 100000, Level: 3, Exp: 25, Pets: bag.String()})
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for i := 0; i < 2; i++ {
		if err := MigrateExp(ctx, db, g, log); err != nil {
			t.Fatal(err)
		}
		got, _ := db.GetCharacter(ctx, acc.ID, c.ID)
		b := parsePets(got.Pets)
		// 旧语义：3 级的起点是 exp.txt 第 4 行 18 → 25 − 18 = 7；2 级起点第 3 行 6 → 7 − 6 = 1；1 级起点 2 → 1 − 2 < 0 → 0
		if got.Exp != 7 || b.Pets[0].Exp != 1 || b.Pool[0].Exp != 0 {
			t.Fatalf("第 %d 次换算后：exp %d 宠物 %d 寄放 %d", i+1, got.Exp, b.Pets[0].Exp, b.Pool[0].Exp)
		}
	}
	if v, _ := db.GetKV(ctx, "expSemantics"); v != "2" {
		t.Fatalf("应记录做过了：%q", v)
	}
}
