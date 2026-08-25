package indicator

import "math"

func init() {
	Register(Spec{
		Name:    "hma",
		Title:   "Hull Moving Average",
		Group:   GroupOverlay,
		Overlay: true,
		Sourced: true,
		Params:  []Param{{Name: "period", Kind: ParamInt, Default: 9, Min: 1, Max: MaxPeriod}},
		Outputs: []string{"hma"},
		New:     func(p Params) Indicator { return NewHMA(p.Int("period"), p.Source()) },
	})
}

type HMA struct {
	period int
	root   int
	source Source
	half   *wma
	full   *wma
	hull   *wma
	value  [1]float64
}

func NewHMA(period int, source Source) *HMA {
	period = max(period, 1)
	root := max(int(math.Round(math.Sqrt(float64(period)))), 1)

	return &HMA{
		period: period,
		root:   root,
		source: source,
		half:   newWMA(max(period/2, 1)),
		full:   newWMA(period),
		hull:   newWMA(root),
	}
}

func (h *HMA) Update(c Candle) {
	price := h.source.Value(c)
	h.half.push(price)
	h.full.push(price)
	if !h.full.ready {
		return
	}

	h.hull.push(2*h.half.value - h.full.value)
	h.value[0] = h.hull.value
}

func (h *HMA) Values() []float64 { return h.value[:] }

func (h *HMA) Ready() bool { return h.hull.ready }

func (h *HMA) Warmup() int { return h.period + h.root - 2 }

func (h *HMA) Reset() {
	h.half.reset()
	h.full.reset()
	h.hull.reset()
	h.value[0] = 0
}
