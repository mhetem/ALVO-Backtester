package backtest

import (
	"math"
	"testing"

	"github.com/mhetem/ALVO-Backtester/internal/sweep"
)

func TestScoreIgnoresARunThatNeverTraded(t *testing.T) {
	// Zero is a number a losing strategy reaches. A run that never opened a position has
	// not earned it, and letting the two tie is how a walk-forward promotes a spec that
	// does nothing at all.
	idle := Metrics{Trades: 0, Sharpe: 0, ReturnPct: 0}

	for _, objective := range sweep.Objectives {
		if _, ok := idle.Score(objective); ok {
			t.Errorf("a run with no trades scored on %s", objective)
		}
	}
}

func TestScorePutsARunWithNoLosingTradeAtTheTop(t *testing.T) {
	factor := 2.5
	mixed := Metrics{Trades: 10, ProfitFactor: &factor}
	flawless := Metrics{Trades: 10}

	best, ok := flawless.Score(sweep.ObjectiveProfitFactor)
	if !ok {
		t.Fatal("a run with no losing trade did not score at all")
	}
	if !math.IsInf(best, 1) {
		t.Errorf("score = %g, want positive infinity: there is no loss to divide by", best)
	}

	other, _ := mixed.Score(sweep.ObjectiveProfitFactor)
	if !(best > other) {
		t.Errorf("a flawless run scored %g against %g, want it to rank higher", best, other)
	}
}

func TestScoreReadsEachObjectiveOffItsOwnField(t *testing.T) {
	metrics := Metrics{
		Trades:          4,
		ReturnPct:       12,
		CAGRPct:         6,
		Sharpe:          1.5,
		Sortino:         2.5,
		Calmar:          0.75,
		ExpectancyCents: 900,
	}

	for objective, want := range map[string]float64{
		sweep.ObjectiveReturn:     12,
		sweep.ObjectiveCAGR:       6,
		sweep.ObjectiveSharpe:     1.5,
		sweep.ObjectiveSortino:    2.5,
		sweep.ObjectiveCalmar:     0.75,
		sweep.ObjectiveExpectancy: 900,
	} {
		got, ok := metrics.Score(objective)
		if !ok {
			t.Errorf("%s did not score", objective)
			continue
		}
		if got != want {
			t.Errorf("%s = %g, want %g", objective, got, want)
		}
	}

	if _, ok := metrics.Score("nonsense"); ok {
		t.Error("an unknown objective scored")
	}
}
