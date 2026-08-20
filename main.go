package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/mhetem/ALVO-Backtester/internal/config"
)

const (
	migrationsDir   = "sql/schema"
	frontendDir     = "frontend/dist"
	startupTimeout  = 30 * time.Second
	shutdownTimeout = 15 * time.Second
)

var commands = []string{"serve", "sync-symbols"}

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("fatal", slog.Any("err", err))
		os.Exit(1)
	}
}

func run(args []string) error {
	command := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := newLogger(cfg)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch command {
	case "serve":
		return runServe(ctx, cfg, log)
	case "sync-symbols":
		return runSyncSymbols(ctx, cfg, log, args)
	default:
		return fmt.Errorf("unknown command %q (want one of: %s)", command, strings.Join(commands, ", "))
	}
}

func openPool(ctx context.Context, cfg config.Config, log *slog.Logger) (*pgxpool.Pool, error) {
	if err := migrateUp(ctx, cfg.DatabaseURL, log); err != nil {
		return nil, err
	}

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return pool, nil
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
