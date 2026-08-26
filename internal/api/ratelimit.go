package api

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	headerForwardedFor = "X-Forwarded-For"

	rateSweepEvery = 10 * time.Minute
	rateIdleAfter  = 15 * time.Minute

	msgRateLimited = "too many requests, slow down"
)

type rateBucket struct {
	tokens float64
	seen   time.Time
}

type limiter struct {
	burst  float64
	refill float64
	now    func() time.Time

	mu      sync.Mutex
	buckets map[string]*rateBucket
	swept   time.Time
}

func newLimiter(burst int, per time.Duration) *limiter {
	return &limiter{
		burst:   float64(burst),
		refill:  float64(burst) / per.Seconds(),
		now:     time.Now,
		buckets: make(map[string]*rateBucket),
	}
}

func (l *limiter) allow(key string) bool {
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweep(now)

	bucket, ok := l.buckets[key]
	if !ok {
		bucket = &rateBucket{tokens: l.burst, seen: now}
		l.buckets[key] = bucket
	}

	bucket.tokens = min(l.burst, bucket.tokens+now.Sub(bucket.seen).Seconds()*l.refill)
	bucket.seen = now

	if bucket.tokens < 1 {
		return false
	}

	bucket.tokens--
	return true
}

func (l *limiter) sweep(now time.Time) {
	if now.Sub(l.swept) < rateSweepEvery {
		return
	}
	l.swept = now

	for key, bucket := range l.buckets {
		if now.Sub(bucket.seen) > rateIdleAfter {
			delete(l.buckets, key)
		}
	}
}

func (l *limiter) retryAfter() time.Duration {
	return time.Duration(float64(time.Second) / l.refill)
}

func (s *Server) limitByIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.ipLimit.allow(clientIP(r, s.cfg.TrustProxy)) {
			respondRateLimited(w, r, s.ipLimit.retryAfter())
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) limitByUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := UserIDFrom(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		if !s.userLimit.allow(userID.String()) {
			respondRateLimited(w, r, s.userLimit.retryAfter())
			return
		}
		next.ServeHTTP(w, r)
	})
}

func respondRateLimited(w http.ResponseWriter, r *http.Request, retryAfter time.Duration) {
	seconds := max(int(math.Ceil(retryAfter.Seconds())), 1)
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	respondError(w, r, http.StatusTooManyRequests, msgRateLimited)
}

func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		hops := strings.Split(r.Header.Get(headerForwardedFor), ",")
		if hop := strings.TrimSpace(hops[len(hops)-1]); hop != "" {
			return hop
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
