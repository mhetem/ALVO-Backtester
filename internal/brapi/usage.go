package brapi

import (
	"context"
	"time"

	database "github.com/mhetem/ALVO-Backtester/internal/db"
)

type UsageRecorder interface {
	RecordRequests(ctx context.Context, day time.Time, n int32) error
}

type nopRecorder struct{}

func (nopRecorder) RecordRequests(context.Context, time.Time, int32) error { return nil }

type DBRecorder struct {
	q *database.Queries
}

func NewDBRecorder(q *database.Queries) *DBRecorder { return &DBRecorder{q: q} }

func (r *DBRecorder) RecordRequests(ctx context.Context, day time.Time, n int32) error {
	_, err := r.q.RecordBrapiRequests(ctx, database.RecordBrapiRequestsParams{
		Day:      day.UTC().Truncate(24 * time.Hour),
		Requests: n,
	})
	return err
}
