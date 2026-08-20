package market

import (
	"testing"
	"testing/fstest"
	"time"
)

const testCalendarBody = `{
  "timezone": "America/Sao_Paulo",
  "session": {"open": "10:00", "close": "17:00"},
  "holidays": [
    {"date": "2026-01-01", "name": "Confraternizacao Universal"},
    {"date": "2026-02-16", "name": "Carnaval"},
    {"date": "2026-02-18", "name": "Quarta-feira de Cinzas", "open": "13:00"}
  ]
}`

func testCalendar(t *testing.T) *Calendar {
	t.Helper()

	fsys := fstest.MapFS{"b3.json": jsonFile(testCalendarBody)}
	calendar, err := LoadCalendar(fsys, "b3.json")
	if err != nil {
		t.Fatalf("LoadCalendar: %v", err)
	}
	return calendar
}

func TestSessionIsFourHundredAndTwentyMinutesInUTC(t *testing.T) {
	calendar := testCalendar(t)

	session, ok := calendar.Session(calendar.Date(2026, time.January, 2))
	if !ok {
		t.Fatal("2026-01-02 is a Friday, want a session")
	}

	if got := session.Duration(); got != 420*time.Minute {
		t.Errorf("session lasted %s, want 7h0m0s", got)
	}
	if got := session.Open.UTC().Format(time.RFC3339); got != "2026-01-02T13:00:00Z" {
		t.Errorf("session opened at %s, want 13:00Z", got)
	}
	if got := session.Close.UTC().Format(time.RFC3339); got != "2026-01-02T20:00:00Z" {
		t.Errorf("session closed at %s, want 20:00Z", got)
	}
	if got := session.Open.In(calendar.Location()).Format("15:04"); got != "10:00" {
		t.Errorf("session opened at %s local, want 10:00", got)
	}
}

func TestClosedDaysHaveNoSession(t *testing.T) {
	calendar := testCalendar(t)

	cases := map[string]time.Time{
		"holiday":  calendar.Date(2026, time.January, 1),
		"carnival": calendar.Date(2026, time.February, 16),
		"saturday": calendar.Date(2026, time.January, 3),
		"sunday":   calendar.Date(2026, time.January, 4),
	}

	for name, day := range cases {
		if calendar.IsTradingDay(day) {
			t.Errorf("%s (%s) was reported as a trading day", name, day.Format(time.DateOnly))
		}
	}
}

func TestPartialSessionUsesTheHolidayHours(t *testing.T) {
	calendar := testCalendar(t)

	session, ok := calendar.Session(calendar.Date(2026, time.February, 18))
	if !ok {
		t.Fatal("Ash Wednesday is a partial session, want a session")
	}

	if got := session.Open.In(calendar.Location()).Format("15:04"); got != "13:00" {
		t.Errorf("session opened at %s local, want 13:00", got)
	}
	if got := session.Duration(); got != 240*time.Minute {
		t.Errorf("session lasted %s, want 4h0m0s", got)
	}
}

func TestSessionAt(t *testing.T) {
	calendar := testCalendar(t)
	day := calendar.Date(2026, time.January, 2)

	if _, ok := calendar.SessionAt(day.Add(11 * time.Hour)); !ok {
		t.Error("11:00 local is inside the session")
	}
	if _, ok := calendar.SessionAt(day.Add(9 * time.Hour)); ok {
		t.Error("09:00 local is before the open")
	}
	if _, ok := calendar.SessionAt(day.Add(17 * time.Hour)); ok {
		t.Error("17:00 local is the close, which is exclusive")
	}
}

func TestNextAndPrevTradingDaySkipClosedDays(t *testing.T) {
	calendar := testCalendar(t)

	next, err := calendar.NextTradingDay(calendar.Date(2025, time.December, 31))
	if err != nil {
		t.Fatalf("NextTradingDay: %v", err)
	}
	if got := next.Format(time.DateOnly); got != "2026-01-02" {
		t.Errorf("next trading day was %s, want 2026-01-02", got)
	}

	prev, err := calendar.PrevTradingDay(calendar.Date(2026, time.January, 4))
	if err != nil {
		t.Fatalf("PrevTradingDay: %v", err)
	}
	if got := prev.Format(time.DateOnly); got != "2026-01-02" {
		t.Errorf("previous trading day was %s, want 2026-01-02", got)
	}
}

func TestTradingDays(t *testing.T) {
	calendar := testCalendar(t)

	days := calendar.TradingDays(calendar.Date(2026, time.January, 1), calendar.Date(2026, time.January, 7))
	if len(days) != 4 {
		t.Fatalf("got %d trading days, want 4", len(days))
	}
	if got := days[0].Format(time.DateOnly); got != "2026-01-02" {
		t.Errorf("first trading day was %s, want 2026-01-02", got)
	}
}

func TestCoversOnlyTheLoadedYears(t *testing.T) {
	calendar := testCalendar(t)

	if !calendar.Covers(calendar.Date(2026, time.June, 1)) {
		t.Error("2026 should be covered")
	}
	if calendar.Covers(calendar.Date(2030, time.June, 1)) {
		t.Error("2030 is outside the loaded calendar")
	}
}

func TestLoadCalendarRejectsBadFiles(t *testing.T) {
	cases := map[string]string{
		"unknown timezone":  `{"timezone":"Mars/Olympus","session":{"open":"10:00","close":"17:00"},"holidays":[{"date":"2026-01-01","name":"x"}]}`,
		"close before open": `{"timezone":"UTC","session":{"open":"17:00","close":"10:00"},"holidays":[{"date":"2026-01-01","name":"x"}]}`,
		"bad clock":         `{"timezone":"UTC","session":{"open":"25:00","close":"17:00"},"holidays":[{"date":"2026-01-01","name":"x"}]}`,
		"no holidays":       `{"timezone":"UTC","session":{"open":"10:00","close":"17:00"},"holidays":[]}`,
		"bad date":          `{"timezone":"UTC","session":{"open":"10:00","close":"17:00"},"holidays":[{"date":"01/01/2026","name":"x"}]}`,
		"missing name":      `{"timezone":"UTC","session":{"open":"10:00","close":"17:00"},"holidays":[{"date":"2026-01-01"}]}`,
		"duplicate date":    `{"timezone":"UTC","session":{"open":"10:00","close":"17:00"},"holidays":[{"date":"2026-01-01","name":"x"},{"date":"2026-01-01","name":"y"}]}`,
		"misspelled field":  `{"timezone":"UTC","session":{"open":"10:00","close":"17:00"},"holiday":[{"date":"2026-01-01","name":"x"}]}`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{"b3.json": jsonFile(body)}
			if _, err := LoadCalendar(fsys, "b3.json"); err == nil {
				t.Error("LoadCalendar accepted the file, want an error")
			}
		})
	}
}
