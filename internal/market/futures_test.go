package market

import (
	"math"
	"testing"
	"time"
)

func onDay(t *testing.T, text string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.DateOnly, text)
	if err != nil {
		t.Fatalf("parsing %q: %v", text, err)
	}
	return parsed
}

// Three contracts rolling twice. A and B overlap for three sessions before A expires,
// B and C for the whole window, so every roll has a session where both still settle.
func winFixture(t *testing.T) []FuturesQuote {
	t.Helper()

	expA, expB, expC := onDay(t, "2025-06-18"), onDay(t, "2025-08-13"), onDay(t, "2025-10-15")

	rows := []struct {
		day        string
		symbol     string
		expiration time.Time
		settlement float64
	}{
		{"2025-06-16", "A", expA, 100}, {"2025-06-16", "B", expB, 103}, {"2025-06-16", "C", expC, 106},
		{"2025-06-17", "A", expA, 101}, {"2025-06-17", "B", expB, 104}, {"2025-06-17", "C", expC, 107},
		{"2025-06-18", "A", expA, 102}, {"2025-06-18", "B", expB, 105}, {"2025-06-18", "C", expC, 108},
		{"2025-06-19", "B", expB, 106}, {"2025-06-19", "C", expC, 109},
		{"2025-06-20", "B", expB, 107}, {"2025-06-20", "C", expC, 110},
		{"2025-08-13", "B", expB, 110}, {"2025-08-13", "C", expC, 113},
		{"2025-08-14", "C", expC, 114},
	}

	quotes := make([]FuturesQuote, 0, len(rows))
	for _, row := range rows {
		quotes = append(quotes, FuturesQuote{
			Symbol:     row.symbol,
			Expiration: row.expiration,
			Multiplier: 0.2,
			Day:        onDay(t, row.day),
			Settlement: row.settlement,
		})
	}
	return quotes
}

func TestBuildContinuousRollsToTheNearestUnexpiredContract(t *testing.T) {
	series, err := BuildContinuous("WIN", winFixture(t), ContinuousOptions{})
	if err != nil {
		t.Fatalf("BuildContinuous: %v", err)
	}

	want := []string{"A", "A", "A", "B", "B", "B", "C"}
	if len(series.Bars) != len(want) {
		t.Fatalf("got %d bars, want %d", len(series.Bars), len(want))
	}
	for i, symbol := range want {
		if series.Bars[i].Symbol != symbol {
			t.Errorf("bar %d (%s) took contract %s, want %s",
				i, series.Bars[i].Day.Format(time.DateOnly), series.Bars[i].Symbol, symbol)
		}
	}

	if len(series.Rolls) != 2 {
		t.Fatalf("got %d rolls, want 2", len(series.Rolls))
	}
	if got := series.Rolls[0].Day.Format(time.DateOnly); got != "2025-06-19" {
		t.Errorf("first roll landed on %s, want 2025-06-19", got)
	}
	if got := series.Rolls[1].Day.Format(time.DateOnly); got != "2025-08-14" {
		t.Errorf("second roll landed on %s, want 2025-08-14", got)
	}
}

// The outgoing contract has no bar on the roll day; it expired the session before. A gap
// measured on the roll day itself finds one contract and silently reports zero, which makes
// back-adjustment a no-op and yields a smooth, wrong series.
func TestBuildContinuousMeasuresTheRollGapOnTheLastOverlappingSession(t *testing.T) {
	series, err := BuildContinuous("WIN", winFixture(t), ContinuousOptions{})
	if err != nil {
		t.Fatalf("BuildContinuous: %v", err)
	}

	for i, roll := range series.Rolls {
		if !roll.Measured {
			t.Errorf("roll %d (%s→%s) was not measured", i, roll.From, roll.To)
		}
		if roll.Gap != 3 {
			t.Errorf("roll %d (%s→%s) gap = %v, want 3", i, roll.From, roll.To, roll.Gap)
		}
	}
}

// The point of back-adjustment: a day-over-day change in the adjusted series must equal the
// change in the contract actually held, including across a roll.
func TestBackAdjustPreservesTheHeldContractsDailyChange(t *testing.T) {
	quotes := winFixture(t)

	raw, err := BuildContinuous("WIN", quotes, ContinuousOptions{})
	if err != nil {
		t.Fatalf("BuildContinuous raw: %v", err)
	}
	adjusted, err := BuildContinuous("WIN", quotes, ContinuousOptions{BackAdjust: true})
	if err != nil {
		t.Fatalf("BuildContinuous adjusted: %v", err)
	}

	settlements := map[string]float64{}
	for _, quote := range quotes {
		settlements[quote.Day.Format(time.DateOnly)+"/"+quote.Symbol] = quote.Settlement
	}

	for i := 1; i < len(adjusted.Bars); i++ {
		prev, cur := adjusted.Bars[i-1], adjusted.Bars[i]

		held := cur.Symbol
		before, ok := settlements[prev.Day.Format(time.DateOnly)+"/"+held]
		if !ok {
			t.Fatalf("no %s settlement on %s to compare against", held, prev.Day.Format(time.DateOnly))
		}
		after := settlements[cur.Day.Format(time.DateOnly)+"/"+held]

		wantChange := after - before
		gotChange := cur.Settlement - prev.Settlement

		if math.Abs(gotChange-wantChange) > 1e-9 {
			t.Errorf("%s→%s: adjusted moved %v, but holding %s moved %v",
				prev.Day.Format(time.DateOnly), cur.Day.Format(time.DateOnly), gotChange, held, wantChange)
		}
	}

	if raw.Bars[len(raw.Bars)-1].Settlement != adjusted.Bars[len(adjusted.Bars)-1].Settlement {
		t.Error("back-adjustment moved the most recent bar; it is the reference and must not shift")
	}
}

func TestBackAdjustAppliesCumulativeGapsBackwards(t *testing.T) {
	series, err := BuildContinuous("WIN", winFixture(t), ContinuousOptions{BackAdjust: true})
	if err != nil {
		t.Fatalf("BuildContinuous: %v", err)
	}

	want := map[string]float64{
		"2025-06-16": 106, // 100 + 3 + 3
		"2025-06-17": 107,
		"2025-06-18": 108,
		"2025-06-19": 109, // 106 + 3
		"2025-06-20": 110,
		"2025-08-13": 113,
		"2025-08-14": 114, // reference, unadjusted
	}

	for _, bar := range series.Bars {
		key := bar.Day.Format(time.DateOnly)
		if got := bar.Settlement; got != want[key] {
			t.Errorf("%s adjusted to %v, want %v", key, got, want[key])
		}
	}
}

func TestBuildContinuousWithoutBackAdjustLeavesSettlementsRaw(t *testing.T) {
	series, err := BuildContinuous("WIN", winFixture(t), ContinuousOptions{})
	if err != nil {
		t.Fatalf("BuildContinuous: %v", err)
	}

	if first := series.Bars[0].Settlement; first != 100 {
		t.Errorf("first raw settlement = %v, want 100", first)
	}
	for _, bar := range series.Bars {
		if bar.Adjustment != 0 {
			t.Errorf("%s carries adjustment %v with BackAdjust off", bar.Day.Format(time.DateOnly), bar.Adjustment)
		}
	}
}

func TestBuildContinuousReportsAnUnmeasurableRoll(t *testing.T) {
	expA, expB := onDay(t, "2025-06-18"), onDay(t, "2025-08-13")

	quotes := []FuturesQuote{
		{Symbol: "A", Expiration: expA, Day: onDay(t, "2025-06-17"), Settlement: 100},
		{Symbol: "A", Expiration: expA, Day: onDay(t, "2025-06-18"), Settlement: 102},
		{Symbol: "B", Expiration: expB, Day: onDay(t, "2025-06-19"), Settlement: 106},
	}

	series, err := BuildContinuous("WIN", quotes, ContinuousOptions{BackAdjust: true})
	if err != nil {
		t.Fatalf("BuildContinuous: %v", err)
	}
	if len(series.Rolls) != 1 {
		t.Fatalf("got %d rolls, want 1", len(series.Rolls))
	}
	if series.Rolls[0].Measured {
		t.Error("a roll with no overlapping session reported Measured = true")
	}
	if series.Rolls[0].Gap != 0 {
		t.Errorf("unmeasurable roll gap = %v, want 0", series.Rolls[0].Gap)
	}
}

func TestBuildContinuousRollOffsetLeavesTheContractEarly(t *testing.T) {
	series, err := BuildContinuous("WIN", winFixture(t), ContinuousOptions{RollOffsetDays: 2})
	if err != nil {
		t.Fatalf("BuildContinuous: %v", err)
	}

	// With a two-day offset, 2025-06-17 already needs a contract living to 2025-06-19,
	// so A is dropped a session earlier than it would be at offset zero.
	for _, bar := range series.Bars {
		if bar.Day.Format(time.DateOnly) == "2025-06-17" && bar.Symbol != "B" {
			t.Errorf("2025-06-17 took %s, want B once the roll offset is 2 days", bar.Symbol)
		}
	}
}

func TestBuildContinuousRejectsAnEmptyRoot(t *testing.T) {
	if _, err := BuildContinuous("  ", winFixture(t), ContinuousOptions{}); err == nil {
		t.Error("BuildContinuous accepted a blank root")
	}
}

func TestBuildContinuousRejectsANegativeRollOffset(t *testing.T) {
	if _, err := BuildContinuous("WIN", winFixture(t), ContinuousOptions{RollOffsetDays: -1}); err == nil {
		t.Error("BuildContinuous accepted a negative roll offset")
	}
}

func TestBuildContinuousHandlesNoQuotes(t *testing.T) {
	series, err := BuildContinuous("WIN", nil, ContinuousOptions{BackAdjust: true})
	if err != nil {
		t.Fatalf("BuildContinuous: %v", err)
	}
	if len(series.Bars) != 0 || len(series.Rolls) != 0 {
		t.Errorf("empty input produced %d bars and %d rolls", len(series.Bars), len(series.Rolls))
	}
}
