package indicator

import "math"

func init() {
	Register(Spec{
		Name:    "zigzag",
		Title:   "ZigZag",
		Group:   GroupStructure,
		Overlay: true,
		Params:  []Param{{Name: "deviation", Kind: ParamFloat, Default: 5, Min: 0.1, Max: 100}},
		Outputs: []string{"zigzag", "direction"},
		New:     func(p Params) Indicator { return NewZigZag(p.Float("deviation")) },
	})
}

type ZigZag struct {
	threshold float64
	pivot     float64
	extreme   float64
	direction float64
	started   bool
	values    [2]float64
}

func NewZigZag(deviation float64) *ZigZag {
	return &ZigZag{threshold: deviation / 100}
}

func (z *ZigZag) Update(c Candle) {
	if !z.started {
		z.pivot, z.extreme, z.started = c.Close, c.Close, true
		z.values[0], z.values[1] = z.extreme, z.direction
		return
	}

	switch {
	case z.direction > 0:
		z.extreme = math.Max(z.extreme, c.High)
		if z.moved(z.extreme, c.Low) {
			z.pivot, z.extreme, z.direction = z.extreme, c.Low, -1
		}

	case z.direction < 0:
		z.extreme = math.Min(z.extreme, c.Low)
		if z.moved(z.extreme, c.High) {
			z.pivot, z.extreme, z.direction = z.extreme, c.High, 1
		}

	default:
		switch {
		case c.High > z.pivot && z.moved(z.pivot, c.High):
			z.extreme, z.direction = c.High, 1
		case c.Low < z.pivot && z.moved(z.pivot, c.Low):
			z.extreme, z.direction = c.Low, -1
		}
	}

	z.values[0], z.values[1] = z.extreme, z.direction
}

func (z *ZigZag) moved(from, to float64) bool {
	if from <= 0 {
		return false
	}
	return math.Abs(to-from)/from >= z.threshold
}

func (z *ZigZag) Values() []float64 { return z.values[:] }

func (z *ZigZag) Ready() bool { return z.started }

func (z *ZigZag) Warmup() int { return 0 }

func (z *ZigZag) PrimeBars() int { return pathPrimeBars }

func (z *ZigZag) Reset() {
	z.pivot = 0
	z.extreme = 0
	z.direction = 0
	z.started = false
	z.values = [2]float64{}
}
