package backtest

import (
	"testing"

	"github.com/mhetem/ALVO-Backtester/internal/market"
)

var twinSymbol = Symbol{ID: 2, Ticker: "TWIN", LotSize: 1, TickSize: 0.01}

func basketOf(t *testing.T, spec string, capital int64, positions int, held ...Instrument) Result {
	t.Helper()

	result, err := Run(Request{
		Plan:         planOf(t, spec),
		Instruments:  held,
		MaxPositions: positions,
		Timeframe:    market.TF1d,
		Capital:      capital,
	})
	if err != nil {
		t.Fatalf("running the basket: %v", err)
	}

	return result
}

func TestABasketOfTwoRunsBothSymbolsOnOnePileOfCash(t *testing.T) {
	const capital = 1_000_000

	alone := runOf(t, holdSpec, testSymbol, capital, holdBars)
	both := basketOf(t, holdSpec, capital, 2,
		Instrument{Symbol: testSymbol, Candles: seriesOf(holdBars)},
		Instrument{Symbol: twinSymbol, Candles: seriesOf(holdBars)},
	)

	if len(both.Trades) != 2 {
		t.Fatalf("trades = %d, want one per symbol (%+v)", len(both.Trades), both.Trades)
	}
	if want := 2 * alone.Metrics.PnLCents; both.Metrics.PnLCents != want {
		t.Errorf("basket profit = %d cents, want %d: two symbols on the same bars, and the cash covers both",
			both.Metrics.PnLCents, want)
	}

	if len(both.Metrics.Symbols) != 2 {
		t.Fatalf("per-symbol rows = %d, want two", len(both.Metrics.Symbols))
	}

	total := int64(0)
	for _, stats := range both.Metrics.Symbols {
		if stats.Trades != 1 {
			t.Errorf("%s traded %d times, want once", stats.Symbol, stats.Trades)
		}
		total += stats.PnLCents
	}
	if total != both.Metrics.PnLCents {
		t.Errorf("the per-symbol rows add up to %d cents, want the run's %d", total, both.Metrics.PnLCents)
	}
}

func TestMaxPositionsCapsHowMuchOfTheBasketIsHeldAtOnce(t *testing.T) {
	both := basketOf(t, holdSpec, 1_000_000, 1,
		Instrument{Symbol: testSymbol, Candles: seriesOf(holdBars)},
		Instrument{Symbol: twinSymbol, Candles: seriesOf(holdBars)},
	)

	if len(both.Trades) == 0 {
		t.Fatal("nothing traded at all")
	}
	if both.Trades[0].Symbol != testSymbol.Ticker {
		t.Errorf("the first seat went to %s, want %s: the basket is offered in order",
			both.Trades[0].Symbol, testSymbol.Ticker)
	}
	if both.Metrics.CrowdedOut == 0 {
		t.Error("crowded_out = 0, want the entries that found no seat counted rather than dropped")
	}
	if both.Metrics.SkippedEntries < both.Metrics.CrowdedOut {
		t.Errorf("skipped entries = %d, want at least the %d crowded out",
			both.Metrics.SkippedEntries, both.Metrics.CrowdedOut)
	}

	// The cap is a promise about what is held at once, not about how many trades a run
	// takes: a position closed on bar i frees its seat for bar i, so two trades may touch
	// at an endpoint. Anything more than touching means two were open together.
	for i, held := range both.Trades {
		for _, other := range both.Trades[i+1:] {
			if held.EntryTS.Before(other.ExitTS) && other.EntryTS.Before(held.ExitTS) {
				t.Errorf("trades %d and %d overlap (%s-%s and %s-%s), but only one seat was on offer",
					held.Seq, other.Seq, held.EntryTS, held.ExitTS, other.EntryTS, other.ExitTS)
			}
		}
	}
}

func TestTheLastBarFillsAnIntentTheRunThenCloses(t *testing.T) {
	// The seat freed by an end-of-run exit is a real seat: the waiting symbol's intent was
	// formed at the previous close, and the engine cannot decline it on the grounds that
	// this bar turned out to be the last one without reading the future.
	both := basketOf(t, holdSpec, 1_000_000, 1,
		Instrument{Symbol: testSymbol, Candles: seriesOf(holdBars)},
		Instrument{Symbol: twinSymbol, Candles: seriesOf(holdBars)},
	)

	last := both.Trades[len(both.Trades)-1]
	if last.Symbol != twinSymbol.Ticker {
		t.Fatalf("the last trade is %s, want %s to take the seat the first one gave up",
			last.Symbol, twinSymbol.Ticker)
	}
	if !last.EntryTS.Equal(both.Trades[0].ExitTS) {
		t.Errorf("%s entered at %s, want the bar %s left on (%s)",
			last.Symbol, last.EntryTS, both.Trades[0].Symbol, both.Trades[0].ExitTS)
	}
	if last.ExitReason != ReasonEndOfRun {
		t.Errorf("exit reason = %q, want %q", last.ExitReason, ReasonEndOfRun)
	}
}

func TestASymbolMissingABarDoesNotShiftTheRestOfTheBasket(t *testing.T) {
	full := seriesOf(holdBars)
	gapped := []market.Candle{full[0], full[1], full[3]}

	both := basketOf(t, holdSpec, 1_000_000, 2,
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
	both := basketOf(t, holdSpec, 1_000_000, 2,
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
