package indicator

import (
	"errors"
	"math"
)

func init() {
	Register(Spec{
		Name:    "psar",
		Title:   "Parabolic SAR",
		Group:   GroupOverlay,
		Overlay: true,
		Params: []Param{
			{Name: "step", Kind: ParamFloat, Default: 0.02, Min: 0.001, Max: 1},
			{Name: "max", Kind: ParamFloat, Default: 0.2, Min: 0.001, Max: 1},
		},
		Outputs: []string{"sar", "direction"},
		Validate: func(p Params) error {
			if p.Float("step") > p.Float("max") {
				return errors.New("step must not exceed max")
			}
			return nil
		},
		New: func(p Params) Indicator { return NewParabolicSAR(p.Float("step"), p.Float("max")) },
	})
}

type ParabolicSAR struct {
	step     float64
	ceiling  float64
	previous Candle
	older    Candle
	bars     int
	sar      float64
	extreme  float64
	rate     float64
	rising   bool
	values   [2]float64
}

func NewParabolicSAR(step, ceiling float64) *ParabolicSAR {
	return &ParabolicSAR{step: step, ceiling: ceiling}
}

func (p *ParabolicSAR) Update(c Candle) {
	switch {
	case p.bars == 0:
		p.previous = c

	case p.bars == 1:
		p.rising = c.Close >= p.previous.Close
		if p.rising {
			p.sar = math.Min(p.previous.Low, c.Low)
			p.extreme = math.Max(p.previous.High, c.High)
		} else {
			p.sar = math.Max(p.previous.High, c.High)
			p.extreme = math.Min(p.previous.Low, c.Low)
		}
		p.rate = p.step
		p.older, p.previous = p.previous, c
		p.emit()

	default:
		p.advance(c)
		p.older, p.previous = p.previous, c
		p.emit()
	}

	p.bars++
}

func (p *ParabolicSAR) advance(c Candle) {
	p.sar += p.rate * (p.extreme - p.sar)

	if p.rising {
		p.sar = math.Min(p.sar, math.Min(p.previous.Low, p.older.Low))
		if c.Low < p.sar {
			p.rising = false
			p.sar = p.extreme
			p.extreme = c.Low
			p.rate = p.step
			return
		}
		if c.High > p.extreme {
			p.extreme = c.High
			p.rate = math.Min(p.rate+p.step, p.ceiling)
		}
		return
	}

	p.sar = math.Max(p.sar, math.Max(p.previous.High, p.older.High))
	if c.High > p.sar {
		p.rising = true
		p.sar = p.extreme
		p.extreme = c.High
		p.rate = p.step
		return
	}
	if c.Low < p.extreme {
		p.extreme = c.Low
		p.rate = math.Min(p.rate+p.step, p.ceiling)
	}
}

func (p *ParabolicSAR) emit() {
	p.values[0] = p.sar
	p.values[1] = 1
	if !p.rising {
		p.values[1] = -1
	}
}

func (p *ParabolicSAR) Values() []float64 { return p.values[:] }

func (p *ParabolicSAR) Ready() bool { return p.bars > 1 }

func (p *ParabolicSAR) Warmup() int { return 1 }

func (p *ParabolicSAR) PrimeBars() int { return pathPrimeBars }

func (p *ParabolicSAR) Reset() {
	p.previous = Candle{}
	p.older = Candle{}
	p.bars = 0
	p.sar = 0
	p.extreme = 0
	p.rate = 0
	p.rising = false
	p.values = [2]float64{}
}
