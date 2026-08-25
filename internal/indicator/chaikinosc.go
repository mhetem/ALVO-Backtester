package indicator

import "errors"

func init() {
	Register(Spec{
		Name:  "chaikin",
		Title: "Chaikin Oscillator",
		Group: GroupVolume,
		Params: []Param{
			{Name: "fast", Kind: ParamInt, Default: 3, Min: 1, Max: MaxPeriod},
			{Name: "slow", Kind: ParamInt, Default: 10, Min: 2, Max: MaxPeriod},
		},
		Outputs: []string{"chaikin"},
		Validate: func(p Params) error {
			if p.Int("fast") >= p.Int("slow") {
				return errors.New("fast must be shorter than slow")
			}
			return nil
		},
		New: func(p Params) Indicator { return NewChaikinOscillator(p.Int("fast"), p.Int("slow")) },
	})
}

type ChaikinOscillator struct {
	inner accumulation
	fast  *ema
	slow  *ema
	value [1]float64
}

func NewChaikinOscillator(fast, slow int) *ChaikinOscillator {
	return &ChaikinOscillator{fast: newEMA(fast), slow: newEMA(slow)}
}

func (c *ChaikinOscillator) Update(candle Candle) {
	c.inner.push(candle)
	c.fast.push(c.inner.total)
	c.slow.push(c.inner.total)
	if !c.slow.ready {
		return
	}

	c.value[0] = c.fast.value - c.slow.value
}

func (c *ChaikinOscillator) Values() []float64 { return c.value[:] }

func (c *ChaikinOscillator) Ready() bool { return c.slow.ready }

func (c *ChaikinOscillator) Warmup() int { return c.slow.period - 1 }

func (c *ChaikinOscillator) PrimeBars() int { return c.slow.period * emaPrimeFactor }

func (c *ChaikinOscillator) Reset() {
	c.inner.reset()
	c.fast.reset()
	c.slow.reset()
	c.value[0] = 0
}
