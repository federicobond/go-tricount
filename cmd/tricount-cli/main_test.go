package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tricount "github.com/federicobond/go-tricount"
)

const sessionResponse = `{"Response":[
	{"Token":{"token":"sess-abc"}},
	{"UserPerson":{"id":79290957,"display_name":"tricount participant"}}
]}`

// newTestApp returns an app wired to a fake API and the buffers it writes to.
func newTestApp(t *testing.T, handler http.HandlerFunc) (*app, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/session-registry-installation" {
			w.Write([]byte(sessionResponse))
			return
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	var stdout, stderr bytes.Buffer
	return &app{
		stdout:   &stdout,
		stderr:   &stderr,
		credPath: filepath.Join(t.TempDir(), "credentials.json"),
		opts:     []tricount.Option{tricount.WithBaseURL(srv.URL)},
	}, &stdout, &stderr
}

func TestListPrintsIDAndTitle(t *testing.T) {
	a, stdout, stderr := newTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Response":[
			{"Registry":{"id":1,"title":"Flat","currency":"EUR","status":"READ_WRITE","memberships":[],"all_registry_entry":[]}},
			{"Registry":{"id":2,"title":"Trip","currency":"JPY","status":"READ_ONLY","memberships":[],"all_registry_entry":[]}}
		]}`))
	})

	if code := a.run([]string{"list"}); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{"1", "Flat", "EUR", "2", "Trip", "JPY"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

func TestFailuresExitNonZero(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"no arguments", nil},
		{"unknown command", []string{"nonesuch"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _, stderr := newTestApp(t, func(w http.ResponseWriter, r *http.Request) {
				t.Error("no request should be made")
			})
			if code := a.run(tc.args); code == 0 {
				t.Errorf("exit code = 0, want non-zero; stderr = %q", stderr.String())
			}
		})
	}
}

func TestAPIFailureExitsNonZero(t *testing.T) {
	a, _, _ := newTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"Error":[{"error_description":"boom"}]}`))
	})
	if code := a.run([]string{"list"}); code == 0 {
		t.Error("exit code = 0, want non-zero when the API fails")
	}
}

// fixtureApp serves the library's recorded registry fixture for any
// non-session request.
func fixtureApp(t *testing.T) (*app, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "registry.json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return newTestApp(t, func(w http.ResponseWriter, r *http.Request) { w.Write(raw) })
}

func TestShowPrintsMembersAndTransactions(t *testing.T) {
	a, stdout, stderr := fixtureApp(t)
	if code := a.run([]string{"show", "102257091"}); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"Taiwan", "JPY", "Alice", "Bob", "Ramen", "Souvenir refund"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

func TestBalancesPrintsNetPositions(t *testing.T) {
	a, stdout, stderr := fixtureApp(t)
	if code := a.run([]string{"balances", "102257091"}); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	out := stdout.String()
	// Alice laid out 507 and owes 254 of it; Bob's income nets to zero.
	for _, want := range []string{"Alice", "253", "Bob", "-253"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

func TestSettlePrintsTransferPlan(t *testing.T) {
	a, stdout, stderr := fixtureApp(t)
	if code := a.run([]string{"settle", "102257091"}); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"Bob", "Alice", "253"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

func TestSelectorMustBeNumeric(t *testing.T) {
	a, _, stderr := fixtureApp(t)
	if code := a.run([]string{"balances", "Taiwan"}); code == 0 {
		t.Error("exit code = 0, want non-zero for a non-numeric id")
	}
	if !strings.Contains(stderr.String(), "Taiwan") {
		t.Errorf("stderr should name the bad argument: %q", stderr.String())
	}
}

const syncResponse = `{"Response":[{"RegistrySynchronization":{
	"all_registry_active":[
		{"id":102257091,"title":"Taiwan","currency":"JPY","status":"READ_WRITE",
		 "public_identifier_token":"tABC123xyz","memberships":[],"all_registry_entry":[]}
	],
	"all_registry_archived":[]
}}]}`

// routedApp serves the sync endpoint with syncResponse and everything else
// with the recorded registry fixture, recording the paths and bodies seen.
func routedApp(t *testing.T) (*app, *bytes.Buffer, *bytes.Buffer, *[]string, *[]string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "registry.json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var paths, bodies []string
	a, stdout, stderr := newTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if b, _ := io.ReadAll(r.Body); len(b) > 0 {
			bodies = append(bodies, string(b))
		}
		if strings.HasSuffix(r.URL.Path, "/registry-synchronization") {
			w.Write([]byte(syncResponse))
			return
		}
		w.Write(raw)
	})
	return a, stdout, stderr, &paths, &bodies
}

func TestJoinSyncsTheToken(t *testing.T) {
	a, stdout, stderr, _, bodies := routedApp(t)
	if code := a.run([]string{"join", "tABC123xyz"}); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if len(*bodies) == 0 || !strings.Contains((*bodies)[0], "tABC123xyz") {
		t.Errorf("the sharing token was not sent: %v", *bodies)
	}
	if out := stdout.String(); !strings.Contains(out, "Taiwan") || !strings.Contains(out, "102257091") {
		t.Errorf("output should name the joined tricount: %q", out)
	}
}

func TestLeaveSendsTheTokenAsDeleted(t *testing.T) {
	a, _, stderr, _, bodies := routedApp(t)
	if code := a.run([]string{"leave", "102257091"}); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	var sawDeleted bool
	for _, b := range *bodies {
		if strings.Contains(b, "all_registry_deleted") && strings.Contains(b, "tABC123xyz") {
			sawDeleted = true
		}
	}
	if !sawDeleted {
		t.Errorf("leave must send the token as deleted: %v", *bodies)
	}
}

func TestLinkSetsTheMember(t *testing.T) {
	a, _, stderr, _, bodies := routedApp(t)
	if code := a.run([]string{"link", "102257091", "Bob"}); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	var sawBob bool
	for _, b := range *bodies {
		if strings.Contains(b, "bbbbbbbb-0000-4000-8000-000000000002") {
			sawBob = true
		}
	}
	if !sawBob {
		t.Errorf("link must send Bob's membership uuid: %v", *bodies)
	}
}

func TestLinkUnknownMemberNamesTheOptions(t *testing.T) {
	a, _, stderr, _, _ := routedApp(t)
	if code := a.run([]string{"link", "102257091", "Carol"}); code == 0 {
		t.Fatal("exit code = 0, want non-zero for an unknown member")
	}
	msg := stderr.String()
	if !strings.Contains(msg, "Carol") || !strings.Contains(msg, "Alice") || !strings.Contains(msg, "Bob") {
		t.Errorf("error should name the bad member and the real ones: %q", msg)
	}
}

func TestWhoamiShowsDeviceAndLinks(t *testing.T) {
	a, stdout, stderr, _, _ := routedApp(t)
	if code := a.run([]string{"whoami"}); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"Taiwan", "Alice"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

func TestListJSON(t *testing.T) {
	a, stdout, stderr := newTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Response":[
			{"Registry":{"id":1,"title":"Flat","currency":"EUR","status":"READ_WRITE","memberships":[],"all_registry_entry":[]}}
		]}`))
	})
	if code := a.run([]string{"list", "--json"}); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	var got []struct {
		ID       int64  `json:"id"`
		Title    string `json:"title"`
		Currency string `json:"currency"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout.String())
	}
	if len(got) != 1 || got[0].ID != 1 || got[0].Title != "Flat" || got[0].Currency != "EUR" {
		t.Errorf("got %+v", got)
	}
}

func TestBalancesJSONKeepsAmountsExact(t *testing.T) {
	a, stdout, stderr := fixtureApp(t)
	if code := a.run([]string{"balances", "--json", "102257091"}); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	var got map[string]struct {
		Value    string `json:"value"`
		Currency string `json:"currency"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout.String())
	}
	// A string, not a float: exactness is the whole point of Amount.
	if got["Alice"].Value != "253" || got["Alice"].Currency != "JPY" {
		t.Errorf("Alice = %+v, want value \"253\" JPY", got["Alice"])
	}
	if got["Bob"].Value != "-253" {
		t.Errorf("Bob = %+v, want value \"-253\"", got["Bob"])
	}
}

func TestSettleJSON(t *testing.T) {
	a, stdout, stderr := fixtureApp(t)
	if code := a.run([]string{"settle", "--json", "102257091"}); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	var got []struct {
		From   string `json:"from"`
		To     string `json:"to"`
		Amount struct {
			Value string `json:"value"`
		} `json:"amount"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout.String())
	}
	if len(got) != 1 || got[0].From != "Bob" || got[0].To != "Alice" || got[0].Amount.Value != "253" {
		t.Errorf("got %+v", got)
	}
}

// cells splits a rendered table line on runs of two or more spaces, which is
// how tabwriter separates columns.
func cells(line string) []string {
	var out []string
	for _, c := range regexp.MustCompile(`\s{2,}`).Split(strings.TrimSpace(line), -1) {
		out = append(out, strings.TrimSpace(c))
	}
	return out
}

// findRow returns the cells of the first line whose first non-empty cell
// matches, or nil.
func findRow(out, contains string) []string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, contains) {
			return cells(line)
		}
	}
	return nil
}

func TestShowGivesEachMemberAColumn(t *testing.T) {
	a, stdout, stderr := fixtureApp(t)
	if code := a.run([]string{"show", "102257091"}); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	out := stdout.String()

	header := findRow(out, "DESCRIPTION")
	if header == nil {
		t.Fatalf("no transaction header:\n%s", out)
	}
	// DATE DESCRIPTION AMOUNT PAID BY, then one column per member.
	want := []string{"DATE", "DESCRIPTION", "AMOUNT", "PAID BY", "Alice", "Bob"}
	if len(header) != len(want) {
		t.Fatalf("header = %q, want %q", header, want)
	}
	for i := range want {
		if header[i] != want[i] {
			t.Errorf("header[%d] = %q, want %q", i, header[i], want[i])
		}
	}

	headerLine := lineContaining(out, "DESCRIPTION")

	// Ramen: Alice paid 507, split 254 to Alice and 253 to Bob.
	ramen := lineContaining(out, "Ramen")
	if got := under(headerLine, ramen, "Alice"); got != "254" {
		t.Errorf("Ramen, Alice = %q, want \"254\"", got)
	}
	if got := under(headerLine, ramen, "Bob"); got != "253" {
		t.Errorf("Ramen, Bob = %q, want \"253\"", got)
	}

	// The income is allocated to Bob alone, so Alice's cell must be empty —
	// that is the case the old table could not show. An empty cell is not
	// the same as a share of zero.
	refund := lineContaining(out, "Souvenir refund")
	if got := under(headerLine, refund, "Alice"); got != "" {
		t.Errorf("refund, Alice = %q, want an empty cell", got)
	}
	if got := under(headerLine, refund, "Bob"); got != "1000" {
		t.Errorf("refund, Bob = %q, want \"1000\"", got)
	}
}

func lineContaining(out, want string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, want) {
			return line
		}
	}
	return ""
}

// under returns the cell of row sitting beneath the named column of header.
// tabwriter aligns columns, so a column's start offset in the header is where
// its cells begin — which is the only way to see a cell that is empty.
func under(header, row, column string) string {
	start := strings.Index(header, column)
	if start < 0 {
		return "<no such column>"
	}
	end := len(row)
	// The next column begins at the next run of two spaces after start.
	if rest := header[start+len(column):]; true {
		if off := regexp.MustCompile(`\s{2,}\S`).FindStringIndex(rest); off != nil {
			if n := start + len(column) + off[1] - 1; n < end {
				end = n
			}
		}
	}
	if start >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[start:end])
}
