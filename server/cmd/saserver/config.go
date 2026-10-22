package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"sa/server/internal/world"
)

// Config 是 h5/config.yaml。同目录的 config.local.yaml（不进 git）里写的项目会盖掉 config.yaml 的，开发时用它写 GM 账号。

// 文件里的相对路径都以 config.yaml 所在的目录为准。
type Config struct {
	Addr     string      `yaml:"addr"`    // 监听地址
	Data     string      `yaml:"data"`    // 原版 gmsv 数据（含 map/ 与 npc/）
	DB       string      `yaml:"db"`      // SQLite 数据库文件
	Assets   string      `yaml:"assets"`  // 导出的网页资源（tools/asset-export 生成）
	Client   string      `yaml:"client"`  // 客户端页面（含 index.html 与 dist/）
	Origins  []string    `yaml:"origins"` // 额外允许的页面来源（默认只允许同源）
	GMs      []string    `yaml:"gms"`     // GM 的账号名（可以多个）：有 GM 指令选单、能用 GM 指令
	Groups   []LineGroup `yaml:"groups"`  // 大区与线路
	TLSCert  string      `yaml:"tlsCert"` // 给了就用 HTTPS / WSS（PEM 证书与私钥；对外时账号密码才不是明文）
	TLSKey   string      `yaml:"tlsKey"`
	Log      string      `yaml:"log"`      // 日志等级：debug / info / warn / error
	MaxChars int         `yaml:"maxChars"` // 每个账号的角色位（原版 2）
	Rules    RulesConfig `yaml:"rules"`    // 游戏规则（world.Rules）
}

// RulesConfig 是 config.yaml 的 rules，对应 world.Rules（时间都以秒为单位）。没写的项目用原版的值。
type RulesConfig struct {
	ExpRate       float64 `yaml:"expRate"`        // 战斗经验倍率
	ShareExp      bool    `yaml:"shareExp"`       // 人物、出战宠物、骑宠都拿一样多的经验（false：原版的谁打倒谁拿）
	DropRate      float64 `yaml:"dropRate"`       // 战利品机率倍率
	EncounterRate float64 `yaml:"encounterRate"`  // 遇敌机率倍率（0 不会遇敌）
	TurnTime      int     `yaml:"turnTime"`       // 每回合选指令的秒数
	LevelUpPoints int     `yaml:"levelUpPoints"`  // 每升一级的加点
	GroundItem    int     `yaml:"groundItem"`     // 地上的道具几秒后消失
	GroundPet     int     `yaml:"groundPet"`      // 地上的宠物几秒后消失
	GroundGold    int     `yaml:"groundGold"`     // 地上的石币几秒后消失
	FamilyFee     int     `yaml:"familyFee"`      // 成立家族的手续费
	NoEscape      []int   `yaml:"noEscapeFloors"` // 逃不掉的楼层（按逃跑改成防御）
	WorldTrans    int     `yaml:"worldTrans"`     // 世界频道要几转
	WorldLevel    int     `yaml:"worldLevel"`     // 世界频道要几级
	WorldFame     int     `yaml:"worldFame"`      // 世界频道每句扣的声望（内部值，100 = 1 点）
	BusStep       int     `yaml:"busStep"`        // 长毛象巴士 / 加美航空每走一步的毫秒数（越小越快，100..2000）
	VendorTax     int     `yaml:"vendorTax"`      // 摆摊卖出时抽的税（百分比，0..100；原版 0）
}

func rulesConfig(r world.Rules) RulesConfig {
	return RulesConfig{ExpRate: r.ExpRate, ShareExp: r.ShareExp, DropRate: r.DropRate, EncounterRate: r.EncounterRate,
		TurnTime: int(r.TurnTime / time.Second), LevelUpPoints: r.LevelUpPoints, GroundItem: int(r.GroundItem / time.Second),
		GroundPet: int(r.GroundPet / time.Second), GroundGold: int(r.GroundGold / time.Second), FamilyFee: r.FamilyFee,
		NoEscape: r.NoEscapeFloors, WorldTrans: r.WorldTrans, WorldLevel: r.WorldLevel, WorldFame: r.WorldFame, BusStep: int(r.BusStep / time.Millisecond), VendorTax: r.VendorTax}
}

// world 检查数值的范围，换成 world.Rules。
func (c RulesConfig) world() (world.Rules, error) {
	sec := func(n int) time.Duration { return time.Duration(n) * time.Second }
	switch {
	case c.ExpRate < 0 || c.DropRate < 0 || c.EncounterRate < 0:
		return world.Rules{}, errors.New("rules：倍率不能是负数")
	case c.TurnTime < 5 || c.TurnTime > 300:
		return world.Rules{}, errors.New("rules.turnTime 要在 5..300 秒")
	case c.BusStep < 100 || c.BusStep > 2000:
		return world.Rules{}, errors.New("rules.busStep 要在 100..2000 毫秒")
	case c.VendorTax < 0 || c.VendorTax > 100:
		return world.Rules{}, errors.New("rules.vendorTax 要在 0..100")
	case c.LevelUpPoints < 0 || c.FamilyFee < 0:
		return world.Rules{}, errors.New("rules.levelUpPoints、familyFee 不能是负数")
	case c.GroundItem < 1 || c.GroundPet < 1 || c.GroundGold < 1:
		return world.Rules{}, errors.New("rules.groundItem / groundPet / groundGold 至少 1 秒")
	case c.WorldTrans < 0 || c.WorldLevel < 0 || c.WorldFame < 0:
		return world.Rules{}, errors.New("rules.worldTrans / worldLevel / worldFame 不能是负数")
	}
	return world.Rules{ExpRate: c.ExpRate, ShareExp: c.ShareExp, DropRate: c.DropRate, EncounterRate: c.EncounterRate,
		TurnTime: sec(c.TurnTime), LevelUpPoints: c.LevelUpPoints, GroundItem: sec(c.GroundItem), GroundPet: sec(c.GroundPet),
		GroundGold: sec(c.GroundGold), FamilyFee: c.FamilyFee,
		NoEscapeFloors: c.NoEscape, WorldTrans: c.WorldTrans, WorldLevel: c.WorldLevel, WorldFame: c.WorldFame, BusStep: time.Duration(c.BusStep) * time.Millisecond, VendorTax: c.VendorTax}, nil

}

type LineGroup struct {
	Name  string   `yaml:"name"`
	Lines []string `yaml:"lines"`
}

func defaultConfig() Config {
	return Config{Addr: "127.0.0.1:8800", Data: "gamedata", DB: "bin/saserver.db", Assets: "assets", Client: "client",
		Groups: []LineGroup{{Name: "石器时代", Lines: []string{"一线", "二线", "三线"}}},
		Log:    "info", MaxChars: 2, Rules: rulesConfig(world.DefaultRules())}
}

// findConfig 先找当前目录的 config.yaml，再找执行档上一层（bin/ 的上一层就是 h5/）。
func findConfig() (string, error) {
	cands := []string{"config.yaml"}
	if exe, err := os.Executable(); err == nil {
		cands = append(cands, filepath.Join(filepath.Dir(exe), "..", "config.yaml"))
	}
	for _, c := range cands {
		if _, err := os.Stat(c); err == nil {
			return filepath.Abs(c)
		}
	}
	return "", fmt.Errorf("找不到 config.yaml（找过 %v）", cands)
}

func loadConfig() (Config, string, error) {
	cfg := defaultConfig()
	path, err := findConfig()
	if err != nil {
		return cfg, "", err
	}
	for _, p := range []string{path, filepath.Join(filepath.Dir(path), "config.local.yaml")} {
		b, err := os.ReadFile(p)
		if errors.Is(err, os.ErrNotExist) && p != path {
			continue
		}
		if err != nil {
			return cfg, path, err
		}
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return cfg, path, fmt.Errorf("%s：%w", p, err)
		}
	}
	base := filepath.Dir(path)
	for _, p := range []*string{&cfg.Data, &cfg.DB, &cfg.Assets, &cfg.Client, &cfg.TLSCert, &cfg.TLSKey} {
		if *p != "" && !filepath.IsAbs(*p) {
			*p = filepath.Join(base, *p)
		}
	}
	if (cfg.TLSCert == "") != (cfg.TLSKey == "") {
		return cfg, path, errors.New("tlsCert 与 tlsKey 要一起给")
	}

	if len(cfg.Groups) == 0 {
		return cfg, path, errors.New("groups 至少要有一个大区")
	}
	for _, g := range cfg.Groups {
		if g.Name == "" || len(g.Lines) == 0 {
			return cfg, path, fmt.Errorf("大区 %q 要有名字和至少一条线路", g.Name)
		}
	}
	if cfg.MaxChars < 1 || cfg.MaxChars > 8 {
		return cfg, path, errors.New("maxChars 要在 1..8")
	}
	if _, err := cfg.Rules.world(); err != nil {
		return cfg, path, err
	}
	return cfg, path, nil
}
