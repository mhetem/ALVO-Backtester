package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/config"
)

const (
	healthcheckPath    = "/api/v1/healthz"
	healthcheckTimeout = 5 * time.Second
)

func runHealthcheck(ctx context.Context, cfg config.Config, _ *slog.Logger, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("healthcheck takes no arguments, got %q", args[0])
	}

	ctx, cancel := context.WithTimeout(ctx, healthcheckTimeout)
	defer cancel()

	url := "http://" + net.JoinHostPort("127.0.0.1", cfg.Port) + healthcheckPath

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", healthcheckPath, resp.Status)
	}

	return nil
}
