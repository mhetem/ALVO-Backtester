package market

import (
	"fmt"
	"slices"
	"strings"
)

type ExistingSymbol struct {
	Ticker  string
	Kind    string
	Active  bool
	Tracked bool
}

type DesiredSymbol struct {
	Ticker     string
	ShortName  string
	LongName   string
	Kind       Kind
	Currency   string
	LotSize    int32
	TickSize   float64
	PointValue *float64
	Tracked    bool
}

type Plan struct {
	Symbols    []DesiredSymbol
	Created    []string
	Admissions []string
	Departures []string
}

func BuildPlan(lists []IndexList, contracts Contracts, existing []ExistingSymbol) (Plan, error) {
	desired := map[string]*DesiredSymbol{}

	add := func(symbol DesiredSymbol) *DesiredSymbol {
		if current, ok := desired[symbol.Ticker]; ok {
			current.Tracked = current.Tracked || symbol.Tracked
			return current
		}
		entry := symbol
		desired[symbol.Ticker] = &entry
		return &entry
	}

	admitted := map[string]bool{}
	unclassified := []string{}

	for _, list := range lists {
		for _, ticker := range list.Tickers {
			admitted[ticker] = true

			kind, err := ClassifyTicker(ticker)
			if err != nil {
				unclassified = append(unclassified, fmt.Sprintf("%s (%s)", ticker, list.File))
				continue
			}

			add(DesiredSymbol{
				Ticker:   ticker,
				Kind:     kind,
				Currency: DefaultCurrency,
				LotSize:  DefaultLotSize(kind),
				TickSize: DefaultTickSize(kind),
				Tracked:  true,
			})
		}
	}

	if len(unclassified) > 0 {
		return Plan{}, fmt.Errorf("unclassifiable tickers in the admission lists: %s", strings.Join(unclassified, ", "))
	}

	for _, future := range contracts.Futures {
		pointValue := future.PointValue
		add(DesiredSymbol{
			Ticker:     future.Root,
			ShortName:  future.Name,
			LongName:   future.LongName,
			Kind:       KindFuture,
			Currency:   DefaultCurrency,
			LotSize:    future.LotSize,
			TickSize:   future.TickSize,
			PointValue: &pointValue,
		})
	}

	for _, seed := range contracts.Symbols {
		entry, ok := desired[seed.Ticker]
		if !ok {
			kind, err := ClassifyTicker(seed.Ticker)
			if err != nil {
				if seed.Kind == "" {
					return Plan{}, fmt.Errorf("%s: %w", ContractsFile, err)
				}
				if kind, err = ParseKind(seed.Kind); err != nil {
					return Plan{}, fmt.Errorf("%s: %w", ContractsFile, err)
				}
			}
			entry = add(DesiredSymbol{
				Ticker:   seed.Ticker,
				Kind:     kind,
				Currency: DefaultCurrency,
				LotSize:  DefaultLotSize(kind),
				TickSize: DefaultTickSize(kind),
			})
		}
		applySeed(entry, seed)
	}

	known := make(map[string]ExistingSymbol, len(existing))
	for _, symbol := range existing {
		known[symbol.Ticker] = symbol
	}

	plan := Plan{
		Symbols:    make([]DesiredSymbol, 0, len(desired)),
		Created:    []string{},
		Admissions: []string{},
		Departures: []string{},
	}

	for _, entry := range desired {
		plan.Symbols = append(plan.Symbols, *entry)
	}
	slices.SortFunc(plan.Symbols, func(a, b DesiredSymbol) int { return strings.Compare(a.Ticker, b.Ticker) })

	for _, symbol := range plan.Symbols {
		previous, ok := known[symbol.Ticker]
		if !ok {
			plan.Created = append(plan.Created, symbol.Ticker)
		}
		if symbol.Tracked && (!ok || !previous.Tracked) {
			plan.Admissions = append(plan.Admissions, symbol.Ticker)
		}
	}

	for _, symbol := range existing {
		if !symbol.Tracked || admitted[symbol.Ticker] {
			continue
		}
		if entry, ok := desired[symbol.Ticker]; ok && entry.Tracked {
			continue
		}
		plan.Departures = append(plan.Departures, symbol.Ticker)
	}
	slices.Sort(plan.Departures)

	return plan, nil
}

func applySeed(symbol *DesiredSymbol, seed SymbolSeed) {
	if seed.Kind != "" {
		if kind, err := ParseKind(seed.Kind); err == nil && kind != symbol.Kind {
			symbol.Kind = kind
			symbol.LotSize = DefaultLotSize(kind)
			symbol.TickSize = DefaultTickSize(kind)
		}
	}
	if seed.ShortName != "" {
		symbol.ShortName = seed.ShortName
	}
	if seed.LongName != "" {
		symbol.LongName = seed.LongName
	}
	if seed.Currency != "" {
		symbol.Currency = seed.Currency
	}
	if seed.LotSize != nil {
		symbol.LotSize = *seed.LotSize
	}
	if seed.TickSize != nil {
		symbol.TickSize = *seed.TickSize
	}
	if seed.PointValue != nil {
		value := *seed.PointValue
		symbol.PointValue = &value
	}
	if seed.Tracked {
		symbol.Tracked = true
	}
}
