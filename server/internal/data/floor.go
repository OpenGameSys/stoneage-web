// Package data 读取原版服务端数据（h5/gamedata，复制自 serv/bin/gmsv/data 的 map/ 与 npc/），格式语义见 h5/CLAUDE.md 的"架构 → 数据来源与语义"。
package data

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// Floor 是一张地图（楼层）。坐标 (x, y) 的下标为 y*W + x。
type Floor struct {
	ID      int
	Name    string
	Palette int // 显示名第二段；-1 表示按时间自动切换（原客户端 palNo = -1）
	W, H    int
	Tiles   []uint16 // 地块的地图图号
	Objs    []uint16 // 物件的地图图号，0 为无
	Path    string
}

func (f *Floor) In(x, y int) bool { return x >= 0 && y >= 0 && x < f.W && y < f.H }

var mapMagic = []byte("LS2MAP")

// ParseFloor 解析一个 LS2MAP 文件（gmsv/map/readmap.c MAP_readMapOne）：
// 魔数[6] + 楼层号 BE u16 + 显示名[32]（GBK，"名字|调色板"）+ 宽、高 BE u16 + 地块[宽×高] BE u16 + 物件[宽×高] BE u16。
func ParseFloor(b []byte) (*Floor, error) {
	if len(b) < 6+2+32+4 || !bytes.Equal(b[:6], mapMagic) {
		return nil, fmt.Errorf("不是 LS2MAP 文件")
	}
	f := &Floor{ID: int(binary.BigEndian.Uint16(b[6:]))}
	show := b[8:40]
	if i := bytes.IndexByte(show, 0); i >= 0 {
		show = show[:i]
	}
	name, pal, _ := strings.Cut(GBK(show), "|")
	f.Name = name
	f.Palette = -1
	if p, err := strconv.Atoi(strings.TrimSpace(pal)); err == nil {
		f.Palette = p
	}
	f.W = int(binary.BigEndian.Uint16(b[40:]))
	f.H = int(binary.BigEndian.Uint16(b[42:]))
	n := f.W * f.H
	if len(b) < 44+n*4 {
		return nil, fmt.Errorf("楼层 %d：数据长度 %d 小于 %dx%d 所需", f.ID, len(b), f.W, f.H)
	}
	f.Tiles = make([]uint16, n)
	f.Objs = make([]uint16, n)
	for i := 0; i < n; i++ {
		f.Tiles[i] = binary.BigEndian.Uint16(b[44+i*2:])
		f.Objs[i] = binary.BigEndian.Uint16(b[44+n*2+i*2:])
	}
	return f, nil
}

// LoadFloors 递归读取 dir 下所有 LS2MAP 文件（MAP_readMapDir）。同一楼层号出现多次时，按路径排序后靠后的覆盖靠前的。
func LoadFloors(dir string) (map[int]*Floor, []string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(paths)
	floors := map[int]*Floor{}
	var warnings []string
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil || !bytes.HasPrefix(b, mapMagic) {
			continue
		}
		f, err := ParseFloor(b)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", p, err))
			continue
		}
		f.Path = p
		if old, ok := floors[f.ID]; ok {
			warnings = append(warnings, fmt.Sprintf("楼层 %d 重复：%s 覆盖 %s", f.ID, p, old.Path))
		}
		floors[f.ID] = f
	}
	return floors, warnings, nil
}

// GBK 把原版数据中的 GBK 字节转成 UTF-8；无法解码的字节保留为替换字符。
func GBK(b []byte) string {
	s, err := simplifiedchinese.GBK.NewDecoder().Bytes(b)
	if err != nil {
		return string(bytes.ToValidUTF8(b, []byte("?")))
	}
	return string(s)
}
