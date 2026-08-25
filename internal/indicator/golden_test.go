package indicator

import (
	"encoding/json"
	"io/fs"
	"math"
	"os"
	"testing"
	"time"
)

const tolerance = 1e-6

type fixtureCandle struct {
	TS     string  `json:"ts"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

type fixtureBars struct {
	Symbol    string          `json:"symbol"`
	Timeframe string          `json:"timeframe"`
	Candles   []fixtureCandle `json:"candles"`
}

type goldenCase struct {
	Key    string               `json:"key"`
	Start  int                  `json:"start"`
	Series map[string][]float64 `json:"series"`
}

type goldenFile struct {
	Bars  string       `json:"bars"`
	Cases []goldenCase `json:"cases"`
}

func repoFS() fs.FS { return os.DirFS("../..") }

func readFixture(t *testing.T, name string, dst any) {
	t.Helper()

	raw, err := fs.ReadFile(repoFS(), "testdata/"+name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatalf("parsing %s: %v", name, err)
	}
}

func goldenBars(t *testing.T) []Candle {
	t.Helper()

	var bars fixtureBars
	readFixture(t, "indicator_bars.json", &bars)

	candles := make([]Candle, 0, len(bars.Candles))
	for i, entry := range bars.Candles {
		ts, err := time.Parse(time.RFC3339, entry.TS)
		if err != nil {
			t.Fatalf("candle %d: %v", i, err)
		}
		candles = append(candles, Candle{
			TS:     ts.UTC(),
			Open:   entry.Open,
			High:   entry.High,
			Low:    entry.Low,
			Close:  entry.Close,
			Volume: entry.Volume,
		})
	}

	return candles
}

func golden(t *testing.T) goldenFile {
	t.Helper()

	var file goldenFile
	readFixture(t, "indicator_golden.json", &file)
	if len(file.Cases) == 0 {
		t.Fatal("the golden file holds no cases")
	}

	return file
}

func TestGoldenValuesMatchTheReferenceImplementation(t *testing.T) {
	candles := goldenBars(t)
	if len(candles) != 200 {
		t.Fatalf("the fixture holds %d bars, want the 200 the plan asks for", len(candles))
	}

	for _, tc := range golden(t).Cases {
		t.Run(tc.Key, func(t *testing.T) {
			instance, err := Parse(tc.Key)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.Key, err)
			}
			if instance.Key != tc.Key {
				t.Errorf("%q canonicalises to %q", tc.Key, instance.Key)
			}

			result := Compute(instance.Indicator, candles)
			if result.Start != tc.Start {
				t.Fatalf("first value at index %d, want %d", result.Start, tc.Start)
			}
			if len(result.Values) != len(instance.Spec.Outputs) {
				t.Fatalf("%d output series, want %d", len(result.Values), len(instance.Spec.Outputs))
			}

			for i, name := range instance.Spec.Outputs {
				want, ok := tc.Series[name]
				if !ok {
					t.Fatalf("the golden file has no %q series", name)
				}
				got := result.Values[i]
				if len(got) != len(want) {
					t.Fatalf("%s has %d values, want %d", name, len(got), len(want))
				}
				for j := range want {
					if math.Abs(got[j]-want[j]) > tolerance {
						t.Errorf("%s[%d] = %.9f, want %.6f (bar %s)",
							name, j, got[j], want[j], candles[tc.Start+j].TS.Format(time.DateOnly))
					}
				}
			}
		})
	}
}

func TestGoldenValuesSurviveAHandCheck(t *testing.T) {
	candles := goldenBars(t)

	closes := make([]float64, 0, 5)
	for _, candle := range candles[:5] {
		closes = append(closes, candle.Close)
	}

	sum := 0.0
	for _, price := range closes {
		sum += price
	}
	mean := sum / 5

	spread := 0.0
	for _, price := range closes {
		spread += (price - mean) * (price - mean)
	}
	spread = math.Sqrt(spread / 5)

	sma, err := Parse("sma:5")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	first := Compute(sma.Indicator, candles[:5])
	if first.Start != 4 {
		t.Fatalf("sma:5 emits from index %d over five bars, want 4", first.Start)
	}
	if got := first.Values[0][0]; math.Abs(got-mean) > tolerance {
		t.Errorf("sma:5 over the first five bars is %.9f, want %.9f", got, mean)
	}

	bands, err := Parse("bb:5:2")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	banded := Compute(bands.Indicator, candles[:5])
	for i, want := range []float64{mean + 2*spread, mean, mean - 2*spread} {
		if got := banded.Values[i][0]; math.Abs(got-want) > tolerance {
			t.Errorf("bb:5:2 %s is %.9f, want %.9f", bands.Spec.Outputs[i], got, want)
		}
	}
}

func TestEveryRegisteredIndicatorEmitsExactlyAtItsWarmup(t *testing.T) {
	candles := goldenBars(t)

	for _, spec := range Catalog() {
		t.Run(spec.Name, func(t *testing.T) {
			instance, err := New(spec.Name, nil, "")
			if err != nil {
				t.Fatalf("New(%s): %v", spec.Name, err)
			}

			result := Compute(instance.Indicator, candles)
			if result.Start != instance.Indicator.Warmup() {
				t.Errorf("first value at index %d, but Warmup() reports %d",
					result.Start, instance.Indicator.Warmup())
			}
			for i, series := range result.Values {
				if len(series) != len(candles)-result.Start {
					t.Errorf("%s has %d values, want %d", spec.Outputs[i], len(series), len(candles)-result.Start)
				}
				for j, value := range series {
					if math.IsNaN(value) || math.IsInf(value, 0) {
						t.Fatalf("%s[%d] is %v", spec.Outputs[i], j, value)
					}
				}
			}
		})
	}
}

func TestNothingIsEmittedBeforeReady(t *testing.T) {
	candles := goldenBars(t)

	for _, spec := range Catalog() {
		instance, err := New(spec.Name, nil, "")
		if err != nil {
			t.Fatalf("New(%s): %v", spec.Name, err)
		}

		warmup := instance.Indicator.Warmup()
		if warmup >= len(candles) {
			continue
		}

		instance.Indicator.Reset()
		for i, candle := range candles {
			instance.Indicator.Update(candle)
			if ready := instance.Indicator.Ready(); ready != (i >= warmup) {
				t.Fatalf("%s: Ready() is %v at bar %d, with Warmup() = %d", spec.Name, ready, i, warmup)
			}
		}
	}
}

func TestResetMakesAnIndicatorReusable(t *testing.T) {
	candles := goldenBars(t)

	for _, tc := range golden(t).Cases {
		instance, err := Parse(tc.Key)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.Key, err)
		}

		first := Compute(instance.Indicator, candles)
		Feed(instance.Indicator, candles)
		second := Compute(instance.Indicator, candles)

		if first.Start != second.Start {
			t.Errorf("%s starts at %d then %d after a reset", tc.Key, first.Start, second.Start)
		}
		for i := range first.Values {
			for j := range first.Values[i] {
				if first.Values[i][j] != second.Values[i][j] {
					t.Fatalf("%s output %d value %d is %v on the first pass and %v on the second",
						tc.Key, i, j, first.Values[i][j], second.Values[i][j])
				}
			}
		}
	}
}
