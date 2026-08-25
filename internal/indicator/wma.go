package indicator

func init() {
	Register(Spec{
		Name:    "wma",
		Title:   "Weighted Moving Average",
		Group:   GroupOverlay,
		Overlay: true,
		Sourced: true,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 20, Min: 1, Max: MaxPeriod}},
		Outputs: []string{"wma"},
		New:     func(p Params) Indicator { return NewWMA(p.Int("period"), p.Source()) },
	})
}

type wma struct {
	period    int
	weight    float64
	window    *ring
	numerator float64
	seeded    bool
	value     float64
	ready     bool
}

func newWMA(period int) *wma {
	period = max(period, 1)
	return &wma{
		period: period,
		weight: float64(period*(period+1)) / 2,
		window: newRing(period),
	}
}

func (w *wma) push(value float64) {
	previous := w.window.sum()
	w.window.push(value)
	if !w.window.full() {
		return
	}

	if w.seeded {
		w.numerator += float64(w.period)*value - previous
	} else {
		w.numerator = 0
		for i := range w.period {
			w.numerator += float64(i+1) * w.window.at(i)
		}
		w.seeded = true
	}

	w.value = w.numerator / w.weight
	w.ready = true
}

func (w *wma) reset() {
	w.window.reset()
	w.numerator = 0
	w.seeded = false
	w.value = 0
	w.ready = false
}

type WMA struct {
	source Source
	inner  *wma
	value  [1]float64
}

func NewWMA(period int, source Source) *WMA {
	return &WMA{source: source, inner: newWMA(period)}
}

func (w *WMA) Update(c Candle) {
	w.inner.push(w.source.Value(c))
	w.value[0] = w.inner.value
}

func (w *WMA) Values() []float64 { return w.value[:] }

func (w *WMA) Ready() bool { return w.inner.ready }

func (w *WMA) Warmup() int { return w.inner.period - 1 }

func (w *WMA) Reset() {
	w.inner.reset()
	w.value[0] = 0
}
