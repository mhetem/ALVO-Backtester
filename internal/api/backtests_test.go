package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func queueRun(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/backtests", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUserID, uuid.New()))

	rec := httptest.NewRecorder()
	testServer(t).handleCreateBacktest(rec, req)

	return rec
}

func rejects(t *testing.T, body, want string) {
	t.Helper()

	rec := queueRun(t, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body)
	}
	if message := decodeError(t, rec); !strings.Contains(message, want) {
		t.Errorf("error = %q, want it to mention %q", message, want)
	}
}

func TestQueueingABacktestChecksTheRequestBeforeTheDatabase(t *testing.T) {
	id := uuid.New().String()

	rejects(t, `{"strategy_id": "not-a-uuid"}`, "UUID")
	rejects(t, `{"strategy_id": "`+id+`", "timeframe": "3m"}`, "unknown timeframe")
	rejects(t, `{"strategy_id": "`+id+`", "timeframe": "1d"}`, "symbol is required")
	rejects(t, `{"strategy_id": "`+id+`", "timeframe": "1d", "symbol": "PETR4"}`, "start is required")
	rejects(t, `{"strategy_id": "`+id+`", "timeframe": "1d", "symbol": "PETR4", "start": "2024-01-02"}`, "end is required")
	rejects(t, `{"strategy_id": "`+id+`", "timeframe": "1d", "symbol": "PETR4", "start": "2024-06-01", "end": "2024-01-02"}`,
		"end must not be before start")
	rejects(t, `{"strategy_id": "`+id+`", "timeframe": "1d", "symbol": "PETR4", "start": "2024-01-02", "end": "2024-06-01", "capital_cents": 1}`,
		"capital_cents")
}

func TestAnUnsignedRequestNeverReachesTheQueue(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backtests", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	testServer(t).handleCreateBacktest(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestCapitalDefaultsRatherThanFailingWhenItIsLeftOut(t *testing.T) {
	capital, err := backtestCapital(0)
	if err != nil {
		t.Fatalf("an omitted capital was rejected: %v", err)
	}
	if capital != defaultCapitalCents {
		t.Errorf("capital = %d cents, want the default %d", capital, defaultCapitalCents)
	}

	if _, err := backtestCapital(maxCapitalCents + 1); err == nil {
		t.Error("a capital beyond the ceiling was accepted")
	}
}
