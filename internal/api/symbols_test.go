package api

import (
	"net/http"
	"strings"
	"testing"

	database "github.com/mhetem/ALVO-Backtester/internal/db"
)

func TestSymbolsRejectsAnUnknownKind(t *testing.T) {
	server := testServer(t)
	rec := get(t, server.handleSymbols, "/api/v1/symbols?kind=etf")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rec.Code)
	}
	if got := decodeError(t, rec); !strings.Contains(got, `unknown symbol kind "etf"`) {
		t.Errorf("error is %q, want it to name the rejected kind", got)
	}
}

func TestSymbolsRejectsABadLimit(t *testing.T) {
	server := testServer(t)
	rec := get(t, server.handleSymbols, "/api/v1/symbols?limit=-3")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rec.Code)
	}
	if got := decodeError(t, rec); !strings.Contains(got, "limit must be at least 1") {
		t.Errorf("error is %q, want it to explain the limit", got)
	}
}

func TestDisplayNamePrefersLongNameOverBrapisEchoedShortName(t *testing.T) {
	long := "Petroleo Brasileiro S.A. - Petrobras"
	echoed := "PETR4"
	blank := "   "

	cases := []struct {
		name string
		row  database.Symbol
		want string
	}{
		{
			name: "long name wins",
			row:  database.Symbol{Ticker: "PETR4", ShortName: &echoed, LongName: &long},
			want: long,
		},
		{
			name: "short name that echoes the ticker is not a display name",
			row:  database.Symbol{Ticker: "PETR4", ShortName: &echoed},
			want: "PETR4",
		},
		{
			name: "blank long name falls through",
			row:  database.Symbol{Ticker: "VALE3", LongName: &blank},
			want: "VALE3",
		},
		{
			name: "nothing enriched leaves the ticker",
			row:  database.Symbol{Ticker: "WIN"},
			want: "WIN",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := displayName(tc.row); got != tc.want {
				t.Errorf("displayName = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClampKeepsLimitsInsideTheCap(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{1, 1},
		{defaultSymbolLimit, defaultSymbolLimit},
		{maxSymbolLimit, maxSymbolLimit},
		{maxSymbolLimit + 500, maxSymbolLimit},
	}

	for _, tc := range cases {
		if got := clamp(tc.in, 1, maxSymbolLimit); got != tc.want {
			t.Errorf("clamp(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
