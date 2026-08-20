package brapi

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL     = "https://brapi.dev/api"
	DefaultTimeout     = 30 * time.Second
	DefaultRate        = 2.0
	DefaultBurst       = 4
	DefaultMaxAttempts = 4

	defaultBackoffBase = 500 * time.Millisecond
	defaultBackoffMax  = 30 * time.Second
	maxResponseBytes   = 64 << 20
	userAgent          = "alvo-backtester/0.1"
	redacted           = "REDACTED"
)

var FreeTickers = []string{"ITUB4", "MGLU3", "PETR4", "VALE3"}

func IsFreeTicker(ticker string) bool {
	return slices.Contains(FreeTickers, strings.ToUpper(strings.TrimSpace(ticker)))
}

type Client struct {
	baseURL     string
	token       string
	http        *http.Client
	limiter     *Limiter
	usage       UsageRecorder
	log         *slog.Logger
	maxAttempts int
	backoffBase time.Duration
	backoffMax  time.Duration
	sleep       func(context.Context, time.Duration) error
	jitter      func() float64
}

type Option func(*Client)

func WithToken(token string) Option {
	return func(c *Client) { c.token = strings.TrimSpace(token) }
}

func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = baseURL }
}

func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

func WithRateLimit(perSecond float64, burst int) Option {
	return func(c *Client) { c.limiter = NewLimiter(perSecond, burst) }
}

func WithUsageRecorder(u UsageRecorder) Option {
	return func(c *Client) { c.usage = u }
}

func WithLogger(log *slog.Logger) Option {
	return func(c *Client) { c.log = log }
}

func WithMaxAttempts(n int) Option {
	return func(c *Client) { c.maxAttempts = n }
}

func WithBackoff(base, ceiling time.Duration) Option {
	return func(c *Client) { c.backoffBase, c.backoffMax = base, ceiling }
}

func New(opts ...Option) *Client {
	c := &Client{
		baseURL:     DefaultBaseURL,
		http:        &http.Client{Timeout: DefaultTimeout},
		limiter:     NewLimiter(DefaultRate, DefaultBurst),
		usage:       nopRecorder{},
		log:         slog.Default(),
		maxAttempts: DefaultMaxAttempts,
		backoffBase: defaultBackoffBase,
		backoffMax:  defaultBackoffMax,
		sleep:       sleepContext,
		jitter:      randomFraction,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.maxAttempts < 1 {
		c.maxAttempts = 1
	}
	c.baseURL = strings.TrimRight(c.baseURL, "/")
	return c
}

func (c *Client) HasToken() bool { return c.token != "" }

func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	query := url.Values{}
	for key, values := range params {
		for _, value := range values {
			if value != "" {
				query.Add(key, value)
			}
		}
	}
	if c.token != "" {
		query.Set("token", c.token)
	}

	var (
		lastErr error
		delay   time.Duration
	)

	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		if attempt > 1 {
			if err := c.sleep(ctx, delay); err != nil {
				return err
			}
		}
		if err := c.limiter.Wait(ctx); err != nil {
			return err
		}

		body, after, err := c.send(ctx, path, query)
		if err == nil {
			if err := json.Unmarshal(body, out); err != nil {
				return fmt.Errorf("brapi: decoding %s response: %w", path, err)
			}
			return nil
		}

		lastErr = err
		if !retryable(err) || attempt == c.maxAttempts {
			return err
		}

		delay = c.backoff(attempt, after)
		c.log.WarnContext(ctx, "brapi request failed, retrying",
			slog.String("path", path),
			slog.Int("attempt", attempt),
			slog.Duration("retry_in", delay),
			slog.String("err", err.Error()),
		)
	}

	return lastErr
}

func (c *Client) send(ctx context.Context, path string, query url.Values) ([]byte, time.Duration, error) {
	target, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, 0, fmt.Errorf("brapi: building %s url: %w", path, err)
	}
	target.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, 0, c.scrub(err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, c.scrub(err)
	}
	defer resp.Body.Close()

	c.recordRequest(ctx, path, resp.StatusCode)

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, 0, c.scrub(err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, retryAfter(resp.Header), &APIError{
			StatusCode: resp.StatusCode,
			Endpoint:   path,
			Message:    apiMessage(body),
		}
	}

	return body, 0, nil
}

func (c *Client) recordRequest(ctx context.Context, path string, status int) {
	if err := c.usage.RecordRequests(context.WithoutCancel(ctx), time.Now().UTC(), 1); err != nil {
		c.log.ErrorContext(ctx, "recording brapi usage",
			slog.String("path", path),
			slog.Int("status", status),
			slog.Any("err", err),
		)
	}
}

func (c *Client) backoff(attempt int, after time.Duration) time.Duration {
	window := time.Duration(float64(c.backoffBase) * math.Pow(2, float64(attempt-1)))
	if window > c.backoffMax {
		window = c.backoffMax
	}

	delay := time.Duration(c.jitter() * float64(window))
	if after > delay {
		delay = after
	}
	if delay < time.Millisecond {
		delay = time.Millisecond
	}
	return delay
}

func (c *Client) scrub(err error) error {
	if err == nil || c.token == "" {
		return err
	}
	msg := err.Error()
	if !strings.Contains(msg, c.token) {
		return err
	}
	return &scrubbedError{msg: strings.ReplaceAll(msg, c.token, redacted), err: err}
}

func randomFraction() float64 {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0.5
	}
	return float64(binary.BigEndian.Uint64(buf[:])>>11) / (1 << 53)
}

func retryable(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable()
	}
	return true
}

func retryAfter(header http.Header) time.Duration {
	value := header.Get("Retry-After")
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		if d := time.Until(at); d > 0 {
			return d
		}
	}
	return 0
}

func apiMessage(body []byte) string {
	var payload struct {
		Message string `json:"message"`
		Error   any    `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		if payload.Message != "" {
			return payload.Message
		}
		if text, ok := payload.Error.(string); ok && text != "" {
			return text
		}
	}

	message := strings.TrimSpace(string(body))
	if len(message) > 200 {
		message = message[:200]
	}
	return message
}

func validateTicker(ticker string) error {
	if strings.TrimSpace(ticker) == "" {
		return errors.New("brapi: empty ticker")
	}
	if strings.ContainsAny(ticker, " \t\n\r/?&#%") {
		return fmt.Errorf("brapi: invalid ticker %q", ticker)
	}
	return nil
}
