//go:build live

package tricount

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestLiveProbeLinkScope answers whether membership_uuid_active is per-device
// or shared by everyone who syncs the tricount, by registering a second device
// and watching what each one sees.
func TestLiveProbeLinkScope(t *testing.T) {
	if os.Getenv("TRICOUNT_PROBE") != "1" {
		t.Skip("set TRICOUNT_PROBE=1")
	}
	ctx := context.Background()

	c1 := liveClient(t)
	cleanupThisRun(t)
	tri := liveTricount(t)
	t.Logf("device 1 = user %d", c1.UserID())

	// A second member to link to, so the two devices can differ.
	extra := tagged("SecondMember")
	if err := c1.AddMembers(ctx, tri, extra); err != nil {
		t.Fatalf("AddMembers: %v", err)
	}
	target := tri.MemberByName(extra)

	// A brand-new device, as if this were someone's phone opening the link.
	dir := t.TempDir()
	creds2, err := LoadOrGenerateCredentials(filepath.Join(dir, "creds2.json"))
	if err != nil {
		t.Fatalf("second credentials: %v", err)
	}
	c2 := NewClient(creds2)
	uid2, err := c2.Authenticate(ctx)
	if err != nil {
		t.Fatalf("second device authenticate: %v", err)
	}
	t.Logf("device 2 = user %d (a different user id: %v)", uid2, uid2 != c1.UserID())

	joined, err := c2.JoinTricount(ctx, tri.PublicToken)
	if err != nil {
		t.Fatalf("second device join: %v", err)
	}
	t.Logf("on joining, device 2 was auto-linked to %q", nameOf(joined, joined.LinkedMemberUUID))

	before1, err := c1.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("device 1 read: %v", err)
	}
	t.Logf("before: device1 link = %q, device2 link = %q",
		nameOf(before1, before1.LinkedMemberUUID), nameOf(joined, joined.LinkedMemberUUID))

	// Device 2 claims the second member.
	if err := c2.LinkToMember(ctx, joined, target); err != nil {
		t.Fatalf("device 2 LinkToMember: %v", err)
	}

	after1, err := c1.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("device 1 re-read: %v", err)
	}
	after2, err := c2.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("device 2 re-read: %v", err)
	}

	t.Logf("after device 2 linked to %q:", extra)
	t.Logf("  device 1 sees its link as %q", nameOf(after1, after1.LinkedMemberUUID))
	t.Logf("  device 2 sees its link as %q", nameOf(after2, after2.LinkedMemberUUID))

	if after1.LinkedMemberUUID == before1.LinkedMemberUUID {
		t.Log("PROBE RESULT: membership_uuid_active is PER-DEVICE — device 1's link was untouched")
	} else {
		t.Log("PROBE RESULT: membership_uuid_active is SHARED — device 2's change moved device 1's link too")
	}

	// Leave the fixture as found: device 2 stops following it.
	if err := c2.LeaveTricount(ctx, after2); err != nil {
		t.Logf("device 2 leave: %v", err)
	}
}

func nameOf(t *Tricount, uuid string) string {
	if uuid == "" {
		return "(unlinked)"
	}
	if m := t.MemberByUUID(uuid); m != nil {
		return m.DisplayName
	}
	return uuid
}
