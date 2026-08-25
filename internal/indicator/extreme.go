package indicator

type extreme struct {
	size    int
	highest bool
	values  []float64
	ages    []int
	head    int
	count   int
	seen    int
}

func newExtreme(size int, highest bool) *extreme {
	size = max(size, 1)
	return &extreme{
		size:    size,
		highest: highest,
		values:  make([]float64, size),
		ages:    make([]int, size),
	}
}

func (e *extreme) push(value float64) {
	if e.count > 0 && e.ages[e.head] <= e.seen-e.size {
		e.head = (e.head + 1) % e.size
		e.count--
	}

	for e.count > 0 {
		tail := (e.head + e.count - 1) % e.size
		if e.highest && e.values[tail] > value {
			break
		}
		if !e.highest && e.values[tail] < value {
			break
		}
		e.count--
	}

	slot := (e.head + e.count) % e.size
	e.values[slot] = value
	e.ages[slot] = e.seen
	e.count++
	e.seen++
}

func (e *extreme) full() bool { return e.seen >= e.size }

func (e *extreme) value() float64 {
	if e.count == 0 {
		return 0
	}
	return e.values[e.head]
}

func (e *extreme) age() int {
	if e.count == 0 {
		return 0
	}
	return e.seen - 1 - e.ages[e.head]
}

func (e *extreme) reset() {
	clear(e.values)
	clear(e.ages)
	e.head = 0
	e.count = 0
	e.seen = 0
}
