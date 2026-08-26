package backtest

import (
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/market"
)

type Metrics struct {
	Basis        string  `json:"basis"`
	Bars         int     `json:"bars"`
	BarsInMarket int     `json:"bars_in_market"`
	TimeInMarket float64 `json:"time_in_market_pct"`
	BarsPerYear  float64 `json:"bars_per_year"`

	CapitalCents     int64   `json:"capital_cents"`
	FinalEquityCents int64   `json:"final_equity_cents"`
	PnLCents         int64   `json:"pnl_cents"`
	FeesCents        int64   `json:"fees_cents"`
	DividendsCents   int64   `json:"dividends_cents"`
	DividendEvents   int     `json:"dividend_events"`
	ReturnPct        float64 `json:"return_pct"`
	CAGRPct          float64 `json:"cagr_pct"`
	VolatilityPct    float64 `json:"volatility_pct"`

	MaxDrawdown         Drawdown `json:"max_drawdown"`
	LongestDrawdownBars int      `json:"longest_drawdown_bars"`
	Sharpe              float64  `json:"sharpe"`
	Sortino             float64  `json:"sortino"`
	Calmar              float64  `json:"calmar"`

	RiskFreePct   float64 `json:"risk_free_pct"`
	RiskFreeStale bool    `json:"risk_free_stale"`

	Trades           int      `json:"trades"`
	LongTrades       int      `json:"long_trades"`
	ShortTrades      int      `json:"short_trades"`
	Wins             int      `json:"wins"`
	Losses           int      `json:"losses"`
	Scratches        int      `json:"scratches"`
	WinRatePct       float64  `json:"win_rate_pct"`
	ProfitFactor     *float64 `json:"profit_factor"`
	ExpectancyCents  int64    `json:"expectancy_cents"`
	AvgWinCents      int64    `json:"avg_win_cents"`
	AvgLossCents     int64    `json:"avg_loss_cents"`
	LargestWinCents  int64    `json:"largest_win_cents"`
	LargestLossCents int64    `json:"largest_loss_cents"`
	MaxConsecLosses  int      `json:"max_consecutive_losses"`
	AvgHoldingBars   float64  `json:"avg_holding_bars"`

	ExitsBySignal   int `json:"exits_by_signal"`
	ExitsByStop     int `json:"exits_by_stop"`
	ExitsByTarget   int `json:"exits_by_target"`
	ExitsAtEnd      int `json:"exits_at_end"`
	AmbiguousBars   int `json:"ambiguous_bars"`
	SkippedEntries  int `json:"skipped_entries"`
	UnadjustedBars  int `json:"unadjusted_bars"`
	UnpricedActions int `json:"unpriced_actions"`

	Benchmarks []Benchmark `json:"benchmarks"`
}

func (e *engine) summarize() {
	e.metrics.Bars = len(e.req.Candles)
	e.metrics.Trades = len(e.trades)
	e.metrics.FinalEquityCents = e.cash
	e.metrics.Basis = e.dist.basis()
	e.metrics.UnadjustedBars = e.dist.unadjusted
	e.metrics.UnpricedActions = e.dist.unpriced

	if len(e.equity) > 0 {
		e.metrics.FinalEquityCents = e.equity[len(e.equity)-1].Cents
	}

	e.tally()
	e.metrics.PnLCents = e.metrics.FinalEquityCents - e.metrics.CapitalCents
	if e.metrics.CapitalCents > 0 {
		e.metrics.ReturnPct = float64(e.metrics.PnLCents) / float64(e.metrics.CapitalCents) * 100
	}
	if e.metrics.Bars > 0 {
		e.metrics.TimeInMarket = float64(e.metrics.BarsInMarket) / float64(e.metrics.Bars) * 100
	}

	periods := e.req.BarsPerYear
	if periods <= 0 {
		periods = market.TradingDaysPerYear
	}
	e.metrics.BarsPerYear = periods

	if len(e.equity) < 2 {
		return
	}

	steps := returnsOf(e.equity)
	rf := e.riskFree(periods)

	e.risk(steps, rf, periods)
	e.compare(steps, rf, periods)
}

func (e *engine) tally() {
	grossWin, grossLoss := int64(0), int64(0)
	streak, holding := 0, 0
	stamps := e.stampIndex()

	for _, trade := range e.trades {
		switch {
		case trade.PnLCents > 0:
			e.metrics.Wins++
			grossWin += trade.PnLCents
			e.metrics.LargestWinCents = max(e.metrics.LargestWinCents, trade.PnLCents)
			streak = 0
		case trade.PnLCents < 0:
			e.metrics.Losses++
			grossLoss += -trade.PnLCents
			e.metrics.LargestLossCents = min(e.metrics.LargestLossCents, trade.PnLCents)
			streak++
			e.metrics.MaxConsecLosses = max(e.metrics.MaxConsecLosses, streak)
		default:
			e.metrics.Scratches++
			streak = 0
		}

		if trade.Side == SideShort {
			e.metrics.ShortTrades++
		} else {
			e.metrics.LongTrades++
		}

		e.metrics.FeesCents += trade.FeesCents
		holding += stamps[trade.ExitTS.Unix()] - stamps[trade.EntryTS.Unix()]

		switch trade.ExitReason {
		case ReasonSignal:
			e.metrics.ExitsBySignal++
		case ReasonStop:
			e.metrics.ExitsByStop++
		case ReasonTarget:
			e.metrics.ExitsByTarget++
		case ReasonEndOfRun:
			e.metrics.ExitsAtEnd++
		}
	}

	if e.metrics.Trades == 0 {
		return
	}

	e.metrics.WinRatePct = float64(e.metrics.Wins) / float64(e.metrics.Trades) * 100
	e.metrics.ExpectancyCents = (grossWin - grossLoss) / int64(e.metrics.Trades)
	e.metrics.AvgHoldingBars = float64(holding) / float64(e.metrics.Trades)

	if e.metrics.Wins > 0 {
		e.metrics.AvgWinCents = grossWin / int64(e.metrics.Wins)
	}
	if e.metrics.Losses > 0 {
		e.metrics.AvgLossCents = -grossLoss / int64(e.metrics.Losses)
	}
	// Gross win over gross loss is undefined without a loss to divide by. JSON has no
	// infinity, and a zero here would read as the opposite of what happened, so the field
	// is null and the report says "no losing trade" rather than printing a number.
	if grossLoss > 0 {
		factor := float64(grossWin) / float64(grossLoss)
		e.metrics.ProfitFactor = &factor
	}
}

func (e *engine) stampIndex() map[int64]int {
	index := make(map[int64]int, len(e.req.Candles))
	for i, candle := range e.req.Candles {
		index[candle.TS.Unix()] = i
	}
	return index
}

func (e *engine) risk(steps, rf []float64, periods float64) {
	first, last := e.equity[0], e.equity[len(e.equity)-1]

	e.metrics.CAGRPct = cagr(first.Cents, last.Cents, last.TS.Sub(first.TS))
	e.metrics.VolatilityPct = annualizedVol(steps, periods)
	e.metrics.MaxDrawdown = deepestDrawdown(e.equity)
	e.metrics.LongestDrawdownBars = longestDrawdown(e.equity)
	e.metrics.Calmar = calmar(e.metrics.CAGRPct, e.metrics.MaxDrawdown.Pct)

	excess := make([]float64, len(steps))
	for i, step := range steps {
		excess[i] = step - rf[i]
	}

	e.metrics.Sharpe = sharpe(excess, periods)
	e.metrics.Sortino = sortino(excess, periods)
}

func (e *engine) riskFree(periods float64) []float64 {
	rf := make([]float64, max(len(e.equity)-1, 0))
	if e.req.Rates == nil {
		return rf
	}

	for i := range rf {
		rf[i] = e.req.Rates.PerPeriod(e.equity[i+1].TS, periods)
	}

	first, last := e.equity[0].TS, e.equity[len(e.equity)-1].TS
	e.metrics.RiskFreePct = e.req.Rates.AnnualPct(last)
	e.metrics.RiskFreeStale = !e.req.Rates.Covers(first, last)

	return rf
}

func (e *engine) compare(steps, rf []float64, periods float64) {
	stamps := make([]time.Time, len(e.equity))
	for i, point := range e.equity {
		stamps[i] = point.TS
	}

	hold := holdBenchmark(e.req, e.dist)
	hold.score(stamps, steps, rf, periods)
	if hold.Unavailable == "" {
		hold.ExcessPct = e.metrics.ReturnPct - hold.ReturnPct
	}

	index := indexBenchmark(e.req)
	index.score(stamps, steps, rf, periods)
	if index.Unavailable == "" {
		index.ExcessPct = e.metrics.ReturnPct - index.ReturnPct
	}

	e.metrics.Benchmarks = []Benchmark{hold, index}
	e.hold = hold.curve
	e.index = index.curve
}
