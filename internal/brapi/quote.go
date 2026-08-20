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

type Quote struct {
	Symbol              string  `json:"symbol"`
	ShortName           string  `json:"shortName"`
	LongName            string  `json:"longName"`
	Currency            string  `json:"currency"`
	RegularMarketPrice  float64 `json:"regularMarketPrice"`
	RegularMarketVolume int64   `json:"regularMarketVolume"`
	RegularMarketTime   Time    `json:"regularMarketTime"`
	HistoricalDataPrice []Bar   `json:"historicalDataPrice"`
}

type Bar struct {
	Date          int64   `json:"date"`
	Open          float64 `json:"open"`
	High          float64 `json:"high"`
	Low           float64 `json:"low"`
	Close         float64 `json:"close"`
	AdjustedClose float64 `json:"adjustedClose"`
	Volume        int64   `json:"volume"`
}

func (b Bar) TS() time.Time { return time.Unix(b.Date, 0).UTC() }

type Time struct {
	time.Time
}

func (t *Time) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" || text == `""` {
		return nil
	}

	if text[0] == '"' {
		parsed, err := time.Parse(time.RFC3339, strings.Trim(text, `"`))
		if err != nil {
			return err
		}
		t.Time = parsed.UTC()
		return nil
	}

	seconds, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return err
	}
	t.Time = time.Unix(seconds, 0).UTC()
	return nil
}

type QuoteOptions struct {
	Range    string
	Interval string
	From     time.Time
	To       time.Time
}

func (c *Client) Quote(ctx context.Context, tickers []string, opts QuoteOptions) ([]Quote, error) {
	if len(tickers) == 0 {
		return nil, errors.New("brapi: quote requires at least one ticker")
	}

	normalised := make([]string, 0, len(tickers))
	for _, ticker := range tickers {
		ticker = strings.ToUpper(strings.TrimSpace(ticker))
		if err := validateTicker(ticker); err != nil {
			return nil, err
		}
		normalised = append(normalised, ticker)
	}

	params := url.Values{}
	if opts.Range != "" {
		params.Set("range", opts.Range)
	}
	if opts.Interval != "" {
		params.Set("interval", opts.Interval)
	}
	if !opts.From.IsZero() {
		params.Set("startDate", opts.From.UTC().Format(time.DateOnly))
	}
	if !opts.To.IsZero() {
		params.Set("endDate", opts.To.UTC().Format(time.DateOnly))
	}

	var payload struct {
		Results []Quote `json:"results"`
	}

	path := "/quote/" + strings.Join(normalised, ",")
	if err := c.get(ctx, path, params, &payload); err != nil {
		return nil, err
	}
	if len(payload.Results) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, strings.Join(normalised, ","))
	}

	return payload.Results, nil
}
