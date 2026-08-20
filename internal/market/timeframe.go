package market

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

type Timeframe string

const (
	TF5m  Timeframe = "5m"
	TF15m Timeframe = "15m"
	TF30m Timeframe = "30m"
	TF1h  Timeframe = "1h"
	TF1d  Timeframe = "1d"
)

var (
	Timeframes       = []Timeframe{TF5m, TF15m, TF30m, TF1h, TF1d}
	StoredTimeframes = []Timeframe{TF5m, TF1d}
)

const BaseBucket = 5 * time.Minute

func (t Timeframe) Valid() bool { return slices.Contains(Timeframes, t) }

func (t Timeframe) Stored() bool { return slices.Contains(StoredTimeframes, t) }

func (t Timeframe) Intraday() bool { return t.Valid() && t != TF1d }

func (t Timeframe) String() string { return string(t) }

func (t Timeframe) BucketWidth() time.Duration {
	switch t {
	case TF5m:
		return 5 * time.Minute
	case TF15m:
		return 15 * time.Minute
	case TF30m:
		return 30 * time.Minute
	case TF1h:
		return time.Hour
	default:
		return 0
	}
}

func (t Timeframe) Base() Timeframe {
	if t == TF1d {
		return TF1d
	}
	return TF5m
}

func (t Timeframe) BaseBars() int {
	if !t.Intraday() {
		return 0
	}
	return int(t.BucketWidth() / BaseBucket)
}

func (t Timeframe) BrapiInterval() string {
	if t == TF1h {
		return "60m"
	}
	return string(t)
}

func ParseTimeframe(text string) (Timeframe, error) {
	tf := Timeframe(strings.ToLower(strings.TrimSpace(text)))
	if !tf.Valid() {
		return "", fmt.Errorf("unknown timeframe %q (want one of: %s)", text, JoinTimeframes(Timeframes))
	}
	return tf, nil
}

func ParseStoredTimeframe(text string) (Timeframe, error) {
	tf, err := ParseTimeframe(text)
	if err != nil {
		return "", err
	}
	if !tf.Stored() {
		return "", fmt.Errorf("timeframe %s is derived on read, not stored (want one of: %s)", tf, JoinTimeframes(StoredTimeframes))
	}
	return tf, nil
}

func JoinTimeframes(list []Timeframe) string {
	names := make([]string, 0, len(list))
	for _, tf := range list {
		names = append(names, string(tf))
	}
	return strings.Join(names, ", ")
}
