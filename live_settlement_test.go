//go:build live

package tricount

import (
	"context"
	"errors"
	"testing"
)

// TestLiveSettlementProbe finds out whether the settlement endpoints work at
// all for an anonymous device. It does not fail when they do not: the point is
// to learn and record the answer. Read its log output; the findings live in
// settlement.go's doc comments and the README.
func TestLiveSettlementProbe(t *testing.T) {
	c := liveClient(t)
	cleanupThisRun(t)
	tri := liveTricount(t)
	ctx := context.Background()
	alice, bob := liveTwoMembers(t, c, tri)

	// A settlement needs something to settle.
	if _, err := c.CreateExpense(ctx, tri, Expense{
		Description: tagged("Settlement probe"),
		Amount:      MustParseAmount("20.00", "EUR"),
		Payer:       alice,
		Split:       SplitEqually(alice, bob),
	}); err != nil {
		t.Fatalf("CreateExpense: %v", err)
	}

	reread, err := c.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}

	id, err := c.CreateSettlement(ctx, reread)
	if err != nil {
		var apiErr *Error
		if errors.As(err, &apiErr) {
			t.Logf("PROBE RESULT: CreateSettlement is unavailable — http %d: %s",
				apiErr.StatusCode, apiErr.Description)
		} else {
			t.Logf("PROBE RESULT: CreateSettlement failed: %v", err)
		}
		t.Log("Callers should use Settle, which computes the plan locally.")
		local, lerr := reread.Settle()
		if lerr != nil {
			t.Fatalf("local Settle: %v", lerr)
		}
		t.Logf("local Settle produced %d transfers", len(local))
		return
	}

	t.Logf("PROBE RESULT: CreateSettlement works and returned id %d", id)

	s, err := c.GetSettlement(ctx, reread, id)
	if err != nil {
		t.Logf("PROBE RESULT: CreateSettlement works but GetSettlement does not: %v", err)
		return
	}
	t.Logf("PROBE RESULT: GetSettlement works, %d items", len(s.Items))
	for _, item := range s.Items {
		t.Logf("  %s pays %s to %s (%s)", item.PayerUUID, item.Amount, item.ReceiverUUID, item.Status)
	}

	local, err := reread.Settle()
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	t.Logf("local Settle produced %d transfers for comparison", len(local))
}
