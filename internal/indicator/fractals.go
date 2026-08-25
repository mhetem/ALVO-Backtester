package indicator

func init() {
	Register(Spec{
		Name:    "fractals",
		Title:   "Williams Fractals",
		Group:   GroupStructure,
		Overlay: true,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 2, Min: 1, Max: 100}},
		Outputs: []string{"up", "down"},
		New:     func(p Params) Indicator { return NewFractals(p.Int("period")) },
	})
}

type Fractals struct {
	period int
	span   int
	highs  *ring
	lows   *ring
	ready  bool
	values [2]float64
}

func NewFractals(period int) *Fractals {
	period = max(period, 1)
	span := 2*period + 1
	return &Fractals{period: period, span: span, highs: newRing(span), lows: newRing(span)}
}

func (f *Fractals) Update(c Candle) {
	f.highs.push(c.High)
	f.lows.push(c.Low)
	if !f.highs.full() {
		return
	}

	if !f.ready {
		f.values[0] = f.highs.at(0)
		f.values[1] = f.lows.at(0)
		for i := 1; i < f.span; i++ {
			f.values[0] = max(f.values[0], f.highs.at(i))
			f.values[1] = min(f.values[1], f.lows.at(i))
		}
		f.ready = true
	}

	if peak, ok := f.pivot(f.highs, true); ok {
		f.values[0] = peak
	}
	if trough, ok := f.pivot(f.lows, false); ok {
		f.values[1] = trough
	}
}

func (f *Fractals) pivot(window *ring, peak bool) (float64, bool) {
	centre := window.at(f.period)

	for i := range f.span {
		if i == f.period {
			continue
		}
		if peak && window.at(i) >= centre {
			return 0, false
		}
		if !peak && window.at(i) <= centre {
			return 0, false
		}
	}

	return centre, true
}

func (f *Fractals) Values() []float64 { return f.values[:] }

func (f *Fractals) Ready() bool { return f.ready }

func (f *Fractals) Warmup() int { return 2 * f.period }

func (f *Fractals) PrimeBars() int { return pathPrimeBars }

func (f *Fractals) Reset() {
	f.highs.reset()
	f.lows.reset()
	f.ready = false
	f.values = [2]float64{}
}
