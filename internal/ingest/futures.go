package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mhetem/ALVO-Backtester/internal/brapi"
	database "github.com/mhetem/ALVO-Backtester/internal/db"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

type FuturesFetcher interface {
	FuturesList(ctx context.Context, includeExpired bool) ([]brapi.FutureContract, error)
	FuturesHistory(ctx context.Context, symbol string, from time.Time) (brapi.FutureContract, error)
	FuturesTermStructure(ctx context.Context, asset string) ([]brapi.FutureTermContract, error)
	HasToken() bool
}

type FuturesIngester struct {
	pool    *pgxpool.Pool
	queries *database.Queries
	client  FuturesFetcher
	cal     *market.Calendar
	log     *slog.Logger
}

func NewFuturesIngester(pool *pgxpool.Pool, client FuturesFetcher, cal *market.Calendar, log *slog.Logger) *FuturesIngester {
	return &FuturesIngester{
		pool:    pool,
		queries: database.New(pool),
		client:  client,
		cal:     cal,
		log:     log,
	}
}

type FuturesOptions struct {
	Roots          []string
	From           time.Time
	IncludeExpired bool
	DryRun         bool
}

type FuturesReport struct {
	Roots          []string
	Listed         int
	Contracts      int
	Expired        int
	Requests       int
	Bars           int
	From           time.Time
	Failures       []string
	IncludeExpired bool
	DryRun         bool
}

func (f *FuturesIngester) Sync(ctx context.Context, opts FuturesOptions) (FuturesReport, error) {
	roots := normaliseRoots(opts.Roots)
	from := opts.From
	if from.IsZero() {
		from = brapi.FuturesFloorDate()
	}

	report := FuturesReport{Roots: roots, From: from, Failures: []string{}, IncludeExpired: opts.IncludeExpired, DryRun: opts.DryRun}

	listed, err := f.client.FuturesList(ctx, opts.IncludeExpired)
	if err != nil {
		return report, err
	}
	report.Listed = len(listed)

	wanted := make([]brapi.FutureContract, 0, len(listed))
	for _, contract := range listed {
		if !slices.Contains(roots, strings.ToUpper(contract.UnderlyingAsset)) {
			continue
		}

		expiration, err := parseFuturesDay(contract.ExpirationDate)
		if err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("%s: %v", contract.Symbol, err))
			continue
		}
		if expiration.Before(from) {
			report.Expired++
			continue
		}

		wanted = append(wanted, contract)
	}

	slices.SortFunc(wanted, func(a, b brapi.FutureContract) int { return strings.Compare(a.Symbol, b.Symbol) })
	report.Contracts = len(wanted)

	if opts.DryRun {
		report.Requests = len(wanted)
		return report, nil
	}

	for _, contract := range wanted {
		bars, err := f.ingestContract(ctx, contract, from)
		report.Requests++
		if err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("%s: %v", contract.Symbol, err))
			f.log.ErrorContext(ctx, "futures history failed",
				slog.String("symbol", contract.Symbol),
				slog.Any("err", err),
			)
		} else {
			report.Bars += bars
		}

		if ctx.Err() != nil {
			return report, ctx.Err()
		}
	}

	return report, nil
}

func (f *FuturesIngester) ingestContract(ctx context.Context, contract brapi.FutureContract, from time.Time) (int, error) {
	row, err := f.upsertContract(ctx, contract)
	if err != nil {
		return 0, err
	}

	history, err := f.client.FuturesHistory(ctx, contract.Symbol, from)
	if err != nil {
		return 0, err
	}

	params := make([]database.UpsertFuturesQuotesParams, 0, len(history.History))
	for _, bar := range history.History {
		if bar.Settlement == nil || *bar.Settlement <= 0 {
			continue
		}

		day := bar.TS()
		if day.Before(from) {
			continue
		}

		params = append(params, database.UpsertFuturesQuotesParams{
			ContractID: row.ID,
			Day:        day,
			Settlement: *bar.Settlement,
			High:       positive(bar.High),
			Low:        positive(bar.Low),
			Close:      positive(bar.Close),
			Average:    positive(bar.Average),
			Volume:     bar.Volume,
			Trades:     bar.Trades,
		})
	}

	if len(params) == 0 {
		return 0, nil
	}

	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var batchErr error
	batch := f.queries.WithTx(tx).UpsertFuturesQuotes(ctx, params)
	batch.Exec(func(_ int, err error) {
		if err != nil && batchErr == nil {
			batchErr = err
		}
	})
	if err := batch.Close(); err != nil && batchErr == nil {
		batchErr = err
	}
	if batchErr != nil {
		return 0, fmt.Errorf("storing %s quotes: %w", contract.Symbol, batchErr)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("committing %s quotes: %w", contract.Symbol, err)
	}

	return len(params), nil
}

func (f *FuturesIngester) upsertContract(ctx context.Context, contract brapi.FutureContract) (database.FuturesContract, error) {
	expiration, err := parseFuturesDay(contract.ExpirationDate)
	if err != nil {
		return database.FuturesContract{}, err
	}

	multiplier := 1.0
	if contract.ContractMultiplier != nil && *contract.ContractMultiplier > 0 {
		multiplier = *contract.ContractMultiplier
	}

	lot := int32(1)
	if contract.AllocationRoundLot != nil && *contract.AllocationRoundLot > 0 {
		lot = *contract.AllocationRoundLot
	}

	currency := strings.ToUpper(strings.TrimSpace(contract.TradingCurrency))
	if currency == "" {
		currency = market.DefaultCurrency
	}

	return f.queries.UpsertFuturesContract(ctx, database.UpsertFuturesContractParams{
		Symbol:      strings.ToUpper(contract.Symbol),
		Root:        strings.ToUpper(contract.UnderlyingAsset),
		Description: text(contract.AssetDescription),
		Segment:     text(contract.Segment),
		Multiplier:  multiplier,
		LotSize:     lot,
		Currency:    currency,
		Isin:        text(contract.ISIN),
		FirstTrade:  optionalDay(contract.FirstTradeDate),
		LastTrade:   optionalDay(contract.LastTradeDate),
		Expiration:  expiration,
	})
}

func normaliseRoots(roots []string) []string {
	if len(roots) == 0 {
		return slices.Clone(market.DefaultFutureRoots)
	}

	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if root = strings.ToUpper(strings.TrimSpace(root)); root != "" {
			out = append(out, root)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func parseFuturesDay(value string) (time.Time, error) {
	day, err := time.Parse(time.DateOnly, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is not a YYYY-MM-DD date", value)
	}
	return day, nil
}

func optionalDay(value string) *time.Time {
	day, err := parseFuturesDay(value)
	if err != nil {
		return nil
	}
	return &day
}

func text(value string) *string {
	if value = strings.TrimSpace(value); value == "" {
		return nil
	}
	return &value
}

func positive(value *float64) *float64 {
	if value == nil || *value <= 0 {
		return nil
	}
	return value
}

type TailReport struct {
	Roots     []string
	Requests  int
	Contracts int
	Bars      int
	Day       time.Time
	Failures  []string
}

// The daily tail is one request per root rather than one per contract: the term structure
// returns every live expiration for an asset in a single call. Expired contracts never move
// again, so they belong to the backfill and not here.
func (f *FuturesIngester) SyncTail(ctx context.Context, roots []string) (TailReport, error) {
	report := TailReport{Roots: []string{}, Failures: []string{}}

	known, err := f.queries.ListFuturesRoots(ctx)
	if err != nil {
		return report, fmt.Errorf("listing futures roots: %w", err)
	}
	if len(known) == 0 {
		return report, nil
	}

	wanted := normaliseRoots(roots)
	for _, root := range known {
		if !slices.Contains(wanted, root) {
			continue
		}
		report.Roots = append(report.Roots, root)
	}

	for _, root := range report.Roots {
		contracts, err := f.client.FuturesTermStructure(ctx, root)
		report.Requests++
		if err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("%s: %v", root, err))
			f.log.ErrorContext(ctx, "futures term structure failed",
				slog.String("root", root),
				slog.Any("err", err),
			)
			continue
		}

		for _, contract := range contracts {
			bars, err := f.storeTermContract(ctx, contract)
			if err != nil {
				report.Failures = append(report.Failures, fmt.Sprintf("%s: %v", contract.Symbol, err))
				continue
			}

			report.Contracts++
			report.Bars += bars

			if day := contract.TS(); day.After(report.Day) {
				report.Day = day
			}
		}

		if ctx.Err() != nil {
			return report, ctx.Err()
		}
	}

	return report, nil
}

func (f *FuturesIngester) storeTermContract(ctx context.Context, contract brapi.FutureTermContract) (int, error) {
	row, err := f.upsertContract(ctx, contract.FutureContract)
	if err != nil {
		return 0, err
	}

	if contract.Settlement == nil || *contract.Settlement <= 0 {
		return 0, nil
	}

	var batchErr error
	batch := f.queries.UpsertFuturesQuotes(ctx, []database.UpsertFuturesQuotesParams{{
		ContractID: row.ID,
		Day:        contract.TS(),
		Settlement: *contract.Settlement,
		High:       positive(contract.High),
		Low:        positive(contract.Low),
		Close:      positive(contract.Close),
		Average:    positive(contract.Average),
		Volume:     contract.Volume,
		Trades:     contract.Trades,
	}})
	batch.Exec(func(_ int, err error) {
		if err != nil && batchErr == nil {
			batchErr = err
		}
	})
	if err := batch.Close(); err != nil && batchErr == nil {
		batchErr = err
	}
	if batchErr != nil {
		return 0, fmt.Errorf("storing %s: %w", contract.Symbol, batchErr)
	}

	return 1, nil
}
