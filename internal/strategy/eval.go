package strategy

import (
	"math"

	"github.com/mhetem/ALVO-Backtester/internal/indicator"
)

const epsilon = 1e-9

type Frame interface {
	Slot(slot, back int) (float64, bool)
	Field(field Field, back int) (float64, bool)
}

type Rule interface {
	Eval(f Frame) (bool, bool)
	depth() int
}

type Term struct {
	Kind  OperandKind
	Slot  int
	Field Field
	Value float64
	Back  int
}

func (t Term) at(f Frame, extra int) (float64, bool) {
	switch t.Kind {
	case OperandLiteral:
		return t.Value, true
	case OperandField:
		return f.Field(t.Field, t.Back+extra)
	default:
		return f.Slot(t.Slot, t.Back+extra)
	}
}

type allRule struct{ rules []Rule }

type anyRule struct{ rules []Rule }

type notRule struct{ rule Rule }

type compareRule struct {
	op    Comparator
	terms []Term
	bars  int
}

func (r allRule) Eval(f Frame) (bool, bool) {
	settled := true

	for _, rule := range r.rules {
		value, known := rule.Eval(f)
		switch {
		case known && !value:
			return false, true
		case !known:
			settled = false
		}
	}

	if !settled {
		return false, false
	}
	return true, true
}

func (r anyRule) Eval(f Frame) (bool, bool) {
	settled := true

	for _, rule := range r.rules {
		value, known := rule.Eval(f)
		switch {
		case known && value:
			return true, true
		case !known:
			settled = false
		}
	}

	if !settled {
		return false, false
	}
	return false, true
}

func (r notRule) Eval(f Frame) (bool, bool) {
	value, known := r.rule.Eval(f)
	if !known {
		return false, false
	}
	return !value, true
}

func (r compareRule) Eval(f Frame) (bool, bool) {
	switch r.op {
	case OpRising, OpFalling:
		return r.trend(f)
	case OpCrossesAbove, OpCrossesBelow:
		return r.cross(f)
	case OpBetween:
		return r.span(f)
	}

	left, known := r.terms[0].at(f, 0)
	if !known {
		return false, false
	}
	right, known := r.terms[1].at(f, 0)
	if !known {
		return false, false
	}

	switch r.op {
	case OpGT:
		return left > right, true
	case OpLT:
		return left < right, true
	case OpGTE:
		return left >= right, true
	case OpLTE:
		return left <= right, true
	case OpEQ:
		return nearly(left, right), true
	}

	return false, false
}

func (r compareRule) cross(f Frame) (bool, bool) {
	now, known := r.terms[0].at(f, 0)
	if !known {
		return false, false
	}
	against, known := r.terms[1].at(f, 0)
	if !known {
		return false, false
	}
	before, known := r.terms[0].at(f, 1)
	if !known {
		return false, false
	}
	againstBefore, known := r.terms[1].at(f, 1)
	if !known {
		return false, false
	}

	if r.op == OpCrossesAbove {
		return now > against && before <= againstBefore, true
	}
	return now < against && before >= againstBefore, true
}

func (r compareRule) trend(f Frame) (bool, bool) {
	for step := range r.bars {
		now, known := r.terms[0].at(f, step)
		if !known {
			return false, false
		}
		before, known := r.terms[0].at(f, step+1)
		if !known {
			return false, false
		}

		rose := now > before
		if r.op == OpFalling {
			rose = now < before
		}
		if !rose {
			return false, true
		}
	}

	return true, true
}

func (r compareRule) span(f Frame) (bool, bool) {
	value, known := r.terms[0].at(f, 0)
	if !known {
		return false, false
	}
	low, known := r.terms[1].at(f, 0)
	if !known {
		return false, false
	}
	high, known := r.terms[2].at(f, 0)
	if !known {
		return false, false
	}

	return value >= low && value <= high, true
}

func (r allRule) depth() int { return deepest(r.rules) }

func (r anyRule) depth() int { return deepest(r.rules) }

func (r notRule) depth() int { return r.rule.depth() }

func (r compareRule) depth() int {
	extra := 0
	switch r.op {
	case OpCrossesAbove, OpCrossesBelow:
		extra = 1
	case OpRising, OpFalling:
		extra = r.bars
	}

	deep := 0
	for _, term := range r.terms {
		if term.Kind != OperandLiteral {
			deep = max(deep, term.Back+extra)
		}
	}

	return deep
}

func deepest(rules []Rule) int {
	deep := 0
	for _, rule := range rules {
		deep = max(deep, rule.depth())
	}
	return deep
}

func depthOf(rule Rule) int {
	if rule == nil {
		return 0
	}
	return rule.depth()
}

func nearly(a, b float64) bool {
	diff := math.Abs(a - b)
	if diff <= epsilon {
		return true
	}
	return diff <= epsilon*math.Max(math.Abs(a), math.Abs(b))
}

type Tape struct {
	plan   *Plan
	size   int
	at     int
	filled int
	bars   []indicator.Candle
	values [][]float64
}

func (p *Plan) NewTape() *Tape {
	size := max(p.Depth+1, 2)

	tape := &Tape{
		plan:   p,
		size:   size,
		at:     -1,
		bars:   make([]indicator.Candle, size),
		values: make([][]float64, len(p.Slots)),
	}
	for i := range tape.values {
		tape.values[i] = make([]float64, size)
	}

	for i := range p.Units {
		p.Units[i].Instance.Indicator.Reset()
	}

	return tape
}

func (t *Tape) Push(candle indicator.Candle) {
	t.at = (t.at + 1) % t.size
	t.filled++
	t.bars[t.at] = candle

	for i := range t.plan.Units {
		unit := &t.plan.Units[i]

		feed := candle
		if unit.Feed >= 0 {
			value, known := t.Slot(unit.Feed, 0)
			if !known {
				t.blank(unit)
				continue
			}
			feed = indicator.Candle{
				TS:     candle.TS,
				Open:   value,
				High:   value,
				Low:    value,
				Close:  value,
				Volume: candle.Volume,
			}
		}

		unit.Instance.Indicator.Update(feed)
		if !unit.Instance.Indicator.Ready() {
			t.blank(unit)
			continue
		}

		values := unit.Instance.Indicator.Values()
		for _, slot := range unit.Slots {
			output := t.plan.Slots[slot].Output
			if output >= len(values) {
				t.values[slot][t.at] = math.NaN()
				continue
			}
			t.values[slot][t.at] = values[output]
		}
	}
}

func (t *Tape) Prime(candles []indicator.Candle) {
	for _, candle := range candles {
		t.Push(candle)
	}
}

func (t *Tape) blank(unit *Unit) {
	for _, slot := range unit.Slots {
		t.values[slot][t.at] = math.NaN()
	}
}

func (t *Tape) index(back int) int {
	return ((t.at-back)%t.size + t.size) % t.size
}

func (t *Tape) reachable(back int) bool {
	return back >= 0 && back < t.size && back < t.filled
}

func (t *Tape) Slot(slot, back int) (float64, bool) {
	if slot < 0 || slot >= len(t.values) || !t.reachable(back) {
		return 0, false
	}

	value := t.values[slot][t.index(back)]
	if math.IsNaN(value) {
		return 0, false
	}
	return value, true
}

func (t *Tape) Field(field Field, back int) (float64, bool) {
	if !t.reachable(back) {
		return 0, false
	}
	return field.Value(t.bars[t.index(back)]), true
}

func (t *Tape) Candle() (indicator.Candle, bool) {
	if t.filled == 0 {
		return indicator.Candle{}, false
	}
	return t.bars[t.at], true
}

func (t *Tape) Bars() int { return t.filled }

func (t *Tape) Entry(leg *Leg) bool { return fires(leg.Entry, t) }

func (t *Tape) Exit(leg *Leg) bool { return fires(leg.Exit, t) }

func (t *Tape) Value(name string) (float64, bool) {
	slot, ok := t.plan.Index[name]
	if !ok {
		return 0, false
	}
	return t.Slot(slot, 0)
}

func fires(rule Rule, f Frame) bool {
	if rule == nil {
		return false
	}
	value, known := rule.Eval(f)
	return known && value
}
