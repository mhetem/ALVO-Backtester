package strategy

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/mhetem/ALVO-Backtester/internal/indicator"
)

func Parse(raw []byte) (Spec, error) {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return Spec{}, &Fault{Message: "a strategy spec must be a JSON object"}
	}

	return (&parser{}).spec(root)
}

type parser struct {
	inputs  map[string]Input
	names   []string
	nodes   int
	stops   int
	targets int
	stopped map[string]bool
}

type rawInput struct {
	indicator string
	params    map[string]float64
	source    string
	output    string
	at        path
}

func (p *parser) spec(v any) (Spec, error) {
	var at path

	body, err := object(v, at, "a strategy spec")
	if err != nil {
		return Spec{}, err
	}
	if err := onlyFields(body, at, "version", "inputs", "entry", "exit", "sizing", "costs"); err != nil {
		return Spec{}, err
	}

	if raw, ok := body["version"]; ok {
		version, err := wholeNumber(raw, at.child("version"), "version")
		if err != nil {
			return Spec{}, err
		}
		if version != Version {
			return Spec{}, at.child("version").faultf("spec version must be %d, got %d", Version, version)
		}
	}

	if err := p.readInputs(body["inputs"], at.child("inputs")); err != nil {
		return Spec{}, err
	}

	entry, ok := body["entry"]
	if !ok {
		return Spec{}, at.child("entry").faultf("a strategy needs an entry rule")
	}
	entrySide, err := p.side(entry, at.child("entry"), false, "entry")
	if err != nil {
		return Spec{}, err
	}

	spec := Spec{Version: Version, Inputs: p.inputs, Entry: entrySide}

	if raw, ok := body["exit"]; ok && raw != nil {
		exitSide, err := p.side(raw, at.child("exit"), true, "exit")
		if err != nil {
			return Spec{}, err
		}
		// An exit for a side that never opens is a rule nothing can ever evaluate, and
		// silently dropping it would hide a spec that does not say what its author meant.
		if exitSide.Long != nil && spec.Entry.Long == nil {
			return Spec{}, at.child("exit").child(KeyLong).faultf("there is no %s entry for this %s exit", KeyLong, KeyLong)
		}
		if exitSide.Short != nil && spec.Entry.Short == nil {
			return Spec{}, at.child("exit").child(KeyShort).faultf("there is no %s entry for this %s exit", KeyShort, KeyShort)
		}
		spec.Exit = &exitSide
	}

	sizing, ok := body["sizing"]
	if !ok {
		return Spec{}, at.child("sizing").faultf("a strategy needs a sizing rule (one of: %s)", joinSizingTypes())
	}
	spec.Sizing, err = readSizing(sizing, at.child("sizing"))
	if err != nil {
		return Spec{}, err
	}
	// risk_pct sizes off the distance to the stop, so every side that can open a position
	// needs one of its own. A short with no stop would size at zero and skip every entry.
	if spec.Sizing.Type == SizeRiskPct {
		for _, key := range []string{KeyLong, KeyShort} {
			if spec.Entry.node(key) != nil && !p.stopped[key] {
				return Spec{}, at.child("sizing").faultf(
					"%s sizes a position off the distance to its stop, so the %s exit needs a %s",
					SizeRiskPct, key, KeyStopLoss)
			}
		}
	}

	spec.Costs, err = readCosts(body["costs"], at.child("costs"))
	if err != nil {
		return Spec{}, err
	}

	return spec, nil
}

func (p *parser) readInputs(v any, at path) error {
	if v == nil {
		return at.faultf("a strategy needs at least one input")
	}

	body, err := object(v, at, "inputs")
	if err != nil {
		return err
	}
	switch {
	case len(body) == 0:
		return at.faultf("a strategy needs at least one input")
	case len(body) > MaxInputs:
		return at.faultf("at most %d inputs per strategy, got %d", MaxInputs, len(body))
	}

	names := sortedKeys(body)
	raws := make(map[string]rawInput, len(body))

	for _, name := range names {
		spot := at.child(name)
		if err := usableName(name, spot); err != nil {
			return err
		}
		raw, err := readInput(body[name], spot)
		if err != nil {
			return err
		}
		raws[name] = raw
	}

	chains := map[string]string{}
	for _, name := range names {
		raw := raws[name]
		spot := raw.at.child("source")

		switch {
		case raw.source == "":
		case raw.source == string(FieldVolume):
			return spot.faultf("volume is not an indicator source — the volumema indicator averages volume instead")
		case Field(raw.source).Valid():
		case raw.source == name:
			return spot.faultf("%q reads from itself", name)
		default:
			if _, ok := raws[raw.source]; !ok {
				return spot.faultf("%q names neither a price field (%s) nor another input", raw.source, JoinFields())
			}
			chains[name] = raw.source
		}
	}

	if cycle := findCycle(names, chains); len(cycle) > 0 {
		head := cycle[0]
		loop := slices.Concat(cycle, []string{head})
		return raws[head].at.child("source").faultf(
			"these inputs feed each other in a loop: %s", strings.Join(loop, " → "))
	}

	p.inputs = make(map[string]Input, len(raws))
	p.names = names

	for _, name := range names {
		input, err := build(raws[name], chains[name] != "")
		if err != nil {
			return err
		}
		p.inputs[name] = input
	}

	return nil
}

func build(raw rawInput, chained bool) (Input, error) {
	spec, ok := indicator.Lookup(raw.indicator)
	if !ok {
		return Input{}, raw.at.child("indicator").faultf(
			"unknown indicator %q (want one of: %s)", raw.indicator, strings.Join(indicator.Names(), ", "))
	}
	if raw.source != "" && !spec.Sourced {
		return Input{}, raw.at.child("source").faultf("%s reads the whole candle, so it takes no source", spec.Name)
	}
	if fault := paramFault(spec, raw.params, raw.at.child("params")); fault != nil {
		return Input{}, fault
	}

	source := indicator.Source("")
	if !chained && raw.source != "" {
		source = indicator.Source(raw.source)
	}

	instance, err := indicator.New(spec.Name, raw.params, source)
	if err != nil {
		return Input{}, raw.at.child("params").faultf("%s", err)
	}

	output := raw.output
	if output == "" {
		output = spec.Outputs[0]
	}
	if !slices.Contains(spec.Outputs, output) {
		return Input{}, raw.at.child("output").faultf(
			"%s has no output %q (want one of: %s)", spec.Name, output, strings.Join(spec.Outputs, ", "))
	}

	input := Input{
		Indicator: spec.Name,
		Params:    instance.Params.All(),
		Output:    output,
		Sourced:   spec.Sourced,
		Multi:     len(spec.Outputs) > 1,
	}
	if spec.Sourced {
		input.Source = raw.source
		if input.Source == "" {
			input.Source = string(indicator.DefaultSource)
		}
	}

	return input, nil
}

func readInput(v any, at path) (rawInput, error) {
	body, err := object(v, at, "an input")
	if err != nil {
		return rawInput{}, err
	}
	if err := onlyFields(body, at, "indicator", "params", "source", "output"); err != nil {
		return rawInput{}, err
	}

	raw := rawInput{at: at, params: map[string]float64{}}

	name, ok := body["indicator"]
	if !ok {
		return rawInput{}, at.child("indicator").faultf("an input needs an indicator")
	}
	text, err := str(name, at.child("indicator"), "an indicator")
	if err != nil {
		return rawInput{}, err
	}
	raw.indicator = clean(text)

	if got, ok := body["params"]; ok && got != nil {
		spot := at.child("params")
		params, err := object(got, spot, "params")
		if err != nil {
			return rawInput{}, err
		}
		for _, key := range sortedKeys(params) {
			value, err := number(params[key], spot.child(key), key)
			if err != nil {
				return rawInput{}, err
			}
			raw.params[clean(key)] = value
		}
	}

	for _, field := range []struct {
		name string
		into *string
	}{{"source", &raw.source}, {"output", &raw.output}} {
		got, ok := body[field.name]
		if !ok || got == nil {
			continue
		}
		text, err := str(got, at.child(field.name), field.name)
		if err != nil {
			return rawInput{}, err
		}
		*field.into = clean(text)
	}

	return raw, nil
}

func paramFault(spec indicator.Spec, params map[string]float64, at path) *Fault {
	declared := make(map[string]indicator.Param, len(spec.Params))
	for _, param := range spec.Params {
		declared[param.Name] = param
	}

	for _, name := range sortedParams(params) {
		param, ok := declared[name]
		if !ok {
			return at.child(name).faultf("%s has no parameter %q (want one of: %s)", spec.Name, name, paramNames(spec))
		}

		value := params[name]
		switch {
		case value < param.Min || value > param.Max:
			return at.child(name).faultf("%s must be between %s and %s, got %s",
				name, show(param.Kind, param.Min), show(param.Kind, param.Max), show(param.Kind, value))
		case param.Kind == indicator.ParamInt && value != math.Trunc(value):
			return at.child(name).faultf("%s must be a whole number, got %s", name, show(indicator.ParamFloat, value))
		}
	}

	return nil
}

func (p *parser) side(v any, at path, brackets bool, what string) (Side, error) {
	body, err := object(v, at, what)
	if err != nil {
		return Side{}, err
	}
	if err := onlyFields(body, at, KeyLong, KeyShort); err != nil {
		return Side{}, err
	}

	var side Side

	if raw, ok := body[KeyLong]; ok {
		side.Long, err = p.leg(raw, at.child(KeyLong), brackets, KeyLong)
		if err != nil {
			return Side{}, err
		}
	}
	if raw, ok := body[KeyShort]; ok {
		side.Short, err = p.leg(raw, at.child(KeyShort), brackets, KeyShort)
		if err != nil {
			return Side{}, err
		}
	}

	if side.Long == nil && side.Short == nil {
		return Side{}, at.faultf("%s needs a %s rule, a %s rule, or both", what, KeyLong, KeyShort)
	}

	return side, nil
}

// Each side carries its own stop and target, so the one-of-each guard counts per side
// rather than per spec: a long stop and a short stop are two positions' worth, not two
// brackets on one position.
func (p *parser) leg(v any, at path, brackets bool, key string) (Node, error) {
	p.stops, p.targets = 0, 0

	node, err := p.node(v, at, 1, brackets)
	if err != nil {
		return nil, err
	}
	if p.stops > 0 {
		if p.stopped == nil {
			p.stopped = map[string]bool{}
		}
		p.stopped[key] = true
	}

	return node, nil
}

func (p *parser) node(v any, at path, depth int, brackets bool) (Node, error) {
	if depth > MaxDepth {
		return nil, at.faultf("rules nest at most %d levels deep", MaxDepth)
	}

	p.nodes++
	if p.nodes > MaxNodes {
		return nil, at.faultf("a strategy carries at most %d rule nodes", MaxNodes)
	}

	body, err := object(v, at, "a rule")
	if err != nil {
		return nil, err
	}
	if len(body) != 1 {
		return nil, at.faultf("a rule is an object carrying exactly one operator, got %d", len(body))
	}

	key := sortedKeys(body)[0]
	value := body[key]
	spot := at.child(key)

	switch key {
	case KeyAll, KeyAny:
		return p.combine(key, value, spot, depth, brackets)
	case KeyNot:
		inner, err := p.node(value, spot, depth+1, false)
		if err != nil {
			return nil, err
		}
		return Not{Node: inner}, nil
	case KeyStopLoss, KeyTakeProfit:
		return p.bracket(key, value, spot, brackets)
	}

	op := Comparator(key)
	if !known(op) {
		return nil, spot.faultf("no such rule %q (want a comparator: %s — or a combinator: %s)",
			key, joinComparators(), strings.Join(Combinators, ", "))
	}

	return p.compare(op, value, spot)
}

func (p *parser) combine(key string, v any, at path, depth int, brackets bool) (Node, error) {
	items, err := list(v, at, key)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		if key == KeyAll {
			return nil, at.faultf("an empty %q matches every bar — say what has to be true", key)
		}
		return nil, at.faultf("an empty %q never matches — say what would trigger it", key)
	}

	nodes := make([]Node, 0, len(items))
	for i, item := range items {
		inner, err := p.node(item, at.index(i), depth+1, brackets && key == KeyAny)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, inner)
	}

	if key == KeyAll {
		return All{Nodes: nodes}, nil
	}
	return Any{Nodes: nodes}, nil
}

func (p *parser) bracket(key string, v any, at path, brackets bool) (Node, error) {
	if !brackets {
		return nil, at.faultf(
			"a %s is a resting order on an open position, not a condition on a bar — it belongs at the top of an exit rule or directly inside that rule's %q",
			key, KeyAny)
	}

	level, err := readLevel(v, at, key)
	if err != nil {
		return nil, err
	}

	if key == KeyStopLoss {
		p.stops++
		if p.stops > 1 {
			return nil, at.faultf("a position carries one %s", key)
		}
		return StopLoss{Level: level}, nil
	}

	p.targets++
	if p.targets > 1 {
		return nil, at.faultf("a position carries one %s", key)
	}
	return TakeProfit{Level: level}, nil
}

func (p *parser) compare(op Comparator, v any, at path) (Node, error) {
	items, err := list(v, at, string(op))
	if err != nil {
		return nil, err
	}

	low, high := arity(op)
	if len(items) < low || len(items) > high {
		return nil, at.faultf("%s takes %s, got %d", op, arityText(low, high), len(items))
	}

	operands := make([]Operand, 0, max(len(items), low))
	for i, item := range items {
		operand, err := p.operand(item, at.index(i))
		if err != nil {
			return nil, err
		}
		operands = append(operands, operand)
	}

	if op == OpBetween {
		from, to := operands[1], operands[2]
		if from.Kind == OperandLiteral && to.Kind == OperandLiteral && from.Value > to.Value {
			return nil, at.index(2).faultf(
				"%s reads [value, low, high], so %v cannot be the high of a span starting at %v", op, to.Value, from.Value)
		}
	}

	if op != OpRising && op != OpFalling {
		return Compare{Op: op, Operands: operands}, nil
	}

	if len(operands) == 1 {
		operands = append(operands, Operand{Kind: OperandLiteral, Value: 1})
	}

	bars := operands[1]
	if bars.Kind != OperandLiteral || bars.Back != 0 || bars.Value < 1 || bars.Value > MaxBack || bars.Value != math.Trunc(bars.Value) {
		return nil, at.index(1).faultf(
			"%s counts bars, so its second operand is a whole number between 1 and %d", op, MaxBack)
	}

	return Compare{Op: op, Operands: operands}, nil
}

func (p *parser) operand(v any, at path) (Operand, error) {
	switch value := v.(type) {
	case float64:
		return Operand{Kind: OperandLiteral, Value: value}, nil
	case string:
		return p.named(value, 0, at)
	case map[string]any:
		return p.ref(value, at)
	default:
		return Operand{}, at.faultf(
			`an operand is an input name, a price field, a number, or {"%s": [name, bars back]}`, KeyRef)
	}
}

func (p *parser) ref(body map[string]any, at path) (Operand, error) {
	if len(body) != 1 {
		return Operand{}, at.faultf(`an operand object carries only {"%s": [name, bars back]}`, KeyRef)
	}

	key := sortedKeys(body)[0]
	if key != KeyRef {
		return Operand{}, at.child(key).faultf(`no such operand %q — an operand object is {"%s": [name, bars back]}`, key, KeyRef)
	}

	spot := at.child(KeyRef)
	items, err := list(body[key], spot, KeyRef)
	if err != nil {
		return Operand{}, err
	}
	if len(items) != 2 {
		return Operand{}, spot.faultf("a %s is [name, bars back], so it takes 2 entries, got %d", KeyRef, len(items))
	}

	name, ok := items[0].(string)
	if !ok {
		return Operand{}, spot.index(0).faultf("a %s looks back at an input or a price field, so its first entry is a name", KeyRef)
	}

	back, err := wholeNumber(items[1], spot.index(1), "bars back")
	if err != nil {
		return Operand{}, err
	}
	if back < 0 || back > MaxBack {
		return Operand{}, spot.index(1).faultf("a %s looks back between 0 and %d bars, got %d", KeyRef, MaxBack, back)
	}

	return p.named(name, back, spot.index(0))
}

func (p *parser) named(name string, back int, at path) (Operand, error) {
	trimmed := clean(name)

	if held, line, dotted := strings.Cut(trimmed, LineSep); dotted {
		return p.line(held, line, back, at)
	}
	if _, ok := p.inputs[trimmed]; ok {
		return Operand{Kind: OperandInput, Input: trimmed, Back: back}, nil
	}
	if Field(trimmed).Valid() {
		return Operand{Kind: OperandField, Field: Field(trimmed), Back: back}, nil
	}

	return Operand{}, at.faultf("%q names neither an input (%s) nor a price field (%s)",
		name, strings.Join(p.names, ", "), JoinFields())
}

// line reads one output of an input that emits several. Declaring the indicator once and
// naming its lines beats declaring it once per line: Ichimoku alone would otherwise spend
// five of the twelve inputs a spec is allowed.
func (p *parser) line(held, line string, back int, at path) (Operand, error) {
	input, ok := p.inputs[held]
	if !ok {
		return Operand{}, at.faultf("%q names no input (want one of: %s)", held, strings.Join(p.names, ", "))
	}

	spec, ok := indicator.Lookup(input.Indicator)
	if !ok {
		return Operand{}, at.faultf("input %q holds unknown indicator %q", held, input.Indicator)
	}
	if !slices.Contains(spec.Outputs, line) {
		return Operand{}, at.faultf("%s has no output %q (want one of: %s)",
			spec.Name, line, strings.Join(spec.Outputs, ", "))
	}

	// A single-output indicator has nothing to disambiguate, so the plain name is the
	// canonical form and the dotted one collapses onto it.
	if len(spec.Outputs) == 1 {
		return Operand{Kind: OperandInput, Input: held, Back: back}, nil
	}

	return Operand{Kind: OperandInput, Input: held, Output: line, Back: back}, nil
}

func readLevel(v any, at path, what string) (Level, error) {
	body, err := object(v, at, what)
	if err != nil {
		return Level{}, err
	}
	if err := onlyFields(body, at, "type", "value", "period", "mult"); err != nil {
		return Level{}, err
	}

	raw, ok := body["type"]
	if !ok {
		return Level{}, at.child("type").faultf("a %s needs a type (one of: %s)", what, joinLevelTypes())
	}
	text, err := str(raw, at.child("type"), "a type")
	if err != nil {
		return Level{}, err
	}

	switch LevelType(clean(text)) {
	case LevelPct:
		value, err := requiredNumber(body, at, "value", what)
		if err != nil {
			return Level{}, err
		}
		if value <= 0 || value >= 1 {
			return Level{}, at.child("value").faultf(
				"a pct %s is a fraction of the entry price above 0 and below 1, got %v", what, value)
		}
		if err := unwanted(body, at, what, LevelPct, "period", "mult"); err != nil {
			return Level{}, err
		}
		return Level{Type: LevelPct, Value: value}, nil

	case LevelATR:
		period, err := requiredWhole(body, at, "period", what)
		if err != nil {
			return Level{}, err
		}
		if fault := atrPeriodFault(period, at.child("period")); fault != nil {
			return Level{}, fault
		}
		mult, err := requiredNumber(body, at, "mult", what)
		if err != nil {
			return Level{}, err
		}
		if mult <= 0 || mult > 100 {
			return Level{}, at.child("mult").faultf("an atr %s multiplies the range by more than 0 and at most 100, got %v", what, mult)
		}
		if err := unwanted(body, at, what, LevelATR, "value"); err != nil {
			return Level{}, err
		}
		return Level{Type: LevelATR, Period: period, Mult: mult}, nil
	}

	return Level{}, at.child("type").faultf("no such %s type %q (want one of: %s)", what, text, joinLevelTypes())
}

func atrPeriodFault(period int, at path) *Fault {
	spec, ok := indicator.Lookup(ATRIndicator)
	if !ok {
		return at.faultf("the %s indicator is not registered", ATRIndicator)
	}

	for _, param := range spec.Params {
		if param.Name != "period" {
			continue
		}
		if float64(period) < param.Min || float64(period) > param.Max {
			return at.faultf("period must be between %s and %s, got %d",
				show(param.Kind, param.Min), show(param.Kind, param.Max), period)
		}
	}

	return nil
}

func readSizing(v any, at path) (Sizing, error) {
	body, err := object(v, at, "sizing")
	if err != nil {
		return Sizing{}, err
	}
	if err := onlyFields(body, at, "type", "value"); err != nil {
		return Sizing{}, err
	}

	raw, ok := body["type"]
	if !ok {
		return Sizing{}, at.child("type").faultf("sizing needs a type (one of: %s)", joinSizingTypes())
	}
	text, err := str(raw, at.child("type"), "a type")
	if err != nil {
		return Sizing{}, err
	}

	kind := SizingType(clean(text))
	if !slices.Contains(SizingTypes, kind) {
		return Sizing{}, at.child("type").faultf("no such sizing type %q (want one of: %s)", text, joinSizingTypes())
	}

	value, err := requiredNumber(body, at, "value", "sizing")
	if err != nil {
		return Sizing{}, err
	}
	spot := at.child("value")

	switch kind {
	case SizeFixedQty:
		if value < 1 || value > 1e9 || value != math.Trunc(value) {
			return Sizing{}, spot.faultf("%s is a whole number of shares between 1 and 1000000000, got %v", kind, value)
		}
	case SizeFixedCash:
		if value < 1 || value > 1e15 || value != math.Trunc(value) {
			return Sizing{}, spot.faultf("%s is a whole number of cents between 1 and 1000000000000000, got %v", kind, value)
		}
	default:
		if value <= 0 || value > 1 {
			return Sizing{}, spot.faultf("%s is a fraction of equity above 0 and at most 1, got %v", kind, value)
		}
	}

	return Sizing{Type: kind, Value: value}, nil
}

func readCosts(v any, at path) (Costs, error) {
	costs := DefaultCosts()
	if v == nil {
		return costs, nil
	}

	body, err := object(v, at, "costs")
	if err != nil {
		return Costs{}, err
	}
	if err := onlyFields(body, at, "brokerage_cents", "fee_bps", "slippage_bps"); err != nil {
		return Costs{}, err
	}

	if raw, ok := body["brokerage_cents"]; ok {
		cents, err := wholeNumber(raw, at.child("brokerage_cents"), "brokerage_cents")
		if err != nil {
			return Costs{}, err
		}
		if cents < 0 || cents > MaxBrokerageCents {
			return Costs{}, at.child("brokerage_cents").faultf(
				"brokerage is a whole number of cents between 0 and %d, got %d", MaxBrokerageCents, cents)
		}
		costs.BrokerageCents = int64(cents)
	}

	for _, field := range []struct {
		name string
		into *float64
	}{{"fee_bps", &costs.FeeBPS}, {"slippage_bps", &costs.SlippageBPS}} {
		raw, ok := body[field.name]
		if !ok {
			continue
		}
		value, err := number(raw, at.child(field.name), field.name)
		if err != nil {
			return Costs{}, err
		}
		if value < 0 || value > MaxBPS {
			return Costs{}, at.child(field.name).faultf(
				"%s is between 0 and %v basis points, got %v", field.name, MaxBPS, value)
		}
		*field.into = value
	}

	return costs, nil
}

func findCycle(names []string, chains map[string]string) []string {
	const (
		open = 1
		shut = 2
	)

	state := make(map[string]int, len(names))

	for _, name := range names {
		if state[name] != 0 {
			continue
		}

		var walk []string
		at := name

		for at != "" && state[at] == 0 {
			state[at] = open
			walk = append(walk, at)
			at = chains[at]
		}

		if at != "" && state[at] == open {
			return walk[slices.Index(walk, at):]
		}
		for _, seen := range walk {
			state[seen] = shut
		}
	}

	return nil
}

func object(v any, at path, what string) (map[string]any, error) {
	body, ok := v.(map[string]any)
	if !ok {
		return nil, at.faultf("%s is a JSON object", what)
	}
	return body, nil
}

func list(v any, at path, what string) ([]any, error) {
	items, ok := v.([]any)
	if !ok {
		return nil, at.faultf("%s takes a JSON array", what)
	}
	return items, nil
}

func str(v any, at path, what string) (string, error) {
	text, ok := v.(string)
	if !ok {
		return "", at.faultf("%s is a string", what)
	}
	return text, nil
}

func number(v any, at path, what string) (float64, error) {
	value, ok := v.(float64)
	if !ok {
		return 0, at.faultf("%s is a number", what)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, at.faultf("%s is a finite number", what)
	}
	return value, nil
}

func wholeNumber(v any, at path, what string) (int, error) {
	value, err := number(v, at, what)
	if err != nil {
		return 0, err
	}
	if value != math.Trunc(value) || math.Abs(value) > math.MaxInt32 {
		return 0, at.faultf("%s is a whole number, got %v", what, value)
	}
	return int(value), nil
}

func requiredNumber(body map[string]any, at path, field, what string) (float64, error) {
	raw, ok := body[field]
	if !ok {
		return 0, at.child(field).faultf("a %s needs a %s", what, field)
	}
	return number(raw, at.child(field), field)
}

func requiredWhole(body map[string]any, at path, field, what string) (int, error) {
	raw, ok := body[field]
	if !ok {
		return 0, at.child(field).faultf("a %s needs a %s", what, field)
	}
	return wholeNumber(raw, at.child(field), field)
}

func unwanted(body map[string]any, at path, what string, kind LevelType, fields ...string) error {
	for _, field := range fields {
		if _, ok := body[field]; ok {
			return at.child(field).faultf("a %s %s takes no %s", kind, what, field)
		}
	}
	return nil
}

func onlyFields(body map[string]any, at path, allowed ...string) error {
	for _, name := range sortedKeys(body) {
		if !slices.Contains(allowed, name) {
			return at.child(name).faultf("no such field %q (want one of: %s)", name, strings.Join(allowed, ", "))
		}
	}
	return nil
}

func usableName(name string, at path) error {
	switch {
	case name == "":
		return at.faultf("an input needs a name")
	case len(name) > MaxNameLen:
		return at.faultf("an input name is at most %d characters", MaxNameLen)
	case !identifier(name):
		return at.faultf(
			"%q is not a usable input name — use lower-case letters, digits and underscores, starting with a letter", name)
	case Field(name).Valid():
		return at.faultf("%q is a price field, so it cannot also name an input", name)
	}

	return nil
}

func identifier(name string) bool {
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case i > 0 && (r >= '0' && r <= '9' || r == '_'):
		default:
			return false
		}
	}
	return name != ""
}

func clean(text string) string {
	return strings.ToLower(strings.TrimSpace(text))
}

func sortedKeys(body map[string]any) []string {
	names := make([]string, 0, len(body))
	for name := range body {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func sortedParams(body map[string]float64) []string {
	names := make([]string, 0, len(body))
	for name := range body {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func paramNames(spec indicator.Spec) string {
	names := make([]string, 0, len(spec.Params))
	for _, param := range spec.Params {
		names = append(names, param.Name)
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

func show(kind indicator.ParamKind, value float64) string {
	if kind == indicator.ParamInt {
		return strconv.Itoa(int(value))
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func arityText(low, high int) string {
	if low == high {
		return fmt.Sprintf("%d operands", low)
	}
	return fmt.Sprintf("%d or %d operands", low, high)
}
