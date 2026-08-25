package indicator

import "math"

func init() {
	Register(Spec{
		Name:    "adx",
		Title:   "Average Directional Index",
		Group:   GroupMomentum,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 14, Min: 1, Max: MaxPeriod}},
		Outputs: []string{"adx", "plus_di", "minus_di"},
		New:     func(p Params) Indicator { return NewADX(p.Int("period")) },
	})
}

type ADX struct {
	period   int
	previous Candle
	seen     bool
	span     *wilder
	plus     *wilder
	minus    *wilder
	index    *wilder
	values   [3]float64
}

func NewADX(period int) *ADX {
	period = max(period, 1)
	return &ADX{
		period: period,
		span:   newWilder(period),
		plus:   newWilder(period),
		minus:  newWilder(period),
		index:  newWilder(period),
	}
}

func (a *ADX) Update(c Candle) {
	if !a.seen {
		a.previous, a.seen = c, true
		return
	}

	up := c.High - a.previous.High
	down := a.previous.Low - c.Low

	plus, minus := 0.0, 0.0
	if up > down && up > 0 {
		plus = up
	}
	if down > up && down > 0 {
		minus = down
	}

	span := math.Max(c.High-c.Low, math.Max(math.Abs(c.High-a.previous.Close), math.Abs(c.Low-a.previous.Close)))
	a.previous = c

	a.span.push(span)
	a.plus.push(plus)
	a.minus.push(minus)
	if !a.span.ready {
		return
	}

	plusDI, minusDI := 0.0, 0.0
	if a.span.value > 0 {
		plusDI = 100 * a.plus.value / a.span.value
		minusDI = 100 * a.minus.value / a.span.value
	}

	strength := 0.0
	if total := plusDI + minusDI; total > 0 {
		strength = 100 * math.Abs(plusDI-minusDI) / total
	}

	a.index.push(strength)
	if !a.index.ready {
		return
	}

	a.values[0] = a.index.value
	a.values[1] = plusDI
	a.values[2] = minusDI
}

func (a *ADX) Values() []float64 { return a.values[:] }

func (a *ADX) Ready() bool { return a.index.ready }

func (a *ADX) Warmup() int { return 2*a.period - 1 }

func (a *ADX) PrimeBars() int { return 2 * a.period * wilderPrimeFactor }

func (a *ADX) Reset() {
	a.previous = Candle{}
	a.seen = false
	a.span.reset()
	a.plus.reset()
	a.minus.reset()
	a.index.reset()
	a.values = [3]float64{}
}
