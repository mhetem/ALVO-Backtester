package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type cachePayload struct {
	Value int `json:"value"`
}

func serveCached(payload any, cacheControl, ifNoneMatch string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/candles", nil)
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}

	rec := httptest.NewRecorder()
	respondCached(rec, req, payload, cacheControl)
	return rec
}

func TestRespondCachedTagsTheBody(t *testing.T) {
	rec := serveCached(cachePayload{Value: 1}, cacheClosedRange, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if rec.Header().Get("ETag") == "" {
		t.Error("no ETag on a cacheable response")
	}
	if got := rec.Header().Get("Cache-Control"); got != cacheClosedRange {
		t.Errorf("Cache-Control is %q, want %q", got, cacheClosedRange)
	}
	if got := rec.Body.String(); got != `{"value":1}` {
		t.Errorf("body is %s", got)
	}
}

func TestRespondCachedAnswers304ForAMatchingETag(t *testing.T) {
	etag := serveCached(cachePayload{Value: 1}, cacheClosedRange, "").Header().Get("ETag")

	rec := serveCached(cachePayload{Value: 1}, cacheClosedRange, etag)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("304 carries a body: %s", rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != cacheClosedRange {
		t.Errorf("304 dropped Cache-Control: %q", got)
	}
}

func TestRespondCachedRevalidatesWhenTheBodyChanged(t *testing.T) {
	etag := serveCached(cachePayload{Value: 1}, cacheOpenRange, "").Header().Get("ETag")

	rec := serveCached(cachePayload{Value: 2}, cacheOpenRange, etag)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 when the payload moved on", rec.Code)
	}
	if rec.Header().Get("ETag") == etag {
		t.Error("a changed body reused the old ETag")
	}
}

func TestETagMatches(t *testing.T) {
	const etag = `"abc123"`

	cases := []struct {
		header string
		want   bool
	}{
		{"", false},
		{etag, true},
		{`W/"abc123"`, true},
		{`"other", "abc123"`, true},
		{`"other"`, false},
		{"*", true},
		{"  " + etag + "  ", true},
	}

	for _, tc := range cases {
		if got := etagMatches(tc.header, etag); got != tc.want {
			t.Errorf("etagMatches(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}
