package brapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFuturesListPagesUntilTotalPages(t *testing.T) {
	var pages []string
	var limits []string

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		pages = append(pages, r.URL.Query().Get("page"))
		limits = append(limits, r.URL.Query().Get("limit"))

		page := r.URL.Query().Get("page")
		_, _ = fmt.Fprintf(w, `{"futures":[{"symbol":"WIN%s26","underlyingAsset":"WIN","expirationDate":"2026-10-14"}],"pagination":{"page":%s,"limit":100,"total":3,"totalPages":3}}`, page, page)
	})

	contracts, err := client.FuturesList(context.Background(), false)
	if err != nil {
		t.Fatalf("FuturesList: %v", err)
	}

	if len(contracts) != 3 {
		t.Errorf("got %d contracts, want one per page (3)", len(contracts))
	}
	if strings.Join(pages, ",") != "1,2,3" {
		t.Errorf("requested pages %v, want 1,2,3", pages)
	}
	for _, limit := range limits {
		if limit != "100" {
			t.Errorf("requested limit %q, want the 100 ceiling brapi enforces", limit)
		}
	}
}

// The default listing omits expired contracts, and a continuous series assembled without
// them rolls across nothing at all.
func TestFuturesListAsksForExpiredContractsOnlyWhenTold(t *testing.T) {
	for _, tc := range []struct {
		include bool
		want    string
	}{
		{true, "true"},
		{false, ""},
	} {
		t.Run(fmt.Sprintf("include=%v", tc.include), func(t *testing.T) {
			var got string

			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.Query().Get("includeExpired")
				_, _ = w.Write([]byte(`{"futures":[],"pagination":{"page":1,"limit":100,"total":0,"totalPages":1}}`))
			})

			if _, err := client.FuturesList(context.Background(), tc.include); err != nil {
				t.Fatalf("FuturesList: %v", err)
			}
			if got != tc.want {
				t.Errorf("includeExpired = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFuturesListStopsOnAnEmptyPage(t *testing.T) {
	calls := 0

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"futures":[],"pagination":{"page":1,"limit":100,"total":9000,"totalPages":90}}`))
	})

	if _, err := client.FuturesList(context.Background(), true); err != nil {
		t.Fatalf("FuturesList: %v", err)
	}
	if calls != 1 {
		t.Errorf("made %d calls, want 1: an empty page ends the walk regardless of totalPages", calls)
	}
}

// brapi names this parameter `symbol`, not `symbols`; sending the plural fails validation
// with a 400 that reads like a missing route.
func TestFuturesHistorySendsSingularSymbolAndStartDate(t *testing.T) {
	var query, path string

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.Query().Encode()
		_, _ = w.Write([]byte(`{"future":{"symbol":"WINV26","underlyingAsset":"WIN","expirationDate":"2026-10-14","contractMultiplier":0.2,"history":[{"date":1787616000,"open":null,"high":177775,"low":173875,"close":177465,"settlement":177497,"volume":18744812}]}}`))
	})

	contract, err := client.FuturesHistory(context.Background(), "winv26", time.Date(2025, 6, 10, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FuturesHistory: %v", err)
	}

	if !strings.HasSuffix(path, "/v2/futures/historical") {
		t.Errorf("path = %q, want the futures historical endpoint", path)
	}
	if !strings.Contains(query, "symbol=WINV26") {
		t.Errorf("query = %q, want a singular uppercased symbol", query)
	}
	if strings.Contains(query, "symbols=") {
		t.Errorf("query = %q, sent the plural form brapi rejects", query)
	}
	if !strings.Contains(query, "startDate=2025-06-10") {
		t.Errorf("query = %q, want startDate to widen the default one-year window", query)
	}

	if len(contract.History) != 1 {
		t.Fatalf("got %d bars, want 1", len(contract.History))
	}
	bar := contract.History[0]
	if bar.Open != nil {
		t.Errorf("open = %v, want nil: brapi never serves an open for futures", *bar.Open)
	}
	if bar.Settlement == nil || *bar.Settlement != 177497 {
		t.Errorf("settlement = %v, want 177497", bar.Settlement)
	}
	if got := bar.TS().Format(time.DateOnly); got != "2026-08-25" {
		t.Errorf("bar timestamp = %s, want 2026-08-25", got)
	}
}

func TestFuturesHistoryOmitsStartDateWhenZero(t *testing.T) {
	var query string

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query().Encode()
		_, _ = w.Write([]byte(`{"future":{"symbol":"WINV26"}}`))
	})

	if _, err := client.FuturesHistory(context.Background(), "WINV26", time.Time{}); err != nil {
		t.Fatalf("FuturesHistory: %v", err)
	}
	if strings.Contains(query, "startDate") {
		t.Errorf("query = %q, want no startDate for a zero time", query)
	}
}

func TestFuturesHistoryRejectsABlankSymbol(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("a blank symbol should not reach the network")
		_, _ = w.Write([]byte(`{}`))
	})

	if _, err := client.FuturesHistory(context.Background(), "   ", time.Time{}); err == nil {
		t.Error("FuturesHistory accepted a blank symbol")
	}
}

func TestFuturesHistoryReportsAnUnknownContractAsNotFound(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"future":null}`))
	})

	_, err := client.FuturesHistory(context.Background(), "WINZ99", time.Time{})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestFuturesFloorDateParses(t *testing.T) {
	if got := FuturesFloorDate().Format(time.DateOnly); got != FuturesFloor {
		t.Errorf("FuturesFloorDate() = %s, want %s", got, FuturesFloor)
	}
}

func TestFuturesTermStructureReturnsTheWholeCurveInOneRequest(t *testing.T) {
	var path, query string

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.Query().Encode()
		_, _ = w.Write([]byte(`{"asset":"WIN","contracts":[
			{"symbol":"WINV26","underlyingAsset":"WIN","expirationDate":"2026-10-14","contractMultiplier":0.2,"date":1787616000,"open":null,"high":177775,"low":173875,"close":177465,"settlement":177497,"volume":18744812},
			{"symbol":"WINZ26","underlyingAsset":"WIN","expirationDate":"2026-12-16","contractMultiplier":0.2,"date":1787616000,"open":null,"high":null,"low":null,"close":null,"settlement":181113,"volume":null}
		]}`))
	})

	contracts, err := client.FuturesTermStructure(context.Background(), " win ")
	if err != nil {
		t.Fatalf("FuturesTermStructure: %v", err)
	}

	if !strings.HasSuffix(path, "/v2/futures/term-structure") {
		t.Errorf("path = %q, want the term-structure endpoint", path)
	}
	if !strings.Contains(query, "asset=WIN") {
		t.Errorf("query = %q, want an uppercased asset", query)
	}

	if len(contracts) != 2 {
		t.Fatalf("got %d contracts, want 2", len(contracts))
	}

	// Metadata and the bar arrive on the same object; both embedded structs must populate.
	front := contracts[0]
	if front.Symbol != "WINV26" || front.ExpirationDate != "2026-10-14" {
		t.Errorf("metadata did not decode: %+v", front.FutureContract)
	}
	if front.Settlement == nil || *front.Settlement != 177497 {
		t.Errorf("settlement = %v, want 177497", front.Settlement)
	}
	if front.TS().Format(time.DateOnly) != "2026-08-25" {
		t.Errorf("date = %s, want 2026-08-25", front.TS().Format(time.DateOnly))
	}

	// A contract that did not trade still settles; that is the column the series is built on.
	back := contracts[1]
	if back.Close != nil {
		t.Error("an untraded contract reported a close")
	}
	if back.Settlement == nil || *back.Settlement != 181113 {
		t.Errorf("untraded contract settlement = %v, want 181113", back.Settlement)
	}
}

func TestFuturesTermStructureRejectsABlankAsset(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("a blank asset should not reach the network")
		_, _ = w.Write([]byte(`{}`))
	})

	if _, err := client.FuturesTermStructure(context.Background(), "  "); err == nil {
		t.Error("FuturesTermStructure accepted a blank asset")
	}
}
