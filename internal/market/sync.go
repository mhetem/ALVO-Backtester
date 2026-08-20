package market

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mhetem/ALVO-Backtester/internal/brapi"
	database "github.com/mhetem/ALVO-Backtester/internal/db"
)

type Syncer struct {
	pool      *pgxpool.Pool
	client    *brapi.Client
	log       *slog.Logger
	contracts Contracts
	lists     []IndexList
}

func NewSyncer(pool *pgxpool.Pool, client *brapi.Client, log *slog.Logger, contracts Contracts, lists []IndexList) *Syncer {
	return &Syncer{pool: pool, client: client, log: log, contracts: contracts, lists: lists}
}

type SyncOptions struct {
	DryRun    bool
	Enrich    bool
	Prune     bool
	BatchSize int
}

type Report struct {
	Lists       []string
	Total       int
	Created     []string
	Admissions  []string
	Departures  []string
	Deactivated []string
	Enriched    int
	DryRun      bool
}

func (s *Syncer) Sync(ctx context.Context, opts SyncOptions) (Report, error) {
	queries := database.New(s.pool)

	rows, err := queries.ListSymbols(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("listing symbols: %w", err)
	}

	existing := make([]ExistingSymbol, 0, len(rows))
	for _, row := range rows {
		existing = append(existing, ExistingSymbol{
			Ticker:  row.Ticker,
			Kind:    row.Kind,
			Active:  row.Active,
			Tracked: row.Tracked,
		})
	}

	plan, err := BuildPlan(s.lists, s.contracts, existing)
	if err != nil {
		return Report{}, err
	}

	report := Report{
		Lists:       make([]string, 0, len(s.lists)),
		Total:       len(plan.Symbols),
		Created:     plan.Created,
		Admissions:  plan.Admissions,
		Departures:  plan.Departures,
		Deactivated: []string{},
		DryRun:      opts.DryRun,
	}
	for _, list := range s.lists {
		report.Lists = append(report.Lists, fmt.Sprintf("%s as of %s, %d tickers (%s)", list.Index, list.AsOf, len(list.Tickers), list.File))
	}

	if opts.DryRun {
		return report, nil
	}

	if opts.Enrich && s.client != nil {
		report.Enriched = s.enrich(ctx, plan.Symbols, opts.BatchSize)
	}

	stale := []string{}
	if opts.Prune && s.client != nil {
		if stale, err = s.staleSymbols(ctx, existing); err != nil {
			return report, err
		}
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return report, fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	inTx := queries.WithTx(tx)
	for _, symbol := range plan.Symbols {
		if _, err := inTx.UpsertSymbol(ctx, symbol.upsertParams(today)); err != nil {
			return report, fmt.Errorf("upserting %s: %w", symbol.Ticker, err)
		}
	}

	if len(stale) > 0 {
		deactivated, err := inTx.DeactivateSymbols(ctx, stale)
		if err != nil {
			return report, fmt.Errorf("deactivating symbols: %w", err)
		}
		report.Deactivated = deactivated
	}

	if err := tx.Commit(ctx); err != nil {
		return report, fmt.Errorf("committing symbol sync: %w", err)
	}

	return report, nil
}

func (s *Syncer) enrich(ctx context.Context, symbols []DesiredSymbol, batchSize int) int {
	if batchSize < 1 {
		batchSize = 1
	}

	position := map[string]int{}
	tickers := []string{}

	for i, symbol := range symbols {
		if symbol.Kind == KindFuture {
			continue
		}
		if !s.client.HasToken() && !brapi.IsFreeTicker(symbol.Ticker) {
			continue
		}
		position[symbol.Ticker] = i
		tickers = append(tickers, symbol.Ticker)
	}

	if len(tickers) == 0 {
		return 0
	}

	enriched := 0
	for chunk := range slices.Chunk(tickers, batchSize) {
		quotes, err := s.client.Quote(ctx, chunk, brapi.QuoteOptions{})
		if err != nil {
			s.log.WarnContext(ctx, "brapi enrichment failed",
				slog.String("tickers", strings.Join(chunk, ",")),
				slog.String("err", err.Error()),
			)
			if ctx.Err() != nil || brapi.IsUnauthorized(err) {
				break
			}
			continue
		}

		for _, quote := range quotes {
			i, ok := position[strings.ToUpper(quote.Symbol)]
			if !ok {
				continue
			}
			if quote.ShortName != "" {
				symbols[i].ShortName = quote.ShortName
			}
			if quote.LongName != "" {
				symbols[i].LongName = quote.LongName
			}
			if quote.Currency != "" {
				symbols[i].Currency = quote.Currency
			}
			enriched++
		}
	}

	return enriched
}

func (s *Syncer) staleSymbols(ctx context.Context, existing []ExistingSymbol) ([]string, error) {
	available, err := s.client.Available(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("listing available tickers: %w", err)
	}

	listed := map[string]bool{}
	for _, ticker := range available.Stocks {
		listed[strings.ToUpper(ticker)] = true
	}
	for _, ticker := range available.Indexes {
		listed[strings.ToUpper(ticker)] = true
	}
	if len(listed) == 0 {
		return nil, errors.New("brapi returned an empty availability list; refusing to deactivate every symbol")
	}

	stale := []string{}
	for _, symbol := range existing {
		if !symbol.Active || Kind(symbol.Kind) == KindFuture || listed[symbol.Ticker] {
			continue
		}
		stale = append(stale, symbol.Ticker)
	}

	slices.Sort(stale)
	return stale, nil
}

func (d DesiredSymbol) upsertParams(day time.Time) database.UpsertSymbolParams {
	params := database.UpsertSymbolParams{
		Ticker:    d.Ticker,
		Kind:      string(d.Kind),
		Currency:  d.Currency,
		LotSize:   d.LotSize,
		TickSize:  d.TickSize,
		Active:    true,
		Tracked:   d.Tracked,
		FirstSeen: &day,
		LastSeen:  &day,
	}

	if d.ShortName != "" {
		params.ShortName = &d.ShortName
	}
	if d.LongName != "" {
		params.LongName = &d.LongName
	}
	if d.PointValue != nil {
		value := *d.PointValue
		params.PointValue = &value
	}

	return params
}
