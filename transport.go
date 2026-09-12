package tricount

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// maxResponseBytes caps how much of a response we will read, so a
// misbehaving server cannot exhaust memory. Attachment listings are the
// largest legitimate payloads and stay far below this.
const maxResponseBytes = 32 << 20

// request describes one API call.
type request struct {
	method string

	// path is used verbatim. Exactly one of path and userPath must be set.
	path string
	// userPath is appended to /v1/user/{id} once the session is
	// established, so callers need not know the user ID in advance.
	userPath string

	query url.Values

	// body is marshalled as JSON. raw, when set, is sent as-is and wins over
	// body; attachment uploads use it for binary payloads.
	body any
	raw  []byte

	contentType string
	headers     map[string]string
}

// do performs an authenticated request, registering a session first and
// retrying exactly once if the session turns out to have expired.
func (c *Client) do(ctx context.Context, r request) ([]byte, error) {
	token, userID, err := c.session(ctx)
	if err != nil {
		return nil, err
	}
	if r.userPath != "" {
		r.path = fmt.Sprintf("/v1/user/%d%s", userID, r.userPath)
	}

	body, err := c.roundTrip(ctx, r, token)
	if err == nil {
		return body, nil
	}
	if !errors.Is(err, ErrUnauthorized) {
		return nil, err
	}

	token, _, err = c.register(ctx, token)
	if err != nil {
		return nil, err
	}
	return c.roundTrip(ctx, r, token)
}

// roundTrip executes a single HTTP exchange with the given token, which may
// be empty during registration.
func (c *Client) roundTrip(ctx context.Context, r request, token string) ([]byte, error) {
	payload := r.raw
	if payload == nil && r.body != nil {
		var err error
		payload, err = json.Marshal(r.body)
		if err != nil {
			return nil, err
		}
	}

	target := c.baseURL + r.path
	if len(r.query) > 0 {
		target += "?" + r.query.Encode()
	}

	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, target, reader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("app-id", c.creds.AppID)
	req.Header.Set("X-Bunq-Client-Request-Id", clientRequestID)
	if token != "" {
		req.Header.Set("X-Bunq-Client-Authentication", token)
	}
	if payload != nil {
		contentType := r.contentType
		if contentType == "" {
			contentType = "application/json"
		}
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, newError(resp, respBody)
	}
	return respBody, nil
}
