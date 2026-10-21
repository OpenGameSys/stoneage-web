// saserver：石器时代 H5 重制版的游戏服务端。
//
//	运行 bin/saserver.exe，设定在 h5/config.yaml（见 config.go），没有命令行参数。
//
// 同一个端口提供：/ws（游戏连接）、/（客户端页面）、/assets/（导出的美术与音频）。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"sa/server/internal/data"
	"sa/server/internal/server"
	"sa/server/internal/store"
	"sa/server/internal/world"
)

func main() {
	level := new(slog.LevelVar)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	cfg, cfgPath, err := loadConfig()
	if err != nil {
		log.Error("读取设定失败", "file", cfgPath, "err", err)
		os.Exit(1)
	}
	if err := level.UnmarshalText([]byte(cfg.Log)); err != nil {
		log.Error("log 要是 debug / info / warn / error", "log", cfg.Log)
		os.Exit(1)
	}
	rules, _ := cfg.Rules.world() // loadConfig 检查过了
	log.Info("设定", "file", cfgPath, "addr", cfg.Addr, "gms", cfg.GMs, "maxChars", cfg.MaxChars, "rules", fmt.Sprintf("%+v", cfg.Rules))
	start := time.Now()
	game, err := data.Load(cfg.Data)
	if err != nil {
		log.Error("读取原版数据失败", "err", err)
		os.Exit(1)
	}
	log.Info("原版数据已加载", "floors", len(game.Floors), "tileAttrs", len(game.Tiles), "warnings", len(game.Warnings), "took", time.Since(start))

	db, err := store.OpenSQLite(cfg.DB)
	if err != nil {
		log.Error("打开数据库失败", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// 每条线路一个独立的世界（原版每条线路是一台 gmsv），共用账号与角色存档
	var lines []*server.Line
	var wg sync.WaitGroup
	var families *world.Families // 家族是所有线路共用的（原版放在帐号服务器 AC）
	gms := map[string]bool{}
	for _, a := range cfg.GMs {
		gms[strings.ToLower(strings.TrimSpace(a))] = true
	}
	for _, group := range cfg.Groups {
		gname := group.Name
		for _, n := range group.Lines {
			id := len(lines) + 1
			w := world.New(game, db, log.With("line", gname+n), int64(id))
			w.GMs = gms
			w.Rules = rules

			if families == nil {
				families = w.Families()
			}
			w.ShareFamilies(families)
			lines = append(lines, &server.Line{ID: id, Group: gname, Name: n, World: w})
			wg.Add(1)
			go func() { w.Run(ctx); wg.Done() }()
		}
	}
	log.Info("线路已启动", "count", len(lines))
	worldDone := make(chan struct{})
	go func() { wg.Wait(); close(worldDone) }()

	mux := http.NewServeMux()
	mux.Handle("/ws", &server.Server{Lines: lines, Store: db, Log: log, Origins: cfg.Origins, MaxChars: cfg.MaxChars})

	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(cfg.Assets))))
	// 客户端页面与脚本要求每次校验（no-cache），改完代码刷新即生效；资源文件不变，交给浏览器缓存
	clientFiles := http.FileServer(http.Dir(cfg.Client))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		clientFiles.ServeHTTP(w, r)
	}))

	srv := &http.Server{Addr: cfg.Addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	log.Info("服务端已启动", "url", "http://"+cfg.Addr+"/")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("监听失败", "err", err)
		os.Exit(1)
	}
	<-worldDone
	log.Info("已退出，在线角色位置已保存")
}
