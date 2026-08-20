package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	database "github.com/mhetem/ALVO-Backtester/internal/db"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

const (
	DefaultDailyChunkDays = 365
	MinIntradayRange      = "3mo"
	MaxIntradayRange      = "max"
)

var intradayRanges = []struct {
	Token string
	Days  int
}{
	{"3mo", 92},
	{"6mo", 183},
	{"1y", 366},
	{"2y", 731},
	{"5y", 1827},
	{"10y", 3653},
}

func IntradayRange(from, now time.Time) string {
	days := int(now.Sub(from).Hours()/24) + 1
	for _, candidate := range intradayRanges {
		if days <= candidate.Days {
			return candidate.Token
		}
	}
	return MaxIntradayRange
}

func ValidateIntradayRange(token string) error {
	if token == MaxIntradayRange {
		return nil
	}
	for _, candidate := range intradayRanges {
		if candidate.Token == token {
			return nil
		}
	}

	accepted := []string{}
	for _, candidate := range intradayRanges {
		accepted = append(accepted, candidate.Token)
	}
	accepted = append(accepted, MaxIntradayRange)

	return fmt.Errorf("range %q is not usable for 5m: brapi returns a different and incomplete intraday series below %s (want one of: %s)",
		token, MinIntradayRange, strings.Join(accepted, ", "))
}

type BackfillOptions struct {
	Timeframe market.Timeframe
	From      time.Time
	To        time.Time
	ChunkDays int
	Range     string
	DryRun    bool
	Force     bool
}

type BackfillReport struct {
	Timeframe   market.Timeframe
	From        time.Time
	To          time.Time
	Symbols     int
	Range       string
	Chunks      int
	Requests    int
	Skipped     int
	Bars        int
	Rejected    int
	Empty       int
	Unreachable []string
	Failures    []ChunkResult
	DryRun      bool
}

func (i *Ingester) Backfill(ctx context.Context, symbols []database.Symbol, opts BackfillOptions) (BackfillReport, error) {
	if !opts.Timeframe.Stored() {
		return BackfillReport{}, fmt.Errorf("cannot backfill %s: only %s are stored", opts.Timeframe, market.JoinTimeframes(market.StoredTimeframes))
	}
	if opts.To.Before(opts.From) {
		return BackfillReport{}, fmt.Errorf("range end %s is before start %s", opts.To.Format(time.DateOnly), opts.From.Format(time.DateOnly))
	}
	if opts.ChunkDays < 1 {
		opts.ChunkDays = DefaultDailyChunkDays
	}

	rangeToken := opts.Range
	if opts.Timeframe == market.TF5m && rangeToken == "" {
		rangeToken = IntradayRange(opts.From, time.Now())
	}
	if rangeToken != "" && opts.Timeframe == market.TF5m {
		if err := ValidateIntradayRange(rangeToken); err != nil {
			return BackfillReport{}, err
		}
	}

	report := BackfillReport{
		Timeframe:   opts.Timeframe,
		From:        opts.From,
		To:          opts.To,
		Symbols:     len(symbols),
		Range:       rangeToken,
		Unreachable: []string{},
		Failures:    []ChunkResult{},
		DryRun:      opts.DryRun,
	}

	windows := chunkWindows(opts.From, opts.To, opts.ChunkDays)
	keepFrom, keepTo := time.Time{}, time.Time{}
	if rangeToken != "" {
		windows = [][2]time.Time{{opts.From, opts.To}}
		keepFrom, keepTo = i.cal.DayBounds(opts.From, opts.To)
	}

	for _, symbol := range symbols {
		if !i.Reachable(symbol.Ticker) {
			report.Unreachable = append(report.Unreachable, symbol.Ticker)
			continue
		}

		for _, window := range windows {
			report.Chunks++

			if !opts.Force {
				skip, err := i.covered(ctx, symbol.ID, opts.Timeframe, window[0], window[1])
				if err != nil {
					return report, err
				}
				if skip {
					report.Skipped++
					continue
				}
			}

			if opts.DryRun {
				report.Requests++
				continue
			}

			result := i.Chunk(ctx, ChunkRequest{
				Symbol:    symbol,
				Timeframe: opts.Timeframe,
				From:      window[0],
				To:        window[1],
				Range:     rangeToken,
				KeepFrom:  keepFrom,
				KeepTo:    keepTo,
			})
			report.Requests++
			report.Bars += result.Bars
			report.Rejected += result.Rejected

			switch result.Status {
			case StatusEmpty:
				report.Empty++
			case StatusFailed:
				report.Failures = append(report.Failures, result)
				i.log.ErrorContext(ctx, "backfill chunk failed",
					slog.String("ticker", symbol.Ticker),
					slog.String("timeframe", string(opts.Timeframe)),
					slog.String("from", window[0].Format(time.DateOnly)),
					slog.String("to", window[1].Format(time.DateOnly)),
					slog.Any("err", result.Err),
				)
			default:
				i.log.InfoContext(ctx, "backfill chunk stored",
					slog.String("ticker", symbol.Ticker),
					slog.String("timeframe", string(opts.Timeframe)),
					slog.String("from", window[0].Format(time.DateOnly)),
					slog.String("to", window[1].Format(time.DateOnly)),
					slog.Int("bars", result.Bars),
					slog.Int("rejected", result.Rejected),
					slog.Duration("took", result.Duration),
				)
			}

			if ctx.Err() != nil {
				return report, ctx.Err()
			}
		}
	}

	return report, nil
}

func (i *Ingester) covered(ctx context.Context, symbolID int64, tf market.Timeframe, from, to time.Time) (bool, error) {
	gaps, err := i.Coverage(ctx, symbolID, tf, from, to)
	if err != nil {
		return false, err
	}
	if gaps.Clean() {
		return true, nil
	}

	today := i.cal.Date(time.Now().In(i.cal.Location()).Date())
	if !to.Before(today) {
		return false, nil
	}

	completed, err := i.queries.CountCompletedIngestRuns(ctx, database.CountCompletedIngestRunsParams{
		SymbolID:   symbolID,
		Timeframe:  string(tf),
		RangeStart: from,
		RangeEnd:   to,
	})
	if err != nil {
		return false, fmt.Errorf("reading ingest history: %w", err)
	}

	return completed > 0, nil
}

func chunkWindows(from, to time.Time, days int) [][2]time.Time {
	windows := [][2]time.Time{}

	for start := from; !start.After(to); start = start.AddDate(0, 0, days) {
		end := start.AddDate(0, 0, days-1)
		if end.After(to) {
			end = to
		}
		windows = append(windows, [2]time.Time{start, end})
	}

	return windows
}
