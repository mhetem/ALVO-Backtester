package market

import (
	"context"
	"fmt"
	"strings"
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

func (s *CandleService) LoadContinuous(ctx context.Context, root string, tf Timeframe, from, to time.Time, opts ContinuousOptions) (Series, error) {
	if tf != TF1d {
		return Series{}, fmt.Errorf("futures are stored daily: %s is not available for %s", tf, root)
	}

	rows, err := s.queries.ListFuturesQuotesByRoot(ctx, database.ListFuturesQuotesByRootParams{
		Root:  strings.ToUpper(strings.TrimSpace(root)),
		Day:   from,
		Day_2: to,
	})
	if err != nil {
		return Series{}, fmt.Errorf("reading %s futures quotes: %w", root, err)
	}

	quotes := make([]FuturesQuote, 0, len(rows))
	for _, row := range rows {
		quotes = append(quotes, FuturesQuote{
			Symbol:     row.Symbol,
			Expiration: row.Expiration,
			Multiplier: row.Multiplier,
			Day:        row.Day,
			Settlement: row.Settlement,
			High:       row.High,
			Low:        row.Low,
			Close:      row.Close,
			Average:    row.Average,
			Volume:     row.Volume,
			Trades:     row.Trades,
		})
	}

	continuous, err := BuildContinuous(root, quotes, opts)
	if err != nil {
		return Series{}, err
	}

	candles := make([]Candle, 0, len(continuous.Bars))
	for _, bar := range continuous.Bars {
		ts := bar.Day
		if session, ok := s.cal.Session(bar.Day); ok {
			ts = session.Open
		}

		candles = append(candles, Candle{
			TS:     ts,
			Open:   bar.Settlement,
			High:   bar.Settlement,
			Low:    bar.Settlement,
			Close:  bar.Settlement,
			Volume: bar.Volume,
		})
	}

	series := Series{
		Timeframe: tf,
		Base:      tf,
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

	return series, nil
}

func (s *CandleService) PageContinuous(ctx context.Context, root string, req PageRequest, opts ContinuousOptions) (Page, error) {
	series, err := s.LoadContinuous(ctx, root, req.Timeframe, req.From, req.To, opts)
	if err != nil {
		return Page{}, err
	}

	candles := series.Candles
	if !req.Before.IsZero() {
		cut := len(candles)
		for i, candle := range candles {
			if !candle.TS.Before(req.Before) {
				cut = i
				break
			}
		}
		candles = candles[:cut]
	}

	limit := req.Limit
	if limit < 1 {
		limit = DefaultPageLimit
	}

	page := Page{
		Timeframe: req.Timeframe,
		Base:      req.Timeframe,
		Start:     req.From,
		End:       req.To,
		Candles:   candles,
	}

	if len(candles) > limit {
		page.Candles = candles[len(candles)-limit:]
		page.HasMore = true
		page.Cursor = page.Candles[0].TS
	}

	return page, nil
}
