package indicator

func init() {
	Register(Spec{
		Name:    "willr",
		Title:   "Williams %R",
		Group:   GroupMomentum,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 14, Min: 1, Max: MaxPeriod}},
		Outputs: []string{"willr"},
		New:     func(p Params) Indicator { return NewWilliamsR(p.Int("period")) },
	})
}

type WilliamsR struct {
	period int
	high   *extreme
	low    *extreme
	value  [1]float64
}

func NewWilliamsR(period int) *WilliamsR {
	period = max(period, 1)
	return &WilliamsR{period: period, high: newExtreme(period, true), low: newExtreme(period, false)}
}

func (w *WilliamsR) Update(c Candle) {
	w.high.push(c.High)
	w.low.push(c.Low)
	if !w.high.full() {
		return
	}

	w.value[0] = stochasticPercent(c.Close, w.high.value(), w.low.value()) - 100
}

func (w *WilliamsR) Values() []float64 { return w.value[:] }

func (w *WilliamsR) Ready() bool { return w.high.full() }

func (w *WilliamsR) Warmup() int { return w.period - 1 }

func (w *WilliamsR) Reset() {
	w.high.reset()
	w.low.reset()
	w.value[0] = 0
}
