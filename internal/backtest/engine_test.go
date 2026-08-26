package backtest

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/market"
	"github.com/mhetem/ALVO-Backtester/internal/strategy"
)

const holdSpec = `{
  "version": 1,
  "inputs": {"anchor": {"indicator": "sma", "params": {"period": 1}}},
  "entry": {"long": {"gt": ["close", 0]}},
  "sizing": {"type": "fixed_qty", "value": 100},
  "costs": {"brokerage_cents": 0, "fee_bps": 0, "slippage_bps": 0}
}`

const costedHoldSpec = `{
  "version": 1,
  "inputs": {"anchor": {"indicator": "sma", "params": {"period": 1}}},
  "entry": {"long": {"gt": ["close", 0]}},
  "sizing": {"type": "fixed_qty", "value": 100},
  "costs": {"brokerage_cents": 500, "fee_bps": 10, "slippage_bps": 0}
}`

const slippedHoldSpec = `{
  "version": 1,
  "inputs": {"anchor": {"indicator": "sma", "params": {"period": 1}}},
  "entry": {"long": {"gt": ["close", 0]}},
  "sizing": {"type": "fixed_qty", "value": 100},
  "costs": {"brokerage_cents": 0, "fee_bps": 0, "slippage_bps": 100}
}`

const crossSpec = `{
  "version": 1,
  "inputs": {"anchor": {"indicator": "sma", "params": {"period": 1}}},
  "entry": {"long": {"crosses_above": ["close", 20]}},
  "sizing": {"type": "fixed_qty", "value": 100},
  "costs": {"brokerage_cents": 0, "fee_bps": 0, "slippage_bps": 0}
}`

const bracketSpec = `{
  "version": 1,
  "inputs": {"anchor": {"indicator": "sma", "params": {"period": 1}}},
  "entry": {"long": {"crosses_above": ["close", 50]}},
  "exit": {"long": {"any": [
    {"stop_loss": {"type": "pct", "value": 0.05}},
    {"take_profit": {"type": "pct", "value": 0.05}}
  ]}},
  "sizing": {"type": "fixed_qty", "value": 100},
  "costs": {"brokerage_cents": 0, "fee_bps": 0, "slippage_bps": 0}
}`

const equitySpec = `{
  "version": 1,
  "inputs": {"anchor": {"indicator": "sma", "params": {"period": 1}}},
  "entry": {"long": {"gt": ["close", 0]}},
  "sizing": {"type": "pct_equity", "value": 1},
  "costs": {"brokerage_cents": 0, "fee_bps": 0, "slippage_bps": 0}
}`

const riskSpec = `{
  "version": 1,
  "inputs": {"anchor": {"indicator": "sma", "params": {"period": 1}}},
  "entry": {"long": {"gt": ["close", 0]}},
  "exit": {"long": {"stop_loss": {"type": "pct", "value": 0.05}}},
  "sizing": {"type": "risk_pct", "value": 0.02},
  "costs": {"brokerage_cents": 0, "fee_bps": 0, "slippage_bps": 0}
}`

const atrStopSpec = `{
  "version": 1,
  "inputs": {"anchor": {"indicator": "sma", "params": {"period": 1}}},
  "entry": {"long": {"gt": ["close", 0]}},
  "exit": {"long": {"stop_loss": {"type": "atr", "period": 5, "mult": 2}}},
  "sizing": {"type": "fixed_qty", "value": 10},
  "costs": {"brokerage_cents": 0, "fee_bps": 0, "slippage_bps": 0}
}`

var (
	testSymbol = Symbol{Ticker: "TEST", LotSize: 1, TickSize: 0.01}
	lotSymbol  = Symbol{Ticker: "LOTS", LotSize: 100, TickSize: 0.01}

	holdBars = [][4]float64{
		{10.0, 10.5, 9.8, 10.0},
		{10.1, 10.6, 10.0, 10.4},
		{10.4, 11.0, 10.3, 10.9},
		{11.0, 11.5, 10.8, 11.2},
	}
)

func seriesOf(rows [][4]float64) []market.Candle {
	start := time.Date(2026, 1, 5, 13, 0, 0, 0, time.UTC)
	out := make([]market.Candle, 0, len(rows))

	for i, row := range rows {
		out = append(out, market.Candle{
			TS:     start.AddDate(0, 0, i),
			Open:   row[0],
			High:   row[1],
			Low:    row[2],
			Close:  row[3],
			Volume: 1000,
		})
	}

	return out
}

func oneOf(symbol Symbol, candles []market.Candle) []Instrument {
	return []Instrument{{Symbol: symbol, Candles: candles}}
}

func flat(prices ...float64) [][4]float64 {
	out := make([][4]float64, 0, len(prices))
	for _, price := range prices {
		out = append(out, [4]float64{price, price, price, price})
	}
	return out
}

func planOf(t *testing.T, spec string) *strategy.Plan {
	t.Helper()

	parsed, err := strategy.Parse([]byte(spec))
	if err != nil {
		t.Fatalf("parsing the spec: %v", err)
	}

	plan, err := strategy.Compile(parsed)
	if err != nil {
		t.Fatalf("compiling the spec: %v", err)
	}

	return plan
}

func runOf(t *testing.T, spec string, symbol Symbol, capital int64, rows [][4]float64) Result {
	t.Helper()

	result, err := Run(Request{
		Plan:        planOf(t, spec),
		Instruments: oneOf(symbol, seriesOf(rows)),
		Timeframe:   market.TF1d,
		Capital:     capital,
	})
	if err != nil {
		t.Fatalf("running the backtest: %v", err)
	}

	return result
}

func onlyTrade(t *testing.T, result Result) Trade {
	t.Helper()

	if len(result.Trades) != 1 {
		t.Fatalf("trades = %d, want exactly one (%+v)", len(result.Trades), result.Trades)
	}

	return result.Trades[0]
}

func TestBuyAndHoldReturnsTheUnderlyingMinusCosts(t *testing.T) {
	const capital = 1_000_000

	candles := seriesOf(holdBars)
	underlying := int64(math.Round((holdBars[3][3] - holdBars[1][0]) * 100 * 100))

	free := runOf(t, holdSpec, testSymbol, capital, holdBars)
	trade := onlyTrade(t, free)

	if !trade.EntryTS.Equal(candles[1].TS) {
		t.Errorf("entry at %s, want the bar after the signal (%s)", trade.EntryTS, candles[1].TS)
	}
	near(t, "entry price", trade.EntryPrice, holdBars[1][0])
	if !trade.ExitTS.Equal(candles[3].TS) {
		t.Errorf("exit at %s, want the last bar (%s)", trade.ExitTS, candles[3].TS)
	}
	near(t, "exit price", trade.ExitPrice, holdBars[3][3])
	if trade.ExitReason != ReasonEndOfRun {
		t.Errorf("exit reason = %q, want %q", trade.ExitReason, ReasonEndOfRun)
	}

	if free.Metrics.PnLCents != underlying {
		t.Errorf("free of costs the run made %d cents, want the underlying's %d", free.Metrics.PnLCents, underlying)
	}
	if free.Metrics.FinalEquityCents != capital+underlying {
		t.Errorf("final equity = %d cents, want %d", free.Metrics.FinalEquityCents, capital+underlying)
	}
	if free.Metrics.FeesCents != 0 {
		t.Errorf("fees = %d cents, want none", free.Metrics.FeesCents)
	}
	if len(free.Equity) != len(holdBars) {
		t.Errorf("equity points = %d, want one per bar (%d)", len(free.Equity), len(holdBars))
	}
	if free.Metrics.BarsInMarket != 2 {
		t.Errorf("bars in market = %d, want the two the position was held over", free.Metrics.BarsInMarket)
	}

	costed := runOf(t, costedHoldSpec, testSymbol, capital, holdBars)
	roundTrip := int64(601 + 612)

	if costed.Metrics.FeesCents != roundTrip {
		t.Errorf("fees = %d cents, want one round trip (%d)", costed.Metrics.FeesCents, roundTrip)
	}
	if free.Metrics.PnLCents-costed.Metrics.PnLCents != roundTrip {
		t.Errorf("costs took %d cents, want exactly one round trip (%d)",
			free.Metrics.PnLCents-costed.Metrics.PnLCents, roundTrip)
	}
}

func TestSlippageMovesBothEndsOfATradeAgainstTheStrategy(t *testing.T) {
	trade := onlyTrade(t, runOf(t, slippedHoldSpec, testSymbol, 1_000_000, holdBars))

	near(t, "entry price", trade.EntryPrice, 10.21)
	near(t, "exit price", trade.ExitPrice, 11.08)
}

func TestASignalReadAtOneCloseFillsAtTheNextOpen(t *testing.T) {
	bars := [][4]float64{
		{10, 10, 10, 10},
		{25, 25, 25, 25},
		{40, 41, 39, 41},
		{42, 42, 42, 42},
	}

	candles := seriesOf(bars)
	trade := onlyTrade(t, runOf(t, crossSpec, testSymbol, 1_000_000, bars))

	if !trade.EntryTS.Equal(candles[2].TS) {
		t.Errorf("entry at %s, want the bar after the crossing (%s)", trade.EntryTS, candles[2].TS)
	}
	near(t, "entry price", trade.EntryPrice, 40)
}

func TestAnAmbiguousBarFillsTheStop(t *testing.T) {
	cases := []struct {
		name      string
		last      [4]float64
		reason    string
		price     float64
		ambiguous int
	}{
		{"the low hits the stop and the high hits the target", [4]float64{100, 106, 94, 100}, ReasonStop, 95, 1},
		{"only the target is touched", [4]float64{100, 106, 96, 100}, ReasonTarget, 105, 0},
		{"only the stop is touched", [4]float64{100, 104, 94, 100}, ReasonStop, 95, 0},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			bars := [][4]float64{
				{40, 40, 40, 40},
				{100, 100, 100, 100},
				{100, 100, 100, 100},
				test.last,
				{95, 95, 95, 95},
			}

			result := runOf(t, bracketSpec, testSymbol, 2_000_000, bars)
			trade := onlyTrade(t, result)

			if trade.ExitReason != test.reason {
				t.Errorf("exit reason = %q, want %q", trade.ExitReason, test.reason)
			}
			near(t, "exit price", trade.ExitPrice, test.price)
			if result.Metrics.AmbiguousBars != test.ambiguous {
				t.Errorf("ambiguous bars = %d, want %d", result.Metrics.AmbiguousBars, test.ambiguous)
			}
		})
	}
}

func TestAPositionIsRoundedDownToWholeLots(t *testing.T) {
	trade := onlyTrade(t, runOf(t, equitySpec, lotSymbol, 1_000_000, flat(72.5, 72.5, 72.5)))

	if trade.Qty != 100 {
		t.Errorf("qty = %d, want the 137 shares the cash bought rounded down to one 100-share lot", trade.Qty)
	}
}

func TestRiskPctSizesOffTheDistanceToTheStop(t *testing.T) {
	trade := onlyTrade(t, runOf(t, riskSpec, testSymbol, 1_000_000, flat(100, 100, 100)))

	if trade.Qty != 40 {
		t.Errorf("qty = %d, want 40: two percent of ten thousand risked over a five-real stop", trade.Qty)
	}
}

func TestAnEntryWaitsForTheBracketItCannotPriceYet(t *testing.T) {
	bars := make([][4]float64, 0, 8)
	for range 8 {
		bars = append(bars, [4]float64{100, 101, 99, 100})
	}

	candles := seriesOf(bars)
	result := runOf(t, atrStopSpec, testSymbol, 1_000_000, bars)
	trade := onlyTrade(t, result)

	if !trade.EntryTS.Equal(candles[6].TS) {
		t.Errorf("entry at %s, want the bar after the atr came ready (%s)", trade.EntryTS, candles[6].TS)
	}
	if result.Metrics.SkippedEntries != 5 {
		t.Errorf("skipped entries = %d, want the five bars the atr was still warming", result.Metrics.SkippedEntries)
	}
}

func TestTheSameSpecOverTheSameCandlesRunsIdentically(t *testing.T) {
	bars := [][4]float64{
		{40, 40, 40, 40},
		{100, 100, 100, 100},
		{100, 100, 100, 100},
		{100, 106, 94, 100},
		{95, 99, 94, 98},
		{98, 110, 97, 109},
	}

	first, err := json.Marshal(runOf(t, bracketSpec, testSymbol, 2_000_000, bars))
	if err != nil {
		t.Fatalf("encoding the first run: %v", err)
	}

	second, err := json.Marshal(runOf(t, bracketSpec, testSymbol, 2_000_000, bars))
	if err != nil {
		t.Fatalf("encoding the second run: %v", err)
	}

	if string(first) != string(second) {
		t.Errorf("two runs of one spec over one tape disagreed:\n%s\n%s", first, second)
	}
}

func TestARunNeedsABarToFillOn(t *testing.T) {
	_, err := Run(Request{
		Plan:        planOf(t, holdSpec),
		Instruments: oneOf(testSymbol, seriesOf(flat(10))),
		Timeframe:   market.TF1d,
		Capital:     1_000_000,
	})
	if err == nil {
		t.Error("a single candle ran as a backtest, with no bar to fill an intent on")
	}
}
