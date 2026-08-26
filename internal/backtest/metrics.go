package backtest

type Metrics struct {
	Bars             int     `json:"bars"`
	BarsInMarket     int     `json:"bars_in_market"`
	Trades           int     `json:"trades"`
	Wins             int     `json:"wins"`
	Losses           int     `json:"losses"`
	Scratches        int     `json:"scratches"`
	CapitalCents     int64   `json:"capital_cents"`
	FinalEquityCents int64   `json:"final_equity_cents"`
	PnLCents         int64   `json:"pnl_cents"`
	FeesCents        int64   `json:"fees_cents"`
	ReturnPct        float64 `json:"return_pct"`
	ExitsBySignal    int     `json:"exits_by_signal"`
	ExitsByStop      int     `json:"exits_by_stop"`
	ExitsByTarget    int     `json:"exits_by_target"`
	ExitsAtEnd       int     `json:"exits_at_end"`
	AmbiguousBars    int     `json:"ambiguous_bars"`
	SkippedEntries   int     `json:"skipped_entries"`
}

func (e *engine) summarize() {
	e.metrics.Bars = len(e.req.Candles)
	e.metrics.Trades = len(e.trades)
	e.metrics.FinalEquityCents = e.cash

	if len(e.equity) > 0 {
		e.metrics.FinalEquityCents = e.equity[len(e.equity)-1].Cents
	}

	for _, trade := range e.trades {
		switch {
		case trade.PnLCents > 0:
			e.metrics.Wins++
		case trade.PnLCents < 0:
			e.metrics.Losses++
		default:
			e.metrics.Scratches++
		}

		e.metrics.FeesCents += trade.FeesCents

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

	e.metrics.PnLCents = e.metrics.FinalEquityCents - e.metrics.CapitalCents
	if e.metrics.CapitalCents > 0 {
		e.metrics.ReturnPct = float64(e.metrics.PnLCents) / float64(e.metrics.CapitalCents) * 100
	}
}
