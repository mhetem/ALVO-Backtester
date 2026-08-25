package indicator

const wilderPrimeFactor = 16

type wilder struct {
	period int
	sum    float64
	count  int
	value  float64
	ready  bool
}

func newWilder(period int) *wilder {
	return &wilder{period: max(period, 1)}
}

func (w *wilder) push(value float64) {
	if w.ready {
		w.value += (value - w.value) / float64(w.period)
		return
	}

	w.sum += value
	w.count++
	if w.count == w.period {
		w.value = w.sum / float64(w.period)
		w.ready = true
	}
}

func (w *wilder) reset() {
	w.sum = 0
	w.count = 0
	w.value = 0
	w.ready = false
}
