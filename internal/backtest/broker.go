package backtest

import (
	"math"

	"github.com/mhetem/ALVO-Backtester/internal/market"
	"github.com/mhetem/ALVO-Backtester/internal/strategy"
)

const tickEpsilon = 1e-9

type OrderKind int

const (
	OrderMarket OrderKind = iota
	OrderLimit
	OrderStop
)

type OrderSide int

const (
	Buy OrderSide = iota
	Sell
)

type Order struct {
	Kind  OrderKind
	Side  OrderSide
	Price float64
}

type Bar struct {
	Open  float64
	High  float64
	Low   float64
	Close float64
}

func barOf(candle market.Candle) Bar {
	return Bar{Open: candle.Open, High: candle.High, Low: candle.Low, Close: candle.Close}
}

func closeOf(candle market.Candle) Bar {
	return Bar{Open: candle.Close, High: candle.Close, Low: candle.Close, Close: candle.Close}
}

func (o Order) Fill(bar Bar) (float64, bool) {
	switch o.Kind {
	case OrderMarket:
		return bar.Open, true

	case OrderLimit:
		if o.Side == Buy {
			if bar.Low > o.Price {
				return 0, false
			}
			return math.Min(bar.Open, o.Price), true
		}
		if bar.High < o.Price {
			return 0, false
		}
		return math.Max(bar.Open, o.Price), true

	case OrderStop:
		if o.Side == Buy {
			if bar.High < o.Price {
				return 0, false
			}
			return math.Max(bar.Open, o.Price), true
		}
		if bar.Low > o.Price {
			return 0, false
		}
		return math.Min(bar.Open, o.Price), true
	}

	return 0, false
}

type Costing struct {
	Costs    strategy.Costs
	TickSize float64
}

func (c Costing) Fill(order Order, raw float64) float64 {
	price := raw
	if order.Kind != OrderLimit {
		price = slipped(price, c.Costs.SlippageBPS, order.Side == Buy)
	}
	return roundTick(price, c.TickSize, order.Side == Buy)
}

func (c Costing) Notional(qty int64, price float64) int64 {
	return int64(math.Round(float64(qty) * price * 100))
}

func (c Costing) Fees(notionalCents int64) int64 {
	if notionalCents < 0 {
		notionalCents = -notionalCents
	}
	return c.Costs.BrokerageCents + int64(math.Round(float64(notionalCents)*c.Costs.FeeBPS/10000))
}

func slipped(price, bps float64, buy bool) float64 {
	move := price * bps / 10000
	if buy {
		return price + move
	}
	return price - move
}

func roundTick(price, tick float64, up bool) float64 {
	if tick <= 0 || price <= 0 {
		return price
	}

	steps := price / tick
	nearest := math.Round(steps)
	if math.Abs(steps-nearest) <= tickEpsilon*math.Max(1, math.Abs(steps)) {
		return price
	}

	if up {
		return math.Ceil(steps) * tick
	}
	return math.Floor(steps) * tick
}
