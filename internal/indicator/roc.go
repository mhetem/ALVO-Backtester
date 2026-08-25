package indicator

func init() {
	Register(Spec{
		Name:    "roc",
		Title:   "Rate of Change",
		Group:   GroupMomentum,
		Sourced: true,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 12, Min: 1, Max: MaxPeriod}},
		Outputs: []string{"roc"},
		New:     func(p Params) Indicator { return NewROC(p.Int("period"), p.Source()) },
	})
}

type ROC struct {
	period int
	source Source
	window *ring
	value  [1]float64
}

func NewROC(period int, source Source) *ROC {
	period = max(period, 1)
	return &ROC{period: period, source: source, window: newRing(period + 1)}
}

func (r *ROC) Update(c Candle) {
	price := r.source.Value(c)
	r.window.push(price)
	if !r.window.full() {
		return
	}

	past := r.window.at(0)
	if past == 0 {
		r.value[0] = 0
		return
	}
	r.value[0] = 100 * (price - past) / past
}

func (r *ROC) Values() []float64 { return r.value[:] }

func (r *ROC) Ready() bool { return r.window.full() }

func (r *ROC) Warmup() int { return r.period }

func (r *ROC) Reset() {
	r.window.reset()
	r.value[0] = 0
}
