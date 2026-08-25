package indicator

const (
	MaxPeriod      = 2000
	emaPrimeFactor = 8
)

func init() {
	Register(Spec{
		Name:    "ema",
		Title:   "Exponential Moving Average",
		Group:   GroupOverlay,
		Overlay: true,
		Sourced: true,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 20, Min: 1, Max: MaxPeriod}},
		Outputs: []string{"ema"},
		New:     func(p Params) Indicator { return NewEMA(p.Int("period"), p.Source()) },
	})
}

type ema struct {
	period int
	alpha  float64
	seed   *ring
	value  float64
	ready  bool
}

func newEMA(period int) *ema {
	period = max(period, 1)
	return &ema{period: period, alpha: 2 / float64(period+1), seed: newRing(period)}
}

func (e *ema) push(value float64) {
	if e.ready {
		e.value += e.alpha * (value - e.value)
		return
	}

	e.seed.push(value)
	if e.seed.full() {
		e.value = e.seed.mean()
		e.ready = true
	}
}

func (e *ema) reset() {
	e.seed.reset()
	e.value = 0
	e.ready = false
}

type EMA struct {
	source Source
	inner  *ema
	value  [1]float64
}

func NewEMA(period int, source Source) *EMA {
	return &EMA{source: source, inner: newEMA(period)}
}

func (e *EMA) Update(c Candle) {
	e.inner.push(e.source.Value(c))
	e.value[0] = e.inner.value
}

func (e *EMA) Values() []float64 { return e.value[:] }

func (e *EMA) Ready() bool { return e.inner.ready }

func (e *EMA) Warmup() int { return e.inner.period - 1 }

func (e *EMA) PrimeBars() int { return e.inner.period * emaPrimeFactor }

func (e *EMA) Reset() {
	e.inner.reset()
	e.value[0] = 0
}
