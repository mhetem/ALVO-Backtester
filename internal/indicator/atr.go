package indicator

import "math"

func init() {
	Register(Spec{
		Name:    "atr",
		Title:   "Average True Range",
		Group:   GroupVolatility,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 14, Min: 1, Max: MaxPeriod}},
		Outputs: []string{"atr"},
		New:     func(p Params) Indicator { return NewATR(p.Int("period")) },
	})
}

type trueRange struct {
	close float64
	seen  bool
}

func (t *trueRange) push(c Candle) (float64, bool) {
	if !t.seen {
		t.close, t.seen = c.Close, true
		return 0, false
	}

	value := math.Max(c.High-c.Low, math.Max(math.Abs(c.High-t.close), math.Abs(c.Low-t.close)))
	t.close = c.Close

	return value, true
}

func (t *trueRange) reset() {
	t.close = 0
	t.seen = false
}

type atr struct {
	period int
	span   trueRange
	smooth *wilder
}

func newATR(period int) *atr {
	period = max(period, 1)
	return &atr{period: period, smooth: newWilder(period)}
}

func (a *atr) push(c Candle) {
	value, ok := a.span.push(c)
	if !ok {
		return
	}
	a.smooth.push(value)
}

func (a *atr) ready() bool { return a.smooth.ready }

func (a *atr) value() float64 { return a.smooth.value }

func (a *atr) reset() {
	a.span.reset()
	a.smooth.reset()
}

type ATR struct {
	inner *atr
	value [1]float64
}

func NewATR(period int) *ATR {
	return &ATR{inner: newATR(period)}
}

func (a *ATR) Update(c Candle) {
	a.inner.push(c)
	a.value[0] = a.inner.value()
}

func (a *ATR) Values() []float64 { return a.value[:] }

func (a *ATR) Ready() bool { return a.inner.ready() }

func (a *ATR) Warmup() int { return a.inner.period }

func (a *ATR) PrimeBars() int { return a.inner.period * wilderPrimeFactor }

func (a *ATR) Reset() {
	a.inner.reset()
	a.value[0] = 0
}
