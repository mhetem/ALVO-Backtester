package brapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type countingRecorder struct{ calls atomic.Int32 }

func (c *countingRecorder) RecordRequests(_ context.Context, _ time.Time, n int32) error {
	c.calls.Add(n)
	return nil
}

func testClient(t *testing.T, handler http.HandlerFunc, opts ...Option) *Client {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	base := []Option{
		WithBaseURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithRateLimit(0, 1),
		WithLogger(slog.New(slog.DiscardHandler)),
	}

	client := New(append(base, opts...)...)
	client.sleep = func(context.Context, time.Duration) error { return nil }
	return client
}

func TestQuoteBuildsRequestAndDecodesResults(t *testing.T) {
	var path, query string

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.Query().Encode()
		_, _ = w.Write([]byte(`{"results":[{"symbol":"PETR4","shortName":"PETROBRAS PN","longName":"Petroleo Brasileiro S.A.","currency":"BRL","regularMarketTime":"2026-08-19T21:00:00.000Z"}]}`))
	}, WithToken("secret"))

	quotes, err := client.Quote(context.Background(), []string{"petr4"}, QuoteOptions{Range: "1y", Interval: "1d"})
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if path != "/quote/PETR4" {
		t.Errorf("path was %q, want /quote/PETR4", path)
	}
	if want := "interval=1d&range=1y&token=secret"; query != want {
		t.Errorf("query was %q, want %q", query, want)
	}

	if len(quotes) != 1 {
		t.Fatalf("got %d quotes, want 1", len(quotes))
	}
	if quotes[0].ShortName != "PETROBRAS PN" {
		t.Errorf("short name was %q", quotes[0].ShortName)
	}
	if got := quotes[0].RegularMarketTime.UTC(); got.Hour() != 21 {
		t.Errorf("regularMarketTime was %s", got)
	}
}

func TestQuoteReturnsErrNotFoundOnEmptyResults(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[]}`))
	})

	if _, err := client.Quote(context.Background(), []string{"NOPE3"}, QuoteOptions{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Quote returned %v, want ErrNotFound", err)
	}
}

func TestGetRetriesOn429AndHonoursRetryAfter(t *testing.T) {
	var attempts atomic.Int32
	delays := []time.Duration{}

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"rate limited"}`))
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"symbol":"PETR4"}]}`))
	})
	client.sleep = func(_ context.Context, d time.Duration) error {
		delays = append(delays, d)
		return nil
	}

	if _, err := client.Quote(context.Background(), []string{"PETR4"}, QuoteOptions{}); err != nil {
		t.Fatalf("Quote: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("made %d attempts, want 2", got)
	}
	if len(delays) != 1 || delays[0] != 2*time.Second {
		t.Errorf("delays were %v, want [2s]", delays)
	}
}

func TestGetGivesUpAfterMaxAttempts(t *testing.T) {
	var attempts atomic.Int32

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}, WithMaxAttempts(3))

	_, err := client.Quote(context.Background(), []string{"PETR4"}, QuoteOptions{})

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Quote returned %v, want an *APIError", err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("status was %d, want 500", apiErr.StatusCode)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("made %d attempts, want 3", got)
	}
}

func TestGetDoesNotRetryClientErrors(t *testing.T) {
	var attempts atomic.Int32

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid token"}`))
	})

	_, err := client.Quote(context.Background(), []string{"PETR4"}, QuoteOptions{})
	if !IsUnauthorized(err) {
		t.Fatalf("Quote returned %v, want an unauthorized error", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("made %d attempts, want 1", got)
	}
	if !strings.Contains(err.Error(), "invalid token") {
		t.Errorf("error %q dropped the brapi message", err)
	}
}

func TestEveryAttemptIsCountedAgainstQuota(t *testing.T) {
	recorder := &countingRecorder{}

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}, WithMaxAttempts(3), WithUsageRecorder(recorder))

	if _, err := client.Quote(context.Background(), []string{"PETR4"}, QuoteOptions{}); err == nil {
		t.Fatal("Quote succeeded, want an error")
	}
	if got := recorder.calls.Load(); got != 3 {
		t.Errorf("recorded %d requests, want 3", got)
	}
}

func TestTokenNeverLeaksIntoTransportErrors(t *testing.T) {
	const token = "super-secret-token"

	client := New(
		WithBaseURL("http://127.0.0.1:1"),
		WithToken(token),
		WithMaxAttempts(1),
		WithRateLimit(0, 1),
		WithLogger(slog.New(slog.DiscardHandler)),
	)

	_, err := client.Quote(context.Background(), []string{"PETR4"}, QuoteOptions{})
	if err == nil {
		t.Fatal("Quote succeeded against a dead port")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("token leaked into %q", err)
	}
	if !strings.Contains(err.Error(), redacted) {
		t.Errorf("error %q was not redacted", err)
	}
}

func TestQuoteRejectsMalformedTickers(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("client sent a request for a malformed ticker")
	})

	if _, err := client.Quote(context.Background(), []string{"PETR4/../admin"}, QuoteOptions{}); err == nil {
		t.Fatal("Quote accepted a malformed ticker")
	}
}

func TestAvailableDecodesLists(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"indexes":["^BVSP"],"stocks":["PETR4","VALE3"]}`))
	})

	available, err := client.Available(context.Background(), "")
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if len(available.Stocks) != 2 || available.Stocks[0] != "PETR4" {
		t.Errorf("stocks were %v", available.Stocks)
	}
	if len(available.Indexes) != 1 || available.Indexes[0] != "^BVSP" {
		t.Errorf("indexes were %v", available.Indexes)
	}
}

func TestIsFreeTicker(t *testing.T) {
	for _, ticker := range []string{"PETR4", "petr4", " VALE3 ", "ITUB4", "MGLU3"} {
		if !IsFreeTicker(ticker) {
			t.Errorf("IsFreeTicker(%q) = false", ticker)
		}
	}
	for _, ticker := range []string{"BBAS3", "", "PETR3"} {
		if IsFreeTicker(ticker) {
			t.Errorf("IsFreeTicker(%q) = true", ticker)
		}
	}
}
