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

	if scheduler.opts.FillAt != DefaultFillAt {
		t.Errorf("FillAt = %s, want %s", Clock(scheduler.opts.FillAt), Clock(DefaultFillAt))
	}
	if scheduler.opts.RunTimeout != DefaultRunTimeout {
		t.Errorf("RunTimeout = %s, want %s", scheduler.opts.RunTimeout, DefaultRunTimeout)
	}
	if scheduler.opts.Sessions != DefaultSyncSessions {
		t.Errorf("Sessions = %d, want %d", scheduler.opts.Sessions, DefaultSyncSessions)
	}
}

// The day is still chosen off the trading calendar, so a holiday removes the trigger
// without anyone maintaining a cron expression; only the hour is now fixed.
func TestDueAtIsTheConfiguredHourOnATradingDay(t *testing.T) {
	scheduler := testScheduler(t, ScheduleOptions{FillAt: 20 * time.Hour})
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

	local := due.In(calendar.Location())
	if local.Hour() != 20 || local.Minute() != 0 {
		t.Errorf("due at %s, want 20:00 in %s", local.Format(time.TimeOnly), calendar.Location())
	}
	if local.Format(time.DateOnly) != trading.Format(time.DateOnly) {
		t.Errorf("due on %s, want the trading day %s", local.Format(time.DateOnly), trading.Format(time.DateOnly))
	}
	if !due.After(session.Close) {
		t.Error("the trigger is not after the close")
	}
}

// An hour earlier than the bell would spend ~300 requests on a session still in progress,
// so a misconfigured one is dragged back to the close.
func TestDueAtNeverFiresBeforeTheClose(t *testing.T) {
	scheduler := testScheduler(t, ScheduleOptions{FillAt: 11 * time.Hour})
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
	if !due.Equal(session.Close) {
		t.Errorf("due at %s, want the close %s", due, session.Close)
	}
}

// The five-minute poll rediscovered the trigger on every tick. A single timer per day has
// to land on the next session by itself, weekends and holidays included.
func TestNextRunSkipsANonTradingDay(t *testing.T) {
	scheduler := testScheduler(t, ScheduleOptions{FillAt: 20 * time.Hour})
	calendar := scheduler.ingester.Calendar()

	// 2026-08-22 is a Saturday.
	saturday := calendar.Date(2026, time.August, 22)
	if calendar.IsTradingDay(saturday) {
		t.Fatal("fixture day is a trading day; pick another")
	}

	next, ok := scheduler.NextRun(saturday)
	if !ok {
		t.Fatal("NextRun found no trigger within the lookahead")
	}
	if !calendar.IsTradingDay(next) {
		t.Errorf("next run %s is not on a trading day", next.In(calendar.Location()).Format(time.DateOnly))
	}
	if !next.After(saturday) {
		t.Errorf("next run %s is not after %s", next, saturday)
	}
}

// Waking at the trigger and immediately scheduling the same trigger again would spin.
func TestNextRunMovesPastATriggerAlreadyServed(t *testing.T) {
	scheduler := testScheduler(t, ScheduleOptions{FillAt: 20 * time.Hour})
	calendar := scheduler.ingester.Calendar()

	trading, err := calendar.NextTradingDay(time.Date(2026, 8, 24, 12, 0, 0, 0, calendar.Location()))
	if err != nil {
		t.Fatalf("NextTradingDay: %v", err)
	}

	due, ok := scheduler.DueAt(trading)
	if !ok {
		t.Fatal("DueAt reported no trigger on a trading day")
	}

	next, ok := scheduler.NextRun(due)
	if !ok {
		t.Fatal("NextRun found no trigger within the lookahead")
	}
	if !next.After(due) {
		t.Errorf("next run %s is not after the trigger it just served, %s", next, due)
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
// a database read — but it still has to stop a restart re-running it the same evening.
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
