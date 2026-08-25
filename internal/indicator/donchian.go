package indicator

func init() {
	Register(Spec{
		Name:    "donchian",
		Title:   "Donchian Channels",
		Group:   GroupOverlay,
		Overlay: true,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 20, Min: 1, Max: MaxPeriod}},
		Outputs: []string{"upper", "middle", "lower"},
		New:     func(p Params) Indicator { return NewDonchian(p.Int("period")) },
	})
}

type Donchian struct {
	period int
	high   *extreme
	low    *extreme
	values [3]float64
}

func NewDonchian(period int) *Donchian {
	period = max(period, 1)
	return &Donchian{period: period, high: newExtreme(period, true), low: newExtreme(period, false)}
}

func (d *Donchian) Update(c Candle) {
	d.high.push(c.High)
	d.low.push(c.Low)
	if !d.high.full() {
		return
	}

	d.values[0] = d.high.value()
	d.values[1] = (d.high.value() + d.low.value()) / 2
	d.values[2] = d.low.value()
}

func (d *Donchian) Values() []float64 { return d.values[:] }

func (d *Donchian) Ready() bool { return d.high.full() }

func (d *Donchian) Warmup() int { return d.period - 1 }

func (d *Donchian) Reset() {
	d.high.reset()
	d.low.reset()
	d.values = [3]float64{}
}
