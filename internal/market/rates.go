package market

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"sort"
	"time"
)

const (
	RatesFile          = "data/rates/selic.json"
	TradingDaysPerYear = 252
)

type RateStep struct {
	From      time.Time
	AnnualPct float64
}

type Rates struct {
	series  string
	source  string
	basis   float64
	through time.Time
	steps   []RateStep
}

type ratesFile struct {
	Series  string  `json:"series"`
	Source  string  `json:"source"`
	Basis   float64 `json:"basis"`
	Through string  `json:"through"`
	Rates   []struct {
		From      string  `json:"from"`
		AnnualPct float64 `json:"annual_pct"`
	} `json:"rates"`
}

func LoadRates(fsys fs.FS, name string) (*Rates, error) {
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var file ratesFile
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", name, err)
	}

	if file.Series == "" {
		return nil, fmt.Errorf("%s: series is required", name)
	}
	if file.Basis < 1 {
		return nil, fmt.Errorf("%s: basis must be the number of periods a year, got %g", name, file.Basis)
	}

	through, err := time.Parse(time.DateOnly, file.Through)
	if err != nil {
		return nil, fmt.Errorf("%s: through must be a YYYY-MM-DD date", name)
	}
	if len(file.Rates) == 0 {
		return nil, fmt.Errorf("%s: rates is empty", name)
	}

	rates := &Rates{
		series:  file.Series,
		source:  file.Source,
		basis:   file.Basis,
		through: through,
		steps:   make([]RateStep, 0, len(file.Rates)),
	}

	for i, entry := range file.Rates {
		from, err := time.Parse(time.DateOnly, entry.From)
		if err != nil {
			return nil, fmt.Errorf("%s: rates[%d].from must be a YYYY-MM-DD date", name, i)
		}
		if entry.AnnualPct < 0 || entry.AnnualPct > 100 {
			return nil, fmt.Errorf("%s: rates[%d].annual_pct is between 0 and 100, got %g", name, i, entry.AnnualPct)
		}
		if i > 0 && !from.After(rates.steps[i-1].From) {
			return nil, fmt.Errorf("%s: rates[%d].from (%s) must come after rates[%d].from", name, i, entry.From, i-1)
		}
		rates.steps = append(rates.steps, RateStep{From: from, AnnualPct: entry.AnnualPct})
	}

	if last := rates.steps[len(rates.steps)-1].From; through.Before(last) {
		return nil, fmt.Errorf("%s: through (%s) is before the last rate change (%s)",
			name, file.Through, last.Format(time.DateOnly))
	}

	return rates, nil
}

func (r *Rates) Series() string { return r.series }

func (r *Rates) Source() string { return r.source }

func (r *Rates) Basis() float64 { return r.basis }

func (r *Rates) Through() time.Time { return r.through }

func (r *Rates) Start() time.Time { return r.steps[0].From }

func (r *Rates) Covers(from, to time.Time) bool {
	return !from.Before(r.steps[0].From) && !to.After(r.through.AddDate(0, 0, 1))
}

func (r *Rates) AnnualPct(day time.Time) float64 {
	at := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)

	i := sort.Search(len(r.steps), func(i int) bool { return r.steps[i].From.After(at) })
	if i == 0 {
		return r.steps[0].AnnualPct
	}

	return r.steps[i-1].AnnualPct
}

func (r *Rates) PerPeriod(day time.Time, periodsPerYear float64) float64 {
	if periodsPerYear <= 0 {
		return 0
	}

	annual := r.AnnualPct(day) / 100
	daily := math.Pow(1+annual, 1/r.basis) - 1

	return math.Pow(1+daily, r.basis/periodsPerYear) - 1
}

func BarsPerYear(cal *Calendar, tf Timeframe) float64 {
	if !tf.Intraday() {
		return TradingDaysPerYear
	}

	width := tf.BucketWidth().Minutes()
	if width <= 0 {
		return TradingDaysPerYear
	}

	return TradingDaysPerYear * math.Max(math.Floor(cal.RegularSession().Minutes()/width), 1)
}
