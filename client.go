package tricount

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultBaseURL = "https://api.tricount.bunq.com"

	// defaultUserAgent mimics the Tricount Android app. The API rejects
	// clients that do not identify themselves as the app.
	defaultUserAgent = "com.bunq.tricount.android:RELEASE:7.0.7:3174:ANDROID:13:C"

	// clientRequestID is a constant lifted from the app. The API requires
	// the header but does not require it to vary per request.
	clientRequestID = "049bfcdf-6ae4-4cee-af7b-45da31ea85d0"
)

// Client talks to the Tricount API as one registered device.
//
// A Client is safe for concurrent use. Sessions are implicit: every method
// registers a session on demand and re-registers once if the session has
// expired, so calling Authenticate is optional.
type Client struct {
	creds     Credentials
	http      *http.Client
	baseURL   string
	userAgent string

	// authMu serializes session registration so a burst of calls performs
	// one registration rather than many.
	authMu sync.Mutex

	mu     sync.Mutex
	token  string
	userID int64
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient supplies the HTTP client to use, which is where callers set
// timeouts, proxies and transport-level instrumentation.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.http = h
		}
	}
}

// WithBaseURL overrides the API base URL. Tests use it; callers rarely need
// it.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithUserAgent overrides the User-Agent header. The default mimics the
// Android app, which is what the API expects.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua }
}

// NewClient returns a Client for the given device credentials. It performs no
// I/O; the first request registers a session.
func NewClient(creds Credentials, opts ...Option) *Client {
	c := &Client{
		creds:     creds,
		http:      &http.Client{Timeout: 30 * time.Second},
		baseURL:   defaultBaseURL,
		userAgent: defaultUserAgent,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Authenticate ensures a session exists and returns the user ID the API
// assigned to this device. Calling it is optional.
func (c *Client) Authenticate(ctx context.Context) (int64, error) {
	_, userID, err := c.session(ctx)
	return userID, err
}

// UserID returns the authenticated user ID, or 0 before the first request.
func (c *Client) UserID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.userID
}

// session returns the current token, registering one if there is none.
func (c *Client) session(ctx context.Context) (string, int64, error) {
	c.mu.Lock()
	token, userID := c.token, c.userID
	c.mu.Unlock()
	if token != "" {
		return token, userID, nil
	}
	return c.register(ctx, "")
}

// register creates a session. When stale is non-empty it re-registers only
// if the stored token is still stale, so concurrent callers that all saw the
// same expired token trigger a single registration.
func (c *Client) register(ctx context.Context, stale string) (string, int64, error) {
	c.authMu.Lock()
	defer c.authMu.Unlock()

	c.mu.Lock()
	current, userID := c.token, c.userID
	c.mu.Unlock()
	if current != "" && current != stale {
		return current, userID, nil
	}

	body, err := json.Marshal(map[string]string{
		"app_installation_uuid": c.creds.AppID,
		"client_public_key":     c.creds.PublicKeyPEM,
		"device_description":    "Android",
	})
	if err != nil {
		return "", 0, err
	}

	raw, err := c.roundTrip(ctx, request{
		method:      http.MethodPost,
		path:        "/v1/session-registry-installation",
		raw:         body,
		contentType: "application/json",
	}, "")
	if err != nil {
		return "", 0, err
	}

	type tokenObject struct {
		Token string `json:"token"`
	}
	type personObject struct {
		ID int64 `json:"id"`
	}
	tokens, err := decodeEnvelope[tokenObject](raw, "Token")
	if err != nil {
		return "", 0, err
	}
	people, err := decodeEnvelope[personObject](raw, "UserPerson")
	if err != nil {
		return "", 0, err
	}
	if len(tokens) == 0 || tokens[0].Token == "" {
		return "", 0, fmt.Errorf("tricount: session registration returned no token: %s", truncate(raw))
	}
	if len(people) == 0 || people[0].ID == 0 {
		return "", 0, fmt.Errorf("tricount: session registration returned no user id: %s", truncate(raw))
	}

	c.mu.Lock()
	c.token, c.userID = tokens[0].Token, people[0].ID
	token, id := c.token, c.userID
	c.mu.Unlock()
	return token, id, nil
}
