package api

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/indicator"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

func ramp(n int, first float64) []market.Candle {
	base := time.Date(2026, 6, 22, 13, 0, 0, 0, time.UTC)
	candles := make([]market.Candle, 0, n)

	for i := range n {
		price := first + float64(i)
		candles = append(candles, market.Candle{
			TS:     base.AddDate(0, 0, i),
			Open:   price,
			High:   price + 0.5,
			Low:    price - 0.5,
			Close:  price,
			Volume: int64(1000 + i),
		})
	}

	return candles
}

func TestIndicatorsRejectsWhatItCannotBuild(t *testing.T) {
	server := testServer(t)

	cases := []struct {
		target string
		want   string
	}{
		{"/api/v1/candles?symbol=PETR4&indicators=nope:9", `unknown indicator "nope"`},
		{"/api/v1/candles?symbol=PETR4&indicators=ema:0", "period must be between"},
		{"/api/v1/candles?symbol=PETR4&indicators=ema:9,rsi:banana", "period must be a number"},
		{"/api/v1/candles?symbol=PETR4&indicators=macd:26:12:9", "fast must be shorter than slow"},
		{"/api/v1/candles?symbol=PETR4&indicators=ema:9:source=vwap", `unknown source "vwap"`},
	}

	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
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

func TestIndicatorSeriesAlignWithTheCandlesTheyDescribe(t *testing.T) {
	candles := ramp(60, 10)
	prime, page := candles[:40], candles[40:]

	instances, err := indicator.ParseList("ema:9,macd:12:26:9")
	if err != nil {
		t.Fatalf("ParseList: %v", err)
	}

	bodies := computeIndicators(instances, prime, page)
	if len(bodies) != 2 {
		t.Fatalf("got %d indicator bodies, want 2", len(bodies))
	}

	for _, body := range bodies {
		if len(body.Series) == 0 {
			t.Fatalf("%s carries no series", body.Key)
		}
		for _, series := range body.Series {
			if series.Start != 0 {
				t.Errorf("%s/%s starts at %d, but priming should have it ready on the first bar",
					body.Key, series.Name, series.Start)
			}
			if len(series.Values) != len(page) {
				t.Errorf("%s/%s has %d values against %d candles", body.Key, series.Name, len(series.Values), len(page))
			}
		}
	}

	if bodies[0].Key != "ema:9" || !bodies[0].Overlay {
		t.Errorf("ema:9 came back as %s with overlay=%v", bodies[0].Key, bodies[0].Overlay)
	}
	if bodies[1].Overlay {
		t.Error("macd is drawn in its own pane, not on price")
	}
	if got := []string{bodies[1].Series[0].Name, bodies[1].Series[1].Name, bodies[1].Series[2].Name}; strings.Join(got, ",") != "macd,signal,histogram" {
		t.Errorf("macd outputs are %v, want macd, signal, histogram", got)
	}

	lag := page[10].Close - bodies[0].Series[0].Values[10]
	if want := (9.0+1)/2 - 1; math.Abs(lag-want) > 1e-9 {
		t.Errorf("ema:9 lags a unit ramp by %v, want the steady-state %v", lag, want)
	}
}

func TestAnUnprimedIndicatorReportsWhereItStarts(t *testing.T) {
	page := ramp(30, 10)

	instances, err := indicator.ParseList("sma:20")
	if err != nil {
		t.Fatalf("ParseList: %v", err)
	}

	bodies := computeIndicators(instances, nil, page)
	series := bodies[0].Series[0]

	if series.Start != 19 {
		t.Errorf("sma:20 over a cold 30-bar page starts at %d, want 19", series.Start)
	}
	if len(series.Values) != len(page)-19 {
		t.Errorf("sma:20 emits %d values, want %d", len(series.Values), len(page)-19)
	}
	if bodies[0].Warmup != 19 {
		t.Errorf("the body reports a warmup of %d, want 19", bodies[0].Warmup)
	}
}

func TestAnIndicatorThatNeverWarmsUpEmitsAnEmptySeries(t *testing.T) {
	page := ramp(5, 10)

	instances, err := indicator.ParseList("bb:20:2")
	if err != nil {
		t.Fatalf("ParseList: %v", err)
	}

	bodies := computeIndicators(instances, nil, page)
	if len(bodies[0].Series) != 3 {
		t.Fatalf("got %d series, want the three bands", len(bodies[0].Series))
	}

	raw, err := json.Marshal(bodies[0])
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	for _, series := range bodies[0].Series {
		if series.Start != len(page) {
			t.Errorf("%s starts at %d, want %d — one past the last candle", series.Name, series.Start, len(page))
		}
		if len(series.Values) != 0 {
			t.Errorf("%s emitted %d values without warming up", series.Name, len(series.Values))
		}
	}
	if !strings.Contains(string(raw), `"values":[]`) {
		t.Errorf("an empty series encodes as something other than []: %s", raw)
	}
}

func TestTheCandleBodyCarriesNoIndicatorKeyWhenNoneWereAsked(t *testing.T) {
	body := newCandlesBody("PETR4", market.Page{Timeframe: market.TF1d, Base: market.TF1d, Candles: ramp(3, 10)})

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if strings.Contains(string(raw), "indicators") {
		t.Errorf("the body mentions indicators when none were requested: %s", raw)
	}
}

func TestIndicatorBodyDescribesItselfWellEnoughForAPicker(t *testing.T) {
	instances, err := indicator.ParseList("bb:20:2.5:source=hlc3")
	if err != nil {
		t.Fatalf("ParseList: %v", err)
	}

	body := computeIndicators(instances, nil, ramp(40, 10))[0]

	if body.Key != "bb:20:2.5:source=hlc3" {
		t.Errorf("key is %q", body.Key)
	}
	if body.Name != "bb" || body.Title != "Bollinger Bands" {
		t.Errorf("name/title are %q/%q", body.Name, body.Title)
	}
	if body.Source != "hlc3" {
		t.Errorf("source is %q, want hlc3", body.Source)
	}
	if body.Params["period"] != 20 || body.Params["mult"] != 2.5 {
		t.Errorf("params are %v", body.Params)
	}
}
