package market

import (
	"slices"
	"testing"
)

func testLists() []IndexList {
	return []IndexList{
		{Index: "ibov", AsOf: "2026-05-04", File: "indexes/ibov-2026-05.json", Tickers: []string{"PETR4", "VALE3", "BPAC11"}},
		{Index: "smll", AsOf: "2026-05-04", File: "indexes/smll-2026-05.json", Tickers: []string{"MGLU3", "PETR4"}},
	}
}

func testContracts() Contracts {
	lotSize := int32(1)
	tickSize := 1.0

	return Contracts{
		Futures: []FutureContract{
			{Root: "WIN", Name: "Mini Ibovespa Futuro", PointValue: 0.2, TickSize: 5, LotSize: 1, Months: "GJMQVZ"},
		},
		Symbols: []SymbolSeed{
			{Ticker: "^BVSP", Kind: "index", ShortName: "IBOVESPA", LotSize: &lotSize, TickSize: &tickSize, Tracked: true},
		},
	}
}

func findSymbol(t *testing.T, plan Plan, ticker string) DesiredSymbol {
	t.Helper()

	for _, symbol := range plan.Symbols {
		if symbol.Ticker == ticker {
			return symbol
		}
	}

	t.Fatalf("%s is missing from the plan", ticker)
	return DesiredSymbol{}
}

func TestBuildPlanAdmitsEveryIndexMemberOnce(t *testing.T) {
	plan, err := BuildPlan(testLists(), testContracts(), nil)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	want := []string{"BPAC11", "MGLU3", "PETR4", "VALE3", "WIN", "^BVSP"}
	got := make([]string, 0, len(plan.Symbols))
	for _, symbol := range plan.Symbols {
		got = append(got, symbol.Ticker)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("plan symbols were %v, want %v", got, want)
	}

	if !slices.Equal(plan.Created, want) {
		t.Errorf("created were %v, want every symbol", plan.Created)
	}
	if wantAdmissions := []string{"BPAC11", "MGLU3", "PETR4", "VALE3", "^BVSP"}; !slices.Equal(plan.Admissions, wantAdmissions) {
		t.Errorf("admissions were %v, want %v", plan.Admissions, wantAdmissions)
	}
	if len(plan.Departures) != 0 {
		t.Errorf("departures were %v, want none", plan.Departures)
	}
}

func TestBuildPlanAppliesContractMechanics(t *testing.T) {
	plan, err := BuildPlan(testLists(), testContracts(), nil)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	stock := findSymbol(t, plan, "PETR4")
	if stock.Kind != KindStock || stock.LotSize != 100 || stock.TickSize != 0.01 {
		t.Errorf("PETR4 was %s lot=%d tick=%v, want stock lot=100 tick=0.01", stock.Kind, stock.LotSize, stock.TickSize)
	}
	if !stock.Tracked {
		t.Error("PETR4 came out of the admission lists untracked")
	}

	unit := findSymbol(t, plan, "BPAC11")
	if unit.Kind != KindUnit || unit.LotSize != 100 {
		t.Errorf("BPAC11 was %s lot=%d, want unit lot=100", unit.Kind, unit.LotSize)
	}

	future := findSymbol(t, plan, "WIN")
	if future.Kind != KindFuture || future.LotSize != 1 || future.TickSize != 5 {
		t.Errorf("WIN was %s lot=%d tick=%v, want future lot=1 tick=5", future.Kind, future.LotSize, future.TickSize)
	}
	if future.PointValue == nil || *future.PointValue != 0.2 {
		t.Errorf("WIN point value was %v, want 0.20", future.PointValue)
	}
	if future.Tracked {
		t.Error("WIN is tracked; contract roots are seeded, not admitted")
	}

	index := findSymbol(t, plan, "^BVSP")
	if index.Kind != KindIndex || index.LotSize != 1 || index.TickSize != 1 {
		t.Errorf("^BVSP was %s lot=%d tick=%v, want index lot=1 tick=1", index.Kind, index.LotSize, index.TickSize)
	}
	if !index.Tracked || index.ShortName != "IBOVESPA" {
		t.Errorf("^BVSP seed was not applied: tracked=%v short=%q", index.Tracked, index.ShortName)
	}
}

func TestBuildPlanReportsDeparturesWithoutRevokingTracking(t *testing.T) {
	existing := []ExistingSymbol{
		{Ticker: "PETR4", Kind: "stock", Active: true, Tracked: true},
		{Ticker: "OLDD3", Kind: "stock", Active: true, Tracked: true},
		{Ticker: "NEVR3", Kind: "stock", Active: true, Tracked: false},
	}

	plan, err := BuildPlan(testLists(), testContracts(), existing)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	if !slices.Equal(plan.Departures, []string{"OLDD3"}) {
		t.Errorf("departures were %v, want [OLDD3]", plan.Departures)
	}
	for _, symbol := range plan.Symbols {
		if symbol.Ticker == "OLDD3" {
			t.Error("a departed symbol was written back into the plan; its row must be left alone")
		}
	}
	if slices.Contains(plan.Admissions, "PETR4") {
		t.Error("PETR4 was already tracked and should not count as a new admission")
	}
	if slices.Contains(plan.Created, "PETR4") {
		t.Error("PETR4 already exists and should not count as created")
	}
}

func TestBuildPlanRejectsUnclassifiableTickers(t *testing.T) {
	lists := []IndexList{
		{Index: "ibov", AsOf: "2026-05-04", File: "indexes/ibov-2026-05.json", Tickers: []string{"PETR4", "NOTATICKER"}},
	}

	if _, err := BuildPlan(lists, testContracts(), nil); err == nil {
		t.Fatal("BuildPlan accepted an unclassifiable ticker, want an error")
	}
}

func TestBuildPlanIsIdempotent(t *testing.T) {
	first, err := BuildPlan(testLists(), testContracts(), nil)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	existing := make([]ExistingSymbol, 0, len(first.Symbols))
	for _, symbol := range first.Symbols {
		existing = append(existing, ExistingSymbol{
			Ticker:  symbol.Ticker,
			Kind:    string(symbol.Kind),
			Active:  true,
			Tracked: symbol.Tracked,
		})
	}

	second, err := BuildPlan(testLists(), testContracts(), existing)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	if len(second.Created) != 0 || len(second.Admissions) != 0 || len(second.Departures) != 0 {
		t.Errorf("a second run reported created=%v admissions=%v departures=%v, want all empty",
			second.Created, second.Admissions, second.Departures)
	}
	if len(second.Symbols) != len(first.Symbols) {
		t.Errorf("second run planned %d symbols, want %d", len(second.Symbols), len(first.Symbols))
	}
}
