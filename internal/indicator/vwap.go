package indicator

func init() {
	Register(Spec{
		Name:    "vwap",
		Title:   "Rolling VWAP",
		Group:   GroupOverlay,
		Overlay: true,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 20, Min: 1, Max: MaxPeriod}},
		Outputs: []string{"vwap"},
		New:     func(p Params) Indicator { return NewVWAP(p.Int("period")) },
	})
}

type VWAP struct {
	period   int
	typical  *ring
	weighted *ring
	volume   *ring
	value    [1]float64
}

func NewVWAP(period int) *VWAP {
	period = max(period, 1)
	return &VWAP{
		period:   period,
		typical:  newRing(period),
		weighted: newRing(period),
		volume:   newRing(period),
	}
}

func (v *VWAP) Update(c Candle) {
	typical := (c.High + c.Low + c.Close) / 3
	v.typical.push(typical)
	v.weighted.push(typical * c.Volume)
	v.volume.push(c.Volume)
	if !v.typical.full() {
		return
	}

	if v.volume.sum() <= 0 {
		v.value[0] = v.typical.mean()
		return
	}
	v.value[0] = v.weighted.sum() / v.volume.sum()
}

func (v *VWAP) Values() []float64 { return v.value[:] }

func (v *VWAP) Ready() bool { return v.typical.full() }

func (v *VWAP) Warmup() int { return v.period - 1 }

func (v *VWAP) Reset() {
	v.typical.reset()
	v.weighted.reset()
	v.volume.reset()
	v.value[0] = 0
}
