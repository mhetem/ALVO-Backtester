package backtest

import (
	"errors"
	"fmt"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/indicator"
	"github.com/mhetem/ALVO-Backtester/internal/market"
	"github.com/mhetem/ALVO-Backtester/internal/strategy"
)

const (
	SideLong  = "long"
	SideShort = "short"

	ReasonSignal   = "signal"
	ReasonStop     = "stop"
	ReasonTarget   = "target"
	ReasonSplit    = "split"
	ReasonEndOfRun = "end_of_run"
)

func sideName(short bool) string {
	if short {
		return SideShort
	}
	return SideLong
}

type Symbol struct {
	ID       int64
	Ticker   string
	Kind     string
	LotSize  int64
	TickSize float64
}

func (s Symbol) Future() bool { return market.Kind(s.Kind) == market.KindFuture }

type Instrument struct {
	Symbol  Symbol
	Prime   []market.Candle
	Candles []market.Candle
}

type Request struct {
	Plan         *strategy.Plan
	Instruments  []Instrument
	MaxPositions int
	Timeframe    market.Timeframe
	Capital      int64
	Index        []market.Candle
	IndexSymbol  string
	Rates        *market.Rates
	Borrow       *market.Borrow
	BarsPerYear  float64
}

func (r Request) Basket() bool { return len(r.Instruments) > 1 }

type Trade struct {
	Seq            int32     `json:"seq"`
	SymbolID       int64     `json:"-"`
	Symbol         string    `json:"symbol"`
	Side           string    `json:"side"`
	Qty            int64     `json:"qty"`
	EntryTS        time.Time `json:"entry_ts"`
	EntryPrice     float64   `json:"entry_price"`
	ExitTS         time.Time `json:"exit_ts"`
	ExitPrice      float64   `json:"exit_price"`
	PnLCents       int64     `json:"pnl_cents"`
	FeesCents      int64     `json:"fees_cents"`
	DividendsCents int64     `json:"dividends_cents"`
	BorrowCents    int64     `json:"borrow_cents"`
	SplitCashCents int64     `json:"split_cash_cents"`
	ExitReason     string    `json:"exit_reason"`
}

type EquityPoint struct {
	TS    time.Time `json:"ts"`
	Cents int64     `json:"cents"`
}

type Result struct {
	Trades  []Trade       `json:"trades"`
	Equity  []EquityPoint `json:"equity"`
	Hold    []int64       `json:"-"`
	Index   []int64       `json:"-"`
	Metrics Metrics       `json:"metrics"`
}

func Run(req Request) (Result, error) {
	if req.Plan == nil {
		return Result{}, errors.New("a run needs a compiled strategy plan")
	}
	if req.Capital < 1 {
		return Result{}, errors.New("a run needs starting capital above zero")
	}
	if len(req.Instruments) == 0 {
		return Result{}, errors.New("a run needs at least one symbol")
	}

	for _, held := range req.Instruments {
		if len(held.Candles) < 2 {
			return Result{}, fmt.Errorf("%s has %d candles: a run needs at least two, since signals are read at one close and filled at the next open",
				held.Symbol.Ticker, len(held.Candles))
		}
	}

	if req.MaxPositions < 1 {
		req.MaxPositions = len(req.Instruments)
	}
	req.MaxPositions = min(req.MaxPositions, len(req.Instruments))

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
