package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/mhetem/ALVO-Backtester/internal/api"
	"github.com/mhetem/ALVO-Backtester/internal/config"
)

const (
	migrationsDir   = "sql/schema"
	frontendDir     = "frontend/dist"
	startupTimeout  = 30 * time.Second
	shutdownTimeout = 15 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", slog.Any("err", err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := newLogger(cfg)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	startCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	if err := migrateUp(startCtx, cfg.DatabaseURL, log); err != nil {
		return err
	}

	pool, err := pgxpool.New(startCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := pool.Ping(startCtx); err != nil {
		return err
	}

	static, err := fs.Sub(frontendFS, frontendDir)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           api.NewServer(cfg, pool, log, static).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", slog.String("addr", srv.Addr), slog.String("platform", cfg.Platform))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	stopCtx, stopCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer stopCancel()

	return srv.Shutdown(stopCtx)
}

func newLogger(cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if cfg.IsDev() {
		opts.Level = slog.LevelDebug
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}

func migrateUp(ctx context.Context, databaseURL string, log *slog.Logger) error {
	pgxCfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return err
	}

	db := stdlib.OpenDB(*pgxCfg.ConnConfig)
	defer db.Close()

	goose.SetBaseFS(schemaFS)
	goose.SetLogger(gooseLogger{log})
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}

	return goose.UpContext(ctx, db, migrationsDir)
}

type gooseLogger struct{ log *slog.Logger }

func (g gooseLogger) Printf(format string, v ...any) {
	g.log.Info(trimNewline(format, v...), slog.String("component", "goose"))
}

func (g gooseLogger) Fatalf(format string, v ...any) {
	g.log.Error(trimNewline(format, v...), slog.String("component", "goose"))
	os.Exit(1)
}

func trimNewline(format string, v ...any) string {
	return strings.TrimRight(fmt.Sprintf(format, v...), "\n")
}
