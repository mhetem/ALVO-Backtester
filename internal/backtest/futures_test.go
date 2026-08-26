package backtest

import (
	"testing"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/market"
)

func futuresBars(days ...string) []market.Candle {
	candles := make([]market.Candle, 0, len(days))
	for _, stamp := range days {
		ts, err := time.Parse(time.DateOnly, stamp)
		if err != nil {
			panic(err)
		}
		candles = append(candles, market.Candle{TS: ts, Open: 1, High: 1, Low: 1, Close: 1})
	}
	return candles
}

func barStamps(candles []market.Candle) []string {
	out := make([]string, 0, len(candles))
	for _, candle := range candles {
		out = append(out, candle.TS.Format(time.DateOnly))
	}
	return out
}

func TestSplitAtSeparatesPrimeFromTheRunWindow(t *testing.T) {
	candles := futuresBars("2025-06-16", "2025-06-17", "2025-06-18", "2025-06-19", "2025-06-20")
	from, _ := time.Parse(time.DateOnly, "2025-06-18")

	prime, run := splitAt(candles, from, 2)

	if got := barStamps(prime); len(got) != 2 || got[0] != "2025-06-16" || got[1] != "2025-06-17" {
		t.Errorf("prime = %v, want the two bars before the window", got)
	}
	if got := barStamps(run); len(got) != 3 || got[0] != "2025-06-18" {
		t.Errorf("run = %v, want the window starting on 2025-06-18", got)
	}
}

func TestSplitAtCapsPrimeToTheBarsAsked(t *testing.T) {
	candles := futuresBars("2025-06-16", "2025-06-17", "2025-06-18", "2025-06-19")
	from, _ := time.Parse(time.DateOnly, "2025-06-19")

	prime, run := splitAt(candles, from, 1)

	if got := barStamps(prime); len(got) != 1 || got[0] != "2025-06-18" {
		t.Errorf("prime = %v, want only the newest bar before the window", got)
	}
	if len(run) != 1 {
		t.Errorf("run = %v, want one bar", barStamps(run))
	}
}

func TestSplitAtReturnsNoPrimeWhenNoneAsked(t *testing.T) {
	candles := futuresBars("2025-06-16", "2025-06-17", "2025-06-18")
	from, _ := time.Parse(time.DateOnly, "2025-06-18")

	prime, run := splitAt(candles, from, 0)

	if prime != nil {
		t.Errorf("prime = %v, want nil when no warmup was requested", barStamps(prime))
	}
	if len(run) != 1 {
		t.Errorf("run = %v, want one bar", barStamps(run))
	}
}

func TestSplitAtHandlesAWindowStartingBeforeEveryBar(t *testing.T) {
	candles := futuresBars("2025-06-16", "2025-06-17")
	from, _ := time.Parse(time.DateOnly, "2020-01-01")

	prime, run := splitAt(candles, from, 5)

	if len(prime) != 0 {
		t.Errorf("prime = %v, want empty", barStamps(prime))
	}
	if len(run) != 2 {
		t.Errorf("run = %v, want every bar", barStamps(run))
	}
}

func TestSplitAtHandlesAWindowStartingAfterEveryBar(t *testing.T) {
	candles := futuresBars("2025-06-16", "2025-06-17")
	from, _ := time.Parse(time.DateOnly, "2026-01-01")

	prime, run := splitAt(candles, from, 1)

	if got := barStamps(prime); len(got) != 1 || got[0] != "2025-06-17" {
		t.Errorf("prime = %v, want the newest bar", got)
	}
	if len(run) != 0 {
		t.Errorf("run = %v, want empty", barStamps(run))
	}
}

func TestSymbolFutureRecognisesTheKind(t *testing.T) {
	if !(Symbol{Kind: string(market.KindFuture)}).Future() {
		t.Error("a future-kind symbol did not report Future()")
	}
	if (Symbol{Kind: string(market.KindStock)}).Future() {
		t.Error("a stock reported Future()")
	}
}
