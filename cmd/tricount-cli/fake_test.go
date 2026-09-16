package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tricount "github.com/federicobond/go-tricount"
)

const fakeTricountID = 102257091

type fakeMember struct {
	UUID string
	Name string
}

// fakeAPI is a stateful stand-in for the registry endpoints the member
// commands touch, so adding a member is visible to the re-read that follows.
type fakeAPI struct {
	members []fakeMember
	linked  string
	puts    []map[string]any
}

// registryObject is the bare registry, which the sync response embeds and the
// registry response wraps.
func (f *fakeAPI) registryObject() string {
	var ms []string
	for i, m := range f.members {
		ms = append(ms, fmt.Sprintf(
			`{"RegistryMembershipNonUser":{"id":%d,"uuid":%q,"status":"ACTIVE","alias":{"display_name":%q,"pointer":{"type":"UUID","value":%q,"name":%q}}}}`,
			400000+i, m.UUID, m.Name, m.UUID, m.Name))
	}
	linked := "null"
	if f.linked != "" {
		linked = fmt.Sprintf("%q", f.linked)
	}
	return fmt.Sprintf(`{"id":%d,"title":"Taiwan","currency":"JPY","status":"READ_WRITE",
		"public_identifier_token":"tABC123xyz","membership_uuid_active":%s,
		"memberships":[%s],"all_registry_entry":[]}`,
		fakeTricountID, linked, strings.Join(ms, ","))
}

func (f *fakeAPI) registryJSON() string {
	return `{"Response":[{"Registry":` + f.registryObject() + `}]}`
}

func (f *fakeAPI) syncJSON() string {
	return `{"Response":[{"RegistrySynchronization":{"all_registry_active":[` +
		f.registryObject() + `],"all_registry_archived":[]}}]}`
}

func (f *fakeAPI) membershipsJSON() string {
	var ms []string
	for i, m := range f.members {
		ms = append(ms, fmt.Sprintf(
			`{"RegistryMembershipNonUser":{"id":%d,"uuid":%q,"status":"ACTIVE","alias":{"display_name":%q,"pointer":{"type":"UUID","value":%q,"name":%q}}}}`,
			400000+i, m.UUID, m.Name, m.UUID, m.Name))
	}
	return `{"Response":[` + strings.Join(ms, ",") + `]}`
}

func (f *fakeAPI) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/registry-synchronization"):
		w.Write([]byte(f.syncJSON()))

	case strings.HasSuffix(r.URL.Path, "/registry-membership"):
		w.Write([]byte(f.membershipsJSON()))

	case r.Method == http.MethodPut:
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		f.puts = append(f.puts, body)
		if uuid, ok := body["membership_uuid_active"].(string); ok {
			f.linked = uuid
		}
		if raw, ok := body["memberships"].([]any); ok {
			var next []fakeMember
			for _, e := range raw {
				m := e.(map[string]any)
				alias := m["alias"].(map[string]any)
				next = append(next, fakeMember{
					UUID: m["uuid"].(string),
					Name: alias["name"].(string),
				})
			}
			f.members = next
		}
		w.Write([]byte(`{"Response":[{"Id":{"id":102257091}}]}`))

	default:
		w.Write([]byte(f.registryJSON()))
	}
}

// newFakeApp wires an app to a stateful fakeAPI seeded with Alice and Bob,
// the device linked to Alice.
func newFakeApp(t *testing.T) (*app, *fakeAPI, *strings.Builder) {
	t.Helper()
	f := &fakeAPI{
		members: []fakeMember{
			{UUID: "aaaaaaaa-0000-4000-8000-000000000001", Name: "Alice"},
			{UUID: "bbbbbbbb-0000-4000-8000-000000000002", Name: "Bob"},
		},
		linked: "aaaaaaaa-0000-4000-8000-000000000001",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/session-registry-installation" {
			w.Write([]byte(sessionResponse))
			return
		}
		f.handle(w, r)
	}))
	t.Cleanup(srv.Close)

	var stderr strings.Builder
	return &app{
		stdout:   &strings.Builder{},
		stderr:   &stderr,
		credPath: t.TempDir() + "/credentials.json",
		opts:     []tricount.Option{tricount.WithBaseURL(srv.URL)},
	}, f, &stderr
}

func (f *fakeAPI) memberNamed(name string) (fakeMember, bool) {
	for _, m := range f.members {
		if m.Name == name {
			return m, true
		}
	}
	return fakeMember{}, false
}

func TestJoinAsExistingMemberLinksWithoutCreating(t *testing.T) {
	a, f, stderr := newFakeApp(t)
	before := len(f.members)

	if code := a.run([]string{"join", "--as", "Bob", "tABC123xyz"}); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if len(f.members) != before {
		t.Errorf("member count = %d, want %d: an existing name must not be duplicated", len(f.members), before)
	}
	bob, _ := f.memberNamed("Bob")
	if f.linked != bob.UUID {
		t.Errorf("linked = %q, want Bob's %q", f.linked, bob.UUID)
	}
}

func TestJoinAsNewMemberCreatesThenLinks(t *testing.T) {
	a, f, stderr := newFakeApp(t)

	if code := a.run([]string{"join", "--as", "Carol", "tABC123xyz"}); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	carol, ok := f.memberNamed("Carol")
	if !ok {
		t.Fatalf("Carol was not created; members = %+v", f.members)
	}
	if f.linked != carol.UUID {
		t.Errorf("linked = %q, want Carol's %q", f.linked, carol.UUID)
	}
	if _, ok := f.memberNamed("Alice"); !ok {
		t.Error("adding a member must not drop the existing ones")
	}
}

func TestLinkCreateMakesTheMember(t *testing.T) {
	a, f, stderr := newFakeApp(t)

	if code := a.run([]string{"link", "--create", fmt.Sprint(fakeTricountID), "Dave"}); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	dave, ok := f.memberNamed("Dave")
	if !ok {
		t.Fatalf("Dave was not created; members = %+v", f.members)
	}
	if f.linked != dave.UUID {
		t.Errorf("linked = %q, want Dave's %q", f.linked, dave.UUID)
	}
}

func TestLinkWithoutCreateStillRefusesUnknownMember(t *testing.T) {
	a, f, stderr := newFakeApp(t)
	before := len(f.members)

	if code := a.run([]string{"link", fmt.Sprint(fakeTricountID), "Dave"}); code == 0 {
		t.Fatal("exit code = 0; without --create an unknown member must fail")
	}
	if len(f.members) != before {
		t.Error("no member should have been created")
	}
	if !strings.Contains(stderr.String(), "--create") {
		t.Errorf("the error should mention --create: %q", stderr.String())
	}
}

// stdoutOf runs a command and returns what it wrote, failing on a bad exit.
func stdoutOf(t *testing.T, a *app, stderr *strings.Builder, args ...string) string {
	t.Helper()
	buf := &strings.Builder{}
	a.stdout = buf
	if code := a.run(args); code != 0 {
		t.Fatalf("%v: exit code = %d, stderr = %q", args, code, stderr.String())
	}
	return buf.String()
}

func TestJoinJSONEmitsTheTricount(t *testing.T) {
	a, _, stderr := newFakeApp(t)
	out := stdoutOf(t, a, stderr, "join", "--json", "tABC123xyz")

	var got struct {
		ID    int64  `json:"ID"`
		Title string `json:"Title"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if got.ID != fakeTricountID || got.Title != "Taiwan" {
		t.Errorf("got %+v", got)
	}
}

func TestJoinAsJSONReflectsTheNewLink(t *testing.T) {
	a, f, stderr := newFakeApp(t)
	out := stdoutOf(t, a, stderr, "join", "--json", "--as", "Carol", "tABC123xyz")

	var got struct {
		LinkedMemberUUID string `json:"LinkedMemberUUID"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	carol, ok := f.memberNamed("Carol")
	if !ok {
		t.Fatal("Carol was not created")
	}
	if got.LinkedMemberUUID != carol.UUID {
		t.Errorf("LinkedMemberUUID = %q, want Carol's %q", got.LinkedMemberUUID, carol.UUID)
	}
}

func TestLinkJSONEmitsTheMember(t *testing.T) {
	a, _, stderr := newFakeApp(t)
	out := stdoutOf(t, a, stderr, "link", "--json", fmt.Sprint(fakeTricountID), "Bob")

	var got struct {
		UUID        string `json:"UUID"`
		DisplayName string `json:"DisplayName"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if got.DisplayName != "Bob" || got.UUID == "" {
		t.Errorf("got %+v", got)
	}
}

func TestLeaveJSONEmitsTheTricountLeft(t *testing.T) {
	a, _, stderr := newFakeApp(t)
	out := stdoutOf(t, a, stderr, "leave", "--json", fmt.Sprint(fakeTricountID))

	var got struct {
		ID int64 `json:"ID"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if got.ID != fakeTricountID {
		t.Errorf("ID = %d, want %d", got.ID, fakeTricountID)
	}
}

func TestWhoamiJSONCarriesIdentityAndLinks(t *testing.T) {
	a, _, stderr := newFakeApp(t)
	out := stdoutOf(t, a, stderr, "whoami", "--json")

	var got struct {
		Device      string
		User        int64
		Credentials string
		Tricounts   []struct {
			ID       int64
			Title    string
			LinkedAs string
		}
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if got.Device == "" || got.User == 0 || got.Credentials == "" {
		t.Errorf("identity fields are incomplete: %+v", got)
	}
	if len(got.Tricounts) != 1 || got.Tricounts[0].Title != "Taiwan" || got.Tricounts[0].LinkedAs != "Alice" {
		t.Errorf("tricounts = %+v", got.Tricounts)
	}
}

// keysOf decodes one JSON object and returns its top-level keys. Asserting on
// these is case-sensitive, which unmarshalling into a tagged struct is not.
func keysOf(t *testing.T, raw string) map[string]bool {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, raw)
	}
	keys := make(map[string]bool, len(obj))
	for k := range obj {
		keys[k] = true
	}
	return keys
}

func firstElement(t *testing.T, raw string) string {
	t.Helper()
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		t.Fatalf("not a JSON array: %v\n%s", err, raw)
	}
	if len(arr) == 0 {
		t.Fatalf("array is empty: %s", raw)
	}
	return string(arr[0])
}

// The CLI's own JSON uses Go field names, matching how the library's types
// marshal, so a script sees one convention across every command.
func TestJSONUsesGoFieldNames(t *testing.T) {
	t.Run("whoami", func(t *testing.T) {
		a, _, stderr := newFakeApp(t)
		got := keysOf(t, stdoutOf(t, a, stderr, "whoami", "--json"))
		for _, want := range []string{"Device", "User", "Credentials", "Tricounts"} {
			if !got[want] {
				t.Errorf("missing key %q; got %v", want, got)
			}
		}
	})

	t.Run("whoami tricount entries", func(t *testing.T) {
		a, _, stderr := newFakeApp(t)
		out := stdoutOf(t, a, stderr, "whoami", "--json")
		var obj struct{ Tricounts []json.RawMessage }
		if err := json.Unmarshal([]byte(out), &obj); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if len(obj.Tricounts) == 0 {
			t.Fatal("no tricounts in output")
		}
		got := keysOf(t, string(obj.Tricounts[0]))
		for _, want := range []string{"ID", "Title", "LinkedAs"} {
			if !got[want] {
				t.Errorf("missing key %q; got %v", want, got)
			}
		}
	})

	t.Run("list", func(t *testing.T) {
		a, _, stderr := newFakeApp(t)
		got := keysOf(t, firstElement(t, stdoutOf(t, a, stderr, "list", "--json")))
		for _, want := range []string{"ID", "Title", "Currency", "Archived"} {
			if !got[want] {
				t.Errorf("missing key %q; got %v", want, got)
			}
		}
	})
}
