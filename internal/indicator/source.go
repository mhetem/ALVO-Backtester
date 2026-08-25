package indicator

import (
	"fmt"
	"slices"
	"strings"
)

type Source string

const (
	SourceClose Source = "close"
	SourceOpen  Source = "open"
	SourceHigh  Source = "high"
	SourceLow   Source = "low"
	SourceHL2   Source = "hl2"
	SourceHLC3  Source = "hlc3"
	SourceOHLC4 Source = "ohlc4"
)

const DefaultSource = SourceClose

var Sources = []Source{SourceClose, SourceOpen, SourceHigh, SourceLow, SourceHL2, SourceHLC3, SourceOHLC4}

func (s Source) Valid() bool { return slices.Contains(Sources, s) }

func (s Source) String() string { return string(s) }

func (s Source) Value(c Candle) float64 {
	switch s {
	case SourceOpen:
		return c.Open
	case SourceHigh:
		return c.High
	case SourceLow:
		return c.Low
	case SourceHL2:
		return (c.High + c.Low) / 2
	case SourceHLC3:
		return (c.High + c.Low + c.Close) / 3
	case SourceOHLC4:
		return (c.Open + c.High + c.Low + c.Close) / 4
	default:
		return c.Close
	}
}

func ParseSource(text string) (Source, error) {
	source := Source(strings.ToLower(strings.TrimSpace(text)))
	if !source.Valid() {
		return "", fmt.Errorf("unknown source %q (want one of: %s)", text, JoinSources())
	}
	return source, nil
}

func JoinSources() string {
	names := make([]string, 0, len(Sources))
	for _, source := range Sources {
		names = append(names, string(source))
	}
	return strings.Join(names, ", ")
}
