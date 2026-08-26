package backtest

import (
	"math"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/market"
	"github.com/mhetem/ALVO-Backtester/internal/strategy"
)

const splitEpsilon = 1e-9

type intentKind int

const (
	intentNone intentKind = iota
	intentEnter
	intentExit
)

type bracket struct {
	kind  strategy.LevelType
	value float64
	set   bool
}

func (b bracket) distance(entry float64) float64 {
	switch {
	case !b.set:
		return 0
	case b.kind == strategy.LevelPct:
		return entry * b.value
	default:
		return b.value
	}
}

func (b bracket) level(entry float64, above bool) float64 {
	if !b.set {
		return 0
	}
	if above {
		return entry + b.distance(entry)
	}
	return entry - b.distance(entry)
}

type intent struct {
	kind   intentKind
	short  bool
	stop   bracket
	target bracket
}

type engine struct {
	req     Request
	plan    *strategy.Plan
	books   []*book
	stamps  []time.Time
	cash    int64
	open    int
	trades  []Trade
	equity  []EquityPoint
	hold    []int64
	index   []int64
	metrics Metrics
	seq     int32
}

func newEngine(req Request) *engine {
	e := &engine{
		req:    req,
		plan:   req.Plan,
		books:  make([]*book, 0, len(req.Instruments)),
		cash:   req.Capital,
		trades: []Trade{},
	}

	for _, held := range req.Instruments {
		e.books = append(e.books, newBook(req.Plan, held))
	}

	e.stamps = timelineOf(e.books)
	e.equity = make([]EquityPoint, 0, len(e.stamps))

	e.metrics.CapitalCents = req.Capital
	e.metrics.MaxPositions = req.MaxPositions

	return e
}

func (e *engine) run() Result {
	for _, ts := range e.stamps {
		for _, b := range e.books {
			candle, at, ok := b.advance(ts)
			if !ok {
				continue
			}

			e.credit(b, at)
			e.charge(b, at)
			e.adjust(b, at, candle)
			e.fill(b, candle)
			e.brackets(b, candle)
			b.tape.Push(candleFor(candle))

			if b.final(at) {
				e.closeOut(b, candle)
			} else {
				e.signal(b)
			}
		}

		e.mark(ts)
	}

	e.summarize()

	return Result{Trades: e.trades, Equity: e.equity, Hold: e.hold, Index: e.index, Metrics: e.metrics}
}

func (e *engine) periods() float64 {
	if e.req.BarsPerYear > 0 {
		return e.req.BarsPerYear
	}
	return market.TradingDaysPerYear
}

func (e *engine) credit(b *book, at int) {
	if !b.pos.open {
		return
	}

	perShare := b.acts.dividendAt(at)
	if perShare <= 0 {
		return
	}

	// The holder of record collects the dividend; whoever borrowed the shares to sell them
	// short owes it. Same number, opposite sign.
	paid := int64(math.Round(float64(b.pos.qty) * perShare * 100))
	if b.pos.short {
		paid = -paid
	}

	e.cash += paid
	b.pos.divCents += paid
	b.stats.DividendsCents += paid
	e.metrics.DividendsCents += paid
	e.metrics.DividendEvents++
}

// Borrow accrues on the value of the shares owed at the previous close, for the same
// reason the dividend is credited there: the fee is rent on a position that was already
// held when the bar opened. It is carried as a float and only whole cents are moved, so a
// small short does not accrue nothing at all through a rounding floor.
func (e *engine) charge(b *book, at int) {
	if !b.pos.open || !b.pos.short || at == 0 || e.req.Borrow == nil {
		return
	}

	prev := b.candles[at-1]
	rate := e.req.Borrow.PerPeriod(b.symbol.Ticker, prev.TS, e.periods())
	if rate <= 0 {
		return
	}

	b.pos.borrowOwed += float64(b.costing.Notional(b.pos.qty, prev.Close)) * rate
	e.settleBorrow(b, int64(b.pos.borrowOwed))
}

func (e *engine) settleBorrow(b *book, cents int64) {
	if cents <= 0 {
		return
	}

	b.pos.borrowOwed -= float64(cents)
	e.cash -= cents
	b.pos.borrowCents += cents
	b.stats.BorrowCents += cents
	e.metrics.BorrowCents += cents
}

// A split multiplies the share count and divides the price by the same number, so a
// position carried through one keeps its value and changes its shape. Brackets move with
// it, because a stop quoted in pre-split money would be hit on the open.
func (e *engine) adjust(b *book, at int, candle market.Candle) {
	factor := b.acts.factorAt(at)
	if factor <= 0 {
		return
	}

	e.metrics.SplitEvents++
	if !b.pos.open {
		return
	}

	price := candle.Open
	if price <= 0 {
		price = candle.Close
	}

	exact := float64(b.pos.qty) * factor
	qty := int64(math.Floor(exact + splitEpsilon))

	// A grouping that leaves less than a whole share pays the holder out instead: there is
	// nothing left to carry, and a fractional position would be a fiction.
	if qty < 1 {
		e.settle(b, candle, price, exact)
		return
	}

	e.cashOut(b, exact-float64(qty), price)

	// entryCents stays put: it is what the position cost, and a split does not refund or
	// charge anything. After a grouping leaves a fraction behind, qty x entryPrice no
	// longer reproduces it, and that gap is exactly the basis of the shares cashed out —
	// which is what makes the trade's profit come out right against the equity curve.
	b.pos.qty = qty
	b.pos.entryPrice /= factor
	if b.pos.stop > 0 {
		b.pos.stop /= factor
	}
	if b.pos.target > 0 {
		b.pos.target /= factor
	}

	e.metrics.SplitsApplied++
}

func (e *engine) cashOut(b *book, shares, price float64) {
	if shares <= 0 || price <= 0 {
		return
	}

	cash := int64(math.Round(shares * price * 100))
	if cash == 0 {
		return
	}
	if b.pos.short {
		cash = -cash
	}

	e.cash += cash
	b.pos.splitCents += cash
	e.metrics.SplitCashCents += cash
}

func (e *engine) settle(b *book, candle market.Candle, price, shares float64) {
	e.cashOut(b, shares, price)

	gross := -b.pos.entryCents
	if b.pos.short {
		gross = b.pos.entryCents
	}

	e.record(b, candle.TS, price, gross, 0, ReasonSplit)
}

func (e *engine) signal(b *book) {
	b.pending = intent{}

	if b.pos.open {
		if b.tape.Exit(e.plan.Leg(b.pos.short)) {
			b.pending = intent{kind: intentExit}
		}
		return
	}

	// Long is offered the bar first. A spec whose two entries fire on the same close has
	// to resolve to one position somehow, and a fixed order is the only resolution that
	// stays deterministic across runs.
	for _, short := range []bool{false, true} {
		leg := e.plan.Leg(short)
		if !leg.Trades() || !b.tape.Entry(leg) {
			continue
		}

		stop, ok := e.bracketFor(b, leg.Stop)
		if !ok {
			e.metrics.SkippedEntries++
			return
		}

		target, ok := e.bracketFor(b, leg.Target)
		if !ok {
			e.metrics.SkippedEntries++
			return
		}

		b.pending = intent{kind: intentEnter, short: short, stop: stop, target: target}
		return
	}
}

func (e *engine) bracketFor(b *book, level *strategy.Bracket) (bracket, bool) {
	if level == nil {
		return bracket{}, true
	}
	if level.Level.Type == strategy.LevelPct {
		return bracket{kind: strategy.LevelPct, value: level.Level.Value, set: true}, true
	}

	span, known := b.tape.Slot(level.Slot, 0)
	if !known || span <= 0 {
		return bracket{}, false
	}

	return bracket{kind: strategy.LevelATR, value: level.Level.Mult * span, set: true}, true
}

func (e *engine) fill(b *book, candle market.Candle) {
	pending := b.pending
	b.pending = intent{}

	if pending.kind == intentNone {
		return
	}

	switch {
	case pending.kind == intentEnter && !b.pos.open:
		e.enter(b, candle, pending)
	case pending.kind == intentExit && b.pos.open:
		e.exit(b, candle, barOf(candle), Order{Kind: OrderMarket, Side: closingSide(b.pos.short)}, ReasonSignal)
	}
}

// A long is opened by buying and closed by selling; a short is the mirror. Everything
// downstream reads the direction off the position rather than assuming one.
func closingSide(short bool) OrderSide {
	if short {
		return Buy
	}
	return Sell
}

func (e *engine) enter(b *book, candle market.Candle, want intent) {
	// The seat count is checked at the fill, not at the signal: another symbol in the
	// basket may have taken the last one in between, and a strategy that could not have
	// known that is exactly what a portfolio run is measuring.
	if e.open >= e.req.MaxPositions {
		e.metrics.CrowdedOut++
		e.metrics.SkippedEntries++
		return
	}

	if want.short && e.req.Borrow != nil && !e.req.Borrow.Available(b.symbol.Ticker, candle.TS) {
		e.metrics.ShortsUnavailable++
		e.metrics.SkippedEntries++
		return
	}

	side := Buy
	if want.short {
		side = Sell
	}
	order := Order{Kind: OrderMarket, Side: side}

	raw, ok := order.Fill(barOf(candle))
	if !ok {
		return
	}

	price := b.costing.Fill(order, raw)
	if price <= 0 {
		return
	}

	qty := e.size(b, price, want.stop.distance(price))
	if qty < 1 {
		e.metrics.SkippedEntries++
		return
	}

	notional := b.costing.Notional(qty, price)
	fees := b.costing.Fees(notional)

	// Selling short pays the proceeds into cash and leaves the shares owed; the debt is
	// carried by marking the position at every close rather than by holding a negative.
	if want.short {
		e.cash += notional - fees
	} else {
		e.cash -= notional + fees
	}

	b.pos = position{
		open:       true,
		short:      want.short,
		qty:        qty,
		entryTS:    candle.TS,
		entryPrice: price,
		entryCents: notional,
		feesCents:  fees,
		stop:       want.stop.level(price, want.short),
		target:     want.target.level(price, !want.short),
	}
	e.open++
}

func (e *engine) brackets(b *book, candle market.Candle) {
	if !b.pos.open {
		return
	}

	bar := barOf(candle)
	side := closingSide(b.pos.short)
	stop := Order{Kind: OrderStop, Side: side, Price: b.pos.stop}
	target := Order{Kind: OrderLimit, Side: side, Price: b.pos.target}

	hitStop := b.pos.stop > 0 && resting(stop, bar)
	hitTarget := b.pos.target > 0 && resting(target, bar)

	if hitStop && hitTarget {
		e.metrics.AmbiguousBars++
	}

	switch {
	case hitStop:
		e.exit(b, candle, bar, stop, ReasonStop)
	case hitTarget:
		e.exit(b, candle, bar, target, ReasonTarget)
	}
}

func (e *engine) closeOut(b *book, candle market.Candle) {
	b.pending = intent{}
	if !b.pos.open {
		return
	}
	e.exit(b, candle, closeOf(candle), Order{Kind: OrderMarket, Side: closingSide(b.pos.short)}, ReasonEndOfRun)
}

func (e *engine) exit(b *book, candle market.Candle, bar Bar, order Order, reason string) {
	raw, ok := order.Fill(bar)
	if !ok {
		return
	}

	price := b.costing.Fill(order, raw)
	notional := b.costing.Notional(b.pos.qty, price)
	fees := b.costing.Fees(notional)

	gross := notional - b.pos.entryCents
	if b.pos.short {
		e.cash -= notional + fees
		gross = b.pos.entryCents - notional
	} else {
		e.cash += notional - fees
	}

	e.record(b, candle.TS, price, gross, fees, reason)
}

func (e *engine) record(b *book, ts time.Time, price float64, gross, fees int64, reason string) {
	e.settleBorrow(b, int64(math.Round(b.pos.borrowOwed)))

	total := b.pos.feesCents + fees
	e.seq++

	trade := Trade{
		Seq:            e.seq,
		SymbolID:       b.symbol.ID,
		Symbol:         b.symbol.Ticker,
		Side:           sideName(b.pos.short),
		Qty:            b.pos.qty,
		EntryTS:        b.pos.entryTS,
		EntryPrice:     b.pos.entryPrice,
		ExitTS:         ts,
		ExitPrice:      price,
		PnLCents:       gross + b.pos.divCents + b.pos.splitCents - b.pos.borrowCents - total,
		FeesCents:      total,
		DividendsCents: b.pos.divCents,
		BorrowCents:    b.pos.borrowCents,
		SplitCashCents: b.pos.splitCents,
		ExitReason:     reason,
	}

	e.trades = append(e.trades, trade)
	b.stats.Trades++
	b.stats.PnLCents += trade.PnLCents
	b.stats.FeesCents += total

	switch {
	case trade.PnLCents > 0:
		b.stats.Wins++
	case trade.PnLCents < 0:
		b.stats.Losses++
	}

	e.open--
	b.pos = position{}
}

func (e *engine) mark(ts time.Time) {
	equity := e.cash
	held := false

	for _, b := range e.books {
		if !b.pos.open {
			continue
		}

		candle, ok := b.held()
		if !ok {
			continue
		}

		value := b.costing.Notional(b.pos.qty, candle.Close)
		if b.pos.short {
			value = -value
		}

		equity += value
		b.stats.BarsInMarket++
		held = true
	}

	if held {
		e.metrics.BarsInMarket++
	}

	e.equity = append(e.equity, EquityPoint{TS: ts, Cents: equity})
}

func resting(order Order, bar Bar) bool {
	_, filled := order.Fill(bar)
	return filled
}
