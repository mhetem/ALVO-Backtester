package backtest

import (
	"errors"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/indicator"
	"github.com/mhetem/ALVO-Backtester/internal/market"
	"github.com/mhetem/ALVO-Backtester/internal/strategy"
)

const (
	SideLong = "long"

	ReasonSignal   = "signal"
	ReasonStop     = "stop"
	ReasonTarget   = "target"
	ReasonEndOfRun = "end_of_run"
)

type Symbol struct {
	Ticker   string
	LotSize  int64
	TickSize float64
}

type Request struct {
	Plan      *strategy.Plan
	Symbol    Symbol
	Timeframe market.Timeframe
	Capital   int64
	Prime     []market.Candle
	Candles   []market.Candle
}

type Trade struct {
	Seq        int32     `json:"seq"`
	Side       string    `json:"side"`
	Qty        int64     `json:"qty"`
	EntryTS    time.Time `json:"entry_ts"`
	EntryPrice float64   `json:"entry_price"`
	ExitTS     time.Time `json:"exit_ts"`
	ExitPrice  float64   `json:"exit_price"`
	PnLCents   int64     `json:"pnl_cents"`
	FeesCents  int64     `json:"fees_cents"`
	ExitReason string    `json:"exit_reason"`
}

type EquityPoint struct {
	TS    time.Time `json:"ts"`
	Cents int64     `json:"cents"`
}

type Result struct {
	Trades  []Trade       `json:"trades"`
	Equity  []EquityPoint `json:"equity"`
	Metrics Metrics       `json:"metrics"`
}

func Run(req Request) (Result, error) {
	if req.Plan == nil {
		return Result{}, errors.New("a run needs a compiled strategy plan")
	}
	if req.Capital < 1 {
		return Result{}, errors.New("a run needs starting capital above zero")
	}
	if len(req.Candles) < 2 {
		return Result{}, errors.New("a run needs at least two candles: signals are read at one close and filled at the next open")
	}

	return newEngine(req).run(), nil
}

func candleFor(candle market.Candle) indicator.Candle {
	return indicator.Candle{
		TS:     candle.TS,
		Open:   candle.Open,
		High:   candle.High,
		Low:    candle.Low,
		Close:  candle.Close,
		Volume: float64(candle.Volume),
	}
}

func candlesFor(candles []market.Candle) []indicator.Candle {
	out := make([]indicator.Candle, 0, len(candles))
	for _, candle := range candles {
		out = append(out, candleFor(candle))
	}
	return out
}
