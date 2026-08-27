package backtest

import (
	"slices"
	"strings"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/market"
)

// Every stock in a basket is its own run on its own slice of the capital. Nothing is shared
// between them — not cash, not a position count — so a basket is N independent results plus
// the sum of their curves, and a stock's numbers read the same whether it was run alone or
// alongside nineteen others.
func runBasket(req Request, shares []int64) Result {
	sleeves := make([]Result, len(req.Instruments))
	for i, held := range req.Instruments {
		sleeves[i] = runSleeve(req, held, shares[i])
	}

	stamps := unionStamps(sleeves)
	trades := mergeTrades(sleeves)

	// Sleeve curves are stored on the union timeline, not on each stock's own bars. Every
	// curve then carries the same stamps as the aggregate, which is what lets the report
	// downsample them identically and draw them on one axis.
	aligned := make([][]int64, len(sleeves))
	for i, sleeve := range sleeves {
		aligned[i] = alignSleeve(stamps, stampsOf(sleeve.Equity), centsOf(sleeve.Equity), shares[i])
	}

	result := Result{
		Trades:  trades,
		Equity:  sumAligned(aligned, stamps),
		Hold:    sumBenchmark(sleeves, shares, stamps, BenchmarkHold),
		Index:   sumBenchmark(sleeves, shares, stamps, BenchmarkIndex),
		Sleeves: make([]Sleeve, 0, len(sleeves)),
	}

	for i, held := range req.Instruments {
		result.Sleeves = append(result.Sleeves, Sleeve{
			Symbol:   held.Symbol.Ticker,
			SymbolID: held.Symbol.ID,
			Capital:  shares[i],
			Equity:   pointsOf(stamps, aligned[i]),
		})
	}

	result.Metrics = aggregateMetrics(req, sleeves, shares, stamps, result.Equity, trades, result.Hold, result.Index)

	return result
}

func runSleeve(req Request, held Instrument, capital int64) Result {
	sub := req
	sub.Instruments = []Instrument{held}
	sub.Capital = capital

	return newEngine(sub).run()
}

// The remainder goes to the first sleeves rather than being dropped, so the sleeves always
// add back up to exactly the capital the run was given.
func splitCapital(total int64, n int) []int64 {
	shares := make([]int64, n)
	base, extra := total/int64(n), total%int64(n)

	for i := range shares {
		shares[i] = base
		if int64(i) < extra {
			shares[i]++
		}
	}

	return shares
}

func unionStamps(sleeves []Result) []time.Time {
	seen := map[int64]time.Time{}
	for _, sleeve := range sleeves {
		for _, point := range sleeve.Equity {
			seen[point.TS.UnixNano()] = point.TS
		}
	}

	keys := make([]int64, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	stamps := make([]time.Time, 0, len(keys))
	for _, key := range keys {
		stamps = append(stamps, seen[key])
	}

	return stamps
}

// A sleeve that has not started yet is worth its untouched capital, and one whose last bar
// has passed holds its final value. Carrying both is what keeps the sum a real portfolio
// curve across symbols that were listed late or delisted early.
func alignSleeve(stamps, own []time.Time, values []int64, start int64) []int64 {
	out := make([]int64, len(stamps))
	last, at := start, 0

	for i, ts := range stamps {
		for at < len(own) && !own[at].After(ts) {
			last = values[at]
			at++
		}
		out[i] = last
	}

	return out
}

func stampsOf(points []EquityPoint) []time.Time {
	stamps := make([]time.Time, len(points))
	for i, point := range points {
		stamps[i] = point.TS
	}
	return stamps
}

func centsOf(points []EquityPoint) []int64 {
	cents := make([]int64, len(points))
	for i, point := range points {
		cents[i] = point.Cents
	}
	return cents
}

func sumAligned(curves [][]int64, stamps []time.Time) []EquityPoint {
	total := make([]int64, len(stamps))
	for _, curve := range curves {
		for k, cents := range curve {
			total[k] += cents
		}
	}

	return pointsOf(stamps, total)
}

func pointsOf(stamps []time.Time, cents []int64) []EquityPoint {
	points := make([]EquityPoint, len(stamps))
	for i, ts := range stamps {
		points[i] = EquityPoint{TS: ts, Cents: cents[i]}
	}
	return points
}

// Summing the sleeves' own benchmark curves is what makes the aggregate benchmark equal
// weight by construction. One sleeve without a curve makes the sum a comparison against
// something that is not the whole basket, so the whole benchmark drops out.
func sumBenchmark(sleeves []Result, shares []int64, stamps []time.Time, kind string) []int64 {
	total := make([]int64, len(stamps))

	for i, sleeve := range sleeves {
		curve := sleeve.Hold
		if kind == BenchmarkIndex {
			curve = sleeve.Index
		}
		if len(curve) != len(sleeve.Equity) || len(curve) == 0 {
			return nil
		}

		aligned := alignSleeve(stamps, stampsOf(sleeve.Equity), curve, shares[i])
		for k, cents := range aligned {
			total[k] += cents
		}
	}

	return total
}

func mergeTrades(sleeves []Result) []Trade {
	merged := []Trade{}
	for _, sleeve := range sleeves {
		merged = append(merged, sleeve.Trades...)
	}

	slices.SortStableFunc(merged, func(a, b Trade) int {
		if !a.EntryTS.Equal(b.EntryTS) {
			return a.EntryTS.Compare(b.EntryTS)
		}
		if !a.ExitTS.Equal(b.ExitTS) {
			return a.ExitTS.Compare(b.ExitTS)
		}
		return strings.Compare(a.Symbol, b.Symbol)
	})

	// Seq is the run's primary key alongside its id, so the sleeves' own numbering, which
	// restarts at one for every stock, cannot survive the merge.
	for i := range merged {
		merged[i].Seq = int32(i + 1)
	}

	return merged
}

func aggregateMetrics(
	req Request,
	sleeves []Result,
	shares []int64,
	stamps []time.Time,
	equity []EquityPoint,
	trades []Trade,
	hold, index []int64,
) Metrics {
	m := Metrics{
		Bars:         len(stamps),
		Trades:       len(trades),
		CapitalCents: req.Capital,
		BarsPerYear:  barsPerYear(req),
		Basis:        BasisPrice,
	}

	for _, sleeve := range sleeves {
		s := sleeve.Metrics
		if s.Basis == BasisTotal {
			m.Basis = BasisTotal
		}
		m.DividendsCents += s.DividendsCents
		m.DividendEvents += s.DividendEvents
		m.BorrowCents += s.BorrowCents
		m.BorrowStale = m.BorrowStale || s.BorrowStale
		m.SplitCashCents += s.SplitCashCents
		m.SkippedEntries += s.SkippedEntries
		m.ShortsUnavailable += s.ShortsUnavailable
		m.UnadjustedBars += s.UnadjustedBars
		m.UnpricedActions += s.UnpricedActions
		m.SplitEvents += s.SplitEvents
		m.SplitsApplied += s.SplitsApplied
		m.AmbiguousBars += s.AmbiguousBars
	}

	// A bar counts once however many sleeves were exposed on it, so time in market stays a
	// share of the run's calendar rather than a sum that can exceed it.
	exposed := map[int64]bool{}
	for _, sleeve := range sleeves {
		for _, ts := range sleeve.held {
			exposed[ts.Unix()] = true
		}
	}
	m.BarsInMarket = len(exposed)

	m.FinalEquityCents = m.CapitalCents
	if len(equity) > 0 {
		m.FinalEquityCents = equity[len(equity)-1].Cents
	}

	tallyTrades(&m, trades, stamps)
	m.Symbols = sleeveStatsOf(sleeves, req.Instruments, shares, req.Capital)

	m.PnLCents = m.FinalEquityCents - m.CapitalCents
	if m.CapitalCents > 0 {
		m.ReturnPct = float64(m.PnLCents) / float64(m.CapitalCents) * 100
	}
	if m.Bars > 0 {
		m.TimeInMarket = float64(m.BarsInMarket) / float64(m.Bars) * 100
	}

	if len(equity) < 2 {
		return m
	}

	steps := returnsOf(equity)
	rf := riskFreeOf(&m, req.Rates, equity, m.BarsPerYear)

	riskOf(&m, equity, steps, rf, m.BarsPerYear)
	m.Benchmarks = aggregateBenchmarks(req, sleeves, stamps, m, steps, rf, hold, index)

	return m
}

func aggregateBenchmarks(
	req Request,
	sleeves []Result,
	stamps []time.Time,
	m Metrics,
	steps, rf []float64,
	hold, index []int64,
) []Benchmark {
	names := make([]string, 0, len(req.Instruments))
	for _, held := range req.Instruments {
		names = append(names, held.Symbol.Ticker)
	}

	marks := []Benchmark{
		{Kind: BenchmarkHold, Symbol: strings.Join(names, ", "), Basis: m.Basis, curve: hold},
		{Kind: BenchmarkIndex, Symbol: req.IndexSymbol, Basis: BasisTotal, curve: index},
	}

	for i := range marks {
		if marks[i].curve == nil {
			marks[i].Unavailable = missingBenchmark(sleeves, marks[i].Kind)
			continue
		}

		for _, sleeve := range sleeves {
			if found, ok := benchmarkOf(sleeve.Metrics.Benchmarks, marks[i].Kind); ok {
				marks[i].DividendsCents += found.DividendsCents
				marks[i].FeesCents += found.FeesCents
			}
		}

		marks[i].score(stamps, steps, rf, m.BarsPerYear)
		if marks[i].Unavailable == "" {
			marks[i].ExcessPct = m.ReturnPct - marks[i].ReturnPct
		}
	}

	return marks
}

func benchmarkOf(marks []Benchmark, kind string) (Benchmark, bool) {
	for _, mark := range marks {
		if mark.Kind == kind {
			return mark, true
		}
	}
	return Benchmark{}, false
}

func missingBenchmark(sleeves []Result, kind string) string {
	for _, sleeve := range sleeves {
		if mark, ok := benchmarkOf(sleeve.Metrics.Benchmarks, kind); ok && mark.Unavailable != "" {
			return mark.Unavailable
		}
	}
	return "one of the basket's symbols has no comparable curve"
}

func sleeveStatsOf(sleeves []Result, instruments []Instrument, shares []int64, capital int64) []SymbolStats {
	stats := make([]SymbolStats, 0, len(sleeves))
	for i, sleeve := range sleeves {
		stats = append(stats, sleeveStats(sleeve, instruments[i].Symbol, shares[i], capital))
	}
	return stats
}

// The book-level tally the sleeve's own engine produced, plus the risk and return numbers
// that only exist once its curve is finished.
func sleeveStats(sleeve Result, symbol Symbol, capital, total int64) SymbolStats {
	stats := SymbolStats{Symbol: symbol.Ticker, Basis: sleeve.Metrics.Basis}
	if len(sleeve.Metrics.Symbols) > 0 {
		stats = sleeve.Metrics.Symbols[0]
	}

	m := sleeve.Metrics

	stats.Symbol = symbol.Ticker
	stats.SymbolID = symbol.ID
	stats.CapitalCents = capital
	stats.FinalEquityCents = m.FinalEquityCents
	stats.DividendsCents = m.DividendsCents
	stats.BorrowCents = m.BorrowCents
	stats.TimeInMarket = m.TimeInMarket
	stats.ReturnPct = m.ReturnPct
	stats.CAGRPct = m.CAGRPct
	stats.VolatilityPct = m.VolatilityPct
	stats.MaxDrawdown = m.MaxDrawdown
	stats.Sharpe = m.Sharpe
	stats.Sortino = m.Sortino
	stats.Calmar = m.Calmar
	stats.ProfitFactor = m.ProfitFactor
	stats.ExpectancyCents = m.ExpectancyCents
	stats.Benchmarks = m.Benchmarks

	// Contribution is against the whole run's capital, not the sleeve's share: it answers
	// what this stock did to the basket's return, which is what the column is read for.
	stats.ContributionPct = 0
	if total > 0 {
		stats.ContributionPct = float64(stats.PnLCents) / float64(total) * 100
	}

	return stats
}

func barsPerYear(req Request) float64 {
	if req.BarsPerYear > 0 {
		return req.BarsPerYear
	}
	return market.TradingDaysPerYear
}
