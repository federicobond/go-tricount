package tricount

import (
	"fmt"
	"math/big"
)

// Split says how a transaction's total is divided between members. Build one
// with SplitEqually, SplitByShares or SplitExactly; the zero Split is
// invalid.
//
// The Split constructors name members, while Amount's Divide methods do the
// arithmetic. The names differ on purpose so the two layers cannot be
// confused.
type Split struct {
	kind    splitKind
	members []*Member
	shares  []int
	amounts []Amount
}

type splitKind int

const (
	splitUnset splitKind = iota
	splitEqual
	splitShares
	splitExact
)

// MemberShare is one member's weight in a ratio split.
type MemberShare struct {
	Member *Member
	Share  int
}

// MemberAmount is one member's exact share of a total.
type MemberAmount struct {
	Member *Member
	Amount Amount
}

// SplitEqually divides the total evenly. Leftover minor units go to the
// earliest members, so EUR 10.00 across three becomes 3.34, 3.33, 3.33.
func SplitEqually(members ...*Member) Split {
	return Split{kind: splitEqual, members: members}
}

// SplitByShares divides the total in proportion to weights, so shares of
// 1, 2, 1 mean the middle member pays twice as much as each of the others. It
// takes a slice rather than a map because remainder assignment depends on
// order, and Go randomizes map iteration.
func SplitByShares(shares []MemberShare) Split {
	s := Split{kind: splitShares}
	for _, ms := range shares {
		s.members = append(s.members, ms.Member)
		s.shares = append(s.shares, ms.Share)
	}
	return s
}

// SplitExactly assigns each member a fixed amount. The amounts must sum
// exactly to the transaction total, and the error says so plainly when they do
// not. Use it when per-person figures are known, as when itemizing a
// restaurant bill.
func SplitExactly(amounts []MemberAmount) Split {
	s := Split{kind: splitExact}
	for _, ma := range amounts {
		s.members = append(s.members, ma.Member)
		s.amounts = append(s.amounts, ma.Amount)
	}
	return s
}

func (s Split) memberList() []*Member { return s.members }

// resolve turns the split into allocations of total, validating the members
// against t as it goes. Returned amounts are positive; the wire layer applies
// the API's sign convention.
func (s Split) resolve(t *Tricount, total Amount) ([]Allocation, error) {
	if s.kind == splitUnset {
		return nil, fmt.Errorf("%w: no split given; use SplitEqually, SplitByShares or SplitExactly",
			ErrInvalidRequest)
	}
	if len(s.members) == 0 {
		return nil, fmt.Errorf("%w: split covers no members", ErrInvalidRequest)
	}
	seen := make(map[string]bool, len(s.members))
	for i, m := range s.members {
		if m == nil {
			return nil, fmt.Errorf("%w: split member %d is nil", ErrInvalidRequest, i)
		}
		if m.UUID == "" {
			return nil, fmt.Errorf("%w: split member %q has no uuid", ErrInvalidRequest, m.DisplayName)
		}
		if !t.IsActiveMember(m.UUID) {
			if t.MemberByUUID(m.UUID) != nil {
				return nil, fmt.Errorf("%w: split member %q has been removed from tricount %d",
					ErrInvalidRequest, m.DisplayName, t.ID)
			}
			return nil, fmt.Errorf("%w: split member %q is not in tricount %d",
				ErrInvalidRequest, m.DisplayName, t.ID)
		}
		if seen[m.UUID] {
			return nil, fmt.Errorf("%w: split names member %q twice",
				ErrInvalidRequest, m.DisplayName)
		}
		seen[m.UUID] = true
	}

	var (
		parts    []Amount
		allocTyp = AllocationAmount
		err      error
	)
	switch s.kind {
	case splitEqual:
		parts, err = total.DivideEqually(len(s.members))
	case splitShares:
		allocTyp = AllocationRatio
		parts, err = total.DivideByShares(s.shares)
	case splitExact:
		parts, err = s.exactParts(total)
	}
	if err != nil {
		return nil, err
	}

	allocs := make([]Allocation, len(s.members))
	for i, m := range s.members {
		allocs[i] = Allocation{
			MemberUUID: m.UUID,
			Amount:     parts[i],
			Type:       allocTyp,
		}
		if s.kind == splitShares {
			allocs[i].ShareRatio = s.shares[i]
		}
	}
	return allocs, nil
}

func (s Split) exactParts(total Amount) ([]Amount, error) {
	if len(s.amounts) != len(s.members) {
		return nil, fmt.Errorf("%w: %d exact amounts for %d members",
			ErrInvalidRequest, len(s.amounts), len(s.members))
	}
	sum := ZeroAmount(total.Currency())
	for i, a := range s.amounts {
		if a.Currency() != total.Currency() {
			return nil, fmt.Errorf("%w: exact amount for %q is in %s, but the total is in %s",
				ErrInvalidRequest, s.members[i].DisplayName, a.Currency(), total.Currency())
		}
		if a.Sign() < 0 {
			return nil, fmt.Errorf("%w: exact amount for %q is negative (%s); amounts are always positive",
				ErrInvalidRequest, s.members[i].DisplayName, a)
		}
		var err error
		if sum, err = sum.Add(a); err != nil {
			return nil, err
		}
	}
	if !sum.Equal(total) {
		return nil, fmt.Errorf("%w: exact amounts sum to %s, but the total is %s",
			ErrInvalidRequest, sum, total)
	}
	parts := make([]Amount, len(s.amounts))
	copy(parts, s.amounts)
	return parts, nil
}

// resolveLocal mirrors an already-resolved split into a second currency,
// producing per-member amounts that sum exactly to localTotal. Converting each
// allocation independently would accumulate rounding error and miss the total,
// so the proportions are re-applied instead.
func (s Split) resolveLocal(allocations []Allocation, localTotal Amount) ([]Amount, error) {
	switch s.kind {
	case splitEqual:
		return localTotal.DivideEqually(len(allocations))
	case splitShares:
		return localTotal.DivideByShares(s.shares)
	case splitExact:
		return weightedLocal(localTotal, allocations)
	}
	return nil, fmt.Errorf("%w: cannot mirror an unset split into %s",
		ErrInvalidRequest, localTotal.Currency())
}

// weightedLocal mirrors allocations into a second currency, in proportion to
// their amounts.
func weightedLocal(local Amount, allocations []Allocation) ([]Amount, error) {
	weights := make([]*big.Int, len(allocations))
	for i, a := range allocations {
		units, err := a.Amount.units()
		if err != nil {
			return nil, err
		}
		weights[i] = new(big.Int).Abs(units)
	}
	return local.divideByWeights(weights)
}
