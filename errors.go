package tricount

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

var (
	// ErrInvalidRequest is returned for client-side validation failures,
	// before any HTTP request is made. Wrapped errors carry the detail.
	ErrInvalidRequest = errors.New("tricount: invalid request")

	ErrUnauthorized = errors.New("tricount: unauthorized")
	ErrForbidden    = errors.New("tricount: forbidden")
	ErrNotFound     = errors.New("tricount: not found")
	ErrRateLimited  = errors.New("tricount: rate limited")
	ErrServer       = errors.New("tricount: server error")
)

// Error is an error response from the API. Compare it against the package
// sentinels with errors.Is:
//
//	if errors.Is(err, tricount.ErrNotFound) { ... }
type Error struct {
	StatusCode int
	// Description is bunq's error_description, or the truncated raw body
	// when the response was not the usual error envelope.
	Description string
	// ResponseID is the X-Bunq-Client-Response-Id header, worth quoting in
	// a bug report.
	ResponseID string
	// RetryAfter is set from the Retry-After header on a 429.
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	if e.Description == "" {
		return fmt.Sprintf("tricount: http %d", e.StatusCode)
	}
	return fmt.Sprintf("tricount: http %d: %s", e.StatusCode, e.Description)
}

// Is maps HTTP status codes onto the package sentinels.
func (e *Error) Is(target error) bool {
	switch target {
	case ErrUnauthorized:
		return e.StatusCode == http.StatusUnauthorized
	case ErrForbidden:
		return e.StatusCode == http.StatusForbidden
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound
	case ErrRateLimited:
		return e.StatusCode == http.StatusTooManyRequests
	case ErrServer:
		return e.StatusCode >= 500
	}
	return false
}

func newError(resp *http.Response, body []byte) *Error {
	e := &Error{
		StatusCode:  resp.StatusCode,
		Description: describeError(body),
		ResponseID:  resp.Header.Get("X-Bunq-Client-Response-Id"),
	}
	if s := resp.Header.Get("Retry-After"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			e.RetryAfter = time.Duration(n) * time.Second
		}
	}
	return e
}

// describeError pulls the human-readable message out of bunq's error
// envelope, falling back to a truncated raw body.
func describeError(body []byte) string {
	var env struct {
		Error []struct {
			Description string `json:"error_description"`
		} `json:"Error"`
	}
	if err := json.Unmarshal(body, &env); err == nil && len(env.Error) > 0 && env.Error[0].Description != "" {
		return env.Error[0].Description
	}
	if len(body) > 256 {
		body = body[:256]
	}
	return string(body)
}
