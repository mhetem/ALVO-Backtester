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

func runBackfill(ctx context.Context, cfg config.Config, log *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("backfill", flag.ContinueOnError)
	tickers := flags.String("symbol", "", "comma-separated tickers to backfill")
	universe := flags.Bool("universe", false, "backfill every tracked symbol")
	timeframe := flags.String("timeframe", string(market.TF1d), "stored timeframe to fetch (5m or 1d)")
	fromText := flags.String("from", "", "first day to fetch, YYYY-MM-DD (default: five years back)")
	toText := flags.String("to", "", "last day to fetch, YYYY-MM-DD (default: today)")
	chunk := flags.Int("chunk", 0, "days of history per brapi request, 1d only (default: 365)")
	rangeToken := flags.String("range", "", "brapi range token to fetch instead of a date window, one request per symbol (5m always uses one)")
	dryRun := flags.Bool("dry-run", false, "count the requests the backfill would make without fetching or writing")
	force := flags.Bool("force", false, "refetch chunks that are already fully covered")

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

	calendar := ingester.Calendar()

	to := calendar.Date(time.Now().In(calendar.Location()).Date())
	if *toText != "" {
		if to, err = parseDay(calendar, *toText); err != nil {
			return err
		}
	}

	from := to.AddDate(-backfillHistory, 0, 0)
	if *fromText != "" {
		if from, err = parseDay(calendar, *fromText); err != nil {
			return err
		}
	}

	symbols, err := resolveSymbols(runCtx, pool, *tickers, *universe)
	if err != nil {
		return err
	}

	if tf == market.TF5m && *chunk > 0 {
		return fmt.Errorf("--chunk does not apply to 5m: brapi ignores startDate/endDate for intraday intervals, so the fetch is one %s-or-wider range request per symbol. Use --range to widen it",
			ingest.MinIntradayRange)
	}

	report, err := ingester.Backfill(runCtx, symbols, ingest.BackfillOptions{
		Timeframe: tf,
		From:      from,
		To:        to,
		ChunkDays: *chunk,
		Range:     *rangeToken,
		DryRun:    *dryRun,
		Force:     *force,
	})
	if err != nil {
		printBackfillReport(os.Stdout, report)
		return err
	}

	printBackfillReport(os.Stdout, report)
	if len(report.Failures) > 0 {
		return fmt.Errorf("%d of %d chunks failed", len(report.Failures), report.Requests)
	}
	return nil
}

func printBackfillReport(w io.Writer, report ingest.BackfillReport) {
	heading := "backfill"
	if report.DryRun {
		heading = "backfill (dry run, nothing was fetched or written)"
	}
	fmt.Fprintln(w, heading)

	fmt.Fprintf(w, "  timeframe    %s\n", report.Timeframe)
	fmt.Fprintf(w, "  range        %s..%s\n", report.From.Format(time.DateOnly), report.To.Format(time.DateOnly))
	if report.Range != "" {
		fmt.Fprintf(w, "  fetched as   range=%s, trimmed to the window above\n", report.Range)
	}
	fmt.Fprintf(w, "  symbols      %d\n", report.Symbols)
	fmt.Fprintf(w, "  chunks       %d planned, %d skipped as already covered\n", report.Chunks, report.Skipped)
	fmt.Fprintf(w, "  requests     %d (%.1f%% of a 15,000-request free month)\n", report.Requests, 100*float64(report.Requests)/freeQuotaMonth)

	if !report.DryRun {
		fmt.Fprintf(w, "  bars         %d stored, %d rejected\n", report.Bars, report.Rejected)
		fmt.Fprintf(w, "  empty        %d\n", report.Empty)
	}

	printUnreachable(w, report.Unreachable)
	printFailures(w, report.Failures)
}
