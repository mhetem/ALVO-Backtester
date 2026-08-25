package indicator

func init() {
	Register(Spec{
		Name:    "volma",
		Title:   "Volume Moving Average",
		Group:   GroupVolume,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 20, Min: 1, Max: MaxPeriod}},
		Outputs: []string{"volma"},
		New:     func(p Params) Indicator { return NewVolumeMA(p.Int("period")) },
	})
}

type VolumeMA struct {
	period int
	window *ring
	value  [1]float64
}

func NewVolumeMA(period int) *VolumeMA {
	period = max(period, 1)
	return &VolumeMA{period: period, window: newRing(period)}
}

func (v *VolumeMA) Update(c Candle) {
	v.window.push(c.Volume)
	if !v.window.full() {
		return
	}

	v.value[0] = v.window.mean()
}

func (v *VolumeMA) Values() []float64 { return v.value[:] }

func (v *VolumeMA) Ready() bool { return v.window.full() }

func (v *VolumeMA) Warmup() int { return v.period - 1 }

func (v *VolumeMA) Reset() {
	v.window.reset()
	v.value[0] = 0
}
