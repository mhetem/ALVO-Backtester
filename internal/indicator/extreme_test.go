package indicator

import "testing"

func TestTheRollingExtremeMatchesAScanOfTheWindow(t *testing.T) {
	candles := syntheticWalk(500)

	for _, size := range []int{1, 2, 7, 60} {
		highest := newExtreme(size, true)
		lowest := newExtreme(size, false)

		for i, candle := range candles {
			highest.push(candle.High)
			lowest.push(candle.Low)

			if want := i+1 >= size; highest.full() != want {
				t.Fatalf("size %d: full() is %v at bar %d", size, highest.full(), i)
			}
			if !highest.full() {
				continue
			}

			window := candles[i-size+1 : i+1]
			top, bottom, at := window[0].High, window[0].Low, 0
			for j, bar := range window {
				if bar.High >= top {
					top, at = bar.High, j
				}
				bottom = min(bottom, bar.Low)
			}

			if highest.value() != top {
				t.Fatalf("size %d bar %d: highest is %v, want %v", size, i, highest.value(), top)
			}
			if lowest.value() != bottom {
				t.Fatalf("size %d bar %d: lowest is %v, want %v", size, i, lowest.value(), bottom)
			}
			if want := size - 1 - at; highest.age() != want {
				t.Fatalf("size %d bar %d: the high is %d bars back, want %d", size, i, highest.age(), want)
			}
		}
	}
}

func TestTheRollingExtremeSurvivesAMonotoneRun(t *testing.T) {
	const size = 4

	highest := newExtreme(size, true)
	for i := range 20 {
		highest.push(float64(20 - i))
		if !highest.full() {
			continue
		}
		if want := float64(20 - i + size - 1); highest.value() != want {
			t.Fatalf("bar %d: highest is %v, want %v", i, highest.value(), want)
		}
		if highest.age() != size-1 {
			t.Fatalf("bar %d: the high is %d bars back, want %d", i, highest.age(), size-1)
		}
	}
}

func TestResettingTheRollingExtremeForgetsEverything(t *testing.T) {
	highest := newExtreme(3, true)
	for _, value := range []float64{9, 8, 7} {
		highest.push(value)
	}

	highest.reset()
	if highest.full() {
		t.Fatal("still full after a reset")
	}

	highest.push(1)
	if highest.value() != 1 {
		t.Errorf("remembers %v from before the reset", highest.value())
	}
}
