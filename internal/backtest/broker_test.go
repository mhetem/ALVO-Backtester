package backtest

import (
	"math"
	"testing"

	"github.com/mhetem/ALVO-Backtester/internal/strategy"
)

func near(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func TestOrdersFillAgainstTheBarTheyMeet(t *testing.T) {
	bar := Bar{Open: 10, High: 12, Low: 9, Close: 11}

	cases := []struct {
		name  string
		order Order
		bar   Bar
		price float64
		fills bool
	}{
		{"a market buy takes the open", Order{Kind: OrderMarket, Side: Buy}, bar, 10, true},
		{"a market sell takes the open", Order{Kind: OrderMarket, Side: Sell}, bar, 10, true},
		{"a sell limit inside the bar fills at its price", Order{Kind: OrderLimit, Side: Sell, Price: 11}, bar, 11, true},
		{"a sell limit above the high never fills", Order{Kind: OrderLimit, Side: Sell, Price: 13}, bar, 0, false},
		{"a sell limit the bar opens through fills better", Order{Kind: OrderLimit, Side: Sell, Price: 9}, bar, 10, true},
		{"a buy limit inside the bar fills at its price", Order{Kind: OrderLimit, Side: Buy, Price: 9.5}, bar, 9.5, true},
		{"a sell stop inside the bar fills at its price", Order{Kind: OrderStop, Side: Sell, Price: 9.5}, bar, 9.5, true},
		{"a sell stop below the low never fills", Order{Kind: OrderStop, Side: Sell, Price: 8.5}, bar, 0, false},
		{"a sell stop gapped through fills at the open", Order{Kind: OrderStop, Side: Sell, Price: 9.5}, Bar{Open: 9, High: 9.2, Low: 8, Close: 8.5}, 9, true},
		{"a buy stop inside the bar fills at its price", Order{Kind: OrderStop, Side: Buy, Price: 11}, bar, 11, true},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			price, filled := test.order.Fill(test.bar)
			if filled != test.fills {
				t.Fatalf("filled = %v, want %v", filled, test.fills)
			}
			if filled {
				near(t, "price", price, test.price)
			}
		})
	}
}

func TestSlippageAlwaysMovesAgainstTheFill(t *testing.T) {
	costing := Costing{Costs: strategy.Costs{SlippageBPS: 100}, TickSize: 0.01}

	near(t, "a slipped buy", costing.Fill(Order{Kind: OrderMarket, Side: Buy}, 10), 10.1)
	near(t, "a slipped sell", costing.Fill(Order{Kind: OrderMarket, Side: Sell}, 10), 9.9)
	near(t, "a slipped stop", costing.Fill(Order{Kind: OrderStop, Side: Sell, Price: 10}, 10), 9.9)
	near(t, "a resting limit", costing.Fill(Order{Kind: OrderLimit, Side: Sell, Price: 10}, 10), 10)
}

func TestAFillLandsOnATickOfTheSymbol(t *testing.T) {
	costing := Costing{Costs: strategy.Costs{}, TickSize: 0.05}

	near(t, "a buy rounds up", costing.Fill(Order{Kind: OrderMarket, Side: Buy}, 10.043), 10.05)
	near(t, "a sell rounds down", costing.Fill(Order{Kind: OrderMarket, Side: Sell}, 10.043), 10)
	near(t, "a price already on a tick", costing.Fill(Order{Kind: OrderMarket, Side: Buy}, 10.05), 10.05)
}

func TestFeesAreBrokeragePlusBasisPointsOfNotional(t *testing.T) {
	costing := Costing{Costs: strategy.Costs{BrokerageCents: 500, FeeBPS: 3.25}}

	if got := costing.Notional(100, 10.1); got != 101000 {
		t.Errorf("notional = %d cents, want 101000", got)
	}
	if got := costing.Fees(1_000_000); got != 825 {
		t.Errorf("fees = %d cents, want 825", got)
	}
}
