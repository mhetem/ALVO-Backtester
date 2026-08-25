package indicator

func init() {
	Register(Spec{
		Name:    "mfi",
		Title:   "Money Flow Index",
		Group:   GroupVolume,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 14, Min: 1, Max: MaxPeriod}},
		Outputs: []string{"mfi"},
		New:     func(p Params) Indicator { return NewMFI(p.Int("period")) },
	})
}

type MFI struct {
	period   int
	previous float64
	seen     bool
	positive *ring
	negative *ring
	value    [1]float64
}

func NewMFI(period int) *MFI {
	period = max(period, 1)
	return &MFI{period: period, positive: newRing(period), negative: newRing(period)}
}

func (m *MFI) Update(c Candle) {
	typical := (c.High + c.Low + c.Close) / 3
	if !m.seen {
		m.previous, m.seen = typical, true
		return
	}

	flow := typical * c.Volume
	positive, negative := 0.0, 0.0
	switch {
	case typical > m.previous:
		positive = flow
	case typical < m.previous:
		negative = flow
	}
	m.previous = typical

	m.positive.push(positive)
	m.negative.push(negative)
	if !m.positive.full() {
		return
	}

	if m.negative.sum() == 0 {
		m.value[0] = 100
		return
	}
	m.value[0] = boundedPercent(100 - 100/(1+m.positive.sum()/m.negative.sum()))
}

func (m *MFI) Values() []float64 { return m.value[:] }

func (m *MFI) Ready() bool { return m.positive.full() }

func (m *MFI) Warmup() int { return m.period }

func (m *MFI) Reset() {
	m.previous = 0
	m.seen = false
	m.positive.reset()
	m.negative.reset()
	m.value[0] = 0
}
