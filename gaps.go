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
	database "github.com/mhetem/ALVO-Backtester/internal/db"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

const gapListLimit = 10

func runGaps(ctx context.Context, cfg config.Config, log *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("gaps", flag.ContinueOnError)
	tickers := flags.String("symbol", "", "comma-separated tickers to check")
	universe := flags.Bool("universe", false, "check every tracked symbol")
	timeframe := flags.String("timeframe", string(market.TF1d), "stored timeframe to check (5m or 1d)")
	fromText := flags.String("from", "", "first day to check, YYYY-MM-DD (default: each symbol's earliest stored bar)")
	toText := flags.String("to", "", "last day to check, YYYY-MM-DD (default: today)")
	partial := flags.Bool("partial", false, "also list sessions that are present but short of a full bar count")

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

	runCtx, cancel := context.WithTimeout(ctx, syncTimeout)
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

	from := time.Time{}
	if *fromText != "" {
		if from, err = parseDay(calendar, *fromText); err != nil {
			return err
		}
	}

	symbols, err := resolveSymbols(runCtx, pool, *tickers, *universe)
	if err != nil {
		return err
	}

	if from.IsZero() {
		fmt.Fprintf(os.Stdout, "gap report  %s  through %s, each symbol measured from its earliest stored bar\n\n", tf, to.Format(time.DateOnly))
	} else {
		fmt.Fprintf(os.Stdout, "gap report  %s  %s..%s\n\n", tf, from.Format(time.DateOnly), to.Format(time.DateOnly))
	}

	dirty, empty := 0, 0
	for _, symbol := range symbols {
		start := from
		if start.IsZero() {
			earliest, stored, err := ingester.EarliestStored(runCtx, symbol.ID, tf)
			if err != nil {
				return err
			}
			if !stored {
				empty++
				fmt.Fprintf(os.Stdout, "%-10s %-5s  no %s candles stored\n", symbol.Ticker, "EMPTY", tf)
				continue
			}
			start = earliest
		}

		report, err := ingester.Coverage(runCtx, symbol.ID, tf, start, to)
		if err != nil {
			return err
		}
		if !report.Clean() {
			dirty++
		}
		printGapReport(os.Stdout, symbol, report, start, *partial)
	}

	if dirty > 0 {
		fmt.Fprintf(os.Stdout, "\n%d of %d symbols have holes to refill\n", dirty, len(symbols))
	}
	if empty > 0 {
		fmt.Fprintf(os.Stdout, "%d of %d symbols have no %s history at all\n", empty, len(symbols), tf)
	}
	return nil
}

func printGapReport(w io.Writer, symbol database.Symbol, report market.GapReport, from time.Time, showPartial bool) {
	status := "clean"
	if !report.Clean() {
		status = "HOLES"
	}

	fmt.Fprintf(w, "%-10s %-5s  %d/%d bars over %d sessions from %s  %s\n",
		symbol.Ticker, status, report.Bars, report.Expected, report.Sessions,
		from.Format(time.DateOnly), missingSummary(report))

	for i, day := range report.Missing {
		if i == gapListLimit {
			fmt.Fprintf(w, "    and %d more missing sessions\n", len(report.Missing)-gapListLimit)
			break
		}
		fmt.Fprintf(w, "    missing  %s\n", day.Format(time.DateOnly))
	}

	for i, ts := range report.Unexpected {
		if i == gapListLimit {
			fmt.Fprintf(w, "    and %d more bars outside any session\n", len(report.Unexpected)-gapListLimit)
			break
		}
		fmt.Fprintf(w, "    outside  %s\n", ts.Format(time.RFC3339))
	}

	if !showPartial {
		return
	}
	for i, coverage := range report.Partial {
		if i == gapListLimit {
			fmt.Fprintf(w, "    and %d more short sessions\n", len(report.Partial)-gapListLimit)
			break
		}
		fmt.Fprintf(w, "    short    %s  %d/%d\n", coverage.Day.Format(time.DateOnly), coverage.Present, coverage.Expected)
	}
}

func missingSummary(report market.GapReport) string {
	return fmt.Sprintf("(%d missing, %d short, %d outside a session)",
		len(report.Missing), len(report.Partial), len(report.Unexpected))
}
