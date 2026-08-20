package brapi

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLimiterPacesRequests(t *testing.T) {
	now := time.Unix(0, 0)
	slept := []time.Duration{}

	limiter := NewLimiter(2, 1)
	limiter.now = func() time.Time { return now }
	limiter.sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		now = now.Add(d)
		return nil
	}

	for range 3 {
		if err := limiter.Wait(context.Background()); err != nil {
			t.Fatalf("Wait: %v", err)
		}
	}

	if len(slept) != 2 {
		t.Fatalf("slept %d times, want 2", len(slept))
	}
	for i, d := range slept {
		if d != 500*time.Millisecond {
			t.Errorf("sleep %d was %s, want 500ms", i, d)
		}
	}
}

func TestLimiterDoesNotSleepWithinBurst(t *testing.T) {
	limiter := NewLimiter(1, 4)
	limiter.now = func() time.Time { return time.Unix(0, 0) }
	limiter.sleep = func(context.Context, time.Duration) error {
		t.Fatal("limiter slept inside its burst")
		return nil
	}

	for range 4 {
		if err := limiter.Wait(context.Background()); err != nil {
			t.Fatalf("Wait: %v", err)
		}
	}
}

func TestLimiterIsUnlimitedAtZeroRate(t *testing.T) {
	limiter := NewLimiter(0, 1)
	limiter.sleep = func(context.Context, time.Duration) error {
		t.Fatal("unlimited limiter slept")
		return nil
	}

	for range 5 {
		if err := limiter.Wait(context.Background()); err != nil {
			t.Fatalf("Wait: %v", err)
		}
	}
}

func TestLimiterHonoursCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := NewLimiter(1, 1).Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait returned %v, want context.Canceled", err)
	}
}
