package indicator

import (
	"math"
	"testing"
)

func TestTrueRangeSpansTheGapAcrossBars(t *testing.T) {
	candles := linearRamp(40, 1)

	_, result := compute(t, "atr:14", candles)
	if result.Start != 14 {
		t.Fatalf("atr:14 starts at bar %d, want 14", result.Start)
	}

	for i, value := range result.Values[0] {
		if !within(value, 1.5) {
			t.Fatalf("bar %d: the range is %v, want the 1.5 a unit ramp gaps", result.Start+i, value)
		}
	}
}

func TestTheAverageRangeNeverGoesNegative(t *testing.T) {
	candles := syntheticWalk(600)

	_, result := compute(t, "atr:14", candles)
	for i, value := range result.Values[0] {
		if value < 0 {
			t.Fatalf("bar %d: the range is %v", result.Start+i, value)
		}
	}
}

func TestStandardDeviationIsBollingerBandsWithoutTheMultiplier(t *testing.T) {
	candles := goldenBars(t)

	_, spread := compute(t, "stddev:20", candles)
	_, bands := compute(t, "bb:20:2", candles)

	if spread.Start != bands.Start {
		t.Fatalf("stddev starts at %d against bb at %d", spread.Start, bands.Start)
	}
	for i := range spread.Values[0] {
		want := (bands.Values[0][i] - bands.Values[1][i]) / 2
		if !within(spread.Values[0][i], want) {
			t.Fatalf("bar %d: stddev is %v against half a band of %v",
				spread.Start+i, spread.Values[0][i], want)
		}
	}
}

func TestHistoricalVolatilityAnnualisesTheSpreadOfLogReturns(t *testing.T) {
	candles := goldenBars(t)

	const period = 20
	_, result := compute(t, "hv:20:252", candles)
	if result.Start != period {
		t.Fatalf("hv:20 starts at bar %d, want %d", result.Start, period)
	}

	returns := make([]float64, 0, len(candles))
	for i := 1; i < len(candles); i++ {
		returns = append(returns, math.Log(candles[i].Close/candles[i-1].Close))
	}

	for i, value := range result.Values[0] {
		window := returns[result.Start+i-period : result.Start+i]

		mean := 0.0
		for _, change := range window {
			mean += change
		}
		mean /= period

		variance := 0.0
		for _, change := range window {
			variance += (change - mean) * (change - mean)
		}

		want := 100 * math.Sqrt(variance/period) * math.Sqrt(252)
		if !within(value, want) {
			t.Fatalf("bar %d: hv is %v, want %v", result.Start+i, value, want)
		}
	}
}

func TestChaikinVolatilityIsFlatWhenTheRangeIs(t *testing.T) {
	candles := linearRamp(80, 1)

	_, result := compute(t, "cvol:10:10", candles)
	if result.Start != 19 {
		t.Fatalf("cvol:10:10 starts at bar %d, want 19", result.Start)
	}

	for i, value := range result.Values[0] {
		if math.Abs(value) > 1e-9 {
			t.Fatalf("bar %d: the range widened by %v%% while it never changed", result.Start+i, value)
		}
	}
}
