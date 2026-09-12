//go:build live

package tricount

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestLiveResetFixture removes every tagged leftover from the fixture — from
// any run, not just this one — and leaves the fixture tricount itself intact.
//
// It is a maintenance tool, not part of the suite: it only runs when
// TRICOUNT_RESET_FIXTURE=1 is set, because deleting everything tagged would be
// wrong to do while another run is in flight.
//
//	TRICOUNT_RESET_FIXTURE=1 go test -tags=live -run TestLiveResetFixture -v ./...
func TestLiveResetFixture(t *testing.T) {
	if os.Getenv("TRICOUNT_RESET_FIXTURE") != "1" {
		t.Skip("set TRICOUNT_RESET_FIXTURE=1 to reset the live fixture")
	}
	c := liveClient(t)
	tri := liveTricount(t)
	ctx := context.Background()

	// Transactions first: a member still named by one may not be deletable.
	var removedTx, removedMembers int
	for _, tx := range tri.Transactions {
		if !isTagged(tx.Description) {
			continue
		}
		if err := c.DeleteTransaction(ctx, tri, tx.ID); err != nil {
			t.Errorf("deleting transaction %d (%q): %v", tx.ID, tx.Description, err)
			continue
		}
		removedTx++
	}

	tri, err := c.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}
	for {
		var target *Member
		for _, m := range tri.Members {
			if isTagged(m.DisplayName) {
				target = m
				break
			}
		}
		if target == nil {
			break
		}
		if err := c.DeleteMember(ctx, tri, target); err != nil {
			t.Errorf("deleting member %q: %v", target.DisplayName, err)
			break
		}
		removedMembers++
		if tri, err = c.GetTricountByID(ctx, tri.ID); err != nil {
			t.Fatalf("re-reading after a member delete: %v", err)
		}
	}

	t.Logf("removed %d transactions and %d members; fixture now has %d members and %d transactions",
		removedTx, removedMembers, len(tri.Members), len(tri.Transactions))
	if tri.ID == 0 {
		t.Fatal("the fixture tricount must survive a reset")
	}
}

// isTagged reports whether a name carries any run's tag, not only this run's.
func isTagged(s string) bool { return strings.Contains(s, "[go-live-") }
