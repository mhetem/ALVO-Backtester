package indicator

func init() {
	Register(Spec{
		Name:    "ichimoku",
		Title:   "Ichimoku Cloud",
		Group:   GroupOverlay,
		Overlay: true,
		Params: []Param{
			{Name: "tenkan", Kind: ParamInt, Default: 9, Min: 1, Max: MaxPeriod},
			{Name: "kijun", Kind: ParamInt, Default: 26, Min: 1, Max: MaxPeriod},
			{Name: "senkou", Kind: ParamInt, Default: 52, Min: 1, Max: MaxPeriod},
			{Name: "displacement", Kind: ParamInt, Default: 26, Min: 0, Max: MaxPeriod},
		},
		Outputs: []string{"tenkan", "kijun", "senkou_a", "senkou_b"},
		New: func(p Params) Indicator {
			return NewIchimoku(p.Int("tenkan"), p.Int("kijun"), p.Int("senkou"), p.Int("displacement"))
		},
	})
}

type Ichimoku struct {
	tenkan       int
	kijun        int
	senkou       int
	displacement int
	tenkanHigh   *extreme
	tenkanLow    *extreme
	kijunHigh    *extreme
	kijunLow     *extreme
	senkouHigh   *extreme
	senkouLow    *extreme
	leadingA     *ring
	leadingB     *ring
	values       [4]float64
}

func NewIchimoku(tenkan, kijun, senkou, displacement int) *Ichimoku {
	tenkan, kijun, senkou = max(tenkan, 1), max(kijun, 1), max(senkou, 1)
	displacement = max(displacement, 0)

	return &Ichimoku{
		tenkan:       tenkan,
		kijun:        kijun,
		senkou:       senkou,
		displacement: displacement,
		tenkanHigh:   newExtreme(tenkan, true),
		tenkanLow:    newExtreme(tenkan, false),
		kijunHigh:    newExtreme(kijun, true),
		kijunLow:     newExtreme(kijun, false),
		senkouHigh:   newExtreme(senkou, true),
		senkouLow:    newExtreme(senkou, false),
		leadingA:     newRing(displacement + 1),
		leadingB:     newRing(displacement + 1),
	}
}

func (i *Ichimoku) Update(c Candle) {
	i.tenkanHigh.push(c.High)
	i.tenkanLow.push(c.Low)
	i.kijunHigh.push(c.High)
	i.kijunLow.push(c.Low)
	i.senkouHigh.push(c.High)
	i.senkouLow.push(c.Low)

	tenkan := (i.tenkanHigh.value() + i.tenkanLow.value()) / 2
	kijun := (i.kijunHigh.value() + i.kijunLow.value()) / 2

	if i.tenkanHigh.full() && i.kijunHigh.full() {
		i.leadingA.push((tenkan + kijun) / 2)
	}
	if i.senkouHigh.full() {
		i.leadingB.push((i.senkouHigh.value() + i.senkouLow.value()) / 2)
	}
	if !i.leadingA.full() || !i.leadingB.full() {
		return
	}

	i.values[0] = tenkan
	i.values[1] = kijun
	i.values[2] = i.leadingA.at(0)
	i.values[3] = i.leadingB.at(0)
}

func (i *Ichimoku) Values() []float64 { return i.values[:] }

func (i *Ichimoku) Ready() bool { return i.leadingA.full() && i.leadingB.full() }

func (i *Ichimoku) Warmup() int {
	return max(i.tenkan, i.kijun, i.senkou) - 1 + i.displacement
}

func (i *Ichimoku) Reset() {
	i.tenkanHigh.reset()
	i.tenkanLow.reset()
	i.kijunHigh.reset()
	i.kijunLow.reset()
	i.senkouHigh.reset()
	i.senkouLow.reset()
	i.leadingA.reset()
	i.leadingB.reset()
	i.values = [4]float64{}
}
