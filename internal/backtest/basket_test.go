package backtest

import (
	"testing"

	"github.com/mhetem/ALVO-Backtester/internal/market"
)

var twinSymbol = Symbol{ID: 2, Ticker: "TWIN", LotSize: 1, TickSize: 0.01}

func basketOf(t *testing.T, spec string, capital int64, held ...Instrument) Result {
	t.Helper()

	result, err := Run(Request{
		Plan:        planOf(t, spec),
		Instruments: held,
		Timeframe:   market.TF1d,
		Capital:     capital,
	})
	if err != nil {
		t.Fatalf("running the basket: %v", err)
	}

	return result
}

func TestSplitCapitalHandsOutEveryCent(t *testing.T) {
	for _, tc := range []struct{ total, n int64 }{{1_000_000, 2}, {1_000_001, 3}, {999_999, 7}, {100, 20}} {
		shares := splitCapital(tc.total, int(tc.n))

		sum := int64(0)
		for _, share := range shares {
			sum += share
		}
		if sum != tc.total {
			t.Errorf("%d split %d ways sums to %d", tc.total, tc.n, sum)
		}
		if spread := shares[0] - shares[len(shares)-1]; spread > 1 {
			t.Errorf("%d split %d ways spreads by %d cents, want at most one", tc.total, tc.n, spread)
		}
	}
}

// The whole point of the change: a stock's numbers do not depend on what it was run
// alongside, because nothing is shared between the sleeves.
func TestASleeveMatchesTheSameStockRunAlone(t *testing.T) {
	const capital = 1_000_000

	alone := runOf(t, holdSpec, testSymbol, capital/2, holdBars)
	both := basketOf(t, holdSpec, capital,
		Instrument{Symbol: testSymbol, Candles: seriesOf(holdBars)},
		Instrument{Symbol: twinSymbol, Candles: seriesOf(holdBars)},
	)

	if len(both.Metrics.Symbols) != 2 {
		t.Fatalf("per-symbol rows = %d, want two", len(both.Metrics.Symbols))
	}

	sleeve := both.Metrics.Symbols[0]
	if sleeve.Symbol != testSymbol.Ticker {
		t.Fatalf("first sleeve is %s, want %s", sleeve.Symbol, testSymbol.Ticker)
	}
	if sleeve.CapitalCents != capital/2 {
		t.Errorf("sleeve capital = %d, want half of %d", sleeve.CapitalCents, capital)
	}
	if sleeve.PnLCents != alone.Metrics.PnLCents {
		t.Errorf("sleeve profit = %d cents, want the %d it makes on its own",
			sleeve.PnLCents, alone.Metrics.PnLCents)
	}
	if sleeve.ReturnPct != alone.Metrics.ReturnPct {
		t.Errorf("sleeve return = %.4f%%, want %.4f%%", sleeve.ReturnPct, alone.Metrics.ReturnPct)
	}
	if sleeve.Sharpe != alone.Metrics.Sharpe {
		t.Errorf("sleeve Sharpe = %.4f, want %.4f", sleeve.Sharpe, alone.Metrics.Sharpe)
	}
	if sleeve.MaxDrawdown.Pct != alone.Metrics.MaxDrawdown.Pct {
		t.Errorf("sleeve drawdown = %.4f%%, want %.4f%%",
			sleeve.MaxDrawdown.Pct, alone.Metrics.MaxDrawdown.Pct)
	}
}

func TestTheAggregateCurveIsTheSumOfItsSleeves(t *testing.T) {
	const capital = 1_000_000

	both := basketOf(t, holdSpec, capital,
		Instrument{Symbol: testSymbol, Candles: seriesOf(holdBars)},
		Instrument{Symbol: twinSymbol, Candles: seriesOf(holdBars)},
	)

	if len(both.Sleeves) != 2 {
		t.Fatalf("sleeves = %d, want two", len(both.Sleeves))
	}
	if both.Equity[0].Cents != capital {
		t.Errorf("the curve starts at %d cents, want the run's capital %d", both.Equity[0].Cents, capital)
	}

	for _, sleeve := range both.Sleeves {
		if len(sleeve.Equity) != len(both.Equity) {
			t.Fatalf("%s has %d points, want the aggregate's %d: sleeves are stored on the union timeline",
				sleeve.Symbol, len(sleeve.Equity), len(both.Equity))
		}
	}

	for i, point := range both.Equity {
		sum := int64(0)
		for _, sleeve := range both.Sleeves {
			if !sleeve.Equity[i].TS.Equal(point.TS) {
				t.Fatalf("%s point %d is stamped %s, want %s", sleeve.Symbol, i, sleeve.Equity[i].TS, point.TS)
			}
			sum += sleeve.Equity[i].Cents
		}
		if sum != point.Cents {
			t.Errorf("bar %d: sleeves add to %d cents, aggregate says %d", i, sum, point.Cents)
		}
	}
}

func TestBothStocksTradeBecauseNothingCompetesForCash(t *testing.T) {
	both := basketOf(t, holdSpec, 1_000_000,
		Instrument{Symbol: testSymbol, Candles: seriesOf(holdBars)},
		Instrument{Symbol: twinSymbol, Candles: seriesOf(holdBars)},
	)

	if len(both.Trades) != 2 {
		t.Fatalf("trades = %d, want one per symbol (%+v)", len(both.Trades), both.Trades)
	}

	// The old shared-cash run offered one seat at a time; these two hold the same bars.
	if !both.Trades[0].EntryTS.Equal(both.Trades[1].EntryTS) {
		t.Errorf("entries at %s and %s, want both stocks in on the same bar",
			both.Trades[0].EntryTS, both.Trades[1].EntryTS)
	}
	if both.Metrics.SkippedEntries != 0 {
		t.Errorf("skipped entries = %d, want none: no sleeve can crowd out another",
			both.Metrics.SkippedEntries)
	}
}

// Seq is half of the trades table's primary key, so the sleeves' own numbering cannot
// survive the merge.
func TestMergedTradesAreRenumberedAcrossSleeves(t *testing.T) {
	both := basketOf(t, holdSpec, 1_000_000,
		Instrument{Symbol: testSymbol, Candles: seriesOf(holdBars)},
		Instrument{Symbol: twinSymbol, Candles: seriesOf(holdBars)},
	)

	for i, trade := range both.Trades {
		if want := int32(i + 1); trade.Seq != want {
			t.Errorf("trade %d has seq %d, want %d", i, trade.Seq, want)
		}
	}
}

func TestPerSymbolRowsAddUpToTheRun(t *testing.T) {
	both := basketOf(t, holdSpec, 1_000_000,
		Instrument{Symbol: testSymbol, Candles: seriesOf(holdBars)},
		Instrument{Symbol: twinSymbol, Candles: seriesOf(holdBars)},
	)

	total, contribution := int64(0), 0.0
	for _, stats := range both.Metrics.Symbols {
		if stats.Trades != 1 {
			t.Errorf("%s traded %d times, want once", stats.Symbol, stats.Trades)
		}
		total += stats.PnLCents
		contribution += stats.ContributionPct
	}

	if total != both.Metrics.PnLCents {
		t.Errorf("the per-symbol rows add up to %d cents, want the run's %d", total, both.Metrics.PnLCents)
	}
	if diff := contribution - both.Metrics.ReturnPct; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("contributions add to %.6f%%, want the run's return %.6f%%", contribution, both.Metrics.ReturnPct)
	}
}

func TestASymbolMissingABarDoesNotShiftTheRestOfTheBasket(t *testing.T) {
	full := seriesOf(holdBars)
	gapped := []market.Candle{full[0], full[1], full[3]}

	both := basketOf(t, holdSpec, 1_000_000,
		Instrument{Symbol: testSymbol, Candles: full},
		Instrument{Symbol: twinSymbol, Candles: gapped},
	)

	// The timeline is the union, so the halted symbol costs nobody a bar.
	if both.Metrics.Bars != len(full) {
		t.Errorf("bars = %d, want the %d of the union", both.Metrics.Bars, len(full))
	}
	if len(both.Equity) != len(full) {
		t.Errorf("equity points = %d, want %d", len(both.Equity), len(full))
	}
	if len(both.Trades) != 2 {
		t.Fatalf("trades = %d, want one per symbol (%+v)", len(both.Trades), both.Trades)
	}

	for _, trade := range both.Trades {
		if !trade.EntryTS.Equal(full[1].TS) {
			t.Errorf("%s entered at %s, want its own second bar (%s)", trade.Symbol, trade.EntryTS, full[1].TS)
		}
		if !trade.ExitTS.Equal(full[3].TS) {
			t.Errorf("%s left at %s, want the last bar it has (%s)", trade.Symbol, trade.ExitTS, full[3].TS)
		}
	}
}

func TestTheHoldBenchmarkSplitsCapitalAcrossTheBasket(t *testing.T) {
	both := basketOf(t, holdSpec, 1_000_000,
		Instrument{Symbol: testSymbol, Candles: seriesOf(holdBars)},
		Instrument{Symbol: twinSymbol, Candles: seriesOf(holdBars)},
	)

	mark := both.Metrics.Benchmarks[0]
	if mark.Unavailable != "" {
		t.Fatalf("buy and hold unavailable: %s", mark.Unavailable)
	}
	if mark.Symbol != "TEST, TWIN" {
		t.Errorf("benchmark symbol = %q, want both names", mark.Symbol)
	}
	if both.Hold[0] != 1_000_000 {
		t.Errorf("the hold curve starts at %d cents, want the run's capital", both.Hold[0])
	}
}

// A single-symbol run is not routed through the merge, but it still has to carry the
// per-symbol row the report reads.
func TestASingleSymbolRunStillDescribesItsStock(t *testing.T) {
	const capital = 1_000_000
	alone := runOf(t, holdSpec, testSymbol, capital, holdBars)

	if len(alone.Sleeves) != 0 {
		t.Errorf("sleeves = %d, want none: the run's own curve is the stock's curve", len(alone.Sleeves))
	}
	if len(alone.Metrics.Symbols) != 1 {
		t.Fatalf("per-symbol rows = %d, want one", len(alone.Metrics.Symbols))
	}

	stats := alone.Metrics.Symbols[0]
	if stats.CapitalCents != capital {
		t.Errorf("sleeve capital = %d, want the whole %d", stats.CapitalCents, capital)
	}
	if stats.ReturnPct != alone.Metrics.ReturnPct {
		t.Errorf("sleeve return = %.4f%%, want the run's %.4f%%", stats.ReturnPct, alone.Metrics.ReturnPct)
	}
}
