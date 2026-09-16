package tricount

import (
	"context"
	"fmt"
	"net/http"
)

// Every member mutation goes through PUT /registry/{id} with the complete
// memberships array. The API offers a per-membership endpoint,
// PUT .../registry-membership/{id}, which returns 200 and then silently
// discards name changes — so this file never uses it. Resending the full list
// is not an optimization opportunity either: members omitted from the array
// are dropped.

// AddMembers adds members by name. Adding no members is a no-op.
func (c *Client) AddMembers(ctx context.Context, t *Tricount, names ...string) error {
	if err := checkTricount(t); err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	memberships := make([]map[string]any, 0, len(t.Members)+len(names))
	for _, m := range t.Members {
		memberships = append(memberships, membershipPayload(m))
	}
	for _, name := range names {
		if name == "" {
			return fmt.Errorf("%w: member name is empty", ErrInvalidRequest)
		}
		uuid, err := newUUID()
		if err != nil {
			return err
		}
		memberships = append(memberships, membershipPayload(&Member{
			UUID:        uuid,
			DisplayName: name,
			Status:      MemberActive,
		}))
	}
	if err := c.putRegistry(ctx, t, map[string]any{"memberships": memberships}); err != nil {
		return err
	}
	return c.refreshMembers(ctx, t)
}

// RenameMember changes a member's display name.
func (c *Client) RenameMember(ctx context.Context, t *Tricount, m *Member, newName string) error {
	if err := checkMember(t, m); err != nil {
		return err
	}
	if newName == "" {
		return fmt.Errorf("%w: new member name is empty", ErrInvalidRequest)
	}
	memberships := make([]map[string]any, 0, len(t.Members))
	for _, existing := range t.Members {
		payload := membershipPayload(existing)
		if existing.UUID == m.UUID {
			payload["alias"].(map[string]any)["name"] = newName
		}
		memberships = append(memberships, payload)
	}
	if err := c.putRegistry(ctx, t, map[string]any{"memberships": memberships}); err != nil {
		return err
	}
	return c.refreshMembers(ctx, t)
}

// DeleteMember removes a member. A member who already appears in transactions
// may be marked deleted by the server rather than removed outright; re-read
// the tricount to see what happened.
func (c *Client) DeleteMember(ctx context.Context, t *Tricount, m *Member) error {
	if err := checkMember(t, m); err != nil {
		return err
	}
	if m.ID == 0 {
		return fmt.Errorf("%w: member %q has no membership id, which deletion needs",
			ErrInvalidRequest, m.DisplayName)
	}
	memberships := make([]map[string]any, 0, len(t.Members))
	for _, existing := range t.Members {
		if existing.UUID == m.UUID {
			continue
		}
		memberships = append(memberships, membershipPayload(existing))
	}
	body := map[string]any{
		"memberships":            memberships,
		"deleted_membership_ids": []int64{m.ID},
	}
	if err := c.putRegistry(ctx, t, body); err != nil {
		return err
	}
	return c.refreshMembers(ctx, t)
}

// LinkToMember makes this device count as the given member, which is what the
// app uses to show "your" balance.
//
// The link is per-device, verified against the live API: two devices following
// the same tricount hold independent links, and one changing its own leaves the
// other's alone. A member can hold more than one link, also verified: a second
// device may claim the member a first is already linked to, and both then count
// as that member. One person running this from a laptop and a server need not
// share a credentials file to be the same member in a tricount.
//
// A device that has just joined is auto-linked to the member with the lowest
// ID, so a tricount created through this library links its creator to the
// placeholder member the API adds.
//
// The API has no way to unlink: once linked you can only switch to another
// member.
func (c *Client) LinkToMember(ctx context.Context, t *Tricount, m *Member) error {
	if err := checkMember(t, m); err != nil {
		return err
	}
	if err := c.putRegistry(ctx, t, map[string]any{"membership_uuid_active": m.UUID}); err != nil {
		return err
	}
	t.LinkedMemberUUID = m.UUID
	return nil
}

// membershipPayload renders a member in the request form. The alias is flat
// here (name/type/value); responses nest it under pointer instead.
func membershipPayload(m *Member) map[string]any {
	status := m.Status
	if status == "" {
		status = MemberActive
	}
	return map[string]any{
		"uuid":                      m.UUID,
		"status":                    string(status),
		"auto_add_card_transaction": "",
		"setting":                   nil,
		"alias": map[string]any{
			"type":  "UUID",
			"value": m.UUID,
			"name":  m.DisplayName,
		},
	}
}

// refreshMembers re-reads the membership list so the caller's Tricount
// reflects what the server actually stored, including any server-assigned IDs
// for members just added.
func (c *Client) refreshMembers(ctx context.Context, t *Tricount) error {
	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		userPath: fmt.Sprintf("/registry/%d/registry-membership", t.ID),
	})
	if err != nil {
		return err
	}
	wires, err := decodeEnvelope[wireMembership](body, "RegistryMembershipNonUser")
	if err != nil {
		return err
	}
	// This endpoint returns deleted memberships as well as live ones, so the
	// two are separated here. Resending a deleted membership makes the server
	// create a duplicate rather than restore it, which is why Members must
	// hold only active ones.
	var active, former []*Member
	for _, w := range wires {
		m := w.toDomain()
		if m.Status == MemberActive {
			active = append(active, m)
		} else {
			former = append(former, m)
		}
	}
	t.Members, t.FormerMembers = active, former
	return nil
}

func checkMember(t *Tricount, m *Member) error {
	if err := checkTricount(t); err != nil {
		return err
	}
	if m == nil {
		return fmt.Errorf("%w: member is nil", ErrInvalidRequest)
	}
	if m.UUID == "" {
		return fmt.Errorf("%w: member %q has no uuid", ErrInvalidRequest, m.DisplayName)
	}
	if !t.IsActiveMember(m.UUID) {
		if t.MemberByUUID(m.UUID) != nil {
			return fmt.Errorf("%w: member %q has been removed from tricount %d",
				ErrInvalidRequest, m.DisplayName, t.ID)
		}
		return fmt.Errorf("%w: member %q is not in tricount %d",
			ErrInvalidRequest, m.DisplayName, t.ID)
	}
	return nil
}
