package indicator

import "math"

func init() {
	Register(Spec{
		Name:    "hv",
		Title:   "Historical Volatility",
		Group:   GroupVolatility,
		Sourced: true,
		Params: []Param{
			{Name: "period", Kind: ParamInt, Default: 20, Min: 2, Max: MaxPeriod},
			{Name: "annual", Kind: ParamInt, Default: 252, Min: 1, Max: MaxPeriod},
		},
		Outputs: []string{"hv"},
		New: func(p Params) Indicator {
			return NewHistoricalVolatility(p.Int("period"), p.Int("annual"), p.Source())
		},
	})
}

type HistoricalVolatility struct {
	period   int
	annual   int
	source   Source
	previous float64
	seen     bool
	window   *window
	value    [1]float64
}

func NewHistoricalVolatility(period, annual int, source Source) *HistoricalVolatility {
	period = max(period, 1)
	return &HistoricalVolatility{
		period: period,
		annual: max(annual, 1),
		source: source,
		window: newWindow(period),
	}
}

func (h *HistoricalVolatility) Update(c Candle) {
	price := h.source.Value(c)
	if !h.seen {
		h.previous, h.seen = price, true
		return
	}

	change := 0.0
	if price > 0 && h.previous > 0 {
		change = math.Log(price / h.previous)
	}
	h.previous = price

	h.window.push(change)
	if !h.window.full() {
		return
	}

	h.value[0] = 100 * h.window.stddev() * math.Sqrt(float64(h.annual))
}

func (h *HistoricalVolatility) Values() []float64 { return h.value[:] }

func (h *HistoricalVolatility) Ready() bool { return h.window.full() }

func (h *HistoricalVolatility) Warmup() int { return h.period }

func (h *HistoricalVolatility) Reset() {
	h.previous = 0
	h.seen = false
	h.window.reset()
	h.value[0] = 0
}
