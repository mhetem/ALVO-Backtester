package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/config"
	"github.com/mhetem/ALVO-Backtester/internal/ingest"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

func runSyncCandles(ctx context.Context, cfg config.Config, log *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("sync-candles", flag.ContinueOnError)
	tickers := flags.String("symbol", "", "comma-separated tickers to refresh")
	universe := flags.Bool("universe", true, "refresh every tracked symbol")
	timeframe := flags.String("timeframe", string(market.TF1d), "stored timeframe to refresh (5m or 1d)")
	sessions := flags.Int("sessions", ingest.DefaultSyncSessions, "trading days of tail to refresh")
	dryRun := flags.Bool("dry-run", false, "count the requests the sync would make without fetching or writing")

	if err := parseFlags(flags, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	tf, err := market.ParseStoredTimeframe(*timeframe)
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithTimeout(ctx, ingestTimeout)
	defer cancel()

	pool, ingester, err := openIngester(runCtx, cfg, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	symbols, err := resolveSymbols(runCtx, ingester, *tickers, *universe && *tickers == "")
	if err != nil {
		return err
	}

	report, err := ingester.Sync(runCtx, symbols, ingest.SyncOptions{
		Timeframe: tf,
		Sessions:  *sessions,
		DryRun:    *dryRun,
	})
	if err != nil {
		printSyncCandlesReport(os.Stdout, report)
		return err
	}

	printSyncCandlesReport(os.Stdout, report)
	if len(report.Failures) > 0 {
		return fmt.Errorf("%d of %d symbols failed", len(report.Failures), report.Requests)
	}
	return nil
}

func printSyncCandlesReport(w io.Writer, report ingest.SyncReport) {
	heading := "candle sync"
	if report.DryRun {
		heading = "candle sync (dry run, nothing was fetched or written)"
	}
	fmt.Fprintln(w, heading)

	fmt.Fprintf(w, "  timeframe    %s\n", report.Timeframe)
	fmt.Fprintf(w, "  sessions     %s..%s\n", report.From.Format(time.DateOnly), report.To.Format(time.DateOnly))
	fmt.Fprintf(w, "  symbols      %d\n", report.Symbols)
	fmt.Fprintf(w, "  requests     %d\n", report.Requests)

	if !report.DryRun {
		fmt.Fprintf(w, "  bars         %d stored, %d rejected\n", report.Bars, report.Rejected)
		fmt.Fprintf(w, "  empty        %d\n", report.Empty)
	}

	printUnreachable(w, report.Unreachable)
	printFailures(w, report.Failures)
}
