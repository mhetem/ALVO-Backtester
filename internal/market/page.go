package market

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	database "github.com/mhetem/ALVO-Backtester/internal/db"
)

const (
	DefaultPageLimit = 1000
	MaxPageLimit     = 5000
)

type PageRequest struct {
	SymbolID  int64
	Timeframe Timeframe
	From      time.Time
	To        time.Time
	Before    time.Time
	Limit     int
}

type Page struct {
	Timeframe Timeframe
	Base      Timeframe
	Start     time.Time
	End       time.Time
	Candles   []Candle
	Cursor    time.Time
	HasMore   bool
}

func (p Page) Oldest() time.Time {
	if len(p.Candles) == 0 {
		return time.Time{}
	}
	return p.Candles[0].TS
}

func ClampPageLimit(limit int) int {
	switch {
	case limit < 1:
		return DefaultPageLimit
	case limit > MaxPageLimit:
		return MaxPageLimit
	default:
		return limit
	}
}

func (s *CandleService) Page(ctx context.Context, req PageRequest) (Page, error) {
	tf := req.Timeframe
	if !tf.Valid() {
		return Page{}, fmt.Errorf("unknown timeframe %q (want one of: %s)", tf, JoinTimeframes(Timeframes))
	}

	limit := ClampPageLimit(req.Limit)
	base := tf.Base()
	start, end := s.cal.DayBounds(req.From, req.To)
	if !req.Before.IsZero() && req.Before.UTC().Before(end) {
		end = req.Before.UTC()
	}

	page := Page{Timeframe: tf, Base: base, Start: start, End: end, Candles: []Candle{}}
	if !end.After(start) {
		return page, nil
	}

	rowCap := baseRowCap(tf, limit)
	rows, err := s.queries.ListCandlesDesc(ctx, database.ListCandlesDescParams{
		SymbolID:  req.SymbolID,
		Timeframe: string(base),
		Ts:        start,
		Ts_2:      end,
		Limit:     rowCap,
	})
	if err != nil {
		return Page{}, fmt.Errorf("reading %s candles: %w", base, err)
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
			return Page{}, err
		}
	}

	trimPage(&page, folded, limit, len(rows) == int(rowCap))
	if page.HasMore && page.Cursor.IsZero() && len(candles) > 0 {
		page.Cursor = candles[0].TS
	}

	return page, nil
}

func baseRowCap(tf Timeframe, limit int) int32 {
	rows := limit + 1
	if tf != tf.Base() {
		rows = limit*tf.BaseBars() + 1
	}
	if rows < 1 || rows > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(rows)
}

func trimPage(page *Page, candles []Candle, limit int, truncated bool) {
	if truncated && len(candles) > 0 {
		candles = candles[1:]
		page.HasMore = true
	}
	if len(candles) > limit {
		candles = candles[len(candles)-limit:]
		page.HasMore = true
	}

	page.Candles = candles
	if page.Candles == nil {
		page.Candles = []Candle{}
	}
	if page.HasMore && len(page.Candles) > 0 {
		page.Cursor = page.Candles[0].TS
	}
}
