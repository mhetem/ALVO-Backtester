package backtest

import (
	"math"
	"testing"

	"github.com/mhetem/ALVO-Backtester/internal/market"
)

const oddLotHoldSpec = `{
  "version": 1,
  "inputs": {"anchor": {"indicator": "sma", "params": {"period": 1}}},
  "entry": {"long": {"gt": ["close", 0]}},
  "sizing": {"type": "fixed_qty", "value": 105},
  "costs": {"brokerage_cents": 0, "fee_bps": 0, "slippage_bps": 0}
}`

var (
	// A 2:1 split at bar 2: the share count doubles and every price halves, so a position
	// carried through it is worth exactly what it was worth the bar before.
	splitBars = [][4]float64{
		{10.0, 10.5, 9.8, 10.0},
		{10.1, 10.6, 10.0, 10.4},
		{5.2, 5.5, 5.15, 5.45},
		{5.5, 5.75, 5.4, 5.6},
	}

	// A 1:10 grouping at bar 2, the other direction: ten shares become one and the price
	// is ten times what it was.
	groupBars = [][4]float64{
		{10.0, 10.5, 9.8, 10.0},
		{10.1, 10.6, 10.0, 10.4},
		{104.0, 110.0, 103.0, 109.0},
		{110.0, 115.0, 108.0, 112.0},
	}
)

func TestASplitDoublesTheShareCountAndLeavesEquityAlone(t *testing.T) {
	const capital = 1_000_000

	result, err := Run(Request{
		Plan:        planOf(t, holdSpec),
		Instruments: oneOf(testSymbol, adjusted(splitBars, []float64{0.5, 0.5, 1.0, 1.0})),
		Timeframe:   market.TF1d,
		Capital:     capital,
	})
	if err != nil {
		t.Fatalf("running the backtest: %v", err)
	}

	if result.Metrics.SplitsApplied != 1 || result.Metrics.SplitEvents != 1 {
		t.Fatalf("splits applied = %d of %d events, want one of one",
			result.Metrics.SplitsApplied, result.Metrics.SplitEvents)
	}
	if result.Metrics.UnpricedActions != 0 {
		t.Errorf("unpriced actions = %d, want none: the split was handled", result.Metrics.UnpricedActions)
	}

	trade := onlyTrade(t, result)
	if trade.Qty != 200 {
		t.Errorf("qty = %d, want 200 after a 2:1 split of 100 shares", trade.Qty)
	}
	if math.Abs(trade.EntryPrice-5.05) > 1e-9 {
		t.Errorf("entry price = %g, want 5.05: half of what was paid", trade.EntryPrice)
	}
	if trade.SplitCashCents != 0 {
		t.Errorf("split cash = %d cents, want none: 100 shares double without a remainder", trade.SplitCashCents)
	}

	// Bought 100 at 10.10 for R$1010, sold 200 at 5.60 for R$1120.
	if want := int64(1_011_000); result.Metrics.FinalEquityCents != want {
		t.Errorf("final equity = %d cents, want %d", result.Metrics.FinalEquityCents, want)
	}

	// The bar the split lands on must not move the curve: 200 shares at 5.45 is the same
	// money as 100 at 10.90 would have been.
	if want := int64(1_008_000); result.Equity[2].Cents != want {
		t.Errorf("equity across the split = %d cents, want %d", result.Equity[2].Cents, want)
	}
}

func TestAGroupingFloorsTheShareCountAndPaysOutTheRemainder(t *testing.T) {
	const capital = 1_000_000

	result, err := Run(Request{
		Plan:        planOf(t, oddLotHoldSpec),
		Instruments: oneOf(testSymbol, adjusted(groupBars, []float64{1.0, 1.0, 0.1, 0.1})),
		Timeframe:   market.TF1d,
		Capital:     capital,
	})
	if err != nil {
		t.Fatalf("running the backtest: %v", err)
	}

	if result.Metrics.SplitsApplied != 1 {
		t.Fatalf("splits applied = %d, want one", result.Metrics.SplitsApplied)
	}

	trade := onlyTrade(t, result)
	if trade.Qty != 10 {
		t.Errorf("qty = %d, want 10: 105 shares grouped one for ten leaves ten and a half", trade.Qty)
	}

	// Half a share sold at the bar's open of 104.00.
	if want := int64(5_200); trade.SplitCashCents != want {
		t.Errorf("split cash = %d cents, want %d for the half share", trade.SplitCashCents, want)
	}

	// Bought 105 at 10.10 for R$1060.50, took R$52.00 for the fraction, sold 10 at
	// 112.00 for R$1120.00.
	if want := int64(1_011_150); result.Metrics.FinalEquityCents != want {
		t.Errorf("final equity = %d cents, want %d", result.Metrics.FinalEquityCents, want)
	}

	// The trade has to explain the whole move, or the report and the curve disagree.
	if want := result.Metrics.FinalEquityCents - capital; trade.PnLCents != want {
		t.Errorf("trade profit = %d cents, want %d to match the equity curve", trade.PnLCents, want)
	}
}

func TestASplitMovesTheBracketsWithThePrice(t *testing.T) {
	// A 5% stop set at 9.595 against a 10.10 entry has to become 4.7975 across a 2:1
	// split, or the first post-split bar takes it out at half the price.
	result, err := Run(Request{
		Plan:        planOf(t, splitStopSpec),
		Instruments: oneOf(testSymbol, adjusted(splitBars, []float64{0.5, 0.5, 1.0, 1.0})),
		Timeframe:   market.TF1d,
		Capital:     1_000_000,
	})
	if err != nil {
		t.Fatalf("running the backtest: %v", err)
	}

	trade := onlyTrade(t, result)
	if trade.ExitReason != ReasonEndOfRun {
		t.Errorf("exit reason = %q, want %q: the stop was quoted in pre-split money",
			trade.ExitReason, ReasonEndOfRun)
	}
	if result.Metrics.ExitsByStop != 0 {
		t.Errorf("stops hit = %d, want none", result.Metrics.ExitsByStop)
	}
}

const splitStopSpec = `{
  "version": 1,
  "inputs": {"anchor": {"indicator": "sma", "params": {"period": 1}}},
  "entry": {"long": {"gt": ["close", 0]}},
  "exit": {"long": {"stop_loss": {"type": "pct", "value": 0.05}}},
  "sizing": {"type": "fixed_qty", "value": 100},
  "costs": {"brokerage_cents": 0, "fee_bps": 0, "slippage_bps": 0}
}`
