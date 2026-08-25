package indicator

func init() {
	Register(Spec{
		Name:    "dema",
		Title:   "Double Exponential Moving Average",
		Group:   GroupOverlay,
		Overlay: true,
		Sourced: true,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 20, Min: 1, Max: MaxPeriod}},
		Outputs: []string{"dema"},
		New:     func(p Params) Indicator { return NewDEMA(p.Int("period"), p.Source()) },
	})
}

type DEMA struct {
	period int
	source Source
	first  *ema
	second *ema
	value  [1]float64
}

func NewDEMA(period int, source Source) *DEMA {
	period = max(period, 1)
	return &DEMA{period: period, source: source, first: newEMA(period), second: newEMA(period)}
}

func (d *DEMA) Update(c Candle) {
	d.first.push(d.source.Value(c))
	if !d.first.ready {
		return
	}

	d.second.push(d.first.value)
	if !d.second.ready {
		return
	}

	d.value[0] = 2*d.first.value - d.second.value
}

func (d *DEMA) Values() []float64 { return d.value[:] }

func (d *DEMA) Ready() bool { return d.second.ready }

func (d *DEMA) Warmup() int { return 2 * (d.period - 1) }

func (d *DEMA) PrimeBars() int { return 2 * d.period * emaPrimeFactor }

func (d *DEMA) Reset() {
	d.first.reset()
	d.second.reset()
	d.value[0] = 0
}
