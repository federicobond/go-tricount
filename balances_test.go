package tricount

import "testing"

func balanceTestTricount() *Tricount {
	return &Tricount{
		ID:       1,
		Currency: "EUR",
		Members: []*Member{
			{ID: 1, UUID: "u-a", DisplayName: "Alice"},
			{ID: 2, UUID: "u-b", DisplayName: "Bob"},
			{ID: 3, UUID: "u-c", DisplayName: "Carol"},
		},
	}
}

func expenseTx(id int64, payer string, total string, shares map[string]string) *Transaction {
	tx := &Transaction{
		ID: id, Description: "e", Kind: KindExpense, Status: TransactionActive,
		Amount: MustParseAmount(total, "EUR"), PayerUUID: payer,
	}
	for _, uuid := range []string{"u-a", "u-b", "u-c"} {
		if s, ok := shares[uuid]; ok {
			tx.Allocations = append(tx.Allocations, Allocation{
				MemberUUID: uuid, Amount: MustParseAmount(s, "EUR"), Type: AllocationAmount,
			})
		}
	}
	return tx
}

func assertBalances(t *testing.T, got map[string]Amount, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d balances %v, want %d", len(got), got, len(want))
	}
	for name, wantStr := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("no balance for %q", name)
			continue
		}
		if g.String() != wantStr {
			t.Errorf("%s = %s, want %s", name, g, wantStr)
		}
	}
}

func TestBalancesSingleExpense(t *testing.T) {
	tri := balanceTestTricount()
	tri.Transactions = []*Transaction{
		expenseTx(1, "u-a", "30.00", map[string]string{"u-a": "10.00", "u-b": "10.00", "u-c": "10.00"}),
	}

	got, err := tri.Balances()
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	assertBalances(t, got, map[string]string{
		"Alice": "20.00", "Bob": "-10.00", "Carol": "-10.00",
	})
}

func TestBalancesIncomeOwesTheGroup(t *testing.T) {
	tri := balanceTestTricount()
	// Bob receives a 60.00 refund that belongs to all three, so he is holding
	// 40.00 of other people's money.
	tri.Transactions = []*Transaction{
		{
			ID: 1, Description: "refund", Kind: KindIncome, Status: TransactionActive,
			Amount: MustParseAmount("60.00", "EUR"), PayerUUID: "u-b",
			Allocations: []Allocation{
				{MemberUUID: "u-a", Amount: MustParseAmount("20.00", "EUR"), Type: AllocationAmount},
				{MemberUUID: "u-b", Amount: MustParseAmount("20.00", "EUR"), Type: AllocationAmount},
				{MemberUUID: "u-c", Amount: MustParseAmount("20.00", "EUR"), Type: AllocationAmount},
			},
		},
	}

	got, err := tri.Balances()
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	assertBalances(t, got, map[string]string{
		"Alice": "20.00", "Bob": "-40.00", "Carol": "20.00",
	})
}

func TestBalancesReimbursementSettles(t *testing.T) {
	tri := balanceTestTricount()
	tri.Transactions = []*Transaction{
		// Alice pays 30.00 for everyone: Alice +20, Bob -10, Carol -10.
		expenseTx(1, "u-a", "30.00", map[string]string{"u-a": "10.00", "u-b": "10.00", "u-c": "10.00"}),
		// Bob pays Alice his 10.00 back.
		{
			ID: 2, Description: "settle", Kind: KindReimbursement, Status: TransactionActive,
			Amount: MustParseAmount("10.00", "EUR"), PayerUUID: "u-b",
			Allocations: []Allocation{
				{MemberUUID: "u-a", Amount: MustParseAmount("10.00", "EUR"), Type: AllocationAmount},
				{MemberUUID: "u-b", Amount: ZeroAmount("EUR"), Type: AllocationAmount},
			},
		},
	}

	got, err := tri.Balances()
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	assertBalances(t, got, map[string]string{
		"Alice": "10.00", "Bob": "0.00", "Carol": "-10.00",
	})
}

func TestBalancesSkipsInactiveTransactions(t *testing.T) {
	tri := balanceTestTricount()
	inactive := expenseTx(1, "u-a", "30.00",
		map[string]string{"u-a": "10.00", "u-b": "10.00", "u-c": "10.00"})
	inactive.Status = TransactionStatus("DELETED")
	tri.Transactions = []*Transaction{inactive}

	got, err := tri.Balances()
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	assertBalances(t, got, map[string]string{
		"Alice": "0.00", "Bob": "0.00", "Carol": "0.00",
	})
}

func TestBalancesUnevenSplitStillSumsToZero(t *testing.T) {
	tri := balanceTestTricount()
	tri.Transactions = []*Transaction{
		expenseTx(1, "u-a", "10.00", map[string]string{"u-a": "3.34", "u-b": "3.33", "u-c": "3.33"}),
	}
	got, err := tri.Balances()
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	assertBalances(t, got, map[string]string{
		"Alice": "6.66", "Bob": "-3.33", "Carol": "-3.33",
	})
	sum := ZeroAmount("EUR")
	for _, b := range got {
		sum, _ = sum.Add(b)
	}
	if !sum.IsZero() {
		t.Errorf("balances sum to %s, want zero", sum)
	}
}

func TestBalancesRejectsInconsistentData(t *testing.T) {
	tri := balanceTestTricount()
	// The allocations do not add up to the total, which the API should never
	// produce. Returning a wrong number silently is worse than erroring.
	tri.Transactions = []*Transaction{
		expenseTx(1, "u-a", "30.00", map[string]string{"u-a": "10.00", "u-b": "10.00"}),
	}
	if _, err := tri.Balances(); err == nil {
		t.Fatal("inconsistent allocations should be reported, not absorbed")
	}
}

func TestBalancesIncludesRemovedMembersByUUID(t *testing.T) {
	// Removing a member keeps their transactions but drops them from the
	// registry payload, so the payer here is not in Members. The figures must
	// still add up rather than the call failing.
	tri := balanceTestTricount()
	tri.Transactions = []*Transaction{
		expenseTx(1, "u-ghost", "30.00", map[string]string{"u-a": "30.00"}),
	}

	got, err := tri.Balances()
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	if b, ok := got["u-ghost"]; !ok {
		t.Errorf("the removed payer is missing; got %v", got)
	} else if b.String() != "30.00" {
		t.Errorf("removed payer = %s, want 30.00", b)
	}
	if got["Alice"].String() != "-30.00" {
		t.Errorf("Alice = %s, want -30.00", got["Alice"])
	}

	sum := ZeroAmount("EUR")
	for _, b := range got {
		sum, _ = sum.Add(b)
	}
	if !sum.IsZero() {
		t.Errorf("balances sum to %s, want zero", sum)
	}

	// Settle must still produce a usable plan naming the removed member.
	transfers, err := tri.Settle()
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if len(transfers) != 1 {
		t.Fatalf("got %d transfers, want 1", len(transfers))
	}
	if transfers[0].To.UUID != "u-ghost" || transfers[0].From.DisplayName != "Alice" {
		t.Errorf("transfer = %+v, want Alice paying u-ghost", transfers[0])
	}
}

func TestBalancesResolvesFormerMembers(t *testing.T) {
	// When the membership list has been refreshed, a removed member is in
	// FormerMembers and resolves to their real name.
	tri := balanceTestTricount()
	tri.FormerMembers = []*Member{
		{ID: 9, UUID: "u-gone", DisplayName: "Dave", Status: MemberStatus("DELETED")},
	}
	tri.Transactions = []*Transaction{
		expenseTx(1, "u-gone", "30.00", map[string]string{"u-a": "30.00"}),
	}
	got, err := tri.Balances()
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	if b, ok := got["Dave"]; !ok || b.String() != "30.00" {
		t.Errorf("Dave = %v (present %v), want 30.00", b, ok)
	}
}

func TestBalancesEmptyTricount(t *testing.T) {
	tri := balanceTestTricount()
	got, err := tri.Balances()
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	assertBalances(t, got, map[string]string{
		"Alice": "0.00", "Bob": "0.00", "Carol": "0.00",
	})
}

func TestSettleProducesMinimalTransfers(t *testing.T) {
	tri := balanceTestTricount()
	tri.Transactions = []*Transaction{
		expenseTx(1, "u-a", "30.00", map[string]string{"u-a": "10.00", "u-b": "10.00", "u-c": "10.00"}),
	}

	transfers, err := tri.Settle()
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if len(transfers) != 2 {
		t.Fatalf("got %d transfers %v, want 2", len(transfers), transfers)
	}
	for _, tr := range transfers {
		if tr.To.DisplayName != "Alice" {
			t.Errorf("transfer to %q, want Alice", tr.To.DisplayName)
		}
		if tr.Amount.String() != "10.00" {
			t.Errorf("transfer amount = %s, want 10.00", tr.Amount)
		}
		if tr.Amount.Sign() <= 0 {
			t.Error("transfers must be positive")
		}
	}
}

func TestSettleIsDeterministic(t *testing.T) {
	tri := balanceTestTricount()
	tri.Transactions = []*Transaction{
		expenseTx(1, "u-a", "30.00", map[string]string{"u-a": "10.00", "u-b": "10.00", "u-c": "10.00"}),
		expenseTx(2, "u-b", "9.00", map[string]string{"u-a": "3.00", "u-b": "3.00", "u-c": "3.00"}),
	}
	first, err := tri.Settle()
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	for i := 0; i < 20; i++ {
		again, err := tri.Settle()
		if err != nil {
			t.Fatalf("Settle: %v", err)
		}
		if len(again) != len(first) {
			t.Fatalf("transfer count varies between runs: %d then %d", len(first), len(again))
		}
		for j := range first {
			if again[j].From.UUID != first[j].From.UUID ||
				again[j].To.UUID != first[j].To.UUID ||
				!again[j].Amount.Equal(first[j].Amount) {
				t.Fatalf("transfer %d varies between runs", j)
			}
		}
	}
}

func TestSettleClearsEveryBalance(t *testing.T) {
	tri := balanceTestTricount()
	tri.Transactions = []*Transaction{
		expenseTx(1, "u-a", "10.00", map[string]string{"u-a": "3.34", "u-b": "3.33", "u-c": "3.33"}),
		expenseTx(2, "u-c", "7.00", map[string]string{"u-a": "2.34", "u-b": "2.33", "u-c": "2.33"}),
	}
	balances, err := tri.Balances()
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	transfers, err := tri.Settle()
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}

	// Applying every transfer must zero every balance exactly.
	after := map[string]Amount{}
	for name, b := range balances {
		after[name] = b
	}
	for _, tr := range transfers {
		after[tr.From.DisplayName], _ = after[tr.From.DisplayName].Add(tr.Amount)
		after[tr.To.DisplayName], _ = after[tr.To.DisplayName].Sub(tr.Amount)
	}
	for name, b := range after {
		if !b.IsZero() {
			t.Errorf("%s is left at %s after settling, want zero", name, b)
		}
	}
}

func TestSettleNothingToDo(t *testing.T) {
	tri := balanceTestTricount()
	transfers, err := tri.Settle()
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if len(transfers) != 0 {
		t.Errorf("got %d transfers for a settled tricount, want 0", len(transfers))
	}
}
