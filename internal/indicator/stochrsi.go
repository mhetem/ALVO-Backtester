package indicator

func init() {
	Register(Spec{
		Name:    "stochrsi",
		Title:   "Stochastic RSI",
		Group:   GroupMomentum,
		Sourced: true,
		Params: []Param{
			{Name: "rsi", Kind: ParamInt, Default: 14, Min: 2, Max: MaxPeriod},
			{Name: "stoch", Kind: ParamInt, Default: 14, Min: 1, Max: MaxPeriod},
			{Name: "k", Kind: ParamInt, Default: 3, Min: 1, Max: MaxPeriod},
			{Name: "d", Kind: ParamInt, Default: 3, Min: 1, Max: MaxPeriod},
		},
		Outputs: []string{"k", "d"},
		New: func(p Params) Indicator {
			return NewStochRSI(p.Int("rsi"), p.Int("stoch"), p.Int("k"), p.Int("d"), p.Source())
		},
	})
}

type StochRSI struct {
	stoch  int
	k      int
	d      int
	rsi    *RSI
	high   *extreme
	low    *extreme
	fast   *ring
	slow   *ring
	values [2]float64
}

func NewStochRSI(period, stoch, k, d int, source Source) *StochRSI {
	stoch, k, d = max(stoch, 1), max(k, 1), max(d, 1)
	return &StochRSI{
		stoch: stoch,
		k:     k,
		d:     d,
		rsi:   NewRSI(period, source),
		high:  newExtreme(stoch, true),
		low:   newExtreme(stoch, false),
		fast:  newRing(k),
		slow:  newRing(d),
	}
}

func (s *StochRSI) Update(c Candle) {
	s.rsi.Update(c)
	if !s.rsi.Ready() {
		return
	}

	strength := s.rsi.Values()[0]
	s.high.push(strength)
	s.low.push(strength)
	if !s.high.full() {
		return
	}

	s.fast.push(stochasticPercent(strength, s.high.value(), s.low.value()))
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

func (s *StochRSI) Values() []float64 { return s.values[:] }

func (s *StochRSI) Ready() bool { return s.slow.full() }

func (s *StochRSI) Warmup() int {
	return s.rsi.Warmup() + s.stoch + s.k + s.d - 3
}

func (s *StochRSI) PrimeBars() int {
	return s.rsi.PrimeBars() + s.stoch + s.k + s.d
}

func (s *StochRSI) Reset() {
	s.rsi.Reset()
	s.high.reset()
	s.low.reset()
	s.fast.reset()
	s.slow.reset()
	s.values = [2]float64{}
}
