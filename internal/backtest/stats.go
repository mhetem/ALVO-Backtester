package backtest

import (
	"math"
	"time"
)

type Drawdown struct {
	Pct       float64   `json:"pct"`
	Cents     int64     `json:"cents"`
	PeakTS    time.Time `json:"peak_ts"`
	TroughTS  time.Time `json:"trough_ts"`
	Bars      int       `json:"bars"`
	Recovered bool      `json:"recovered"`
}

func returnsOf(curve []EquityPoint) []float64 {
	if len(curve) < 2 {
		return nil
	}

	steps := make([]float64, 0, len(curve)-1)
	for i := 1; i < len(curve); i++ {
		prev := curve[i-1].Cents
		if prev == 0 {
			steps = append(steps, 0)
			continue
		}
		steps = append(steps, float64(curve[i].Cents-prev)/math.Abs(float64(prev)))
	}

	return steps
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	total := 0.0
	for _, value := range values {
		total += value
	}

	return total / float64(len(values))
}

func stddev(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}

	avg := mean(values)
	sum := 0.0
	for _, value := range values {
		delta := value - avg
		sum += delta * delta
	}

	return math.Sqrt(sum / float64(len(values)-1))
}

func downside(values []float64, target float64) float64 {
	if len(values) < 2 {
		return 0
	}

	sum := 0.0
	for _, value := range values {
		if delta := value - target; delta < 0 {
			sum += delta * delta
		}
	}

	return math.Sqrt(sum / float64(len(values)-1))
}

func annualizedVol(steps []float64, periodsPerYear float64) float64 {
	if periodsPerYear <= 0 {
		return 0
	}
	return stddev(steps) * math.Sqrt(periodsPerYear) * 100
}

func cagr(first, last int64, span time.Duration) float64 {
	years := span.Hours() / 24 / 365.25
	if first <= 0 || last <= 0 || years <= 0 {
		return 0
	}

	return (math.Pow(float64(last)/float64(first), 1/years) - 1) * 100
}

func sharpe(excess []float64, periodsPerYear float64) float64 {
	spread := stddev(excess)
	if spread == 0 || periodsPerYear <= 0 {
		return 0
	}

	return mean(excess) / spread * math.Sqrt(periodsPerYear)
}

func sortino(excess []float64, periodsPerYear float64) float64 {
	spread := downside(excess, 0)
	if spread == 0 || periodsPerYear <= 0 {
		return 0
	}

	return mean(excess) / spread * math.Sqrt(periodsPerYear)
}

func calmar(annualPct, maxDrawdownPct float64) float64 {
	if maxDrawdownPct >= 0 {
		return 0
	}
	return annualPct / -maxDrawdownPct
}

func deepestDrawdown(curve []EquityPoint) Drawdown {
	if len(curve) == 0 {
		return Drawdown{}
	}

	worst := Drawdown{}
	peak := curve[0]
	peakAt := 0
	peakCents := int64(0)

	for i, point := range curve {
		if point.Cents >= peak.Cents {
			peak = point
			peakAt = i
			continue
		}
		if peak.Cents <= 0 {
			continue
		}

		fall := float64(point.Cents-peak.Cents) / float64(peak.Cents) * 100
		if fall < worst.Pct {
			worst = Drawdown{
				Pct:      fall,
				Cents:    point.Cents - peak.Cents,
				PeakTS:   peak.TS,
				TroughTS: point.TS,
				Bars:     i - peakAt,
			}
			peakCents = peak.Cents
		}
	}

	if worst.Pct == 0 {
		return Drawdown{}
	}

	for _, point := range curve {
		if point.TS.After(worst.TroughTS) && point.Cents >= peakCents {
			worst.Recovered = true
			break
		}
	}

	return worst
}

func longestDrawdown(curve []EquityPoint) int {
	if len(curve) == 0 {
		return 0
	}

	longest := 0
	peak := curve[0].Cents
	peakAt := 0

	for i, point := range curve {
		if point.Cents >= peak {
			peak = point.Cents
			peakAt = i
			continue
		}
		longest = max(longest, i-peakAt)
	}

	return longest
}
