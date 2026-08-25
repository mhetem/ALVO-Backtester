package indicator

import (
	"math"
	"slices"
	"testing"
)

func TestTheSlidingWeightedAverageMatchesTheNaiveComputation(t *testing.T) {
	candles := syntheticWalk(1000)

	const period = 20
	moving := newWMA(period)
	weight := float64(period*(period+1)) / 2

	for i, candle := range candles {
		moving.push(candle.Close)
		if i+1 < period {
			continue
		}

		want := 0.0
		for k := range period {
			want += float64(k+1) * candles[i-period+1+k].Close
		}
		want /= weight

		if math.Abs(moving.value-want) > 1e-9 {
			t.Fatalf("bar %d: the sliding update is %.12f against %.12f recomputed", i, moving.value, want)
		}
	}
}

func TestHullRoundsItsInnerPeriodsTheWayPineDoes(t *testing.T) {
	cases := []struct {
		period int
		root   int
	}{{9, 3}, {16, 4}, {20, 4}, {13, 4}, {49, 7}, {1, 1}}

	for _, tc := range cases {
		hull := NewHMA(tc.period, SourceClose)
		if hull.root != tc.root {
			t.Errorf("hma:%d smooths over %d bars, want %d", tc.period, hull.root, tc.root)
		}
		if want := tc.period + tc.root - 2; hull.Warmup() != want {
			t.Errorf("hma:%d warms up in %d bars, want %d", tc.period, hull.Warmup(), want)
		}
	}
}

func TestTheDoubleAndTripleAveragesShedTheirLagOnARamp(t *testing.T) {
	candles := linearRamp(400, 1)

	for _, key := range []string{"dema:20", "tema:20"} {
		_, result := compute(t, key, candles)

		last := len(candles) - 1
		got := result.Values[0][last-result.Start]
		if lag := candles[last].Close - got; math.Abs(lag) > 1e-6 {
			t.Errorf("%s trails a unit ramp by %v, want none of it", key, lag)
		}
	}
}

func TestKeltnerSitsOnTheExponentialAverageOfClose(t *testing.T) {
	candles := goldenBars(t)

	_, bands := compute(t, "keltner:20:2:10", candles)
	_, basis := compute(t, "ema:20", candles)

	for i := range bands.Values[1] {
		bar := bands.Start + i
		want := basis.Values[0][bar-basis.Start]
		if !within(bands.Values[1][i], want) {
			t.Fatalf("bar %d: the middle band is %v against an ema:20 of %v", bar, bands.Values[1][i], want)
		}
		if bands.Values[0][i] <= bands.Values[1][i] || bands.Values[1][i] <= bands.Values[2][i] {
			t.Fatalf("bar %d: the bands are out of order: %v", bar, []float64{
				bands.Values[0][i], bands.Values[1][i], bands.Values[2][i],
			})
		}
	}
}

func TestDonchianTracksTheHighestHighAndLowestLow(t *testing.T) {
	candles := goldenBars(t)

	const period = 20
	_, result := compute(t, "donchian:20", candles)

	for i := range result.Values[0] {
		bar := result.Start + i
		window := candles[bar-period+1 : bar+1]

		top, bottom := window[0].High, window[0].Low
		for _, candle := range window {
			top = max(top, candle.High)
			bottom = min(bottom, candle.Low)
		}

		if result.Values[0][i] != top || result.Values[2][i] != bottom {
			t.Fatalf("bar %d: the channel is %v..%v, want %v..%v",
				bar, result.Values[2][i], result.Values[0][i], bottom, top)
		}
		if want := (top + bottom) / 2; result.Values[1][i] != want {
			t.Fatalf("bar %d: the midline is %v, want %v", bar, result.Values[1][i], want)
		}
	}
}

func TestRollingVWAPCollapsesToATypicalPriceAverageWhenVolumeIsFlat(t *testing.T) {
	candles := linearRamp(60, 0.25)

	_, weighted := compute(t, "vwap:20", candles)
	_, plain := compute(t, "sma:20:source=hlc3", candles)

	if weighted.Start != plain.Start {
		t.Fatalf("vwap starts at %d against sma at %d", weighted.Start, plain.Start)
	}
	for i := range weighted.Values[0] {
		if !within(weighted.Values[0][i], plain.Values[0][i]) {
			t.Fatalf("bar %d: vwap is %v against an unweighted %v",
				weighted.Start+i, weighted.Values[0][i], plain.Values[0][i])
		}
	}
}

func TestParabolicSARStaysUnderAnUnbrokenUptrend(t *testing.T) {
	candles := linearRamp(200, 1)

	_, result := compute(t, "psar:0.02:0.2", candles)
	if result.Start != 1 {
		t.Fatalf("the first stop is at bar %d, want bar 1", result.Start)
	}

	for i := range result.Values[0] {
		bar := result.Start + i
		if result.Values[1][i] != 1 {
			t.Fatalf("bar %d: the trend flipped to %v inside a straight ramp", bar, result.Values[1][i])
		}
		if result.Values[0][i] > candles[bar].Low {
			t.Fatalf("bar %d: the stop is at %v, above a low of %v", bar, result.Values[0][i], candles[bar].Low)
		}
	}
}

func TestParabolicSARFlipsWhenThePriceTurns(t *testing.T) {
	candles := tentRamp(240, 10)

	_, result := compute(t, "psar:0.02:0.2", candles)

	flips := 0
	for i := 1; i < len(result.Values[1]); i++ {
		if result.Values[1][i] != result.Values[1][i-1] {
			flips++
		}
	}
	if flips != 1 {
		t.Errorf("the stop flipped %d times over one turn, want exactly 1", flips)
	}
}

func TestSuperTrendRidesBelowARisingMarket(t *testing.T) {
	candles := linearRamp(200, 1)

	_, result := compute(t, "supertrend:10:3", candles)

	last := len(result.Values[1]) - 1
	if result.Values[1][last] != 1 {
		t.Fatalf("the trend reads %v at the end of a straight ramp", result.Values[1][last])
	}
	if got := result.Values[0][last]; got >= candles[len(candles)-1].Close {
		t.Errorf("the trailing stop is %v, at or above a close of %v", got, candles[len(candles)-1].Close)
	}
}

func TestIchimokuComputesInPlaceAndDeclaresItsDisplacement(t *testing.T) {
	candles := goldenBars(t)

	const displacement = 26
	instance, result := compute(t, "ichimoku:9:26:52:26", candles)

	if want := []int{0, 0, displacement, displacement, -displacement}; !slices.Equal(instance.Offsets, want) {
		t.Fatalf("offsets are %v, want %v", instance.Offsets, want)
	}

	for i := range result.Values[2] {
		bar := result.Start + i

		tenkan := midpoint(candles, bar, 9)
		kijun := midpoint(candles, bar, 26)
		if want := (tenkan + kijun) / 2; !within(result.Values[2][i], want) {
			t.Fatalf("bar %d: senkou a is %v, want the %v computed at that bar",
				bar, result.Values[2][i], want)
		}
		if want := midpoint(candles, bar, 52); !within(result.Values[3][i], want) {
			t.Fatalf("bar %d: senkou b is %v, want %v", bar, result.Values[3][i], want)
		}
		if want := candles[bar].Close; !within(result.Values[4][i], want) {
			t.Fatalf("bar %d: chikou is %v, want the close of %v", bar, result.Values[4][i], want)
		}
	}
}

func TestOnlyDisplacedIndicatorsDeclareAnOffset(t *testing.T) {
	for _, key := range []string{"ema:9", "bb:20:2", "macd:12:26:9", "psar:0.02:0.2"} {
		instance, err := Parse(key)
		if err != nil {
			t.Fatalf("Parse(%q): %v", key, err)
		}
		if MaxOffset([]Instance{instance}) != 0 {
			t.Errorf("%s declares offsets %v, want every output drawn on its own bar", key, instance.Offsets)
		}
		if len(instance.Offsets) != len(instance.Spec.Outputs) {
			t.Errorf("%s has %d outputs but %d offsets", key, len(instance.Spec.Outputs), len(instance.Offsets))
		}
	}
}

func tentRamp(bars int, base float64) []Candle {
	candles := linearRamp(bars, 1)

	for i := range candles {
		shift := 2*float64(max(i-bars/2, 0)) - (base - 10)
		candles[i].Open -= shift
		candles[i].High -= shift
		candles[i].Low -= shift
		candles[i].Close -= shift
	}

	return candles
}

func midpoint(candles []Candle, at, period int) float64 {
	top, bottom := candles[at].High, candles[at].Low
	for _, candle := range candles[at-period+1 : at+1] {
		top = max(top, candle.High)
		bottom = min(bottom, candle.Low)
	}
	return (top + bottom) / 2
}
