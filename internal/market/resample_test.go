package market

import (
	"encoding/json"
	"io/fs"
	"testing"
	"time"
)

type fixtureCandle struct {
	TS     string  `json:"ts"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume int64   `json:"volume"`
}

type fixtureSeries struct {
	Timeframe string          `json:"timeframe"`
	Candles   []fixtureCandle `json:"candles"`
}

func loadFixture(t *testing.T, name string) []Candle {
	t.Helper()

	raw, err := fs.ReadFile(repoFS(), "testdata/"+name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}

	var series fixtureSeries
	if err := json.Unmarshal(raw, &series); err != nil {
		t.Fatalf("parsing %s: %v", name, err)
	}

	candles := make([]Candle, 0, len(series.Candles))
	for i, entry := range series.Candles {
		ts, err := time.Parse(time.RFC3339, entry.TS)
		if err != nil {
			t.Fatalf("%s: candle %d: %v", name, i, err)
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

func committedCalendar(t *testing.T) *Calendar {
	t.Helper()

	calendar, err := LoadCalendar(repoFS(), HolidaysFile)
	if err != nil {
		t.Fatalf("LoadCalendar: %v", err)
	}
	return calendar
}

func TestResampleMatchesTheHandCheckedReference(t *testing.T) {
	calendar := committedCalendar(t)
	base := loadFixture(t, "synthetic_5m.json")

	for _, tc := range []struct {
		target Timeframe
		file   string
	}{
		{TF15m, "synthetic_15m.json"},
		{TF30m, "synthetic_30m.json"},
		{TF1h, "synthetic_1h.json"},
	} {
		t.Run(string(tc.target), func(t *testing.T) {
			want := loadFixture(t, tc.file)

			got, err := Resample(calendar, base, tc.target)
			if err != nil {
				t.Fatalf("Resample: %v", err)
			}
			if len(got) != len(want) {
				t.Fatalf("resampled to %d candles, want %d", len(got), len(want))
			}

			for i := range want {
				if !got[i].TS.Equal(want[i].TS) {
					t.Fatalf("candle %d opens at %s, want %s", i, got[i].TS.Format(time.RFC3339), want[i].TS.Format(time.RFC3339))
				}
				if got[i].Open != want[i].Open || got[i].High != want[i].High ||
					got[i].Low != want[i].Low || got[i].Close != want[i].Close || got[i].Volume != want[i].Volume {
					t.Errorf("candle %d at %s is O%g H%g L%g C%g V%d, want O%g H%g L%g C%g V%d",
						i, want[i].TS.Format(time.RFC3339),
						got[i].Open, got[i].High, got[i].Low, got[i].Close, got[i].Volume,
						want[i].Open, want[i].High, want[i].Low, want[i].Close, want[i].Volume)
				}
			}
		})
	}
}

func TestResampleAnchorsBucketsToTheSessionOpen(t *testing.T) {
	calendar := committedCalendar(t)
	base := loadFixture(t, "synthetic_5m.json")

	got, err := Resample(calendar, base, TF1h)
	if err != nil {
		t.Fatalf("Resample: %v", err)
	}

	session, ok := calendar.Session(calendar.Date(2026, time.August, 20))
	if !ok {
		t.Fatal("2026-08-20 should be a trading day")
	}

	if !got[0].TS.Equal(session.Open) {
		t.Errorf("first 1h bucket opens at %s, want the session open %s",
			got[0].TS.Format(time.RFC3339), session.Open.Format(time.RFC3339))
	}

	for _, candle := range got {
		day, ok := calendar.Session(candle.TS)
		if !ok {
			t.Fatalf("bucket %s does not fall on a trading day", candle.TS.Format(time.RFC3339))
		}
		if offset := candle.TS.Sub(day.Open); offset%time.Hour != 0 {
			t.Errorf("bucket %s sits %s past the session open, which is not a whole hour",
				candle.TS.Format(time.RFC3339), offset)
		}
	}
}

func TestResampleSkipsBucketsWithNoBars(t *testing.T) {
	calendar := committedCalendar(t)
	base := loadFixture(t, "synthetic_5m.json")

	got, err := Resample(calendar, base, TF15m)
	if err != nil {
		t.Fatalf("Resample: %v", err)
	}

	sessions := map[string]int{}
	for _, candle := range got {
		day, _ := calendar.Session(candle.TS)
		sessions[day.Day.Format(time.DateOnly)]++
	}

	if len(sessions) != 2 {
		t.Fatalf("resampled %d sessions, want 2", len(sessions))
	}
	for day, count := range sessions {
		if count != 28 {
			t.Errorf("%s resampled to %d 15m buckets, want 28", day, count)
		}
	}
}

func TestResampleOpenComesFromTheFirstPresentBar(t *testing.T) {
	calendar := committedCalendar(t)
	base := loadFixture(t, "synthetic_5m.json")

	session, ok := calendar.Session(calendar.Date(2026, time.August, 21))
	if !ok {
		t.Fatal("2026-08-21 should be a trading day")
	}

	var firstBar Candle
	for _, candle := range base {
		if !candle.TS.Before(session.Open) {
			firstBar = candle
			break
		}
	}
	if firstBar.TS.Equal(session.Open) {
		t.Fatal("the fixture is supposed to omit the opening bar of the second session")
	}

	got, err := Resample(calendar, base, TF15m)
	if err != nil {
		t.Fatalf("Resample: %v", err)
	}

	for _, candle := range got {
		if !candle.TS.Equal(session.Open) {
			continue
		}
		if candle.Open != firstBar.Open {
			t.Errorf("the holed opening bucket opens at %g, want the first present bar's open %g", candle.Open, firstBar.Open)
		}
		return
	}

	t.Fatalf("no 15m bucket at the second session's open %s", session.Open.Format(time.RFC3339))
}

func TestResampleRefusesStoredAndUnknownTimeframes(t *testing.T) {
	calendar := committedCalendar(t)
	base := loadFixture(t, "synthetic_5m.json")

	if _, err := Resample(calendar, base, TF1d); err == nil {
		t.Error("resampling to 1d should fail: the daily bar is stored, not folded")
	}
	if _, err := Resample(calendar, base, Timeframe("7m")); err == nil {
		t.Error("resampling to an unknown timeframe should fail")
	}
}

func TestResampleRejectsUnsortedAndMisalignedInput(t *testing.T) {
	calendar := committedCalendar(t)
	base := loadFixture(t, "synthetic_5m.json")

	unsorted := []Candle{base[1], base[0]}
	if _, err := Resample(calendar, unsorted, TF15m); err == nil {
		t.Error("resampling descending input should fail")
	}

	duplicated := []Candle{base[0], base[0]}
	if _, err := Resample(calendar, duplicated, TF15m); err == nil {
		t.Error("resampling duplicated timestamps should fail")
	}

	misaligned := []Candle{base[0]}
	misaligned[0].TS = misaligned[0].TS.Add(time.Minute)
	if _, err := Resample(calendar, misaligned, TF15m); err == nil {
		t.Error("resampling a bar that is not on a 5m boundary should fail")
	}

	weekend := []Candle{base[0]}
	weekend[0].TS = weekend[0].TS.AddDate(0, 0, 2)
	if _, err := Resample(calendar, weekend, TF15m); err == nil {
		t.Error("resampling a bar dated on a Saturday should fail")
	}
}

func TestResampleIsIdempotentOnFiveMinutes(t *testing.T) {
	calendar := committedCalendar(t)
	base := loadFixture(t, "synthetic_5m.json")

	got, err := Resample(calendar, base, TF5m)
	if err != nil {
		t.Fatalf("Resample: %v", err)
	}
	if len(got) != len(base) {
		t.Fatalf("resampling 5m to 5m produced %d candles, want %d", len(got), len(base))
	}

	for i := range base {
		if got[i] != base[i] {
			t.Fatalf("candle %d changed: got %+v, want %+v", i, got[i], base[i])
		}
	}
}
