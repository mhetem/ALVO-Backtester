package backtest

import (
	"math"
	"strings"
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

type holding struct {
	qty       int64
	last      float64
	at        int
	dividends int64
	fees      int64
}

// The basket case is an equal-weight buy-and-hold: each symbol gets the same share of the
// starting capital at its own second bar, carries the same costs, dividends and splits as
// the engine, and is liquidated at the end. A one-symbol basket is the same code with the
// division by one, which is why there is no second path for it.
func holdBenchmark(req Request, books []*book, stamps []time.Time, basis string) Benchmark {
	mark := Benchmark{Kind: BenchmarkHold, Symbol: basketName(books), Basis: basis}

	if len(books) == 0 || len(stamps) < 2 {
		mark.Unavailable = "there are no bars to hold"
		return mark
	}

	share := req.Capital / int64(len(books))
	cash := req.Capital
	legs := make([]holding, len(books))
	curve := make([]int64, len(stamps))
	bought := false

	for i, ts := range stamps {
		for k, b := range books {
			leg := &legs[k]
			if leg.at >= len(b.candles) || !b.candles[leg.at].TS.Equal(ts) {
				continue
			}

			candle := b.candles[leg.at]
			at := leg.at
			leg.at++

			if leg.qty > 0 {
				if perShare := b.acts.dividendAt(at); perShare > 0 {
					paid := int64(math.Round(float64(leg.qty) * perShare * 100))
					cash += paid
					leg.dividends += paid
				}
				if factor := b.acts.factorAt(at); factor > 0 {
					price := candle.Open
					if price <= 0 {
						price = candle.Close
					}
					exact := float64(leg.qty) * factor
					next := int64(math.Floor(exact + splitEpsilon))
					cash += int64(math.Round((exact - float64(next)) * price * 100))
					leg.qty = next
				}
			}

			// The benchmark buys at the second bar, not the first: the engine structurally
			// cannot fill before then, and handing buy-and-hold a bar no strategy can have
			// would tax every strategy by something that is not the strategy.
			if at == 1 && leg.qty == 0 {
				entry := b.costing.Fill(Order{Kind: OrderMarket, Side: Buy}, candle.Open)
				lot := max(b.symbol.LotSize, 1)
				if qty := affordableQty(share, entry, b.costing, lot); entry > 0 && qty > 0 {
					notional := b.costing.Notional(qty, entry)
					fees := b.costing.Fees(notional)
					cash -= notional + fees
					leg.qty = qty
					leg.fees = fees
					bought = true
				}
			}

			leg.last = candle.Close
		}

		value := cash
		for k := range legs {
			value += books[k].costing.Notional(legs[k].qty, legs[k].last)
		}
		curve[i] = value
	}

	if !bought {
		mark.Unavailable = "starting capital does not cover one lot at the opening price"
		return mark
	}

	final := cash
	for k, b := range books {
		leg := &legs[k]
		if leg.qty == 0 {
			continue
		}

		exit := b.costing.Fill(Order{Kind: OrderMarket, Side: Sell}, leg.last)
		notional := b.costing.Notional(leg.qty, exit)
		fees := b.costing.Fees(notional)

		final += notional - fees
		leg.fees += fees
	}
	curve[len(curve)-1] = final

	for _, leg := range legs {
		mark.DividendsCents += leg.dividends
		mark.FeesCents += leg.fees
	}

	mark.curve = curve

	return mark
}

func basketName(books []*book) string {
	names := make([]string, 0, len(books))
	for _, b := range books {
		names = append(names, b.symbol.Ticker)
	}
	return strings.Join(names, ", ")
}

func indexBenchmark(req Request, stamps []time.Time) Benchmark {
	mark := Benchmark{Kind: BenchmarkIndex, Symbol: req.IndexSymbol, Basis: BasisTotal}

	if req.IndexSymbol == "" {
		mark.Unavailable = "no benchmark index is configured"
		return mark
	}
	if len(req.Index) < 2 {
		mark.Unavailable = "no " + req.IndexSymbol + " candles cover this range"
		return mark
	}

	closes := alignCloses(stamps, req.Index)
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

func alignCloses(stamps []time.Time, index []market.Candle) []float64 {
	byTS := make(map[int64]float64, len(index))
	for _, candle := range index {
		byTS[candle.TS.Unix()] = candle.Close
	}

	closes := make([]float64, len(stamps))
	last := 0.0
	matched := 0

	for i, ts := range stamps {
		if close, ok := byTS[ts.Unix()]; ok && close > 0 {
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
