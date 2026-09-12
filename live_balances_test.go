//go:build live

package tricount

import (
	"context"
	"testing"
)

// TestLiveBalancesReconcile builds a known set of transactions in the fixture,
// then checks the computed balances against figures worked out by hand. It also
// prints the fixture link and the balances so they can be compared against the
// Tricount app by eye — the one check no automated test can make.
func TestLiveBalancesReconcile(t *testing.T) {
	c := liveClient(t)
	cleanupThisRun(t)
	tri := liveTricount(t)
	ctx := context.Background()
	alice, bob := liveTwoMembers(t, c, tri)

	// Only this run's members take part, so pre-existing fixture members stay
	// at zero and the expected figures are exact.
	if _, err := c.CreateExpense(ctx, tri, Expense{
		Description: tagged("Groceries"),
		Amount:      MustParseAmount("40.00", "EUR"),
		Payer:       alice,
		Split:       SplitEqually(alice, bob),
	}); err != nil {
		t.Fatalf("CreateExpense: %v", err)
	}
	if _, err := c.CreateReimbursement(ctx, tri, Reimbursement{
		Description: tagged("Part payment"),
		Amount:      MustParseAmount("5.00", "EUR"),
		From:        bob,
		To:          alice,
	}); err != nil {
		t.Fatalf("CreateReimbursement: %v", err)
	}

	reread, err := c.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}
	balances, err := reread.Balances()
	if err != nil {
		t.Fatalf("Balances on real data: %v", err)
	}

	t.Logf("fixture https://tricount.com/%s — compare these against the app:", reread.PublicToken)
	for name, b := range balances {
		t.Logf("  %-40s %s", name, b)
	}

	// Alice laid out 40.00 and consumed 20.00, then received 5.00 back:
	// +20.00 - 5.00 = +15.00. Bob is the mirror image.
	if got := balances[alice.DisplayName].String(); got != "15.00" {
		t.Errorf("%s = %s, want 15.00", alice.DisplayName, got)
	}
	if got := balances[bob.DisplayName].String(); got != "-15.00" {
		t.Errorf("%s = %s, want -15.00", bob.DisplayName, got)
	}

	transfers, err := reread.Settle()
	if err != nil {
		t.Fatalf("Settle on real data: %v", err)
	}
	var found bool
	for _, tr := range transfers {
		if tr.From.UUID == bob.UUID && tr.To.UUID == alice.UUID {
			found = true
			if got := tr.Amount.String(); got != "15.00" {
				t.Errorf("settle transfer = %s, want 15.00", got)
			}
		}
	}
	if !found {
		t.Errorf("no Bob-to-Alice transfer in the plan: %+v", transfers)
	}
}

// TestLiveBalancesWithIncome records an income entry and reports the balances
// it produces, so the income sign convention can be checked against the app.
// The convention is documented on Balances; this test is how you verify it.
func TestLiveBalancesWithIncome(t *testing.T) {
	c := liveClient(t)
	cleanupThisRun(t)
	tri := liveTricount(t)
	ctx := context.Background()
	alice, bob := liveTwoMembers(t, c, tri)

	if _, err := c.CreateIncome(ctx, tri, Income{
		Description: tagged("Deposit refund"),
		Amount:      MustParseAmount("60.00", "EUR"),
		Receiver:    bob,
		Split:       SplitEqually(alice, bob),
	}); err != nil {
		t.Fatalf("CreateIncome: %v", err)
	}

	reread, err := c.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}
	balances, err := reread.Balances()
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}

	t.Logf("income of 60.00 received by %s, split with %s:", bob.DisplayName, alice.DisplayName)
	t.Logf("  %-40s %s", alice.DisplayName, balances[alice.DisplayName])
	t.Logf("  %-40s %s", bob.DisplayName, balances[bob.DisplayName])
	t.Logf("open https://tricount.com/%s to compare against the app", reread.PublicToken)

	// Bob holds 30.00 that belongs to Alice.
	if got := balances[bob.DisplayName].String(); got != "-30.00" {
		t.Errorf("%s = %s, want -30.00 (the receiver owes the group)", bob.DisplayName, got)
	}
	if got := balances[alice.DisplayName].String(); got != "30.00" {
		t.Errorf("%s = %s, want 30.00", alice.DisplayName, got)
	}
}
