package api

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mhetem/ALVO-Backtester/internal/indicator"
)

func layoutOf(entries ...layoutEntryRequest) layoutRequest {
	return layoutRequest{Name: "a layout", Version: layoutVersion, Indicators: entries}
}

func hidden() *bool {
	value := false
	return &value
}

func TestALayoutRejectsWhatItCannotReplay(t *testing.T) {
	tooMany := make([]layoutEntryRequest, 0, indicator.MaxInstances+1)
	for i := range indicator.MaxInstances + 1 {
		tooMany = append(tooMany, layoutEntryRequest{Key: "sma:" + strconv.Itoa(2+i)})
	}

	cases := []struct {
		name string
		req  layoutRequest
		want string
	}{
		{
			"unknown indicator",
			layoutOf(layoutEntryRequest{Key: "nope:9"}),
			`unknown indicator "nope"`,
		},
		{
			"out of range parameter",
			layoutOf(layoutEntryRequest{Key: "ema:0"}),
			"period must be between",
		},
		{
			"unknown source",
			layoutOf(layoutEntryRequest{Key: "ema:9:source=vwap"}),
			`unknown source "vwap"`,
		},
		{
			"the same indicator twice",
			layoutOf(layoutEntryRequest{Key: "ema:9"}, layoutEntryRequest{Key: "EMA : 9 "}),
			"appears twice",
		},
		{
			"more indicators than a request can carry",
			layoutRequest{Version: layoutVersion, Indicators: tooMany},
			"at most",
		},
		{
			"a future layout version",
			layoutRequest{Name: "a layout", Version: 2},
			"layout version must be 1",
		},
		{
			"more colours than outputs",
			layoutOf(layoutEntryRequest{Key: "ema:9", Colors: []int{0, 1}}),
			"is too many",
		},
		{
			"a colour outside the palette",
			layoutOf(layoutEntryRequest{Key: "ema:9", Colors: []int{PaletteSlots}}),
			"palette slot",
		},
		{
			"a negative colour",
			layoutOf(layoutEntryRequest{Key: "ema:9", Colors: []int{-1}}),
			"palette slot",
		},
		{
			"an unknown line style",
			layoutOf(layoutEntryRequest{Key: "ema:9", Style: "squiggly"}),
			"line style must be one of",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeLayout(tc.req)
			if err == nil {
				t.Fatalf("normalizeLayout accepted %+v", tc.req)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error is %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestALayoutNeedsAUsableName(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"", "needs a name"},
		{"   ", "needs a name"},
		{strings.Repeat("x", maxLayoutName+1), "at most"},
	}

	for _, tc := range cases {
		name, want := tc.name, tc.want
		got, err := normalizeLayoutName(name)
		if err == nil {
			t.Fatalf("normalizeLayoutName(%q) returned %q", name, got)
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error for %q is %q, want it to contain %q", name, err, want)
		}
	}

	if got, err := normalizeLayoutName("  Ichimoku daily  "); err != nil || got != "Ichimoku daily" {
		t.Errorf("normalizeLayoutName trimmed to %q (err %v), want %q", got, err, "Ichimoku daily")
	}
}

func TestALayoutIsStoredCanonically(t *testing.T) {
	entries, err := normalizeLayout(layoutOf(
		layoutEntryRequest{Key: "EMA : period=9 "},
		layoutEntryRequest{Key: "macd", Visible: hidden(), Colors: []int{3}},
		layoutEntryRequest{Key: "rsi:14:source=close", Style: "dotted"},
	))
	if err != nil {
		t.Fatalf("normalizeLayout: %v", err)
	}

	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, entry.Key)
	}
	want := []string{"ema:9", "macd:12:26:9", "rsi:14"}
	if !slices.Equal(keys, want) {
		t.Fatalf("keys are %v, want %v", keys, want)
	}

	if !entries[0].Visible {
		t.Error("an entry with no visible field came back hidden, want visible")
	}
	if entries[1].Visible {
		t.Error("an entry sent as hidden came back visible")
	}

	if got := entries[1].Colors; !slices.Equal(got, []int{3, 1, 2}) {
		t.Errorf("macd colours are %v, want the given slot then the defaults for its other outputs", got)
	}
	if got := len(entries[2].Colors); got != 1 {
		t.Errorf("rsi carries %d colours, want one per output", got)
	}

	if entries[0].Style != styleSolid {
		t.Errorf("an entry with no style came back %q, want %q", entries[0].Style, styleSolid)
	}
	if entries[2].Style != styleDotted {
		t.Errorf("an entry sent as dotted came back %q", entries[2].Style)
	}
}
