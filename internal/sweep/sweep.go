package sweep

import (
	"fmt"
	"slices"
	"strings"
)

const (
	KindGrid        = "grid"
	KindWalkForward = "walk_forward"

	PhaseInSample    = "in_sample"
	PhaseOutOfSample = "out_of_sample"

	MaxAxes     = 3
	MaxValues   = 40
	MaxPoints   = 200
	MaxRuns     = 400
	MaxFolds    = 12
	MinFoldDays = 5
)

var Kinds = []string{KindGrid, KindWalkForward}

const (
	ObjectiveReturn       = "return_pct"
	ObjectiveCAGR         = "cagr_pct"
	ObjectiveSharpe       = "sharpe"
	ObjectiveSortino      = "sortino"
	ObjectiveCalmar       = "calmar"
	ObjectiveProfitFactor = "profit_factor"
	ObjectiveExpectancy   = "expectancy_cents"

	DefaultObjective = ObjectiveSharpe
)

var Objectives = []string{
	ObjectiveReturn, ObjectiveCAGR, ObjectiveSharpe, ObjectiveSortino,
	ObjectiveCalmar, ObjectiveProfitFactor, ObjectiveExpectancy,
}

func ParseKind(text string) (string, error) {
	kind := strings.ToLower(strings.TrimSpace(text))
	if kind == "" {
		return KindGrid, nil
	}
	if !slices.Contains(Kinds, kind) {
		return "", fmt.Errorf("no such sweep kind %q (want one of: %s)", text, strings.Join(Kinds, ", "))
	}
	return kind, nil
}

func ParseObjective(text string) (string, error) {
	objective := strings.ToLower(strings.TrimSpace(text))
	if objective == "" {
		return DefaultObjective, nil
	}
	if !slices.Contains(Objectives, objective) {
		return "", fmt.Errorf("no such objective %q (want one of: %s)", text, strings.Join(Objectives, ", "))
	}
	return objective, nil
}
