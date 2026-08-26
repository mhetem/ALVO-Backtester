package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mhetem/ALVO-Backtester/internal/brapi"
	database "github.com/mhetem/ALVO-Backtester/internal/db"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

const (
	StatusOK      = "ok"
	StatusEmpty   = "empty"
	StatusSkipped = "skipped"
	StatusFailed  = "failed"

	DefaultWriteBatch  = 500
	rejectionLogSample = 3
)

type Fetcher interface {
	Quote(ctx context.Context, tickers []string, opts brapi.QuoteOptions) ([]brapi.Quote, error)
	HasToken() bool
}

type Ingester struct {
	pool       *pgxpool.Pool
	queries    *database.Queries
	client     Fetcher
	cal        *market.Calendar
	log        *slog.Logger
	writeBatch int
}

func NewIngester(pool *pgxpool.Pool, client Fetcher, cal *market.Calendar, log *slog.Logger) *Ingester {
	return &Ingester{
		pool:       pool,
		queries:    database.New(pool),
		client:     client,
		cal:        cal,
		log:        log,
		writeBatch: DefaultWriteBatch,
	}
}

func (i *Ingester) Calendar() *market.Calendar { return i.cal }

func (i *Ingester) Reachable(ticker string) bool {
	return i.client.HasToken() || brapi.IsFreeTicker(ticker)
}

func (i *Ingester) TrackedSymbols(ctx context.Context) ([]database.Symbol, error) {
	symbols, err := i.queries.ListTrackedSymbols(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing tracked symbols: %w", err)
	}
	return slices.DeleteFunc(symbols, func(s database.Symbol) bool {
		return market.Kind(s.Kind) == market.KindFuture
	}), nil
}

func (i *Ingester) SymbolByTicker(ctx context.Context, ticker string) (database.Symbol, error) {
	return i.queries.GetSymbolByTicker(ctx, ticker)
}

type ChunkRequest struct {
	Symbol    database.Symbol
	Timeframe market.Timeframe
	From      time.Time
	To        time.Time
	Range     string
	KeepFrom  time.Time
	KeepTo    time.Time
}

type ChunkResult struct {
	Ticker    string
	Timeframe market.Timeframe
	From      time.Time
	To        time.Time
	Status    string
	Bars      int
	Rejected  int
	Duration  time.Duration
	Err       error
}

func (i *Ingester) Chunk(ctx context.Context, req ChunkRequest) ChunkResult {
	started := time.Now()
	result := ChunkResult{
		Ticker:    req.Symbol.Ticker,
		Timeframe: req.Timeframe,
		From:      req.From,
		To:        req.To,
		Status:    StatusFailed,
	}

	runID, err := i.queries.StartIngestRun(ctx, database.StartIngestRunParams{
		SymbolID:   req.Symbol.ID,
		Timeframe:  string(req.Timeframe),
		RangeStart: req.From,
		RangeEnd:   req.To,
	})
	if err != nil {
		result.Err = fmt.Errorf("opening ingest run for %s: %w", req.Symbol.Ticker, err)
		return result
	}

	result = i.fetchAndStore(ctx, req, result)
	result.Duration = time.Since(started)

	i.finish(ctx, runID, result)
	return result
}

func (i *Ingester) fetchAndStore(ctx context.Context, req ChunkRequest, result ChunkResult) ChunkResult {
	opts := brapi.QuoteOptions{Interval: req.Timeframe.BrapiInterval()}
	if req.Range != "" {
		opts.Range = req.Range
	} else {
		opts.From, opts.To = req.From, req.To
	}

	quotes, err := i.client.Quote(ctx, []string{req.Symbol.Ticker}, opts)
	if err != nil {
		result.Err = err
		return result
	}

	bars := []brapi.Bar{}
	for _, quote := range quotes {
		bars = append(bars, quote.HistoricalDataPrice...)
	}

	candles, rejected := Normalize(i.cal, req.Timeframe, bars)
	result.Rejected = len(rejected)
	i.logRejections(ctx, req, rejected)

	if !req.KeepFrom.IsZero() || !req.KeepTo.IsZero() {
		kept := candles[:0]
		for _, candle := range candles {
			if !req.KeepFrom.IsZero() && candle.TS.Before(req.KeepFrom) {
				continue
			}
			if !req.KeepTo.IsZero() && !candle.TS.Before(req.KeepTo) {
				continue
			}
			kept = append(kept, candle)
		}
		candles = kept
	}

	if len(candles) == 0 {
		result.Status = StatusEmpty
		return result
	}

	if err := i.Store(ctx, req.Symbol.ID, req.Timeframe, candles); err != nil {
		result.Err = err
		return result
	}

	result.Status = StatusOK
	result.Bars = len(candles)
	return result
}

func (i *Ingester) Store(ctx context.Context, symbolID int64, tf market.Timeframe, candles []market.Candle) error {
	if !tf.Stored() {
		return fmt.Errorf("refusing to store %s: only %s are written", tf, market.JoinTimeframes(market.StoredTimeframes))
	}
	if len(candles) == 0 {
		return nil
	}

	tx, err := i.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning candle write: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	inTx := i.queries.WithTx(tx)

	for start := 0; start < len(candles); start += i.writeBatch {
		end := min(start+i.writeBatch, len(candles))

		params := make([]database.UpsertCandlesParams, 0, end-start)
		for _, candle := range candles[start:end] {
			params = append(params, database.UpsertCandlesParams{
				SymbolID:  symbolID,
				Timeframe: string(tf),
				Ts:        candle.TS,
				Open:      candle.Open,
				High:      candle.High,
				Low:       candle.Low,
				Close:     candle.Close,
				AdjClose:  candle.AdjClose,
				Volume:    candle.Volume,
			})
		}

		var batchErr error
		batch := inTx.UpsertCandles(ctx, params)
		batch.Exec(func(index int, err error) {
			if err != nil && batchErr == nil {
				batchErr = fmt.Errorf("upserting %s candle at %s: %w",
					tf, candles[start+index].TS.UTC().Format(time.RFC3339), err)
			}
		})
		if batchErr != nil {
			return batchErr
		}
	}

	return tx.Commit(ctx)
}

func (i *Ingester) EarliestStored(ctx context.Context, symbolID int64, tf market.Timeframe) (time.Time, bool, error) {
	row, err := i.queries.EarliestCandle(ctx, database.EarliestCandleParams{
		SymbolID:  symbolID,
		Timeframe: string(tf),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("reading the earliest %s candle: %w", tf, err)
	}
	return row.Ts, true, nil
}

func (i *Ingester) LatestStored(ctx context.Context, symbolID int64, tf market.Timeframe) (time.Time, bool, error) {
	row, err := i.queries.LatestCandle(ctx, database.LatestCandleParams{
		SymbolID:  symbolID,
		Timeframe: string(tf),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("reading the latest %s candle: %w", tf, err)
	}
	return row.Ts, true, nil
}

func (i *Ingester) Coverage(ctx context.Context, symbolID int64, tf market.Timeframe, from, to time.Time) (market.GapReport, error) {
	start, end := i.cal.DayBounds(from, to)

	stamps, err := i.queries.ListCandleTimestamps(ctx, database.ListCandleTimestampsParams{
		SymbolID:  symbolID,
		Timeframe: string(tf),
		Ts:        start,
		Ts_2:      end,
	})
	if err != nil {
		return market.GapReport{}, fmt.Errorf("reading stored timestamps: %w", err)
	}
	return market.FindGaps(i.cal, tf, from, to, stamps)
}

func clampInt32(n int64) int32 {
	if n < 0 {
		return 0
	}
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(n)
}

func (i *Ingester) finish(ctx context.Context, runID int64, result ChunkResult) {
	params := database.FinishIngestRunParams{
		ID:       runID,
		Status:   result.Status,
		Bars:     clampInt32(int64(result.Bars)),
		Rejected: clampInt32(int64(result.Rejected)),
	}

	if ms := result.Duration.Milliseconds(); ms >= 0 {
		value := clampInt32(ms)
		params.DurationMs = &value
	}

	if result.Err != nil {
		message := result.Err.Error()
		params.Error = &message

		var apiErr *brapi.APIError
		if errors.As(result.Err, &apiErr) {
			status := clampInt32(int64(apiErr.StatusCode))
			params.HttpStatus = &status
		}
	}

	if err := i.queries.FinishIngestRun(context.WithoutCancel(ctx), params); err != nil {
		i.log.ErrorContext(ctx, "closing ingest run",
			slog.Int64("run_id", runID),
			slog.Any("err", err),
		)
	}
}

func (i *Ingester) logRejections(ctx context.Context, req ChunkRequest, rejected []Rejection) {
	if len(rejected) == 0 {
		return
	}

	sample := rejected
	if len(sample) > rejectionLogSample {
		sample = sample[:rejectionLogSample]
	}

	reasons := make([]string, 0, len(sample))
	for _, rejection := range sample {
		reasons = append(reasons, fmt.Sprintf("%s: %s", rejection.TS.UTC().Format(time.RFC3339), rejection.Reason))
	}

	i.log.WarnContext(ctx, "rejected bars",
		slog.String("ticker", req.Symbol.Ticker),
		slog.String("timeframe", string(req.Timeframe)),
		slog.Int("count", len(rejected)),
		slog.Any("sample", reasons),
	)
}
