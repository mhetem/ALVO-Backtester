package sweep

import (
	"testing"
	"time"
)

func day(t *testing.T, text string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.DateOnly, text)
	if err != nil {
		t.Fatalf("parsing %q: %v", text, err)
	}

	return parsed
}

func TestFoldsRollForwardByOneTestWindow(t *testing.T) {
	folds, err := Folds(day(t, "2026-01-01"), day(t, "2026-12-31"), 90, 30)
	if err != nil {
		t.Fatalf("planning the folds: %v", err)
	}

	if len(folds) != 9 {
		t.Fatalf("folds = %d, want 9 in a 365 day range", len(folds))
	}

	if folds[0].InStart != "2026-01-01" || folds[0].InEnd != "2026-03-31" {
		t.Errorf("fold 0 trains %s to %s, want 2026-01-01 to 2026-03-31", folds[0].InStart, folds[0].InEnd)
	}
	if folds[0].OutStart != "2026-04-01" || folds[0].OutEnd != "2026-04-30" {
		t.Errorf("fold 0 tests %s to %s, want 2026-04-01 to 2026-04-30", folds[0].OutStart, folds[0].OutEnd)
	}

	for i, fold := range folds {
		if fold.Fold != i {
			t.Errorf("folds[%d].Fold = %d, want %d", i, fold.Fold, i)
		}

		// The test window must start the day the training window ends, or a fold is either
		// testing on days it trained on or skipping days entirely.
		if want := day(t, fold.InEnd).AddDate(0, 0, 1).Format(time.DateOnly); fold.OutStart != want {
			t.Errorf("fold %d tests from %s, want the day after training ends (%s)", i, fold.OutStart, want)
		}

		// And each fold's test window must pick up where the last one left off, so every
		// day between the first test and the last is tested exactly once.
		if i > 0 {
			if want := day(t, folds[i-1].OutEnd).AddDate(0, 0, 1).Format(time.DateOnly); fold.OutStart != want {
				t.Errorf("fold %d tests from %s, want %s", i, fold.OutStart, want)
			}
		}

		if day(t, fold.OutEnd).After(day(t, "2026-12-31")) {
			t.Errorf("fold %d tests past the end of the range, to %s", i, fold.OutEnd)
		}
	}
}

func TestFoldsRefuseARangeThatFitsNothing(t *testing.T) {
	if _, err := Folds(day(t, "2026-01-01"), day(t, "2026-03-01"), 90, 30); err == nil {
		t.Error("a 60 day range accepted a 90 day training window")
	}
}

func TestFoldsRefuseAWindowTooShortToMeanAnything(t *testing.T) {
	if _, err := Folds(day(t, "2026-01-01"), day(t, "2026-12-31"), 1, 30); err == nil {
		t.Errorf("a one day training window was accepted, want at least %d", MinFoldDays)
	}
	if _, err := Folds(day(t, "2026-01-01"), day(t, "2026-12-31"), 90, 0); err == nil {
		t.Errorf("a zero day test window was accepted, want at least %d", MinFoldDays)
	}
}

func TestFoldsStopAtTheCap(t *testing.T) {
	folds, err := Folds(day(t, "2020-01-01"), day(t, "2026-12-31"), 90, 30)
	if err != nil {
		t.Fatalf("planning the folds: %v", err)
	}
	if len(folds) != MaxFolds {
		t.Errorf("folds = %d over seven years, want the cap of %d", len(folds), MaxFolds)
	}
}

func TestAFoldNamesItsOwnWindows(t *testing.T) {
	folds, err := Folds(day(t, "2026-01-01"), day(t, "2026-12-31"), 90, 30)
	if err != nil {
		t.Fatalf("planning the folds: %v", err)
	}

	start, end, err := folds[0].Window(PhaseInSample, time.UTC)
	if err != nil {
		t.Fatalf("reading the in-sample window: %v", err)
	}
	if start.Format(time.DateOnly) != folds[0].InStart || end.Format(time.DateOnly) != folds[0].InEnd {
		t.Errorf("in-sample window is %s to %s, want %s to %s",
			start.Format(time.DateOnly), end.Format(time.DateOnly), folds[0].InStart, folds[0].InEnd)
	}

	start, end, err = folds[0].Window(PhaseOutOfSample, time.UTC)
	if err != nil {
		t.Fatalf("reading the out-of-sample window: %v", err)
	}
	if start.Format(time.DateOnly) != folds[0].OutStart || end.Format(time.DateOnly) != folds[0].OutEnd {
		t.Errorf("out-of-sample window is %s to %s, want %s to %s",
			start.Format(time.DateOnly), end.Format(time.DateOnly), folds[0].OutStart, folds[0].OutEnd)
	}
}
