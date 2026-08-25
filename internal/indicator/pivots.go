package indicator

const (
	fibFirst  = 0.382
	fibSecond = 0.618
	fibThird  = 1.0
)

func init() {
	Register(Spec{
		Name:    "pivots",
		Title:   "Pivot Points",
		Group:   GroupStructure,
		Overlay: true,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 1, Min: 1, Max: MaxPeriod}},
		Outputs: pivotOutputs,
		New:     func(p Params) Indicator { return NewPivotPoints(p.Int("period"), false) },
	})

	Register(Spec{
		Name:    "fibpivots",
		Title:   "Fibonacci Pivot Points",
		Group:   GroupStructure,
		Overlay: true,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 1, Min: 1, Max: MaxPeriod}},
		Outputs: pivotOutputs,
		New:     func(p Params) Indicator { return NewPivotPoints(p.Int("period"), true) },
	})
}

var pivotOutputs = []string{"pivot", "r1", "r2", "r3", "s1", "s2", "s3"}

type PivotPoints struct {
	period int
	fib    bool
	high   *extreme
	low    *extreme
	closes *ring
	ready  bool
	values [7]float64
}

func NewPivotPoints(period int, fib bool) *PivotPoints {
	period = max(period, 1)
	return &PivotPoints{
		period: period,
		fib:    fib,
		high:   newExtreme(period, true),
		low:    newExtreme(period, false),
		closes: newRing(period),
	}
}

func (p *PivotPoints) Update(c Candle) {
	if p.high.full() {
		p.compute(p.high.value(), p.low.value(), p.closes.at(p.period-1))
		p.ready = true
	}

	p.high.push(c.High)
	p.low.push(c.Low)
	p.closes.push(c.Close)
}

func (p *PivotPoints) compute(high, low, last float64) {
	pivot := (high + low + last) / 3
	span := high - low

	p.values[0] = pivot

	if p.fib {
		p.values[1] = pivot + fibFirst*span
		p.values[2] = pivot + fibSecond*span
		p.values[3] = pivot + fibThird*span
		p.values[4] = pivot - fibFirst*span
		p.values[5] = pivot - fibSecond*span
		p.values[6] = pivot - fibThird*span
		return
	}

	p.values[1] = 2*pivot - low
	p.values[2] = pivot + span
	p.values[3] = high + 2*(pivot-low)
	p.values[4] = 2*pivot - high
	p.values[5] = pivot - span
	p.values[6] = low - 2*(high-pivot)
}

func (p *PivotPoints) Values() []float64 { return p.values[:] }

func (p *PivotPoints) Ready() bool { return p.ready }

func (p *PivotPoints) Warmup() int { return p.period }

func (p *PivotPoints) Reset() {
	p.high.reset()
	p.low.reset()
	p.closes.reset()
	p.ready = false
	p.values = [7]float64{}
}
