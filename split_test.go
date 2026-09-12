package tricount

import (
	"errors"
	"strings"
	"testing"
)

func splitTestTricount() *Tricount {
	return &Tricount{
		ID:       1,
		Currency: "EUR",
		Members: []*Member{
			{ID: 1, UUID: "u-alice", DisplayName: "Alice"},
			{ID: 2, UUID: "u-bob", DisplayName: "Bob"},
			{ID: 3, UUID: "u-carol", DisplayName: "Carol"},
		},
	}
}

func allocationStrings(allocs []Allocation) []string {
	out := make([]string, len(allocs))
	for i, a := range allocs {
		out[i] = a.MemberUUID + "=" + a.Amount.String()
	}
	return out
}

func assertAllocations(t *testing.T, got []Allocation, want []string) {
	t.Helper()
	gots := allocationStrings(got)
	if len(gots) != len(want) {
		t.Fatalf("got %d allocations %v, want %d %v", len(gots), gots, len(want), want)
	}
	for i := range gots {
		if gots[i] != want[i] {
			t.Errorf("allocation %d = %q, want %q (all: %v)", i, gots[i], want[i], gots)
		}
	}
}

func TestSplitEquallyResolves(t *testing.T) {
	tri := splitTestTricount()
	total := MustParseAmount("10.00", "EUR")

	allocs, err := SplitEqually(tri.Members...).resolve(tri, total)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	assertAllocations(t, allocs, []string{"u-alice=3.34", "u-bob=3.33", "u-carol=3.33"})
	for _, a := range allocs {
		if a.Type != AllocationAmount {
			t.Errorf("type = %q, want AMOUNT", a.Type)
		}
		if a.ShareRatio != 0 {
			t.Errorf("share ratio = %d, want 0 for an equal split", a.ShareRatio)
		}
	}
}

func TestSplitBySharesResolves(t *testing.T) {
	tri := splitTestTricount()
	total := MustParseAmount("100.00", "EUR")

	allocs, err := SplitByShares([]MemberShare{
		{tri.Members[0], 1},
		{tri.Members[1], 2},
		{tri.Members[2], 1},
	}).resolve(tri, total)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	assertAllocations(t, allocs, []string{"u-alice=25.00", "u-bob=50.00", "u-carol=25.00"})
	for i, a := range allocs {
		if a.Type != AllocationRatio {
			t.Errorf("allocation %d type = %q, want RATIO", i, a.Type)
		}
	}
	if allocs[1].ShareRatio != 2 {
		t.Errorf("Bob's share ratio = %d, want 2", allocs[1].ShareRatio)
	}
}

func TestSplitExactlyResolves(t *testing.T) {
	tri := splitTestTricount()
	total := MustParseAmount("100.00", "EUR")

	allocs, err := SplitExactly([]MemberAmount{
		{tri.Members[0], MustParseAmount("50.00", "EUR")},
		{tri.Members[1], MustParseAmount("30.00", "EUR")},
		{tri.Members[2], MustParseAmount("20.00", "EUR")},
	}).resolve(tri, total)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	assertAllocations(t, allocs, []string{"u-alice=50.00", "u-bob=30.00", "u-carol=20.00"})
	for _, a := range allocs {
		if a.Type != AllocationAmount {
			t.Errorf("type = %q, want AMOUNT", a.Type)
		}
	}
}

func TestSplitExactlyMustSumToTotal(t *testing.T) {
	tri := splitTestTricount()
	total := MustParseAmount("100.00", "EUR")

	_, err := SplitExactly([]MemberAmount{
		{tri.Members[0], MustParseAmount("50.00", "EUR")},
		{tri.Members[1], MustParseAmount("30.00", "EUR")},
	}).resolve(tri, total)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "80.00") || !strings.Contains(msg, "100.00") {
		t.Errorf("error %q should mention both the sum and the total", msg)
	}
}

func TestSplitRejectsBadInput(t *testing.T) {
	tri := splitTestTricount()
	total := MustParseAmount("10.00", "EUR")
	stranger := &Member{ID: 9, UUID: "u-stranger", DisplayName: "Stranger"}

	cases := []struct {
		name  string
		split Split
	}{
		{"no members", SplitEqually()},
		{"nil member", SplitEqually(tri.Members[0], nil)},
		{"member not in tricount", SplitEqually(tri.Members[0], stranger)},
		{"duplicate member", SplitEqually(tri.Members[0], tri.Members[0])},
		{"no shares", SplitByShares(nil)},
		{"zero share", SplitByShares([]MemberShare{{tri.Members[0], 0}})},
		{"negative share", SplitByShares([]MemberShare{{tri.Members[0], -1}})},
		{"no exact amounts", SplitExactly(nil)},
		{"exact wrong currency", SplitExactly([]MemberAmount{
			{tri.Members[0], MustParseAmount("1000", "JPY")},
		})},
		{"negative exact amount", SplitExactly([]MemberAmount{
			{tri.Members[0], MustParseAmount("-10.00", "EUR")},
			{tri.Members[1], MustParseAmount("20.00", "EUR")},
		})},
		{"zero split", Split{}},
	}
	for _, c := range cases {
		if _, err := c.split.resolve(tri, total); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s: err = %v, want ErrInvalidRequest", c.name, err)
		}
	}
}

func TestSplitJPYHasNoMinorUnits(t *testing.T) {
	tri := splitTestTricount()
	tri.Currency = "JPY"
	total := MustParseAmount("507", "JPY")

	allocs, err := SplitEqually(tri.Members[0], tri.Members[1]).resolve(tri, total)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	assertAllocations(t, allocs, []string{"u-alice=254", "u-bob=253"})
}

func TestResolveLocalMirrorsTheSplit(t *testing.T) {
	tri := splitTestTricount()
	tri.Currency = "JPY"
	total := MustParseAmount("15000", "JPY")
	local := MustParseAmount("100.00", "USD")

	split := SplitEqually(tri.Members...)
	allocs, err := split.resolve(tri, total)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	locals, err := split.resolveLocal(allocs, local)
	if err != nil {
		t.Fatalf("resolveLocal: %v", err)
	}
	if len(locals) != 3 {
		t.Fatalf("got %d local amounts, want 3", len(locals))
	}
	sum := ZeroAmount("USD")
	for _, l := range locals {
		if l.Currency() != "USD" {
			t.Errorf("local currency = %q, want USD", l.Currency())
		}
		sum, _ = sum.Add(l)
	}
	if !sum.Equal(local) {
		t.Errorf("local allocations sum to %s, want %s", sum, local)
	}
	if got := locals[0].String(); got != "33.34" {
		t.Errorf("first local = %q, want \"33.34\"", got)
	}
}

func TestResolveLocalForExactSplitKeepsProportions(t *testing.T) {
	tri := splitTestTricount()
	tri.Currency = "EUR"
	total := MustParseAmount("100.00", "EUR")
	local := MustParseAmount("1000", "JPY")

	split := SplitExactly([]MemberAmount{
		{tri.Members[0], MustParseAmount("50.00", "EUR")},
		{tri.Members[1], MustParseAmount("30.00", "EUR")},
		{tri.Members[2], MustParseAmount("20.00", "EUR")},
	})
	allocs, err := split.resolve(tri, total)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	locals, err := split.resolveLocal(allocs, local)
	if err != nil {
		t.Fatalf("resolveLocal: %v", err)
	}
	want := []string{"500", "300", "200"}
	sum := ZeroAmount("JPY")
	for i, l := range locals {
		if l.String() != want[i] {
			t.Errorf("local %d = %q, want %q", i, l.String(), want[i])
		}
		sum, _ = sum.Add(l)
	}
	if !sum.Equal(local) {
		t.Errorf("local allocations sum to %s, want %s", sum, local)
	}
}

func TestResolveLocalHandlesZeroAllocations(t *testing.T) {
	// A reimbursement-shaped exact split has a zero allocation; deriving the
	// local mirror must not divide by zero or lose the remainder.
	tri := splitTestTricount()
	total := MustParseAmount("50.00", "EUR")
	local := MustParseAmount("7500", "JPY")

	split := SplitExactly([]MemberAmount{
		{tri.Members[0], MustParseAmount("50.00", "EUR")},
		{tri.Members[1], ZeroAmount("EUR")},
	})
	allocs, err := split.resolve(tri, total)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	locals, err := split.resolveLocal(allocs, local)
	if err != nil {
		t.Fatalf("resolveLocal: %v", err)
	}
	if got := locals[0].String(); got != "7500" {
		t.Errorf("first local = %q, want \"7500\"", got)
	}
	if !locals[1].IsZero() {
		t.Errorf("second local = %q, want zero", locals[1].String())
	}
}
