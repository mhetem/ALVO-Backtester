package indicator

func init() {
	Register(Spec{
		Name:  "cvol",
		Title: "Chaikin Volatility",
		Group: GroupVolatility,
		Params: []Param{
			{Name: "period", Kind: ParamInt, Default: 10, Min: 1, Max: MaxPeriod},
			{Name: "roc", Kind: ParamInt, Default: 10, Min: 1, Max: MaxPeriod},
		},
		Outputs: []string{"cvol"},
		New:     func(p Params) Indicator { return NewChaikinVolatility(p.Int("period"), p.Int("roc")) },
	})
}

type ChaikinVolatility struct {
	period int
	change int
	spread *ema
	window *ring
	value  [1]float64
}

func NewChaikinVolatility(period, change int) *ChaikinVolatility {
	period, change = max(period, 1), max(change, 1)
	return &ChaikinVolatility{
		period: period,
		change: change,
		spread: newEMA(period),
		window: newRing(change + 1),
	}
}

func (c *ChaikinVolatility) Update(candle Candle) {
	c.spread.push(candle.High - candle.Low)
	if !c.spread.ready {
		return
	}

	c.window.push(c.spread.value)
	if !c.window.full() {
		return
	}

	past := c.window.at(0)
	if past == 0 {
		c.value[0] = 0
		return
	}
	c.value[0] = 100 * (c.spread.value - past) / past
}

func (c *ChaikinVolatility) Values() []float64 { return c.value[:] }

func (c *ChaikinVolatility) Ready() bool { return c.window.full() }

func (c *ChaikinVolatility) Warmup() int { return c.period - 1 + c.change }

func (c *ChaikinVolatility) PrimeBars() int { return c.period*emaPrimeFactor + c.change }

func (c *ChaikinVolatility) Reset() {
	c.spread.reset()
	c.window.reset()
	c.value[0] = 0
}
