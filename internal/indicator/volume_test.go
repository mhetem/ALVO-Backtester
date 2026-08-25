package indicator

import "testing"

func TestOnBalanceVolumeAddsEveryUpBar(t *testing.T) {
	candles := linearRamp(50, 1)

	_, result := compute(t, "obv", candles)
	if result.Start != 1 {
		t.Fatalf("obv starts at bar %d, want 1", result.Start)
	}

	for i, value := range result.Values[0] {
		bar := result.Start + i
		if want := 1000 * float64(bar); value != want {
			t.Fatalf("bar %d: obv is %v, want %v", bar, value, want)
		}
	}
}

func TestTheCumulativeSeriesAnchorAtThePageAndTheOthersDoNot(t *testing.T) {
	anchored := map[string]bool{"obv": true, "ad": true}

	for _, spec := range Catalog() {
		instance, err := New(spec.Name, nil, "")
		if err != nil {
			t.Fatalf("New(%s): %v", spec.Name, err)
		}

		_, ok := instance.Indicator.(Anchorer)
		if ok != anchored[spec.Name] {
			t.Errorf("%s anchors: %v, want %v", spec.Name, ok, anchored[spec.Name])
		}
	}
}

func TestAnchoringMakesACumulativeSeriesIndependentOfHowDeepItPrimed(t *testing.T) {
	candles := syntheticWalk(800)
	split := len(candles) / 2

	for _, key := range []string{"obv", "ad"} {
		instance, err := Parse(key)
		if err != nil {
			t.Fatalf("Parse(%q): %v", key, err)
		}

		runs := make([]Result, 0, 2)
		for _, depth := range []int{5, 300} {
			instance.Indicator.Reset()
			Feed(instance.Indicator, candles[split-depth:split])
			Anchor(instance.Indicator)
			runs = append(runs, Emit(instance.Indicator, candles[split:]))
		}

		if runs[0].Start != runs[1].Start {
			t.Fatalf("%s starts at %d after a short prime and %d after a long one",
				key, runs[0].Start, runs[1].Start)
		}
		for i := range runs[0].Values[0] {
			if runs[0].Values[0][i] != runs[1].Values[0][i] {
				t.Fatalf("%s value %d is %v after a short prime and %v after a long one",
					key, i, runs[0].Values[0][i], runs[1].Values[0][i])
			}
		}
	}
}

func TestVolumeWeightedAveragesCollapseToTheirPlainFormWhenVolumeIsFlat(t *testing.T) {
	candles := linearRamp(60, 0.25)

	_, weighted := compute(t, "vwma:20", candles)
	_, plain := compute(t, "sma:20", candles)

	for i := range weighted.Values[0] {
		if !within(weighted.Values[0][i], plain.Values[0][i]) {
			t.Fatalf("bar %d: vwma is %v against an sma of %v",
				weighted.Start+i, weighted.Values[0][i], plain.Values[0][i])
		}
	}
}

func TestVolumeMovingAverageAveragesVolume(t *testing.T) {
	candles := goldenBars(t)

	const period = 20
	_, result := compute(t, "volma:20", candles)

	for i, value := range result.Values[0] {
		bar := result.Start + i

		want := 0.0
		for _, candle := range candles[bar-period+1 : bar+1] {
			want += candle.Volume
		}
		want /= period

		if !within(value, want) {
			t.Fatalf("bar %d: the volume average is %v, want %v", bar, value, want)
		}
	}
}

func TestMoneyFlowReadsOneHundredWhenNothingFlowsOut(t *testing.T) {
	candles := linearRamp(40, 1)

	_, result := compute(t, "mfi:14", candles)
	if result.Start != 14 {
		t.Fatalf("mfi:14 starts at bar %d, want 14", result.Start)
	}

	for i, value := range result.Values[0] {
		if value != 100 {
			t.Fatalf("bar %d: money flow is %v inside a straight climb, want 100", result.Start+i, value)
		}
	}
}

func TestTheChaikinOscillatorIsBlindToWhereTheAccumulationLineStarted(t *testing.T) {
	candles := syntheticWalk(400)

	instance, err := Parse("chaikin:3:10")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	full := Compute(instance.Indicator, candles)

	instance.Indicator.Reset()
	shifted := instance.Indicator.(*ChaikinOscillator)
	shifted.inner.total = 1e9
	primed := Emit(instance.Indicator, candles)

	for i := range full.Values[0] {
		if !within(primed.Values[0][i], full.Values[0][i]) {
			t.Fatalf("value %d is %v from a shifted line against %v from a fresh one",
				i, primed.Values[0][i], full.Values[0][i])
		}
	}
}
