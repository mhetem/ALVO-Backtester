package indicator

func init() {
	Register(Spec{
		Name:    "stddev",
		Title:   "Standard Deviation",
		Group:   GroupVolatility,
		Sourced: true,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 20, Min: 2, Max: MaxPeriod}},
		Outputs: []string{"stddev"},
		New:     func(p Params) Indicator { return NewStdDev(p.Int("period"), p.Source()) },
	})
}

type StdDev struct {
	period int
	source Source
	window *window
	value  [1]float64
}

func NewStdDev(period int, source Source) *StdDev {
	period = max(period, 1)
	return &StdDev{period: period, source: source, window: newWindow(period)}
}

func (s *StdDev) Update(c Candle) {
	s.window.push(s.source.Value(c))
	if !s.window.full() {
		return
	}

	s.value[0] = s.window.stddev()
}

func (s *StdDev) Values() []float64 { return s.value[:] }

func (s *StdDev) Ready() bool { return s.window.full() }

func (s *StdDev) Warmup() int { return s.period - 1 }

func (s *StdDev) Reset() {
	s.window.reset()
	s.value[0] = 0
}
