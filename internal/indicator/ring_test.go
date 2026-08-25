package indicator

import (
	"math"
	"testing"
)

func TestRingKeepsARollingSumWithoutReallocating(t *testing.T) {
	r := newRing(3)
	values := []float64{1, 2, 3, 4, 5}
	wants := []float64{1, 3, 6, 9, 12}

	for i, value := range values {
		r.push(value)
		if got := r.mean() * float64(r.len()); math.Abs(got-wants[i]) > 1e-12 {
			t.Errorf("after %v the sum is %v, want %v", values[:i+1], got, wants[i])
		}
		if r.full() != (i >= 2) {
			t.Errorf("full() is %v after %d pushes on a ring of 3", r.full(), i+1)
		}
	}

	if cap(r.values) != 3 {
		t.Errorf("the backing array grew to %d", cap(r.values))
	}

	r.reset()
	if r.len() != 0 || r.mean() != 0 || r.full() {
		t.Error("reset left state behind")
	}
}

func TestWindowStddevMatchesTheNaiveComputation(t *testing.T) {
	values := []float64{31.79, 31.47, 30.99, 31.05, 30.4, 30.88, 33.2, 32.05, 31.6, 31.9,
		30.02, 29.89, 30.15, 31.4, 32.8, 32.1, 31.05, 30.6, 30.44, 31.2}

	const size = 5
	w := newWindow(size)

	for i, value := range values {
		w.push(value)
		if !w.full() {
			continue
		}

		chunk := values[i-size+1 : i+1]
		sum := 0.0
		for _, x := range chunk {
			sum += x
		}
		mean := sum / size

		spread := 0.0
		for _, x := range chunk {
			spread += (x - mean) * (x - mean)
		}
		spread = math.Sqrt(spread / size)

		if math.Abs(w.mean()-mean) > 1e-12 {
			t.Errorf("bar %d: mean is %v, want %v", i, w.mean(), mean)
		}
		if math.Abs(w.stddev()-spread) > 1e-12 {
			t.Errorf("bar %d: stddev is %v, want %v", i, w.stddev(), spread)
		}
	}
}

func TestWindowStddevIsZeroOnAFlatSeries(t *testing.T) {
	w := newWindow(10)
	for range 40 {
		w.push(42)
	}

	if got := w.stddev(); got != 0 {
		t.Errorf("a flat window has stddev %v, want exactly 0", got)
	}
	if got := w.mean(); got != 42 {
		t.Errorf("a flat window has mean %v, want 42", got)
	}
}

func TestWilderSmoothingSeedsWithASimpleAverage(t *testing.T) {
	w := newWilder(3)

	for _, value := range []float64{3, 6, 9} {
		w.push(value)
	}
	if !w.ready {
		t.Fatal("three pushes did not seed a 3-period smoother")
	}
	if w.value != 6 {
		t.Errorf("the seed is %v, want the simple mean 6", w.value)
	}

	w.push(12)
	if want := 6 + (12-6)/3.0; math.Abs(w.value-want) > 1e-12 {
		t.Errorf("after smoothing the value is %v, want %v", w.value, want)
	}
}
