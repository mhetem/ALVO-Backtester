package backtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	database "github.com/mhetem/ALVO-Backtester/internal/db"
	"github.com/mhetem/ALVO-Backtester/internal/market"
	"github.com/mhetem/ALVO-Backtester/internal/strategy"
)

const (
	MaxBars = market.DefaultCandleLimit

	// IndexTicker is the benchmark every run is measured against. brapi answers it with 401
	// until the token is Pro, so a missing series is reported, never fatal.
	IndexTicker = "^BVSP"

	pollInterval  = 2 * time.Second
	sweepInterval = 5 * time.Minute
	runTimeout    = 10 * time.Minute
	staleAfter    = 30 * time.Minute
	writeTimeout  = 60 * time.Second
	maxErrorLen   = 500
)

type Runner struct {
	pool    *pgxpool.Pool
	queries *database.Queries
	candles *market.CandleService
	cal     *market.Calendar
	rates   *market.Rates
	log     *slog.Logger
	workers int
	poll    time.Duration
	nudge   chan struct{}
	wg      sync.WaitGroup
}

func NewRunner(pool *pgxpool.Pool, cal *market.Calendar, rates *market.Rates, log *slog.Logger) *Runner {
	workers := Workers()

	return &Runner{
		pool:    pool,
		queries: database.New(pool),
		candles: market.NewCandleService(pool, cal),
		cal:     cal,
		rates:   rates,
		log:     log,
		workers: workers,
		poll:    pollInterval,
		nudge:   make(chan struct{}, workers),
	}
}

func Workers() int {
	return max(runtime.GOMAXPROCS(0)-1, 1)
}

func (r *Runner) Start(ctx context.Context) {
	r.log.Info("backtest workers ready", slog.Int("workers", r.workers))

	for i := range r.workers {
		r.wg.Add(1)
		go r.work(ctx, i)
	}

	r.wg.Add(1)
	go r.sweep(ctx)
}

func (r *Runner) Wait() { r.wg.Wait() }

func (r *Runner) Nudge() {
	select {
	case r.nudge <- struct{}{}:
	default:
	}
}

func (r *Runner) work(ctx context.Context, worker int) {
	defer r.wg.Done()

	ticker := time.NewTicker(r.poll)
	defer ticker.Stop()

	for {
		claimed, err := r.step(ctx)
		if err != nil {
			r.log.Error("draining the backtest queue",
				slog.Int("worker", worker),
				slog.Any("err", err),
			)
		}
		if claimed && ctx.Err() == nil {
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-r.nudge:
		case <-ticker.C:
		}
	}
}

func (r *Runner) sweep(ctx context.Context) {
	defer r.wg.Done()

	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		requeued, err := r.queries.RequeueStaleBacktestRuns(ctx, time.Now().Add(-staleAfter))
		switch {
		case err != nil && ctx.Err() == nil:
			r.log.Error("requeueing abandoned backtest runs", slog.Any("err", err))
		case requeued > 0:
			r.log.Warn("requeued abandoned backtest runs", slog.Int64("runs", requeued))
		}
	}
}

func (r *Runner) step(ctx context.Context) (bool, error) {
	row, err := r.queries.ClaimBacktestRun(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || ctx.Err() != nil {
			return false, nil
		}
		return false, err
	}

	started := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	result, err := r.execute(runCtx, row)
	switch {
	case err == nil:
		r.log.Info("backtest done",
			slog.String("run", row.ID.String()),
			slog.Int("bars", result.Metrics.Bars),
			slog.Int("trades", result.Metrics.Trades),
			slog.Duration("took", time.Since(started)),
		)
		return true, r.finish(ctx, row, result)

	case errors.Is(ctx.Err(), context.Canceled):
		return true, r.requeue(ctx, row)

	default:
		r.log.Warn("backtest failed",
			slog.String("run", row.ID.String()),
			slog.Any("err", err),
		)
		return true, r.fail(ctx, row, err)
	}
}

func (r *Runner) execute(ctx context.Context, row database.BacktestRun) (Result, error) {
	spec, err := strategy.Parse(row.Spec)
	if err != nil {
		return Result{}, fmt.Errorf("reading the run's spec: %w", err)
	}

	plan, err := strategy.Compile(spec)
	if err != nil {
		return Result{}, fmt.Errorf("compiling the run's spec: %w", err)
	}

	symbol, err := r.queries.GetSymbolByID(ctx, row.SymbolID)
	if err != nil {
		return Result{}, fmt.Errorf("reading the run's symbol: %w", err)
	}

	timeframe, err := market.ParseTimeframe(row.Timeframe)
	if err != nil {
		return Result{}, err
	}

	from := r.cal.Date(row.StartDate.Date())
	to := r.cal.Date(row.EndDate.Date())

	series, err := r.candles.Load(ctx, row.SymbolID, timeframe, from, to, MaxBars)
	if err != nil {
		return Result{}, err
	}
	if series.BaseBars >= MaxBars {
		return Result{}, fmt.Errorf("%s %s holds more than %d bars between %s and %s: narrow the range or use a wider timeframe",
			symbol.Ticker, timeframe, MaxBars, row.StartDate.Format(time.DateOnly), row.EndDate.Format(time.DateOnly))
	}
	if len(series.Candles) < 2 {
		return Result{}, fmt.Errorf("%s has %d %s candles between %s and %s: a run needs at least two",
			symbol.Ticker, len(series.Candles), timeframe, row.StartDate.Format(time.DateOnly), row.EndDate.Format(time.DateOnly))
	}

	prime, err := r.candles.Prime(ctx, market.PrimeRequest{
		SymbolID:  row.SymbolID,
		Timeframe: timeframe,
		Before:    series.Candles[0].TS,
		Bars:      plan.PrimeBars,
	})
	if err != nil {
		return Result{}, err
	}

	return Run(Request{
		Plan:        plan,
		Symbol:      Symbol{Ticker: symbol.Ticker, LotSize: int64(symbol.LotSize), TickSize: symbol.TickSize},
		Timeframe:   timeframe,
		Capital:     row.CapitalCents,
		Prime:       prime,
		Candles:     series.Candles,
		Index:       r.index(ctx, symbol.ID, timeframe, from, to),
		IndexSymbol: IndexTicker,
		Rates:       r.rates,
		BarsPerYear: market.BarsPerYear(r.cal, timeframe),
	})
}

func (r *Runner) index(ctx context.Context, against int64, tf market.Timeframe, from, to time.Time) []market.Candle {
	symbol, err := r.queries.GetSymbolByTicker(ctx, IndexTicker)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			r.log.Warn("reading the benchmark symbol", slog.String("ticker", IndexTicker), slog.Any("err", err))
		}
		return nil
	}
	if symbol.ID == against {
		return nil
	}

	series, err := r.candles.Load(ctx, symbol.ID, tf, from, to, MaxBars)
	if err != nil {
		r.log.Warn("reading benchmark candles", slog.String("ticker", IndexTicker), slog.Any("err", err))
		return nil
	}

	return series.Candles
}

func (r *Runner) finish(ctx context.Context, row database.BacktestRun, result Result) error {
	metrics, err := json.Marshal(result.Metrics)
	if err != nil {
		return fmt.Errorf("encoding run metrics: %w", err)
	}

	writeCtx, cancel := r.detach(ctx, writeTimeout)
	defer cancel()

	tx, err := r.pool.Begin(writeCtx)
	if err != nil {
		return fmt.Errorf("beginning the result write: %w", err)
	}
	defer func() { _ = tx.Rollback(writeCtx) }()

	inTx := r.queries.WithTx(tx)

	if len(result.Trades) > 0 {
		trades := make([]database.CreateBacktestTradesParams, 0, len(result.Trades))
		for _, trade := range result.Trades {
			trades = append(trades, database.CreateBacktestTradesParams{
				RunID:          row.ID,
				Seq:            trade.Seq,
				Side:           trade.Side,
				Qty:            trade.Qty,
				EntryTs:        trade.EntryTS,
				EntryPrice:     trade.EntryPrice,
				ExitTs:         &trade.ExitTS,
				ExitPrice:      &trade.ExitPrice,
				PnlCents:       &trade.PnLCents,
				FeesCents:      trade.FeesCents,
				DividendsCents: trade.DividendsCents,
				ExitReason:     &trade.ExitReason,
			})
		}
		if _, err := inTx.CreateBacktestTrades(writeCtx, trades); err != nil {
			return fmt.Errorf("writing run trades: %w", err)
		}
	}

	if len(result.Equity) > 0 {
		equity := make([]database.CreateBacktestEquityParams, 0, len(result.Equity))
		for i, point := range result.Equity {
			equity = append(equity, database.CreateBacktestEquityParams{
				RunID:       row.ID,
				Ts:          point.TS,
				EquityCents: point.Cents,
				HoldCents:   curveAt(result.Hold, i),
				IndexCents:  curveAt(result.Index, i),
			})
		}
		if _, err := inTx.CreateBacktestEquity(writeCtx, equity); err != nil {
			return fmt.Errorf("writing the equity curve: %w", err)
		}
	}

	if err := inTx.FinishBacktestRun(writeCtx, database.FinishBacktestRunParams{ID: row.ID, Metrics: metrics}); err != nil {
		return fmt.Errorf("marking the run done: %w", err)
	}

	return tx.Commit(writeCtx)
}

func (r *Runner) fail(ctx context.Context, row database.BacktestRun, cause error) error {
	writeCtx, cancel := r.detach(ctx, writeTimeout)
	defer cancel()

	message := cause.Error()
	if runes := []rune(message); len(runes) > maxErrorLen {
		message = string(runes[:maxErrorLen])
	}

	return r.queries.FailBacktestRun(writeCtx, database.FailBacktestRunParams{ID: row.ID, Error: &message})
}

func (r *Runner) requeue(ctx context.Context, row database.BacktestRun) error {
	writeCtx, cancel := r.detach(ctx, writeTimeout)
	defer cancel()

	r.log.Info("returning an interrupted backtest to the queue", slog.String("run", row.ID.String()))

	return r.queries.RequeueBacktestRun(writeCtx, row.ID)
}

func (r *Runner) detach(ctx context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), limit)
}

func curveAt(curve []int64, i int) *int64 {
	if i < 0 || i >= len(curve) {
		return nil
	}
	return &curve[i]
}
