package market

import (
	"testing"
	"time"
)

func sessionOpens(t *testing.T, cal *Calendar, days ...time.Time) []time.Time {
	t.Helper()

	opens := make([]time.Time, 0, len(days))
	for _, day := range days {
		session, ok := cal.Session(day)
		if !ok {
			t.Fatalf("%s is not a trading day", day.Format(time.DateOnly))
		}
		opens = append(opens, session.Open)
	}
	return opens
}

func TestFindGapsAcceptsAFullWeekOfDailyBars(t *testing.T) {
	calendar := committedCalendar(t)
	from := calendar.Date(2026, time.August, 17)
	to := calendar.Date(2026, time.August, 21)

	days := []time.Time{}
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		if calendar.IsTradingDay(day) {
			days = append(days, day)
		}
	}

	report, err := FindGaps(calendar, TF1d, from, to, sessionOpens(t, calendar, days...))
	if err != nil {
		t.Fatalf("FindGaps: %v", err)
	}

	if report.Sessions != 5 {
		t.Errorf("the week of 2026-08-17 has %d sessions, want 5", report.Sessions)
	}
	if report.Bars != 5 || report.Expected != 5 {
		t.Errorf("counted %d/%d bars, want 5/5", report.Bars, report.Expected)
	}
	if !report.Clean() {
		t.Errorf("a fully covered week reported holes: %+v", report.Missing)
	}
}

func TestFindGapsReportsAMissingTradingDay(t *testing.T) {
	calendar := committedCalendar(t)
	from := calendar.Date(2026, time.August, 17)
	to := calendar.Date(2026, time.August, 21)

	present := sessionOpens(t, calendar,
		calendar.Date(2026, time.August, 17),
		calendar.Date(2026, time.August, 18),
		calendar.Date(2026, time.August, 20),
		calendar.Date(2026, time.August, 21),
	)

	report, err := FindGaps(calendar, TF1d, from, to, present)
	if err != nil {
		t.Fatalf("FindGaps: %v", err)
	}

	if len(report.Missing) != 1 {
		t.Fatalf("reported %d missing sessions, want 1", len(report.Missing))
	}
	if got := report.Missing[0].Format(time.DateOnly); got != "2026-08-19" {
		t.Errorf("reported %s missing, want 2026-08-19", got)
	}
	if report.Clean() {
		t.Error("a report with a missing session is not clean")
	}
}

func TestFindGapsTreatsHolidaysAsCorrectlyAbsent(t *testing.T) {
	calendar := committedCalendar(t)
	from := calendar.Date(2026, time.September, 4)
	to := calendar.Date(2026, time.September, 8)

	present := sessionOpens(t, calendar,
		calendar.Date(2026, time.September, 4),
		calendar.Date(2026, time.September, 8),
	)

	report, err := FindGaps(calendar, TF1d, from, to, present)
	if err != nil {
		t.Fatalf("FindGaps: %v", err)
	}

	if report.Sessions != 2 {
		t.Errorf("Sep 4-8 2026 holds %d sessions, want 2: the 7th is Independence Day and the 5th-6th a weekend", report.Sessions)
	}
	if !report.Clean() {
		t.Errorf("the holiday was counted as a hole: %+v", report.Missing)
	}
}

func TestFindGapsFlagsBarsOutsideAnySession(t *testing.T) {
	calendar := committedCalendar(t)
	from := calendar.Date(2026, time.August, 17)
	to := calendar.Date(2026, time.August, 21)

	present := sessionOpens(t, calendar,
		calendar.Date(2026, time.August, 17),
		calendar.Date(2026, time.August, 18),
		calendar.Date(2026, time.August, 19),
		calendar.Date(2026, time.August, 20),
		calendar.Date(2026, time.August, 21),
	)
	present = append(present, calendar.Date(2026, time.August, 22).Add(13*time.Hour).UTC())

	report, err := FindGaps(calendar, TF1d, from, to, present)
	if err != nil {
		t.Fatalf("FindGaps: %v", err)
	}

	if len(report.Unexpected) != 1 {
		t.Fatalf("reported %d bars outside a session, want 1", len(report.Unexpected))
	}
	if report.Clean() {
		t.Error("a Saturday bar makes the report dirty")
	}
	if report.Bars != 5 {
		t.Errorf("counted %d in-session bars, want 5", report.Bars)
	}
}

func TestFindGapsSeparatesShortSessionsFromMissingOnes(t *testing.T) {
	calendar := committedCalendar(t)
	day := calendar.Date(2026, time.August, 20)
	session, _ := calendar.Session(day)

	present := []time.Time{}
	for i := range 80 {
		present = append(present, session.Open.Add(time.Duration(i)*5*time.Minute))
	}

	report, err := FindGaps(calendar, TF5m, day, day, present)
	if err != nil {
		t.Fatalf("FindGaps: %v", err)
	}

	if report.Expected != 84 {
		t.Errorf("expected %d 5m bars in a full session, want 84", report.Expected)
	}
	if len(report.Missing) != 0 {
		t.Errorf("a session with 80 of 84 bars is short, not missing: %+v", report.Missing)
	}
	if len(report.Partial) != 1 || report.Partial[0].Present != 80 {
		t.Fatalf("reported %+v, want one short session with 80 bars", report.Partial)
	}
	if !report.Clean() {
		t.Error("a short session is normal for an illiquid ticker and must not fail the report")
	}
}

func TestFindGapsRefusesDerivedTimeframes(t *testing.T) {
	calendar := committedCalendar(t)
	day := calendar.Date(2026, time.August, 20)

	if _, err := FindGaps(calendar, TF15m, day, day, nil); err == nil {
		t.Error("15m is resampled on read and has no stored bars to check")
	}
	if _, err := FindGaps(calendar, TF1d, day, day.AddDate(0, 0, -1), nil); err == nil {
		t.Error("an inverted range should fail")
	}
}
