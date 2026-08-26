package strategy

import (
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/mhetem/ALVO-Backtester/internal/indicator"
)

type Slot struct {
	Unit   int
	Output int
	Name   string
}

type Unit struct {
	Instance indicator.Instance
	Feed     int
	Slots    []int
	Warmup   int
	Prime    int
}

type Bracket struct {
	Level Level
	Slot  int
}

type Plan struct {
	Spec      Spec
	Units     []Unit
	Slots     []Slot
	Index     map[string]int
	Entry     Rule
	Exit      Rule
	Stop      *Bracket
	Target    *Bracket
	Depth     int
	Warmup    int
	PrimeBars int
}

type builder struct {
	plan  *Plan
	units map[string]int
	slots map[string]int
}

func Compile(spec Spec) (*Plan, error) {
	b := &builder{
		plan:  &Plan{Spec: spec, Index: map[string]int{}},
		units: map[string]int{},
		slots: map[string]int{},
	}

	if err := b.inputs(); err != nil {
		return nil, err
	}

	entry, err := b.rule(spec.Entry.Long)
	if err != nil {
		return nil, err
	}
	b.plan.Entry = entry

	if spec.Exit != nil {
		residual, err := b.hoist(spec.Exit.Long)
		if err != nil {
			return nil, err
		}
		if residual != nil {
			exit, err := b.rule(residual)
			if err != nil {
				return nil, err
			}
			b.plan.Exit = exit
		}
	}

	b.plan.Depth = max(depthOf(b.plan.Entry), depthOf(b.plan.Exit))
	b.measure()

	return b.plan, nil
}

func (b *builder) inputs() error {
	chains := map[string]string{}
	names := make([]string, 0, len(b.plan.Spec.Inputs))

	for name, input := range b.plan.Spec.Inputs {
		names = append(names, name)
		if input.Sourced && !Field(input.Source).Valid() {
			chains[name] = input.Source
		}
	}
	slices.Sort(names)

	for _, name := range order(names, chains) {
		input := b.plan.Spec.Inputs[name]

		feed := -1
		if upstream, chained := chains[name]; chained {
			at, ok := b.plan.Index[upstream]
			if !ok {
				return fmt.Errorf("input %q reads from %q, which has no slot", name, upstream)
			}
			feed = at
		}

		source := indicator.Source("")
		if feed < 0 && input.Sourced {
			source = indicator.Source(input.Source)
		}

		instance, err := indicator.New(input.Indicator, input.Params, source)
		if err != nil {
			return fmt.Errorf("input %q: %w", name, err)
		}

		at := b.attach(instance, feed, outputOf(instance, input.Output), name)
		b.plan.Index[name] = at
	}

	return nil
}

func (b *builder) attach(instance indicator.Instance, feed, output int, name string) int {
	key := instance.Key + "|" + strconv.Itoa(feed)

	unit, held := b.units[key]
	if !held {
		unit = len(b.plan.Units)
		b.units[key] = unit
		b.plan.Units = append(b.plan.Units, Unit{Instance: instance, Feed: feed})
	}

	slotKey := key + "#" + strconv.Itoa(output)
	at, held := b.slots[slotKey]
	if held {
		return at
	}

	at = len(b.plan.Slots)
	b.slots[slotKey] = at
	b.plan.Slots = append(b.plan.Slots, Slot{Unit: unit, Output: output, Name: name})
	b.plan.Units[unit].Slots = append(b.plan.Units[unit].Slots, at)

	return at
}

func (b *builder) hoist(node Node) (Node, error) {
	switch shape := node.(type) {
	case StopLoss:
		bracket, err := b.bracket(shape.Level)
		if err != nil {
			return nil, err
		}
		b.plan.Stop = bracket
		return nil, nil

	case TakeProfit:
		bracket, err := b.bracket(shape.Level)
		if err != nil {
			return nil, err
		}
		b.plan.Target = bracket
		return nil, nil

	case Any:
		kept := make([]Node, 0, len(shape.Nodes))
		for _, inner := range shape.Nodes {
			residual, err := b.hoist(inner)
			if err != nil {
				return nil, err
			}
			if residual != nil {
				kept = append(kept, residual)
			}
		}
		switch len(kept) {
		case 0:
			return nil, nil
		case 1:
			return kept[0], nil
		default:
			return Any{Nodes: kept}, nil
		}
	}

	return node, nil
}

func (b *builder) bracket(level Level) (*Bracket, error) {
	if level.Type != LevelATR {
		return &Bracket{Level: level, Slot: -1}, nil
	}

	instance, err := indicator.New(ATRIndicator, map[string]float64{"period": float64(level.Period)}, "")
	if err != nil {
		return nil, fmt.Errorf("%s for a bracket: %w", ATRIndicator, err)
	}

	return &Bracket{Level: level, Slot: b.attach(instance, -1, 0, ATRIndicator)}, nil
}

func (b *builder) rule(node Node) (Rule, error) {
	switch shape := node.(type) {
	case All:
		rules, err := b.rules(shape.Nodes)
		if err != nil {
			return nil, err
		}
		return allRule{rules: rules}, nil

	case Any:
		rules, err := b.rules(shape.Nodes)
		if err != nil {
			return nil, err
		}
		return anyRule{rules: rules}, nil

	case Not:
		inner, err := b.rule(shape.Node)
		if err != nil {
			return nil, err
		}
		return notRule{rule: inner}, nil

	case Compare:
		return b.compare(shape)

	case StopLoss, TakeProfit:
		return nil, errors.New("a bracket cannot be evaluated as a condition")
	}

	return nil, fmt.Errorf("unknown rule shape %T", node)
}

func (b *builder) rules(nodes []Node) ([]Rule, error) {
	out := make([]Rule, 0, len(nodes))
	for _, node := range nodes {
		rule, err := b.rule(node)
		if err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	return out, nil
}

func (b *builder) compare(node Compare) (Rule, error) {
	rule := compareRule{op: node.Op, bars: 1}

	operands := node.Operands
	if node.Op == OpRising || node.Op == OpFalling {
		rule.bars = int(operands[1].Value)
		operands = operands[:1]
	}

	rule.terms = make([]Term, 0, len(operands))
	for _, operand := range operands {
		term := Term{Kind: operand.Kind, Field: operand.Field, Value: operand.Value, Back: operand.Back, Slot: -1}
		if operand.Kind == OperandInput {
			at, ok := b.plan.Index[operand.Input]
			if !ok {
				return nil, fmt.Errorf("operand %q has no slot", operand.Input)
			}
			term.Slot = at
		}
		rule.terms = append(rule.terms, term)
	}

	return rule, nil
}

func (b *builder) measure() {
	plan := b.plan

	for i := range plan.Units {
		unit := &plan.Units[i]
		own := unit.Instance.Indicator.Warmup()
		prime := indicator.PrimeBars([]indicator.Instance{unit.Instance})

		if unit.Feed >= 0 {
			upstream := plan.Units[plan.Slots[unit.Feed].Unit]
			own += upstream.Warmup
			prime += upstream.Prime
		}

		unit.Warmup = own
		unit.Prime = prime

		plan.Warmup = max(plan.Warmup, own)
		plan.PrimeBars = max(plan.PrimeBars, prime)
	}

	plan.Warmup += plan.Depth
	plan.PrimeBars = min(plan.PrimeBars+plan.Depth, indicator.MaxPrimeBars)
}

func outputOf(instance indicator.Instance, name string) int {
	at := slices.Index(instance.Spec.Outputs, name)
	if at < 0 {
		return 0
	}
	return at
}

func order(names []string, chains map[string]string) []string {
	out := make([]string, 0, len(names))
	done := make(map[string]bool, len(names))

	var visit func(string)
	visit = func(name string) {
		if done[name] {
			return
		}
		done[name] = true
		if upstream, chained := chains[name]; chained {
			visit(upstream)
		}
		out = append(out, name)
	}

	for _, name := range names {
		visit(name)
	}

	return out
}
