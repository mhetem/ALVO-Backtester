package indicator

func init() {
	Register(Spec{
		Name:    "supertrend",
		Title:   "SuperTrend",
		Group:   GroupOverlay,
		Overlay: true,
		Params: []Param{
			{Name: "period", Kind: ParamInt, Default: 10, Min: 1, Max: MaxPeriod},
			{Name: "mult", Kind: ParamFloat, Default: 3, Min: 0.1, Max: 20},
		},
		Outputs: []string{"supertrend", "direction"},
		New:     func(p Params) Indicator { return NewSuperTrend(p.Int("period"), p.Float("mult")) },
	})
}

type SuperTrend struct {
	mult    float64
	span    *atr
	upper   float64
	lower   float64
	close   float64
	rising  bool
	started bool
	values  [2]float64
}

func NewSuperTrend(period int, mult float64) *SuperTrend {
	return &SuperTrend{mult: mult, span: newATR(period)}
}

func (s *SuperTrend) Update(c Candle) {
	s.span.push(c)
	if !s.span.ready() {
		return
	}

	middle := (c.High + c.Low) / 2
	band := s.mult * s.span.value()
	upper := middle + band
	lower := middle - band

	if !s.started {
		s.upper, s.lower = upper, lower
		s.rising = c.Close > upper
		s.started = true
	} else {
		if upper < s.upper || s.close > s.upper {
			s.upper = upper
		}
		if lower > s.lower || s.close < s.lower {
			s.lower = lower
		}
		switch {
		case s.rising && c.Close < s.lower:
			s.rising = false
		case !s.rising && c.Close > s.upper:
			s.rising = true
		}
	}

	s.close = c.Close

	if s.rising {
		s.values[0], s.values[1] = s.lower, 1
		return
	}
	s.values[0], s.values[1] = s.upper, -1
}

func (s *SuperTrend) Values() []float64 { return s.values[:] }

func (s *SuperTrend) Ready() bool { return s.span.ready() }

func (s *SuperTrend) Warmup() int { return s.span.period }

func (s *SuperTrend) PrimeBars() int { return max(s.span.period*wilderPrimeFactor, pathPrimeBars) }

func (s *SuperTrend) Reset() {
	s.span.reset()
	s.upper = 0
	s.lower = 0
	s.close = 0
	s.rising = false
	s.started = false
	s.values = [2]float64{}
}
