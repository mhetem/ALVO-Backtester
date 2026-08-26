package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mhetem/ALVO-Backtester/internal/brapi"
	"github.com/mhetem/ALVO-Backtester/internal/config"
	database "github.com/mhetem/ALVO-Backtester/internal/db"
	"github.com/mhetem/ALVO-Backtester/internal/ingest"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

const (
	ingestTimeout   = 12 * time.Hour
	freeQuotaMonth  = 15000
	backfillHistory = 5
)

func openIngester(ctx context.Context, cfg config.Config, log *slog.Logger) (*pgxpool.Pool, *ingest.Ingester, error) {
	calendar, err := market.LoadCalendar(dataFS, market.HolidaysFile)
	if err != nil {
		return nil, nil, err
	}

	pool, err := openPool(ctx, cfg, log)
	if err != nil {
		return nil, nil, err
	}

	client := brapi.New(
		brapi.WithToken(cfg.BrapiToken),
		brapi.WithLogger(log),
		brapi.WithUsageRecorder(brapi.NewDBRecorder(database.New(pool))),
	)

	return pool, ingest.NewIngester(pool, client, calendar, log), nil
}

func resolveSymbols(ctx context.Context, ingester *ingest.Ingester, tickers string, universe bool) ([]database.Symbol, error) {
	if universe {
		symbols, err := ingester.TrackedSymbols(ctx)
		if err != nil {
			return nil, err
		}
		if len(symbols) == 0 {
			return nil, fmt.Errorf("no tracked symbols; run sync-symbols first")
		}
		return symbols, nil
	}

	names := []string{}
	for _, name := range strings.Split(tickers, ",") {
		if name = strings.ToUpper(strings.TrimSpace(name)); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("pass --symbol PETR4[,VALE3] or --universe")
	}

	symbols := make([]database.Symbol, 0, len(names))
	for _, name := range names {
		symbol, err := ingester.SymbolByTicker(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("looking up %s: %w", name, err)
		}
		symbols = append(symbols, symbol)
	}

	return symbols, nil
}

func parseFlags(flags *flag.FlagSet, args []string) error {
	if err := flags.Parse(args); err != nil {
		return err
	}
	if extra := flags.Args(); len(extra) > 0 {
		return fmt.Errorf("unexpected argument %q: flags need a leading --, as in --%s", extra[0], extra[0])
	}
	return nil
}

func parseDay(cal *market.Calendar, text string) (time.Time, error) {
	parsed, err := time.ParseInLocation(time.DateOnly, strings.TrimSpace(text), cal.Location())
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is not a YYYY-MM-DD date", text)
	}
	return parsed, nil
}

func printUnreachable(w io.Writer, tickers []string) {
	if len(tickers) == 0 {
		return
	}

	fmt.Fprintf(w, "\n%d symbol(s) skipped: brapi serves them only with a token, and BRAPI_TOKEN is unset\n", len(tickers))
	fmt.Fprintf(w, "  %s\n", tickerList(tickers))
	fmt.Fprintf(w, "  the four free tickers are %s\n", strings.Join(brapi.FreeTickers, ", "))
}

func printFailures(w io.Writer, failures []ingest.ChunkResult) {
	if len(failures) == 0 {
		return
	}

	fmt.Fprintf(w, "\n%d chunk(s) failed:\n", len(failures))
	for i, failure := range failures {
		if i == reportMaxList {
			fmt.Fprintf(w, "  and %d more\n", len(failures)-reportMaxList)
			break
		}
		fmt.Fprintf(w, "  %-8s %s %s..%s  %v\n",
			failure.Ticker,
			failure.Timeframe,
			failure.From.Format(time.DateOnly),
			failure.To.Format(time.DateOnly),
			failure.Err,
		)
	}
}
