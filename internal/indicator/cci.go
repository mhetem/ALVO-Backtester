package indicator

import "math"

const cciFactor = 0.015

func init() {
	Register(Spec{
		Name:    "cci",
		Title:   "Commodity Channel Index",
		Group:   GroupMomentum,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 20, Min: 1, Max: MaxPeriod}},
		Outputs: []string{"cci"},
		New:     func(p Params) Indicator { return NewCCI(p.Int("period")) },
	})
}

type CCI struct {
	period int
	window *ring
	value  [1]float64
}

func NewCCI(period int) *CCI {
	period = max(period, 1)
	return &CCI{period: period, window: newRing(period)}
}

func (c *CCI) Update(candle Candle) {
	c.window.push((candle.High + candle.Low + candle.Close) / 3)
	if !c.window.full() {
		return
	}

	mean := c.window.mean()
	spread := 0.0
	for i := range c.period {
		spread += math.Abs(c.window.at(i) - mean)
	}
	spread /= float64(c.period)

	if spread == 0 {
		c.value[0] = 0
		return
	}
	c.value[0] = (c.window.at(c.period-1) - mean) / (cciFactor * spread)
}

func (c *CCI) Values() []float64 { return c.value[:] }

func (c *CCI) Ready() bool { return c.window.full() }

func (c *CCI) Warmup() int { return c.period - 1 }

func (c *CCI) Reset() {
	c.window.reset()
	c.value[0] = 0
}
