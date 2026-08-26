package backtest

import (
	"math"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/market"
)

const (
	BenchmarkHold  = "buy_and_hold"
	BenchmarkIndex = "index"
)

type Benchmark struct {
	Kind           string  `json:"kind"`
	Symbol         string  `json:"symbol"`
	Basis          string  `json:"basis"`
	ReturnPct      float64 `json:"return_pct"`
	CAGRPct        float64 `json:"cagr_pct"`
	VolatilityPct  float64 `json:"volatility_pct"`
	MaxDrawdownPct float64 `json:"max_drawdown_pct"`
	Sharpe         float64 `json:"sharpe"`
	ExcessPct      float64 `json:"excess_pct"`
	Correlation    float64 `json:"correlation"`
	Beta           float64 `json:"beta"`
	DividendsCents int64   `json:"dividends_cents,omitempty"`
	FeesCents      int64   `json:"fees_cents,omitempty"`
	Unavailable    string  `json:"unavailable,omitempty"`

	curve []int64
	steps []float64
}

func holdBenchmark(req Request, dist distributions) Benchmark {
	mark := Benchmark{Kind: BenchmarkHold, Symbol: req.Symbol.Ticker, Basis: dist.basis()}

	costing := Costing{Costs: req.Plan.Spec.Costs, TickSize: req.Symbol.TickSize}
	entry := costing.Fill(Order{Kind: OrderMarket, Side: Buy}, req.Candles[1].Open)
	if entry <= 0 {
		mark.Unavailable = "the second bar has no usable open"
		return mark
	}

	lot := max(req.Symbol.LotSize, 1)
	qty := affordableQty(req.Capital, entry, costing, lot)
	if qty < 1 {
		mark.Unavailable = "starting capital does not cover one lot at the opening price"
		return mark
	}

	notional := costing.Notional(qty, entry)
	fees := costing.Fees(notional)
	cash := req.Capital - notional - fees

	mark.curve = make([]int64, len(req.Candles))
	mark.curve[0] = req.Capital

	dividends := int64(0)
	for i := 1; i < len(req.Candles); i++ {
		if perShare := dist.at(i); perShare > 0 {
			dividends += int64(math.Round(float64(qty) * perShare * 100))
		}
		mark.curve[i] = cash + dividends + costing.Notional(qty, req.Candles[i].Close)
	}

	exit := costing.Fill(Order{Kind: OrderMarket, Side: Sell}, req.Candles[len(req.Candles)-1].Close)
	exitNotional := costing.Notional(qty, exit)
	exitFees := costing.Fees(exitNotional)

	last := len(req.Candles) - 1
	mark.curve[last] = cash + dividends + exitNotional - exitFees

	mark.DividendsCents = dividends
	mark.FeesCents = fees + exitFees

	return mark
}

func indexBenchmark(req Request) Benchmark {
	mark := Benchmark{Kind: BenchmarkIndex, Symbol: req.IndexSymbol, Basis: BasisTotal}

	if req.IndexSymbol == "" {
		mark.Unavailable = "no benchmark index is configured"
		return mark
	}
	if len(req.Index) < 2 {
		mark.Unavailable = "no " + req.IndexSymbol + " candles cover this range"
		return mark
	}

	closes := alignCloses(req.Candles, req.Index)
	if closes == nil {
		mark.Unavailable = "no " + req.IndexSymbol + " candle lines up with the first bar of the run"
		return mark
	}

	mark.curve = make([]int64, len(closes))
	base := closes[0]
	for i, close := range closes {
		mark.curve[i] = int64(math.Round(float64(req.Capital) * close / base))
	}

	return mark
}

func alignCloses(candles, index []market.Candle) []float64 {
	byTS := make(map[int64]float64, len(index))
	for _, candle := range index {
		byTS[candle.TS.Unix()] = candle.Close
	}

	closes := make([]float64, len(candles))
	last := 0.0
	matched := 0

	for i, candle := range candles {
		if close, ok := byTS[candle.TS.Unix()]; ok && close > 0 {
			last = close
			matched++
		}
		if last <= 0 {
			return nil
		}
		closes[i] = last
	}

	if matched < 2 {
		return nil
	}

	return closes
}

func affordableQty(capital int64, price float64, costing Costing, lot int64) int64 {
	budget := float64(capital - costing.Costs.BrokerageCents)
	unit := price * 100 * (1 + costing.Costs.FeeBPS/10000)
	if budget <= 0 || unit <= 0 {
		return 0
	}

	qty := int64(math.Floor(budget/unit)) / lot * lot
	for qty > 0 {
		notional := costing.Notional(qty, price)
		if notional+costing.Fees(notional) <= capital {
			break
		}
		qty -= lot
	}

	return max(qty, 0)
}

func (b *Benchmark) score(stamps []time.Time, strategy []float64, rf []float64, periodsPerYear float64) {
	if len(b.curve) < 2 || len(b.curve) != len(stamps) || b.Unavailable != "" {
		return
	}

	points := make([]EquityPoint, len(b.curve))
	for i, cents := range b.curve {
		points[i] = EquityPoint{TS: stamps[i], Cents: cents}
	}

	b.steps = returnsOf(points)
	first, last := b.curve[0], b.curve[len(b.curve)-1]

	if first > 0 {
		b.ReturnPct = float64(last-first) / float64(first) * 100
	}
	b.CAGRPct = cagr(first, last, stamps[len(stamps)-1].Sub(stamps[0]))
	b.VolatilityPct = annualizedVol(b.steps, periodsPerYear)
	b.MaxDrawdownPct = deepestDrawdown(points).Pct

	if len(rf) == len(b.steps) {
		excess := make([]float64, len(b.steps))
		for i, step := range b.steps {
			excess[i] = step - rf[i]
		}
		b.Sharpe = sharpe(excess, periodsPerYear)
	}

	b.Correlation = correlation(strategy, b.steps)
	b.Beta = beta(strategy, b.steps)
}

func correlation(a, b []float64) float64 {
	if len(a) != len(b) || len(a) < 2 {
		return 0
	}

	spreadA, spreadB := stddev(a), stddev(b)
	if spreadA == 0 || spreadB == 0 {
		return 0
	}

	return covariance(a, b) / (spreadA * spreadB)
}

func beta(a, b []float64) float64 {
	if len(a) != len(b) || len(a) < 2 {
		return 0
	}

	variance := stddev(b)
	if variance == 0 {
		return 0
	}

	return covariance(a, b) / (variance * variance)
}

func covariance(a, b []float64) float64 {
	avgA, avgB := mean(a), mean(b)

	sum := 0.0
	for i := range a {
		sum += (a[i] - avgA) * (b[i] - avgB)
	}

	return sum / float64(len(a)-1)
}
