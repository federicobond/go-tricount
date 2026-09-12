package tricount

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestClient returns a Client pointed at a server that answers session
// registration itself and delegates everything else to handler.
func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/session-registry-installation" {
			w.Write([]byte(sessionResponse))
			return
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return NewClient(testCredentials(), WithBaseURL(srv.URL)), srv
}

func TestGetTricount(t *testing.T) {
	fixture := loadFixture(t, "registry.json")
	var gotPath, gotQuery string

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.Write(fixture)
	})

	tri, err := c.GetTricount(context.Background(), "tABC123xyz")
	if err != nil {
		t.Fatalf("GetTricount: %v", err)
	}
	if tri.ID != 102257091 {
		t.Errorf("ID = %d", tri.ID)
	}
	if len(tri.Transactions) != 2 {
		t.Errorf("got %d transactions, want 2", len(tri.Transactions))
	}
	if gotPath != "/v1/user/79290957/registry" {
		t.Errorf("path = %q", gotPath)
	}
	if gotQuery != "public_identifier_token=tABC123xyz" {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestGetTricountRejectsEmptyToken(t *testing.T) {
	var called bool
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{"Response":[]}`))
	})

	if _, err := c.GetTricount(context.Background(), ""); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("err = %v, want ErrInvalidRequest", err)
	}
	if called {
		t.Error("an empty token must not reach the network")
	}
}

func TestGetTricountNotFound(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Response":[]}`))
	})
	if _, err := c.GetTricount(context.Background(), "tNOPE"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetTricountByID(t *testing.T) {
	fixture := loadFixture(t, "registry.json")
	var gotQuery string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write(fixture)
	})

	tri, err := c.GetTricountByID(context.Background(), 102257091)
	if err != nil {
		t.Fatalf("GetTricountByID: %v", err)
	}
	if tri.Title != "Taiwan" {
		t.Errorf("Title = %q", tri.Title)
	}
	if gotQuery != "registry_id=102257091" {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestListTricounts(t *testing.T) {
	var gotQuery string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"Response":[
			{"Registry":{"id":1,"title":"One","currency":"EUR","status":"READ_WRITE","memberships":[],"all_registry_entry":[]}},
			{"Registry":{"id":2,"title":"Two","currency":"JPY","status":"READ_ONLY","memberships":[],"all_registry_entry":[]}}
		]}`))
	})

	list, err := c.ListTricounts(context.Background())
	if err != nil {
		t.Fatalf("ListTricounts: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d tricounts, want 2", len(list))
	}
	if list[0].Title != "One" || list[1].Title != "Two" {
		t.Errorf("titles = %q, %q", list[0].Title, list[1].Title)
	}
	if !list[1].IsArchived() {
		t.Error("the second tricount should be archived")
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want none", gotQuery)
	}
}

func TestListTricountsEmpty(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Response":[]}`))
	})
	list, err := c.ListTricounts(context.Background())
	if err != nil {
		t.Fatalf("ListTricounts: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("got %d tricounts, want 0", len(list))
	}
}

func TestCreateTricount(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"Response":[{"Id":{"id":104488759}}]}`))
	})

	id, err := c.CreateTricount(context.Background(), "Viaje 2027", "EUR", "Road trip")
	if err != nil {
		t.Fatalf("CreateTricount: %v", err)
	}
	if id != 104488759 {
		t.Errorf("id = %d", id)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/v1/user/79290957/registry" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["title"] != "Viaje 2027" || gotBody["currency"] != "EUR" || gotBody["description"] != "Road trip" {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestCreateTricountValidation(t *testing.T) {
	var called bool
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{"Response":[{"Id":{"id":1}}]}`))
	})

	if _, err := c.CreateTricount(context.Background(), "", "EUR", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Error("an empty title should be rejected")
	}
	if _, err := c.CreateTricount(context.Background(), "Trip", "", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Error("an empty currency should be rejected")
	}
	if called {
		t.Error("validation failures must not reach the network")
	}
}

func TestUpdateTricountSendsOnlySetFields(t *testing.T) {
	var gotBody map[string]any
	var gotMethod, gotPath string

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"Response":[{"Id":{"id":102257091}}]}`))
	})

	tri := &Tricount{ID: 102257091, Title: "Taiwan", Currency: "JPY", Status: StatusActive}
	title, emoji := "Taiwan 2026", "🍜"
	if err := c.UpdateTricount(context.Background(), tri, TricountUpdate{Title: &title, Emoji: &emoji}); err != nil {
		t.Fatalf("UpdateTricount: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", gotMethod)
	}
	if gotPath != "/v1/user/79290957/registry/102257091" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["title"] != "Taiwan 2026" || gotBody["emoji"] != "🍜" {
		t.Errorf("body = %+v", gotBody)
	}
	if _, ok := gotBody["category"]; ok {
		t.Error("an unset field must not be sent")
	}
	for _, forbidden := range []string{"currency", "description"} {
		if _, ok := gotBody[forbidden]; ok {
			t.Errorf("%s must never be sent on an update", forbidden)
		}
	}
	if tri.Title != "Taiwan 2026" {
		t.Errorf("local Title = %q, want it refreshed", tri.Title)
	}
	if tri.Emoji != "🍜" {
		t.Errorf("local Emoji = %q, want it refreshed", tri.Emoji)
	}
}

func TestUpdateTricountEmptyIsNoOp(t *testing.T) {
	var called bool
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{"Response":[{"Id":{"id":1}}]}`))
	})
	tri := &Tricount{ID: 1, Currency: "EUR"}
	if err := c.UpdateTricount(context.Background(), tri, TricountUpdate{}); err != nil {
		t.Fatalf("UpdateTricount with nothing set: %v", err)
	}
	if called {
		t.Error("an update with no fields set should make no request")
	}
}

func TestArchiveAndUnarchive(t *testing.T) {
	var bodies []map[string]any
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		bodies = append(bodies, b)
		w.Write([]byte(`{"Response":[{"Id":{"id":1}}]}`))
	})

	tri := &Tricount{ID: 1, Currency: "EUR", Status: StatusActive}
	if err := c.ArchiveTricount(context.Background(), tri); err != nil {
		t.Fatalf("ArchiveTricount: %v", err)
	}
	if !tri.IsArchived() {
		t.Error("local status not updated to archived")
	}
	if err := c.UnarchiveTricount(context.Background(), tri); err != nil {
		t.Fatalf("UnarchiveTricount: %v", err)
	}
	if tri.IsArchived() {
		t.Error("local status not updated to active")
	}

	if len(bodies) != 2 {
		t.Fatalf("got %d requests, want 2", len(bodies))
	}
	if bodies[0]["status"] != "READ_ONLY" {
		t.Errorf("archive body = %+v", bodies[0])
	}
	if bodies[1]["status"] != "READ_WRITE" {
		t.Errorf("unarchive body = %+v", bodies[1])
	}
}

func TestDeleteTricount(t *testing.T) {
	var gotMethod, gotPath string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Write([]byte(`{"Response":[{"Id":{"id":104488759}}]}`))
	})

	tri := &Tricount{ID: 104488759, Currency: "EUR"}
	if err := c.DeleteTricount(context.Background(), tri); err != nil {
		t.Fatalf("DeleteTricount: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", gotMethod)
	}
	if gotPath != "/v1/user/79290957/registry/104488759" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestMutationsRejectNilAndZeroTricount(t *testing.T) {
	var called bool
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{"Response":[{"Id":{"id":1}}]}`))
	})
	ctx := context.Background()
	title := "x"

	if err := c.UpdateTricount(ctx, nil, TricountUpdate{Title: &title}); !errors.Is(err, ErrInvalidRequest) {
		t.Error("a nil tricount should be rejected")
	}
	if err := c.ArchiveTricount(ctx, &Tricount{}); !errors.Is(err, ErrInvalidRequest) {
		t.Error("a zero-id tricount should be rejected")
	}
	if err := c.DeleteTricount(ctx, nil); !errors.Is(err, ErrInvalidRequest) {
		t.Error("a nil tricount should be rejected")
	}
	if called {
		t.Error("validation failures must not reach the network")
	}
}

// syncResponse is a registry-synchronization reply. Note that the registries
// inside it are NOT wrapped in {"Registry": ...}, unlike every other endpoint.
const syncResponse = `{"Response":[{"RegistrySynchronization":{
	"all_registry_active":[
		{"id":102257091,"title":"Taiwan","currency":"JPY","status":"READ_WRITE",
		 "public_identifier_token":"tABC123xyz","memberships":[],"all_registry_entry":[]}
	],
	"all_registry_archived":[
		{"id":102257092,"title":"Old","currency":"EUR","status":"READ_ONLY",
		 "public_identifier_token":"tOLD","memberships":[],"all_registry_entry":[]}
	]
}}]}`

func TestSyncTricounts(t *testing.T) {
	var gotPath string
	var gotBody map[string][]map[string]string

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(syncResponse))
	})

	res, err := c.SyncTricounts(context.Background(), []string{"tABC123xyz"}, []string{"tOLD"})
	if err != nil {
		t.Fatalf("SyncTricounts: %v", err)
	}
	if gotPath != "/v1/user/79290957/registry-synchronization" {
		t.Errorf("path = %q", gotPath)
	}
	if len(gotBody["all_registry_active"]) != 1 ||
		gotBody["all_registry_active"][0]["public_identifier_token"] != "tABC123xyz" {
		t.Errorf("active tokens = %+v", gotBody["all_registry_active"])
	}
	if len(gotBody["all_registry_archived"]) != 1 {
		t.Errorf("archived tokens = %+v", gotBody["all_registry_archived"])
	}
	if gotBody["all_registry_deleted"] == nil {
		t.Error("all_registry_deleted must be sent, even empty")
	}

	if len(res.Active) != 1 || res.Active[0].ID != 102257091 {
		t.Errorf("Active = %+v", res.Active)
	}
	if len(res.Archived) != 1 || res.Archived[0].Title != "Old" {
		t.Errorf("Archived = %+v", res.Archived)
	}
}

func TestJoinTricountSyncsThenRereads(t *testing.T) {
	fixture := loadFixture(t, "registry.json")
	var paths []string

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/v1/user/79290957/registry-synchronization" {
			w.Write([]byte(syncResponse))
			return
		}
		w.Write(fixture)
	})

	tri, err := c.JoinTricount(context.Background(), "tABC123xyz")
	if err != nil {
		t.Fatalf("JoinTricount: %v", err)
	}
	if len(tri.Transactions) != 2 {
		t.Errorf("got %d transactions, want the full re-read of 2", len(tri.Transactions))
	}
	if len(paths) != 2 {
		t.Fatalf("made %d requests (%v), want sync then re-read", len(paths), paths)
	}
	if paths[0] != "/v1/user/79290957/registry-synchronization" {
		t.Errorf("first request was %q", paths[0])
	}
	if paths[1] != "/v1/user/79290957/registry" {
		t.Errorf("second request was %q", paths[1])
	}
}

func TestJoinTricountNotInSyncResult(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Response":[{"RegistrySynchronization":{"all_registry_active":[],"all_registry_archived":[]}}]}`))
	})
	if _, err := c.JoinTricount(context.Background(), "tNOPE"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestJoinTricountRejectsEmptyToken(t *testing.T) {
	var called bool
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(syncResponse))
	})
	if _, err := c.JoinTricount(context.Background(), ""); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("err = %v, want ErrInvalidRequest", err)
	}
	if called {
		t.Error("an empty token must not reach the network")
	}
}

func TestLeaveTricount(t *testing.T) {
	var gotBody map[string][]map[string]string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"Response":[{"RegistrySynchronization":{"all_registry_active":[],"all_registry_archived":[]}}]}`))
	})

	tri := &Tricount{ID: 102257091, Currency: "JPY", PublicToken: "tABC123xyz"}
	if err := c.LeaveTricount(context.Background(), tri); err != nil {
		t.Fatalf("LeaveTricount: %v", err)
	}
	if len(gotBody["all_registry_deleted"]) != 1 ||
		gotBody["all_registry_deleted"][0]["public_identifier_token"] != "tABC123xyz" {
		t.Errorf("deleted tokens = %+v", gotBody["all_registry_deleted"])
	}
	if len(gotBody["all_registry_active"]) != 0 {
		t.Errorf("active should be empty, got %+v", gotBody["all_registry_active"])
	}
}

func TestLeaveTricountNeedsPublicToken(t *testing.T) {
	var called bool
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	if err := c.LeaveTricount(context.Background(), &Tricount{ID: 1, Currency: "EUR"}); !errors.Is(err, ErrInvalidRequest) {
		t.Error("leaving without a public token should be rejected")
	}
	if called {
		t.Error("validation failures must not reach the network")
	}
}
