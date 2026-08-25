package indicator

func init() {
	Register(Spec{
		Name:    "aroon",
		Title:   "Aroon",
		Group:   GroupMomentum,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 25, Min: 1, Max: MaxPeriod}},
		Outputs: []string{"up", "down", "oscillator"},
		New:     func(p Params) Indicator { return NewAroon(p.Int("period")) },
	})
}

type Aroon struct {
	period int
	high   *extreme
	low    *extreme
	values [3]float64
}

func NewAroon(period int) *Aroon {
	period = max(period, 1)
	return &Aroon{period: period, high: newExtreme(period+1, true), low: newExtreme(period+1, false)}
}

func (a *Aroon) Update(c Candle) {
	a.high.push(c.High)
	a.low.push(c.Low)
	if !a.high.full() {
		return
	}

	up := 100 * float64(a.period-a.high.age()) / float64(a.period)
	down := 100 * float64(a.period-a.low.age()) / float64(a.period)

	a.values[0] = up
	a.values[1] = down
	a.values[2] = up - down
}

func (a *Aroon) Values() []float64 { return a.values[:] }

func (a *Aroon) Ready() bool { return a.high.full() }

func (a *Aroon) Warmup() int { return a.period }

func (a *Aroon) Reset() {
	a.high.reset()
	a.low.reset()
	a.values = [3]float64{}
}
