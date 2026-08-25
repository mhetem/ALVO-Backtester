package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/mhetem/ALVO-Backtester/internal/indicator"
)

func decodeCatalog(t *testing.T, rec *httptest.ResponseRecorder) catalogBody {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var body catalogBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parsing %q: %v", rec.Body.String(), err)
	}
	return body
}

func TestTheCatalogueListsEveryRegisteredIndicator(t *testing.T) {
	server := testServer(t)

	body := decodeCatalog(t, get(t, server.handleIndicators, "/api/v1/indicators"))

	if body.Count != len(body.Indicators) {
		t.Errorf("the body counts %d indicators but carries %d", body.Count, len(body.Indicators))
	}
	if body.Count != len(indicator.Catalog()) {
		t.Errorf("the endpoint lists %d indicators against a registry of %d",
			body.Count, len(indicator.Catalog()))
	}
	if body.MaxPerRequest != indicator.MaxInstances {
		t.Errorf("the body caps a request at %d, want %d", body.MaxPerRequest, indicator.MaxInstances)
	}
	if len(body.Groups) != len(indicator.Groups) || len(body.Sources) != len(indicator.Sources) {
		t.Errorf("the body offers %d groups and %d sources", len(body.Groups), len(body.Sources))
	}
}

func TestEveryCatalogueEntryIsEnoughToBuildARequest(t *testing.T) {
	server := testServer(t)

	body := decodeCatalog(t, get(t, server.handleIndicators, "/api/v1/indicators"))

	for _, entry := range body.Indicators {
		t.Run(entry.Name, func(t *testing.T) {
			if entry.Title == "" {
				t.Error("has no title")
			}
			if !slices.Contains(body.Groups, entry.Group) {
				t.Errorf("sits in group %q, which the body does not list", entry.Group)
			}
			if len(entry.Outputs) == 0 {
				t.Error("declares no outputs")
			}

			instance, err := indicator.Parse(entry.Key)
			if err != nil {
				t.Fatalf("its own key %q does not parse: %v", entry.Key, err)
			}
			if instance.Spec.Name != entry.Name {
				t.Errorf("key %q builds a %s", entry.Key, instance.Spec.Name)
			}
			if instance.Indicator.Warmup() != entry.Warmup {
				t.Errorf("the body reports a warmup of %d against %d",
					entry.Warmup, instance.Indicator.Warmup())
			}

			for _, param := range entry.Params {
				if param.Min > param.Max {
					t.Errorf("%s ranges %v..%v", param.Name, param.Min, param.Max)
				}
				if param.Default < param.Min || param.Default > param.Max {
					t.Errorf("%s defaults to %v, outside %v..%v",
						param.Name, param.Default, param.Min, param.Max)
				}
				if param.Kind != "int" && param.Kind != "float" {
					t.Errorf("%s is of kind %q", param.Name, param.Kind)
				}
			}
		})
	}
}

func TestTheCatalogueIsCachedAndRevalidates(t *testing.T) {
	server := testServer(t)

	first := get(t, server.handleIndicators, "/api/v1/indicators")
	if got := first.Header().Get("Cache-Control"); got != cacheCatalog {
		t.Errorf("Cache-Control is %q, want %q", got, cacheCatalog)
	}

	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("the catalogue came back without an ETag")
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/indicators", nil)
	request.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	server.handleIndicators(second, request)

	if second.Code != http.StatusNotModified {
		t.Errorf("a repeat request answered %d, want 304", second.Code)
	}
}

func TestTheCatalogueMarksOverlaysApartFromPanes(t *testing.T) {
	server := testServer(t)

	body := decodeCatalog(t, get(t, server.handleIndicators, "/api/v1/indicators"))

	overlays := map[string]bool{}
	for _, entry := range body.Indicators {
		overlays[entry.Name] = entry.Overlay
	}

	for name, want := range map[string]bool{
		"sma":        true,
		"bb":         true,
		"keltner":    true,
		"donchian":   true,
		"supertrend": true,
		"psar":       true,
		"ichimoku":   true,
		"vwma":       true,
		"rsi":        false,
		"macd":       false,
		"adx":        false,
		"atr":        false,
		"obv":        false,
		"stoch":      false,
	} {
		if overlays[name] != want {
			t.Errorf("%s draws on the price pane: %v, want %v", name, overlays[name], want)
		}
	}
}
