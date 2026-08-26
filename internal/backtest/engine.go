package backtest

import (
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/market"
	"github.com/mhetem/ALVO-Backtester/internal/strategy"
)

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
	stop   bracket
	target bracket
}

type position struct {
	open       bool
	qty        int64
	entryTS    time.Time
	entryPrice float64
	entryCents int64
	feesCents  int64
	stop       float64
	target     float64
}

type engine struct {
	req     Request
	plan    *strategy.Plan
	costing Costing
	tape    *strategy.Tape
	cash    int64
	pos     position
	pending intent
	trades  []Trade
	equity  []EquityPoint
	metrics Metrics
	seq     int32
}

func newEngine(req Request) *engine {
	e := &engine{
		req:     req,
		plan:    req.Plan,
		costing: Costing{Costs: req.Plan.Spec.Costs, TickSize: req.Symbol.TickSize},
		tape:    req.Plan.NewTape(),
		cash:    req.Capital,
		trades:  []Trade{},
		equity:  make([]EquityPoint, 0, len(req.Candles)),
	}

	e.metrics.CapitalCents = req.Capital
	e.tape.Prime(candlesFor(req.Prime))

	return e
}

func (e *engine) run() Result {
	last := len(e.req.Candles) - 1

	for i, candle := range e.req.Candles {
		e.fill(candle)
		e.brackets(candle)
		e.tape.Push(candleFor(candle))

		if i < last {
			e.signal()
		} else {
			e.closeOut(candle)
		}

		e.mark(candle)
	}

	e.summarize()

	return Result{Trades: e.trades, Equity: e.equity, Metrics: e.metrics}
}

func (e *engine) signal() {
	e.pending = intent{}

	if e.pos.open {
		if e.tape.Exit() {
			e.pending = intent{kind: intentExit}
		}
		return
	}

	if !e.tape.Entry() {
		return
	}

	stop, ok := e.bracketFor(e.plan.Stop)
	if !ok {
		e.metrics.SkippedEntries++
		return
	}

	target, ok := e.bracketFor(e.plan.Target)
	if !ok {
		e.metrics.SkippedEntries++
		return
	}

	e.pending = intent{kind: intentEnter, stop: stop, target: target}
}

func (e *engine) bracketFor(level *strategy.Bracket) (bracket, bool) {
	if level == nil {
		return bracket{}, true
	}
	if level.Level.Type == strategy.LevelPct {
		return bracket{kind: strategy.LevelPct, value: level.Level.Value, set: true}, true
	}

	span, known := e.tape.Slot(level.Slot, 0)
	if !known || span <= 0 {
		return bracket{}, false
	}

	return bracket{kind: strategy.LevelATR, value: level.Level.Mult * span, set: true}, true
}

func (e *engine) fill(candle market.Candle) {
	pending := e.pending
	e.pending = intent{}

	if pending.kind == intentNone {
		return
	}

	switch {
	case pending.kind == intentEnter && !e.pos.open:
		e.enter(candle, pending)
	case pending.kind == intentExit && e.pos.open:
		e.exit(candle, barOf(candle), Order{Kind: OrderMarket, Side: Sell}, ReasonSignal)
	}
}

func (e *engine) enter(candle market.Candle, want intent) {
	order := Order{Kind: OrderMarket, Side: Buy}

	raw, ok := order.Fill(barOf(candle))
	if !ok {
		return
	}

	price := e.costing.Fill(order, raw)
	if price <= 0 {
		return
	}

	qty := e.size(price, want.stop.distance(price))
	if qty < 1 {
		e.metrics.SkippedEntries++
		return
	}

	notional := e.costing.Notional(qty, price)
	fees := e.costing.Fees(notional)
	e.cash -= notional + fees

	e.pos = position{
		open:       true,
		qty:        qty,
		entryTS:    candle.TS,
		entryPrice: price,
		entryCents: notional,
		feesCents:  fees,
		stop:       want.stop.level(price, false),
		target:     want.target.level(price, true),
	}
}

func (e *engine) brackets(candle market.Candle) {
	if !e.pos.open {
		return
	}

	bar := barOf(candle)
	stop := Order{Kind: OrderStop, Side: Sell, Price: e.pos.stop}
	target := Order{Kind: OrderLimit, Side: Sell, Price: e.pos.target}

	hitStop := e.pos.stop > 0 && resting(stop, bar)
	hitTarget := e.pos.target > 0 && resting(target, bar)

	if hitStop && hitTarget {
		e.metrics.AmbiguousBars++
	}

	switch {
	case hitStop:
		e.exit(candle, bar, stop, ReasonStop)
	case hitTarget:
		e.exit(candle, bar, target, ReasonTarget)
	}
}

func (e *engine) closeOut(candle market.Candle) {
	e.pending = intent{}
	if !e.pos.open {
		return
	}
	e.exit(candle, closeOf(candle), Order{Kind: OrderMarket, Side: Sell}, ReasonEndOfRun)
}

func (e *engine) exit(candle market.Candle, bar Bar, order Order, reason string) {
	raw, ok := order.Fill(bar)
	if !ok {
		return
	}

	price := e.costing.Fill(order, raw)
	notional := e.costing.Notional(e.pos.qty, price)
	fees := e.costing.Fees(notional)
	e.cash += notional - fees

	total := e.pos.feesCents + fees
	e.seq++
	e.trades = append(e.trades, Trade{
		Seq:        e.seq,
		Side:       SideLong,
		Qty:        e.pos.qty,
		EntryTS:    e.pos.entryTS,
		EntryPrice: e.pos.entryPrice,
		ExitTS:     candle.TS,
		ExitPrice:  price,
		PnLCents:   notional - e.pos.entryCents - total,
		FeesCents:  total,
		ExitReason: reason,
	})

	e.pos = position{}
}

func (e *engine) mark(candle market.Candle) {
	equity := e.cash
	if e.pos.open {
		equity += e.costing.Notional(e.pos.qty, candle.Close)
		e.metrics.BarsInMarket++
	}

	e.equity = append(e.equity, EquityPoint{TS: candle.TS, Cents: equity})
}

func resting(order Order, bar Bar) bool {
	_, filled := order.Fill(bar)
	return filled
}
