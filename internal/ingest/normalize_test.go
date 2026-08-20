package ingest

import (
	"encoding/json"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/brapi"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

func repoFS() fs.FS { return os.DirFS("../..") }

func testCalendar(t *testing.T) *market.Calendar {
	t.Helper()

	calendar, err := market.LoadCalendar(repoFS(), market.HolidaysFile)
	if err != nil {
		t.Fatalf("LoadCalendar: %v", err)
	}
	return calendar
}

func loadBars(t *testing.T, name string) []brapi.Bar {
	t.Helper()

	raw, err := fs.ReadFile(repoFS(), "testdata/"+name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}

	var payload struct {
		Results []brapi.Quote `json:"results"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("parsing %s: %v", name, err)
	}
	if len(payload.Results) == 0 {
		t.Fatalf("%s has no results", name)
	}

	return payload.Results[0].HistoricalDataPrice
}

func TestNormalizeDailyBarsLandOnTheSessionOpen(t *testing.T) {
	calendar := testCalendar(t)
	bars := loadBars(t, "brapi_petr4_1d.json")

	candles, rejected := Normalize(calendar, market.TF1d, bars)
	if len(rejected) != 0 {
		t.Errorf("rejected %d daily bars from a committed fixture: %+v", len(rejected), rejected[:min(3, len(rejected))])
	}
	if len(candles) == 0 {
		t.Fatal("the daily fixture normalized to nothing")
	}

	for _, candle := range candles {
		session, ok := calendar.Session(candle.TS)
		if !ok {
			t.Fatalf("%s is not a trading day", candle.TS.Format(time.RFC3339))
		}
		if !candle.TS.Equal(session.Open) {
			t.Fatalf("daily bar stored at %s, want the session open %s",
				candle.TS.Format(time.RFC3339), session.Open.Format(time.RFC3339))
		}
		if candle.TS.Location() != time.UTC {
			t.Fatalf("daily bar stored in %s, want UTC", candle.TS.Location())
		}
	}

	for i := 1; i < len(candles); i++ {
		if !candles[i].TS.After(candles[i-1].TS) {
			t.Fatalf("normalized candles are not strictly ascending at index %d", i)
		}
	}
}

func TestNormalizeKeepsBrapiLocalMidnightOnTheRightDay(t *testing.T) {
	calendar := testCalendar(t)
	bars := loadBars(t, "brapi_petr4_1d.json")

	candles, _ := Normalize(calendar, market.TF1d, bars)

	for _, bar := range bars {
		raw := bar.TS().In(calendar.Location())
		want := calendar.Date(raw.Date())

		found := false
		for _, candle := range candles {
			session, _ := calendar.Session(candle.TS)
			if session.Day.Equal(want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("brapi dated a bar %s, which normalized onto no session: an off-by-one in the timezone conversion",
				raw.Format(time.RFC3339))
		}
	}
}

func TestNormalizeIntradayBarsAlignToFiveMinuteBuckets(t *testing.T) {
	calendar := testCalendar(t)
	bars := loadBars(t, "brapi_petr4_5m.json")

	candles, rejected := Normalize(calendar, market.TF5m, bars)
	if len(rejected) != 0 {
		t.Errorf("rejected %d intraday bars from a committed fixture: %+v", len(rejected), rejected[:min(3, len(rejected))])
	}

	for _, candle := range candles {
		session, ok := calendar.Session(candle.TS)
		if !ok {
			t.Fatalf("%s is not a trading day", candle.TS.Format(time.RFC3339))
		}
		if offset := candle.TS.Sub(session.Open); offset%(5*time.Minute) != 0 || offset < 0 {
			t.Fatalf("bar at %s sits %s past the session open, which is not a 5m boundary",
				candle.TS.Format(time.RFC3339), offset)
		}
	}
}

func TestNormalizeRejectsBarsOffTheTradingCalendar(t *testing.T) {
	calendar := testCalendar(t)
	saturday := calendar.Date(2026, time.August, 22)
	holiday := calendar.Date(2026, time.September, 7)

	bars := []brapi.Bar{
		{Date: saturday.Unix(), Open: 10, High: 11, Low: 9, Close: 10, Volume: 1},
		{Date: holiday.Unix(), Open: 10, High: 11, Low: 9, Close: 10, Volume: 1},
		{Date: calendar.Date(2026, time.August, 21).Unix(), Open: 10, High: 11, Low: 9, Close: 10, Volume: 1},
	}

	candles, rejected := Normalize(calendar, market.TF1d, bars)
	if len(candles) != 1 {
		t.Errorf("normalized %d candles, want only the Friday", len(candles))
	}
	if len(rejected) != 2 {
		t.Fatalf("rejected %d bars, want the Saturday and Independence Day", len(rejected))
	}
	for _, rejection := range rejected {
		if rejection.Reason == "" {
			t.Error("every rejection needs a reason a human can act on")
		}
	}
}

func TestNormalizeRejectsImpossibleBars(t *testing.T) {
	calendar := testCalendar(t)
	friday := calendar.Date(2026, time.August, 21).Unix()

	bars := []brapi.Bar{{Date: friday, Open: 10, High: 9, Low: 11, Close: 10, Volume: 1}}

	candles, rejected := Normalize(calendar, market.TF1d, bars)
	if len(candles) != 0 || len(rejected) != 1 {
		t.Fatalf("a bar whose high is below its low must be rejected, got %d candles and %d rejections", len(candles), len(rejected))
	}
}

func TestNormalizeFoldsBarsSharingABucket(t *testing.T) {
	calendar := testCalendar(t)
	session, _ := calendar.Session(calendar.Date(2026, time.August, 20))

	first := session.Open
	second := session.Open.Add(2 * time.Minute)

	bars := []brapi.Bar{
		{Date: second.Unix(), Open: 10.5, High: 12, Low: 10.4, Close: 11, Volume: 30},
		{Date: first.Unix(), Open: 10, High: 10.6, Low: 9.5, Close: 10.5, Volume: 20},
	}

	candles, rejected := Normalize(calendar, market.TF5m, bars)
	if len(rejected) != 0 {
		t.Fatalf("unexpected rejections: %+v", rejected)
	}
	if len(candles) != 1 {
		t.Fatalf("folded to %d candles, want 1", len(candles))
	}

	got := candles[0]
	if !got.TS.Equal(session.Open) {
		t.Errorf("folded bucket opens at %s, want %s", got.TS, session.Open)
	}
	if got.Open != 10 || got.High != 12 || got.Low != 9.5 || got.Close != 11 || got.Volume != 50 {
		t.Errorf("folded to O%g H%g L%g C%g V%d, want O10 H12 L9.5 C11 V50",
			got.Open, got.High, got.Low, got.Close, got.Volume)
	}
}

func TestNormalizeTreatsNullAdjustedCloseAsAbsent(t *testing.T) {
	calendar := testCalendar(t)

	intraday, _ := Normalize(calendar, market.TF5m, loadBars(t, "brapi_petr4_5m.json"))
	for _, candle := range intraday {
		if candle.AdjClose != nil {
			t.Fatalf("brapi sends no adjusted close on 5m bars, but %s carries %g",
				candle.TS.Format(time.RFC3339), *candle.AdjClose)
		}
	}

	daily, _ := Normalize(calendar, market.TF1d, loadBars(t, "brapi_petr4_1d.json"))
	adjusted, absent := 0, 0
	for _, candle := range daily {
		if candle.AdjClose == nil {
			absent++
			continue
		}
		adjusted++
	}
	if adjusted == 0 {
		t.Error("the daily fixture should carry adjusted closes")
	}
	if absent == 0 {
		t.Error("the daily fixture is supposed to include a bar whose adjustedClose is null")
	}
}

func TestFoldedIntradayReproducesTheOfficialDailyRange(t *testing.T) {
	calendar := testCalendar(t)

	intraday, _ := Normalize(calendar, market.TF5m, loadBars(t, "brapi_petr4_5m.json"))
	daily, _ := Normalize(calendar, market.TF1d, loadBars(t, "brapi_petr4_1d.json"))

	official := map[string]market.Candle{}
	for _, candle := range daily {
		session, _ := calendar.Session(candle.TS)
		official[session.Day.Format(time.DateOnly)] = candle
	}

	folded := map[string]market.Candle{}
	order := []string{}
	for _, candle := range intraday {
		session, _ := calendar.Session(candle.TS)
		key := session.Day.Format(time.DateOnly)

		current, seen := folded[key]
		if !seen {
			folded[key] = candle
			order = append(order, key)
			continue
		}
		current.High = max(current.High, candle.High)
		current.Low = min(current.Low, candle.Low)
		current.Close = candle.Close
		current.Volume += candle.Volume
		folded[key] = current
	}

	checked := 0
	for _, key := range order {
		want, ok := official[key]
		if !ok {
			continue
		}
		got := folded[key]
		checked++

		if got.Open != want.Open || got.High != want.High || got.Low != want.Low {
			t.Errorf("%s folds to O%g H%g L%g but the stored daily bar is O%g H%g L%g",
				key, got.Open, got.High, got.Low, want.Open, want.High, want.Low)
		}
		if got.Close == want.Close {
			t.Errorf("%s folds to the official close %g; the closing auction is supposed to make these differ, "+
				"which is why 1d is stored rather than folded", key, got.Close)
		}
		if got.Close > want.High || got.Close < want.Low {
			t.Errorf("%s folds to a close of %g, outside the day's %g-%g range", key, got.Close, want.Low, want.High)
		}
	}

	if checked == 0 {
		t.Fatal("the 5m and 1d fixtures do not overlap, so nothing was reconciled")
	}
}
