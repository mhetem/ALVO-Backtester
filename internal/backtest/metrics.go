package backtest

import (
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/market"
)

// One stock's whole run. A basket is N of these plus the sum of their curves, so a sleeve
// carries the same risk numbers the aggregate does rather than a trade tally alone.
type SymbolStats struct {
	Symbol           string      `json:"symbol"`
	SymbolID         int64       `json:"-"`
	Basis            string      `json:"basis"`
	CapitalCents     int64       `json:"capital_cents"`
	FinalEquityCents int64       `json:"final_equity_cents"`
	Trades           int         `json:"trades"`
	Wins             int         `json:"wins"`
	Losses           int         `json:"losses"`
	WinRatePct       float64     `json:"win_rate_pct"`
	PnLCents         int64       `json:"pnl_cents"`
	FeesCents        int64       `json:"fees_cents"`
	DividendsCents   int64       `json:"dividends_cents"`
	BorrowCents      int64       `json:"borrow_cents"`
	BorrowAnnualPct  float64     `json:"borrow_annual_pct"`
	BarsInMarket     int         `json:"bars_in_market"`
	TimeInMarket     float64     `json:"time_in_market_pct"`
	ContributionPct  float64     `json:"contribution_pct"`
	ReturnPct        float64     `json:"return_pct"`
	CAGRPct          float64     `json:"cagr_pct"`
	VolatilityPct    float64     `json:"volatility_pct"`
	MaxDrawdown      Drawdown    `json:"max_drawdown"`
	Sharpe           float64     `json:"sharpe"`
	Sortino          float64     `json:"sortino"`
	Calmar           float64     `json:"calmar"`
	ProfitFactor     *float64    `json:"profit_factor"`
	ExpectancyCents  int64       `json:"expectancy_cents"`
	Benchmarks       []Benchmark `json:"benchmarks,omitempty"`
}

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
	BorrowCents      int64   `json:"borrow_cents"`
	BorrowStale      bool    `json:"borrow_stale"`
	SplitCashCents   int64   `json:"split_cash_cents"`
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

	ExitsBySignal     int `json:"exits_by_signal"`
	ExitsByStop       int `json:"exits_by_stop"`
	ExitsByTarget     int `json:"exits_by_target"`
	ExitsBySplit      int `json:"exits_by_split"`
	ExitsAtEnd        int `json:"exits_at_end"`
	AmbiguousBars     int `json:"ambiguous_bars"`
	SkippedEntries    int `json:"skipped_entries"`
	ShortsUnavailable int `json:"shorts_unavailable"`
	UnadjustedBars    int `json:"unadjusted_bars"`
	UnpricedActions   int `json:"unpriced_actions"`
	SplitEvents       int `json:"split_events"`
	SplitsApplied     int `json:"splits_applied"`

	Symbols    []SymbolStats `json:"symbols"`
	Benchmarks []Benchmark   `json:"benchmarks"`
}

func (e *engine) summarize() {
	e.metrics.Bars = len(e.stamps)
	e.metrics.Trades = len(e.trades)
	e.metrics.FinalEquityCents = e.cash
	e.metrics.Basis = e.basis()

	for _, b := range e.books {
		e.metrics.UnadjustedBars += b.acts.unadjusted
		e.metrics.UnpricedActions += b.acts.unpriced
	}

	if len(e.equity) > 0 {
		e.metrics.FinalEquityCents = e.equity[len(e.equity)-1].Cents
	}

	e.tally()
	e.describeSymbols()

	e.metrics.PnLCents = e.metrics.FinalEquityCents - e.metrics.CapitalCents
	if e.metrics.CapitalCents > 0 {
		e.metrics.ReturnPct = float64(e.metrics.PnLCents) / float64(e.metrics.CapitalCents) * 100
	}
	if e.metrics.Bars > 0 {
		e.metrics.TimeInMarket = float64(e.metrics.BarsInMarket) / float64(e.metrics.Bars) * 100
	}

	e.metrics.BarsPerYear = e.periods()

	if len(e.equity) < 2 {
		return
	}

	steps := returnsOf(e.equity)
	rf := e.riskFree(e.periods())

	e.risk(steps, rf, e.periods())
	e.compare(steps, rf, e.periods())
}

// A basket is on a total-return basis as soon as any of its symbols is: mixing the two
// would be a comparison between different things, and saying so is the whole point of the
// field. Only a run where nothing carried an adjusted close is a price return.
func (e *engine) basis() string {
	for _, b := range e.books {
		if b.acts.basis() == BasisTotal {
			return BasisTotal
		}
	}
	return BasisPrice
}

func (e *engine) describeSymbols() {
	last := time.Time{}
	if len(e.stamps) > 0 {
		last = e.stamps[len(e.stamps)-1]
	}

	e.metrics.Symbols = make([]SymbolStats, 0, len(e.books))
	for _, b := range e.books {
		stats := b.stats
		stats.SymbolID = b.symbol.ID
		if stats.Trades > 0 {
			stats.WinRatePct = float64(stats.Wins) / float64(stats.Trades) * 100
		}
		if e.metrics.CapitalCents > 0 {
			stats.ContributionPct = float64(stats.PnLCents) / float64(e.metrics.CapitalCents) * 100
		}
		if e.req.Borrow != nil {
			stats.BorrowAnnualPct = e.req.Borrow.AnnualPct(b.symbol.Ticker, last)
		}
		e.metrics.Symbols = append(e.metrics.Symbols, stats)
	}

	if e.req.Borrow != nil && !last.IsZero() {
		e.metrics.BorrowStale = !e.req.Borrow.Covers(last)
	}
}

func (e *engine) tally() {
	tallyTrades(&e.metrics, e.trades, e.stamps)
}

// Shared with the basket aggregate, which has a merged trade log and a union timeline but
// counts wins, streaks and holding periods exactly the way one sleeve does.
func tallyTrades(m *Metrics, trades []Trade, stamps []time.Time) {
	grossWin, grossLoss := int64(0), int64(0)
	streak, holding := 0, 0
	index := stampIndex(stamps)

	for _, trade := range trades {
		switch {
		case trade.PnLCents > 0:
			m.Wins++
			grossWin += trade.PnLCents
			m.LargestWinCents = max(m.LargestWinCents, trade.PnLCents)
			streak = 0
		case trade.PnLCents < 0:
			m.Losses++
			grossLoss += -trade.PnLCents
			m.LargestLossCents = min(m.LargestLossCents, trade.PnLCents)
			streak++
			m.MaxConsecLosses = max(m.MaxConsecLosses, streak)
		default:
			m.Scratches++
			streak = 0
		}

		if trade.Side == SideShort {
			m.ShortTrades++
		} else {
			m.LongTrades++
		}

		m.FeesCents += trade.FeesCents
		holding += index[trade.ExitTS.Unix()] - index[trade.EntryTS.Unix()]

		switch trade.ExitReason {
		case ReasonSignal:
			m.ExitsBySignal++
		case ReasonStop:
			m.ExitsByStop++
		case ReasonTarget:
			m.ExitsByTarget++
		case ReasonSplit:
			m.ExitsBySplit++
		case ReasonEndOfRun:
			m.ExitsAtEnd++
		}
	}

	if m.Trades == 0 {
		return
	}

	m.WinRatePct = float64(m.Wins) / float64(m.Trades) * 100
	m.ExpectancyCents = (grossWin - grossLoss) / int64(m.Trades)
	m.AvgHoldingBars = float64(holding) / float64(m.Trades)

	if m.Wins > 0 {
		m.AvgWinCents = grossWin / int64(m.Wins)
	}
	if m.Losses > 0 {
		m.AvgLossCents = -grossLoss / int64(m.Losses)
	}
	// Gross win over gross loss is undefined without a loss to divide by. JSON has no
	// infinity, and a zero here would read as the opposite of what happened, so the field
	// is null and the report says "no losing trade" rather than printing a number.
	if grossLoss > 0 {
		factor := float64(grossWin) / float64(grossLoss)
		m.ProfitFactor = &factor
	}
}

func stampIndex(stamps []time.Time) map[int64]int {
	index := make(map[int64]int, len(stamps))
	for i, ts := range stamps {
		index[ts.Unix()] = i
	}
	return index
}

func (e *engine) risk(steps, rf []float64, periods float64) {
	riskOf(&e.metrics, e.equity, steps, rf, periods)
}

func riskOf(m *Metrics, equity []EquityPoint, steps, rf []float64, periods float64) {
	first, last := equity[0], equity[len(equity)-1]

	m.CAGRPct = cagr(first.Cents, last.Cents, last.TS.Sub(first.TS))
	m.VolatilityPct = annualizedVol(steps, periods)
	m.MaxDrawdown = deepestDrawdown(equity)
	m.LongestDrawdownBars = longestDrawdown(equity)
	m.Calmar = calmar(m.CAGRPct, m.MaxDrawdown.Pct)

	excess := make([]float64, len(steps))
	for i, step := range steps {
		excess[i] = step - rf[i]
	}

	m.Sharpe = sharpe(excess, periods)
	m.Sortino = sortino(excess, periods)
}

func (e *engine) riskFree(periods float64) []float64 {
	return riskFreeOf(&e.metrics, e.req.Rates, e.equity, periods)
}

func riskFreeOf(m *Metrics, rates *market.Rates, equity []EquityPoint, periods float64) []float64 {
	rf := make([]float64, max(len(equity)-1, 0))
	if rates == nil {
		return rf
	}

	for i := range rf {
		rf[i] = rates.PerPeriod(equity[i+1].TS, periods)
	}

	first, last := equity[0].TS, equity[len(equity)-1].TS
	m.RiskFreePct = rates.AnnualPct(last)
	m.RiskFreeStale = !rates.Covers(first, last)

	return rf
}

func (e *engine) compare(steps, rf []float64, periods float64) {
	stamps := make([]time.Time, len(e.equity))
	for i, point := range e.equity {
		stamps[i] = point.TS
	}

	hold := holdBenchmark(e.req, e.books, e.stamps, e.basis())
	hold.score(stamps, steps, rf, periods)
	if hold.Unavailable == "" {
		hold.ExcessPct = e.metrics.ReturnPct - hold.ReturnPct
	}

	index := indexBenchmark(e.req, e.stamps)
	index.score(stamps, steps, rf, periods)
	if index.Unavailable == "" {
		index.ExcessPct = e.metrics.ReturnPct - index.ReturnPct
	}

	e.metrics.Benchmarks = []Benchmark{hold, index}
	e.hold = hold.curve
	e.index = index.curve
}
