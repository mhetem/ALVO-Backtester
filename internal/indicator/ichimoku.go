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
		Outputs: []string{"tenkan", "kijun", "senkou_a", "senkou_b", "chikou"},
		Offsets: func(p Params) []int {
			ahead := p.Int("displacement")
			return []int{0, 0, ahead, ahead, -ahead}
		},
		New: func(p Params) Indicator {
			return NewIchimoku(p.Int("tenkan"), p.Int("kijun"), p.Int("senkou"))
		},
	})
}

type Ichimoku struct {
	tenkan     int
	kijun      int
	senkou     int
	tenkanHigh *extreme
	tenkanLow  *extreme
	kijunHigh  *extreme
	kijunLow   *extreme
	senkouHigh *extreme
	senkouLow  *extreme
	values     [5]float64
}

func NewIchimoku(tenkan, kijun, senkou int) *Ichimoku {
	tenkan, kijun, senkou = max(tenkan, 1), max(kijun, 1), max(senkou, 1)

	return &Ichimoku{
		tenkan:     tenkan,
		kijun:      kijun,
		senkou:     senkou,
		tenkanHigh: newExtreme(tenkan, true),
		tenkanLow:  newExtreme(tenkan, false),
		kijunHigh:  newExtreme(kijun, true),
		kijunLow:   newExtreme(kijun, false),
		senkouHigh: newExtreme(senkou, true),
		senkouLow:  newExtreme(senkou, false),
	}
}

func (i *Ichimoku) Update(c Candle) {
	i.tenkanHigh.push(c.High)
	i.tenkanLow.push(c.Low)
	i.kijunHigh.push(c.High)
	i.kijunLow.push(c.Low)
	i.senkouHigh.push(c.High)
	i.senkouLow.push(c.Low)

	if !i.Ready() {
		return
	}

	tenkan := (i.tenkanHigh.value() + i.tenkanLow.value()) / 2
	kijun := (i.kijunHigh.value() + i.kijunLow.value()) / 2

	i.values[0] = tenkan
	i.values[1] = kijun
	i.values[2] = (tenkan + kijun) / 2
	i.values[3] = (i.senkouHigh.value() + i.senkouLow.value()) / 2
	i.values[4] = c.Close
}

func (i *Ichimoku) Values() []float64 { return i.values[:] }

func (i *Ichimoku) Ready() bool {
	return i.tenkanHigh.full() && i.kijunHigh.full() && i.senkouHigh.full()
}

func (i *Ichimoku) Warmup() int {
	return max(i.tenkan, i.kijun, i.senkou) - 1
}

func (i *Ichimoku) Reset() {
	i.tenkanHigh.reset()
	i.tenkanLow.reset()
	i.kijunHigh.reset()
	i.kijunLow.reset()
	i.senkouHigh.reset()
	i.senkouLow.reset()
	i.values = [5]float64{}
}
