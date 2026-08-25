package indicator

func init() {
	Register(Spec{
		Name:    "tema",
		Title:   "Triple Exponential Moving Average",
		Group:   GroupOverlay,
		Overlay: true,
		Sourced: true,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 20, Min: 1, Max: MaxPeriod}},
		Outputs: []string{"tema"},
		New:     func(p Params) Indicator { return NewTEMA(p.Int("period"), p.Source()) },
	})
}

type TEMA struct {
	period int
	source Source
	first  *ema
	second *ema
	third  *ema
	value  [1]float64
}

func NewTEMA(period int, source Source) *TEMA {
	period = max(period, 1)
	return &TEMA{
		period: period,
		source: source,
		first:  newEMA(period),
		second: newEMA(period),
		third:  newEMA(period),
	}
}

func (t *TEMA) Update(c Candle) {
	t.first.push(t.source.Value(c))
	if !t.first.ready {
		return
	}

	t.second.push(t.first.value)
	if !t.second.ready {
		return
	}

	t.third.push(t.second.value)
	if !t.third.ready {
		return
	}

	t.value[0] = 3*t.first.value - 3*t.second.value + t.third.value
}

func (t *TEMA) Values() []float64 { return t.value[:] }

func (t *TEMA) Ready() bool { return t.third.ready }

func (t *TEMA) Warmup() int { return 3 * (t.period - 1) }

func (t *TEMA) PrimeBars() int { return 3 * t.period * emaPrimeFactor }

func (t *TEMA) Reset() {
	t.first.reset()
	t.second.reset()
	t.third.reset()
	t.value[0] = 0
}
