package world

import (
	"context"
	"log/slog"

	"sa/server/internal/data"
	"sa/server/internal/store"
)

// MigrateExp 把旧存档的经验换成原版的语义（kv expSemantics = 2 记录做过了）。
// v0.2.0 以前经验是从 1 级起的累计值、升级门槛又错了一行（n 级看 exp.txt 第 n+2 行）；
// 原版（CHAR_HandleExp）每级从 0 累计、升级时扣掉本级所需。换算：新经验 = 旧经验 − 旧语义下这一级的起点（LevelExp[level]），
// 不足 0 算 0，不会超过本级所需 − 1。人物与宠物栏里的每只宠物（含寄放的）都换。
func MigrateExp(ctx context.Context, db store.Store, game *data.Game, log *slog.Logger) error {
	const key, want = "expSemantics", "2"
	if v, err := db.GetKV(ctx, key); err == nil && v == want {
		return nil
	}
	conv := func(level, exp int) int {
		start := 0
		if level >= 0 && level < len(game.Battle.LevelExp) {
			start = game.Battle.LevelExp[level]
		}
		exp = max(0, exp-start)
		if need := game.Battle.NextLevelExp(level); need > 0 {
			exp = min(exp, need-1)
		}
		return exp
	}
	chars, err := db.AllCharacters(ctx)
	if err != nil {
		return err
	}
	n := 0
	for _, c := range chars {
		exp := conv(c.Level, c.Exp)
		pets := c.Pets
		if bag := parsePets(c.Pets); bag != nil {
			for _, p := range append(bag.Pets[:], bag.Pool...) {
				if p != nil {
					p.Exp = conv(p.Level, p.Exp)
				}
			}
			pets = bag.String()
		}
		if exp == c.Exp && pets == c.Pets {
			continue
		}
		if err := db.SaveExpPets(ctx, c.ID, exp, pets); err != nil {
			return err
		}
		n++
	}
	log.Info("经验存档已换成原版语义", "characters", len(chars), "changed", n)
	return db.PutKV(ctx, key, want)
}
