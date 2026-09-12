package tricount

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// membershipListResponse is what GET registry-membership returns; the client
// uses it to refresh members after a change.
const membershipListResponse = `{"Response":[
	{"RegistryMembershipNonUser":{"id":1,"uuid":"uuid-alice","status":"ACTIVE",
	 "alias":{"display_name":"Alice","pointer":{"type":"UUID","value":"uuid-alice","name":"Alice"}}}},
	{"RegistryMembershipNonUser":{"id":2,"uuid":"uuid-bob","status":"ACTIVE",
	 "alias":{"display_name":"Bob","pointer":{"type":"UUID","value":"uuid-bob","name":"Bob"}}}}
]}`

func twoMemberTricount() *Tricount {
	return &Tricount{
		ID:       1,
		Currency: "EUR",
		Status:   StatusActive,
		Members: []*Member{
			{ID: 1, UUID: "uuid-alice", DisplayName: "Alice", Status: MemberActive},
			{ID: 2, UUID: "uuid-bob", DisplayName: "Bob", Status: MemberActive},
		},
	}
}

// memberTestServer answers the membership PUT and the membership refresh GET,
// recording the PUT body.
func memberTestServer(t *testing.T, put *map[string]any) *Client {
	t.Helper()
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(membershipListResponse))
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding PUT body: %v", err)
		}
		*put = body
		w.Write([]byte(`{"Response":[{"Id":{"id":1}}]}`))
	})
	return c
}

func TestAddMembersSendsFullList(t *testing.T) {
	var put map[string]any
	c := memberTestServer(t, &put)
	tri := twoMemberTricount()

	if err := c.AddMembers(context.Background(), tri, "Carol", "Dave"); err != nil {
		t.Fatalf("AddMembers: %v", err)
	}

	memberships, ok := put["memberships"].([]any)
	if !ok {
		t.Fatalf("body has no memberships array: %+v", put)
	}
	if len(memberships) != 4 {
		t.Fatalf("sent %d memberships, want 4 (2 existing + 2 new)", len(memberships))
	}

	names := map[string]bool{}
	for _, raw := range memberships {
		m := raw.(map[string]any)
		alias, ok := m["alias"].(map[string]any)
		if !ok {
			t.Fatalf("membership has no alias: %+v", m)
		}
		if _, bad := alias["display_name"]; bad {
			t.Error("alias used the response form (display_name) in a request")
		}
		if alias["type"] != "UUID" {
			t.Errorf("alias type = %v, want UUID", alias["type"])
		}
		if alias["value"] != m["uuid"] {
			t.Errorf("alias value %v does not match uuid %v", alias["value"], m["uuid"])
		}
		if m["status"] != "ACTIVE" {
			t.Errorf("status = %v, want ACTIVE", m["status"])
		}
		names[alias["name"].(string)] = true
	}
	for _, want := range []string{"Alice", "Bob", "Carol", "Dave"} {
		if !names[want] {
			t.Errorf("membership list is missing %q; got %v", want, names)
		}
	}

	if len(tri.Members) != 2 {
		t.Errorf("local members = %d, want the 2 the stub server reports", len(tri.Members))
	}
}

func TestAddMembersValidation(t *testing.T) {
	var put map[string]any
	c := memberTestServer(t, &put)
	tri := twoMemberTricount()
	ctx := context.Background()

	if err := c.AddMembers(ctx, tri); err != nil {
		t.Errorf("adding no members should be a no-op, got %v", err)
	}
	if put != nil {
		t.Error("adding no members should make no request")
	}
	if err := c.AddMembers(ctx, tri, ""); !errors.Is(err, ErrInvalidRequest) {
		t.Error("an empty member name should be rejected")
	}
	if err := c.AddMembers(ctx, nil, "Carol"); !errors.Is(err, ErrInvalidRequest) {
		t.Error("a nil tricount should be rejected")
	}
}

func TestRenameMemberSendsFullListWithNewName(t *testing.T) {
	var put map[string]any
	c := memberTestServer(t, &put)
	tri := twoMemberTricount()
	bob := tri.MemberByName("Bob")

	if err := c.RenameMember(context.Background(), tri, bob, "Roberto"); err != nil {
		t.Fatalf("RenameMember: %v", err)
	}

	memberships := put["memberships"].([]any)
	if len(memberships) != 2 {
		t.Fatalf("sent %d memberships, want 2", len(memberships))
	}
	got := map[string]string{}
	for _, raw := range memberships {
		m := raw.(map[string]any)
		alias := m["alias"].(map[string]any)
		got[m["uuid"].(string)] = alias["name"].(string)
	}
	if got["uuid-bob"] != "Roberto" {
		t.Errorf("Bob's new name = %q, want \"Roberto\"", got["uuid-bob"])
	}
	if got["uuid-alice"] != "Alice" {
		t.Errorf("Alice's name changed to %q; it should be resent unchanged", got["uuid-alice"])
	}
	if _, ok := put["deleted_membership_ids"]; ok {
		t.Error("a rename must not send deleted_membership_ids")
	}
}

func TestRenameMemberValidation(t *testing.T) {
	var put map[string]any
	c := memberTestServer(t, &put)
	tri := twoMemberTricount()
	ctx := context.Background()

	if err := c.RenameMember(ctx, tri, tri.Members[0], ""); !errors.Is(err, ErrInvalidRequest) {
		t.Error("an empty new name should be rejected")
	}
	if err := c.RenameMember(ctx, tri, nil, "X"); !errors.Is(err, ErrInvalidRequest) {
		t.Error("a nil member should be rejected")
	}
	stranger := &Member{ID: 99, UUID: "uuid-stranger", DisplayName: "Stranger"}
	if err := c.RenameMember(ctx, tri, stranger, "X"); !errors.Is(err, ErrInvalidRequest) {
		t.Error("a member not in this tricount should be rejected")
	}
	if put != nil {
		t.Error("validation failures must not reach the network")
	}
}

func TestDeleteMemberOmitsAndListsID(t *testing.T) {
	var put map[string]any
	c := memberTestServer(t, &put)
	tri := twoMemberTricount()
	bob := tri.MemberByName("Bob")

	if err := c.DeleteMember(context.Background(), tri, bob); err != nil {
		t.Fatalf("DeleteMember: %v", err)
	}

	memberships := put["memberships"].([]any)
	if len(memberships) != 1 {
		t.Fatalf("sent %d memberships, want 1 (Bob omitted)", len(memberships))
	}
	if uuid := memberships[0].(map[string]any)["uuid"]; uuid != "uuid-alice" {
		t.Errorf("remaining membership = %v, want uuid-alice", uuid)
	}
	ids, ok := put["deleted_membership_ids"].([]any)
	if !ok || len(ids) != 1 {
		t.Fatalf("deleted_membership_ids = %+v, want one entry", put["deleted_membership_ids"])
	}
	if int64(ids[0].(float64)) != bob.ID {
		t.Errorf("deleted id = %v, want %d", ids[0], bob.ID)
	}
}

func TestLinkToMember(t *testing.T) {
	var put map[string]any
	c := memberTestServer(t, &put)
	tri := twoMemberTricount()
	bob := tri.MemberByName("Bob")

	if err := c.LinkToMember(context.Background(), tri, bob); err != nil {
		t.Fatalf("LinkToMember: %v", err)
	}
	if put["membership_uuid_active"] != "uuid-bob" {
		t.Errorf("body = %+v, want membership_uuid_active uuid-bob", put)
	}
	if _, ok := put["memberships"]; ok {
		t.Error("linking should not resend the membership list")
	}
	if tri.LinkedMemberUUID != "uuid-bob" {
		t.Errorf("local LinkedMemberUUID = %q, want uuid-bob", tri.LinkedMemberUUID)
	}
	if tri.LinkedMember() != bob {
		t.Error("LinkedMember should resolve to Bob")
	}
}

func TestMemberEndpointIsNeverUsedForRenames(t *testing.T) {
	// PUT .../registry-membership/{id} returns 200 and silently discards name
	// changes, so the library must never send a rename there.
	var paths []string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodGet {
			w.Write([]byte(membershipListResponse))
			return
		}
		w.Write([]byte(`{"Response":[{"Id":{"id":1}}]}`))
	})

	tri := twoMemberTricount()
	if err := c.RenameMember(context.Background(), tri, tri.Members[0], "Alicia"); err != nil {
		t.Fatalf("RenameMember: %v", err)
	}
	for _, p := range paths {
		if strings.Contains(p, "/registry-membership/") {
			t.Errorf("rename touched the per-membership endpoint: %s", p)
		}
	}
}
