package indicator

import "errors"

func init() {
	Register(Spec{
		Name:    "macd",
		Title:   "MACD",
		Group:   GroupMomentum,
		Sourced: true,
		Params: []Param{
			{Name: "fast", Kind: ParamInt, Default: 12, Min: 1, Max: MaxPeriod},
			{Name: "slow", Kind: ParamInt, Default: 26, Min: 2, Max: MaxPeriod},
			{Name: "signal", Kind: ParamInt, Default: 9, Min: 1, Max: MaxPeriod},
		},
		Outputs: []string{"macd", "signal", "histogram"},
		Validate: func(p Params) error {
			if p.Int("fast") >= p.Int("slow") {
				return errors.New("fast must be shorter than slow")
			}
			return nil
		},
		New: func(p Params) Indicator {
			return NewMACD(p.Int("fast"), p.Int("slow"), p.Int("signal"), p.Source())
		},
	})
}

type MACD struct {
	source Source
	fast   *ema
	slow   *ema
	signal *ema
	values [3]float64
}

func NewMACD(fast, slow, signal int, source Source) *MACD {
	return &MACD{
		source: source,
		fast:   newEMA(fast),
		slow:   newEMA(slow),
		signal: newEMA(signal),
	}
}

func (m *MACD) Update(c Candle) {
	price := m.source.Value(c)
	m.fast.push(price)
	m.slow.push(price)
	if !m.slow.ready {
		return
	}

	line := m.fast.value - m.slow.value
	m.signal.push(line)
	if !m.signal.ready {
		return
	}

	m.values[0] = line
	m.values[1] = m.signal.value
	m.values[2] = line - m.signal.value
}

func (m *MACD) Values() []float64 { return m.values[:] }

func (m *MACD) Ready() bool { return m.signal.ready }

func (m *MACD) Warmup() int { return m.slow.period - 1 + m.signal.period - 1 }

func (m *MACD) PrimeBars() int { return (m.slow.period + m.signal.period) * emaPrimeFactor }

func (m *MACD) Reset() {
	m.fast.reset()
	m.slow.reset()
	m.signal.reset()
	m.values = [3]float64{}
}
