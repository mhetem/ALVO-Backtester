package indicator

func init() {
	Register(Spec{
		Name:    "sma",
		Title:   "Simple Moving Average",
		Group:   GroupOverlay,
		Overlay: true,
		Sourced: true,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 20, Min: 1, Max: MaxPeriod}},
		Outputs: []string{"sma"},
		New:     func(p Params) Indicator { return NewSMA(p.Int("period"), p.Source()) },
	})
}

type SMA struct {
	period int
	source Source
	window *ring
	value  [1]float64
}

func NewSMA(period int, source Source) *SMA {
	return &SMA{period: max(period, 1), source: source, window: newRing(period)}
}

func (s *SMA) Update(c Candle) {
	s.window.push(s.source.Value(c))
	if s.window.full() {
		s.value[0] = s.window.mean()
	}
}

func (s *SMA) Values() []float64 { return s.value[:] }

func (s *SMA) Ready() bool { return s.window.full() }

func (s *SMA) Warmup() int { return s.period - 1 }

func (s *SMA) Reset() {
	s.window.reset()
	s.value[0] = 0
}
