package market

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	BorrowFile      = "data/borrow/b3_btb.json"
	MaxBorrowAnnual = 300.0
)

type BorrowStep struct {
	From      time.Time
	AnnualPct float64
}

type BorrowWindow struct {
	From time.Time
	To   time.Time
}

type Borrow struct {
	source      string
	basis       float64
	through     time.Time
	fallback    float64
	rates       map[string][]BorrowStep
	unavailable map[string][]BorrowWindow
}

type borrowFile struct {
	Source   string  `json:"source"`
	Basis    float64 `json:"basis"`
	Through  string  `json:"through"`
	Fallback float64 `json:"default_annual_pct"`
	Rates    map[string][]struct {
		From      string  `json:"from"`
		AnnualPct float64 `json:"annual_pct"`
	} `json:"rates"`
	Unavailable []struct {
		Ticker string `json:"ticker"`
		From   string `json:"from"`
		To     string `json:"to"`
	} `json:"unavailable"`
}

func LoadBorrow(fsys fs.FS, name string) (*Borrow, error) {
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var file borrowFile
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", name, err)
	}

	if file.Basis < 1 {
		return nil, fmt.Errorf("%s: basis must be the number of periods a year, got %g", name, file.Basis)
	}
	if file.Fallback < 0 || file.Fallback > MaxBorrowAnnual {
		return nil, fmt.Errorf("%s: default_annual_pct is between 0 and %g, got %g", name, MaxBorrowAnnual, file.Fallback)
	}

	through, err := time.Parse(time.DateOnly, file.Through)
	if err != nil {
		return nil, fmt.Errorf("%s: through must be a YYYY-MM-DD date", name)
	}

	borrow := &Borrow{
		source:      file.Source,
		basis:       file.Basis,
		through:     through,
		fallback:    file.Fallback,
		rates:       make(map[string][]BorrowStep, len(file.Rates)),
		unavailable: map[string][]BorrowWindow{},
	}

	for ticker, entries := range file.Rates {
		key := strings.ToUpper(strings.TrimSpace(ticker))
		if key == "" {
			return nil, fmt.Errorf("%s: rates has an entry with no ticker", name)
		}
		if len(entries) == 0 {
			return nil, fmt.Errorf("%s: rates[%s] is empty", name, key)
		}

		steps := make([]BorrowStep, 0, len(entries))
		for i, entry := range entries {
			from, err := time.Parse(time.DateOnly, entry.From)
			if err != nil {
				return nil, fmt.Errorf("%s: rates[%s][%d].from must be a YYYY-MM-DD date", name, key, i)
			}
			if entry.AnnualPct < 0 || entry.AnnualPct > MaxBorrowAnnual {
				return nil, fmt.Errorf("%s: rates[%s][%d].annual_pct is between 0 and %g, got %g",
					name, key, i, MaxBorrowAnnual, entry.AnnualPct)
			}
			if i > 0 && !from.After(steps[i-1].From) {
				return nil, fmt.Errorf("%s: rates[%s][%d].from (%s) must come after rates[%s][%d].from",
					name, key, i, entry.From, key, i-1)
			}
			steps = append(steps, BorrowStep{From: from, AnnualPct: entry.AnnualPct})
		}

		if last := steps[len(steps)-1].From; through.Before(last) {
			return nil, fmt.Errorf("%s: through (%s) is before the last rate change for %s (%s)",
				name, file.Through, key, last.Format(time.DateOnly))
		}

		borrow.rates[key] = steps
	}

	for i, entry := range file.Unavailable {
		key := strings.ToUpper(strings.TrimSpace(entry.Ticker))
		if key == "" {
			return nil, fmt.Errorf("%s: unavailable[%d].ticker is required", name, i)
		}

		from, err := time.Parse(time.DateOnly, entry.From)
		if err != nil {
			return nil, fmt.Errorf("%s: unavailable[%d].from must be a YYYY-MM-DD date", name, i)
		}

		to := through
		if entry.To != "" {
			to, err = time.Parse(time.DateOnly, entry.To)
			if err != nil {
				return nil, fmt.Errorf("%s: unavailable[%d].to must be a YYYY-MM-DD date", name, i)
			}
		}
		if to.Before(from) {
			return nil, fmt.Errorf("%s: unavailable[%d].to (%s) is before from (%s)", name, i, entry.To, entry.From)
		}

		borrow.unavailable[key] = append(borrow.unavailable[key], BorrowWindow{From: from, To: to})
	}

	return borrow, nil
}

func (b *Borrow) Source() string { return b.source }

func (b *Borrow) Basis() float64 { return b.basis }

func (b *Borrow) Through() time.Time { return b.through }

func (b *Borrow) DefaultAnnualPct() float64 { return b.fallback }

func (b *Borrow) Covers(to time.Time) bool { return !to.After(b.through.AddDate(0, 0, 1)) }

func (b *Borrow) AnnualPct(ticker string, day time.Time) float64 {
	steps, held := b.rates[strings.ToUpper(ticker)]
	if !held {
		return b.fallback
	}

	at := dayOf(day)
	i := sort.Search(len(steps), func(i int) bool { return steps[i].From.After(at) })
	if i == 0 {
		return steps[0].AnnualPct
	}

	return steps[i-1].AnnualPct
}

func (b *Borrow) PerPeriod(ticker string, day time.Time, periodsPerYear float64) float64 {
	if periodsPerYear <= 0 {
		return 0
	}

	annual := b.AnnualPct(ticker, day) / 100
	daily := math.Pow(1+annual, 1/b.basis) - 1

	return math.Pow(1+daily, b.basis/periodsPerYear) - 1
}

func (b *Borrow) Available(ticker string, day time.Time) bool {
	windows, held := b.unavailable[strings.ToUpper(ticker)]
	if !held {
		return true
	}

	at := dayOf(day)
	for _, window := range windows {
		if !at.Before(window.From) && !at.After(window.To) {
			return false
		}
	}

	return true
}

func dayOf(day time.Time) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
}
