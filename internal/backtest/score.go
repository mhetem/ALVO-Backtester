package backtest

import (
	"math"

	"github.com/mhetem/ALVO-Backtester/internal/sweep"
)

// Score is what ranks one point of a sweep against another. A run that never traded is not
// scored at all rather than scored zero: zero is a real number a losing strategy can
// reach, and letting "did nothing" tie with it is how a walk-forward ends up testing a
// parameter set that never opened a position.
func (m Metrics) Score(objective string) (float64, bool) {
	if m.Trades == 0 {
		return 0, false
	}

	switch objective {
	case sweep.ObjectiveReturn:
		return m.ReturnPct, true
	case sweep.ObjectiveCAGR:
		return m.CAGRPct, true
	case sweep.ObjectiveSharpe:
		return m.Sharpe, true
	case sweep.ObjectiveSortino:
		return m.Sortino, true
	case sweep.ObjectiveCalmar:
		return m.Calmar, true
	case sweep.ObjectiveExpectancy:
		return float64(m.ExpectancyCents), true
	case sweep.ObjectiveProfitFactor:
		// A run with no losing trade has no profit factor to print, but it does have a
		// place in a ranking, and that place is the top.
		if m.ProfitFactor == nil {
			return math.Inf(1), true
		}
		return *m.ProfitFactor, true
	}

	return 0, false
}
