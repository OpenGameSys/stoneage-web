package data

import (
	"errors"
	"slices"
)

// 新角色规则，来源 gmsv/char/char.c CHAR_createNewChar、CHAR_makeCharFromOptionAtCreate，
// char_data.c CHAR_checkFaceImageNumber，以及 char.c CHAR_complianceParameter 的数值公式。

const (
	FaceBase      = 30000 // CG_CHR_MAKE_FACE
	MaxStatPoints = 20    // MAXPARAMETER（开了 _NEW_PLAYER_CF：四项之和不超过 20）
	MaxAttrPoints = 10    // MAXATTRIBUTE：四属性之和必须正好 10
	InitialCharm  = 60    // 新角色 CHAR_CHARM
)

// NewCharOption 是创建角色时玩家选择的参数（客户端 editCharParam 的结果）。
type NewCharOption struct {
	Sprite, Face             int
	Vital, Str, Tgh, Dex     int
	Earth, Water, Fire, Wind int
}

// FaceNumber 计算头像图号：30000 + 形象序号×100 + 颜色×25 + 眼睛×5 + 嘴巴（login.cpp editCharParam）。
func FaceNumber(body, color, eye, mouth int) int {
	return FaceBase + body*100 + color*25 + eye*5 + mouth
}

// Validate 按原服务端的规则校验。
func (o NewCharOption) Validate() error {
	if !slices.Contains(PlayerSprites, o.Sprite) {
		return errors.New("形象不可选")
	}
	// CHAR_checkFaceImageNumber：头像的形象序号与颜色必须和身体一致；眼睛、嘴巴各 0..4
	image := o.Sprite - PlayerSprites[0]
	off := o.Face - FaceBase
	if off < 0 || off/100*20+off%100/25*5 != image || off%25/5 > 4 || off%5 > 4 {
		return errors.New("头像与形象不匹配")
	}
	sum := 0
	for _, p := range []int{o.Vital, o.Str, o.Tgh, o.Dex} {
		if p < 0 || p > MaxStatPoints {
			return errors.New("基本状态超出范围")
		}
		sum += p
	}
	if sum > MaxStatPoints {
		return errors.New("基本状态点数超过 20")
	}
	attrs := []int{o.Earth, o.Water, o.Fire, o.Wind}
	asum, cnt := 0, 0
	for _, a := range attrs {
		if a < 0 || a > MaxAttrPoints {
			return errors.New("属性超出范围")
		}
		asum += a
		if a > 0 {
			cnt++
		}
	}
	switch {
	case asum != MaxAttrPoints:
		return errors.New("属性点数必须正好分配 10 点")
	case cnt > 2:
		return errors.New("最多只能选两种属性")
	case o.Earth > 0 && o.Fire > 0, o.Water > 0 && o.Wind > 0:
		return errors.New("相克的属性不能同时选择")
	}
	return nil
}

// Derived 是由基本状态推出的显示数值（CHAR_complianceParameter，存档值为点数×100，这里直接用点数计算）。
type Derived struct{ HP, Atk, Def, Agi int }

func DeriveStats(vital, str, tgh, dex int) Derived {
	v, s, t, d := float64(vital), float64(str), float64(tgh), float64(dex)
	return Derived{
		HP:  int(v*4 + s + t + d),
		Atk: int(s + t*0.1 + v*0.1 + d*0.05),
		Def: int(t + s*0.1 + v*0.1 + d*0.05),
		Agi: int(d),
	}
}
