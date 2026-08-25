package indicator

import "testing"

func TestClassicPivotsReadTheBarBeforeThem(t *testing.T) {
	candles := goldenBars(t)

	_, result := compute(t, "pivots:1", candles)
	if result.Start != 1 {
		t.Fatalf("pivots:1 starts at bar %d, want 1", result.Start)
	}

	for i := range result.Values[0] {
		bar := result.Start + i
		previous := candles[bar-1]

		pivot := (previous.High + previous.Low + previous.Close) / 3
		span := previous.High - previous.Low
		want := []float64{
			pivot,
			2*pivot - previous.Low,
			pivot + span,
			previous.High + 2*(pivot-previous.Low),
			2*pivot - previous.High,
			pivot - span,
			previous.Low - 2*(previous.High-pivot),
		}

		for j, value := range want {
			if !within(result.Values[j][i], value) {
				t.Fatalf("bar %d: %s is %v, want %v", bar, pivotOutputs[j], result.Values[j][i], value)
			}
		}
	}
}

func TestPivotLevelsComeOutInOrder(t *testing.T) {
	candles := goldenBars(t)

	for _, key := range []string{"pivots:1", "fibpivots:1", "pivots:5"} {
		_, result := compute(t, key, candles)

		for i := range result.Values[0] {
			ladder := []float64{
				result.Values[6][i], result.Values[5][i], result.Values[4][i],
				result.Values[0][i],
				result.Values[1][i], result.Values[2][i], result.Values[3][i],
			}
			for j := 1; j < len(ladder); j++ {
				if ladder[j] < ladder[j-1] {
					t.Fatalf("%s bar %d: the ladder runs %v", key, result.Start+i, ladder)
				}
			}
		}
	}
}

func TestFibonacciPivotsAreSymmetricAroundThePivot(t *testing.T) {
	candles := goldenBars(t)

	_, result := compute(t, "fibpivots:1", candles)
	for i := range result.Values[0] {
		pivot := result.Values[0][i]
		for j := 1; j <= 3; j++ {
			above := result.Values[j][i] - pivot
			below := pivot - result.Values[j+3][i]
			if !within(above, below) {
				t.Fatalf("bar %d: r%d sits %v above the pivot while s%d sits %v below",
					result.Start+i, j, above, j, below)
			}
		}
	}
}

func TestFractalsStepOnlyOntoAConfirmedPivot(t *testing.T) {
	candles := goldenBars(t)

	const period = 2
	_, result := compute(t, "fractals:2", candles)
	if result.Start != 2*period {
		t.Fatalf("fractals:2 starts at bar %d, want %d", result.Start, 2*period)
	}

	steps := 0
	for i := 1; i < len(result.Values[0]); i++ {
		if result.Values[0][i] == result.Values[0][i-1] {
			continue
		}
		steps++

		centre := result.Start + i - period
		if result.Values[0][i] != candles[centre].High {
			t.Fatalf("bar %d: the level stepped to %v, which is no bar's high",
				result.Start+i, result.Values[0][i])
		}
		for j := centre - period; j <= centre+period; j++ {
			if j != centre && candles[j].High >= candles[centre].High {
				t.Fatalf("bar %d: the level stepped onto a high that bar %d matches", result.Start+i, j)
			}
		}
	}

	if steps == 0 {
		t.Error("the level never moved over 200 bars, so this proves nothing")
	}
}

func TestZigZagTurnsOnceOverASingleReversal(t *testing.T) {
	candles := tentRamp(240, 200)

	_, result := compute(t, "zigzag:5", candles)
	if result.Start != 0 {
		t.Fatalf("zigzag starts at bar %d, want the first one", result.Start)
	}

	turns := 0
	for i, heading := range result.Values[1] {
		if heading != -1 && heading != 0 && heading != 1 {
			t.Fatalf("bar %d: the direction reads %v", i, heading)
		}
		if i > 0 && heading != result.Values[1][i-1] {
			turns++
		}
	}

	if turns != 2 {
		t.Errorf("the direction changed %d times over one peak, want 2 — undecided, up, then down", turns)
	}
	if last := result.Values[1][len(result.Values[1])-1]; last != -1 {
		t.Errorf("the run ends heading %v, want -1", last)
	}
}

func TestTheZigZagLineOnlyExtendsWhileALegRuns(t *testing.T) {
	candles := goldenBars(t)

	_, result := compute(t, "zigzag:5", candles)
	for i := 1; i < len(result.Values[0]); i++ {
		if result.Values[1][i] != result.Values[1][i-1] {
			continue
		}
		switch {
		case result.Values[1][i] > 0 && result.Values[0][i] < result.Values[0][i-1]:
			t.Fatalf("bar %d: a rising leg fell from %v to %v", i, result.Values[0][i-1], result.Values[0][i])
		case result.Values[1][i] < 0 && result.Values[0][i] > result.Values[0][i-1]:
			t.Fatalf("bar %d: a falling leg rose from %v to %v", i, result.Values[0][i-1], result.Values[0][i])
		}
	}
}
