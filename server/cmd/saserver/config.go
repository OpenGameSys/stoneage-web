package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config 是 h5/config.yaml。同目录的 config.local.yaml（不进 git）里写的项目会盖掉 config.yaml 的，开发时用它打开 gm。
// 文件里的相对路径都以 config.yaml 所在的目录为准。
type Config struct {
	Addr    string      `yaml:"addr"`    // 监听地址
	Data    string      `yaml:"data"`    // 原版 gmsv 数据（含 map/ 与 npc/）
	DB      string      `yaml:"db"`      // SQLite 数据库文件
	Assets  string      `yaml:"assets"`  // 导出的网页资源（tools/asset-export 生成）
	Client  string      `yaml:"client"`  // 客户端页面（含 index.html 与 dist/）
	Origins []string    `yaml:"origins"` // 额外允许的页面来源（默认只允许同源）
	GM      bool        `yaml:"gm"`      // 允许所有玩家使用调试指令，只用于开发
	Groups  []LineGroup `yaml:"groups"`  // 大区与线路
}

type LineGroup struct {
	Name  string   `yaml:"name"`
	Lines []string `yaml:"lines"`
}

func defaultConfig() Config {
	return Config{Addr: "127.0.0.1:8800", Data: "gamedata", DB: "bin/saserver.db", Assets: "assets", Client: "client",
		Groups: []LineGroup{{Name: "石器时代", Lines: []string{"一线", "二线", "三线"}}}}
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
	for _, p := range []*string{&cfg.Data, &cfg.DB, &cfg.Assets, &cfg.Client} {
		if !filepath.IsAbs(*p) {
			*p = filepath.Join(base, *p)
		}
	}
	if len(cfg.Groups) == 0 {
		return cfg, path, errors.New("groups 至少要有一个大区")
	}
	for _, g := range cfg.Groups {
		if g.Name == "" || len(g.Lines) == 0 {
			return cfg, path, fmt.Errorf("大区 %q 要有名字和至少一条线路", g.Name)
		}
	}
	return cfg, path, nil
}
