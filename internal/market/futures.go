package market

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

const DefaultRollOffsetDays = 0

type FuturesQuote struct {
	Symbol     string
	Expiration time.Time
	Multiplier float64
	Day        time.Time
	Settlement float64
	High       *float64
	Low        *float64
	Close      *float64
	Average    *float64
	Volume     *int64
	Trades     *int64
}

type ContinuousBar struct {
	Day        time.Time
	Symbol     string
	Settlement float64
	Adjustment float64
	Traded     bool
	Volume     int64
	Roll       bool
}

type ContinuousSeries struct {
	Root       string
	Multiplier float64
	Bars       []ContinuousBar
	Rolls      []Roll
}

type Roll struct {
	Day      time.Time
	From     string
	To       string
	Gap      float64
	Measured bool
}

type ContinuousOptions struct {
	RollOffsetDays int
	BackAdjust     bool
}

func BuildContinuous(root string, quotes []FuturesQuote, opts ContinuousOptions) (ContinuousSeries, error) {
	root = strings.ToUpper(strings.TrimSpace(root))
	if root == "" {
		return ContinuousSeries{}, fmt.Errorf("continuous series requires a root")
	}
	if opts.RollOffsetDays < 0 {
		return ContinuousSeries{}, fmt.Errorf("roll offset must not be negative, got %d", opts.RollOffsetDays)
	}

	byDay := map[string][]FuturesQuote{}
	days := []time.Time{}
	multiplier := 0.0

	for _, quote := range quotes {
		key := quote.Day.Format(time.DateOnly)
		if _, seen := byDay[key]; !seen {
			days = append(days, quote.Day)
		}
		byDay[key] = append(byDay[key], quote)
		multiplier = quote.Multiplier
	}

	slices.SortFunc(days, func(a, b time.Time) int { return a.Compare(b) })

	series := ContinuousSeries{Root: root, Multiplier: multiplier, Bars: []ContinuousBar{}, Rolls: []Roll{}}

	previous := ""
	used := []time.Time{}

	for _, day := range days {
		front, ok := frontContract(byDay[day.Format(time.DateOnly)], day, opts.RollOffsetDays)
		if !ok {
			continue
		}

		bar := ContinuousBar{
			Day:        day,
			Symbol:     front.Symbol,
			Settlement: front.Settlement,
			Traded:     front.Close != nil,
		}
		if front.Volume != nil {
			bar.Volume = *front.Volume
		}

		if previous != "" && front.Symbol != previous {
			bar.Roll = true
			gap, measured := rollGap(byDay, used, previous, front.Symbol)
			series.Rolls = append(series.Rolls, Roll{
				Day:      day,
				From:     previous,
				To:       front.Symbol,
				Gap:      gap,
				Measured: measured,
			})
		}

		previous = front.Symbol
		used = append(used, day)
		series.Bars = append(series.Bars, bar)
	}

	if opts.BackAdjust {
		backAdjust(&series)
	}

	return series, nil
}

func frontContract(quotes []FuturesQuote, day time.Time, offsetDays int) (FuturesQuote, bool) {
	cutoff := day.AddDate(0, 0, offsetDays)

	candidates := make([]FuturesQuote, 0, len(quotes))
	for _, quote := range quotes {
		if !quote.Expiration.Before(cutoff) {
			candidates = append(candidates, quote)
		}
	}
	if len(candidates) == 0 {
		return FuturesQuote{}, false
	}

	slices.SortFunc(candidates, func(a, b FuturesQuote) int { return a.Expiration.Compare(b.Expiration) })
	return candidates[0], true
}

func rollGap(byDay map[string][]FuturesQuote, history []time.Time, from, to string) (float64, bool) {
	for i := len(history) - 1; i >= 0; i-- {
		quotes := byDay[history[i].Format(time.DateOnly)]

		out, ok := settlementOf(quotes, from)
		if !ok {
			continue
		}
		in, ok := settlementOf(quotes, to)
		if !ok {
			continue
		}

		return in - out, true
	}

	return 0, false
}

func settlementOf(quotes []FuturesQuote, symbol string) (float64, bool) {
	for _, quote := range quotes {
		if quote.Symbol == symbol {
			return quote.Settlement, true
		}
	}
	return 0, false
}

func backAdjust(series *ContinuousSeries) {
	if len(series.Rolls) == 0 {
		return
	}

	gaps := make(map[string]float64, len(series.Rolls))
	for _, roll := range series.Rolls {
		gaps[roll.Day.Format(time.DateOnly)] = roll.Gap
	}

	offset := 0.0
	for i := len(series.Bars) - 1; i >= 0; i-- {
		series.Bars[i].Adjustment = offset
		series.Bars[i].Settlement += offset

		if gap, ok := gaps[series.Bars[i].Day.Format(time.DateOnly)]; ok {
			offset += gap
		}
	}
}
