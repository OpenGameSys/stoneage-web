package main

import (
	"strings"
	"testing"
)

// rules 的项目写到最上层（walkSpeed: 3）照样套用并警告；拼错的项目只警告。
func TestCheckKeys(t *testing.T) {
	cfg := defaultConfig()
	if err := checkKeys([]byte("addr: x\nwalkSpeed: 3\nfoo: 1\nrules:\n  bar: 2\n"), "config.local.yaml", &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Rules.WalkSpeed != 3 {
		t.Fatalf("walkSpeed 应套进 rules：%v", cfg.Rules.WalkSpeed)
	}
	all := strings.Join(cfg.Warnings, "\n")
	for _, s := range []string{"walkSpeed 是游戏规则", "不认得的设定 foo", "rules 底下不认得的设定 bar"} {
		if !strings.Contains(all, s) {
			t.Fatalf("少了警告 %q：%s", s, all)
		}
	}
}
