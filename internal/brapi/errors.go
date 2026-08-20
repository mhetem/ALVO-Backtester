package brapi

import (
	"errors"
	"fmt"
	"net/http"
)

var ErrNotFound = errors.New("brapi: ticker not found")

type APIError struct {
	StatusCode int
	Endpoint   string
	Message    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("brapi: %s returned %d %s", e.Endpoint, e.StatusCode, http.StatusText(e.StatusCode))
	}
	return fmt.Sprintf("brapi: %s returned %d: %s", e.Endpoint, e.StatusCode, e.Message)
}

func (e *APIError) Retryable() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= http.StatusInternalServerError
}

func (e *APIError) Is(target error) bool {
	return target == ErrNotFound && e.StatusCode == http.StatusNotFound
}

func IsUnauthorized(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.StatusCode {
	case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden:
		return true
	}
	return false
}

type scrubbedError struct {
	msg string
	err error
}

func (e *scrubbedError) Error() string { return e.msg }

func (e *scrubbedError) Unwrap() error { return e.err }
