package main

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// config.local.yaml 只写 rules 的一两项时，其余的项目要保留原来的值。
func TestRulesPartialOverride(t *testing.T) {
	cfg := defaultConfig()
	if err := yaml.Unmarshal([]byte("rules:\n  expRate: 2.5\n  shareExp: false\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	r, err := cfg.Rules.world()
	if err != nil {
		t.Fatal(err)
	}
	if r.ExpRate != 2.5 || r.ShareExp || r.TurnTime != 30*time.Second || r.GroundGold != 1800*time.Second || r.FamilyFee != 10000 {
		t.Errorf("盖掉两项后的规则不对：%+v", r)
	}
	cfg.Rules.TurnTime = 1
	if _, err := cfg.Rules.world(); err == nil {
		t.Error("turnTime 1 秒应该不行")
	}
}
