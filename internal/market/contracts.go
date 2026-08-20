package market

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
)

const (
	ContractsFile = "data/contracts.json"
	IndexesDir    = "data/indexes"
	HolidaysFile  = "data/b3_holidays.json"

	monthCodes = "FGHJKMNQUVXZ"
)

type Contracts struct {
	Futures []FutureContract `json:"futures"`
	Symbols []SymbolSeed     `json:"symbols"`
}

type FutureContract struct {
	Root       string  `json:"root"`
	Name       string  `json:"name"`
	LongName   string  `json:"long_name"`
	PointValue float64 `json:"point_value"`
	TickSize   float64 `json:"tick_size"`
	LotSize    int32   `json:"lot_size"`
	Months     string  `json:"months"`
}

type SymbolSeed struct {
	Ticker     string   `json:"ticker"`
	Kind       string   `json:"kind"`
	ShortName  string   `json:"short_name"`
	LongName   string   `json:"long_name"`
	Currency   string   `json:"currency"`
	LotSize    *int32   `json:"lot_size"`
	TickSize   *float64 `json:"tick_size"`
	PointValue *float64 `json:"point_value"`
	Tracked    bool     `json:"tracked"`
}

func LoadContracts(fsys fs.FS, name string) (Contracts, error) {
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return Contracts{}, fmt.Errorf("reading %s: %w", name, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var contracts Contracts
	if err := decoder.Decode(&contracts); err != nil {
		return Contracts{}, fmt.Errorf("parsing %s: %w", name, err)
	}
	if err := contracts.validate(); err != nil {
		return Contracts{}, fmt.Errorf("%s: %w", name, err)
	}
	return contracts, nil
}

func (c Contracts) validate() error {
	roots := make(map[string]bool, len(c.Futures))
	for i, future := range c.Futures {
		switch {
		case future.Root == "":
			return fmt.Errorf("futures[%d]: root is required", i)
		case roots[future.Root]:
			return fmt.Errorf("futures[%d]: duplicate root %q", i, future.Root)
		case future.PointValue <= 0:
			return fmt.Errorf("futures[%d] (%s): point_value must be positive", i, future.Root)
		case future.TickSize <= 0:
			return fmt.Errorf("futures[%d] (%s): tick_size must be positive", i, future.Root)
		case future.LotSize <= 0:
			return fmt.Errorf("futures[%d] (%s): lot_size must be positive", i, future.Root)
		case future.Months == "":
			return fmt.Errorf("futures[%d] (%s): months is required", i, future.Root)
		}
		for _, code := range future.Months {
			if !strings.ContainsRune(monthCodes, code) {
				return fmt.Errorf("futures[%d] (%s): unknown month code %q", i, future.Root, string(code))
			}
		}
		roots[future.Root] = true
	}

	tickers := make(map[string]bool, len(c.Symbols))
	for i, seed := range c.Symbols {
		switch {
		case seed.Ticker == "":
			return fmt.Errorf("symbols[%d]: ticker is required", i)
		case tickers[seed.Ticker]:
			return fmt.Errorf("symbols[%d]: duplicate ticker %q", i, seed.Ticker)
		case seed.LotSize != nil && *seed.LotSize <= 0:
			return fmt.Errorf("symbols[%d] (%s): lot_size must be positive", i, seed.Ticker)
		case seed.TickSize != nil && *seed.TickSize <= 0:
			return fmt.Errorf("symbols[%d] (%s): tick_size must be positive", i, seed.Ticker)
		case seed.PointValue != nil && *seed.PointValue <= 0:
			return fmt.Errorf("symbols[%d] (%s): point_value must be positive", i, seed.Ticker)
		}
		if seed.Kind != "" {
			if _, err := ParseKind(seed.Kind); err != nil {
				return fmt.Errorf("symbols[%d] (%s): %w", i, seed.Ticker, err)
			}
		}
		tickers[seed.Ticker] = true
	}

	return nil
}

func (c Contracts) Roots() []string {
	roots := make([]string, 0, len(c.Futures))
	for _, future := range c.Futures {
		roots = append(roots, future.Root)
	}
	return roots
}
