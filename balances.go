package tricount

import (
	"fmt"
	"sort"
)

// Transfer is one payment in a settlement plan: From should pay Amount to To.
type Transfer struct {
	From   *Member `json:"from"`
	To     *Member `json:"to"`
	Amount Amount  `json:"amount"`
}

// Balances computes each member's net position from the tricount's
// transactions, keyed by display name. A positive balance means the member is
// owed money; a negative one means they owe it.
//
// The model, in one line per kind:
//
//   - Expense: the payer laid out the total, so they are credited it, and each
//     allocation debits its member. Alice paying EUR 30.00 split three ways
//     leaves Alice at +20.00 and the others at -10.00 each.
//   - Reimbursement: the same shape. The payer is credited, the recipient
//     debited, which is exactly what cancels an existing debt.
//   - Income: the mirror image. A member who receives money on the group's
//     behalf is holding funds that belong to the others, so the receiver is
//     debited the total and each allocation credits its member. Bob taking a
//     EUR 60.00 refund split three ways leaves Bob at -40.00.
//
// Note that the Python tricount-api treats income with the same sign as an
// expense, which makes a receiver appear to be owed money rather than to owe
// it. Its own guide warns that its balances do not always reconcile with a
// manual sum. If you are comparing figures against the Tricount app, income is
// the entry worth checking first.
//
// A transaction may name a member the tricount payload does not carry, because
// removing a member keeps their transactions while GET /registry lists only
// active memberships. Such a member is included under their membership UUID
// rather than a display name, so the figures still add up. Everything else is
// checked: allocations must sum to their transaction's total, and the balances
// themselves must sum to zero, or Balances returns an error rather than a
// plausible wrong number.
func (t *Tricount) Balances() (map[string]Amount, error) {
	byUUID, members, err := balancesByUUID(t)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Amount, len(byUUID))
	for uuid, balance := range byUUID {
		out[balanceKey(members[uuid], uuid)] = balance
	}
	return out, nil
}

// balanceKey is a member's display name, falling back to the membership UUID
// for a member the payload does not describe.
func balanceKey(m *Member, uuid string) string {
	if m != nil && m.DisplayName != "" {
		return m.DisplayName
	}
	return uuid
}

// balancesByUUID is the real computation. It returns balances keyed by
// membership UUID alongside the members it resolved, which is what Settle
// needs and what makes unnamed members representable.
func balancesByUUID(t *Tricount) (map[string]Amount, map[string]*Member, error) {
	if t == nil {
		return nil, nil, fmt.Errorf("%w: tricount is nil", ErrInvalidRequest)
	}
	currency := t.Currency
	if currency == "" {
		return nil, nil, fmt.Errorf("%w: tricount %d has no currency", ErrInvalidRequest, t.ID)
	}

	balances := make(map[string]Amount, len(t.Members))
	members := make(map[string]*Member, len(t.Members))
	// include registers a UUID so it appears in the result even at zero.
	include := func(uuid string) {
		if _, ok := balances[uuid]; !ok {
			balances[uuid] = ZeroAmount(currency)
			members[uuid] = t.MemberByUUID(uuid)
		}
	}
	for _, m := range t.Members {
		include(m.UUID)
	}

	for _, tx := range t.Transactions {
		if tx.Status != TransactionActive {
			continue
		}
		include(tx.PayerUUID)

		// Allocations must reconstruct the total, or the arithmetic below is
		// meaningless.
		sum := ZeroAmount(currency)
		for _, a := range tx.Allocations {
			include(a.MemberUUID)
			var err error
			if sum, err = sum.Add(a.Amount); err != nil {
				return nil, nil, fmt.Errorf("transaction %d: %w", tx.ID, err)
			}
		}
		if !sum.Equal(tx.Amount) {
			return nil, nil, fmt.Errorf("transaction %d is inconsistent: allocations sum to %s but the total is %s",
				tx.ID, sum, tx.Amount)
		}

		// Income is the mirror image of an expense: the receiver holds money
		// that belongs to the allocated members.
		invert := tx.Kind == KindIncome
		if invert {
			balances[tx.PayerUUID], _ = balances[tx.PayerUUID].Sub(tx.Amount)
		} else {
			balances[tx.PayerUUID], _ = balances[tx.PayerUUID].Add(tx.Amount)
		}
		for _, a := range tx.Allocations {
			if invert {
				balances[a.MemberUUID], _ = balances[a.MemberUUID].Add(a.Amount)
			} else {
				balances[a.MemberUUID], _ = balances[a.MemberUUID].Sub(a.Amount)
			}
		}
	}

	total := ZeroAmount(currency)
	for _, b := range balances {
		total, _ = total.Add(b)
	}
	if !total.IsZero() {
		return nil, nil, fmt.Errorf("tricount %d: balances sum to %s rather than zero, which means the data is inconsistent",
			t.ID, total)
	}
	return balances, members, nil
}

// Settle returns a plan of transfers that clears every balance, matching the
// largest creditor against the largest debtor until nothing is left. The
// result is the minimum number of transfers for the common case and is never
// wrong about totals. Ties break by membership UUID, so the plan is
// deterministic.
//
// A member the payload does not describe, because they were removed after
// their transactions were recorded, appears as a Member carrying only a UUID.
func (t *Tricount) Settle() ([]Transfer, error) {
	balances, members, err := balancesByUUID(t)
	if err != nil {
		return nil, err
	}

	type position struct {
		member  *Member
		balance Amount
	}
	var creditors, debtors []position

	// Iterate UUIDs in sorted order so the plan does not depend on map
	// iteration order.
	uuids := make([]string, 0, len(balances))
	for uuid := range balances {
		uuids = append(uuids, uuid)
	}
	sort.Strings(uuids)

	for _, uuid := range uuids {
		m := members[uuid]
		if m == nil {
			m = &Member{UUID: uuid, DisplayName: uuid}
		}
		switch b := balances[uuid]; {
		case b.Sign() > 0:
			creditors = append(creditors, position{m, b})
		case b.Sign() < 0:
			debtors = append(debtors, position{m, b.Neg()})
		}
	}

	byAmountThenUUID := func(s []position) func(i, j int) bool {
		return func(i, j int) bool {
			c, err := s[i].balance.Cmp(s[j].balance)
			if err == nil && c != 0 {
				return c > 0 // largest first
			}
			return s[i].member.UUID < s[j].member.UUID
		}
	}
	sort.SliceStable(creditors, byAmountThenUUID(creditors))
	sort.SliceStable(debtors, byAmountThenUUID(debtors))

	var transfers []Transfer
	i, j := 0, 0
	for i < len(creditors) && j < len(debtors) {
		cr, db := &creditors[i], &debtors[j]
		amount := cr.balance
		if c, err := db.balance.Cmp(amount); err != nil {
			return nil, err
		} else if c < 0 {
			amount = db.balance
		}
		if amount.Sign() > 0 {
			transfers = append(transfers, Transfer{From: db.member, To: cr.member, Amount: amount})
		}
		if cr.balance, err = cr.balance.Sub(amount); err != nil {
			return nil, err
		}
		if db.balance, err = db.balance.Sub(amount); err != nil {
			return nil, err
		}
		if cr.balance.IsZero() {
			i++
		}
		if db.balance.IsZero() {
			j++
		}
	}
	return transfers, nil
}
