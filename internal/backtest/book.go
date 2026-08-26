package backtest

import (
	"slices"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/market"
	"github.com/mhetem/ALVO-Backtester/internal/strategy"
)

type position struct {
	open        bool
	short       bool
	qty         int64
	entryTS     time.Time
	entryPrice  float64
	entryCents  int64
	feesCents   int64
	divCents    int64
	borrowCents int64
	borrowOwed  float64
	splitCents  int64
	stop        float64
	target      float64
}

// A book is one symbol's whole state inside a run: its own bars, its own indicator tape,
// its own corporate actions and its own position. Cash is the only thing a basket shares,
// which is what makes the portfolio case a loop over books rather than a second engine.
type book struct {
	symbol  Symbol
	candles []market.Candle
	acts    actions
	tape    *strategy.Tape
	costing Costing
	at      int
	pos     position
	pending intent
	stats   SymbolStats
}

func newBook(plan *strategy.Plan, held Instrument) *book {
	b := &book{
		symbol:  held.Symbol,
		candles: held.Candles,
		acts:    actionsOf(held.Candles),
		tape:    plan.NewTape(),
		costing: Costing{Costs: plan.Spec.Costs, TickSize: held.Symbol.TickSize},
	}

	b.tape.Prime(candlesFor(held.Prime))
	b.stats = SymbolStats{Symbol: held.Symbol.Ticker, Basis: b.acts.basis()}

	return b
}

func (b *book) advance(ts time.Time) (market.Candle, int, bool) {
	if b.at >= len(b.candles) || !b.candles[b.at].TS.Equal(ts) {
		return market.Candle{}, 0, false
	}

	at := b.at
	b.at++

	return b.candles[at], at, true
}

func (b *book) final(at int) bool { return at == len(b.candles)-1 }

func (b *book) held() (market.Candle, bool) {
	if b.at < 1 {
		return market.Candle{}, false
	}
	return b.candles[b.at-1], true
}

// The timeline is the union of every symbol's bars. A symbol that did not trade on a given
// stamp simply has no bar there, which is how a basket survives a ticker that was halted,
// listed late, or delisted mid-run without shifting anybody else's bars.
func timelineOf(books []*book) []time.Time {
	if len(books) == 1 {
		stamps := make([]time.Time, len(books[0].candles))
		for i, candle := range books[0].candles {
			stamps[i] = candle.TS
		}
		return stamps
	}

	seen := map[int64]time.Time{}
	for _, b := range books {
		for _, candle := range b.candles {
			seen[candle.TS.UnixNano()] = candle.TS
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
