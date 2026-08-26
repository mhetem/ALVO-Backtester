package market

import (
	"math"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func loadBorrow(t *testing.T, body string) (*Borrow, error) {
	t.Helper()

	fsys := fstest.MapFS{"borrow.json": &fstest.MapFile{Data: []byte(body)}}

	return LoadBorrow(fsys, "borrow.json")
}

func TestCommittedBorrowCurveIsValid(t *testing.T) {
	borrow, err := LoadBorrow(repoFS(), BorrowFile)
	if err != nil {
		t.Fatalf("LoadBorrow: %v", err)
	}

	if borrow.Basis() != TradingDaysPerYear {
		t.Errorf("basis is %g, want the %d-business-day Brazilian convention", borrow.Basis(), TradingDaysPerYear)
	}
	if borrow.DefaultAnnualPct() <= 0 {
		t.Errorf("the default borrow rate is %g%%, want a positive one: a free short is the bug this file exists to fix",
			borrow.DefaultAnnualPct())
	}
	if borrow.Source() == "" {
		t.Error("the borrow file names no source")
	}
}

func TestBorrowCompoundsBackToTheAnnualRate(t *testing.T) {
	borrow, err := loadBorrow(t, `{
      "source": "test", "basis": 252, "through": "2030-01-01",
      "default_annual_pct": 10.0, "rates": {}, "unavailable": []
    }`)
	if err != nil {
		t.Fatalf("LoadBorrow: %v", err)
	}

	// A daily rate is only right if 252 of them compound back to the annual figure the
	// file names, which is the whole point of declaring the basis.
	daily := borrow.PerPeriod("ANY", time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC), TradingDaysPerYear)
	if got := math.Pow(1+daily, TradingDaysPerYear) - 1; math.Abs(got-0.10) > 1e-9 {
		t.Errorf("252 daily borrow charges compound to %g, want 0.10", got)
	}

	// An intraday bar takes its share of a day, not of a calendar year: 84 five-minute
	// bars a day must come to the same daily rate.
	intraday := borrow.PerPeriod("ANY", time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC), TradingDaysPerYear*84)
	if got := math.Pow(1+intraday, 84) - 1; math.Abs(got-daily) > 1e-12 {
		t.Errorf("84 intraday borrow charges compound to %g, want the daily %g", got, daily)
	}
}

func TestBorrowWindowsBoundTheirOwnDates(t *testing.T) {
	borrow, err := loadBorrow(t, `{
      "source": "test", "basis": 252, "through": "2030-01-01",
      "default_annual_pct": 2.0, "rates": {},
      "unavailable": [{"ticker": "MGLU3", "from": "2026-03-01", "to": "2026-03-31"}]
    }`)
	if err != nil {
		t.Fatalf("LoadBorrow: %v", err)
	}

	for _, tc := range []struct {
		day       string
		available bool
	}{
		{"2026-02-28", true},
		{"2026-03-01", false},
		{"2026-03-15", false},
		{"2026-03-31", false},
		{"2026-04-01", true},
	} {
		day, err := time.Parse(time.DateOnly, tc.day)
		if err != nil {
			t.Fatalf("parsing %s: %v", tc.day, err)
		}
		if got := borrow.Available("MGLU3", day); got != tc.available {
			t.Errorf("MGLU3 available on %s = %v, want %v", tc.day, got, tc.available)
		}
	}

	if !borrow.Available("PETR4", time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC)) {
		t.Error("PETR4 is unavailable, want a ticker the file does not name to be borrowable")
	}
}

func TestBorrowRefusesAFileItCannotTrust(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "no basis",
			body: `{"source": "t", "basis": 0, "through": "2030-01-01", "default_annual_pct": 2}`,
		},
		{
			name: "no through",
			body: `{"source": "t", "basis": 252, "through": "", "default_annual_pct": 2}`,
		},
		{
			name: "absurd default",
			body: `{"source": "t", "basis": 252, "through": "2030-01-01", "default_annual_pct": 5000}`,
		},
		{
			name: "steps out of order",
			body: `{"source": "t", "basis": 252, "through": "2030-01-01", "default_annual_pct": 2,
              "rates": {"X": [{"from": "2026-05-01", "annual_pct": 3}, {"from": "2026-01-01", "annual_pct": 4}]}}`,
		},
		{
			name: "window ends before it starts",
			body: `{"source": "t", "basis": 252, "through": "2030-01-01", "default_annual_pct": 2,
              "unavailable": [{"ticker": "X", "from": "2026-05-01", "to": "2026-01-01"}]}`,
		},
		{
			name: "through predates a rate change",
			body: `{"source": "t", "basis": 252, "through": "2026-01-01", "default_annual_pct": 2,
              "rates": {"X": [{"from": "2027-01-01", "annual_pct": 3}]}}`,
		},
		{
			name: "unknown field",
			body: `{"source": "t", "basis": 252, "through": "2030-01-01", "default_annual_pct": 2, "surprise": 1}`,
		},
	} {
		if _, err := loadBorrow(t, tc.body); err == nil {
			t.Errorf("%s: the file was accepted", tc.name)
		} else if !strings.Contains(err.Error(), "borrow.json") {
			t.Errorf("%s: the error is %q, want it to name the file", tc.name, err)
		}
	}
}
