package indicator

func init() {
	Register(Spec{
		Name:    "mom",
		Title:   "Momentum",
		Group:   GroupMomentum,
		Sourced: true,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 10, Min: 1, Max: MaxPeriod}},
		Outputs: []string{"mom"},
		New:     func(p Params) Indicator { return NewMomentum(p.Int("period"), p.Source()) },
	})
}

type Momentum struct {
	period int
	source Source
	window *ring
	value  [1]float64
}

func NewMomentum(period int, source Source) *Momentum {
	period = max(period, 1)
	return &Momentum{period: period, source: source, window: newRing(period + 1)}
}

func (m *Momentum) Update(c Candle) {
	price := m.source.Value(c)
	m.window.push(price)
	if !m.window.full() {
		return
	}

	m.value[0] = price - m.window.at(0)
}

func (m *Momentum) Values() []float64 { return m.value[:] }

func (m *Momentum) Ready() bool { return m.window.full() }

func (m *Momentum) Warmup() int { return m.period }

func (m *Momentum) Reset() {
	m.window.reset()
	m.value[0] = 0
}
