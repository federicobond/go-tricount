package tricount

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
)

const sessionResponse = `{"Response":[
	{"Token":{"token":"sess-abc"}},
	{"UserPerson":{"id":79290957,"display_name":"tricount participant"}}
]}`

// testCredentials are fixed so tests can assert on the headers.
func testCredentials() Credentials {
	return Credentials{
		AppID:        "11111111-2222-4333-8444-555555555555",
		PublicKeyPEM: "-----BEGIN RSA PUBLIC KEY-----\nZmFrZQ==\n-----END RSA PUBLIC KEY-----\n",
	}
}

func TestAuthenticateSendsExpectedHeadersAndBody(t *testing.T) {
	var gotHeader http.Header
	var gotBody map[string]string
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decoding request body: %v", err)
		}
		w.Write([]byte(sessionResponse))
	}))
	defer srv.Close()

	creds := testCredentials()
	c := NewClient(creds, WithBaseURL(srv.URL))
	userID, err := c.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if userID != 79290957 {
		t.Errorf("userID = %d, want 79290957", userID)
	}
	if c.UserID() != 79290957 {
		t.Errorf("UserID() = %d, want 79290957", c.UserID())
	}

	if gotPath != "/v1/session-registry-installation" {
		t.Errorf("path = %q", gotPath)
	}
	if got := gotHeader.Get("User-Agent"); got != defaultUserAgent {
		t.Errorf("User-Agent = %q, want %q", got, defaultUserAgent)
	}
	if got := gotHeader.Get("app-id"); got != creds.AppID {
		t.Errorf("app-id = %q, want %q", got, creds.AppID)
	}
	if got := gotHeader.Get("X-Bunq-Client-Request-Id"); got != clientRequestID {
		t.Errorf("X-Bunq-Client-Request-Id = %q, want %q", got, clientRequestID)
	}
	if got := gotHeader.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := gotBody["app_installation_uuid"]; got != creds.AppID {
		t.Errorf("app_installation_uuid = %q, want %q", got, creds.AppID)
	}
	if got := gotBody["client_public_key"]; got != creds.PublicKeyPEM {
		t.Errorf("client_public_key = %q", got)
	}
	if got := gotBody["device_description"]; got != "Android" {
		t.Errorf("device_description = %q, want \"Android\"", got)
	}
}

func TestImplicitAuthenticationAndUserPath(t *testing.T) {
	var registrations atomic.Int64
	var dataPath, dataToken string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/session-registry-installation" {
			registrations.Add(1)
			w.Write([]byte(sessionResponse))
			return
		}
		dataPath = r.URL.Path
		dataToken = r.Header.Get("X-Bunq-Client-Authentication")
		w.Write([]byte(`{"Response":[]}`))
	}))
	defer srv.Close()

	c := NewClient(testCredentials(), WithBaseURL(srv.URL))
	if _, err := c.do(context.Background(), request{
		method:   http.MethodGet,
		userPath: "/registry",
	}); err != nil {
		t.Fatalf("do: %v", err)
	}

	if got := registrations.Load(); got != 1 {
		t.Errorf("registrations = %d, want 1", got)
	}
	if dataPath != "/v1/user/79290957/registry" {
		t.Errorf("userPath expanded to %q", dataPath)
	}
	if dataToken != "sess-abc" {
		t.Errorf("auth header = %q, want \"sess-abc\"", dataToken)
	}

	if _, err := c.do(context.Background(), request{method: http.MethodGet, userPath: "/registry"}); err != nil {
		t.Fatalf("second do: %v", err)
	}
	if got := registrations.Load(); got != 1 {
		t.Errorf("registrations after reuse = %d, want 1", got)
	}
}

func TestReauthenticatesOnceOn401(t *testing.T) {
	var registrations, dataCalls atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/session-registry-installation" {
			n := registrations.Add(1)
			if n == 1 {
				w.Write([]byte(sessionResponse))
			} else {
				w.Write([]byte(`{"Response":[{"Token":{"token":"sess-second"}},{"UserPerson":{"id":79290957}}]}`))
			}
			return
		}
		n := dataCalls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"Error":[{"error_description":"Session expired"}]}`))
			return
		}
		if got := r.Header.Get("X-Bunq-Client-Authentication"); got != "sess-second" {
			t.Errorf("retry used token %q, want \"sess-second\"", got)
		}
		w.Write([]byte(`{"Response":[]}`))
	}))
	defer srv.Close()

	c := NewClient(testCredentials(), WithBaseURL(srv.URL))
	if _, err := c.do(context.Background(), request{method: http.MethodGet, userPath: "/registry"}); err != nil {
		t.Fatalf("do: %v", err)
	}
	if got := registrations.Load(); got != 2 {
		t.Errorf("registrations = %d, want 2", got)
	}
	if got := dataCalls.Load(); got != 2 {
		t.Errorf("data calls = %d, want 2", got)
	}
}

func TestGivesUpAfterSecond401(t *testing.T) {
	var dataCalls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/session-registry-installation" {
			w.Write([]byte(sessionResponse))
			return
		}
		dataCalls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"Error":[{"error_description":"nope"}]}`))
	}))
	defer srv.Close()

	c := NewClient(testCredentials(), WithBaseURL(srv.URL))
	_, err := c.do(context.Background(), request{method: http.MethodGet, userPath: "/registry"})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if got := dataCalls.Load(); got != 2 {
		t.Errorf("data calls = %d, want exactly 2 (one retry, no more)", got)
	}
}

func TestConcurrentCallsRegisterOnce(t *testing.T) {
	var registrations atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/session-registry-installation" {
			registrations.Add(1)
			w.Write([]byte(sessionResponse))
			return
		}
		w.Write([]byte(`{"Response":[]}`))
	}))
	defer srv.Close()

	c := NewClient(testCredentials(), WithBaseURL(srv.URL))
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.do(context.Background(), request{method: http.MethodGet, userPath: "/registry"}); err != nil {
				t.Errorf("do: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := registrations.Load(); got != 1 {
		t.Errorf("registrations = %d, want 1", got)
	}
}

func TestQueryAndErrorPropagation(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/session-registry-installation" {
			w.Write([]byte(sessionResponse))
			return
		}
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"Error":[{"error_description":"Registry not found"}]}`))
	}))
	defer srv.Close()

	c := NewClient(testCredentials(), WithBaseURL(srv.URL))
	_, err := c.do(context.Background(), request{
		method:   http.MethodGet,
		userPath: "/registry",
		query:    url.Values{"public_identifier_token": {"tABC123"}},
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Description != "Registry not found" {
		t.Errorf("err = %#v, want the description preserved", err)
	}
	if gotQuery != "public_identifier_token=tABC123" {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sessionResponse))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewClient(testCredentials(), WithBaseURL(srv.URL))
	if _, err := c.Authenticate(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
