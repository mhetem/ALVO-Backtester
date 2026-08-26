package backtest

import (
	"math"

	"github.com/mhetem/ALVO-Backtester/internal/strategy"
)

func (e *engine) size(b *book, price, stopDistance float64) int64 {
	if price <= 0 {
		return 0
	}

	sizing := e.plan.Spec.Sizing
	lot := max(b.symbol.LotSize, 1)

	var qty int64
	switch sizing.Type {
	case strategy.SizeFixedQty:
		qty = int64(sizing.Value)
	case strategy.SizePctEquity:
		qty = sharesFor(int64(math.Round(float64(e.cash)*sizing.Value)), price)
	case strategy.SizeFixedCash:
		qty = sharesFor(int64(sizing.Value), price)
	case strategy.SizeRiskPct:
		if stopDistance <= 0 {
			return 0
		}
		qty = int64(math.Floor(float64(e.cash) * sizing.Value / 100 / stopDistance))
	}

	qty = min(qty, e.affordable(b, price))
	qty = qty / lot * lot

	for qty > 0 && e.cost(b, qty, price) > e.cash {
		qty -= lot
	}

	return max(qty, 0)
}

func (e *engine) affordable(b *book, price float64) int64 {
	budget := float64(e.cash - b.costing.Costs.BrokerageCents)
	unit := price * 100 * (1 + b.costing.Costs.FeeBPS/10000)
	if budget <= 0 || unit <= 0 {
		return 0
	}
	return int64(math.Floor(budget / unit))
}

func (e *engine) cost(b *book, qty int64, price float64) int64 {
	notional := b.costing.Notional(qty, price)
	return notional + b.costing.Fees(notional)
}

func sharesFor(cents int64, price float64) int64 {
	if cents < 1 || price <= 0 {
		return 0
	}
	return int64(math.Floor(float64(cents) / (price * 100)))
}
