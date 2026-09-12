//go:build live

package tricount

import (
	"context"
	"testing"
)

// liveTwoMembers adds two tagged members to the fixture and returns them.
func liveTwoMembers(t *testing.T, c *Client, tri *Tricount) (*Member, *Member) {
	t.Helper()
	ctx := context.Background()
	a, b := tagged("PayerA"), tagged("PayerB")
	if err := c.AddMembers(ctx, tri, a, b); err != nil {
		t.Fatalf("AddMembers: %v", err)
	}
	ma, mb := tri.MemberByName(a), tri.MemberByName(b)
	if ma == nil || mb == nil {
		t.Fatalf("added members are missing; have %v", memberNames(tri))
	}
	return ma, mb
}

func TestLiveCreateExpenseEqualSplit(t *testing.T) {
	c := liveClient(t)
	cleanupThisRun(t)
	tri := liveTricount(t)
	ctx := context.Background()
	alice, bob := liveTwoMembers(t, c, tri)

	id, err := c.CreateExpense(ctx, tri, Expense{
		Description: tagged("Dinner"),
		Amount:      MustParseAmount("25.51", "EUR"),
		Payer:       alice,
		Split:       SplitEqually(alice, bob),
		Category:    CategoryFoodAndDrink,
	})
	if err != nil {
		t.Fatalf("CreateExpense: %v", err)
	}

	reread, err := c.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}
	tx := reread.TransactionByID(id)
	if tx == nil {
		t.Fatalf("transaction %d is missing after creation", id)
	}
	if tx.Kind != KindExpense {
		t.Errorf("Kind = %q, want expense", tx.Kind)
	}
	// The round trip must preserve the positive-amount contract.
	if got := tx.Amount.String(); got != "25.51" {
		t.Errorf("Amount = %q, want \"25.51\"", got)
	}
	if tx.PayerUUID != alice.UUID {
		t.Errorf("PayerUUID = %q, want Alice", tx.PayerUUID)
	}
	if tx.Category != CategoryFoodAndDrink {
		t.Errorf("Category = %q", tx.Category)
	}

	// The odd cent must land somewhere, and the parts must still add up.
	sum := ZeroAmount("EUR")
	for _, a := range tx.Allocations {
		sum, err = sum.Add(a.Amount)
		if err != nil {
			t.Fatalf("summing allocations: %v", err)
		}
	}
	if !sum.Equal(tx.Amount) {
		t.Errorf("allocations sum to %s, want %s", sum, tx.Amount)
	}
	t.Logf("allocations of 25.51 came back as %s", allocationStrings(tx.Allocations))
}

func TestLiveCreateExpenseRatioAndExactSplits(t *testing.T) {
	c := liveClient(t)
	cleanupThisRun(t)
	tri := liveTricount(t)
	ctx := context.Background()
	alice, bob := liveTwoMembers(t, c, tri)

	ratioID, err := c.CreateExpense(ctx, tri, Expense{
		Description: tagged("Hotel ratio"),
		Amount:      MustParseAmount("300.00", "EUR"),
		Payer:       alice,
		Split:       SplitByShares([]MemberShare{{alice, 2}, {bob, 1}}),
	})
	if err != nil {
		t.Fatalf("ratio CreateExpense: %v", err)
	}

	exactID, err := c.CreateExpense(ctx, tri, Expense{
		Description: tagged("Bill exact"),
		Amount:      MustParseAmount("41.30", "EUR"),
		Payer:       bob,
		Split: SplitExactly([]MemberAmount{
			{alice, MustParseAmount("17.80", "EUR")},
			{bob, MustParseAmount("23.50", "EUR")},
		}),
	})
	if err != nil {
		t.Fatalf("exact CreateExpense: %v", err)
	}

	reread, err := c.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}

	ratio := reread.TransactionByID(ratioID)
	if ratio == nil {
		t.Fatalf("ratio transaction %d missing", ratioID)
	}
	byMember := map[string]string{}
	for _, a := range ratio.Allocations {
		byMember[a.MemberUUID] = a.Amount.String()
	}
	if byMember[alice.UUID] != "200.00" || byMember[bob.UUID] != "100.00" {
		t.Errorf("ratio allocations = %v, want 200.00 and 100.00", byMember)
	}

	exact := reread.TransactionByID(exactID)
	if exact == nil {
		t.Fatalf("exact transaction %d missing", exactID)
	}
	byMember = map[string]string{}
	for _, a := range exact.Allocations {
		byMember[a.MemberUUID] = a.Amount.String()
	}
	if byMember[alice.UUID] != "17.80" || byMember[bob.UUID] != "23.50" {
		t.Errorf("exact allocations = %v, want 17.80 and 23.50", byMember)
	}
}

func TestLiveCreateIncomeAndReimbursement(t *testing.T) {
	c := liveClient(t)
	cleanupThisRun(t)
	tri := liveTricount(t)
	ctx := context.Background()
	alice, bob := liveTwoMembers(t, c, tri)

	incomeID, err := c.CreateIncome(ctx, tri, Income{
		Description: tagged("Deposit returned"),
		Amount:      MustParseAmount("60.00", "EUR"),
		Receiver:    bob,
		Split:       SplitEqually(alice, bob),
	})
	if err != nil {
		t.Fatalf("CreateIncome: %v", err)
	}

	reimbID, err := c.CreateReimbursement(ctx, tri, Reimbursement{
		Description: tagged("Settling up"),
		Amount:      MustParseAmount("15.00", "EUR"),
		From:        alice,
		To:          bob,
	})
	if err != nil {
		t.Fatalf("CreateReimbursement: %v", err)
	}

	reread, err := c.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}

	income := reread.TransactionByID(incomeID)
	if income == nil {
		t.Fatalf("income %d missing", incomeID)
	}
	if income.Kind != KindIncome {
		t.Errorf("income Kind = %q", income.Kind)
	}
	if got := income.Amount.String(); got != "60.00" {
		t.Errorf("income Amount = %q", got)
	}

	reimb := reread.TransactionByID(reimbID)
	if reimb == nil {
		t.Fatalf("reimbursement %d missing", reimbID)
	}
	if reimb.Kind != KindReimbursement {
		t.Errorf("reimbursement Kind = %q, want BALANCE", reimb.Kind)
	}
	if reimb.PayerUUID != alice.UUID {
		t.Errorf("reimbursement payer = %q, want Alice", reimb.PayerUUID)
	}
}

func TestLiveCreateForeignCurrencyExpense(t *testing.T) {
	c := liveClient(t)
	cleanupThisRun(t)
	tri := liveTricount(t) // EUR
	ctx := context.Background()
	alice, bob := liveTwoMembers(t, c, tri)

	local := MustParseAmount("15000", "JPY")
	rate, err := c.ExchangeRate(ctx, "JPY", tri.Currency)
	if err != nil {
		t.Fatalf("ExchangeRate: %v", err)
	}
	converted, err := local.Convert(rate, tri.Currency)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	id, err := c.CreateExpense(ctx, tri, Expense{
		Description: tagged("Tokyo hotel"),
		Amount:      converted,
		Payer:       alice,
		Split:       SplitEqually(alice, bob),
		LocalAmount: &local,
	})
	if err != nil {
		t.Fatalf("CreateExpense: %v", err)
	}

	reread, err := c.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}
	tx := reread.TransactionByID(id)
	if tx == nil {
		t.Fatalf("transaction %d missing", id)
	}
	if tx.LocalAmount == nil {
		t.Fatal("LocalAmount did not survive the round trip")
	}
	if tx.LocalAmount.Currency() != "JPY" {
		t.Errorf("LocalAmount currency = %q, want JPY", tx.LocalAmount.Currency())
	}
	if tx.ExchangeRate == "" {
		t.Error("ExchangeRate did not survive the round trip")
	}
	t.Logf("JPY 15000 at %s became %s; round-tripped local=%s rate=%s",
		rate, converted, tx.LocalAmount, tx.ExchangeRate)
}

func TestLiveUpdateAndDeleteTransaction(t *testing.T) {
	c := liveClient(t)
	cleanupThisRun(t)
	tri := liveTricount(t)
	ctx := context.Background()
	alice, bob := liveTwoMembers(t, c, tri)

	id, err := c.CreateExpense(ctx, tri, Expense{
		Description: tagged("Before"),
		Amount:      MustParseAmount("10.00", "EUR"),
		Payer:       alice,
		Split:       SplitEqually(alice, bob),
	})
	if err != nil {
		t.Fatalf("CreateExpense: %v", err)
	}

	if err := c.UpdateExpense(ctx, tri, id, Expense{
		Description: tagged("After"),
		Amount:      MustParseAmount("42.00", "EUR"),
		Payer:       bob,
		Split:       SplitByShares([]MemberShare{{alice, 1}, {bob, 2}}),
		Category:    CategoryTransport,
	}); err != nil {
		t.Fatalf("UpdateExpense: %v", err)
	}

	reread, err := c.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}
	tx := reread.TransactionByID(id)
	if tx == nil {
		t.Fatalf("transaction %d vanished after the update", id)
	}
	if tx.Description != tagged("After") {
		t.Errorf("description = %q, want the updated one", tx.Description)
	}
	if got := tx.Amount.String(); got != "42.00" {
		t.Errorf("amount = %q, want \"42.00\"", got)
	}
	if tx.PayerUUID != bob.UUID {
		t.Errorf("payer = %q, want Bob after the update", tx.PayerUUID)
	}
	if tx.Category != CategoryTransport {
		t.Errorf("category = %q", tx.Category)
	}
	byMember := map[string]string{}
	for _, a := range tx.Allocations {
		byMember[a.MemberUUID] = a.Amount.String()
	}
	if byMember[alice.UUID] != "14.00" || byMember[bob.UUID] != "28.00" {
		t.Errorf("allocations = %v, want 14.00 and 28.00", byMember)
	}

	if err := c.DeleteTransaction(ctx, reread, id); err != nil {
		t.Fatalf("DeleteTransaction: %v", err)
	}
	final, err := c.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("re-reading after delete: %v", err)
	}
	if final.TransactionByID(id) != nil {
		t.Errorf("transaction %d is still present after deletion", id)
	}
}
