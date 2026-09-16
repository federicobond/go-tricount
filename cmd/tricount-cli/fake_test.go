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
