package market

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

type SessionCoverage struct {
	Day      time.Time
	Expected int
	Present  int
}

type GapReport struct {
	Timeframe  Timeframe
	From       time.Time
	To         time.Time
	Sessions   int
	Expected   int
	Bars       int
	Missing    []time.Time
	Partial    []SessionCoverage
	Unexpected []time.Time
}

func (r GapReport) Clean() bool { return len(r.Missing) == 0 && len(r.Unexpected) == 0 }

func FindGaps(cal *Calendar, tf Timeframe, from, to time.Time, present []time.Time) (GapReport, error) {
	if cal == nil {
		return GapReport{}, errors.New("gaps: calendar is required")
	}
	if !tf.Stored() {
		return GapReport{}, fmt.Errorf("gaps: %s is derived on read and has no stored bars to check", tf)
	}
	if to.Before(from) {
		return GapReport{}, fmt.Errorf("gaps: range end %s is before start %s", dateKey(to.In(cal.loc)), dateKey(from.In(cal.loc)))
	}

	report := GapReport{
		Timeframe:  tf,
		From:       from.UTC(),
		To:         to.UTC(),
		Missing:    []time.Time{},
		Partial:    []SessionCoverage{},
		Unexpected: []time.Time{},
	}

	counts := map[string]int{}
	for _, ts := range present {
		session, ok := cal.Session(ts)
		if !ok || !session.Contains(ts) {
			report.Unexpected = append(report.Unexpected, ts.UTC())
			continue
		}
		counts[dateKey(session.Day)]++
		report.Bars++
	}
	slices.SortFunc(report.Unexpected, func(a, b time.Time) int { return a.Compare(b) })

	for _, day := range cal.TradingDays(from, to) {
		session, ok := cal.Session(day)
		if !ok {
			continue
		}

		expected := cal.BucketCount(tf, session)
		report.Sessions++
		report.Expected += expected

		switch found := counts[dateKey(session.Day)]; {
		case found == 0:
			report.Missing = append(report.Missing, session.Day)
		case found < expected:
			report.Partial = append(report.Partial, SessionCoverage{Day: session.Day, Expected: expected, Present: found})
		}
	}

	return report, nil
}
