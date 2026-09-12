//go:build live

// This file provisions the persistent fixture the live suite runs against.
//
// Two files under .tricount-live/ (gitignored) hold everything:
//
//	credentials.json — this machine's device identity, generated once
//	fixture.json     — the throwaway tricount's id and sharing token
//
// Both are created only when missing and are never overwritten. The fixture
// tricount is NEVER deleted: cleanup removes only the transactions and
// members a given run created, identified by a run tag in their names.
// TestMain verifies the fixture survived.

package tricount

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	liveDir             = ".tricount-live"
	liveFixtureTitle    = "go-tricount live fixture"
	liveFixtureCurrency = "EUR"
	liveFixtureNote     = "Reusable fixture for go-tricount live tests. Do not delete."
)

// liveFixtureFile is the on-disk record of the reusable tricount.
type liveFixtureFile struct {
	ID          int64  `json:"id"`
	PublicToken string `json:"public_token"`
	Title       string `json:"title"`
}

func credentialsPath() string {
	if p := os.Getenv("TRICOUNT_LIVE_CREDENTIALS"); p != "" {
		return p
	}
	return filepath.Join(liveDir, "credentials.json")
}

func fixturePath() string {
	return filepath.Join(liveDir, "fixture.json")
}

var (
	clientOnce   sync.Once
	sharedClient *Client
	clientErr    error
)

// liveClient returns one authenticated client shared by every live test, so a
// run registers a single session against the persistent device identity.
func liveClient(t *testing.T) *Client {
	t.Helper()
	clientOnce.Do(func() {
		creds, err := LoadOrGenerateCredentials(credentialsPath())
		if err != nil {
			clientErr = fmt.Errorf("live credentials at %s: %w", credentialsPath(), err)
			return
		}
		c := NewClient(creds)
		if os.Getenv("TRICOUNT_RECORD") == "1" {
			liveRecorder = &recorder{
				base:    http.DefaultTransport,
				secrets: []string{creds.AppID, creds.PublicKeyPEM},
			}
			c = NewClient(creds, WithHTTPClient(&http.Client{
				Timeout:   30 * time.Second,
				Transport: liveRecorder,
			}))
		}
		if _, err := c.Authenticate(context.Background()); err != nil {
			clientErr = fmt.Errorf("authenticating against the real API: %w", err)
			return
		}
		sharedClient = c
	})
	if clientErr != nil {
		t.Fatalf("%v", clientErr)
	}
	return sharedClient
}

var (
	fixtureOnce   sync.Once
	sharedFixture *Tricount
	fixtureErr    error
)

// liveTricount returns the reusable fixture tricount, freshly read so each
// test sees current state. It provisions the fixture on first use.
func liveTricount(t *testing.T) *Tricount {
	t.Helper()
	c := liveClient(t)
	fixtureOnce.Do(func() {
		sharedFixture, fixtureErr = loadOrCreateFixture(c)
		if liveRecorder != nil && sharedFixture != nil {
			liveRecorder.addSecret(sharedFixture.PublicToken)
		}
	})
	if fixtureErr != nil {
		t.Fatalf("live fixture: %v", fixtureErr)
	}
	tri, err := c.GetTricountByID(context.Background(), sharedFixture.ID)
	if err != nil {
		t.Fatalf("re-reading the live fixture %d: %v", sharedFixture.ID, err)
	}
	return tri
}

func loadOrCreateFixture(c *Client) (*Tricount, error) {
	ctx := context.Background()

	// An explicit token wins, so the suite can be pointed at a fixture kept
	// somewhere else.
	if tok := os.Getenv("TRICOUNT_LIVE_TOKEN"); tok != "" {
		return c.JoinTricount(ctx, tok)
	}

	raw, readErr := os.ReadFile(fixturePath())
	switch {
	case readErr == nil:
		var f liveFixtureFile
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("reading %s: %w", fixturePath(), err)
		}
		if f.PublicToken != "" {
			// Joining rather than reading re-syncs the fixture in case this
			// device's list was reset.
			return c.JoinTricount(ctx, f.PublicToken)
		}
		if f.ID != 0 {
			return c.GetTricountByID(ctx, f.ID)
		}
		return nil, fmt.Errorf("%s names no tricount", fixturePath())
	case errors.Is(readErr, os.ErrNotExist):
		// fall through and create it
	default:
		return nil, readErr
	}

	id, err := c.CreateTricount(ctx, liveFixtureTitle, liveFixtureCurrency, liveFixtureNote)
	if err != nil {
		return nil, fmt.Errorf("creating the live fixture tricount: %w", err)
	}
	tri, err := c.GetTricountByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("reading back the live fixture %d: %w", id, err)
	}

	record := liveFixtureFile{ID: tri.ID, PublicToken: tri.PublicToken, Title: tri.Title}
	if err := saveFixtureFile(record); err != nil {
		// The tricount exists but we could not record it. Say so loudly
		// enough that the link can be rescued from the log.
		fmt.Fprintf(os.Stderr,
			"\nWARNING: created live fixture tricount %d but could not save %s: %v\n"+
				"  https://tricount.com/%s\n"+
				"  Record that link by hand or the next run will create another one.\n\n",
			tri.ID, fixturePath(), err, tri.PublicToken)
		return nil, err
	}

	fmt.Fprintf(os.Stderr,
		"\ncreated live test tricount %q (id %d)\n  https://tricount.com/%s\n  saved to %s — keep this file\n\n",
		tri.Title, tri.ID, tri.PublicToken, fixturePath())
	return tri, nil
}

// saveFixtureFile writes the record atomically and refuses to clobber an
// existing one, for the same reason Credentials.Save does.
func saveFixtureFile(f liveFixtureFile) error {
	path := fixturePath()
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("refusing to overwrite existing %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(raw, '\n'), 0o600)
}

// runTag identifies one `go test` invocation. Everything a run creates
// carries it, and cleanup deletes only what carries it.
var runTag = func() string {
	u, err := newUUID()
	if err != nil {
		return "go-live-nouuid"
	}
	return "go-live-" + strings.ReplaceAll(u, "-", "")[:6]
}()

// tagged decorates a name so cleanup can recognise it later.
func tagged(name string) string { return fmt.Sprintf("%s [%s]", name, runTag) }

// isFromThisRun reports whether a description or display name was created by
// this run.
func isFromThisRun(s string) bool { return strings.Contains(s, "["+runTag+"]") }

// TestMain guards the invariant the whole live strategy depends on: the
// fixture tricount must still be there when the run ends.
func TestMain(m *testing.M) {
	code := m.Run()
	if sharedClient != nil && sharedFixture != nil {
		if _, err := sharedClient.GetTricountByID(context.Background(), sharedFixture.ID); err != nil {
			fmt.Fprintf(os.Stderr,
				"\nFATAL: the live fixture tricount %d is gone after this run: %v\n"+
					"Something deleted it. That must never happen; fix the test that did.\n",
				sharedFixture.ID, err)
			if code == 0 {
				code = 1
			}
		}
	}
	os.Exit(code)
}

// cleanupThisRun registers a teardown that deletes only what this run
// created, recognised by the run tag in its name. It never deletes the
// fixture tricount.
func cleanupThisRun(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		if sharedClient == nil || sharedFixture == nil {
			return
		}
		ctx := context.Background()
		tri, err := sharedClient.GetTricountByID(ctx, sharedFixture.ID)
		if err != nil {
			t.Logf("cleanup: re-reading the fixture: %v", err)
			return
		}
		// Transactions go first: a member who still appears in one may not be
		// deletable.
		for _, tx := range tri.Transactions {
			if !isFromThisRun(tx.Description) {
				continue
			}
			if err := sharedClient.DeleteTransaction(ctx, tri, tx.ID); err != nil {
				t.Logf("cleanup: deleting transaction %d (%q): %v", tx.ID, tx.Description, err)
			}
		}
		if tri, err = sharedClient.GetTricountByID(ctx, sharedFixture.ID); err != nil {
			t.Logf("cleanup: re-reading after transaction deletes: %v", err)
			return
		}

		for _, m := range tri.Members {
			if !isFromThisRun(m.DisplayName) {
				continue
			}
			if err := sharedClient.DeleteMember(ctx, tri, m); err != nil {
				t.Logf("cleanup: deleting member %q: %v", m.DisplayName, err)
				continue
			}
			// DeleteMember refreshed tri.Members, so the loop is iterating a
			// stale slice; re-read and start over.
			if tri, err = sharedClient.GetTricountByID(ctx, sharedFixture.ID); err != nil {
				t.Logf("cleanup: re-reading after a member delete: %v", err)
				return
			}
		}
	})
}

// recorder saves every response body to testdata/recorded/, scrubbed, when
// TRICOUNT_RECORD=1 is set. The files it writes are where the offline fixtures
// come from.
type recorder struct {
	base http.RoundTripper

	mu      sync.Mutex
	n       int
	secrets []string
}

// liveRecorder is non-nil when recording is on, so the fixture token can be
// added to the scrub list once it is known.
var liveRecorder *recorder

func (rec *recorder) addSecret(s string) {
	if s == "" {
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.secrets = append(rec.secrets, s)
}

func (rec *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rec.base.RoundTrip(req)
	if err != nil || resp.Body == nil {
		return resp, err
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	rec.save(req, body)
	return resp, nil
}

var userIDInPath = regexp.MustCompile(`user-\d+`)

func (rec *recorder) save(req *http.Request, body []byte) {
	rec.mu.Lock()
	rec.n++
	n := rec.n
	secrets := append([]string(nil), rec.secrets...)
	rec.mu.Unlock()

	slug := strings.Trim(req.URL.Path, "/")
	slug = strings.ReplaceAll(slug, "/", "-")
	// Drop the volatile user id so filenames stay comparable between runs.
	slug = userIDInPath.ReplaceAllString(slug, "user")
	name := fmt.Sprintf("%03d-%s-%s.json", n, strings.ToLower(req.Method), slug)

	dir := filepath.Join("testdata", "recorded")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "recorder: %v\n", err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, name), scrubSecrets(body, secrets), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "recorder: %v\n", err)
	}
}
