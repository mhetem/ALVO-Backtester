package api

import (
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	database "github.com/mhetem/ALVO-Backtester/internal/db"
)

func TestABasketNeedsAUsableName(t *testing.T) {
	cases := []struct {
		name  string
		given string
		want  string
	}{
		{"empty", "", "needs a name"},
		{"only whitespace", "   ", "needs a name"},
		{"longer than the column allows", strings.Repeat("a", maxBasketName+1), "at most"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := normalizeBasketName(tc.given); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("normalizeBasketName(%q) error = %v, want one containing %q", tc.given, err, tc.want)
			}
		})
	}

	name, err := normalizeBasketName("  Blue chips  ")
	if err != nil {
		t.Fatalf("normalizeBasketName: %v", err)
	}
	if name != "Blue chips" {
		t.Errorf("name = %q, want %q", name, "Blue chips")
	}
}

// A saved basket goes through the same normalisation a run's basket does, so anything the
// user stores here is something POST /backtests will still accept later.
func TestASavedBasketIsNormalisedLikeARunBasket(t *testing.T) {
	tickers, err := readSavedBasket([]string{" petr4 ", "VALE3", "petr4", "", "  "})
	if err != nil {
		t.Fatalf("readSavedBasket: %v", err)
	}

	want := []string{"PETR4", "VALE3"}
	if len(tickers) != len(want) {
		t.Fatalf("tickers = %v, want %v", tickers, want)
	}
	for i, ticker := range want {
		if tickers[i] != ticker {
			t.Errorf("tickers[%d] = %q, want %q", i, tickers[i], ticker)
		}
	}
}

func TestASavedBasketRejectsWhatARunWouldRefuse(t *testing.T) {
	tooMany := make([]string, 0, maxBasket+1)
	for i := range maxBasket + 1 {
		tooMany = append(tooMany, "TICK"+strconv.Itoa(i))
	}

	cases := []struct {
		name  string
		given []string
		want  string
	}{
		{"no symbols at all", nil, "at least one symbol"},
		{"only blanks", []string{"", "   "}, "at least one symbol"},
		{"more symbols than a run can hold", tooMany, "at most"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := readSavedBasket(tc.given); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("readSavedBasket(%v) error = %v, want one containing %q", tc.given, err, tc.want)
			}
		})
	}
}

// The membership is what a caller feeds back to POST /backtests, so an empty basket has to
// serialise as [] rather than null.
func TestABasketBodyNeverCarriesANullSymbolList(t *testing.T) {
	body := basketFrom(database.SymbolBasket{ID: uuid.New(), Name: "Blue chips"}, nil)

	if body.Symbols == nil {
		t.Error("Symbols is nil, want an empty slice")
	}
	if body.Count != 0 {
		t.Errorf("Count = %d, want 0", body.Count)
	}
}

func TestABasketBodyCountsItsSymbols(t *testing.T) {
	basket := []database.Symbol{
		{ID: 1, Ticker: "PETR4", Kind: "stock", Currency: "BRL", LotSize: 100},
		{ID: 2, Ticker: "VALE3", Kind: "stock", Currency: "BRL", LotSize: 100},
	}

	body := basketFrom(database.SymbolBasket{ID: uuid.New(), Name: "Blue chips"}, symbolBodies(basket))

	if body.Count != len(basket) {
		t.Errorf("Count = %d, want %d", body.Count, len(basket))
	}
	if len(body.Symbols) != len(basket) || body.Symbols[0].Ticker != "PETR4" || body.Symbols[1].Ticker != "VALE3" {
		t.Errorf("Symbols = %v, want PETR4 then VALE3 in order", body.Symbols)
	}
}

// The copyfrom rows carry the order the caller sent, which is the order the basket reads
// back in.
func TestBasketMemberParamsNumberTheSymbolsInOrder(t *testing.T) {
	id := uuid.New()
	basket := []database.Symbol{{ID: 7, Ticker: "PETR4"}, {ID: 9, Ticker: "VALE3"}}

	members := basketMemberParams(id, basket)

	if len(members) != len(basket) {
		t.Fatalf("members = %d rows, want %d", len(members), len(basket))
	}
	for i, member := range members {
		if member.BasketID != id {
			t.Errorf("members[%d].BasketID = %s, want %s", i, member.BasketID, id)
		}
		if member.Ord != int32(i) {
			t.Errorf("members[%d].Ord = %d, want %d", i, member.Ord, i)
		}
		if member.SymbolID != basket[i].ID {
			t.Errorf("members[%d].SymbolID = %d, want %d", i, member.SymbolID, basket[i].ID)
		}
	}
}
