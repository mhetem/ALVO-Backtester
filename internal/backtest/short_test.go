package backtest

import (
	"math"
	"testing"

	"github.com/mhetem/ALVO-Backtester/internal/market"
)

const shortHoldSpec = `{
  "version": 1,
  "inputs": {"anchor": {"indicator": "sma", "params": {"period": 1}}},
  "entry": {"short": {"gt": ["close", 0]}},
  "sizing": {"type": "fixed_qty", "value": 100},
  "costs": {"brokerage_cents": 0, "fee_bps": 0, "slippage_bps": 0}
}`

const shortBracketSpec = `{
  "version": 1,
  "inputs": {"anchor": {"indicator": "sma", "params": {"period": 1}}},
  "entry": {"short": {"crosses_below": ["close", 50]}},
  "exit": {"short": {"any": [
    {"stop_loss": {"type": "pct", "value": 0.05}},
    {"take_profit": {"type": "pct", "value": 0.05}}
  ]}},
  "sizing": {"type": "fixed_qty", "value": 100},
  "costs": {"brokerage_cents": 0, "fee_bps": 0, "slippage_bps": 0}
}`

const flipSpec = `{
  "version": 1,
  "inputs": {"anchor": {"indicator": "sma", "params": {"period": 1}}},
  "entry": {
    "long":  {"gt": ["close", 20]},
    "short": {"lt": ["close", 20]}
  },
  "exit": {
    "long":  {"lt": ["close", 20]},
    "short": {"gt": ["close", 20]}
  },
  "sizing": {"type": "fixed_qty", "value": 100},
  "costs": {"brokerage_cents": 0, "fee_bps": 0, "slippage_bps": 0}
}`

// A falling market: the mirror of holdBars, so a short must earn exactly what a long
// loses over the same fills.
var fallBars = [][4]float64{
	{11.2, 11.5, 10.8, 11.0},
	{10.9, 11.0, 10.3, 10.4},
	{10.4, 10.6, 10.0, 10.1},
	{10.0, 10.5, 9.8, 10.0},
}

func TestShortSellingEarnsTheFallMinusCosts(t *testing.T) {
	const capital = 1_000_000

	candles := seriesOf(fallBars)
	fall := int64(math.Round((fallBars[1][0] - fallBars[3][3]) * 100 * 100))

	result := runOf(t, shortHoldSpec, testSymbol, capital, fallBars)
	trade := onlyTrade(t, result)

	if trade.Side != SideShort {
		t.Errorf("side = %q, want %q", trade.Side, SideShort)
	}
	if !trade.EntryTS.Equal(candles[1].TS) {
		t.Errorf("entry at %s, want the bar after the signal (%s)", trade.EntryTS, candles[1].TS)
	}
	near(t, "entry price", trade.EntryPrice, fallBars[1][0])
	near(t, "exit price", trade.ExitPrice, fallBars[3][3])

	if trade.PnLCents != fall {
		t.Errorf("the short made %d cents, want the fall's %d", trade.PnLCents, fall)
	}
	if result.Metrics.PnLCents != fall {
		t.Errorf("run PnL = %d cents, want %d", result.Metrics.PnLCents, fall)
	}
	if result.Metrics.FinalEquityCents != capital+fall {
		t.Errorf("final equity = %d cents, want %d", result.Metrics.FinalEquityCents, capital+fall)
	}
	if result.Metrics.ShortTrades != 1 || result.Metrics.LongTrades != 0 {
		t.Errorf("long = %d, short = %d, want one short", result.Metrics.LongTrades, result.Metrics.ShortTrades)
	}
}

func TestAShortLosesWhatTheSameLongWouldMake(t *testing.T) {
	long := runOf(t, holdSpec, testSymbol, 1_000_000, holdBars)
	short := runOf(t, shortHoldSpec, testSymbol, 1_000_000, holdBars)

	if got := long.Metrics.PnLCents + short.Metrics.PnLCents; got != 0 {
		t.Errorf("long %d and short %d over the same bars sum to %d, want 0",
			long.Metrics.PnLCents, short.Metrics.PnLCents, got)
	}
}

func TestAShortEquityCurveTracksTheDebt(t *testing.T) {
	// Marked at every close, a short's equity is cash less what it costs to buy the
	// shares back. A rising market has to show that as a falling curve.
	result := runOf(t, shortHoldSpec, testSymbol, 1_000_000, holdBars)

	if len(result.Equity) != len(holdBars) {
		t.Fatalf("equity points = %d, want one per bar", len(result.Equity))
	}
	for i := 2; i < len(result.Equity); i++ {
		if result.Equity[i].Cents >= result.Equity[i-1].Cents {
			t.Errorf("equity rose from %d to %d at bar %d while short into a rally",
				result.Equity[i-1].Cents, result.Equity[i].Cents, i)
		}
	}
}

func TestAShortStopSitsAboveTheEntry(t *testing.T) {
	// Price crosses below 50 and then rallies through the 5% stop. The stop must fire on
	// the way up, which is the opposite direction to a long's.
	result := runOf(t, shortBracketSpec, testSymbol, 1_000_000, [][4]float64{
		{51, 51, 51, 51},
		{50, 50, 49, 49},
		{49, 49, 49, 49},
		{49, 53, 49, 53},
		{53, 53, 53, 53},
	})

	trade := onlyTrade(t, result)
	if trade.ExitReason != ReasonStop {
		t.Fatalf("exit reason = %q, want %q", trade.ExitReason, ReasonStop)
	}
	if trade.ExitPrice <= trade.EntryPrice {
		t.Errorf("stopped out at %g against an entry of %g, want the stop above the entry",
			trade.ExitPrice, trade.EntryPrice)
	}
	if trade.PnLCents >= 0 {
		t.Errorf("a stopped-out short made %d cents, want a loss", trade.PnLCents)
	}
}

func TestAShortTargetSitsBelowTheEntry(t *testing.T) {
	result := runOf(t, shortBracketSpec, testSymbol, 1_000_000, [][4]float64{
		{51, 51, 51, 51},
		{50, 50, 49, 49},
		{49, 49, 49, 49},
		{49, 49, 45, 45},
		{45, 45, 45, 45},
	})

	trade := onlyTrade(t, result)
	if trade.ExitReason != ReasonTarget {
		t.Fatalf("exit reason = %q, want %q", trade.ExitReason, ReasonTarget)
	}
	if trade.ExitPrice >= trade.EntryPrice {
		t.Errorf("took profit at %g against an entry of %g, want the target below the entry",
			trade.ExitPrice, trade.EntryPrice)
	}
	if trade.PnLCents <= 0 {
		t.Errorf("a short that hit its target made %d cents, want a gain", trade.PnLCents)
	}
}

func TestFlippingSidesCostsAFlatBar(t *testing.T) {
	// The exit signals at one close and fills at the next open; the opposite entry is only
	// evaluated at that bar's close. Reversing therefore takes one flat bar, exactly as it
	// does when a long exits and re-enters.
	result := runOf(t, flipSpec, testSymbol, 1_000_000, flat(25, 25, 15, 15, 25, 25))

	if len(result.Trades) < 2 {
		t.Fatalf("trades = %d, want the position to flip at least once", len(result.Trades))
	}

	first, second := result.Trades[0], result.Trades[1]
	if first.Side == second.Side {
		t.Errorf("both trades are %q, want the position to have flipped sides", first.Side)
	}
	if !second.EntryTS.After(first.ExitTS) {
		t.Errorf("the reversal entered at %s, on or before the exit at %s", second.EntryTS, first.ExitTS)
	}
}

func TestLongWinsABarWhereBothSidesFire(t *testing.T) {
	// Nothing in the data makes one side righter than the other, so the tie has to break
	// the same way every run or the engine stops being deterministic.
	both := `{
      "version": 1,
      "inputs": {"anchor": {"indicator": "sma", "params": {"period": 1}}},
      "entry": {"long": {"gt": ["close", 0]}, "short": {"gt": ["close", 0]}},
      "sizing": {"type": "fixed_qty", "value": 100},
      "costs": {"brokerage_cents": 0, "fee_bps": 0, "slippage_bps": 0}
    }`

	result := runOf(t, both, testSymbol, 1_000_000, holdBars)
	if trade := onlyTrade(t, result); trade.Side != SideLong {
		t.Errorf("side = %q, want %q to win the tie", trade.Side, SideLong)
	}
}

func TestAShortPaysTheDividendItBorrowedThrough(t *testing.T) {
	// The lender of the shares is still entitled to the dividend, so a short position is
	// charged what a long would have collected.
	plain := runOf(t, shortHoldSpec, testSymbol, 1_000_000, holdBars)

	paid, err := Run(Request{
		Plan:      planOf(t, shortHoldSpec),
		Symbol:    testSymbol,
		Timeframe: market.TF1d,
		Capital:   1_000_000,
		Candles:   adjusted(holdBars, []float64{0.95, 0.95, 1.0, 1.0}),
	})
	if err != nil {
		t.Fatalf("running the backtest: %v", err)
	}

	want := -int64(math.Round(100 * holdBars[1][3] * 0.05 * 100))
	if paid.Metrics.DividendsCents != want {
		t.Errorf("dividends = %d cents, want %d charged against the short", paid.Metrics.DividendsCents, want)
	}
	if got := paid.Metrics.PnLCents - plain.Metrics.PnLCents; got != want {
		t.Errorf("the dividend moved the run by %d cents, want %d", got, want)
	}
	if trade := onlyTrade(t, paid); trade.DividendsCents != want {
		t.Errorf("the trade carries %d cents of dividends, want %d", trade.DividendsCents, want)
	}
}
