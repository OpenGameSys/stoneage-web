package data

import (
	"fmt"
	"path/filepath"
)

// Hometown 是新角色的出生点：四个村庄长老的位置（gmsv/char/char_data.c elders[]）。
type Hometown struct{ Floor, X, Y int }

var Hometowns = []Hometown{{1006, 15, 22}, {2006, 20, 16}, {3006, 21, 16}, {4006, 14, 20}}

// PlayerSprites 是可选的玩家形象（char_data.c CHAR_playerImageNumber，不含 _MO_IMAGE_EXTENSION 扩展）。
var PlayerSprites = func() []int {
	var s []int
	for k := 0; k < 12; k++ {
		for j := 0; j < 4; j++ {
			s = append(s, 100000+20*k+5*j)
		}
	}
	return s
}()

// Game 汇总 M1 需要的原版数据。
type Game struct {
	Floors   map[int]*Floor
	Tiles    TileSet
	Warps    map[int]map[[2]int]Warp // 楼层 → (x,y) → 传送点
	NPCs     map[int][]NPC           // 楼层 → 可显示的 NPC
	Battle   *BattleData             // 遇敌、敌人、经验表、战斗背景
	Items    map[int]*ItemDef        // itemset6.txt
	Atoms    map[string]bool         // 素材的名字（itematom.txt，ITEM_initItemAtom；不在表里的成分合成时略过）
	Elders   map[int]Hometown        // 长老编号 → 记录点位置（elders[] 与记录点 NPC 的 Born）
	Quiz     []Question              // 猜谜题库（question.txt）
	Quests   []JobDaily              // 任务日志的行（jobdaily.txt，CHAR_JobDaily）
	Ride     *Ride                   // 骑宠形象对照（ride/familyridefunction.lua）
	Warnings []string
}

// Load 读取 gmsv 的 data 目录。
func Load(dataDir string) (*Game, error) {
	g := &Game{Warps: map[int]map[[2]int]Warp{}, NPCs: map[int][]NPC{}}
	var err error
	if g.Floors, g.Warnings, err = LoadFloors(filepath.Join(dataDir, "map")); err != nil {
		return nil, fmt.Errorf("读取地图：%w", err)
	}
	if g.Tiles, err = LoadMapSet(filepath.Join(dataDir, "map", "mapset.txt")); err != nil {
		return nil, fmt.Errorf("读取 mapset.txt：%w", err)
	}
	warps, err := LoadMapWarps(filepath.Join(dataDir, "map", "mapwarp.txt"))
	if err != nil {
		return nil, fmt.Errorf("读取 mapwarp.txt：%w", err)
	}
	if g.Battle, err = LoadBattle(dataDir); err != nil {
		return nil, fmt.Errorf("读取战斗数据：%w", err)
	}
	if g.Items, err = LoadItems(dataDir); err != nil {
		return nil, fmt.Errorf("读取道具：%w", err)
	}
	if g.Atoms, err = LoadAtoms(dataDir); err != nil {
		return nil, fmt.Errorf("读取素材：%w", err)
	}
	npcs, err := LoadNPCs(filepath.Join(dataDir, "npc"))
	if err != nil {
		return nil, fmt.Errorf("读取 NPC：%w", err)
	}
	g.Elders = loadElders(npcs)
	g.Quiz = loadQuizFile(dataDir)
	g.Quests = loadJobDailyFile(dataDir)
	g.Ride = LoadRide(dataDir)
	for _, w := range append(warps, NPCWarps(npcs)...) {
		if g.Floors[w.Floor] == nil || g.Floors[w.ToFloor] == nil {
			continue // 原版里有指向不存在楼层的传送点
		}
		if g.Warps[w.Floor] == nil {
			g.Warps[w.Floor] = map[[2]int]Warp{}
		}
		g.Warps[w.Floor][[2]int{w.X, w.Y}] = w
	}
	for _, n := range npcs {
		// M1 只做静态显示：有形象、不是传送点、所在楼层存在
		if n.Sprite > 0 && n.Type != "npcgen_warp" && g.Floors[n.Floor] != nil {
			g.NPCs[n.Floor] = append(g.NPCs[n.Floor], n)
		}
	}
	return g, nil
}

// WarpAt 返回 (x,y) 上的传送点。
func (g *Game) WarpAt(floor, x, y int) (Warp, bool) {
	w, ok := g.Warps[floor][[2]int{x, y}]
	return w, ok
}
