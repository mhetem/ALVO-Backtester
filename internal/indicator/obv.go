package indicator

func init() {
	Register(Spec{
		Name:    "obv",
		Title:   "On-Balance Volume",
		Group:   GroupVolume,
		Outputs: []string{"obv"},
		New:     func(Params) Indicator { return NewOBV() },
	})
}

type OBV struct {
	previous float64
	seen     bool
	ready    bool
	total    float64
	value    [1]float64
}

func NewOBV() *OBV { return &OBV{} }

func (o *OBV) Update(c Candle) {
	if o.seen {
		switch {
		case c.Close > o.previous:
			o.total += c.Volume
		case c.Close < o.previous:
			o.total -= c.Volume
		}
		o.value[0] = o.total
		o.ready = true
	}

	o.previous, o.seen = c.Close, true
}

func (o *OBV) Values() []float64 { return o.value[:] }

func (o *OBV) Ready() bool { return o.ready }

func (o *OBV) Warmup() int { return 1 }

func (o *OBV) Anchor() { o.total = 0 }

func (o *OBV) Reset() {
	o.previous = 0
	o.seen = false
	o.ready = false
	o.total = 0
	o.value[0] = 0
}
