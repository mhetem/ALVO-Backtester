package indicator

import (
	"strings"
	"testing"
)

func TestParseCanonicalisesEveryWayOfSpellingTheSameIndicator(t *testing.T) {
	cases := []struct {
		text string
		key  string
	}{
		{"ema:9", "ema:9"},
		{" EMA : 9 ", "ema:9"},
		{"ema:period=9", "ema:9"},
		{"ema", "ema:20"},
		{"macd", "macd:12:26:9"},
		{"macd:12:26:9", "macd:12:26:9"},
		{"macd:signal=9:fast=12:slow=26", "macd:12:26:9"},
		{"bb:20:2", "bb:20:2"},
		{"bb:20:2.5", "bb:20:2.5"},
		{"ema:9:source=hl2", "ema:9:source=hl2"},
		{"ema:9:source=close", "ema:9"},
		{"rsi:14", "rsi:14"},
	}

	for _, tc := range cases {
		instance, err := Parse(tc.text)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.text, err)
			continue
		}
		if instance.Key != tc.key {
			t.Errorf("Parse(%q) keys as %q, want %q", tc.text, instance.Key, tc.key)
		}
	}
}

func TestParseRejectsWhatTheRegistryCannotBuild(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"", "names no indicator"},
		{"nope:9", `unknown indicator "nope"`},
		{"ema:", "empty parameter"},
		{"ema:nine", "period must be a number"},
		{"ema:9.5", "period must be a whole number"},
		{"ema:0", "period must be between 1 and 2000"},
		{"ema:9000", "period must be between 1 and 2000"},
		{"ema:9:21", "ema takes 1 parameter"},
		{"ema:9:period=21", "period is set twice"},
		{"ema:9:source=vwap", `unknown source "vwap"`},
		{"ema:9:decay=0.5", `ema has no parameter "decay"`},
		{"macd:26:12:9", "fast must be shorter than slow"},
		{"bb:20:0", "mult must be between 0.1 and 10"},
	}

	for _, tc := range cases {
		_, err := Parse(tc.text)
		if err == nil {
			t.Errorf("Parse(%q) succeeded, want an error mentioning %q", tc.text, tc.want)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Parse(%q) failed with %q, want it to mention %q", tc.text, err, tc.want)
		}
	}
}

func TestUnknownIndicatorNamesTheCatalogue(t *testing.T) {
	_, err := Parse("stochastic:14")
	if err == nil {
		t.Fatal("an unregistered indicator parsed")
	}
	for _, name := range Names() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("%q does not list %s", err, name)
		}
	}
}

func TestParseListDropsDuplicatesAndKeepsOrder(t *testing.T) {
	instances, err := ParseList("ema:9,rsi:14,ema:period=9,ema:21")
	if err != nil {
		t.Fatalf("ParseList: %v", err)
	}

	want := []string{"ema:9", "rsi:14", "ema:21"}
	if len(instances) != len(want) {
		t.Fatalf("parsed %d instances, want %d", len(instances), len(want))
	}
	for i, key := range want {
		if instances[i].Key != key {
			t.Errorf("instance %d is %s, want %s", i, instances[i].Key, key)
		}
	}
}

func TestParseListIsEmptyForAnEmptyParam(t *testing.T) {
	instances, err := ParseList("   ")
	if err != nil {
		t.Fatalf("ParseList: %v", err)
	}
	if len(instances) != 0 {
		t.Errorf("parsed %d instances from an empty parameter", len(instances))
	}
}

func TestParseListCapsHowManyIndicatorsOneRequestCanAskFor(t *testing.T) {
	periods := []string{"5", "9", "10", "12", "20", "21", "50", "100", "200"}
	entries := make([]string, 0, len(periods))
	for _, period := range periods {
		entries = append(entries, "ema:"+period)
	}

	if _, err := ParseList(strings.Join(entries, ",")); err == nil {
		t.Fatalf("%d indicators parsed, want a cap at %d", len(entries), MaxInstances)
	}

	if _, err := ParseList(strings.Join(entries[:MaxInstances], ",")); err != nil {
		t.Errorf("%d indicators is within the cap but failed: %v", MaxInstances, err)
	}
}

func TestPrimeBarsClearsEveryRequestedWarmup(t *testing.T) {
	instances, err := ParseList("ema:9,macd:12:26:9,sma:5,rsi:14")
	if err != nil {
		t.Fatalf("ParseList: %v", err)
	}

	bars := PrimeBars(instances)
	for _, instance := range instances {
		if bars < instance.Indicator.Warmup() {
			t.Errorf("priming reads %d bars, short of %s's %d-bar warmup",
				bars, instance.Key, instance.Indicator.Warmup())
		}
	}
	if bars > MaxPrimeBars {
		t.Errorf("PrimeBars is %d, above the %d cap", bars, MaxPrimeBars)
	}
	if got := PrimeBars(nil); got != 0 {
		t.Errorf("PrimeBars(nil) is %d, want 0", got)
	}
}

func TestOnlyRecursiveIndicatorsAskForMoreThanTheirWarmup(t *testing.T) {
	cases := map[string]bool{
		"sma:200":      false,
		"bb:20:2":      false,
		"ema:9":        true,
		"rsi:14":       true,
		"macd:12:26:9": true,
	}

	for key, recursive := range cases {
		instance, err := Parse(key)
		if err != nil {
			t.Fatalf("Parse(%q): %v", key, err)
		}

		depth := PrimeBars([]Instance{instance})
		warmup := instance.Indicator.Warmup()

		switch {
		case depth < warmup:
			t.Errorf("%s primes with %d bars, short of its %d-bar warmup", key, depth, warmup)
		case recursive && depth <= warmup:
			t.Errorf("%s carries state past its warmup but primes with only %d bars", key, depth)
		case !recursive && depth > warmup:
			t.Errorf("%s has a finite window but primes with %d bars for a %d-bar warmup", key, depth, warmup)
		}
	}
}

func TestEverySpecInTheCatalogueIsUsable(t *testing.T) {
	specs := Catalog()
	if len(specs) == 0 {
		t.Fatal("nothing is registered")
	}

	for _, spec := range specs {
		instance, err := New(spec.Name, nil, "")
		if err != nil {
			t.Errorf("New(%s) with defaults: %v", spec.Name, err)
			continue
		}
		if got := len(instance.Indicator.Values()); got != len(spec.Outputs) {
			t.Errorf("%s returns %d values for %d declared outputs", spec.Name, got, len(spec.Outputs))
		}
		if spec.Title == "" {
			t.Errorf("%s has no title for the picker to show", spec.Name)
		}
		if spec.Group == "" {
			t.Errorf("%s belongs to no group", spec.Name)
		}
		for _, param := range spec.Params {
			if _, ok := instance.Params.All()[param.Name]; !ok {
				t.Errorf("%s did not resolve its %s parameter", spec.Name, param.Name)
			}
		}
	}
}

func TestSourceSelectorReadsEveryPriceField(t *testing.T) {
	candle := Candle{Open: 10, High: 20, Low: 6, Close: 12}

	cases := map[Source]float64{
		SourceOpen:  10,
		SourceHigh:  20,
		SourceLow:   6,
		SourceClose: 12,
		SourceHL2:   13,
		SourceHLC3:  (20 + 6 + 12) / 3.0,
		SourceOHLC4: 12,
	}

	for source, want := range cases {
		if got := source.Value(candle); got != want {
			t.Errorf("%s reads %v, want %v", source, got, want)
		}
	}

	for _, source := range Sources {
		if _, ok := cases[source]; !ok {
			t.Errorf("%s is offered but untested", source)
		}
	}
}
