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
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

const (
	syncTimeout   = 30 * time.Minute
	reportMaxList = 20
)

func runSyncSymbols(ctx context.Context, cfg config.Config, log *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("sync-symbols", flag.ContinueOnError)
	dryRun := flags.Bool("dry-run", false, "report the admission diff without writing to the database or calling brapi")
	noEnrich := flags.Bool("no-enrich", false, "skip the brapi lookup that fills in symbol names")
	prune := flags.Bool("prune", false, "mark symbols brapi no longer lists as inactive")
	batch := flags.Int("batch", 1, "tickers per brapi request (1 on Free, 10 on Startup, 20 on Pro)")

	if err := parseFlags(flags, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	syncCtx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	contracts, err := market.LoadContracts(dataFS, market.ContractsFile)
	if err != nil {
		return err
	}

	lists, err := market.LoadIndexLists(dataFS, market.IndexesDir)
	if err != nil {
		return err
	}

	calendar, err := market.LoadCalendar(dataFS, market.HolidaysFile)
	if err != nil {
		return err
	}

	pool, err := openPool(syncCtx, cfg, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	client := brapi.New(
		brapi.WithToken(cfg.BrapiToken),
		brapi.WithLogger(log),
		brapi.WithUsageRecorder(brapi.NewDBRecorder(database.New(pool))),
	)

	syncer := market.NewSyncer(pool, client, log, contracts, lists)
	report, err := syncer.Sync(syncCtx, market.SyncOptions{
		DryRun:    *dryRun,
		Enrich:    !*noEnrich,
		Prune:     *prune,
		BatchSize: *batch,
	})
	if err != nil {
		return err
	}

	printSyncReport(os.Stdout, report, calendar, client.HasToken())
	return nil
}

func printSyncReport(w io.Writer, report market.Report, calendar *market.Calendar, hasToken bool) {
	heading := "symbol sync"
	if report.DryRun {
		heading = "symbol sync (dry run, nothing was written)"
	}
	fmt.Fprintln(w, heading)

	for _, list := range report.Lists {
		fmt.Fprintf(w, "  list         %s\n", list)
	}

	minYear, maxYear := calendar.Years()
	summary := fmt.Sprintf("%d-%d, %d holidays", minYear, maxYear, calendar.Holidays())
	if next, err := calendar.NextTradingDay(time.Now()); err == nil {
		if session, ok := calendar.Session(next); ok {
			summary += fmt.Sprintf(", next session %s %s-%s %s",
				session.Day.Format(time.DateOnly),
				session.Open.In(calendar.Location()).Format("15:04"),
				session.Close.In(calendar.Location()).Format("15:04"),
				calendar.Location(),
			)
		}
	}
	fmt.Fprintf(w, "  calendar     %s\n", summary)

	token := "absent, enrichment limited to the free tickers"
	if hasToken {
		token = "present"
	}
	fmt.Fprintf(w, "  brapi token  %s\n", token)

	fmt.Fprintf(w, "  symbols      %d\n", report.Total)
	fmt.Fprintf(w, "  enriched     %d\n", report.Enriched)
	fmt.Fprintf(w, "  created      %s\n", tickerList(report.Created))
	fmt.Fprintf(w, "  admissions   %s\n", tickerList(report.Admissions))
	fmt.Fprintf(w, "  departures   %s\n", tickerList(report.Departures))
	fmt.Fprintf(w, "  deactivated  %s\n", tickerList(report.Deactivated))

	if len(report.Departures) > 0 {
		fmt.Fprintln(w, "\ndepartures are reported, never applied: tracked does not go back to false")
	}
}

func tickerList(tickers []string) string {
	switch {
	case len(tickers) == 0:
		return "none"
	case len(tickers) > reportMaxList:
		return fmt.Sprintf("%s and %d more", strings.Join(tickers[:reportMaxList], ", "), len(tickers)-reportMaxList)
	default:
		return strings.Join(tickers, ", ")
	}
}
