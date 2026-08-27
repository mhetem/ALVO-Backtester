package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mhetem/ALVO-Backtester/internal/market"
)

const (
	DefaultFillAt     = 20 * time.Hour
	DefaultRunTimeout = time.Hour

	maxDayLookahead = 30
	retryAfter      = time.Hour
)

type ScheduleOptions struct {
	Intraday   bool
	Futures    bool
	FillAt     time.Duration
	RunTimeout time.Duration
	Sessions   int
}

type Scheduler struct {
	ingester *Ingester
	futures  *FuturesIngester
	log      *slog.Logger
	opts     ScheduleOptions

	// The candle sync guards against a repeat run through ingest_runs because it costs ~300
	// requests. The futures tail costs one per root, so an in-memory mark is proportionate:
	// the worst a restart can do is spend four requests on an upsert that changes nothing.
	tailedFor time.Time
}

func NewScheduler(ingester *Ingester, futures *FuturesIngester, log *slog.Logger, opts ScheduleOptions) *Scheduler {
	if opts.FillAt <= 0 || opts.FillAt >= 24*time.Hour {
		opts.FillAt = DefaultFillAt
	}
	if opts.RunTimeout <= 0 {
		opts.RunTimeout = DefaultRunTimeout
	}
	if opts.Sessions < 1 {
		opts.Sessions = DefaultSyncSessions
	}

	return &Scheduler{ingester: ingester, futures: futures, log: log, opts: opts}
}

func (s *Scheduler) Run(ctx context.Context) {
	s.log.InfoContext(ctx, "ingest scheduler started",
		slog.Bool("intraday", s.opts.Intraday),
		slog.Bool("futures", s.opts.Futures),
		slog.String("fill_at", Clock(s.opts.FillAt)),
		slog.String("timezone", s.ingester.cal.Location().String()),
	)

	// A process that comes up after the day's fill time still gets that day: the pass is
	// guarded by ingest_runs, so catching up costs nothing once it has already run.
	s.tick(ctx)

	for {
		next, wait := s.sleep(time.Now())
		s.log.InfoContext(ctx, "next candle fill scheduled",
			slog.Time("at", next),
			slog.Duration("in", wait),
		)

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			s.log.InfoContext(ctx, "ingest scheduler stopped")
			return
		case <-timer.C:
			s.tick(ctx)
		}
	}
}

func (s *Scheduler) sleep(now time.Time) (time.Time, time.Duration) {
	next, ok := s.NextRun(now)
	if !ok {
		return now.Add(retryAfter), retryAfter
	}
	return next, next.Sub(now)
}

// The trigger is a wall-clock time on a trading day, so a weekend or a holiday moves it on
// to the next session instead of burning a pass on a day with no candles to fetch.
func (s *Scheduler) NextRun(now time.Time) (time.Time, bool) {
	cal := s.ingester.cal

	day := now.In(cal.Location())
	for range maxDayLookahead {
		if due, ok := s.DueAt(day); ok && due.After(now) {
			return due, true
		}
		day = day.AddDate(0, 0, 1)
	}

	return time.Time{}, false
}

func (s *Scheduler) tick(ctx context.Context) {
	now := time.Now()

	due, ok := s.DueAt(now)
	if !ok || now.Before(due) {
		return
	}

	for _, tf := range s.timeframes() {
		ran, err := s.alreadySynced(ctx, tf, due)
		if err != nil {
			s.log.ErrorContext(ctx, "scheduler could not read ingest history",
				slog.String("timeframe", string(tf)),
				slog.Any("err", err),
			)
			return
		}
		if ran {
			continue
		}

		s.sync(ctx, tf, due)

		if ctx.Err() != nil {
			return
		}
	}

	s.syncFutures(ctx, due)
}

// The futures tail is skipped silently when nothing has been backfilled: SyncTail reads the
// roots already in the store and spends no requests on an empty one.
func (s *Scheduler) syncFutures(ctx context.Context, due time.Time) {
	if !s.opts.Futures || s.futures == nil || !s.tailedFor.Before(due) {
		return
	}

	runCtx, cancel := context.WithTimeout(ctx, s.opts.RunTimeout)
	defer cancel()

	started := time.Now()
	report, err := s.futures.SyncTail(runCtx, nil)
	if err == nil {
		s.tailedFor = due
	}
	if err != nil {
		s.log.ErrorContext(ctx, "scheduled futures tail failed",
			slog.Time("due", due),
			slog.Int("requests", report.Requests),
			slog.Any("err", err),
		)
		return
	}
	if report.Requests == 0 {
		return
	}

	s.log.InfoContext(ctx, "scheduled futures tail finished",
		slog.Time("due", due),
		slog.String("roots", strings.Join(report.Roots, ",")),
		slog.Int("requests", report.Requests),
		slog.Int("contracts", report.Contracts),
		slog.Int("bars", report.Bars),
		slog.String("day", report.Day.Format(time.DateOnly)),
		slog.Int("failures", len(report.Failures)),
		slog.Duration("took", time.Since(started)),
	)
}

// brapi keeps revising the day's closing candle for hours after the bell, so the single
// pass is pinned to an hour of the evening rather than to an offset from the close. The
// clamp keeps a misconfigured early hour, or a short holiday session, from firing while the
// market is still open.
func (s *Scheduler) DueAt(now time.Time) (time.Time, bool) {
	cal := s.ingester.cal

	session, ok := cal.Session(now.In(cal.Location()))
	if !ok {
		return time.Time{}, false
	}

	due := atClock(session.Day, s.opts.FillAt, cal.Location())
	if due.Before(session.Close) {
		return session.Close, true
	}

	return due, true
}

func atClock(day time.Time, offset time.Duration, loc *time.Location) time.Time {
	minutes := int(offset / time.Minute)
	return time.Date(day.Year(), day.Month(), day.Day(), minutes/60, minutes%60, 0, 0, loc)
}

func Clock(offset time.Duration) string {
	minutes := int(offset / time.Minute)
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

func (s *Scheduler) timeframes() []market.Timeframe {
	if s.opts.Intraday {
		return []market.Timeframe{market.TF1d, market.TF5m}
	}
	return []market.Timeframe{market.TF1d}
}

func (s *Scheduler) alreadySynced(ctx context.Context, tf market.Timeframe, due time.Time) (bool, error) {
	latest, err := s.ingester.queries.LatestSyncRunAt(ctx, string(tf))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading the latest %s ingest run: %w", tf, err)
	}
	return !latest.Before(due), nil
}

func (s *Scheduler) sync(ctx context.Context, tf market.Timeframe, due time.Time) {
	symbols, err := s.ingester.TrackedSymbols(ctx)
	if err != nil {
		s.log.ErrorContext(ctx, "scheduled sync could not resolve the universe", slog.Any("err", err))
		return
	}
	if len(symbols) == 0 {
		s.log.WarnContext(ctx, "scheduled sync found no tracked symbols")
		return
	}

	runCtx, cancel := context.WithTimeout(ctx, s.opts.RunTimeout)
	defer cancel()

	started := time.Now()
	report, err := s.ingester.Sync(runCtx, symbols, SyncOptions{
		Timeframe: tf,
		Sessions:  s.opts.Sessions,
	})
	if err != nil {
		s.log.ErrorContext(ctx, "scheduled sync failed",
			slog.String("timeframe", string(tf)),
			slog.Time("due", due),
			slog.Int("requests", report.Requests),
			slog.Any("err", err),
		)
		return
	}

	s.log.InfoContext(ctx, "scheduled sync finished",
		slog.String("timeframe", string(tf)),
		slog.Time("due", due),
		slog.Int("symbols", report.Symbols),
		slog.Int("requests", report.Requests),
		slog.Int("bars", report.Bars),
		slog.Int("rejected", report.Rejected),
		slog.Int("empty", report.Empty),
		slog.Int("unreachable", len(report.Unreachable)),
		slog.Int("failures", len(report.Failures)),
		slog.Duration("took", time.Since(started)),
	)
}
