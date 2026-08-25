package indicator

func init() {
	Register(Spec{
		Name:    "rsi",
		Title:   "Relative Strength Index",
		Group:   GroupMomentum,
		Sourced: true,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 14, Min: 2, Max: MaxPeriod}},
		Outputs: []string{"rsi"},
		New:     func(p Params) Indicator { return NewRSI(p.Int("period"), p.Source()) },
	})
}

type RSI struct {
	period int
	source Source
	gains  *wilder
	losses *wilder
	prev   float64
	seen   bool
	value  [1]float64
}

func NewRSI(period int, source Source) *RSI {
	period = max(period, 1)
	return &RSI{period: period, source: source, gains: newWilder(period), losses: newWilder(period)}
}

func (r *RSI) Update(c Candle) {
	price := r.source.Value(c)
	if !r.seen {
		r.prev, r.seen = price, true
		return
	}

	change := price - r.prev
	r.prev = price

	gain, loss := 0.0, 0.0
	if change > 0 {
		gain = change
	} else {
		loss = -change
	}

	r.gains.push(gain)
	r.losses.push(loss)
	if !r.gains.ready {
		return
	}

	if r.losses.value == 0 {
		r.value[0] = 100
		return
	}
	r.value[0] = 100 - 100/(1+r.gains.value/r.losses.value)
}

func (r *RSI) Values() []float64 { return r.value[:] }

func (r *RSI) Ready() bool { return r.gains.ready }

func (r *RSI) Warmup() int { return r.period }

func (r *RSI) PrimeBars() int { return r.period * wilderPrimeFactor }

func (r *RSI) Reset() {
	r.gains.reset()
	r.losses.reset()
	r.prev = 0
	r.seen = false
	r.value[0] = 0
}
