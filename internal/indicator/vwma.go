package indicator

func init() {
	Register(Spec{
		Name:    "vwma",
		Title:   "Volume Weighted Moving Average",
		Group:   GroupVolume,
		Overlay: true,
		Sourced: true,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 20, Min: 1, Max: MaxPeriod}},
		Outputs: []string{"vwma"},
		New:     func(p Params) Indicator { return NewVWMA(p.Int("period"), p.Source()) },
	})
}

type VWMA struct {
	period   int
	source   Source
	price    *ring
	weighted *ring
	volume   *ring
	value    [1]float64
}

func NewVWMA(period int, source Source) *VWMA {
	period = max(period, 1)
	return &VWMA{
		period:   period,
		source:   source,
		price:    newRing(period),
		weighted: newRing(period),
		volume:   newRing(period),
	}
}

func (v *VWMA) Update(c Candle) {
	price := v.source.Value(c)
	v.price.push(price)
	v.weighted.push(price * c.Volume)
	v.volume.push(c.Volume)
	if !v.price.full() {
		return
	}

	if v.volume.sum() <= 0 {
		v.value[0] = v.price.mean()
		return
	}
	v.value[0] = v.weighted.sum() / v.volume.sum()
}

func (v *VWMA) Values() []float64 { return v.value[:] }

func (v *VWMA) Ready() bool { return v.price.full() }

func (v *VWMA) Warmup() int { return v.period - 1 }

func (v *VWMA) Reset() {
	v.price.reset()
	v.weighted.reset()
	v.volume.reset()
	v.value[0] = 0
}
