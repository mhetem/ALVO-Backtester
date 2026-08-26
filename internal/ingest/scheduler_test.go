package ingest

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/mhetem/ALVO-Backtester/internal/market"
)

func testScheduler(t *testing.T, opts ScheduleOptions) *Scheduler {
	t.Helper()
	return NewScheduler(testIngester(t), nil, slog.New(slog.NewTextHandler(io.Discard, nil)), opts)
}

func TestNewSchedulerFillsDefaults(t *testing.T) {
	scheduler := testScheduler(t, ScheduleOptions{})

	if scheduler.opts.CloseDelay != DefaultCloseDelay {
		t.Errorf("CloseDelay = %s, want %s", scheduler.opts.CloseDelay, DefaultCloseDelay)
	}
	if scheduler.opts.PollInterval != DefaultPollInterval {
		t.Errorf("PollInterval = %s, want %s", scheduler.opts.PollInterval, DefaultPollInterval)
	}
	if scheduler.opts.RunTimeout != DefaultRunTimeout {
		t.Errorf("RunTimeout = %s, want %s", scheduler.opts.RunTimeout, DefaultRunTimeout)
	}
	if scheduler.opts.Sessions != DefaultSyncSessions {
		t.Errorf("Sessions = %d, want %d", scheduler.opts.Sessions, DefaultSyncSessions)
	}
}

// The trigger comes off the trading calendar, so a short session moves it and a holiday
// removes it, without anyone maintaining a cron expression.
func TestDueAtFollowsTheSessionClose(t *testing.T) {
	scheduler := testScheduler(t, ScheduleOptions{CloseDelay: 30 * time.Minute})
	calendar := scheduler.ingester.Calendar()

	trading, err := calendar.NextTradingDay(time.Date(2026, 8, 24, 12, 0, 0, 0, calendar.Location()))
	if err != nil {
		t.Fatalf("NextTradingDay: %v", err)
	}

	session, ok := calendar.Session(trading)
	if !ok {
		t.Fatalf("%s came back from NextTradingDay but has no session", trading.Format(time.DateOnly))
	}

	due, ok := scheduler.DueAt(trading)
	if !ok {
		t.Fatal("DueAt reported no trigger on a trading day")
	}
	if want := session.Close.Add(30 * time.Minute); !due.Equal(want) {
		t.Errorf("due at %s, want %s (close + 30m)", due, want)
	}
	if !due.After(session.Close) {
		t.Error("the trigger is not after the close")
	}
}

func TestDueAtHasNoTriggerOnANonTradingDay(t *testing.T) {
	scheduler := testScheduler(t, ScheduleOptions{})
	calendar := scheduler.ingester.Calendar()

	// 2026-08-22 is a Saturday.
	saturday := calendar.Date(2026, time.August, 22)
	if calendar.IsTradingDay(saturday) {
		t.Fatal("fixture day is a trading day; pick another")
	}

	if _, ok := scheduler.DueAt(saturday); ok {
		t.Error("DueAt produced a trigger on a Saturday")
	}
}

func TestSchedulerTimeframesFollowTheIntradaySwitch(t *testing.T) {
	on := testScheduler(t, ScheduleOptions{Intraday: true}).timeframes()
	if len(on) != 2 || on[0] != market.TF1d || on[1] != market.TF5m {
		t.Errorf("intraday on = %v, want [1d 5m] with daily first", on)
	}

	off := testScheduler(t, ScheduleOptions{Intraday: false}).timeframes()
	if len(off) != 1 || off[0] != market.TF1d {
		t.Errorf("intraday off = %v, want [1d]", off)
	}
}

// Daily runs before intraday so a Free-tier deployment, which only gets the first pass,
// still gets the timeframe it can actually serve.
func TestSchedulerRunsDailyBeforeIntraday(t *testing.T) {
	frames := testScheduler(t, ScheduleOptions{Intraday: true}).timeframes()
	if frames[0] != market.TF1d {
		t.Errorf("first timeframe = %s, want 1d", frames[0])
	}
}

func TestSyncFuturesIsSkippedWhenTheFlagIsOff(t *testing.T) {
	scheduler := testScheduler(t, ScheduleOptions{Futures: false})

	// A nil futures ingester would panic if the flag were not honoured.
	scheduler.syncFutures(t.Context(), time.Now())
}

func TestSyncFuturesIsSkippedWithoutAnIngester(t *testing.T) {
	scheduler := testScheduler(t, ScheduleOptions{Futures: true})

	if scheduler.futures != nil {
		t.Fatal("fixture unexpectedly has a futures ingester")
	}
	scheduler.syncFutures(t.Context(), time.Now())
}

// The tail costs one request per root, so the repeat guard is an in-memory mark rather than
// a database read — but it still has to stop a five-minute poll re-running it all evening.
func TestSyncFuturesRunsOncePerTrigger(t *testing.T) {
	scheduler := testScheduler(t, ScheduleOptions{Futures: true})
	due := time.Now()

	scheduler.tailedFor = due
	scheduler.syncFutures(t.Context(), due)

	if !scheduler.tailedFor.Equal(due) {
		t.Error("the guard moved for a trigger already served")
	}

	later := due.Add(24 * time.Hour)
	if !scheduler.tailedFor.Before(later) {
		t.Error("the next session's trigger would be treated as already served")
	}
}
