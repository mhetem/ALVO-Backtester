package indicator

func init() {
	Register(Spec{
		Name:  "stoch",
		Title: "Stochastic Oscillator",
		Group: GroupMomentum,
		Params: []Param{
			{Name: "k", Kind: ParamInt, Default: 14, Min: 1, Max: MaxPeriod},
			{Name: "smooth", Kind: ParamInt, Default: 3, Min: 1, Max: MaxPeriod},
			{Name: "d", Kind: ParamInt, Default: 3, Min: 1, Max: MaxPeriod},
		},
		Outputs: []string{"k", "d"},
		New: func(p Params) Indicator {
			return NewStochastic(p.Int("k"), p.Int("smooth"), p.Int("d"))
		},
	})
}

const neutralStochastic = 50.0

type Stochastic struct {
	k      int
	smooth int
	d      int
	high   *extreme
	low    *extreme
	fast   *ring
	slow   *ring
	values [2]float64
}

func NewStochastic(k, smooth, d int) *Stochastic {
	k, smooth, d = max(k, 1), max(smooth, 1), max(d, 1)
	return &Stochastic{
		k:      k,
		smooth: smooth,
		d:      d,
		high:   newExtreme(k, true),
		low:    newExtreme(k, false),
		fast:   newRing(smooth),
		slow:   newRing(d),
	}
}

func (s *Stochastic) Update(c Candle) {
	s.high.push(c.High)
	s.low.push(c.Low)
	if !s.high.full() {
		return
	}

	s.fast.push(stochasticPercent(c.Close, s.high.value(), s.low.value()))
	if !s.fast.full() {
		return
	}

	smoothed := boundedPercent(s.fast.mean())
	s.slow.push(smoothed)
	if !s.slow.full() {
		return
	}

	s.values[0] = smoothed
	s.values[1] = boundedPercent(s.slow.mean())
}

func stochasticPercent(price, top, bottom float64) float64 {
	if top <= bottom {
		return neutralStochastic
	}
	return 100 * (price - bottom) / (top - bottom)
}

func boundedPercent(value float64) float64 { return min(max(value, 0), 100) }

func (s *Stochastic) Values() []float64 { return s.values[:] }

func (s *Stochastic) Ready() bool { return s.slow.full() }

func (s *Stochastic) Warmup() int { return s.k + s.smooth + s.d - 3 }

func (s *Stochastic) Reset() {
	s.high.reset()
	s.low.reset()
	s.fast.reset()
	s.slow.reset()
	s.values = [2]float64{}
}
