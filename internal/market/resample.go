package market

import (
	"errors"
	"fmt"
	"time"
)

var ErrNotResamplable = errors.New("timeframe is stored, not resampled")

func Resample(cal *Calendar, base []Candle, target Timeframe) ([]Candle, error) {
	if cal == nil {
		return nil, errors.New("resample: calendar is required")
	}
	if !target.Valid() {
		return nil, fmt.Errorf("resample: unknown timeframe %q", target)
	}
	if target == TF1d {
		return nil, fmt.Errorf("resample: %w: 1d is read from storage, never folded from 5m", ErrNotResamplable)
	}

	out := make([]Candle, 0, len(base)/max(target.BaseBars(), 1)+1)

	var (
		current Candle
		bucket  time.Time
		open    bool
		prev    time.Time
	)

	flush := func() {
		if open {
			out = append(out, current)
			open = false
		}
	}

	for i, candle := range base {
		if err := candle.Validate(); err != nil {
			return nil, fmt.Errorf("resample: base candle %d (%s): %w", i, candle.TS.UTC().Format(time.RFC3339), err)
		}
		if !prev.IsZero() && !candle.TS.After(prev) {
			return nil, fmt.Errorf("resample: base candles are not strictly ascending at index %d (%s follows %s)",
				i, candle.TS.UTC().Format(time.RFC3339), prev.UTC().Format(time.RFC3339))
		}
		prev = candle.TS

		aligned, err := cal.BucketOpen(TF5m, candle.TS)
		if err != nil {
			return nil, fmt.Errorf("resample: base candle %d: %w", i, err)
		}
		if !aligned.Equal(candle.TS) {
			return nil, fmt.Errorf("resample: base candle %d at %s is not aligned to a 5m bucket (expected %s)",
				i, candle.TS.UTC().Format(time.RFC3339), aligned.UTC().Format(time.RFC3339))
		}

		start, err := cal.BucketOpen(target, candle.TS)
		if err != nil {
			return nil, fmt.Errorf("resample: base candle %d: %w", i, err)
		}

		if !open || !start.Equal(bucket) {
			flush()
			bucket, open = start, true
			current = Candle{TS: start, Open: candle.Open, High: candle.High, Low: candle.Low}
		}

		current.High = max(current.High, candle.High)
		current.Low = min(current.Low, candle.Low)
		current.Close = candle.Close
		current.Volume += candle.Volume
		if candle.AdjClose != nil {
			value := *candle.AdjClose
			current.AdjClose = &value
		}
	}

	flush()
	return out, nil
}
