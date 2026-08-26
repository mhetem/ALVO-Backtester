package strategy

import (
	"slices"
	"testing"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/indicator"
)

func closes(values ...float64) []indicator.Candle {
	start := time.Date(2026, 1, 5, 13, 0, 0, 0, time.UTC)
	out := make([]indicator.Candle, 0, len(values))

	for i, value := range values {
		out = append(out, indicator.Candle{
			TS:     start.AddDate(0, 0, i),
			Open:   value,
			High:   value,
			Low:    value,
			Close:  value,
			Volume: 1000,
		})
	}

	return out
}

func entries(plan *Plan, candles []indicator.Candle) []bool {
	tape := plan.NewTape()
	fired := make([]bool, 0, len(candles))

	for _, candle := range candles {
		tape.Push(candle)
		fired = append(fired, tape.Entry(&plan.Long))
	}

	return fired
}

func firesOn(t *testing.T, entry string, candles []indicator.Candle, want []bool) {
	t.Helper()

	plan := mustCompile(t, parts{
		inputs: `{"fast": {"indicator": "sma", "params": {"period": 3}}}`,
		entry:  `{"long": ` + entry + `}`,
		exit:   "null",
	}.json())

	if got := entries(plan, candles); !slices.Equal(got, want) {
		t.Errorf("%s fired %v, want %v", entry, got, want)
	}
}

func TestARuleOnlyEverReadsBackwards(t *testing.T) {
	firesOn(t,
		`{"gt": ["close", {"ref": ["close", 1]}]}`,
		closes(1, 2, 3, 2, 4),
		[]bool{false, true, true, false, true},
	)
}

func TestACrossingNeedsTheBarBeforeIt(t *testing.T) {
	firesOn(t,
		`{"crosses_above": ["close", 10]}`,
		closes(9, 10, 11, 10, 12),
		[]bool{false, false, true, false, true},
	)
}

func TestNothingFiresWhileAnIndicatorIsStillWarming(t *testing.T) {
	firesOn(t,
		`{"gt": ["fast", 0]}`,
		closes(1, 2, 3, 4),
		[]bool{false, false, true, true},
	)
}

func TestNotOfSomethingUnknownStaysUnknown(t *testing.T) {
	firesOn(t,
		`{"not": {"gt": ["fast", 100]}}`,
		closes(1, 2, 3, 4),
		[]bool{false, false, true, true},
	)
}

func TestAllStopsAtTheFirstThingItKnowsIsFalse(t *testing.T) {
	firesOn(t,
		`{"all": [{"lt": ["close", 0]}, {"gt": ["fast", 0]}]}`,
		closes(1, 2, 3, 4),
		[]bool{false, false, false, false},
	)
}

func TestAnyFiresOnTheFirstThingItKnowsIsTrue(t *testing.T) {
	firesOn(t,
		`{"any": [{"gt": ["close", 0]}, {"gt": ["fast", 0]}]}`,
		closes(1, 2, 3, 4),
		[]bool{true, true, true, true},
	)
}

func TestRisingCountsBarsRatherThanValues(t *testing.T) {
	firesOn(t,
		`{"rising": ["close", 2]}`,
		closes(1, 2, 3, 3, 4, 5, 6),
		[]bool{false, false, true, false, false, true, true},
	)
}

func TestFallingIsRisingTurnedOver(t *testing.T) {
	firesOn(t,
		`{"falling": ["close", 1]}`,
		closes(3, 2, 2, 1),
		[]bool{false, true, false, true},
	)
}

func TestASpanIncludesItsEdges(t *testing.T) {
	firesOn(t,
		`{"between": ["close", 10, 20]}`,
		closes(9, 10, 15, 20, 21),
		[]bool{false, true, true, true, false},
	)
}

func TestEqualityToleratesTheDriftFloatsCarry(t *testing.T) {
	firesOn(t,
		`{"eq": ["close", 0.3]}`,
		closes(0.1+0.2, 0.4),
		[]bool{true, false},
	)
}

func TestVolumeIsAnOperandLikeAnyPrice(t *testing.T) {
	plan := mustCompile(t, parts{
		inputs: `{"fast": {"indicator": "sma", "params": {"period": 3}}}`,
		entry:  `{"long": {"gt": ["volume", 900]}}`,
		exit:   "null",
	}.json())

	candles := closes(1, 2)
	candles[1].Volume = 100

	if got := entries(plan, candles); !slices.Equal(got, []bool{true, false}) {
		t.Errorf("fired %v, want the first bar only", got)
	}
}

func TestAChainedInputReadsTheOneAboveIt(t *testing.T) {
	plan := mustCompile(t, parts{
		inputs: `{"base": {"indicator": "sma", "params": {"period": 2}}, "smooth": {"indicator": "sma", "params": {"period": 2}, "source": "base"}}`,
		entry:  `{"long": {"gt": ["smooth", 0]}}`,
		exit:   "null",
	}.json())

	tape := plan.NewTape()
	candles := closes(2, 4, 6, 8)
	seen := make([]float64, 0, len(candles))
	fired := make([]bool, 0, len(candles))

	for _, candle := range candles {
		tape.Push(candle)
		fired = append(fired, tape.Entry(&plan.Long))

		value, known := tape.Value("smooth")
		if !known {
			value = -1
		}
		seen = append(seen, value)
	}

	if !slices.Equal(fired, []bool{false, false, true, true}) {
		t.Errorf("fired %v, want nothing until both averages are ready", fired)
	}
	if !slices.Equal(seen, []float64{-1, -1, 4, 6}) {
		t.Errorf("smooth = %v, want the average of the averages once it exists", seen)
	}
}

func TestTheSameSpecOverTheSameCandlesSignalsIdentically(t *testing.T) {
	body := planExample
	candles := closes(10, 11, 12, 11, 10, 9, 10, 12, 14, 13, 15, 16, 14, 13, 12,
		11, 13, 15, 17, 19, 18, 17, 16, 18, 20, 22, 21, 20, 19, 21, 23, 25)

	first := mustCompile(t, body)
	second := mustCompile(t, body)

	if !slices.Equal(entries(first, candles), entries(second, candles)) {
		t.Error("two compilations of one spec disagreed over the same candles")
	}

	replayed := entries(first, candles)
	if !slices.Equal(entries(first, candles), replayed) {
		t.Error("a plan reused for a second run did not reset cleanly")
	}
}

func TestATapeRefusesToReadPastItsOwnHistory(t *testing.T) {
	plan := mustCompile(t, parts{entry: `{"long": {"gt": [{"ref": ["close", 3]}, 0]}}`, exit: "null"}.json())

	tape := plan.NewTape()
	tape.Push(closes(5)[0])

	if _, known := tape.Field("close", 3); known {
		t.Error("a tape holding one bar answered for a bar four back")
	}
	if _, known := tape.Field("close", 0); !known {
		t.Error("a tape holding one bar could not answer for that bar")
	}
	if tape.Bars() != 1 {
		t.Errorf("bars = %d, want 1", tape.Bars())
	}
}
