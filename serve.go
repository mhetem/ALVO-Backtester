package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/api"
	"github.com/mhetem/ALVO-Backtester/internal/backtest"
	"github.com/mhetem/ALVO-Backtester/internal/config"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

func runServe(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	startCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	calendar, err := market.LoadCalendar(dataFS, market.HolidaysFile)
	if err != nil {
		return err
	}

	rates, err := market.LoadRates(dataFS, market.RatesFile)
	if err != nil {
		return err
	}

	pool, err := openPool(startCtx, cfg, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	static, err := fs.Sub(frontendFS, frontendDir)
	if err != nil {
		return err
	}

	runner := backtest.NewRunner(pool, calendar, rates, log)
	runner.Start(ctx)

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           api.NewServer(cfg, pool, log, static, calendar, runner).Handler(),
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

	err = srv.Shutdown(stopCtx)
	runner.Wait()

	return err
}
