package strategy

import (
	"testing"

	"github.com/mhetem/ALVO-Backtester/internal/indicator"
)

func mustCompile(t *testing.T, body string) *Plan {
	t.Helper()

	plan, err := Compile(mustParse(t, body))
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}

	return plan
}

func warmupOf(t *testing.T, name string, params map[string]float64) int {
	t.Helper()

	instance, err := indicator.New(name, params, "")
	if err != nil {
		t.Fatalf("building %s: %v", name, err)
	}

	return instance.Indicator.Warmup()
}

func TestTwoInputsOnTheSameIndicatorShareOneInstance(t *testing.T) {
	plan := mustCompile(t, parts{
		inputs: `{"a": {"indicator": "ema", "params": {"period": 9}}, "b": {"indicator": "ema", "params": {"period": 9}}}`,
		entry:  `{"long": {"gt": ["a", "b"]}}`,
		exit:   "null",
	}.json())

	if len(plan.Units) != 1 {
		t.Errorf("units = %d, want the one ema both inputs name", len(plan.Units))
	}
	if len(plan.Slots) != 1 {
		t.Errorf("slots = %d, want one", len(plan.Slots))
	}
	if plan.Index["a"] != plan.Index["b"] {
		t.Errorf("a is slot %d and b is slot %d, want the same slot", plan.Index["a"], plan.Index["b"])
	}
}

func TestOneIndicatorCanBackSeveralSlots(t *testing.T) {
	plan := mustCompile(t, parts{
		inputs: `{"up": {"indicator": "bb", "output": "upper"}, "down": {"indicator": "bb", "output": "lower"}}`,
		entry:  `{"long": {"gt": ["close", "up"]}}`,
		exit:   `{"long": {"lt": ["close", "down"]}}`,
	}.json())

	if len(plan.Units) != 1 {
		t.Fatalf("units = %d, want the one band both inputs read", len(plan.Units))
	}
	if len(plan.Slots) != 2 {
		t.Errorf("slots = %d, want one per output read", len(plan.Slots))
	}
	if len(plan.Units[0].Slots) != 2 {
		t.Errorf("the band feeds %d slots, want 2", len(plan.Units[0].Slots))
	}
	if plan.Index["up"] == plan.Index["down"] {
		t.Error("upper and lower landed in the same slot")
	}
}

func TestBracketsLeaveTheConditionTree(t *testing.T) {
	plan := mustCompile(t, planExample)

	if plan.Exit == nil {
		t.Fatal("the exit lost the crossing it was written with")
	}
	if plan.Stop == nil || plan.Target == nil {
		t.Fatalf("stop = %v, target = %v, want both hoisted out", plan.Stop, plan.Target)
	}
	if plan.Stop.Slot < 0 {
		t.Error("an atr stop compiled without a slot to read its range from")
	}
	if plan.Target.Slot != -1 {
		t.Errorf("a percentage target claimed slot %d, want none", plan.Target.Slot)
	}
	if plan.Exit.depth() != 1 {
		t.Errorf("exit depth = %d, want the one bar a crossing looks back", plan.Exit.depth())
	}
}

func TestAnExitOfNothingButBracketsHasNoRule(t *testing.T) {
	both := mustCompile(t, parts{
		exit: `{"long": {"any": [{"stop_loss": ` + pctStop + `}, {"take_profit": {"type": "pct", "value": 0.05}}]}}`,
	}.json())

	if both.Exit != nil {
		t.Error("an exit made only of brackets left a condition behind")
	}
	if both.Stop == nil || both.Target == nil {
		t.Error("the brackets did not survive the hoist")
	}

	alone := mustCompile(t, parts{exit: `{"long": {"stop_loss": ` + pctStop + `}}`}.json())
	if alone.Exit != nil || alone.Stop == nil {
		t.Errorf("exit rule = %v, stop = %v, want only a stop", alone.Exit, alone.Stop)
	}
}

func TestABracketSharesTheAverageRangeAnInputAlreadyBuilt(t *testing.T) {
	plan := mustCompile(t, parts{
		inputs: `{"vol": {"indicator": "atr", "params": {"period": 14}}}`,
		entry:  `{"long": {"gt": ["vol", 0]}}`,
		exit:   `{"long": {"stop_loss": {"type": "atr", "period": 14, "mult": 2}}}`,
	}.json())

	built := 0
	for _, unit := range plan.Units {
		if unit.Instance.Key == "atr:14" {
			built++
		}
	}
	if built != 1 {
		t.Errorf("atr:14 was instantiated %d times, want once", built)
	}
	if plan.Stop.Slot != plan.Index["vol"] {
		t.Errorf("the stop reads slot %d and the input reads slot %d, want the same", plan.Stop.Slot, plan.Index["vol"])
	}
}

func TestAChainedInputWaitsForWhatFeedsIt(t *testing.T) {
	plan := mustCompile(t, parts{
		inputs: `{"rsi": {"indicator": "rsi", "params": {"period": 14}}, "smooth": {"indicator": "sma", "params": {"period": 3}, "source": "rsi"}}`,
		entry:  `{"long": {"gt": ["smooth", 50]}}`,
		exit:   "null",
	}.json())

	if len(plan.Units) != 2 {
		t.Fatalf("units = %d, want the rsi and the average over it", len(plan.Units))
	}

	upstream, downstream := plan.Units[0], plan.Units[1]
	if upstream.Instance.Spec.Name != "rsi" {
		t.Fatalf("the first unit is %s, want the rsi that feeds the other", upstream.Instance.Spec.Name)
	}
	if downstream.Feed != plan.Index["rsi"] {
		t.Errorf("the average reads slot %d, want the rsi slot %d", downstream.Feed, plan.Index["rsi"])
	}
	if upstream.Feed != -1 {
		t.Errorf("the rsi reads slot %d, want the candles", upstream.Feed)
	}

	want := warmupOf(t, "rsi", map[string]float64{"period": 14}) + warmupOf(t, "sma", map[string]float64{"period": 3})
	if plan.Warmup != want {
		t.Errorf("warmup = %d, want %d — a chain waits for both halves", plan.Warmup, want)
	}
}

func TestThePlanKnowsHowFarBackItsRulesRead(t *testing.T) {
	cases := []struct {
		name  string
		entry string
		want  int
	}{
		{"a plain comparison", `{"long": {"gt": ["fast", "slow"]}}`, 0},
		{"a crossing", crossEntry, 1},
		{"an explicit look-back", `{"long": {"gt": [{"ref": ["close", 5]}, "fast"]}}`, 5},
		{"a run of rising bars", `{"long": {"rising": ["fast", 3]}}`, 3},
		{"a look-back inside a crossing", `{"long": {"crosses_above": [{"ref": ["fast", 2]}, "slow"]}}`, 3},
		{"the deepest of several", `{"long": {"any": [{"gt": ["fast", "slow"]}, {"rising": ["slow", 4]}]}}`, 4},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			plan := mustCompile(t, parts{entry: test.entry, exit: "null"}.json())
			if plan.Depth != test.want {
				t.Errorf("depth = %d, want %d", plan.Depth, test.want)
			}
		})
	}
}

func TestALiteralNeedsNoHistory(t *testing.T) {
	plan := mustCompile(t, parts{entry: `{"long": {"crosses_above": ["close", 10]}}`, exit: "null"}.json())

	for _, term := range plan.Entry.(compareRule).terms {
		if term.Kind == OperandLiteral && term.Slot != -1 {
			t.Errorf("a literal claimed slot %d", term.Slot)
		}
	}
	if plan.Depth != 1 {
		t.Errorf("depth = %d, want the one bar the crossing looks back", plan.Depth)
	}
}
