package market

import (
	"testing"
	"time"
)

func pageCandles(n int) []Candle {
	base := time.Date(2026, 6, 22, 13, 0, 0, 0, time.UTC)
	candles := make([]Candle, 0, n)
	for i := range n {
		candles = append(candles, Candle{
			TS:     base.Add(time.Duration(i) * BaseBucket),
			Open:   10,
			High:   11,
			Low:    9,
			Close:  10.5,
			Volume: int64(i),
		})
	}
	return candles
}

func TestBaseRowCapProbesOneRowPastTheLimit(t *testing.T) {
	cases := []struct {
		tf    Timeframe
		limit int
		want  int32
	}{
		{TF5m, 100, 101},
		{TF1d, 100, 101},
		{TF15m, 100, 301},
		{TF30m, 100, 601},
		{TF1h, 100, 1201},
	}

	for _, tc := range cases {
		if got := baseRowCap(tc.tf, tc.limit); got != tc.want {
			t.Errorf("baseRowCap(%s, %d) = %d, want %d", tc.tf, tc.limit, got, tc.want)
		}
	}
}

func TestTrimPageKeepsEverythingWhenTheFetchWasNotTruncated(t *testing.T) {
	candles := pageCandles(10)

	var page Page
	trimPage(&page, candles, 50, false)

	if len(page.Candles) != 10 {
		t.Fatalf("kept %d candles, want 10", len(page.Candles))
	}
	if page.HasMore {
		t.Error("HasMore is true on a page that read the whole window")
	}
	if !page.Cursor.IsZero() {
		t.Errorf("Cursor is %s, want zero when there is nothing older", page.Cursor)
	}
}

func TestTrimPageDropsTheOldestBucketWhenTheFetchWasTruncated(t *testing.T) {
	candles := pageCandles(10)

	var page Page
	trimPage(&page, candles, 50, true)

	if len(page.Candles) != 9 {
		t.Fatalf("kept %d candles, want 9", len(page.Candles))
	}
	if !page.Candles[0].TS.Equal(candles[1].TS) {
		t.Errorf("oldest kept candle is %s, want %s", page.Candles[0].TS, candles[1].TS)
	}
	if !page.HasMore {
		t.Error("HasMore is false after dropping a possibly partial bucket")
	}
	if !page.Cursor.Equal(candles[1].TS) {
		t.Errorf("Cursor is %s, want the oldest kept candle %s", page.Cursor, candles[1].TS)
	}
}

func TestTrimPageCutsBackToTheLimitFromTheNewestEnd(t *testing.T) {
	candles := pageCandles(10)

	var page Page
	trimPage(&page, candles, 4, false)

	if len(page.Candles) != 4 {
		t.Fatalf("kept %d candles, want 4", len(page.Candles))
	}
	if !page.Candles[0].TS.Equal(candles[6].TS) {
		t.Errorf("oldest kept candle is %s, want %s", page.Candles[0].TS, candles[6].TS)
	}
	if !page.Candles[3].TS.Equal(candles[9].TS) {
		t.Errorf("newest kept candle is %s, want %s", page.Candles[3].TS, candles[9].TS)
	}
	if !page.HasMore || !page.Cursor.Equal(candles[6].TS) {
		t.Errorf("HasMore=%v cursor=%s, want true and %s", page.HasMore, page.Cursor, candles[6].TS)
	}
}

func TestTrimPageCursorIsExclusiveSoPagesDoNotOverlap(t *testing.T) {
	candles := pageCandles(12)

	var first Page
	trimPage(&first, candles[6:], 6, true)

	remaining := []Candle{}
	for _, candle := range candles {
		if candle.TS.Before(first.Cursor) {
			remaining = append(remaining, candle)
		}
	}

	var second Page
	trimPage(&second, remaining, 6, false)

	seen := map[time.Time]int{}
	for _, candle := range append(append([]Candle{}, first.Candles...), second.Candles...) {
		seen[candle.TS]++
	}
	for ts, count := range seen {
		if count != 1 {
			t.Errorf("%s appears %d times across the two pages", ts, count)
		}
	}
	if len(seen) != 11 {
		t.Errorf("the two pages cover %d distinct candles, want 11", len(seen))
	}
}

func TestTrimPageHandlesAnEmptyWindow(t *testing.T) {
	var page Page
	trimPage(&page, nil, 100, true)

	if page.Candles == nil {
		t.Fatal("Candles is nil, want an empty slice so the columnar body encodes as []")
	}
	if len(page.Candles) != 0 {
		t.Errorf("kept %d candles, want 0", len(page.Candles))
	}
	if !page.Cursor.IsZero() {
		t.Errorf("Cursor is %s, want zero when nothing was kept", page.Cursor)
	}
}

func TestClampPageLimit(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{0, DefaultPageLimit},
		{-1, DefaultPageLimit},
		{1, 1},
		{MaxPageLimit, MaxPageLimit},
		{MaxPageLimit + 1, MaxPageLimit},
	}

	for _, tc := range cases {
		if got := ClampPageLimit(tc.in); got != tc.want {
			t.Errorf("ClampPageLimit(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
