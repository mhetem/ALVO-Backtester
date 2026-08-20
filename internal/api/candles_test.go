package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/config"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

func testServer(t *testing.T) *Server {
	t.Helper()

	calendar, err := market.LoadCalendar(os.DirFS("../.."), market.HolidaysFile)
	if err != nil {
		t.Fatalf("LoadCalendar: %v", err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewServer(config.Config{}, nil, log, nil, calendar)
}

func get(t *testing.T, handler http.HandlerFunc, target string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parsing error body %q: %v", rec.Body.String(), err)
	}
	return body.Error
}

func TestCandlesRejectsBadRequestsBeforeTouchingTheDatabase(t *testing.T) {
	server := testServer(t)

	cases := []struct {
		name   string
		target string
		want   string
	}{
		{"no symbol", "/api/v1/candles", "symbol is required"},
		{"unknown timeframe", "/api/v1/candles?symbol=PETR4&timeframe=1m", `unknown timeframe "1m"`},
		{"unknown timeframe", "/api/v1/candles?symbol=PETR4&timeframe=4h", `unknown timeframe "4h"`},
		{"bad from", "/api/v1/candles?symbol=PETR4&from=22-06-2026", "from must be a YYYY-MM-DD date"},
		{"bad to", "/api/v1/candles?symbol=PETR4&to=yesterday", "to must be a YYYY-MM-DD date"},
		{"inverted window", "/api/v1/candles?symbol=PETR4&from=2026-08-20&to=2026-08-01", "from must not be after to"},
		{"bad limit", "/api/v1/candles?symbol=PETR4&limit=many", "limit must be a whole number"},
		{"zero limit", "/api/v1/candles?symbol=PETR4&limit=0", "limit must be at least 1"},
		{"bad cursor", "/api/v1/candles?symbol=PETR4&cursor=2026-06-22", "cursor must be an RFC3339 timestamp"},
	}

	for _, tc := range cases {
		t.Run(tc.name+" "+tc.target, func(t *testing.T) {
			rec := get(t, server.handleCandles, tc.target)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", rec.Code)
			}
			if got := decodeError(t, rec); !strings.Contains(got, tc.want) {
				t.Errorf("error is %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

func TestUnknownTimeframeListsTheValidSet(t *testing.T) {
	server := testServer(t)
	rec := get(t, server.handleCandles, "/api/v1/candles?symbol=PETR4&timeframe=2h")

	message := decodeError(t, rec)
	for _, tf := range market.Timeframes {
		if !strings.Contains(message, tf.String()) {
			t.Errorf("%q does not list %s", message, tf)
		}
	}
}

func TestCandlesBodyIsColumnar(t *testing.T) {
	ts := time.Date(2026, 6, 22, 13, 0, 0, 0, time.UTC)
	page := market.Page{
		Timeframe: market.TF15m,
		Base:      market.TF5m,
		HasMore:   true,
		Cursor:    ts,
		Candles: []market.Candle{
			{TS: ts, Open: 30.1, High: 30.9, Low: 30.0, Close: 30.5, Volume: 1200},
			{TS: ts.Add(15 * time.Minute), Open: 30.5, High: 31.2, Low: 30.4, Close: 31.0, Volume: 900},
		},
	}

	body := newCandlesBody("PETR4", page)

	if body.Count != 2 {
		t.Fatalf("count is %d, want 2", body.Count)
	}
	for name, length := range map[string]int{
		"ts": len(body.TS), "o": len(body.Open), "h": len(body.High),
		"l": len(body.Low), "c": len(body.Close), "v": len(body.Volume),
	} {
		if length != body.Count {
			t.Errorf("%s has %d entries, want %d", name, length, body.Count)
		}
	}

	if body.TS[0] != ts.Unix() {
		t.Errorf("ts[0] is %d, want %d seconds since epoch", body.TS[0], ts.Unix())
	}
	if body.Timeframe != "15m" || body.Base != "5m" {
		t.Errorf("timeframe/base are %s/%s, want 15m/5m", body.Timeframe, body.Base)
	}
	if body.NextCursor != "2026-06-22T13:00:00Z" {
		t.Errorf("next_cursor is %q, want 2026-06-22T13:00:00Z", body.NextCursor)
	}
}

func TestCandlesBodyOmitsTheCursorWhenTheWindowIsExhausted(t *testing.T) {
	body := newCandlesBody("PETR4", market.Page{Timeframe: market.TF1d, Base: market.TF1d, Candles: []market.Candle{}})

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if strings.Contains(string(raw), "next_cursor") {
		t.Errorf("body carries a cursor with nothing older to fetch: %s", raw)
	}
	if !strings.Contains(string(raw), `"ts":[]`) {
		t.Errorf("empty series encodes ts as something other than []: %s", raw)
	}
}
