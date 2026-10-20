package data

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
)

// 咒术（精灵）的资料：battle/magic.txt（magic_base.c MAGIC_initMagic）与 battle/attmagic.bin（ATTMAGIC_initMagic，攻击魔法的范围）。
// 咒术挂在道具上（itemset6 第 56 栏的 magicid，第 58 栏是要用掉的 MP），装备着就能用。

// 咒术的目标（MAGIC_TARGET_*）
const (
	MagicTargetMyself = iota
	MagicTargetOther
	MagicTargetAllMySide
	MagicTargetAllOtherSide
	MagicTargetAll
	MagicTargetNone
	MagicTargetOtherWithoutMyself
	MagicTargetWithoutMyselfAndPet
	MagicTargetWholeOtherSide // 点哪一边就是那一边全体
	MagicTargetSingle         // 敌方一人
	MagicTargetOneRow
	MagicTargetAllRows
)

// Magic 是 magic.txt 的一行：名字、说明、函数名、参数、编号、使用场合（0 都可、1 战斗、2 地图）、目标、能不能对倒下的人用、
// 攻击魔法在 attmagic.bin 里的编号（没写为 -1）。
type Magic struct {
	ID            int
	Name, Comment string
	Func, Option  string
	Field, Target int
	Dead          bool
	Idx           int
}

// AttMagic 是 attmagic.bin 的一笔：攻击魔法的动画与范围（siField：以目标为中心 3 排 × 5 格）。
type AttMagic struct {
	Sprite, AttackType int // 攻击方式：0 单人、1 整排轮流、2 整排同时、3 全体轮流、4 全体同时
	SliceTime          int // 轮流攻击时的时间差（毫秒）
	ShowType           int // 1 显示在指定的画面位置（Sx, Sy），否则以目标为准再加上 (Sx, Sy)
	Sx, Sy             int
	Field              [3][5]bool
}

// LoadMagic 读 battle/magic.txt 与 battle/attmagic.bin（没有档时回传空的表）。
func LoadMagic(dataDir string) (map[int]*Magic, []AttMagic) {
	out := map[int]*Magic{}
	if lines, err := readLines(filepath.Join(dataDir, "battle", "magic.txt")); err == nil {
		for _, l := range lines {
			l = strings.TrimLeft(strings.ReplaceAll(l, "\t", " "), " ")
			c := cols(l)
			if len(c) < 8 {
				continue
			}
			m := &Magic{Name: c[0], Comment: c[1], Func: strings.TrimSpace(c[2]), Option: c[3],
				ID: num(c[4], -1), Field: num(c[5], 0), Target: num(c[6], 0), Dead: num(c[7], 0) == 1, Idx: -1}
			if len(c) > 8 {
				m.Idx = num(c[8], -1)
			}
			if m.ID >= 0 {
				out[m.ID] = m
			}
		}
	}
	var att []AttMagic
	if b, err := os.ReadFile(filepath.Join(dataDir, "battle", "attmagic.bin")); err == nil {
		const size = 33 * 4
		for off := 0; off+size <= len(b); off += size {
			v := func(i int) int { return int(int32(binary.LittleEndian.Uint32(b[off+i*4:]))) }
			a := AttMagic{Sprite: v(0), AttackType: v(1), SliceTime: v(2), ShowType: v(3), Sx: v(4), Sy: v(5)}
			for r := 0; r < 3; r++ {
				for k := 0; k < 5; k++ {
					a.Field[r][k] = v(18+r*5+k) != 0
				}
			}
			att = append(att, a)
		}
	}
	return out, att
}
