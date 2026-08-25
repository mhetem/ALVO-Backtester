package indicator

func init() {
	Register(Spec{
		Name:    "ad",
		Title:   "Accumulation/Distribution Line",
		Group:   GroupVolume,
		Outputs: []string{"ad"},
		New:     func(Params) Indicator { return NewADLine() },
	})
}

type accumulation struct {
	total float64
}

func (a *accumulation) push(c Candle) {
	if c.High > c.Low {
		a.total += ((c.Close-c.Low)-(c.High-c.Close)) / (c.High - c.Low) * c.Volume
	}
}

func (a *accumulation) reset() { a.total = 0 }

type ADLine struct {
	inner accumulation
	ready bool
	value [1]float64
}

func NewADLine() *ADLine { return &ADLine{} }

func (a *ADLine) Update(c Candle) {
	a.inner.push(c)
	a.value[0] = a.inner.total
	a.ready = true
}

func (a *ADLine) Values() []float64 { return a.value[:] }

func (a *ADLine) Ready() bool { return a.ready }

func (a *ADLine) Warmup() int { return 0 }

func (a *ADLine) Anchor() { a.inner.reset() }

func (a *ADLine) Reset() {
	a.inner.reset()
	a.ready = false
	a.value[0] = 0
}
