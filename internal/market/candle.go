package market

import (
	"errors"
	"fmt"
	"time"
)

type Candle struct {
	TS       time.Time
	Open     float64
	High     float64
	Low      float64
	Close    float64
	AdjClose *float64
	Volume   int64
}

func (c Candle) Validate() error {
	switch {
	case c.TS.IsZero():
		return errors.New("timestamp is zero")
	case c.Open <= 0 || c.High <= 0 || c.Low <= 0 || c.Close <= 0:
		return fmt.Errorf("non-positive price (o=%g h=%g l=%g c=%g)", c.Open, c.High, c.Low, c.Close)
	case c.High < c.Low:
		return fmt.Errorf("high %g is below low %g", c.High, c.Low)
	case c.High < c.Open || c.High < c.Close:
		return fmt.Errorf("high %g is below open %g or close %g", c.High, c.Open, c.Close)
	case c.Low > c.Open || c.Low > c.Close:
		return fmt.Errorf("low %g is above open %g or close %g", c.Low, c.Open, c.Close)
	case c.AdjClose != nil && *c.AdjClose <= 0:
		return fmt.Errorf("non-positive adjusted close %g", *c.AdjClose)
	case c.Volume < 0:
		return fmt.Errorf("negative volume %d", c.Volume)
	}
	return nil
}

func (c *Calendar) BucketOpen(tf Timeframe, ts time.Time) (time.Time, error) {
	if !tf.Valid() {
		return time.Time{}, fmt.Errorf("unknown timeframe %q", tf)
	}

	session, ok := c.Session(ts)
	if !ok {
		return time.Time{}, fmt.Errorf("%s falls on %s, which is not a trading day", ts.UTC().Format(time.RFC3339), dateKey(ts.In(c.loc)))
	}

	if tf == TF1d {
		return session.Open, nil
	}

	if ts.Before(session.Open) || !ts.Before(session.Close) {
		return time.Time{}, fmt.Errorf("%s is outside the %s session %s-%s",
			ts.In(c.loc).Format("2006-01-02 15:04"),
			dateKey(session.Day),
			session.Open.In(c.loc).Format("15:04"),
			session.Close.In(c.loc).Format("15:04"),
		)
	}

	width := tf.BucketWidth()
	offset := ts.Sub(session.Open)
	return session.Open.Add(offset / width * width), nil
}

func (c *Calendar) DayBounds(from, to time.Time) (time.Time, time.Time) {
	start := c.midnight(from)
	end := c.midnight(to).AddDate(0, 0, 1)
	return start.UTC(), end.UTC()
}

func (c *Calendar) BucketCount(tf Timeframe, session Session) int {
	if tf == TF1d {
		return 1
	}
	width := tf.BucketWidth()
	if width <= 0 {
		return 0
	}
	return int((session.Duration() + width - 1) / width)
}

func (c *Calendar) FutureBuckets(tf Timeframe, after time.Time, count int) []time.Time {
	buckets := []time.Time{}
	if count < 1 || !tf.Valid() {
		return buckets
	}

	cursor := after.In(c.loc)
	for len(buckets) < count {
		if session, ok := c.Session(cursor); ok {
			for _, bucket := range c.SessionBuckets(tf, session) {
				if bucket.After(after) {
					buckets = append(buckets, bucket.UTC())
					if len(buckets) == count {
						return buckets
					}
				}
			}
		}

		next, err := c.NextTradingDay(cursor)
		if err != nil {
			return buckets
		}
		cursor = next
	}

	return buckets
}

func (c *Calendar) SessionBuckets(tf Timeframe, session Session) []time.Time {
	count := c.BucketCount(tf, session)
	buckets := make([]time.Time, 0, count)

	if tf == TF1d {
		return append(buckets, session.Open)
	}

	width := tf.BucketWidth()
	for i := range count {
		buckets = append(buckets, session.Open.Add(time.Duration(i)*width))
	}
	return buckets
}
