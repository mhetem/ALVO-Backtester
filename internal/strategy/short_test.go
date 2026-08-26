package strategy

import (
	"encoding/json"
	"strings"
	"testing"
)

const bothSides = `{
  "version": 1,
  "inputs": {
    "fast": {"indicator": "ema", "params": {"period": 9}},
    "slow": {"indicator": "ema", "params": {"period": 21}}
  },
  "entry": {
    "long":  {"crosses_above": ["fast", "slow"]},
    "short": {"crosses_below": ["fast", "slow"]}
  },
  "exit": {
    "long":  {"any": [{"crosses_below": ["fast", "slow"]}, {"stop_loss": {"type": "pct", "value": 0.02}}]},
    "short": {"any": [{"crosses_above": ["fast", "slow"]}, {"stop_loss": {"type": "pct", "value": 0.03}}]}
  },
  "sizing": {"type": "fixed_qty", "value": 100},
  "costs":  {"brokerage_cents": 0, "fee_bps": 0, "slippage_bps": 0}
}`

func TestBothSidesCompileToTheirOwnLeg(t *testing.T) {
	plan := mustCompile(t, bothSides)

	if !plan.Long.Trades() || !plan.Short.Trades() {
		t.Fatalf("long trades = %v, short trades = %v, want both", plan.Long.Trades(), plan.Short.Trades())
	}
	if plan.Long.Exit == nil || plan.Short.Exit == nil {
		t.Error("each side keeps the crossing left behind after its bracket is hoisted")
	}
	if plan.Long.Stop == nil || plan.Short.Stop == nil {
		t.Fatal("each side carries its own stop")
	}

	// The two stops are different sizes, so sharing one Bracket would silently give one
	// side the other's risk.
	if plan.Long.Stop.Level.Value == plan.Short.Stop.Level.Value {
		t.Errorf("both stops read %g, want 0.02 long and 0.03 short", plan.Long.Stop.Level.Value)
	}
	if plan.Long.Target != nil || plan.Short.Target != nil {
		t.Error("neither side declared a take_profit")
	}
}

func TestOneSidedSpecsLeaveTheOtherLegEmpty(t *testing.T) {
	long := mustCompile(t, parts{}.json())
	if !long.Long.Trades() || long.Short.Trades() {
		t.Errorf("long-only spec: long = %v, short = %v", long.Long.Trades(), long.Short.Trades())
	}

	short := mustCompile(t, parts{
		entry: `{"short": {"crosses_below": ["fast", "slow"]}}`,
		exit:  `{"short": {"crosses_above": ["fast", "slow"]}}`,
	}.json())
	if short.Long.Trades() || !short.Short.Trades() {
		t.Errorf("short-only spec: long = %v, short = %v", short.Long.Trades(), short.Short.Trades())
	}
}

func TestEachSideCarriesItsOwnBracketBudget(t *testing.T) {
	// One stop per side is fine; two on the same side is still the old error.
	if _, err := Parse([]byte(bothSides)); err != nil {
		t.Fatalf("a stop on each side was rejected: %v", err)
	}

	fault := faultOf(t, parts{
		entry: `{"long": {"gt": ["fast", "slow"]}, "short": {"lt": ["fast", "slow"]}}`,
		exit: `{"long": {"any": [{"stop_loss": {"type": "pct", "value": 0.02}}, {"stop_loss": {"type": "pct", "value": 0.03}}]},
		        "short": {"lt": ["fast", "slow"]}}`,
	}.json())
	if !strings.Contains(fault.Error(), "one stop_loss") {
		t.Errorf("message = %q, want it to reject two stops on one side", fault.Error())
	}
}

func TestRiskSizingNeedsAStopOnEverySideThatTrades(t *testing.T) {
	fault := faultOf(t, parts{
		entry:  `{"long": {"gt": ["fast", "slow"]}, "short": {"lt": ["fast", "slow"]}}`,
		exit:   `{"long": {"stop_loss": {"type": "pct", "value": 0.02}}, "short": {"lt": ["fast", "slow"]}}`,
		sizing: `{"type": "risk_pct", "value": 0.01}`,
	}.json())

	if fault.Pointer != "/sizing" {
		t.Errorf("pointer = %q, want /sizing", fault.Pointer)
	}
	if !strings.Contains(fault.Error(), "short exit needs a stop_loss") {
		t.Errorf("message = %q, want it to name the side that is missing its stop", fault.Error())
	}
}

func TestBothSidesSurviveACanonicalRoundTrip(t *testing.T) {
	spec := mustParse(t, bothSides)

	encoded, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("encoding the spec: %v", err)
	}
	for _, want := range []string{`"long"`, `"short"`} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("canonical spec %s dropped %s", encoded, want)
		}
	}

	again, err := Parse(encoded)
	if err != nil {
		t.Fatalf("reparsing the canonical spec: %v", err)
	}
	if again.Entry.Short == nil || again.Exit == nil || again.Exit.Short == nil {
		t.Error("the short side did not survive the round trip")
	}
}

const cloudSpec = `{
  "version": 1,
  "inputs": {"cloud": {"indicator": "ichimoku"}},
  "entry": {"long": {"all": [
    {"crosses_above": ["close", "cloud.senkou_a"]},
    {"gt": ["cloud.tenkan", "cloud.kijun"]}
  ]}},
  "exit": {"long": {"lt": ["cloud.tenkan", "cloud.kijun"]}},
  "sizing": {"type": "fixed_qty", "value": 100},
  "costs":  {"brokerage_cents": 0, "fee_bps": 0, "slippage_bps": 0}
}`

func TestOneInputReachesEveryLineItEmits(t *testing.T) {
	plan := mustCompile(t, cloudSpec)

	// One declaration and one computation, feeding a slot per line the rules actually
	// read. tenkan is ichimoku's first output, so it is the slot the input already
	// declared and naming it explicitly costs nothing extra: three slots, not four.
	if len(plan.Units) != 1 {
		t.Fatalf("units = %d, want the single ichimoku all the lines come from", len(plan.Units))
	}
	if len(plan.Slots) != 3 {
		t.Errorf("slots = %d, want one per distinct line read (%v)", len(plan.Slots), plan.Slots)
	}
	if len(plan.Units[0].Slots) != 3 {
		t.Errorf("the cloud feeds %d slots, want 3", len(plan.Units[0].Slots))
	}

	for _, name := range []string{"cloud.kijun", "cloud.senkou_a"} {
		if !slotNamed(plan, name) {
			t.Errorf("no slot reads %s", name)
		}
	}
	if slotNamed(plan, "cloud.tenkan") {
		t.Error("tenkan is the declared output, so it should reuse the input's own slot")
	}
	if slotNamed(plan, "cloud.chikou") {
		t.Error("chikou is never read, so it should cost no slot")
	}
}

func TestAnUnreadLineCostsNothing(t *testing.T) {
	plain := mustCompile(t, parts{
		inputs: `{"cloud": {"indicator": "ichimoku"}}`,
		entry:  `{"long": {"gt": ["close", "cloud"]}}`,
		exit:   "null",
	}.json())

	if len(plain.Slots) != 1 {
		t.Errorf("slots = %d, want just the input's declared output", len(plain.Slots))
	}
}

func TestALineTheIndicatorDoesNotEmitIsRejected(t *testing.T) {
	fault := faultOf(t, parts{
		inputs: `{"cloud": {"indicator": "ichimoku"}}`,
		entry:  `{"long": {"gt": ["close", "cloud.kumo"]}}`,
		exit:   "null",
	}.json())

	if !strings.Contains(fault.Error(), `has no output "kumo"`) {
		t.Errorf("message = %q, want it to name the bad line", fault.Error())
	}
}

func TestADottedNameOnAnUnknownInputIsRejected(t *testing.T) {
	fault := faultOf(t, parts{
		entry: `{"long": {"gt": ["close", "cloud.tenkan"]}}`,
		exit:  "null",
	}.json())

	if !strings.Contains(fault.Error(), "names no input") {
		t.Errorf("message = %q, want it to say the input does not exist", fault.Error())
	}
}

func TestADottedNameOnASingleOutputIndicatorCollapses(t *testing.T) {
	// "fast.ema" and "fast" are the same series, so they must land in the same slot and
	// the canonical spec must print the shorter of the two.
	spec := mustParse(t, parts{
		entry: `{"long": {"gt": ["fast.ema", "slow"]}}`,
		exit:  "null",
	}.json())

	encoded, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("encoding the spec: %v", err)
	}
	if strings.Contains(string(encoded), "fast.ema") {
		t.Errorf("canonical spec %s kept a redundant line name", encoded)
	}
}

func TestDottedLinesSurviveACanonicalRoundTrip(t *testing.T) {
	spec := mustParse(t, cloudSpec)

	encoded, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("encoding the spec: %v", err)
	}
	for _, want := range []string{"cloud.senkou_a", "cloud.tenkan", "cloud.kijun"} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("canonical spec %s dropped %s", encoded, want)
		}
	}

	if _, err := Parse(encoded); err != nil {
		t.Fatalf("reparsing the canonical spec: %v", err)
	}
}

func slotNamed(plan *Plan, name string) bool {
	for _, slot := range plan.Slots {
		if slot.Name == name {
			return true
		}
	}
	return false
}
