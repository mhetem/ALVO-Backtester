package ingest

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/brapi"
	database "github.com/mhetem/ALVO-Backtester/internal/db"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

type stubFetcher struct {
	token  bool
	quotes []brapi.Quote
	err    error
	asked  []string
}

func (s *stubFetcher) HasToken() bool { return s.token }

func (s *stubFetcher) Quote(_ context.Context, tickers []string, _ brapi.QuoteOptions) ([]brapi.Quote, error) {
	s.asked = append(s.asked, tickers...)
	return s.quotes, s.err
}

func testIngester(t *testing.T) *Ingester {
	t.Helper()
	return NewIngester(nil, &stubFetcher{}, testCalendar(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestBackfillSkipsTokenOnlySymbolsInsteadOfFailingOnThem(t *testing.T) {
	ingester := testIngester(t)
	calendar := ingester.Calendar()

	report, err := ingester.Backfill(context.Background(), []database.Symbol{{ID: 1, Ticker: "^BVSP"}}, BackfillOptions{
		Timeframe: market.TF1d,
		From:      calendar.Date(2026, time.August, 1),
		To:        calendar.Date(2026, time.August, 20),
	})
	if err != nil {
		t.Fatalf("a token-only symbol must not fail the backfill: %v", err)
	}

	if len(report.Unreachable) != 1 || report.Unreachable[0] != "^BVSP" {
		t.Errorf("reported unreachable %v, want [^BVSP]", report.Unreachable)
	}
	if len(report.Failures) != 0 {
		t.Errorf("a symbol brapi will not serve without a token is a skip, not a failure: %+v", report.Failures)
	}
	if report.Chunks != 0 || report.Requests != 0 {
		t.Errorf("planned %d chunks and made %d requests for an unreachable symbol, want 0 and 0", report.Chunks, report.Requests)
	}

	if asked := ingester.client.(*stubFetcher).asked; len(asked) != 0 {
		t.Errorf("brapi was asked for %v, want nothing", asked)
	}
}

func TestReachableFollowsTheTokenAndTheFreeList(t *testing.T) {
	ingester := testIngester(t)

	for _, ticker := range brapi.FreeTickers {
		if !ingester.Reachable(ticker) {
			t.Errorf("%s is tokenless and must be reachable without a token", ticker)
		}
	}
	if ingester.Reachable("^BVSP") {
		t.Error("^BVSP needs a token and must not be reachable without one")
	}

	withToken := NewIngester(nil, &stubFetcher{token: true}, ingester.Calendar(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if !withToken.Reachable("^BVSP") {
		t.Error("every symbol is reachable once a token is set")
	}
}

func TestChunkWindowsTileTheRangeWithoutOverlap(t *testing.T) {
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, time.March, 31, 0, 0, 0, 0, time.UTC)

	windows := chunkWindows(from, to, 30)
	if len(windows) != 3 {
		t.Fatalf("90 days in 30-day chunks produced %d windows, want 3", len(windows))
	}

	if !windows[0][0].Equal(from) {
		t.Errorf("the first window starts at %s, want %s", windows[0][0], from)
	}
	if last := windows[len(windows)-1][1]; !last.Equal(to) {
		t.Errorf("the last window ends at %s, want %s", last, to)
	}

	for i, window := range windows {
		if window[1].Before(window[0]) {
			t.Fatalf("window %d is inverted: %s..%s", i, window[0], window[1])
		}
		if i == 0 {
			continue
		}
		if gap := window[0].Sub(windows[i-1][1]); gap != 24*time.Hour {
			t.Errorf("window %d starts %s after the previous one ends, want exactly one day", i, gap)
		}
	}
}

func TestChunkWindowsCoverASingleDay(t *testing.T) {
	day := time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC)

	windows := chunkWindows(day, day, 90)
	if len(windows) != 1 {
		t.Fatalf("a one-day range produced %d windows, want 1", len(windows))
	}
	if !windows[0][0].Equal(day) || !windows[0][1].Equal(day) {
		t.Errorf("the window is %s..%s, want %s..%s", windows[0][0], windows[0][1], day, day)
	}
}

func TestIntradayRangeNeverGoesBelowThreeMonths(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		from time.Time
		want string
	}{
		{now, MinIntradayRange},
		{now.AddDate(0, 0, -1), MinIntradayRange},
		{now.AddDate(0, 0, -80), MinIntradayRange},
		{now.AddDate(0, 0, -120), "6mo"},
		{now.AddDate(0, 0, -300), "1y"},
		{now.AddDate(-3, 0, 0), "5y"},
		{now.AddDate(-20, 0, 0), MaxIntradayRange},
	} {
		if got := IntradayRange(tc.from, now); got != tc.want {
			t.Errorf("a window starting %s resolves to range=%s, want %s",
				tc.from.Format(time.DateOnly), got, tc.want)
		}
	}
}

func TestValidateIntradayRangeRejectsTheDefectiveWindows(t *testing.T) {
	for _, token := range []string{"1d", "5d", "7d", "1mo", "ytd"} {
		if err := ValidateIntradayRange(token); err == nil {
			t.Errorf("range=%s returns brapi's incomplete intraday series and must be refused", token)
		}
	}
	for _, token := range []string{"3mo", "6mo", "1y", "2y", "5y", "10y", "max"} {
		if err := ValidateIntradayRange(token); err != nil {
			t.Errorf("range=%s should be accepted: %v", token, err)
		}
	}
}

func TestRefreshWindowWalksBackOverWeekendsAndHolidays(t *testing.T) {
	ingester := testIngester(t)
	calendar := ingester.Calendar()

	from, to, err := ingester.RefreshWindow(calendar.Date(2026, time.September, 8), 3)
	if err != nil {
		t.Fatalf("RefreshWindow: %v", err)
	}

	if got := to.Format(time.DateOnly); got != "2026-09-08" {
		t.Errorf("the window ends on %s, want 2026-09-08", got)
	}
	if got := from.Format(time.DateOnly); got != "2026-09-03" {
		t.Errorf("three sessions back from 2026-09-08 is %s, want 2026-09-03: the 7th is Independence Day and the 5th-6th a weekend", got)
	}
}

func TestRefreshWindowStartsFromTheLastTradingDay(t *testing.T) {
	ingester := testIngester(t)
	calendar := ingester.Calendar()

	_, to, err := ingester.RefreshWindow(calendar.Date(2026, time.August, 22), 1)
	if err != nil {
		t.Fatalf("RefreshWindow: %v", err)
	}
	if got := to.Format(time.DateOnly); got != "2026-08-21" {
		t.Errorf("running on a Saturday refreshes up to %s, want the Friday 2026-08-21", got)
	}
}

func TestStoreRefusesDerivedTimeframes(t *testing.T) {
	ingester := testIngester(t)
	calendar := ingester.Calendar()
	session, _ := calendar.Session(calendar.Date(2026, time.August, 20))

	candles := []market.Candle{{TS: session.Open, Open: 10, High: 11, Low: 9, Close: 10, Volume: 1}}

	for _, tf := range []market.Timeframe{market.TF15m, market.TF30m, market.TF1h} {
		if err := ingester.Store(context.Background(), 1, tf, candles); err == nil {
			t.Errorf("storing %s should fail: it is resampled on read", tf)
		}
	}
}

func TestBackfillAlwaysFetchesFiveMinutesByRange(t *testing.T) {
	ingester := testIngester(t)
	calendar := ingester.Calendar()

	report, err := ingester.Backfill(context.Background(), nil, BackfillOptions{
		Timeframe: market.TF5m,
		From:      calendar.Date(2026, time.August, 1),
		To:        calendar.Date(2026, time.August, 20),
	})
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if report.Range == "" {
		t.Fatal("a 5m backfill must carry a range token: brapi ignores startDate/endDate for intraday")
	}
	if err := ValidateIntradayRange(report.Range); err != nil {
		t.Errorf("the derived range is unusable: %v", err)
	}

	daily, err := ingester.Backfill(context.Background(), nil, BackfillOptions{
		Timeframe: market.TF1d,
		From:      calendar.Date(2026, time.August, 1),
		To:        calendar.Date(2026, time.August, 20),
	})
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if daily.Range != "" {
		t.Errorf("1d honours startDate/endDate and should not force a range, got %q", daily.Range)
	}
}

func TestBackfillRejectsADefectiveExplicitRange(t *testing.T) {
	ingester := testIngester(t)
	calendar := ingester.Calendar()

	_, err := ingester.Backfill(context.Background(), nil, BackfillOptions{
		Timeframe: market.TF5m,
		From:      calendar.Date(2026, time.August, 1),
		To:        calendar.Date(2026, time.August, 20),
		Range:     "1mo",
	})
	if err == nil {
		t.Error("--range 1mo returns the incomplete intraday series and must be refused")
	}
}

func TestBackfillRefusesDerivedTimeframesAndInvertedRanges(t *testing.T) {
	ingester := testIngester(t)
	calendar := ingester.Calendar()

	_, err := ingester.Backfill(context.Background(), nil, BackfillOptions{
		Timeframe: market.TF15m,
		From:      calendar.Date(2026, time.August, 1),
		To:        calendar.Date(2026, time.August, 20),
	})
	if err == nil {
		t.Error("backfilling a derived timeframe should fail")
	}

	_, err = ingester.Backfill(context.Background(), nil, BackfillOptions{
		Timeframe: market.TF1d,
		From:      calendar.Date(2026, time.August, 20),
		To:        calendar.Date(2026, time.August, 1),
	})
	if err == nil {
		t.Error("an inverted range should fail")
	}
}
