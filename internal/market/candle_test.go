package market

import (
	"testing"
	"time"
)

func TestTimeframeShape(t *testing.T) {
	for _, tc := range []struct {
		tf       Timeframe
		stored   bool
		intraday bool
		width    time.Duration
		baseBars int
		brapi    string
	}{
		{TF5m, true, true, 5 * time.Minute, 1, "5m"},
		{TF15m, false, true, 15 * time.Minute, 3, "15m"},
		{TF30m, false, true, 30 * time.Minute, 6, "30m"},
		{TF1h, false, true, time.Hour, 12, "60m"},
		{TF1d, true, false, 0, 0, "1d"},
	} {
		if got := tc.tf.Stored(); got != tc.stored {
			t.Errorf("%s Stored() = %v, want %v", tc.tf, got, tc.stored)
		}
		if got := tc.tf.Intraday(); got != tc.intraday {
			t.Errorf("%s Intraday() = %v, want %v", tc.tf, got, tc.intraday)
		}
		if got := tc.tf.BucketWidth(); got != tc.width {
			t.Errorf("%s BucketWidth() = %s, want %s", tc.tf, got, tc.width)
		}
		if got := tc.tf.BaseBars(); got != tc.baseBars {
			t.Errorf("%s BaseBars() = %d, want %d", tc.tf, got, tc.baseBars)
		}
		if got := tc.tf.BrapiInterval(); got != tc.brapi {
			t.Errorf("%s BrapiInterval() = %q, want %q", tc.tf, got, tc.brapi)
		}
	}
}

func TestParseTimeframeRejectsWhatTheProjectDoesNotServe(t *testing.T) {
	if _, err := ParseTimeframe("1m"); err == nil {
		t.Error("1m is out of scope and should not parse")
	}
	if _, err := ParseTimeframe(" 1D "); err != nil {
		t.Errorf("timeframes should parse case- and space-insensitively: %v", err)
	}

	if _, err := ParseStoredTimeframe("15m"); err == nil {
		t.Error("15m is resampled on read and should not parse as a stored timeframe")
	}
	if _, err := ParseStoredTimeframe("5m"); err != nil {
		t.Errorf("5m is stored: %v", err)
	}
}

func TestBucketOpenAnchorsToTheSessionNotMidnight(t *testing.T) {
	calendar := committedCalendar(t)
	session, ok := calendar.Session(calendar.Date(2026, time.August, 20))
	if !ok {
		t.Fatal("2026-08-20 should be a trading day")
	}

	for _, tc := range []struct {
		tf     Timeframe
		offset time.Duration
		want   time.Duration
	}{
		{TF5m, 0, 0},
		{TF5m, 4 * time.Minute, 0},
		{TF5m, 5 * time.Minute, 5 * time.Minute},
		{TF15m, 14 * time.Minute, 0},
		{TF15m, 15 * time.Minute, 15 * time.Minute},
		{TF30m, 59 * time.Minute, 30 * time.Minute},
		{TF1h, 59 * time.Minute, 0},
		{TF1h, 61 * time.Minute, time.Hour},
		{TF1h, 419 * time.Minute, 6 * time.Hour},
	} {
		got, err := calendar.BucketOpen(tc.tf, session.Open.Add(tc.offset))
		if err != nil {
			t.Fatalf("%s at +%s: %v", tc.tf, tc.offset, err)
		}
		if want := session.Open.Add(tc.want); !got.Equal(want) {
			t.Errorf("%s at +%s bucketed to %s, want %s", tc.tf, tc.offset,
				got.Format(time.RFC3339), want.Format(time.RFC3339))
		}
	}
}

func TestBucketOpenForDailyIsTheSessionOpen(t *testing.T) {
	calendar := committedCalendar(t)
	session, ok := calendar.Session(calendar.Date(2026, time.August, 20))
	if !ok {
		t.Fatal("2026-08-20 should be a trading day")
	}

	midnight := calendar.Date(2026, time.August, 20)
	got, err := calendar.BucketOpen(TF1d, midnight)
	if err != nil {
		t.Fatalf("BucketOpen: %v", err)
	}
	if !got.Equal(session.Open) {
		t.Errorf("the daily bucket for a local-midnight timestamp is %s, want the session open %s",
			got.Format(time.RFC3339), session.Open.Format(time.RFC3339))
	}

	if got.Location() != time.UTC {
		t.Errorf("stored timestamps must be UTC, got %s", got.Location())
	}
}

func TestBucketOpenRejectsNonSessionTimestamps(t *testing.T) {
	calendar := committedCalendar(t)
	session, _ := calendar.Session(calendar.Date(2026, time.August, 20))

	if _, err := calendar.BucketOpen(TF5m, session.Open.Add(-time.Minute)); err == nil {
		t.Error("a bar before the open should be rejected, not folded into the first bucket")
	}
	if _, err := calendar.BucketOpen(TF5m, session.Close); err == nil {
		t.Error("a bar at the close should be rejected: the last bucket opens five minutes earlier")
	}
	if _, err := calendar.BucketOpen(TF1d, calendar.Date(2026, time.January, 1)); err == nil {
		t.Error("New Year's Day is a holiday and should have no bucket")
	}
	if _, err := calendar.BucketOpen(TF1d, calendar.Date(2026, time.August, 22)); err == nil {
		t.Error("2026-08-22 is a Saturday and should have no bucket")
	}
}

func TestSessionBucketsCoverAFullAndAShortenedSession(t *testing.T) {
	calendar := committedCalendar(t)

	regular, _ := calendar.Session(calendar.Date(2026, time.August, 20))
	for _, tc := range []struct {
		tf   Timeframe
		want int
	}{{TF5m, 84}, {TF15m, 28}, {TF30m, 14}, {TF1h, 7}, {TF1d, 1}} {
		if got := calendar.BucketCount(tc.tf, regular); got != tc.want {
			t.Errorf("a 420-minute session holds %d %s buckets, want %d", got, tc.tf, tc.want)
		}
	}

	buckets := calendar.SessionBuckets(TF1h, regular)
	if len(buckets) != 7 {
		t.Fatalf("SessionBuckets returned %d buckets, want 7", len(buckets))
	}
	if !buckets[0].Equal(regular.Open) {
		t.Errorf("the first bucket is %s, want the session open %s", buckets[0], regular.Open)
	}
	if last := buckets[6]; !last.Equal(regular.Open.Add(6 * time.Hour)) {
		t.Errorf("the last 1h bucket opens at %s, want six hours past the open", last)
	}

	ash, ok := calendar.Session(calendar.Date(2026, time.February, 18))
	if !ok {
		t.Fatal("Ash Wednesday is a shortened session, not a closed day")
	}
	if got, want := calendar.BucketCount(TF1h, ash), 4; got != want {
		t.Errorf("the 13:00-17:00 Ash Wednesday session holds %d 1h buckets, want %d", got, want)
	}
}

func TestDayBoundsSpanWholeExchangeDays(t *testing.T) {
	calendar := committedCalendar(t)

	start, end := calendar.DayBounds(calendar.Date(2026, time.August, 20), calendar.Date(2026, time.August, 21))
	session, _ := calendar.Session(calendar.Date(2026, time.August, 21))

	if !start.Before(session.Open) {
		t.Errorf("the window starts at %s, which is not before the last day's open %s", start, session.Open)
	}
	if !end.After(session.Close) {
		t.Errorf("the window ends at %s, which does not include the last day's close %s", end, session.Close)
	}
	if got := end.Sub(start); got != 48*time.Hour {
		t.Errorf("a two-day window spans %s, want 48h", got)
	}
}

func TestCandleValidateCatchesImpossibleBars(t *testing.T) {
	ts := time.Date(2026, time.August, 20, 13, 0, 0, 0, time.UTC)
	sound := Candle{TS: ts, Open: 10, High: 11, Low: 9, Close: 10.5, Volume: 100}

	if err := sound.Validate(); err != nil {
		t.Fatalf("a sound candle failed validation: %v", err)
	}

	for name, mutate := range map[string]func(*Candle){
		"zero timestamp":   func(c *Candle) { c.TS = time.Time{} },
		"zero price":       func(c *Candle) { c.Close = 0 },
		"high below low":   func(c *Candle) { c.High, c.Low = 9, 11 },
		"high below close": func(c *Candle) { c.Close = 12 },
		"low above open":   func(c *Candle) { c.Low = 10.2 },
		"negative volume":  func(c *Candle) { c.Volume = -1 },
		"zero adjusted close": func(c *Candle) {
			zero := 0.0
			c.AdjClose = &zero
		},
	} {
		broken := sound
		mutate(&broken)
		if err := broken.Validate(); err == nil {
			t.Errorf("%s should fail validation", name)
		}
	}
}
