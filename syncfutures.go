package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/brapi"
	"github.com/mhetem/ALVO-Backtester/internal/config"
	database "github.com/mhetem/ALVO-Backtester/internal/db"
	"github.com/mhetem/ALVO-Backtester/internal/ingest"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

func runSyncFutures(ctx context.Context, cfg config.Config, log *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("sync-futures", flag.ContinueOnError)
	roots := flags.String("root", "", "comma-separated roots to sync (default: WIN,IND,WDO,DOL)")
	fromText := flags.String("from", "", "first day to fetch, YYYY-MM-DD (default: brapi's "+brapi.FuturesFloor+" floor)")
	includeExpired := flags.Bool("include-expired", true, "also fetch contracts that have already expired, which is what the rolls in a continuous series are made of")
	tail := flags.Bool("tail", false, "refresh only the live contracts from the term structure, one request per root")
	dryRun := flags.Bool("dry-run", false, "count the contracts and requests without fetching or writing")

	if err := parseFlags(flags, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	runCtx, cancel := context.WithTimeout(ctx, ingestTimeout)
	defer cancel()

	calendar, err := market.LoadCalendar(dataFS, market.HolidaysFile)
	if err != nil {
		return err
	}

	pool, err := openPool(runCtx, cfg, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	if cfg.BrapiToken == "" {
		return errors.New("futures need a token: brapi serves /v2/futures only to authenticated plans")
	}

	client := brapi.New(
		brapi.WithToken(cfg.BrapiToken),
		brapi.WithLogger(log),
		brapi.WithUsageRecorder(brapi.NewDBRecorder(database.New(pool))),
	)

	from := time.Time{}
	if *fromText != "" {
		if from, err = parseDay(calendar, *fromText); err != nil {
			return err
		}
	}

	ingester := ingest.NewFuturesIngester(pool, client, calendar, log)

	if *tail {
		report, err := ingester.SyncTail(runCtx, splitList(*roots))
		printTailReport(os.Stdout, report)
		if err != nil {
			return err
		}
		if len(report.Failures) > 0 {
			return fmt.Errorf("%d root(s) failed", len(report.Failures))
		}
		return nil
	}

	report, err := ingester.Sync(runCtx, ingest.FuturesOptions{
		Roots:          splitList(*roots),
		From:           from,
		IncludeExpired: *includeExpired,
		DryRun:         *dryRun,
	})
	if err != nil {
		printFuturesReport(os.Stdout, report)
		return err
	}

	printFuturesReport(os.Stdout, report)
	if len(report.Failures) > 0 {
		return fmt.Errorf("%d contract(s) failed", len(report.Failures))
	}
	return nil
}

func printFuturesReport(w io.Writer, report ingest.FuturesReport) {
	heading := "futures sync"
	if report.DryRun {
		heading = "futures sync (dry run, nothing was fetched or written)"
	}
	fmt.Fprintln(w, heading)

	fmt.Fprintf(w, "  roots        %s\n", strings.Join(report.Roots, ", "))
	fmt.Fprintf(w, "  from         %s\n", report.From.Format(time.DateOnly))
	fmt.Fprintf(w, "  listed       %d contracts on brapi\n", report.Listed)
	fmt.Fprintf(w, "  expired      %s\n", expiredNote(report.IncludeExpired))
	fmt.Fprintf(w, "  selected     %d for these roots, %d skipped as expired before the window\n", report.Contracts, report.Expired)
	fmt.Fprintf(w, "  requests     %d\n", report.Requests)

	if !report.DryRun {
		fmt.Fprintf(w, "  bars         %d stored\n", report.Bars)
	}

	if len(report.Failures) == 0 {
		return
	}

	fmt.Fprintf(w, "\n%d contract(s) failed:\n", len(report.Failures))
	for i, failure := range report.Failures {
		if i == reportMaxList {
			fmt.Fprintf(w, "  and %d more\n", len(report.Failures)-reportMaxList)
			break
		}
		fmt.Fprintf(w, "  %s\n", failure)
	}
}

func splitList(raw string) []string {
	out := []string{}
	for _, item := range strings.Split(raw, ",") {
		if item = strings.ToUpper(strings.TrimSpace(item)); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func expiredNote(included bool) string {
	if included {
		return "included: rolls need the contracts that have already settled"
	}
	return "excluded: only currently listed contracts, so the series will contain no rolls"
}

func printTailReport(w io.Writer, report ingest.TailReport) {
	fmt.Fprintln(w, "futures tail")

	fmt.Fprintf(w, "  roots        %s\n", tickerList(report.Roots))
	fmt.Fprintf(w, "  requests     %d, one per root\n", report.Requests)
	fmt.Fprintf(w, "  contracts    %d live expirations refreshed\n", report.Contracts)
	fmt.Fprintf(w, "  bars         %d stored\n", report.Bars)

	if !report.Day.IsZero() {
		fmt.Fprintf(w, "  session      %s\n", report.Day.Format(time.DateOnly))
	}
	if report.Requests == 0 {
		fmt.Fprintln(w, "\nnothing backfilled yet: run sync-futures without --tail first")
	}

	if len(report.Failures) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%d root(s) failed:\n", len(report.Failures))
	for _, failure := range report.Failures {
		fmt.Fprintf(w, "  %s\n", failure)
	}
}
