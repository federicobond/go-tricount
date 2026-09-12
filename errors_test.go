package tricount

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestErrorSentinelMapping(t *testing.T) {
	cases := []struct {
		status   int
		sentinel error
	}{
		{http.StatusUnauthorized, ErrUnauthorized},
		{http.StatusForbidden, ErrForbidden},
		{http.StatusNotFound, ErrNotFound},
		{http.StatusTooManyRequests, ErrRateLimited},
		{http.StatusInternalServerError, ErrServer},
		{http.StatusBadGateway, ErrServer},
	}
	for _, c := range cases {
		var err error = &Error{StatusCode: c.status}
		if !errors.Is(err, c.sentinel) {
			t.Errorf("status %d does not match its sentinel", c.status)
		}
	}

	var err error = &Error{StatusCode: http.StatusBadRequest}
	for _, s := range []error{ErrUnauthorized, ErrForbidden, ErrNotFound, ErrRateLimited, ErrServer} {
		if errors.Is(err, s) {
			t.Errorf("status 400 wrongly matched %v", s)
		}
	}
	if errors.Is(err, ErrInvalidRequest) {
		t.Error("a server error must not match ErrInvalidRequest")
	}
}

func TestNewErrorParsesBody(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Header: http.Header{
			"X-Bunq-Client-Response-Id": []string{"abc-123"},
		},
	}
	body := []byte(`{"Error":[{"error_description":"Field currency is superfluous."}]}`)

	e := newError(resp, body)
	if e.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want 400", e.StatusCode)
	}
	if e.Description != "Field currency is superfluous." {
		t.Errorf("Description = %q", e.Description)
	}
	if e.ResponseID != "abc-123" {
		t.Errorf("ResponseID = %q, want \"abc-123\"", e.ResponseID)
	}
	if got, want := e.Error(), "tricount: http 400: Field currency is superfluous."; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestNewErrorRetryAfter(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"30"}},
	}
	e := newError(resp, nil)
	if e.RetryAfter != 30*time.Second {
		t.Errorf("RetryAfter = %v, want 30s", e.RetryAfter)
	}
	if !errors.Is(error(e), ErrRateLimited) {
		t.Error("429 should match ErrRateLimited")
	}
}

func TestNewErrorFallsBackToRawBody(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{}}
	e := newError(resp, []byte("<html>gateway down</html>"))
	if e.Description != "<html>gateway down</html>" {
		t.Errorf("Description = %q, want the raw body", e.Description)
	}

	long := make([]byte, 1000)
	for i := range long {
		long[i] = 'x'
	}
	e = newError(resp, long)
	if len(e.Description) > 256 {
		t.Errorf("Description length = %d, want it truncated to 256", len(e.Description))
	}
}
