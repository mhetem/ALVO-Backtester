package indicator

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func compute(t *testing.T, key string, candles []Candle) (Instance, Result) {
	t.Helper()

	instance, err := Parse(key)
	if err != nil {
		t.Fatalf("Parse(%q): %v", key, err)
	}
	return instance, Compute(instance.Indicator, candles)
}

func linearRamp(bars int, slope float64) []Candle {
	candles := make([]Candle, 0, bars)
	ts := time.Date(2020, 1, 2, 13, 0, 0, 0, time.UTC)

	for i := range bars {
		price := 10 + slope*float64(i)
		candles = append(candles, Candle{
			TS:     ts.AddDate(0, 0, i),
			Open:   price,
			High:   price + 0.5,
			Low:    price - 0.5,
			Close:  price,
			Volume: 1000,
		})
	}

	return candles
}

func TestTheCatalogueCoversEveryGroupThePlanNames(t *testing.T) {
	counts := map[Group]int{}
	for _, spec := range Catalog() {
		counts[spec.Group]++
	}

	for _, group := range Groups {
		if counts[group] == 0 {
			t.Errorf("no indicator is registered under %s", group)
		}
		delete(counts, group)
	}
	for group := range counts {
		t.Errorf("%s is used by an indicator but missing from Groups", group)
	}
}

func TestOnlySinglePriceIndicatorsTakeASource(t *testing.T) {
	want := []string{
		"bb", "dema", "ema", "hma", "hv", "macd", "mom", "roc", "rsi",
		"sma", "stddev", "stochrsi", "tema", "vwma", "wma",
	}

	got := []string{}
	for _, spec := range Catalog() {
		if spec.Sourced {
			got = append(got, spec.Name)
		}
	}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("indicators taking a source are %v, want %v", got, want)
	}
}

func TestAnIndicatorThatReadsSeveralPriceFieldsRejectsASource(t *testing.T) {
	for _, spec := range Catalog() {
		if spec.Sourced {
			continue
		}

		_, err := Parse(spec.Name + ":source=hl2")
		if err == nil {
			t.Errorf("%s accepted a source it cannot honour", spec.Name)
			continue
		}
		if !strings.Contains(err.Error(), "does not take a source") {
			t.Errorf("%s rejected a source with %q", spec.Name, err)
		}
	}
}

func TestEverySpecNamesItsOutputsOnceAndKeysBackToItself(t *testing.T) {
	for _, spec := range Catalog() {
		t.Run(spec.Name, func(t *testing.T) {
			if !slices.Contains(Groups, spec.Group) {
				t.Errorf("sits in unknown group %q", spec.Group)
			}

			seen := map[string]bool{}
			for _, name := range spec.Outputs {
				if strings.TrimSpace(name) == "" {
					t.Error("has an unnamed output")
				}
				if seen[name] {
					t.Errorf("names two outputs %q", name)
				}
				seen[name] = true
			}

			instance, err := New(spec.Name, nil, "")
			if err != nil {
				t.Fatalf("cannot be built from its own defaults: %v", err)
			}

			round, err := Parse(instance.Key)
			if err != nil {
				t.Fatalf("its own key %q does not parse: %v", instance.Key, err)
			}
			if round.Key != instance.Key {
				t.Errorf("key %q re-keys as %q", instance.Key, round.Key)
			}
		})
	}
}

func TestEveryGoldenCaseNamesADistinctIndicator(t *testing.T) {
	covered := map[string]bool{}
	for _, tc := range golden(t).Cases {
		instance, err := Parse(tc.Key)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.Key, err)
		}
		covered[instance.Spec.Name] = true
	}

	for _, spec := range Catalog() {
		if !covered[spec.Name] {
			t.Errorf("%s has no golden case", spec.Name)
		}
	}
}
