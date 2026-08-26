package backtest

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/market"
)

const dividendSpec = `{
  "version": 1,
  "inputs": {"anchor": {"indicator": "sma", "params": {"period": 1}}},
  "entry": {"long": {"gt": ["close", 0]}},
  "sizing": {"type": "fixed_qty", "value": 100},
  "costs": {"brokerage_cents": 0, "fee_bps": 0, "slippage_bps": 0}
}`

func adjusted(rows [][4]float64, ratios []float64) []market.Candle {
	candles := seriesOf(rows)
	for i := range candles {
		if ratios[i] <= 0 {
			continue
		}
		value := candles[i].Close * ratios[i]
		candles[i].AdjClose = &value
	}
	return candles
}

func curveOf(cents ...int64) []EquityPoint {
	start := time.Date(2026, 1, 5, 13, 0, 0, 0, time.UTC)
	points := make([]EquityPoint, 0, len(cents))
	for i, value := range cents {
		points = append(points, EquityPoint{TS: start.AddDate(0, 0, i), Cents: value})
	}
	return points
}

func TestActionsReadTheAdjustmentRatio(t *testing.T) {
	// A R$0.50 dividend goes ex on bar 2 against a R$10.00 previous close, so the ratio
	// before it is 0.95 of the ratio after: 1 - 0.95/1.0 = 5% of 10.00.
	candles := adjusted(holdBars, []float64{0.95, 0.95, 1.0, 1.0})

	found := actionsOf(candles)
	if found.events != 1 {
		t.Fatalf("events = %d, want exactly one (%v)", found.events, found.perShare)
	}
	if found.basis() != BasisTotal {
		t.Errorf("basis = %q, want %q", found.basis(), BasisTotal)
	}

	want := holdBars[1][3] * 0.05
	if math.Abs(found.dividendAt(2)-want) > 1e-9 {
		t.Errorf("dividend at bar 2 = %g, want %g", found.dividendAt(2), want)
	}
	for _, i := range []int{0, 1, 3} {
		if found.dividendAt(i) != 0 {
			t.Errorf("dividend at bar %d = %g, want none", i, found.dividendAt(i))
		}
	}
}

func TestActionsReadASplitSizedJumpAsASplit(t *testing.T) {
	// A 2:1 split reads as a 50% "dividend", which is past maxImpliedYield. Crediting it
	// would invent cash no shareholder received; it is a share count that doubled.
	candles := adjusted(holdBars, []float64{0.5, 0.5, 1.0, 1.0})

	found := actionsOf(candles)
	if found.splits != 1 {
		t.Fatalf("splits = %d, want one (%v)", found.splits, found.factor)
	}
	if found.events != 0 || found.unpriced != 0 {
		t.Errorf("events = %d, unpriced = %d, want neither", found.events, found.unpriced)
	}
	if got := found.factorAt(2); math.Abs(got-2) > 1e-9 {
		t.Errorf("split factor at bar 2 = %g, want 2", got)
	}
	if found.dividendAt(2) != 0 {
		t.Errorf("credited %g per share for a split, want nothing", found.dividendAt(2))
	}
}

func TestActionsCountAJumpThatIsNeitherDividendNorSplit(t *testing.T) {
	// 1 - 0.62 is a 38% implied yield: too large for a dividend, and 1/0.62 = 1.61 is not
	// within a percent of any ratio a corporate action uses. It is counted, not acted on.
	candles := adjusted(holdBars, []float64{0.62, 0.62, 1.0, 1.0})

	found := actionsOf(candles)
	if found.unpriced != 1 {
		t.Errorf("unpriced actions = %d, want one", found.unpriced)
	}
	if found.splits != 0 || found.events != 0 {
		t.Errorf("splits = %d, events = %d, want neither", found.splits, found.events)
	}
}

func TestActionsFallBackToPriceReturn(t *testing.T) {
	found := actionsOf(seriesOf(holdBars))
	if found.basis() != BasisPrice {
		t.Errorf("basis = %q, want %q when no bar carries adj_close", found.basis(), BasisPrice)
	}
	if found.events != 0 || found.unpriced != 0 {
		t.Errorf("events = %d, unpriced = %d, want neither", found.events, found.unpriced)
	}
}

func TestDividendsLandInCashAndInTheTrade(t *testing.T) {
	// 100 shares held across a bar-2 ex-date paying 5% of the 10.40 close: 52 cents a
	// share, R$52.00 in all, and the run must be exactly that much better off.
	plain := runOf(t, dividendSpec, testSymbol, 1_000_000, holdBars)

	paid, err := Run(Request{
		Plan:        planOf(t, dividendSpec),
		Instruments: oneOf(testSymbol, adjusted(holdBars, []float64{0.95, 0.95, 1.0, 1.0})),
		Timeframe:   market.TF1d,
		Capital:     1_000_000,
	})
	if err != nil {
		t.Fatalf("running the backtest: %v", err)
	}

	want := int64(math.Round(100 * holdBars[1][3] * 0.05 * 100))
	if paid.Metrics.DividendsCents != want {
		t.Errorf("dividends = %d cents, want %d", paid.Metrics.DividendsCents, want)
	}
	if paid.Metrics.DividendEvents != 1 {
		t.Errorf("dividend events = %d, want one", paid.Metrics.DividendEvents)
	}
	if got := paid.Metrics.PnLCents - plain.Metrics.PnLCents; got != want {
		t.Errorf("dividends added %d cents to the run, want %d", got, want)
	}

	trade := onlyTrade(t, paid)
	if trade.DividendsCents != want {
		t.Errorf("the trade carries %d cents of dividends, want %d", trade.DividendsCents, want)
	}
	if trade.PnLCents != paid.Metrics.PnLCents {
		t.Errorf("trade PnL %d disagrees with the run's %d", trade.PnLCents, paid.Metrics.PnLCents)
	}
	if paid.Metrics.Basis != BasisTotal {
		t.Errorf("basis = %q, want %q", paid.Metrics.Basis, BasisTotal)
	}
}

func TestDrawdownFindsTheDeepestFallAndItsRecovery(t *testing.T) {
	curve := curveOf(100, 120, 90, 110, 130)

	worst := deepestDrawdown(curve)
	if math.Abs(worst.Pct-(-25)) > 1e-9 {
		t.Errorf("max drawdown = %g%%, want -25%% from 120 to 90", worst.Pct)
	}
	if worst.Cents != -30 {
		t.Errorf("drawdown = %d cents, want -30", worst.Cents)
	}
	if !worst.PeakTS.Equal(curve[1].TS) || !worst.TroughTS.Equal(curve[2].TS) {
		t.Errorf("drawdown ran %s to %s, want %s to %s", worst.PeakTS, worst.TroughTS, curve[1].TS, curve[2].TS)
	}
	if worst.Bars != 1 {
		t.Errorf("drawdown spanned %d bars, want 1", worst.Bars)
	}
	if !worst.Recovered {
		t.Error("the curve climbs back past 120, so the drawdown recovered")
	}
}

func TestDrawdownStaysOpenWhenItNeverRecovers(t *testing.T) {
	worst := deepestDrawdown(curveOf(100, 120, 90, 100, 110))
	if worst.Recovered {
		t.Error("the curve never returns to 120, so the drawdown is still open")
	}
	if longest := longestDrawdown(curveOf(100, 120, 90, 100, 110)); longest != 3 {
		t.Errorf("longest drawdown = %d bars, want 3", longest)
	}
}

func TestFlatCurveHasNoDrawdown(t *testing.T) {
	if worst := deepestDrawdown(curveOf(100, 100, 100)); worst.Pct != 0 || worst.Bars != 0 {
		t.Errorf("drawdown = %+v, want none on a flat curve", worst)
	}
}

func TestSharpeIsZeroWhenEveryStepIsIdentical(t *testing.T) {
	steps := []float64{0.01, 0.01, 0.01, 0.01}
	if got := sharpe(steps, 252); got != 0 {
		t.Errorf("sharpe = %g, want 0 when the spread is zero", got)
	}
}

func TestSortinoIgnoresUpsideDeviation(t *testing.T) {
	// Two series with the same mean: one only rises, the other swings. Sortino punishes
	// the second and leaves the first without a denominator at all.
	rising := []float64{0.01, 0.02, 0.03, 0.02}
	swinging := []float64{-0.02, 0.06, -0.02, 0.06}

	if got := sortino(rising, 252); got != 0 {
		t.Errorf("sortino = %g, want 0 when nothing falls below zero", got)
	}
	if got := sortino(swinging, 252); got <= 0 {
		t.Errorf("sortino = %g, want a positive figure for a positive mean", got)
	}
}

func TestCAGRCompoundsOverTheCalendarSpan(t *testing.T) {
	// Doubling across exactly one year is 100%; across two it is the square root of two.
	year := 365.25 * 24 * time.Hour

	if got := cagr(1000, 2000, year); math.Abs(got-100) > 1e-6 {
		t.Errorf("cagr = %g%%, want 100%% for a double in a year", got)
	}
	if got := cagr(1000, 2000, 2*year); math.Abs(got-41.4213562373) > 1e-6 {
		t.Errorf("cagr = %g%%, want 41.42%% for a double in two years", got)
	}
	if got := cagr(0, 2000, year); got != 0 {
		t.Errorf("cagr = %g, want 0 when the curve starts at nothing", got)
	}
}

func TestCalmarNeedsADrawdownToDivideBy(t *testing.T) {
	if got := calmar(20, -10); math.Abs(got-2) > 1e-9 {
		t.Errorf("calmar = %g, want 2", got)
	}
	if got := calmar(20, 0); got != 0 {
		t.Errorf("calmar = %g, want 0 when the curve never fell", got)
	}
}

func TestBuyAndHoldBenchmarkMatchesAHeldPosition(t *testing.T) {
	// The benchmark buys at the second bar's open, the same bar the engine can first fill
	// on, so a strategy that is always long has nothing left to explain.
	result := runOf(t, equitySpec, testSymbol, 1_000_000, holdBars)

	hold := result.Metrics.Benchmarks[0]
	if hold.Kind != BenchmarkHold {
		t.Fatalf("first benchmark is %q, want %q", hold.Kind, BenchmarkHold)
	}
	if hold.Unavailable != "" {
		t.Fatalf("benchmark unavailable: %s", hold.Unavailable)
	}
	if math.Abs(hold.ReturnPct-result.Metrics.ReturnPct) > 1e-9 {
		t.Errorf("buy-and-hold returned %g%% against the run's %g%%, want them equal",
			hold.ReturnPct, result.Metrics.ReturnPct)
	}
	if math.Abs(hold.ExcessPct) > 1e-9 {
		t.Errorf("excess = %g%%, want nothing over a benchmark the strategy replicates", hold.ExcessPct)
	}
}

func TestIndexBenchmarkReportsWhyItIsMissing(t *testing.T) {
	result := runOf(t, holdSpec, testSymbol, 1_000_000, holdBars)

	index := result.Metrics.Benchmarks[1]
	if index.Kind != BenchmarkIndex {
		t.Fatalf("second benchmark is %q, want %q", index.Kind, BenchmarkIndex)
	}
	if index.Unavailable == "" {
		t.Error("no index candles were supplied, so the benchmark must say so rather than read as a flat zero")
	}
	if index.ReturnPct != 0 || index.ExcessPct != 0 {
		t.Errorf("an unavailable benchmark reported %g%% and %g%% excess, want neither",
			index.ReturnPct, index.ExcessPct)
	}
}

func TestIndexBenchmarkTracksTheIndexClose(t *testing.T) {
	index := seriesOf([][4]float64{
		{100, 100, 100, 100},
		{100, 110, 100, 110},
		{110, 120, 110, 120},
		{120, 130, 120, 125},
	})

	result, err := Run(Request{
		Plan:        planOf(t, holdSpec),
		Instruments: oneOf(testSymbol, seriesOf(holdBars)),
		Timeframe:   market.TF1d,
		Capital:     1_000_000,
		Index:       index,
		IndexSymbol: "^BVSP",
	})
	if err != nil {
		t.Fatalf("running the backtest: %v", err)
	}

	mark := result.Metrics.Benchmarks[1]
	if mark.Unavailable != "" {
		t.Fatalf("benchmark unavailable: %s", mark.Unavailable)
	}
	if math.Abs(mark.ReturnPct-25) > 1e-6 {
		t.Errorf("index returned %g%%, want 25%% from 100 to 125", mark.ReturnPct)
	}
	if got := result.Index[0]; got != 1_000_000 {
		t.Errorf("the index curve starts at %d cents, want the run's capital", got)
	}
	if got := result.Index[len(result.Index)-1]; got != 1_250_000 {
		t.Errorf("the index curve ends at %d cents, want 1250000", got)
	}
}

func TestTradeTallyCountsWhatTheReportShows(t *testing.T) {
	result := runOf(t, bracketSpec, testSymbol, 1_000_000, [][4]float64{
		{49, 49, 49, 49},
		{50, 51, 49, 51},
		{51, 51, 51, 51},
		{51, 54, 51, 54},
		{54, 54, 54, 54},
		{54, 54, 50, 50},
		{50, 50, 50, 50},
	})

	m := result.Metrics
	if m.Trades == 0 {
		t.Fatal("the spec crosses 50 and brackets at 5%, so it must trade")
	}
	if m.Wins+m.Losses+m.Scratches != m.Trades {
		t.Errorf("wins %d + losses %d + scratches %d != trades %d", m.Wins, m.Losses, m.Scratches, m.Trades)
	}
	if m.WinRatePct != float64(m.Wins)/float64(m.Trades)*100 {
		t.Errorf("win rate = %g%%, want %d of %d", m.WinRatePct, m.Wins, m.Trades)
	}
	if m.AvgHoldingBars <= 0 {
		t.Errorf("average holding = %g bars, want a positive span", m.AvgHoldingBars)
	}
	if m.TimeInMarket <= 0 || m.TimeInMarket > 100 {
		t.Errorf("time in market = %g%%, want a share of the run", m.TimeInMarket)
	}
	if m.LargestWinCents < 0 || m.LargestLossCents > 0 {
		t.Errorf("largest win %d and largest loss %d have the wrong signs", m.LargestWinCents, m.LargestLossCents)
	}
}

func TestProfitFactorIsNullWithoutALoss(t *testing.T) {
	// JSON cannot carry an infinity, and encoding one would fail the whole run rather
	// than report it, so an undefined profit factor has to survive a round trip as null.
	result := runOf(t, holdSpec, testSymbol, 1_000_000, holdBars)
	if result.Metrics.ProfitFactor != nil {
		t.Errorf("profit factor = %g, want null when nothing lost", *result.Metrics.ProfitFactor)
	}

	encoded, err := json.Marshal(result.Metrics)
	if err != nil {
		t.Fatalf("encoding metrics: %v", err)
	}
	if !bytes.Contains(encoded, []byte(`"profit_factor":null`)) {
		t.Errorf("metrics encoded without a null profit factor: %s", encoded)
	}
}
