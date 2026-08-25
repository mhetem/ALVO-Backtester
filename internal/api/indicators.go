package api

import (
	"github.com/mhetem/ALVO-Backtester/internal/indicator"
	"github.com/mhetem/ALVO-Backtester/internal/market"
)

type seriesBody struct {
	Name   string    `json:"name"`
	Start  int       `json:"start"`
	Values []float64 `json:"values"`
}

type indicatorBody struct {
	Key     string             `json:"key"`
	Name    string             `json:"name"`
	Title   string             `json:"title"`
	Params  map[string]float64 `json:"params"`
	Source  string             `json:"source,omitempty"`
	Overlay bool               `json:"overlay"`
	Warmup  int                `json:"warmup"`
	Series  []seriesBody       `json:"series"`
}

func indicatorCandles(candles []market.Candle) []indicator.Candle {
	out := make([]indicator.Candle, 0, len(candles))
	for _, candle := range candles {
		out = append(out, indicator.Candle{
			TS:     candle.TS,
			Open:   candle.Open,
			High:   candle.High,
			Low:    candle.Low,
			Close:  candle.Close,
			Volume: float64(candle.Volume),
		})
	}
	return out
}

func computeIndicators(instances []indicator.Instance, prime, page []market.Candle) []indicatorBody {
	if len(instances) == 0 {
		return nil
	}

	warmup := indicatorCandles(prime)
	bars := indicatorCandles(page)
	bodies := make([]indicatorBody, 0, len(instances))

	for _, instance := range instances {
		instance.Indicator.Reset()
		indicator.Feed(instance.Indicator, warmup)
		result := indicator.Emit(instance.Indicator, bars)

		body := indicatorBody{
			Key:     instance.Key,
			Name:    instance.Spec.Name,
			Title:   instance.Spec.Title,
			Params:  instance.Params.All(),
			Overlay: instance.Spec.Overlay,
			Warmup:  instance.Indicator.Warmup(),
			Series:  make([]seriesBody, 0, len(instance.Spec.Outputs)),
		}
		if instance.Spec.Sourced {
			body.Source = instance.Params.Source().String()
		}

		for i, name := range instance.Spec.Outputs {
			series := seriesBody{Name: name, Start: result.Start, Values: []float64{}}
			if i < len(result.Values) {
				series.Values = result.Values[i]
			}
			body.Series = append(body.Series, series)
		}

		bodies = append(bodies, body)
	}

	return bodies
}
