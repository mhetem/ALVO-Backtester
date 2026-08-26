package backtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	database "github.com/mhetem/ALVO-Backtester/internal/db"
	"github.com/mhetem/ALVO-Backtester/internal/market"
	"github.com/mhetem/ALVO-Backtester/internal/strategy"
	"github.com/mhetem/ALVO-Backtester/internal/sweep"
)

const (
	MaxBars = market.DefaultCandleLimit

	// IndexTicker is the benchmark every run is measured against. brapi answers it with 401
	// until the token is Pro, so a missing series is reported, never fatal.
	IndexTicker = "^BVSP"

	uniqueViolation = "23505"

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
	borrow  *market.Borrow
	log     *slog.Logger
	workers int
	poll    time.Duration
	nudge   chan struct{}
	wg      sync.WaitGroup
}

func NewRunner(pool *pgxpool.Pool, cal *market.Calendar, rates *market.Rates, borrow *market.Borrow, log *slog.Logger) *Runner {
	workers := Workers()

	return &Runner{
		pool:    pool,
		queries: database.New(pool),
		candles: market.NewCandleService(pool, cal),
		cal:     cal,
		rates:   rates,
		borrow:  borrow,
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

		r.promote(ctx)
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
		written := r.finish(ctx, row, result)
		r.settled(ctx, row)
		return true, written

	case errors.Is(ctx.Err(), context.Canceled):
		return true, r.requeue(ctx, row)

	default:
		r.log.Warn("backtest failed",
			slog.String("run", row.ID.String()),
			slog.Any("err", err),
		)
		failed := r.fail(ctx, row, err)
		r.settled(ctx, row)
		return true, failed
	}
}

// A fold's out-of-sample run cannot be queued until every in-sample run of that fold has
// stopped, so the check runs when a sweep child settles rather than on a timer. The timer
// in sweep() is the safety net for a worker that died between the two.
func (r *Runner) settled(ctx context.Context, row database.BacktestRun) {
	if row.SweepID == nil || ctx.Err() != nil {
		return
	}
	r.promote(ctx)
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

	timeframe, err := market.ParseTimeframe(row.Timeframe)
	if err != nil {
		return Result{}, err
	}

	basket, err := r.basket(ctx, row)
	if err != nil {
		return Result{}, err
	}

	from := r.cal.Date(row.StartDate.Date())
	to := r.cal.Date(row.EndDate.Date())

	instruments := make([]Instrument, 0, len(basket))
	ids := make([]int64, 0, len(basket))

	for _, symbol := range basket {
		if symbol.Future() {
			instrument, err := r.futuresInstrument(ctx, symbol, timeframe, from, to, plan.PrimeBars)
			if err != nil {
				return Result{}, err
			}
			instruments = append(instruments, instrument)
			ids = append(ids, symbol.ID)
			continue
		}

		series, err := r.candles.Load(ctx, symbol.ID, timeframe, from, to, MaxBars)
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
			SymbolID:  symbol.ID,
			Timeframe: timeframe,
			Before:    series.Candles[0].TS,
			Bars:      plan.PrimeBars,
		})
		if err != nil {
			return Result{}, err
		}

		instruments = append(instruments, Instrument{Symbol: symbol, Prime: prime, Candles: series.Candles})
		ids = append(ids, symbol.ID)
	}

	return Run(Request{
		Plan:         plan,
		Instruments:  instruments,
		MaxPositions: int(row.MaxPositions),
		Timeframe:    timeframe,
		Capital:      row.CapitalCents,
		Index:        r.index(ctx, ids, timeframe, from, to),
		IndexSymbol:  IndexTicker,
		Rates:        r.rates,
		Borrow:       r.borrow,
		BarsPerYear:  market.BarsPerYear(r.cal, timeframe),
	})
}

func (r *Runner) basket(ctx context.Context, row database.BacktestRun) ([]Symbol, error) {
	rows, err := r.queries.ListBacktestRunSymbols(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("reading the run's basket: %w", err)
	}

	if len(rows) > 0 {
		basket := make([]Symbol, 0, len(rows))
		for _, held := range rows {
			basket = append(basket, Symbol{
				ID:       held.ID,
				Ticker:   held.Ticker,
				Kind:     held.Kind,
				LotSize:  int64(held.LotSize),
				TickSize: held.TickSize,
			})
		}
		return basket, nil
	}

	primary, err := r.queries.GetSymbolByID(ctx, row.SymbolID)
	if err != nil {
		return nil, fmt.Errorf("reading the run's symbol: %w", err)
	}

	return []Symbol{{
		ID:       primary.ID,
		Ticker:   primary.Ticker,
		Kind:     primary.Kind,
		LotSize:  int64(primary.LotSize),
		TickSize: primary.TickSize,
	}}, nil
}

func (r *Runner) index(ctx context.Context, against []int64, tf market.Timeframe, from, to time.Time) []market.Candle {
	symbol, err := r.queries.GetSymbolByTicker(ctx, IndexTicker)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			r.log.Warn("reading the benchmark symbol", slog.String("ticker", IndexTicker), slog.Any("err", err))
		}
		return nil
	}
	if slices.Contains(against, symbol.ID) {
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
				SymbolID:       trade.SymbolID,
				Side:           trade.Side,
				Qty:            trade.Qty,
				EntryTs:        trade.EntryTS,
				EntryPrice:     trade.EntryPrice,
				ExitTs:         &trade.ExitTS,
				ExitPrice:      &trade.ExitPrice,
				PnlCents:       &trade.PnLCents,
				FeesCents:      trade.FeesCents,
				DividendsCents: trade.DividendsCents,
				BorrowCents:    trade.BorrowCents,
				SplitCashCents: trade.SplitCashCents,
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

func (r *Runner) promote(ctx context.Context) {
	writeCtx, cancel := r.detach(ctx, writeTimeout)
	defer cancel()

	ready, err := r.queries.ReadyWalkForwardFolds(writeCtx)
	if err != nil {
		r.log.Error("looking for folds ready to test out of sample", slog.Any("err", err))
		return
	}

	for _, fold := range ready {
		if fold.SweepID == nil || fold.Fold == nil {
			continue
		}
		if err := r.advance(writeCtx, *fold.SweepID, *fold.Fold); err != nil {
			r.log.Error("queueing a fold's out-of-sample run",
				slog.String("sweep", fold.SweepID.String()),
				slog.Int("fold", int(*fold.Fold)),
				slog.Any("err", err),
			)
		}
	}
}

// The winner of a fold is chosen on the objective the sweep was created with, over the
// in-sample window only. The out-of-sample run it queues sees the next window for the
// first time, which is the whole point of the exercise.
func (r *Runner) advance(ctx context.Context, sweepID uuid.UUID, fold int32) error {
	held, err := r.queries.GetSweepByID(ctx, sweepID)
	if err != nil {
		return fmt.Errorf("reading the sweep: %w", err)
	}

	var plan []sweep.Fold
	if err := json.Unmarshal(held.Folds, &plan); err != nil {
		return fmt.Errorf("reading the sweep's folds: %w", err)
	}

	at := slices.IndexFunc(plan, func(candidate sweep.Fold) bool { return candidate.Fold == int(fold) })
	if at < 0 {
		return fmt.Errorf("this sweep has no fold %d", fold)
	}

	start, end, err := plan[at].Window(sweep.PhaseOutOfSample, r.cal.Location())
	if err != nil {
		return err
	}

	rows, err := r.queries.ListSweepFoldRuns(ctx, database.ListSweepFoldRunsParams{SweepID: &sweepID, Fold: &fold})
	if err != nil {
		return fmt.Errorf("reading the fold's in-sample runs: %w", err)
	}

	winner, ok := bestOf(rows, held.Objective)
	if !ok {
		// Not a failure: every point of this fold's grid finished without opening a
		// position, so there is no winner to carry into the next window. The fold stays
		// unresolved and the report says so rather than inventing one.
		r.log.Info("no in-sample run of this fold scored, so nothing is tested out of sample",
			slog.String("sweep", sweepID.String()),
			slog.Int("fold", int(fold)),
			slog.String("objective", held.Objective),
		)
		return nil
	}

	phase := sweep.PhaseOutOfSample
	created, err := r.queries.CreateBacktestRun(ctx, database.CreateBacktestRunParams{
		UserID:       held.UserID,
		StrategyID:   held.StrategyID,
		Spec:         winner.Spec,
		SymbolID:     held.SymbolID,
		Timeframe:    held.Timeframe,
		StartDate:    start,
		EndDate:      end,
		CapitalCents: held.CapitalCents,
		MaxPositions: held.MaxPositions,
		SweepID:      &sweepID,
		Params:       winner.Params,
		Point:        winner.Point,
		Fold:         &fold,
		Phase:        &phase,
	})
	if err != nil {
		// Two workers can finish a fold's last two in-sample runs at once and both reach
		// here. The partial unique index makes the loser a no-op rather than a duplicate.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return nil
		}
		return fmt.Errorf("queueing the out-of-sample run: %w", err)
	}

	symbols, err := r.queries.ListSweepSymbols(ctx, sweepID)
	if err != nil {
		return fmt.Errorf("reading the sweep's basket: %w", err)
	}

	basket := make([]database.CreateBacktestRunSymbolsParams, 0, len(symbols))
	for _, symbol := range symbols {
		basket = append(basket, database.CreateBacktestRunSymbolsParams{
			RunID:    created.ID,
			Ord:      symbol.Ord,
			SymbolID: symbol.ID,
		})
	}
	if len(basket) > 0 {
		if _, err := r.queries.CreateBacktestRunSymbols(ctx, basket); err != nil {
			return fmt.Errorf("writing the out-of-sample basket: %w", err)
		}
	}

	r.Nudge()

	return nil
}

func bestOf(rows []database.ListSweepFoldRunsRow, objective string) (database.ListSweepFoldRunsRow, bool) {
	var best database.ListSweepFoldRunsRow
	top, found := 0.0, false

	for _, row := range rows {
		var metrics Metrics
		if err := json.Unmarshal(row.Metrics, &metrics); err != nil {
			continue
		}

		score, ok := metrics.Score(objective)
		if !ok {
			continue
		}
		// Strictly greater, over rows already ordered by point, so a tie keeps the first
		// point of the grid and the same sweep re-run picks the same winner.
		if !found || score > top {
			best, top, found = row, score, true
		}
	}

	return best, found
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

func (r *Runner) futuresInstrument(ctx context.Context, symbol Symbol, tf market.Timeframe, from, to time.Time, primeBars int) (Instrument, error) {
	series, err := r.candles.LoadContinuous(ctx, symbol.Ticker, tf, from.AddDate(-2, 0, 0), to, market.ContinuousOptions{BackAdjust: true})
	if err != nil {
		return Instrument{}, err
	}

	prime, candles := splitAt(series.Candles, from, primeBars)

	if len(candles) < 2 {
		return Instrument{}, fmt.Errorf("%s has %d continuous %s bars between %s and %s: a run needs at least two",
			symbol.Ticker, len(candles), tf, from.Format(time.DateOnly), to.Format(time.DateOnly))
	}

	return Instrument{Symbol: symbol, Prime: prime, Candles: candles}, nil
}

func splitAt(candles []market.Candle, from time.Time, primeBars int) ([]market.Candle, []market.Candle) {
	cut := len(candles)
	for i, candle := range candles {
		if !candle.TS.Before(from) {
			cut = i
			break
		}
	}

	prime := candles[:cut]
	if primeBars > 0 && len(prime) > primeBars {
		prime = prime[len(prime)-primeBars:]
	}
	if primeBars <= 0 {
		prime = nil
	}

	return prime, candles[cut:]
}
