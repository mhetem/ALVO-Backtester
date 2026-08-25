package indicator

func init() {
	Register(Spec{
		Name:    "bb",
		Title:   "Bollinger Bands",
		Group:   GroupVolatility,
		Overlay: true,
		Sourced: true,
		Params: []Param{
			{Name: "period", Kind: ParamInt, Default: 20, Min: 2, Max: MaxPeriod},
			{Name: "mult", Kind: ParamFloat, Default: 2, Min: 0.1, Max: 10},
		},
		Outputs: []string{"upper", "middle", "lower"},
		New: func(p Params) Indicator {
			return NewBollingerBands(p.Int("period"), p.Float("mult"), p.Source())
		},
	})
}

type BollingerBands struct {
	period int
	mult   float64
	source Source
	window *window
	values [3]float64
}

func NewBollingerBands(period int, mult float64, source Source) *BollingerBands {
	period = max(period, 1)
	return &BollingerBands{period: period, mult: mult, source: source, window: newWindow(period)}
}

func (b *BollingerBands) Update(c Candle) {
	b.window.push(b.source.Value(c))
	if !b.window.full() {
		return
	}

	middle := b.window.mean()
	spread := b.mult * b.window.stddev()

	b.values[0] = middle + spread
	b.values[1] = middle
	b.values[2] = middle - spread
}

func (b *BollingerBands) Values() []float64 { return b.values[:] }

func (b *BollingerBands) Ready() bool { return b.window.full() }

func (b *BollingerBands) Warmup() int { return b.period - 1 }

func (b *BollingerBands) Reset() {
	b.window.reset()
	b.values = [3]float64{}
}
