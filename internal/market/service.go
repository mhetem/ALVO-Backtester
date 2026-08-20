package market

import (
	"context"
	"fmt"
	"time"

	database "github.com/mhetem/ALVO-Backtester/internal/db"
)

const DefaultCandleLimit = 50000

type CandleService struct {
	queries *database.Queries
	cal     *Calendar
}

func NewCandleService(db database.DBTX, cal *Calendar) *CandleService {
	return &CandleService{queries: database.New(db), cal: cal}
}

type Series struct {
	Timeframe  Timeframe
	Base       Timeframe
	BaseBars   int
	BaseVolume int64
	BaseHigh   float64
	BaseLow    float64
	Candles    []Candle
}

func (s *CandleService) Load(ctx context.Context, symbolID int64, tf Timeframe, from, to time.Time, limit int32) (Series, error) {
	if !tf.Valid() {
		return Series{}, fmt.Errorf("unknown timeframe %q (want one of: %s)", tf, JoinTimeframes(Timeframes))
	}
	if limit < 1 {
		limit = DefaultCandleLimit
	}

	base := tf.Base()
	start, end := s.cal.DayBounds(from, to)

	rows, err := s.queries.ListCandles(ctx, database.ListCandlesParams{
		SymbolID:  symbolID,
		Timeframe: string(base),
		Ts:        start,
		Ts_2:      end,
		Limit:     limit,
	})
	if err != nil {
		return Series{}, fmt.Errorf("reading %s candles: %w", base, err)
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

	series := Series{
		Timeframe: tf,
		Base:      base,
		BaseBars:  len(candles),
		Candles:   candles,
	}
	for i, candle := range candles {
		series.BaseVolume += candle.Volume
		if i == 0 || candle.High > series.BaseHigh {
			series.BaseHigh = candle.High
		}
		if i == 0 || candle.Low < series.BaseLow {
			series.BaseLow = candle.Low
		}
	}

	if tf == base {
		return series, nil
	}

	resampled, err := Resample(s.cal, candles, tf)
	if err != nil {
		return Series{}, err
	}
	series.Candles = resampled

	return series, nil
}
