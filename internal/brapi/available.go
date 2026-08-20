package brapi

import (
	"context"
	"net/url"
)

type Available struct {
	Indexes []string `json:"indexes"`
	Stocks  []string `json:"stocks"`
}

func (c *Client) Available(ctx context.Context, search string) (Available, error) {
	params := url.Values{}
	if search != "" {
		params.Set("search", search)
	}

	var out Available
	if err := c.get(ctx, "/available", params, &out); err != nil {
		return Available{}, err
	}
	return out, nil
}
