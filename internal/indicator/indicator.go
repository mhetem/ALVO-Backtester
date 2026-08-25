package indicator

import "time"

type Candle struct {
	TS     time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

type Indicator interface {
	Update(Candle)
	Values() []float64
	Ready() bool
	Warmup() int
	Reset()
}

type Result struct {
	Start  int
	Values [][]float64
}

func Feed(ind Indicator, candles []Candle) {
	for _, candle := range candles {
		ind.Update(candle)
	}
}

func Emit(ind Indicator, candles []Candle) Result {
	result := Result{Start: len(candles), Values: [][]float64{}}

	for i, candle := range candles {
		ind.Update(candle)
		if !ind.Ready() {
			continue
		}

		values := ind.Values()
		if i < result.Start {
			result.Start = i
			result.Values = make([][]float64, len(values))
			for j := range result.Values {
				result.Values[j] = make([]float64, 0, len(candles)-i)
			}
		}
		for j, value := range values {
			result.Values[j] = append(result.Values[j], value)
		}
	}

	return result
}

func Compute(ind Indicator, candles []Candle) Result {
	ind.Reset()
	return Emit(ind, candles)
}
