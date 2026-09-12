//go:build live

package tricount

import (
	"os"
	"testing"
)

// TestLiveInspectFixture dumps the fixture's current contents: members,
// transactions and balances. It is a read-only inspector for answering "what is
// in there right now", not part of the suite, so it only runs when asked:
//
//	TRICOUNT_PROBE=1 go test -tags=live -run TestLiveInspectFixture -v ./...
func TestLiveInspectFixture(t *testing.T) {
	if os.Getenv("TRICOUNT_PROBE") != "1" {
		t.Skip("set TRICOUNT_PROBE=1 to inspect the live fixture")
	}
	c := liveClient(t)
	tri := liveTricount(t)

	t.Logf("tricount %q (id %d, %s) created %s",
		tri.Title, tri.ID, tri.Currency, tri.Created.Format("2006-01-02 15:04:05"))
	t.Logf("this device (user %d) is linked to %q", c.UserID(),
		nameOf(tri, tri.LinkedMemberUUID))

	t.Logf("active members (%d):", len(tri.Members))
	for _, m := range tri.Members {
		t.Logf("  id=%d uuid=%s name=%q", m.ID, m.UUID, m.DisplayName)
	}
	t.Logf("former members (%d)", len(tri.FormerMembers))
	t.Logf("transactions (%d):", len(tri.Transactions))
	for _, tx := range tri.Transactions {
		t.Logf("  id=%d %s %s %q payer=%s",
			tx.ID, tx.Kind, tx.Amount, tx.Description, nameOf(tri, tx.PayerUUID))
	}

	if len(tri.Transactions) > 0 {
		balances, err := tri.Balances()
		if err != nil {
			t.Fatalf("Balances: %v", err)
		}
		t.Log("balances:")
		for name, b := range balances {
			t.Logf("  %-30s %s", name, b)
		}
	}
}
