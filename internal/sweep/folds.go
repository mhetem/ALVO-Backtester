package sweep

import (
	"fmt"
	"time"
)

type Fold struct {
	Fold     int    `json:"fold"`
	InStart  string `json:"in_start"`
	InEnd    string `json:"in_end"`
	OutStart string `json:"out_start"`
	OutEnd   string `json:"out_end"`
}

func (f Fold) Window(phase string, loc *time.Location) (time.Time, time.Time, error) {
	from, to := f.InStart, f.InEnd
	if phase == PhaseOutOfSample {
		from, to = f.OutStart, f.OutEnd
	}

	start, err := time.ParseInLocation(time.DateOnly, from, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("fold %d has an unreadable %s start: %w", f.Fold, phase, err)
	}

	end, err := time.ParseInLocation(time.DateOnly, to, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("fold %d has an unreadable %s end: %w", f.Fold, phase, err)
	}

	return start, end, nil
}

// Folds roll forward by one out-of-sample window at a time, so every day between the first
// test and the last is tested exactly once, by a set of parameters chosen without ever
// having seen it. The windows are calendar days because a run's dates are calendar days;
// the trading calendar decides how many bars that turns into.
func Folds(start, end time.Time, inDays, outDays int) ([]Fold, error) {
	switch {
	case inDays < MinFoldDays:
		return nil, fmt.Errorf("in_sample_days is at least %d, got %d", MinFoldDays, inDays)
	case outDays < MinFoldDays:
		return nil, fmt.Errorf("out_of_sample_days is at least %d, got %d", MinFoldDays, outDays)
	}

	span := int(end.Sub(start).Hours()/24) + 1
	if span < inDays+outDays {
		return nil, fmt.Errorf("%s to %s is %d days, and one fold needs %d: widen the range, or shorten the windows",
			start.Format(time.DateOnly), end.Format(time.DateOnly), span, inDays+outDays)
	}

	folds := make([]Fold, 0, MaxFolds)

	for i := 0; ; i++ {
		inStart := start.AddDate(0, 0, i*outDays)
		inEnd := inStart.AddDate(0, 0, inDays-1)
		outStart := inEnd.AddDate(0, 0, 1)
		outEnd := outStart.AddDate(0, 0, outDays-1)

		if outEnd.After(end) {
			break
		}

		folds = append(folds, Fold{
			Fold:     i,
			InStart:  inStart.Format(time.DateOnly),
			InEnd:    inEnd.Format(time.DateOnly),
			OutStart: outStart.Format(time.DateOnly),
			OutEnd:   outEnd.Format(time.DateOnly),
		})

		if len(folds) == MaxFolds {
			break
		}
	}

	if len(folds) == 0 {
		return nil, fmt.Errorf("no fold fits between %s and %s with a %d day window tested over %d",
			start.Format(time.DateOnly), end.Format(time.DateOnly), inDays, outDays)
	}

	return folds, nil
}
