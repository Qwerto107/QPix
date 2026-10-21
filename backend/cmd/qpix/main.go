package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"qpix/internal/app"
	"qpix/internal/config"
	"qpix/internal/database"
	"qpix/internal/storage"
)

func main() {
	if err := run(); err != nil {
		slog.Error("服务启动失败", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	db, err := database.Open(cfg.DBDriver, cfg.DSN)
	if err != nil {
		return err
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err = database.Migrate(db); err != nil {
		return err
	}
	local, err := storage.NewLocal(cfg.StorageRoot)
	if err != nil {
		return err
	}
	api, err := app.New(cfg, db, map[string]storage.Store{"local": local})
	if err != nil {
		return err
	}
	server := &http.Server{Addr: cfg.Address, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: 2 * time.Minute, IdleTimeout: time.Minute, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			maintenanceCtx, cancel := context.WithTimeout(ctx, time.Minute)
			api.Maintain(maintenanceCtx)
			cancel()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			slog.Error("优雅关闭失败", "error", err)
		}
	}()
	slog.Info("QPix 已启动", "address", cfg.Address, "database", cfg.DBDriver)
	err = server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
