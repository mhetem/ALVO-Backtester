package indicator

import "math"

type ring struct {
	values []float64
	next   int
	count  int
	total  float64
}

func newRing(size int) *ring {
	return &ring{values: make([]float64, max(size, 1))}
}

func (r *ring) push(value float64) float64 {
	evicted := r.values[r.next]
	r.values[r.next] = value
	r.next = (r.next + 1) % len(r.values)
	if r.count < len(r.values) {
		r.count++
	}
	r.total += value - evicted
	return evicted
}

func (r *ring) len() int { return r.count }

func (r *ring) at(i int) float64 {
	if i < 0 || i >= r.count {
		return 0
	}
	start := (r.next - r.count + len(r.values)) % len(r.values)
	return r.values[(start+i)%len(r.values)]
}

func (r *ring) sum() float64 { return r.total }

func (r *ring) full() bool { return r.count == len(r.values) }

func (r *ring) mean() float64 {
	if r.count == 0 {
		return 0
	}
	return r.total / float64(r.count)
}

func (r *ring) reset() {
	clear(r.values)
	r.next = 0
	r.count = 0
	r.total = 0
}

type window struct {
	ring  *ring
	sqDev float64
}

func newWindow(size int) *window {
	return &window{ring: newRing(size)}
}

func (w *window) push(value float64) {
	sliding := w.ring.full()
	before := w.ring.mean()
	evicted := w.ring.push(value)
	after := w.ring.mean()

	if sliding {
		w.sqDev += (value - evicted) * (value - after + evicted - before)
		return
	}
	w.sqDev += (value - before) * (value - after)
}

func (w *window) full() bool { return w.ring.full() }

func (w *window) mean() float64 { return w.ring.mean() }

func (w *window) stddev() float64 {
	if w.ring.len() == 0 {
		return 0
	}
	return math.Sqrt(math.Max(w.sqDev, 0) / float64(w.ring.len()))
}

func (w *window) reset() {
	w.ring.reset()
	w.sqDev = 0
}
