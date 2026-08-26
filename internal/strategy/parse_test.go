package strategy

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const (
	crossInputs = `{"fast": {"indicator": "ema", "params": {"period": 9}}, "slow": {"indicator": "ema", "params": {"period": 21}}}`
	crossEntry  = `{"long": {"crosses_above": ["fast", "slow"]}}`
	crossExit   = `{"long": {"crosses_below": ["fast", "slow"]}}`
	pctSizing   = `{"type": "pct_equity", "value": 0.95}`
	pctStop     = `{"type": "pct", "value": 0.02}`
)

const planExample = `{
  "version": 1,
  "inputs": {
    "fast": {"indicator": "ema", "params": {"period": 9},  "source": "close"},
    "slow": {"indicator": "ema", "params": {"period": 21}, "source": "close"},
    "rsi":  {"indicator": "rsi", "params": {"period": 14}}
  },
  "entry": {
    "long": {"all": [
      {"crosses_above": ["fast", "slow"]},
      {"lt": ["rsi", 70]}
    ]}
  },
  "exit": {
    "long": {"any": [
      {"crosses_below": ["fast", "slow"]},
      {"stop_loss":   {"type": "atr", "period": 14, "mult": 2.0}},
      {"take_profit": {"type": "pct", "value": 0.05}}
    ]}
  },
  "sizing": {"type": "pct_equity", "value": 0.95},
  "costs":  {"brokerage_cents": 0, "fee_bps": 3.25, "slippage_bps": 5}
}`

type parts struct {
	version string
	inputs  string
	entry   string
	exit    string
	sizing  string
	costs   string
}

func (p parts) json() string {
	fill := func(value, fallback string) string {
		if value == "" {
			return fallback
		}
		return value
	}

	body := `{"version": ` + fill(p.version, "1") +
		`, "inputs": ` + fill(p.inputs, crossInputs) +
		`, "entry": ` + fill(p.entry, crossEntry) +
		`, "exit": ` + fill(p.exit, crossExit) +
		`, "sizing": ` + fill(p.sizing, pctSizing)

	if p.costs != "" {
		body += `, "costs": ` + p.costs
	}

	return body + `}`
}

func faultOf(t *testing.T, body string) *Fault {
	t.Helper()

	_, err := Parse([]byte(body))
	if err == nil {
		t.Fatalf("expected a fault from %s", body)
	}

	var fault *Fault
	if !errors.As(err, &fault) {
		t.Fatalf("expected a *Fault, got %T: %v", err, err)
	}

	return fault
}

func mustParse(t *testing.T, body string) Spec {
	t.Helper()

	spec, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	return spec
}

func TestASpecPointsAtWhatItCannotAccept(t *testing.T) {
	cases := []struct {
		name    string
		body    parts
		pointer string
		want    string
	}{
		{
			"an unknown indicator",
			parts{inputs: `{"fast": {"indicator": "nope"}}`, entry: `{"long": {"gt": ["fast", 1]}}`, exit: "null"},
			"/inputs/fast/indicator",
			`unknown indicator "nope"`,
		},
		{
			"a parameter outside its range",
			parts{inputs: `{"fast": {"indicator": "ema", "params": {"period": 0}}}`, entry: `{"long": {"gt": ["fast", 1]}}`, exit: "null"},
			"/inputs/fast/params/period",
			"must be between",
		},
		{
			"a parameter the indicator does not declare",
			parts{inputs: `{"fast": {"indicator": "ema", "params": {"length": 9}}}`, entry: `{"long": {"gt": ["fast", 1]}}`, exit: "null"},
			"/inputs/fast/params/length",
			"has no parameter",
		},
		{
			"a fractional period",
			parts{inputs: `{"fast": {"indicator": "ema", "params": {"period": 9.5}}}`, entry: `{"long": {"gt": ["fast", 1]}}`, exit: "null"},
			"/inputs/fast/params/period",
			"whole number",
		},
		{
			"a source that names nothing",
			parts{inputs: `{"fast": {"indicator": "ema", "source": "vwap"}}`, entry: `{"long": {"gt": ["fast", 1]}}`, exit: "null"},
			"/inputs/fast/source",
			"names neither a price field",
		},
		{
			"a source on an indicator that reads whole candles",
			parts{inputs: `{"fast": {"indicator": "atr", "source": "close"}}`, entry: `{"long": {"gt": ["fast", 1]}}`, exit: "null"},
			"/inputs/fast/source",
			"takes no source",
		},
		{
			"volume as an indicator source",
			parts{inputs: `{"fast": {"indicator": "sma", "source": "volume"}}`, entry: `{"long": {"gt": ["fast", 1]}}`, exit: "null"},
			"/inputs/fast/source",
			"volumema",
		},
		{
			"an output the indicator does not emit",
			parts{inputs: `{"fast": {"indicator": "macd", "output": "trigger"}}`, entry: `{"long": {"gt": ["fast", 1]}}`, exit: "null"},
			"/inputs/fast/output",
			"has no output",
		},
		{
			"an input reading from itself",
			parts{inputs: `{"fast": {"indicator": "ema", "source": "fast"}}`, entry: `{"long": {"gt": ["fast", 1]}}`, exit: "null"},
			"/inputs/fast/source",
			"reads from itself",
		},
		{
			"inputs feeding each other in a loop",
			parts{
				inputs: `{"a": {"indicator": "ema", "source": "b"}, "b": {"indicator": "ema", "source": "a"}}`,
				entry:  `{"long": {"gt": ["a", "b"]}}`, exit: "null",
			},
			"/inputs/a/source",
			"loop",
		},
		{
			"a price field naming an input",
			parts{inputs: `{"close": {"indicator": "ema"}}`, entry: `{"long": {"gt": ["close", 1]}}`, exit: "null"},
			"/inputs/close",
			"price field",
		},
		{
			"an input name that is not an identifier",
			parts{inputs: `{"Fast": {"indicator": "ema"}}`, entry: `{"long": {"gt": ["close", 1]}}`, exit: "null"},
			"/inputs/Fast",
			"not a usable input name",
		},
		{
			"a field the schema has no room for",
			parts{inputs: `{"fast": {"indicator": "ema", "colour": "red"}}`, entry: `{"long": {"gt": ["fast", 1]}}`, exit: "null"},
			"/inputs/fast/colour",
			`no such field "colour"`,
		},
		{
			"an operand that resolves to nothing",
			parts{entry: `{"long": {"gt": ["fastt", 1]}}`},
			"/entry/long/gt/0",
			"names neither an input",
		},
		{
			"an unknown comparator",
			parts{entry: `{"long": {"above": ["fast", "slow"]}}`},
			"/entry/long/above",
			"no such rule",
		},
		{
			"a comparator given the wrong number of operands",
			parts{entry: `{"long": {"gt": ["fast"]}}`},
			"/entry/long/gt",
			"2 operands",
		},
		{
			"an empty all",
			parts{entry: `{"long": {"all": []}}`},
			"/entry/long/all",
			"matches every bar",
		},
		{
			"an empty any",
			parts{entry: `{"long": {"any": []}}`},
			"/entry/long/any",
			"never matches",
		},
		{
			"a rule carrying two operators",
			parts{entry: `{"long": {"gt": ["fast", "slow"], "lt": ["fast", "slow"]}}`},
			"/entry/long",
			"exactly one operator",
		},
		{
			"a stop loss on the entry side",
			parts{entry: `{"long": {"stop_loss": ` + pctStop + `}}`},
			"/entry/long/stop_loss",
			"resting order",
		},
		{
			"a stop loss buried inside an all",
			parts{exit: `{"long": {"all": [{"stop_loss": ` + pctStop + `}]}}`},
			"/exit/long/all/0/stop_loss",
			"resting order",
		},
		{
			"a stop loss under a not",
			parts{exit: `{"long": {"not": {"stop_loss": ` + pctStop + `}}}`},
			"/exit/long/not/stop_loss",
			"resting order",
		},
		{
			"two stop losses on one position",
			parts{exit: `{"long": {"any": [{"stop_loss": ` + pctStop + `}, {"stop_loss": {"type": "pct", "value": 0.03}}]}}`},
			"/exit/long/any/1/stop_loss",
			"one stop_loss",
		},
		{
			"a percentage stop that also carries a period",
			parts{exit: `{"long": {"stop_loss": {"type": "pct", "value": 0.02, "period": 14}}}`},
			"/exit/long/stop_loss/period",
			"takes no period",
		},
		{
			"a stop percentage outside its range",
			parts{exit: `{"long": {"stop_loss": {"type": "pct", "value": 1.5}}}`},
			"/exit/long/stop_loss/value",
			"above 0 and below 1",
		},
		{
			"an atr stop with no multiplier",
			parts{exit: `{"long": {"stop_loss": {"type": "atr", "period": 14}}}`},
			"/exit/long/stop_loss/mult",
			"needs a mult",
		},
		{
			"a ref that looks further back than the tape holds",
			parts{entry: `{"long": {"gt": [{"ref": ["fast", 100000]}, "slow"]}}`},
			"/entry/long/gt/0/ref/1",
			"looks back between 0 and",
		},
		{
			"a ref onto a number",
			parts{entry: `{"long": {"gt": [{"ref": [3, 1]}, "slow"]}}`},
			"/entry/long/gt/0/ref/0",
			"first entry is a name",
		},
		{
			"an unknown sizing type",
			parts{sizing: `{"type": "martingale", "value": 1}`},
			"/sizing/type",
			"no such sizing type",
		},
		{
			"a fraction of equity above one",
			parts{sizing: `{"type": "pct_equity", "value": 1.5}`},
			"/sizing/value",
			"at most 1",
		},
		{
			"a fractional share count",
			parts{sizing: `{"type": "fixed_qty", "value": 10.5}`},
			"/sizing/value",
			"whole number of shares",
		},
		{
			"risk sizing with no stop to measure against",
			parts{sizing: `{"type": "risk_pct", "value": 0.01}`},
			"/sizing",
			"needs a stop_loss",
		},
		{
			"costs beyond anything a broker charges",
			parts{costs: `{"fee_bps": 5000}`},
			"/costs/fee_bps",
			"basis points",
		},
		{
			"a spec version from the future",
			parts{version: "2"},
			"/version",
			"spec version must be 1",
		},
		{
			"a span that runs backwards",
			parts{entry: `{"long": {"between": ["fast", 70, 30]}}`},
			"/entry/long/between/2",
			"cannot be the high",
		},
		{
			"rising counted by something that is not a number",
			parts{entry: `{"long": {"rising": ["fast", "slow"]}}`},
			"/entry/long/rising/1",
			"whole number between 1",
		},
		{
			"a side that is neither long nor short",
			parts{entry: `{"sideways": {"gt": ["fast", "slow"]}}`},
			"/entry/sideways",
			`no such field "sideways"`,
		},
		{
			"an entry with no side at all",
			parts{entry: `{}`},
			"/entry",
			"needs a long rule, a short rule, or both",
		},
		{
			"a short exit with no short entry",
			parts{
				entry: `{"long": {"gt": ["fast", "slow"]}}`,
				exit:  `{"short": {"lt": ["fast", "slow"]}}`,
			},
			"/exit/short",
			"there is no short entry",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fault := faultOf(t, test.body.json())

			if fault.Pointer != test.pointer {
				t.Errorf("pointer = %q, want %q (message: %s)", fault.Pointer, test.pointer, fault.Message)
			}
			if !strings.Contains(fault.Message, test.want) {
				t.Errorf("message = %q, want it to mention %q", fault.Message, test.want)
			}
		})
	}
}

func TestASpecNeedsTheThingsARunCannotInvent(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		pointer string
		want    string
	}{
		{"no entry", `{"version": 1, "inputs": ` + crossInputs + `, "sizing": ` + pctSizing + `}`, "/entry", "needs an entry rule"},
		{"no sizing", `{"version": 1, "inputs": ` + crossInputs + `, "entry": ` + crossEntry + `}`, "/sizing", "needs a sizing rule"},
		{"no inputs", `{"version": 1, "entry": ` + crossEntry + `, "sizing": ` + pctSizing + `}`, "/inputs", "at least one input"},
		{"an empty inputs object", `{"version": 1, "inputs": {}, "entry": ` + crossEntry + `, "sizing": ` + pctSizing + `}`, "/inputs", "at least one input"},
		{"a spec that is not an object", `[]`, "", "JSON object"},
		{"a spec that is not JSON", `{`, "", "JSON object"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fault := faultOf(t, test.body)

			if fault.Pointer != test.pointer {
				t.Errorf("pointer = %q, want %q (message: %s)", fault.Pointer, test.pointer, fault.Message)
			}
			if !strings.Contains(fault.Message, test.want) {
				t.Errorf("message = %q, want it to mention %q", fault.Message, test.want)
			}
		})
	}
}

func TestTheSpecFromThePlanRoundTrips(t *testing.T) {
	spec := mustParse(t, planExample)

	first, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}

	second, err := json.Marshal(mustParse(t, string(first)))
	if err != nil {
		t.Fatalf("re-encoding: %v", err)
	}

	if string(first) != string(second) {
		t.Errorf("a canonical spec did not survive a second pass\nfirst:  %s\nsecond: %s", first, second)
	}
}

func TestACanonicalSpecPinsEveryParameterItRunsOn(t *testing.T) {
	spec := mustParse(t, parts{
		inputs: `{"rsi": {"indicator": "rsi"}, "band": {"indicator": "bb", "output": "upper"}}`,
		entry:  `{"long": {"lt": ["rsi", 30]}}`,
		exit:   `{"long": {"gt": ["close", "band"]}}`,
	}.json())

	rsi := spec.Inputs["rsi"]
	if got := rsi.Params["period"]; got != 14 {
		t.Errorf("rsi period = %v, want the registry default 14 written down", got)
	}
	if rsi.Source != "close" {
		t.Errorf("rsi source = %q, want close spelled out", rsi.Source)
	}

	encoded, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}

	for _, want := range []string{`"period":14`, `"source":"close"`, `"output":"upper"`, `"fee_bps":3.25`} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("canonical spec %s is missing %s", encoded, want)
		}
	}

	if strings.Contains(string(encoded), `"output":"rsi"`) {
		t.Errorf("canonical spec %s names an output for a single-output indicator", encoded)
	}
}

func TestACanonicalSpecSpellsOutWhatWasImplied(t *testing.T) {
	spec := mustParse(t, parts{
		entry: `{"long": {"all": [{"rising": ["fast"]}, {"gt": [{"ref": ["fast", 0]}, "slow"]}]}}`,
		exit:  "null",
	}.json())

	encoded, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}

	if !strings.Contains(string(encoded), `"rising":["fast",1]`) {
		t.Errorf("canonical spec %s did not write down how many bars rising counts", encoded)
	}
	if strings.Contains(string(encoded), `"ref"`) {
		t.Errorf("canonical spec %s kept a ref that looks back no bars", encoded)
	}
	if strings.Contains(string(encoded), `"exit"`) {
		t.Errorf("canonical spec %s invented an exit the author left out", encoded)
	}
}

func TestCostsDefaultToWhatB3Charges(t *testing.T) {
	spec := mustParse(t, parts{}.json())

	if spec.Costs != DefaultCosts() {
		t.Errorf("costs = %+v, want %+v", spec.Costs, DefaultCosts())
	}

	partial := mustParse(t, parts{costs: `{"fee_bps": 0}`}.json())
	if partial.Costs.FeeBPS != 0 {
		t.Errorf("fee_bps = %v, want the zero the author asked for", partial.Costs.FeeBPS)
	}
	if partial.Costs.SlippageBPS != DefaultSlippageBPS {
		t.Errorf("slippage_bps = %v, want the default %v", partial.Costs.SlippageBPS, DefaultSlippageBPS)
	}
}

func TestASpecStaysWithinItsBounds(t *testing.T) {
	inputs := make([]string, 0, MaxInputs+1)
	for i := range MaxInputs + 1 {
		inputs = append(inputs, `"i`+string(rune('a'+i))+`": {"indicator": "ema", "params": {"period": `+string(rune('2'+i%8))+`}}`)
	}

	fault := faultOf(t, parts{
		inputs: "{" + strings.Join(inputs, ", ") + "}",
		entry:  `{"long": {"gt": ["ia", "ib"]}}`,
		exit:   "null",
	}.json())

	if !strings.Contains(fault.Message, "at most") {
		t.Errorf("message = %q, want it to cap the input count", fault.Message)
	}

	nested := `{"gt": ["fast", "slow"]}`
	for range MaxDepth + 1 {
		nested = `{"not": ` + nested + `}`
	}

	deep := faultOf(t, parts{entry: `{"long": ` + nested + `}`}.json())
	if !strings.Contains(deep.Message, "nest at most") {
		t.Errorf("message = %q, want it to cap the nesting depth", deep.Message)
	}
}
