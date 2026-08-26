package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mhetem/ALVO-Backtester/internal/strategy"
)

const validSpec = `{
  "version": 1,
  "inputs": {
    "fast": {"indicator": "ema", "params": {"period": 9}},
    "slow": {"indicator": "ema", "params": {"period": 21}}
  },
  "entry": {"long": {"crosses_above": ["fast", "slow"]}},
  "exit": {"long": {"any": [
    {"crosses_below": ["fast", "slow"]},
    {"stop_loss": {"type": "atr", "period": 14, "mult": 2}}
  ]}},
  "sizing": {"type": "pct_equity", "value": 0.95}
}`

func quiet() *Server {
	return &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func validate(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/strategies/validate", strings.NewReader(body))
	rec := httptest.NewRecorder()
	quiet().handleValidateStrategy(rec, req)

	return rec
}

func TestValidatingASpecReturnsWhatARunWouldNeed(t *testing.T) {
	rec := validate(t, `{"spec": `+validSpec+`}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body)
	}

	var body validationBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	if !body.Valid {
		t.Error("a spec that parses came back invalid")
	}
	if body.Plan == nil {
		t.Fatal("a validated spec came back with no plan")
	}

	if body.Plan.Inputs != 2 {
		t.Errorf("inputs = %d, want 2", body.Plan.Inputs)
	}
	if len(body.Plan.Indicators) != 3 {
		t.Errorf("indicators = %v, want the two emas and the stop's atr", body.Plan.Indicators)
	}
	if !body.Plan.Long.StopLoss {
		t.Error("the plan lost the stop the spec attached")
	}
	if body.Plan.Long.TakeProfit {
		t.Error("the plan invented a target the spec never asked for")
	}
	if !body.Plan.Long.RuleExit {
		t.Error("the plan lost the crossing the exit was written with")
	}
	if body.Plan.Warmup < 21 {
		t.Errorf("warmup = %d, want at least the slow ema's", body.Plan.Warmup)
	}

	if string(body.Spec) == validSpec {
		t.Error("the spec came back exactly as sent, so it was never canonicalised")
	}
}

func TestARejectedSpecSaysWhichNodeIsWrong(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		pointer string
	}{
		{
			"an unknown indicator",
			`{"spec": {"inputs": {"a": {"indicator": "nope"}}, "entry": {"long": {"gt": ["a", 1]}}, "sizing": {"type": "fixed_qty", "value": 100}}}`,
			"/inputs/a/indicator",
		},
		{
			"an operand naming nothing",
			`{"spec": {"inputs": {"a": {"indicator": "ema"}}, "entry": {"long": {"gt": ["b", 1]}}, "sizing": {"type": "fixed_qty", "value": 100}}}`,
			"/entry/long/gt/0",
		},
		{
			"a sizing rule nothing can measure",
			`{"spec": {"inputs": {"a": {"indicator": "ema"}}, "entry": {"long": {"gt": ["a", 1]}}, "sizing": {"type": "risk_pct", "value": 0.01}}}`,
			"/sizing",
		},
		{
			"no spec at all",
			`{"name": "nameless"}`,
			"",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			rec := validate(t, test.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body)
			}

			var fault faultBody
			if err := json.Unmarshal(rec.Body.Bytes(), &fault); err != nil {
				t.Fatalf("decoding: %v", err)
			}
			if fault.Pointer != test.pointer {
				t.Errorf("pointer = %q, want %q (error: %s)", fault.Pointer, test.pointer, fault.Error)
			}
			if fault.Error == "" {
				t.Error("a rejected spec came back with no reason")
			}
		})
	}
}

func TestASpecTooLargeToBeAStrategyIsRefused(t *testing.T) {
	rec := validate(t, `{"spec": {"inputs": {"a": "`+strings.Repeat("x", maxSpecBytes)+`"}}}`)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestAStrategyNeedsAUsableName(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"", "needs a name"},
		{"   ", "needs a name"},
		{strings.Repeat("n", maxStrategyName+1), "at most"},
	}

	for _, test := range cases {
		_, err := normalizeStrategyName(test.name)
		if err == nil {
			t.Errorf("%q was accepted as a name", test.name)
			continue
		}
		if !strings.Contains(err.Error(), test.want) {
			t.Errorf("%q: error = %q, want it to mention %q", test.name, err, test.want)
		}
	}

	got, err := normalizeStrategyName("  EMA cross  ")
	if err != nil || got != "EMA cross" {
		t.Errorf("normalizing = %q, %v, want %q trimmed", got, err, "EMA cross")
	}
}

func TestAPlanSummaryIsOmittedRatherThanFaked(t *testing.T) {
	if describePlan(nil) != nil {
		t.Error("a missing plan came back as a summary")
	}

	spec, err := strategy.Parse([]byte(validSpec))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	plan, err := strategy.Compile(spec)
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}

	body := describePlan(plan)
	if body == nil {
		t.Fatal("a compiled plan came back with no summary")
	}
	if body.Slots != len(plan.Slots) || body.Depth != plan.Depth {
		t.Errorf("summary = %+v, want it to mirror the plan", body)
	}
}
