package indicator

import (
	"math"
	"testing"
)

func assertBounded(t *testing.T, instance Instance, result Result, low, high float64) {
	t.Helper()

	for i, series := range result.Values {
		for j, value := range series {
			if value < low || value > high {
				t.Fatalf("%s %s[%d] is %v, outside %v..%v",
					instance.Key, instance.Spec.Outputs[i], result.Start+j, value, low, high)
			}
		}
	}
}

func TestTheBoundedOscillatorsStayInsideTheirRange(t *testing.T) {
	candles := goldenBars(t)

	cases := []struct {
		key       string
		low, high float64
	}{
		{"stoch:14:3:3", 0, 100},
		{"stochrsi:14:14:3:3", 0, 100},
		{"rsi:14", 0, 100},
		{"mfi:14", 0, 100},
		{"willr:14", -100, 0},
		{"aroon:25", -100, 100},
		{"adx:14", 0, 100},
	}

	for _, tc := range cases {
		instance, result := compute(t, tc.key, candles)
		assertBounded(t, instance, result, tc.low, tc.high)
	}
}

func TestWilliamsRIsTheStochasticShiftedOntoItsOwnScale(t *testing.T) {
	candles := goldenBars(t)

	_, williams := compute(t, "willr:14", candles)
	_, stochastic := compute(t, "stoch:14:1:1", candles)

	if williams.Start != stochastic.Start {
		t.Fatalf("willr starts at %d against an unsmoothed stochastic at %d", williams.Start, stochastic.Start)
	}
	for i := range williams.Values[0] {
		if want := stochastic.Values[0][i] - 100; !within(williams.Values[0][i], want) {
			t.Fatalf("bar %d: willr is %v, want %v", williams.Start+i, williams.Values[0][i], want)
		}
	}
}

func TestMomentumAndRateOfChangeReadAStraightRamp(t *testing.T) {
	candles := linearRamp(60, 1)

	_, moved := compute(t, "mom:10", candles)
	if moved.Start != 10 {
		t.Fatalf("mom:10 starts at bar %d, want 10", moved.Start)
	}
	for i, value := range moved.Values[0] {
		if !within(value, 10) {
			t.Fatalf("bar %d: a unit ramp moved %v over ten bars, want 10", moved.Start+i, value)
		}
	}

	_, rate := compute(t, "roc:12", candles)
	for i, value := range rate.Values[0] {
		bar := rate.Start + i
		want := 100 * 12 / (candles[bar].Close - 12)
		if !within(value, want) {
			t.Fatalf("bar %d: roc:12 is %v, want %v", bar, value, want)
		}
	}
}

func TestCCIReadsTheSameDistanceAllTheWayUpARamp(t *testing.T) {
	candles := linearRamp(80, 1)

	_, result := compute(t, "cci:20", candles)
	if result.Start != 19 {
		t.Fatalf("cci:20 starts at bar %d, want 19", result.Start)
	}

	for i, value := range result.Values[0] {
		if !within(value, 9.5/(cciFactor*5)) {
			t.Fatalf("bar %d: cci is %v, want a constant %v", result.Start+i, value, 9.5/(cciFactor*5))
		}
	}
}

func TestDirectionalMovementReadsAPureUptrend(t *testing.T) {
	candles := linearRamp(120, 1)

	_, result := compute(t, "adx:14", candles)
	if result.Start != 27 {
		t.Fatalf("adx:14 starts at bar %d, want 2*14-1", result.Start)
	}

	last := len(result.Values[0]) - 1
	if !within(result.Values[0][last], 100) {
		t.Errorf("adx is %v at the end of a straight climb, want 100", result.Values[0][last])
	}
	if !within(result.Values[1][last], 100.0/1.5) {
		t.Errorf("+di is %v, want 100/1.5", result.Values[1][last])
	}
	if result.Values[2][last] != 0 {
		t.Errorf("-di is %v inside an uptrend, want 0", result.Values[2][last])
	}
}

func TestAroonPinsBothEndsOnAMonotoneRun(t *testing.T) {
	candles := linearRamp(80, 1)

	_, result := compute(t, "aroon:25", candles)
	if result.Start != 25 {
		t.Fatalf("aroon:25 starts at bar %d, want 25", result.Start)
	}

	for i := range result.Values[0] {
		bar := result.Start + i
		if result.Values[0][i] != 100 || result.Values[1][i] != 0 {
			t.Fatalf("bar %d: aroon reads up %v down %v inside a straight climb",
				bar, result.Values[0][i], result.Values[1][i])
		}
		if want := result.Values[0][i] - result.Values[1][i]; result.Values[2][i] != want {
			t.Fatalf("bar %d: the oscillator is %v, want %v", bar, result.Values[2][i], want)
		}
	}
}

func TestStochasticRSIRunsAStochasticOverTheRSISeries(t *testing.T) {
	candles := goldenBars(t)

	const stoch = 14
	_, strength := compute(t, "rsi:14", candles)
	_, stretched := compute(t, "stochrsi:14:14:1:1", candles)

	for i := range stretched.Values[0] {
		at := stretched.Start + i - strength.Start
		window := strength.Values[0][at-stoch+1 : at+1]

		low, high := window[0], window[0]
		for _, value := range window {
			low, high = math.Min(low, value), math.Max(high, value)
		}

		want := neutralStochastic
		if high > low {
			want = 100 * (strength.Values[0][at] - low) / (high - low)
		}
		if !within(stretched.Values[0][i], want) {
			t.Fatalf("bar %d: the stochastic rsi is %v, want %v", stretched.Start+i, stretched.Values[0][i], want)
		}
	}
}
