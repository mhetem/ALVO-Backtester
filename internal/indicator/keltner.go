package indicator

func init() {
	Register(Spec{
		Name:    "keltner",
		Title:   "Keltner Channels",
		Group:   GroupOverlay,
		Overlay: true,
		Params: []Param{
			{Name: "period", Kind: ParamInt, Default: 20, Min: 1, Max: MaxPeriod},
			{Name: "mult", Kind: ParamFloat, Default: 2, Min: 0.1, Max: 10},
			{Name: "atr", Kind: ParamInt, Default: 10, Min: 1, Max: MaxPeriod},
		},
		Outputs: []string{"upper", "middle", "lower"},
		New: func(p Params) Indicator {
			return NewKeltner(p.Int("period"), p.Float("mult"), p.Int("atr"))
		},
	})
}

type Keltner struct {
	period int
	mult   float64
	basis  *ema
	span   *atr
	values [3]float64
}

func NewKeltner(period int, mult float64, atrPeriod int) *Keltner {
	period = max(period, 1)
	return &Keltner{period: period, mult: mult, basis: newEMA(period), span: newATR(atrPeriod)}
}

func (k *Keltner) Update(c Candle) {
	k.basis.push(c.Close)
	k.span.push(c)
	if !k.basis.ready || !k.span.ready() {
		return
	}

	band := k.mult * k.span.value()

	k.values[0] = k.basis.value + band
	k.values[1] = k.basis.value
	k.values[2] = k.basis.value - band
}

func (k *Keltner) Values() []float64 { return k.values[:] }

func (k *Keltner) Ready() bool { return k.basis.ready && k.span.ready() }

func (k *Keltner) Warmup() int { return max(k.period-1, k.span.period) }

func (k *Keltner) PrimeBars() int {
	return max(k.period*emaPrimeFactor, k.span.period*wilderPrimeFactor)
}

func (k *Keltner) Reset() {
	k.basis.reset()
	k.span.reset()
	k.values = [3]float64{}
}
