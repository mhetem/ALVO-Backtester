package brapi

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	FuturesListLimit = 100
	FuturesFloor     = "2025-06-10"
)

func FuturesFloorDate() time.Time {
	day, _ := time.Parse(time.DateOnly, FuturesFloor)
	return day
}

type FutureContract struct {
	Symbol             string   `json:"symbol"`
	UnderlyingAsset    string   `json:"underlyingAsset"`
	AssetDescription   string   `json:"assetDescription"`
	Segment            string   `json:"segment"`
	QuotationType      string   `json:"quotationType"`
	ExpirationDate     string   `json:"expirationDate"`
	FirstTradeDate     string   `json:"firstTradeDate"`
	LastTradeDate      string   `json:"lastTradeDate"`
	ContractMultiplier *float64 `json:"contractMultiplier"`
	AllocationRoundLot *int32   `json:"allocationRoundLot"`
	TradingCurrency    string   `json:"tradingCurrency"`
	ISIN               string   `json:"isin"`

	History []FutureBar `json:"history"`
}

type FutureBar struct {
	Date       int64    `json:"date"`
	Open       *float64 `json:"open"`
	High       *float64 `json:"high"`
	Low        *float64 `json:"low"`
	Close      *float64 `json:"close"`
	Average    *float64 `json:"average"`
	Settlement *float64 `json:"settlement"`
	Volume     *int64   `json:"volume"`
	Trades     *int64   `json:"trades"`
}

func (b FutureBar) TS() time.Time { return time.Unix(b.Date, 0).UTC() }

type futuresPagination struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	Total      int `json:"total"`
	TotalPages int `json:"totalPages"`
}

func (c *Client) FuturesList(ctx context.Context, includeExpired bool) ([]FutureContract, error) {
	contracts := []FutureContract{}

	for page := 1; ; page++ {
		params := url.Values{}
		params.Set("limit", strconv.Itoa(FuturesListLimit))
		params.Set("page", strconv.Itoa(page))
		if includeExpired {
			params.Set("includeExpired", "true")
		}

		var payload struct {
			Futures    []FutureContract  `json:"futures"`
			Pagination futuresPagination `json:"pagination"`
		}
		if err := c.get(ctx, "/v2/futures/list", params, &payload); err != nil {
			return nil, fmt.Errorf("listing futures page %d: %w", page, err)
		}

		contracts = append(contracts, payload.Futures...)

		if len(payload.Futures) == 0 || page >= payload.Pagination.TotalPages {
			return contracts, nil
		}
	}
}

type FutureTermContract struct {
	FutureContract
	FutureBar
}

func (c *Client) FuturesTermStructure(ctx context.Context, asset string) ([]FutureTermContract, error) {
	asset = strings.ToUpper(strings.TrimSpace(asset))
	if asset == "" {
		return nil, errors.New("brapi: term structure requires an asset")
	}

	params := url.Values{}
	params.Set("asset", asset)

	var payload struct {
		Asset     string               `json:"asset"`
		Contracts []FutureTermContract `json:"contracts"`
	}
	if err := c.get(ctx, "/v2/futures/term-structure", params, &payload); err != nil {
		return nil, fmt.Errorf("reading the %s term structure: %w", asset, err)
	}

	return payload.Contracts, nil
}

func (c *Client) FuturesHistory(ctx context.Context, symbol string, from time.Time) (FutureContract, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return FutureContract{}, errors.New("brapi: futures history requires a symbol")
	}

	params := url.Values{}
	params.Set("symbol", symbol)
	if !from.IsZero() {
		params.Set("startDate", from.UTC().Format(time.DateOnly))
	}

	var payload struct {
		Future FutureContract `json:"future"`
	}
	if err := c.get(ctx, "/v2/futures/historical", params, &payload); err != nil {
		return FutureContract{}, err
	}
	if payload.Future.Symbol == "" {
		return FutureContract{}, fmt.Errorf("%w: %s", ErrNotFound, symbol)
	}

	return payload.Future, nil
}
