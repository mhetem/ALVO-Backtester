package sweep

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/mhetem/ALVO-Backtester/internal/strategy"
)

const valueEpsilon = 1e-9

type Axis struct {
	Path   string    `json:"path"`
	Values []float64 `json:"values"`
}

type AxisRequest struct {
	Path   string    `json:"path"`
	From   *float64  `json:"from"`
	To     *float64  `json:"to"`
	Step   *float64  `json:"step"`
	Values []float64 `json:"values"`
}

type Point struct {
	Index  int                `json:"index"`
	Values map[string]float64 `json:"values"`
	Spec   []byte             `json:"-"`
}

// An axis addresses the number it varies with a JSON Pointer, which is the same vocabulary
// a spec fault already speaks. Only the three shapes that are numbers a strategy actually
// tunes can be reached: an indicator parameter, the sizing value, and a cost.
func ReadAxes(reqs []AxisRequest) ([]Axis, error) {
	switch {
	case len(reqs) == 0:
		return nil, fmt.Errorf("a sweep needs at least one axis, as in {\"path\": \"/inputs/fast/params/period\", \"from\": 5, \"to\": 20, \"step\": 5}")
	case len(reqs) > MaxAxes:
		return nil, fmt.Errorf("at most %d axes per sweep, got %d", MaxAxes, len(reqs))
	}

	axes := make([]Axis, 0, len(reqs))
	seen := map[string]bool{}

	for i, req := range reqs {
		path := strings.TrimSpace(req.Path)
		if err := usablePath(path); err != nil {
			return nil, fmt.Errorf("axes[%d]: %w", i, err)
		}
		if seen[path] {
			return nil, fmt.Errorf("axes[%d]: %s is swept twice", i, path)
		}
		seen[path] = true

		values, err := readValues(req)
		if err != nil {
			return nil, fmt.Errorf("axes[%d] (%s): %w", i, path, err)
		}

		axes = append(axes, Axis{Path: path, Values: values})
	}

	return axes, nil
}

func readValues(req AxisRequest) ([]float64, error) {
	values := slices.Clone(req.Values)

	if len(values) == 0 {
		if req.From == nil || req.To == nil || req.Step == nil {
			return nil, fmt.Errorf("an axis takes either values, or from, to and step")
		}
		from, to, step := *req.From, *req.To, *req.Step

		switch {
		case step <= 0:
			return nil, fmt.Errorf("step must be above zero, got %v", step)
		case to < from:
			return nil, fmt.Errorf("to (%v) is below from (%v)", to, from)
		}

		count := int(math.Floor((to-from)/step+valueEpsilon)) + 1
		if count > MaxValues {
			return nil, fmt.Errorf("from %v to %v by %v is %d values, and at most %d fit on one axis",
				from, to, step, count, MaxValues)
		}

		for i := range count {
			values = append(values, round(from+float64(i)*step))
		}
	}

	for i := range values {
		values[i] = round(values[i])
	}
	slices.Sort(values)
	values = slices.Compact(values)

	switch {
	case len(values) == 0:
		return nil, fmt.Errorf("an axis needs at least one value")
	case len(values) > MaxValues:
		return nil, fmt.Errorf("at most %d values on one axis, got %d", MaxValues, len(values))
	}

	return values, nil
}

func usablePath(path string) error {
	tokens, err := tokensOf(path)
	if err != nil {
		return err
	}

	switch {
	case len(tokens) == 4 && tokens[0] == "inputs" && tokens[2] == "params":
		return nil
	case len(tokens) == 2 && tokens[0] == "sizing" && tokens[1] == "value":
		return nil
	case len(tokens) == 2 && tokens[0] == "costs":
		return nil
	}

	return fmt.Errorf("%q is not a sweepable path (want /inputs/<name>/params/<param>, /sizing/value, or /costs/<field>)", path)
}

func tokensOf(path string) ([]string, error) {
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("a path is a JSON Pointer and starts with a slash, as in /sizing/value")
	}

	raw := strings.Split(strings.TrimPrefix(path, "/"), "/")
	tokens := make([]string, 0, len(raw))

	for _, token := range raw {
		if token == "" {
			return nil, fmt.Errorf("%q has an empty step", path)
		}
		token = strings.ReplaceAll(token, "~1", "/")
		tokens = append(tokens, strings.ReplaceAll(token, "~0", "~"))
	}

	return tokens, nil
}

// Every point is applied to the base spec and re-parsed, so a period a sweep would drive
// out of range is rejected when the sweep is created rather than by the worker that
// happens to claim that one run.
func Apply(spec []byte, values map[string]float64) ([]byte, error) {
	var root map[string]any
	if err := json.Unmarshal(spec, &root); err != nil {
		return nil, fmt.Errorf("reading the base spec: %w", err)
	}

	paths := make([]string, 0, len(values))
	for path := range values {
		paths = append(paths, path)
	}
	slices.Sort(paths)

	for _, path := range paths {
		if err := setAt(root, path, values[path]); err != nil {
			return nil, err
		}
	}

	raw, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("rebuilding the spec: %w", err)
	}

	parsed, err := strategy.Parse(raw)
	if err != nil {
		return nil, err
	}

	return json.Marshal(parsed)
}

func setAt(root map[string]any, path string, value float64) error {
	tokens, err := tokensOf(path)
	if err != nil {
		return err
	}

	at := root
	for _, token := range tokens[:len(tokens)-1] {
		next, held := at[token]
		if !held {
			return fmt.Errorf("%s: this spec has no %q", path, token)
		}
		body, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: %q is not an object", path, token)
		}
		at = body
	}

	leaf := tokens[len(tokens)-1]
	held, ok := at[leaf]
	if !ok {
		return fmt.Errorf("%s: this spec has no %q", path, leaf)
	}
	if _, ok := held.(float64); !ok {
		return fmt.Errorf("%s: %q is not a number", path, leaf)
	}

	at[leaf] = value

	return nil
}

// The first axis varies slowest, so a grid reads as rows of the first axis across columns
// of the second — which is the shape the heatmap draws.
func Grid(spec []byte, axes []Axis) ([]Point, error) {
	total := 1
	for _, axis := range axes {
		total *= len(axis.Values)
		if total > MaxPoints {
			return nil, fmt.Errorf("this grid is %d points or more, and at most %d run at once", total, MaxPoints)
		}
	}

	points := make([]Point, 0, total)
	cursor := make([]int, len(axes))

	for i := range total {
		values := make(map[string]float64, len(axes))
		for k, axis := range axes {
			values[axis.Path] = axis.Values[cursor[k]]
		}

		built, err := Apply(spec, values)
		if err != nil {
			return nil, err
		}

		points = append(points, Point{Index: i, Values: values, Spec: built})

		for k := len(axes) - 1; k >= 0; k-- {
			cursor[k]++
			if cursor[k] < len(axes[k].Values) {
				break
			}
			cursor[k] = 0
		}
	}

	return points, nil
}

func round(value float64) float64 {
	return math.Round(value*1e6) / 1e6
}
