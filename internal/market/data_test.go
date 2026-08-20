package market

import (
	"io/fs"
	"os"
	"slices"
	"testing"
	"time"
)

func repoFS() fs.FS { return os.DirFS("../..") }

func TestCommittedContractsAreValid(t *testing.T) {
	contracts, err := LoadContracts(repoFS(), ContractsFile)
	if err != nil {
		t.Fatalf("LoadContracts: %v", err)
	}

	roots := contracts.Roots()
	for _, root := range []string{"WIN", "WDO"} {
		if !slices.Contains(roots, root) {
			t.Errorf("%s is missing from %s", root, ContractsFile)
		}
	}

	want := map[string]float64{"WIN": 0.2, "WDO": 10}
	for _, future := range contracts.Futures {
		if expected, ok := want[future.Root]; ok && future.PointValue != expected {
			t.Errorf("%s point value is %v, want %v", future.Root, future.PointValue, expected)
		}
	}
}

func TestCommittedIndexListsAreValid(t *testing.T) {
	lists, err := LoadIndexLists(repoFS(), IndexesDir)
	if err != nil {
		t.Fatalf("LoadIndexLists: %v", err)
	}
	if len(lists) == 0 {
		t.Fatal("no index composition files are committed")
	}

	for _, ticker := range UnionTickers(lists) {
		if _, err := ClassifyTicker(ticker); err != nil {
			t.Errorf("%s: %v", ticker, err)
		}
	}
}

func TestCommittedCalendarCoversTheDevelopmentWindow(t *testing.T) {
	calendar, err := LoadCalendar(repoFS(), HolidaysFile)
	if err != nil {
		t.Fatalf("LoadCalendar: %v", err)
	}

	minYear, maxYear := calendar.Years()
	if minYear > 2021 || maxYear < 2026 {
		t.Errorf("calendar covers %d-%d, want at least 2021-2026", minYear, maxYear)
	}

	if calendar.Location().String() != "America/Sao_Paulo" {
		t.Errorf("calendar timezone is %s, want America/Sao_Paulo", calendar.Location())
	}

	session, ok := calendar.Session(calendar.Date(2026, time.August, 20))
	if !ok {
		t.Fatal("2026-08-20 is an ordinary Thursday, want a session")
	}
	if got := session.Duration(); got != 420*time.Minute {
		t.Errorf("the regular session lasts %s, want 7h0m0s", got)
	}

	if calendar.IsTradingDay(calendar.Date(2026, time.January, 1)) {
		t.Error("2026-01-01 is a holiday")
	}

	ashWednesday, ok := calendar.Session(calendar.Date(2026, time.February, 18))
	if !ok {
		t.Fatal("Ash Wednesday 2026 should be a partial session, not a closed day")
	}
	if got := ashWednesday.Open.In(calendar.Location()).Format("15:04"); got != "13:00" {
		t.Errorf("Ash Wednesday opened at %s, want 13:00", got)
	}
}

func TestCommittedDataPlansTheFourFreeTickers(t *testing.T) {
	contracts, err := LoadContracts(repoFS(), ContractsFile)
	if err != nil {
		t.Fatalf("LoadContracts: %v", err)
	}

	lists, err := LoadIndexLists(repoFS(), IndexesDir)
	if err != nil {
		t.Fatalf("LoadIndexLists: %v", err)
	}

	plan, err := BuildPlan(lists, contracts, nil)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	for _, ticker := range []string{"PETR4", "VALE3", "ITUB4", "MGLU3"} {
		symbol := findSymbol(t, plan, ticker)
		if symbol.Kind != KindStock {
			t.Errorf("%s is %s, want stock", ticker, symbol.Kind)
		}
		if symbol.LotSize != 100 {
			t.Errorf("%s lot size is %d, want 100", ticker, symbol.LotSize)
		}
		if symbol.TickSize != 0.01 {
			t.Errorf("%s tick size is %v, want 0.01", ticker, symbol.TickSize)
		}
		if !symbol.Tracked {
			t.Errorf("%s is not tracked", ticker)
		}
	}
}
