package data

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// TileAttr 是 mapset.txt 中一个地图图号的属性（只取 M1 用到的列）。
type TileAttr struct {
	Walkable   int  // 0 不可走；1 可走；2 物件专用：无论地块如何都可走
	HaveHeight bool // 有高度（飞行时不可越过）
}

// TileSet 按地图图号索引。不在表中的图号：原版 MAP_getImageInt 返回 FALSE，即不可走、无高度。
type TileSet map[int]TileAttr

// LoadMapSet 读取 mapset.txt（MAP_readMapConfFile）：每行空白分隔，第 1 列图号，
// 第 4 列起依次是 MAP_confentries：walkable、haveheight、defence、…
func LoadMapSet(path string) (TileSet, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	set := TileSet{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		col := strings.Fields(line)
		if len(col) < 5 {
			continue
		}
		num, err := strconv.Atoi(col[0])
		if err != nil {
			continue
		}
		walk, _ := strconv.Atoi(col[3])
		height, _ := strconv.Atoi(col[4])
		set[num] = TileAttr{Walkable: walk, HaveHeight: height != 0}
	}
	return set, sc.Err()
}

// Walkable 是 map_deal.c MAP_walkAbleFromPoint（非飞行）的语义：
// 物件 walkable=0 不可走；=1 时看地块是否为 1；=2 一定可走；其他值不可走。
func (s TileSet) Walkable(f *Floor, x, y int) bool {
	if !f.In(x, y) {
		return false
	}
	i := y*f.W + x
	switch s[int(f.Objs[i])].Walkable {
	case 1:
		return s[int(f.Tiles[i])].Walkable == 1
	case 2:
		return true
	default:
		return false
	}
}
