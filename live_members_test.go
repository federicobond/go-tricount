//go:build live

package tricount

import (
	"context"
	"testing"
)

func TestLiveMemberLifecycle(t *testing.T) {
	c := liveClient(t)
	cleanupThisRun(t)
	tri := liveTricount(t)
	ctx := context.Background()

	before := len(tri.Members)
	name := tagged("Carol")

	if err := c.AddMembers(ctx, tri, name); err != nil {
		t.Fatalf("AddMembers: %v", err)
	}
	if len(tri.Members) != before+1 {
		t.Fatalf("member count = %d, want %d", len(tri.Members), before+1)
	}
	carol := tri.MemberByName(name)
	if carol == nil {
		t.Fatalf("added member %q is missing after the refresh", name)
	}
	if carol.ID == 0 {
		t.Error("added member has no server-assigned id")
	}

	renamed := tagged("Caroline")
	if err := c.RenameMember(ctx, tri, carol, renamed); err != nil {
		t.Fatalf("RenameMember: %v", err)
	}
	if tri.MemberByName(renamed) == nil {
		t.Errorf("rename did not persist; members are %v", memberNames(tri))
	}
	if tri.MemberByName(name) != nil {
		t.Errorf("the old name is still present; members are %v", memberNames(tri))
	}

	// Renaming must survive a full re-read, since the per-membership endpoint
	// is the one that silently fails to persist.
	reread, err := c.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}
	if reread.MemberByName(renamed) == nil {
		t.Errorf("rename did not survive a re-read; members are %v", memberNames(reread))
	}

	target := reread.MemberByName(renamed)
	if err := c.DeleteMember(ctx, reread, target); err != nil {
		t.Fatalf("DeleteMember: %v", err)
	}
	final, err := c.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("re-reading after delete: %v", err)
	}
	if final.MemberByName(renamed) != nil {
		t.Errorf("member still present after deletion; members are %v", memberNames(final))
	}
}

func TestLiveLinkToMember(t *testing.T) {
	c := liveClient(t)
	cleanupThisRun(t)
	tri := liveTricount(t)
	ctx := context.Background()

	name := tagged("Linkee")
	if err := c.AddMembers(ctx, tri, name); err != nil {
		t.Fatalf("AddMembers: %v", err)
	}
	target := tri.MemberByName(name)
	if target == nil {
		t.Fatalf("member %q missing", name)
	}

	if err := c.LinkToMember(ctx, tri, target); err != nil {
		t.Fatalf("LinkToMember: %v", err)
	}
	reread, err := c.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}
	if reread.LinkedMemberUUID != target.UUID {
		t.Errorf("LinkedMemberUUID = %q, want %q", reread.LinkedMemberUUID, target.UUID)
	}

	// Link back to the first member so the fixture is left in a sane state.
	// Unlinking is not possible, so switching is the best we can do.
	if len(reread.Members) > 0 && reread.Members[0].UUID != target.UUID {
		if err := c.LinkToMember(ctx, reread, reread.Members[0]); err != nil {
			t.Logf("relinking to the first member: %v", err)
		}
	}
}

func memberNames(t *Tricount) []string {
	names := make([]string, 0, len(t.Members))
	for _, m := range t.Members {
		names = append(names, m.DisplayName)
	}
	return names
}
