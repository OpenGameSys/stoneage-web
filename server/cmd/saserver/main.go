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
	"net"
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
	for _, w := range cfg.Warnings {
		log.Warn("设定", "msg", w)
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
	if err := world.MigrateExp(context.Background(), db, game, log); err != nil {
		log.Error("换算经验存档失败", "err", err)
		os.Exit(1)
	}

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
			w.Name = gname + n

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
	// GM 账号：还没注册的名字提醒一下（注册时会保留给管理员，别人注册不走）；对外监听时再提醒一次
	for name := range gms {
		if ok, err := db.AccountExists(ctx, name); err == nil && !ok {
			log.Warn("GM 账号还没注册（别人注册不了这个名字，请尽快自己注册）", "account", name)
		}
	}
	if host, _, _ := net.SplitHostPort(cfg.Addr); host != "127.0.0.1" && host != "localhost" && host != "::1" {
		if cfg.TLSCert == "" {
			log.Warn("对外监听而且没有 TLS：账号密码会明文传输", "addr", cfg.Addr)
		}
		if len(gms) > 0 {
			log.Warn("对外监听时请确认 GM 账号用的是强密码", "gms", cfg.GMs)
		}
	}
	mux.Handle("/ws", &server.Server{Lines: lines, Store: db, Log: log, Origins: cfg.Origins, MaxChars: cfg.MaxChars, GMs: gms})

	mux.Handle("/assets/", gzipped(http.StripPrefix("/assets/", http.FileServer(noListing{http.Dir(cfg.Assets)}))))
	// 客户端页面与脚本要求每次校验（no-cache），改完代码刷新即生效；资源文件不变，交给浏览器缓存
	clientFiles := gzipped(http.FileServer(noListing{http.Dir(cfg.Client)}))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		clientFiles.ServeHTTP(w, r)
	}))

	srv := &http.Server{Addr: cfg.Addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	scheme := "http"
	if cfg.TLSCert != "" {
		scheme = "https"
	}
	log.Info("服务端已启动", "url", scheme+"://"+cfg.Addr+"/")
	if cfg.TLSCert != "" {
		err = srv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
	} else {
		err = srv.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {

		log.Error("监听失败", "err", err)
		os.Exit(1)
	}
	<-worldDone
	log.Info("已退出，在线角色位置已保存")
}

// noListing 是不列目录的 http.FileSystem：打开到目录时只给 index.html，没有就 404（不把源码目录列给人看）。
type noListing struct{ fs http.FileSystem }

func (n noListing) Open(name string) (http.File, error) {
	f, err := n.fs.Open(name)
	if err != nil {
		return nil, err
	}
	if st, err := f.Stat(); err == nil && st.IsDir() {
		idx := strings.TrimSuffix(name, "/") + "/index.html"
		if _, err := n.fs.Open(idx); err != nil {
			f.Close()
			return nil, os.ErrNotExist
		}
	}
	return f, nil
}
