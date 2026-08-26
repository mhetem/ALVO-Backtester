package backtest

import (
	"math"

	"github.com/mhetem/ALVO-Backtester/internal/market"
)

const (
	maxImpliedYield = 0.30

	BasisTotal = "total_return"
	BasisPrice = "price_return"
)

type distributions struct {
	perShare   []float64
	events     int
	unadjusted int
	unpriced   int
}

func distributionsOf(candles []market.Candle) distributions {
	found := distributions{perShare: make([]float64, len(candles))}

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

		cash := prev.Close * (1 - before/after)
		if cash <= 0 || math.IsNaN(cash) || math.IsInf(cash, 0) {
			continue
		}

		if cash > prev.Close*maxImpliedYield {
			found.unpriced++
			continue
		}

		found.perShare[i] = cash
		found.events++
	}

	return found
}

func ratioOf(candle market.Candle) float64 {
	if candle.AdjClose == nil || candle.Close <= 0 {
		return 0
	}
	return *candle.AdjClose / candle.Close
}

func (d distributions) at(i int) float64 {
	if i < 0 || i >= len(d.perShare) {
		return 0
	}
	return d.perShare[i]
}

func (d distributions) basis() string {
	if d.unadjusted == len(d.perShare) {
		return BasisPrice
	}
	return BasisTotal
}
