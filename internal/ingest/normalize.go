package ingest

import (
	"cmp"
	"slices"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/brapi"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

type Rejection struct {
	TS     time.Time
	Reason string
}

func Normalize(cal *market.Calendar, tf market.Timeframe, bars []brapi.Bar) ([]market.Candle, []Rejection) {
	sorted := slices.Clone(bars)
	slices.SortStableFunc(sorted, func(a, b brapi.Bar) int { return cmp.Compare(a.Date, b.Date) })

	folded := map[int64]*market.Candle{}
	order := []int64{}
	rejected := []Rejection{}

	for _, bar := range sorted {
		ts := bar.TS()

		bucket, err := cal.BucketOpen(tf, ts)
		if err != nil {
			rejected = append(rejected, Rejection{TS: ts, Reason: err.Error()})
			continue
		}

		candle := market.Candle{
			TS:     bucket,
			Open:   bar.Open,
			High:   bar.High,
			Low:    bar.Low,
			Close:  bar.Close,
			Volume: bar.Volume,
		}
		if bar.AdjustedClose != nil && *bar.AdjustedClose > 0 {
			value := *bar.AdjustedClose
			candle.AdjClose = &value
		}

		if err := candle.Validate(); err != nil {
			rejected = append(rejected, Rejection{TS: ts, Reason: err.Error()})
			continue
		}

		key := bucket.Unix()
		current, seen := folded[key]
		if !seen {
			copied := candle
			folded[key] = &copied
			order = append(order, key)
			continue
		}

		current.High = max(current.High, candle.High)
		current.Low = min(current.Low, candle.Low)
		current.Close = candle.Close
		current.Volume += candle.Volume
		if candle.AdjClose != nil {
			current.AdjClose = candle.AdjClose
		}
	}

	slices.Sort(order)
	candles := make([]market.Candle, 0, len(order))
	for _, key := range order {
		candles = append(candles, *folded[key])
	}

	return candles, rejected
}
