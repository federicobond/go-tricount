//go:build live

package tricount

import (
	"context"
	"testing"
)

// TestLiveEntryUUIDIsIdempotencyKey pins down the three undocumented
// behaviours that make a caller-chosen entry UUID usable as an idempotency
// key.
func TestLiveEntryUUIDIsIdempotencyKey(t *testing.T) {
	ctx := context.Background()

	c := liveClient(t)
	cleanupThisRun(t)
	tri := liveTricount(t)
	alice, bob := liveTwoMembers(t, c, tri)

	chosen, err := newUUID()
	if err != nil {
		t.Fatalf("newUUID: %v", err)
	}

	expense := func(amount string) Expense {
		return Expense{
			UUID:        chosen,
			Description: tagged("UUIDKey"),
			Amount:      MustParseAmount(amount, "EUR"),
			Payer:       alice,
			Split:       SplitEqually(alice, bob),
		}
	}

	// 1. The server keeps the caller's UUID rather than assigning its own.
	first, err := c.CreateExpense(ctx, tri, expense("10.00"))
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	reread, err := c.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}
	tx := reread.TransactionByID(first)
	if tx == nil {
		t.Fatalf("transaction %d is missing after creation", first)
	}
	if tx.UUID != chosen {
		t.Fatalf("UUID = %q, want the caller's %q; it cannot serve as a key", tx.UUID, chosen)
	}

	// 2. Re-sending the same UUID does not create a second entry.
	second, err := c.CreateExpense(ctx, tri, expense("10.00"))
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if second != first {
		t.Errorf("re-sending the UUID returned id %d, want the original %d", second, first)
	}

	// 3. The first write wins: a repeat with different content is ignored.
	if _, err := c.CreateExpense(ctx, tri, expense("99.00")); err != nil {
		t.Fatalf("third create: %v", err)
	}

	after, err := c.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("re-reading after the third create: %v", err)
	}
	var n int
	var seen *Transaction
	for i := range after.Transactions {
		if after.Transactions[i].UUID == chosen {
			n++
			seen = after.Transactions[i]
		}
	}
	if n != 1 {
		t.Fatalf("%d entries carry UUID %s, want exactly 1", n, chosen)
	}
	if got := seen.Amount.String(); got != "10.00" {
		t.Errorf("Amount = %q, want \"10.00\"; the API upserts rather than ignoring repeats", got)
	}
}
