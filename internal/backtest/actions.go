package backtest

import (
	"math"

	"github.com/mhetem/ALVO-Backtester/internal/market"
)

const (
	maxImpliedYield = 0.30
	splitTolerance  = 0.01
	flatRatio       = 1e-6

	BasisTotal = "total_return"
	BasisPrice = "price_return"
)

// The share-count multipliers a corporate action is allowed to resolve to. Every entry
// below 1 is a grouping, every entry above it a split. Nothing between 1 and 3:2 is on the
// list: an action that small reads as an implied yield under maxImpliedYield and is taken
// as a dividend instead, which is the right call for a market where 20% dividends happen
// and 5:4 splits do not.
var splitTerms = []float64{1.5, 2, 2.5, 3, 10.0 / 3, 4, 5, 6, 8, 10, 15, 20, 25, 50, 100}

type actions struct {
	perShare   []float64
	factor     []float64
	events     int
	splits     int
	unadjusted int
	unpriced   int
}

func actionsOf(candles []market.Candle) actions {
	found := actions{
		perShare: make([]float64, len(candles)),
		factor:   make([]float64, len(candles)),
	}

	for i, candle := range candles {
		if candle.AdjClose == nil {
			found.unadjusted++
		}
		if i == 0 {
			continue
		}

		prev := candles[i-1]
		before, after := ratioOf(prev), ratioOf(candle)
		if before <= 0 || after <= 0 {
			continue
		}

		ratio := before / after
		if ratio <= 0 || math.IsNaN(ratio) || math.IsInf(ratio, 0) || math.Abs(ratio-1) <= flatRatio {
			continue
		}

		cash := prev.Close * (1 - ratio)
		if cash > 0 && cash <= prev.Close*maxImpliedYield {
			found.perShare[i] = cash
			found.events++
			continue
		}

		if factor, ok := splitFactor(ratio); ok {
			found.factor[i] = factor
			found.splits++
			continue
		}

		found.unpriced++
	}

	return found
}

// A split divides the price by the same number it multiplies the share count by, so the
// adjustment ratio the data carries is the reciprocal of the factor a position needs.
// Only the terms a real corporate action uses are accepted; anything else is a price move
// too large to explain and is counted rather than acted on.
func splitFactor(ratio float64) (float64, bool) {
	want := 1 / ratio

	best, gap := 0.0, splitTolerance
	for _, term := range splitTerms {
		for _, candidate := range []float64{term, 1 / term} {
			off := math.Abs(candidate-want) / want
			if off < gap {
				best, gap = candidate, off
			}
		}
	}

	return best, best > 0
}

func ratioOf(candle market.Candle) float64 {
	if candle.AdjClose == nil || candle.Close <= 0 {
		return 0
	}
	return *candle.AdjClose / candle.Close
}

func (a actions) dividendAt(i int) float64 {
	if i < 0 || i >= len(a.perShare) {
		return 0
	}
	return a.perShare[i]
}

func (a actions) factorAt(i int) float64 {
	if i < 0 || i >= len(a.factor) {
		return 0
	}
	return a.factor[i]
}

func (a actions) basis() string {
	if a.unadjusted == len(a.perShare) {
		return BasisPrice
	}
	return BasisTotal
}
