package backtest

import (
	"testing"
	"testing/fstest"

	"github.com/mhetem/ALVO-Backtester/internal/market"
)

const borrowFile = `{
  "source": "test",
  "basis": 252,
  "through": "2030-01-01",
  "default_annual_pct": 10.0,
  "rates": {},
  "unavailable": []
}`

const haltedBorrowFile = `{
  "source": "test",
  "basis": 252,
  "through": "2030-01-01",
  "default_annual_pct": 10.0,
  "rates": {"TEST": [{"from": "2020-01-01", "annual_pct": 45.0}]},
  "unavailable": [{"ticker": "TEST", "from": "2026-01-01", "to": "2026-02-01"}]
}`

func borrowOf(t *testing.T, body string) *market.Borrow {
	t.Helper()

	fsys := fstest.MapFS{"borrow.json": &fstest.MapFile{Data: []byte(body)}}

	borrow, err := market.LoadBorrow(fsys, "borrow.json")
	if err != nil {
		t.Fatalf("loading the borrow curve: %v", err)
	}

	return borrow
}

func TestAShortPaysToBorrowWhatItSold(t *testing.T) {
	free := runOf(t, shortHoldSpec, testSymbol, 1_000_000, holdBars)

	charged, err := Run(Request{
		Plan:        planOf(t, shortHoldSpec),
		Instruments: oneOf(testSymbol, seriesOf(holdBars)),
		Timeframe:   market.TF1d,
		Capital:     1_000_000,
		Borrow:      borrowOf(t, borrowFile),
	})
	if err != nil {
		t.Fatalf("running the backtest: %v", err)
	}

	if free.Metrics.BorrowCents != 0 {
		t.Errorf("a run with no borrow curve paid %d cents, want nothing", free.Metrics.BorrowCents)
	}
	if charged.Metrics.BorrowCents <= 0 {
		t.Fatalf("borrow = %d cents, want a positive fee on a short held for three bars",
			charged.Metrics.BorrowCents)
	}

	// The fee is the only difference between the two runs, so it has to account for the
	// whole gap: a free short is exactly the borrow cost better off.
	if got := free.Metrics.PnLCents - charged.Metrics.PnLCents; got != charged.Metrics.BorrowCents {
		t.Errorf("the borrow moved the run by %d cents, want %d", got, charged.Metrics.BorrowCents)
	}

	if trade := onlyTrade(t, charged); trade.BorrowCents != charged.Metrics.BorrowCents {
		t.Errorf("the trade carries %d cents of borrow, want %d", trade.BorrowCents, charged.Metrics.BorrowCents)
	}
}

func TestALongNeverPaysBorrow(t *testing.T) {
	result, err := Run(Request{
		Plan:        planOf(t, holdSpec),
		Instruments: oneOf(testSymbol, seriesOf(holdBars)),
		Timeframe:   market.TF1d,
		Capital:     1_000_000,
		Borrow:      borrowOf(t, borrowFile),
	})
	if err != nil {
		t.Fatalf("running the backtest: %v", err)
	}

	if result.Metrics.BorrowCents != 0 {
		t.Errorf("a long paid %d cents to borrow, want nothing: it owns the shares",
			result.Metrics.BorrowCents)
	}
}

func TestAHardToBorrowNameCannotBeShorted(t *testing.T) {
	result, err := Run(Request{
		Plan:        planOf(t, shortHoldSpec),
		Instruments: oneOf(testSymbol, seriesOf(holdBars)),
		Timeframe:   market.TF1d,
		Capital:     1_000_000,
		Borrow:      borrowOf(t, haltedBorrowFile),
	})
	if err != nil {
		t.Fatalf("running the backtest: %v", err)
	}

	if len(result.Trades) != 0 {
		t.Fatalf("trades = %d, want none: no lender would part with the shares", len(result.Trades))
	}
	if result.Metrics.ShortsUnavailable == 0 {
		t.Error("shorts_unavailable = 0, want the skipped entries counted rather than silent")
	}
	if result.Metrics.SkippedEntries < result.Metrics.ShortsUnavailable {
		t.Errorf("skipped entries = %d, want at least the %d that had nothing to borrow",
			result.Metrics.SkippedEntries, result.Metrics.ShortsUnavailable)
	}
	if result.Metrics.FinalEquityCents != 1_000_000 {
		t.Errorf("final equity = %d cents, want the capital untouched", result.Metrics.FinalEquityCents)
	}
}

func TestTheBorrowCurveReadsAPerTickerRate(t *testing.T) {
	borrow := borrowOf(t, haltedBorrowFile)

	if got := borrow.AnnualPct("TEST", seriesOf(holdBars)[0].TS); got != 45.0 {
		t.Errorf("TEST borrows at %g%%, want the 45%% the file names for it", got)
	}
	if got := borrow.AnnualPct("OTHER", seriesOf(holdBars)[0].TS); got != 10.0 {
		t.Errorf("an unlisted ticker borrows at %g%%, want the 10%% default", got)
	}
	if !borrow.Available("OTHER", seriesOf(holdBars)[0].TS) {
		t.Error("an unlisted ticker is unavailable, want available unless the file says otherwise")
	}
}
