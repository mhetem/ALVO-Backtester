package brapi

import (
	"context"
	"math"
	"sync"
	"time"
)

type Limiter struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time

	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

func NewLimiter(perSecond float64, burst int) *Limiter {
	if burst < 1 {
		burst = 1
	}
	return &Limiter{
		rate:   perSecond,
		burst:  float64(burst),
		tokens: float64(burst),
		now:    time.Now,
		sleep:  sleepContext,
	}
}

func (l *Limiter) Wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d := l.reserve()
	if d <= 0 {
		return nil
	}
	return l.sleep(ctx, d)
}

func (l *Limiter) reserve() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.rate <= 0 || math.IsInf(l.rate, 1) {
		return 0
	}

	now := l.now()
	if l.last.IsZero() {
		l.last = now
	}
	if elapsed := now.Sub(l.last); elapsed > 0 {
		l.tokens = math.Min(l.burst, l.tokens+elapsed.Seconds()*l.rate)
		l.last = now
	}

	l.tokens--
	if l.tokens >= 0 {
		return 0
	}
	return time.Duration(-l.tokens / l.rate * float64(time.Second))
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
