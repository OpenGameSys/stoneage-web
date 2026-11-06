package world

import (
	"strings"
	"testing"
	"time"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 溜宠：对玛蕾菲雅用祖母给的项链（19624，参数 80）→ 她离开宠物栏、跟着主人走；放再久也不消失；主人传送她一起传；
// 再用一次说"请先收回放出的宠物！"；右键点她收回；超过道具等级的不行。
func TestPetFollow(t *testing.T) {
	w, db, g := setup(t)
	if g.Items[19624] == nil {
		t.Skip("没有项链 19624")
	}
	h := data.Hometowns[0]
	c := newChar(t, db, "walker", h.Floor, h.X, h.Y)
	cl := &fakeClient{}
	w.Enter(c, cl)
	id := int(c.ID)
	pet := enemyPet(t, w, 1479, 10, c.ID)
	slot := -1
	w.Do(func() { slot = w.addPet(w.players[id], pet) })
	giveItem(w, id, 19624)
	inv := -1
	w.Do(func() {
		p := w.players[id]
		for i := data.EquipSlots; i < data.ItemSlots; i++ {
			if p.inv[i] != nil && p.inv[i].ID == 19624 {
				inv = i
			}
		}
	})
	cl.take()

	w.ItemUse(id, inv, slot+1)
	var o *entity
	w.Do(func() {
		p := w.players[id]
		o = w.followPetOf(p)
		if o == nil || o.ground.pet != pet || p.pets.Pets[slot] != nil || p.inv[inv] == nil {
			t.Fatalf("应放出跟着走、离开宠物栏、项链不用掉：%v %v %v", o, p.pets.Pets[slot], p.inv[inv])
		}
	})

	// 主人走 6 步，宠物一直在身边
	const fl = 1000 // 萨姆吉尔村的空地
	w.Do(func() { w.placeOnFloor(w.players[id], fl, 60, 70, 1) })
	walked := 0
	for i := 0; i < 6; i++ {
		var px, py int
		w.Do(func() {
			p := w.players[id]
			p.stepTokens, p.lastStep = StepBurst, time.Time{}
			px, py = p.x, p.y
		})
		f := g.Floors[fl]
		dir := -1
		var ox, oy int
		w.Do(func() { ox, oy = o.x, o.y })
		best := -1
		for k := 0; k < 8; k++ { // 往离宠物最远的方向走
			if nx, ny := px+MOVE[k][0], py+MOVE[k][1]; g.Tiles.Walkable(f, nx, ny) {
				if _, warp := g.WarpAt(fl, nx, ny); !warp {
					if dist := max(abs(nx-ox), abs(ny-oy)); dist > best {
						best, dir = dist, k
					}
				}
			}
		}
		if dir < 0 {
			break
		}
		w.Move(id, px, py, dir)
		walked++
		w.Do(func() {
			p := w.players[id]
			if max(abs(o.x-p.x), abs(o.y-p.y)) > 2 {
				t.Errorf("第 %d 步宠物应跟在旁边：主人 (%d,%d) 宠物 (%d,%d)", i+1, p.x, p.y, o.x, o.y)
			}
		})
	}
	if walked == 0 {
		t.Fatal("走不动")
	}
	w.Do(func() {
		if o.ground.steps == 0 {
			t.Error("宠物应跟着走了几步")
		}
	})

	// 放再久也不消失
	w.Do(func() {
		now := w.now()
		w.now = func() time.Time { return now.Add(time.Hour) }
		w.sweepGround()
		if w.followPetOf(w.players[id]) != o {
			t.Error("溜着的宠物不应因为放太久消失")
		}
		w.now = func() time.Time { return now }
	})

	// 传送：一起到同一格
	w.Do(func() {
		p := w.players[id]
		w.placeOnFloor(p, 1000, 50, 50, 1)
		if o.floor != 1000 || o.x != 50 || o.y != 50 || w.floors[1000][o.id] != o {
			t.Errorf("主人传送时宠物应传到同一格：%d (%d,%d)", o.floor, o.x, o.y)
		}
	})

	// 跟着走的玛蕾菲雅照样能说话（10 级在 1000 号：上限 15）
	w.Do(func() {
		p := w.players[id]
		o.x, o.y = p.x+MOVE[p.dir][0], p.y+MOVE[p.dir][1]
	})
	cl.take()
	w.Talk(id, o.id)
	w.Do(func() {})
	if win, ok := find[*proto.NpcWindow](cl.take()); !ok || pet.LimitLevel != 15 {
		t.Errorf("跟着走的宠物也能说话：%+v %d", win, pet.LimitLevel)
	}

	// 再用一次（对另一只宠物）：先收回
	second := enemyPet(t, w, 1479, 10, c.ID)
	s2 := -1
	w.Do(func() { s2 = w.addPet(w.players[id], second) })
	cl.take()
	w.ItemUse(id, inv, s2+1)
	w.Do(func() {})
	if m, ok := find[*proto.ChatMsg](cl.take()); !ok || !strings.Contains(m.Text, "请先收回") {
		t.Errorf("已经溜着一只应说请先收回：%+v", m)
	}

	// 右键点她收回
	w.PickUp(id, -1) // 脚下没有
	dir := 0
	w.Do(func() { dir = w.players[id].dir })
	w.PickUp(id, dir)
	w.Do(func() {})
	w.Do(func() {
		p := w.players[id]
		if w.followPetOf(p) != nil || petSlotOf(p.pets, pet) < 0 {
			t.Errorf("右键应收回到宠物栏")
		}
	})
	if m, ok := find[*proto.ChatMsg](cl.take()); !ok || !strings.Contains(m.Text, "拾回") {
		t.Errorf("收回应说拾回：%+v", m)
	}

	// 等级超过道具的参数
	pet.Level = 81
	w.Do(func() { slot = petSlotOf(w.players[id].pets, pet) })
	w.ItemUse(id, inv, slot+1)
	w.Do(func() {})
	if m, ok := find[*proto.ChatMsg](cl.take()); !ok || !strings.Contains(m.Text, "等级不足") {
		t.Errorf("81 级用项链（80）应说道具的等级不足：%+v", m)
	}
}

// 下线时收回溜着的宠物并存档。
func TestPetFollowLogout(t *testing.T) {
	w, db, g := setup(t)
	if g.Items[19011] == nil {
		t.Skip("没有宠物项圈")
	}
	h := data.Hometowns[0]
	c := newChar(t, db, "walker2", h.Floor, h.X, h.Y)
	cl := &fakeClient{}
	w.Enter(c, cl)
	id := int(c.ID)
	pet := enemyPet(t, w, 1479, 10, c.ID)
	w.Do(func() {
		p := w.players[id]
		slot := w.addPet(p, pet)
		w.dropPetFollow(p, slot)
	})
	w.Leave(id, cl)
	w.Do(func() {
		for _, es := range w.floors {
			for _, e := range es {
				if e.ground != nil && e.ground.pet == pet {
					t.Error("下线后宠物不应还在地上")
				}
			}
		}
	})
}

func petSlotOf(b *petBag, p *Pet) int {
	for i, q := range b.Pets {
		if q == p {
			return i
		}
	}
	return -1
}
