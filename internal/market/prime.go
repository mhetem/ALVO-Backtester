package market

import (
	"context"
	"fmt"
	"slices"
	"time"

	database "github.com/mhetem/ALVO-Backtester/internal/db"
)

const MaxPrimeBars = 5000

var beginningOfTime time.Time

type PrimeRequest struct {
	SymbolID  int64
	Timeframe Timeframe
	Before    time.Time
	Bars      int
}

func (s *CandleService) Prime(ctx context.Context, req PrimeRequest) ([]Candle, error) {
	tf := req.Timeframe
	if !tf.Valid() {
		return nil, fmt.Errorf("unknown timeframe %q (want one of: %s)", tf, JoinTimeframes(Timeframes))
	}
	if req.Bars < 1 || req.Before.IsZero() {
		return nil, nil
	}

	bars := min(req.Bars, MaxPrimeBars)
	base := tf.Base()
	rowCap := baseRowCap(tf, bars)

	rows, err := s.queries.ListCandlesDesc(ctx, database.ListCandlesDescParams{
		SymbolID:  req.SymbolID,
		Timeframe: string(base),
		Ts:        beginningOfTime,
		Ts_2:      req.Before.UTC(),
		Limit:     rowCap,
	})
	if err != nil {
		return nil, fmt.Errorf("reading %s candles: %w", base, err)
	}

	candles := make([]Candle, 0, len(rows))
	for _, row := range rows {
		candles = append(candles, Candle{
			TS:       row.Ts,
			Open:     row.Open,
			High:     row.High,
			Low:      row.Low,
			Close:    row.Close,
			AdjClose: row.AdjClose,
			Volume:   row.Volume,
		})
	}
	slices.Reverse(candles)

	folded := candles
	if tf != base {
		if folded, err = Resample(s.cal, candles, tf); err != nil {
			return nil, err
		}
	}

	var page Page
	trimPage(&page, folded, bars, len(rows) == int(rowCap))

	return page.Candles, nil
}
