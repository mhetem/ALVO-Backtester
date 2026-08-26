package ingest

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/market"
)

func TestNormaliseRootsFallsBackToTheContractRoots(t *testing.T) {
	got := normaliseRoots(nil)
	if !slices.Equal(got, market.DefaultFutureRoots) {
		t.Errorf("normaliseRoots(nil) = %v, want %v", got, market.DefaultFutureRoots)
	}
}

func TestNormaliseRootsUppercasesSortsAndDedupes(t *testing.T) {
	got := normaliseRoots([]string{" win ", "wdo", "WIN", "", "   "})
	want := []string{"WDO", "WIN"}

	if !slices.Equal(got, want) {
		t.Errorf("normaliseRoots = %v, want %v", got, want)
	}
}

func TestParseFuturesDay(t *testing.T) {
	got, err := parseFuturesDay(" 2026-10-14 ")
	if err != nil {
		t.Fatalf("parseFuturesDay: %v", err)
	}
	if got.Format(time.DateOnly) != "2026-10-14" {
		t.Errorf("parsed %s, want 2026-10-14", got.Format(time.DateOnly))
	}

	if _, err := parseFuturesDay("14/10/2026"); err == nil {
		t.Error("parseFuturesDay accepted a non-ISO date")
	}
}

func TestOptionalDayIsNilForUnparseableInput(t *testing.T) {
	if got := optionalDay(""); got != nil {
		t.Errorf("optionalDay(\"\") = %v, want nil", got)
	}
	if got := optionalDay("2025-06-10"); got == nil || got.Format(time.DateOnly) != "2025-06-10" {
		t.Errorf("optionalDay lost a valid date: %v", got)
	}
}

func TestTextIsNilWhenBlank(t *testing.T) {
	if got := text("   "); got != nil {
		t.Errorf("text(\"   \") = %q, want nil", *got)
	}
	if got := text(" Minicontrato "); got == nil || *got != "Minicontrato" {
		t.Errorf("text did not trim to a value: %v", got)
	}
}

// brapi returns nulls and occasional zeroes for traded fields on sessions with no trades;
// neither may reach a column constrained to be positive.
func TestPositiveRejectsNilAndNonPositive(t *testing.T) {
	zero, negative, real := 0.0, -1.0, 177465.0

	if got := positive(nil); got != nil {
		t.Error("positive(nil) returned a value")
	}
	if got := positive(&zero); got != nil {
		t.Error("positive(0) returned a value")
	}
	if got := positive(&negative); got != nil {
		t.Error("positive(-1) returned a value")
	}
	if got := positive(&real); got == nil || *got != real {
		t.Errorf("positive(%v) = %v, want the value through", real, got)
	}
}

func TestFuturesReportRootsAreNormalisedForTheHeading(t *testing.T) {
	roots := normaliseRoots([]string{"dol", "WIN"})
	if strings.Join(roots, ", ") != "DOL, WIN" {
		t.Errorf("roots rendered as %q, want \"DOL, WIN\"", strings.Join(roots, ", "))
	}
}
