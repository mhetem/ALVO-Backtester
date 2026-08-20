package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	database "github.com/mhetem/ALVO-Backtester/internal/db"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

const (
	DefaultSyncSessions = 5
	IntradaySyncRange   = "3mo"
)

type SyncOptions struct {
	Timeframe market.Timeframe
	Sessions  int
	DryRun    bool
}

type SyncReport struct {
	Timeframe   market.Timeframe
	From        time.Time
	To          time.Time
	Symbols     int
	Requests    int
	Bars        int
	Rejected    int
	Empty       int
	Unreachable []string
	Failures    []ChunkResult
	DryRun      bool
}

func (i *Ingester) Sync(ctx context.Context, symbols []database.Symbol, opts SyncOptions) (SyncReport, error) {
	if !opts.Timeframe.Stored() {
		return SyncReport{}, fmt.Errorf("cannot sync %s: only %s are stored", opts.Timeframe, market.JoinTimeframes(market.StoredTimeframes))
	}
	if opts.Sessions < 1 {
		opts.Sessions = DefaultSyncSessions
	}

	from, to, err := i.RefreshWindow(time.Now(), opts.Sessions)
	if err != nil {
		return SyncReport{}, err
	}

	report := SyncReport{
		Timeframe:   opts.Timeframe,
		From:        from,
		To:          to,
		Symbols:     len(symbols),
		Unreachable: []string{},
		Failures:    []ChunkResult{},
		DryRun:      opts.DryRun,
	}

	keepFrom, keepTo := i.cal.DayBounds(from, to)

	for _, symbol := range symbols {
		if !i.Reachable(symbol.Ticker) {
			report.Unreachable = append(report.Unreachable, symbol.Ticker)
			continue
		}
		if opts.DryRun {
			report.Requests++
			continue
		}

		req := ChunkRequest{
			Symbol:    symbol,
			Timeframe: opts.Timeframe,
			From:      from,
			To:        to,
		}
		if opts.Timeframe == market.TF5m {
			req.Range = IntradaySyncRange
			req.KeepFrom, req.KeepTo = keepFrom, keepTo
		}

		result := i.Chunk(ctx, req)
		report.Requests++
		report.Bars += result.Bars
		report.Rejected += result.Rejected

		switch result.Status {
		case StatusEmpty:
			report.Empty++
		case StatusFailed:
			report.Failures = append(report.Failures, result)
			i.log.ErrorContext(ctx, "sync chunk failed",
				slog.String("ticker", symbol.Ticker),
				slog.String("timeframe", string(opts.Timeframe)),
				slog.Any("err", result.Err),
			)
		default:
			i.log.InfoContext(ctx, "sync chunk stored",
				slog.String("ticker", symbol.Ticker),
				slog.String("timeframe", string(opts.Timeframe)),
				slog.Int("bars", result.Bars),
				slog.Duration("took", result.Duration),
			)
		}

		if ctx.Err() != nil {
			return report, ctx.Err()
		}
	}

	return report, nil
}

func (i *Ingester) RefreshWindow(now time.Time, sessions int) (time.Time, time.Time, error) {
	if sessions < 1 {
		sessions = 1
	}

	end := i.cal.Date(now.In(i.cal.Location()).Date())
	if !i.cal.IsTradingDay(end) {
		previous, err := i.cal.PrevTradingDay(end)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		end = previous
	}

	start := end
	for range sessions - 1 {
		previous, err := i.cal.PrevTradingDay(start)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		start = previous
	}

	return start, end, nil
}
