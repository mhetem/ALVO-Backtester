package indicator

import (
	"math"
	"testing"
	"time"
)

const halfATick = 0.005

func syntheticWalk(bars int) []Candle {
	state := uint64(20260825)
	noise := func() float64 {
		state = state*6364136223846793005 + 1442695040888963407
		return float64(state>>11)/float64(uint64(1)<<53)*2 - 1
	}

	candles := make([]Candle, 0, bars)
	ts := time.Date(2020, 1, 2, 13, 0, 0, 0, time.UTC)
	price := 30.0

	for range bars {
		open := price
		price = math.Max(price+0.2*noise(), 1)
		high := math.Max(open, price) + 0.1*math.Abs(noise())
		low := math.Min(open, price) - 0.1*math.Abs(noise())

		candles = append(candles, Candle{
			TS:     ts,
			Open:   open,
			High:   high,
			Low:    low,
			Close:  price,
			Volume: 1_000_000 + 1000*math.Abs(noise()),
		})
		ts = ts.AddDate(0, 0, 1)
	}

	return candles
}

func TestPrimingReproducesWhatFullHistoryWouldHaveComputed(t *testing.T) {
	candles := syntheticWalk(2000)
	split := len(candles) / 2

	for _, key := range []string{"sma:20", "sma:200", "ema:9", "ema:21", "rsi:14", "rsi:2", "macd:12:26:9", "bb:20:2"} {
		t.Run(key, func(t *testing.T) {
			instance, err := Parse(key)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			depth := PrimeBars([]Instance{instance})
			if depth >= split {
				t.Fatalf("priming wants %d bars of a %d-bar run-up, so this proves nothing", depth, split)
			}

			full := Compute(instance.Indicator, candles)
			instance.Indicator.Reset()
			Feed(instance.Indicator, candles[split-depth:split])
			paged := Emit(instance.Indicator, candles[split:])

			if paged.Start != 0 {
				t.Fatalf("the primed page starts emitting at index %d, want the very first bar", paged.Start)
			}

			worst := 0.0
			for i, series := range paged.Values {
				want := full.Values[i][split-full.Start:]
				if len(series) != len(want) {
					t.Fatalf("%s has %d primed values against %d from full history",
						instance.Spec.Outputs[i], len(series), len(want))
				}
				for j := range series {
					worst = math.Max(worst, math.Abs(series[j]-want[j]))
				}
			}

			if worst > halfATick {
				t.Errorf("priming with %d bars leaves the page off by %g, more than half a tick", depth, worst)
			}
			t.Logf("primed with %d bars, worst disagreement %g", depth, worst)
		})
	}
}

func TestAnUnprimedPageIsVisiblyWrong(t *testing.T) {
	candles := goldenBars(t)
	split := len(candles) / 2

	instance, err := Parse("sma:20")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	full := Compute(instance.Indicator, candles)
	cold := Compute(instance.Indicator, candles[split:])

	if cold.Start != instance.Indicator.Warmup() {
		t.Fatalf("a cold page starts at %d, want a full warmup of %d", cold.Start, instance.Indicator.Warmup())
	}
	if full.Values[0][split-full.Start] == cold.Values[0][0] {
		t.Error("a cold page happens to match full history, so this test proves nothing")
	}
}

func TestComputeIsAFoldOverUpdate(t *testing.T) {
	candles := goldenBars(t)

	instance, err := Parse("macd:12:26:9")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	batch := Compute(instance.Indicator, candles)

	instance.Indicator.Reset()
	streamed := make([][]float64, len(instance.Spec.Outputs))
	start := len(candles)
	for i, candle := range candles {
		instance.Indicator.Update(candle)
		if !instance.Indicator.Ready() {
			continue
		}
		start = min(start, i)
		for j, value := range instance.Indicator.Values() {
			streamed[j] = append(streamed[j], value)
		}
	}

	if start != batch.Start {
		t.Fatalf("streaming starts at %d, Compute at %d", start, batch.Start)
	}
	for i := range streamed {
		for j := range streamed[i] {
			if streamed[i][j] != batch.Values[i][j] {
				t.Fatalf("output %d value %d is %v streamed and %v batched", i, j, streamed[i][j], batch.Values[i][j])
			}
		}
	}
}
