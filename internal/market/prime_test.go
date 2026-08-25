package market

import (
	"context"
	"testing"
	"time"
)

func TestPrimeAsksTheDatabaseForNothingWhenThereIsNothingToPrime(t *testing.T) {
	service := NewCandleService(nil, committedCalendar(t))
	before := time.Date(2026, 6, 22, 13, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		req  PrimeRequest
	}{
		{"no bars wanted", PrimeRequest{SymbolID: 1, Timeframe: TF1d, Before: before, Bars: 0}},
		{"negative bars", PrimeRequest{SymbolID: 1, Timeframe: TF1d, Before: before, Bars: -5}},
		{"no page to prime", PrimeRequest{SymbolID: 1, Timeframe: TF1d, Bars: 100}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candles, err := service.Prime(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("Prime: %v", err)
			}
			if candles != nil {
				t.Errorf("got %d candles, want none", len(candles))
			}
		})
	}
}

func TestPrimeRejectsATimeframeItCannotFold(t *testing.T) {
	service := NewCandleService(nil, committedCalendar(t))

	_, err := service.Prime(context.Background(), PrimeRequest{
		SymbolID:  1,
		Timeframe: Timeframe("4h"),
		Before:    time.Date(2026, 6, 22, 13, 0, 0, 0, time.UTC),
		Bars:      10,
	})
	if err == nil {
		t.Fatal("an unknown timeframe primed without complaint")
	}
}

func TestPrimeReadsWholeBucketsWorthOfBaseRows(t *testing.T) {
	cases := []struct {
		tf   Timeframe
		bars int
		want int32
	}{
		{TF1d, 200, 201},
		{TF5m, 200, 201},
		{TF15m, 200, 601},
		{TF1h, 200, 2401},
	}

	for _, tc := range cases {
		if got := baseRowCap(tc.tf, tc.bars); got != tc.want {
			t.Errorf("priming %d %s buckets reads %d base rows, want %d", tc.bars, tc.tf, got, tc.want)
		}
	}
}
