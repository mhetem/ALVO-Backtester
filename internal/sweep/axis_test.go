package sweep

import (
	"encoding/json"
	"strings"
	"testing"
)

const baseSpec = `{
  "version": 1,
  "inputs": {"fast": {"indicator": "ema", "params": {"period": 9}, "source": "close"}},
  "entry": {"long": {"crosses_above": ["close", "fast"]}},
  "sizing": {"type": "fixed_qty", "value": 100},
  "costs": {"brokerage_cents": 0, "fee_bps": 3.25, "slippage_bps": 5}
}`

func value(v float64) *float64 { return &v }

func TestAnAxisExpandsFromToAndStep(t *testing.T) {
	axes, err := ReadAxes([]AxisRequest{{
		Path: "/inputs/fast/params/period",
		From: value(5),
		To:   value(20),
		Step: value(5),
	}})
	if err != nil {
		t.Fatalf("reading the axis: %v", err)
	}

	want := []float64{5, 10, 15, 20}
	if len(axes[0].Values) != len(want) {
		t.Fatalf("values = %v, want %v", axes[0].Values, want)
	}
	for i, got := range axes[0].Values {
		if got != want[i] {
			t.Errorf("values[%d] = %g, want %g", i, got, want[i])
		}
	}
}

func TestAnAxisSortsAndDedupesTheValuesItIsGiven(t *testing.T) {
	axes, err := ReadAxes([]AxisRequest{{
		Path:   "/sizing/value",
		Values: []float64{300, 100, 200, 100},
	}})
	if err != nil {
		t.Fatalf("reading the axis: %v", err)
	}

	want := []float64{100, 200, 300}
	if len(axes[0].Values) != len(want) {
		t.Fatalf("values = %v, want %v", axes[0].Values, want)
	}
	for i, got := range axes[0].Values {
		if got != want[i] {
			t.Errorf("values[%d] = %g, want %g", i, got, want[i])
		}
	}
}

func TestAnAxisRefusesAPathThatIsNotATunableNumber(t *testing.T) {
	for _, path := range []string{"/version", "/entry/long", "inputs/fast/params/period", "/inputs/fast"} {
		if _, err := ReadAxes([]AxisRequest{{Path: path, Values: []float64{1}}}); err == nil {
			t.Errorf("%q was accepted as a sweepable path", path)
		}
	}
}

func TestTwoAxesCannotSweepTheSameNumber(t *testing.T) {
	_, err := ReadAxes([]AxisRequest{
		{Path: "/sizing/value", Values: []float64{100}},
		{Path: "/sizing/value", Values: []float64{200}},
	})
	if err == nil {
		t.Error("the same path was accepted on two axes")
	}
}

func TestApplyRewritesTheNumberAndReparsesTheSpec(t *testing.T) {
	built, err := Apply([]byte(baseSpec), map[string]float64{"/inputs/fast/params/period": 21})
	if err != nil {
		t.Fatalf("applying the point: %v", err)
	}

	var root map[string]any
	if err := json.Unmarshal(built, &root); err != nil {
		t.Fatalf("reading the built spec: %v", err)
	}

	inputs := root["inputs"].(map[string]any)
	params := inputs["fast"].(map[string]any)["params"].(map[string]any)
	if got := params["period"].(float64); got != 21 {
		t.Errorf("period = %g, want 21", got)
	}
}

func TestApplyRefusesToInventAFieldTheSpecDoesNotHave(t *testing.T) {
	_, err := Apply([]byte(baseSpec), map[string]float64{"/inputs/slow/params/period": 21})
	if err == nil {
		t.Fatal("a path naming an input that does not exist was applied")
	}
	if !strings.Contains(err.Error(), "slow") {
		t.Errorf("the error is %q, want it to name the missing input", err)
	}
}

func TestAGridVariesTheFirstAxisSlowest(t *testing.T) {
	axes, err := ReadAxes([]AxisRequest{
		{Path: "/inputs/fast/params/period", Values: []float64{5, 10}},
		{Path: "/sizing/value", Values: []float64{100, 200}},
	})
	if err != nil {
		t.Fatalf("reading the axes: %v", err)
	}

	points, err := Grid([]byte(baseSpec), axes)
	if err != nil {
		t.Fatalf("building the grid: %v", err)
	}
	if len(points) != 4 {
		t.Fatalf("points = %d, want 4", len(points))
	}

	want := [][2]float64{{5, 100}, {5, 200}, {10, 100}, {10, 200}}
	for i, point := range points {
		if point.Index != i {
			t.Errorf("points[%d].Index = %d, want %d", i, point.Index, i)
		}
		period := point.Values["/inputs/fast/params/period"]
		size := point.Values["/sizing/value"]
		if period != want[i][0] || size != want[i][1] {
			t.Errorf("points[%d] = (%g, %g), want (%g, %g)", i, period, size, want[i][0], want[i][1])
		}
	}
}

func TestAGridRejectsAPointTheSpecWouldRefuse(t *testing.T) {
	// A fixed share count of zero is not a strategy, and finding that out here is the whole
	// reason every point is parsed before anything is queued.
	axes, err := ReadAxes([]AxisRequest{{Path: "/sizing/value", Values: []float64{0, 100}}})
	if err != nil {
		t.Fatalf("reading the axis: %v", err)
	}

	if _, err := Grid([]byte(baseSpec), axes); err == nil {
		t.Error("a grid containing an unrunnable point was accepted")
	}
}

func TestAGridRefusesToOutgrowTheQueue(t *testing.T) {
	axes := []Axis{
		{Path: "/inputs/fast/params/period", Values: make([]float64, MaxValues)},
		{Path: "/sizing/value", Values: make([]float64, MaxValues)},
	}
	for i := range axes[0].Values {
		axes[0].Values[i] = float64(i + 2)
		axes[1].Values[i] = float64(i + 1)
	}

	if _, err := Grid([]byte(baseSpec), axes); err == nil {
		t.Errorf("a %d point grid was accepted, want a refusal past %d", MaxValues*MaxValues, MaxPoints)
	}
}
