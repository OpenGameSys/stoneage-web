package data

import (
	"os"
	"testing"
)

// 用仓库里的原版数据做集成测试；数据不存在时跳过。
const realData = "../../../gamedata"

func loadReal(t *testing.T) *Game {
	t.Helper()
	if _, err := os.Stat(realData); err != nil {
		t.Skip("没有原版数据")
	}
	g, err := Load(realData)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestLoadRealData(t *testing.T) {
	g := loadReal(t)
	warps, npcs := 0, 0
	for _, m := range g.Warps {
		warps += len(m)
	}
	for _, l := range g.NPCs {
		npcs += len(l)
	}
	t.Logf("楼层 %d，图号属性 %d，传送点 %d，可显示 NPC %d，警告 %d", len(g.Floors), len(g.Tiles), warps, npcs, len(g.Warnings))
	if len(g.Floors) < 100 || len(g.Tiles) < 1000 || warps < 1000 {
		t.Fatalf("数据量异常")
	}
	for i, h := range Hometowns {
		f := g.Floors[h.Floor]
		if f == nil {
			t.Fatalf("出生村庄 %d 的楼层 %d 不存在", i, h.Floor)
		}
		if !g.Tiles.Walkable(f, h.X, h.Y) {
			t.Errorf("出生点 %d (%d,%d,%d) 不可走", i, h.Floor, h.X, h.Y)
		}
		t.Logf("出生村庄 %d：%d %s %dx%d", i, f.ID, f.Name, f.W, f.H)
	}
	// 传送点的目标位置应该可以站人（抽查，统计不可站的比例）
	bad, total := 0, 0
	for _, m := range g.Warps {
		for _, w := range m {
			total++
			if !g.Tiles.Walkable(g.Floors[w.ToFloor], w.ToX, w.ToY) {
				bad++
			}
		}
	}
	t.Logf("传送目标不可走：%d / %d", bad, total)
}

func TestParseFloorHeader(t *testing.T) {
	b := []byte("LS2MAP\x42\x6a")
	name := []byte("\xb5\xd8\xcf\xc2|3")
	b = append(b, append(name, make([]byte, 32-len(name))...)...)
	b = append(b, 0, 2, 0, 1)         // 2x1
	b = append(b, 0x13, 0x88, 0, 100) // 地块 5000, 100
	b = append(b, 0, 0, 0x2e, 0xe0)   // 物件 0, 12000
	f, err := ParseFloor(b)
	if err != nil {
		t.Fatal(err)
	}
	if f.ID != 17002 || f.Name != "地下" || f.Palette != 3 || f.W != 2 || f.H != 1 || f.Tiles[0] != 5000 || f.Objs[1] != 12000 {
		t.Fatalf("解析错误：%+v", f)
	}
}

func TestWalkableRule(t *testing.T) {
	f := &Floor{W: 3, H: 1, Tiles: []uint16{1, 1, 2}, Objs: []uint16{0, 9, 8}}
	set := TileSet{0: {Walkable: 1}, 1: {Walkable: 1}, 2: {Walkable: 0}, 8: {Walkable: 2}, 9: {Walkable: 0}}
	if !set.Walkable(f, 0, 0) || set.Walkable(f, 1, 0) || !set.Walkable(f, 2, 0) || set.Walkable(f, 3, 0) {
		t.Fatal("通行规则与 MAP_walkAbleFromPoint 不一致")
	}
}

func TestLoadItems(t *testing.T) {
	if _, err := os.Stat(realData); err != nil {
		t.Skip("没有原版数据")
	}
	items, err := LoadItems(realData)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 10000 {
		t.Fatalf("道具太少：%d", len(items))
	}
	axe := items[0]
	if axe == nil || axe.Name != "小斧头" || axe.Type != ItemAxe || axe.Mods[ModAtk] != [2]int{9, 9} || axe.Mods[ModDef] != [2]int{-3, -3} || EquipSlot(axe.Type) != SlotArm {
		t.Fatalf("小斧头不对：%+v", axe)
	}
	meat := items[1234]
	if meat == nil || meat.Name != "小块肉" || meat.UseFunc != "ITEM_useRecovery" || meat.Argument != "体20" || meat.Target != 1 {
		t.Fatalf("小块肉不对：%+v", meat)
	}
	// 贝诺美斯（猛毒的精灵）的毒抗性 +10
	if items[1].Resist[0] != [2]int{10, 10} {
		t.Fatalf("抗性栏位不对：%+v", items[1].Resist)
	}
}

func TestNpcArgAndWindows(t *testing.T) {
	if v, ok := ArgField("NEWWARPMAN|FREE:LV>0|FreeMsg:a:b|WARP:1,2,3", "FREE"); !ok || v != "LV>0" {
		t.Fatalf("FREE 不应命中 FreeMsg：%q", v)
	}
	if v, _ := ArgField("FreeMsg:a:b", "FreeMsg"); v != "a" {
		t.Fatalf("取第一个 : 与下一个 : 之间：%q", v)
	}
	if v, _ := ArgField("NEWWARPMAN|WARP:1,2,3", "WARP"); v != "1,2,3" {
		t.Fatalf("没有 : 的段要略过：%q", v)
	}
	g := loadReal(t)
	counts := map[string]int{}
	for _, ns := range g.NPCs {
		for _, n := range ns {
			counts[n.Function]++
			if n.Function == "Windowman" && n.Windows[1] == nil {
				t.Errorf("万事通 %s 没有 1 号窗口", n.Source)
			}
		}
	}
	for _, fn := range []string{"WarpMan", "Windowman", "SavePoint", "TimeMan"} {
		if counts[fn] == 0 {
			t.Errorf("没有读到 %s", fn)
		}
	}
	if h := g.Elders[4]; h != (Hometown{1000, 92, 99}) {
		t.Fatalf("记录点 4 应在 1000(92,99)：%+v", h)
	}
	if h := g.Elders[0]; h != Hometowns[0] {
		t.Fatalf("长老 0 是第一个村庄：%+v", h)
	}
	t.Logf("NPC 功能：%v", counts)
}
