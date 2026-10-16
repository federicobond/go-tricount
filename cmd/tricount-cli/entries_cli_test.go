package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// allocationsOf returns member uuid -> allocation of the one entry posted.
func allocationsOf(t *testing.T, f *fakeAPI) map[string]map[string]any {
	t.Helper()
	if len(f.entries) != 1 {
		t.Fatalf("entries posted = %d, want 1", len(f.entries))
	}
	out := map[string]map[string]any{}
	for _, raw := range f.entries[0]["allocations"].([]any) {
		a := raw.(map[string]any)
		out[a["membership_uuid"].(string)] = a
	}
	return out
}

func amountValue(a any) string {
	return a.(map[string]any)["value"].(string)
}

func TestAddPostsAnExpenseWithRatioSplit(t *testing.T) {
	a, f, stderr := newFakeApp(t)
	out := stdoutOf(t, a, stderr, "add", "1500", "Ramen",
		"--paid-by", "Bob", "--split", "Alice=2,Bob=1", "--on", "2026-10-10", "--category", "Food 🍜")

	if len(f.entries) != 1 {
		t.Fatalf("entries posted = %d, want 1", len(f.entries))
	}
	e := f.entries[0]
	bob, _ := f.memberNamed("Bob")
	if e["description"] != "Ramen" || e["membership_uuid_owner"] != bob.UUID {
		t.Errorf("entry = %v", e)
	}
	// Expenses go on the wire negative, in the tricount's currency.
	if got := amountValue(e["amount"]); got != "-1500" {
		t.Errorf("amount = %q, want -1500", got)
	}
	if e["type_transaction"] != "NORMAL" {
		t.Errorf("type_transaction = %v", e["type_transaction"])
	}
	if !strings.HasPrefix(e["date"].(string), "2026-10-10") {
		t.Errorf("date = %v, want 10 Oct", e["date"])
	}
	if e["category_custom"] != "Food 🍜" {
		t.Errorf("category_custom = %v", e["category_custom"])
	}
	alice, _ := f.memberNamed("Alice")
	allocs := allocationsOf(t, f)
	if amountValue(allocs[alice.UUID]["amount"]) != "-1000" || amountValue(allocs[bob.UUID]["amount"]) != "-500" {
		t.Errorf("allocations = %v, want Alice -1000, Bob -500", allocs)
	}
	if !strings.Contains(out, "555000111") {
		t.Errorf("stdout should carry the new transaction id: %q", out)
	}
}

func TestAddDefaultsToTheLinkedMemberSplitEqually(t *testing.T) {
	a, f, stderr := newFakeApp(t)
	stdoutOf(t, a, stderr, "add", "1000", "Taxi")

	alice, _ := f.memberNamed("Alice")
	bob, _ := f.memberNamed("Bob")
	if got := f.entries[0]["membership_uuid_owner"]; got != alice.UUID {
		t.Errorf("payer = %v, want the linked member Alice", got)
	}
	allocs := allocationsOf(t, f)
	if amountValue(allocs[alice.UUID]["amount"]) != "-500" || amountValue(allocs[bob.UUID]["amount"]) != "-500" {
		t.Errorf("allocations = %v, want 500 each", allocs)
	}
}

func TestAddSplitAmongNamedMembersOnly(t *testing.T) {
	a, f, stderr := newFakeApp(t)
	stdoutOf(t, a, stderr, "add", "900", "Gift for Alice", "--split", "Bob")

	bob, _ := f.memberNamed("Bob")
	allocs := allocationsOf(t, f)
	if len(allocs) != 1 || amountValue(allocs[bob.UUID]["amount"]) != "-900" {
		t.Errorf("allocations = %v, want Bob alone with 900", allocs)
	}
}

func TestAddSplitExactAmounts(t *testing.T) {
	a, f, stderr := newFakeApp(t)
	stdoutOf(t, a, stderr, "add", "1000", "Dinner", "--exact", "Alice=400,Bob=600")

	alice, _ := f.memberNamed("Alice")
	bob, _ := f.memberNamed("Bob")
	allocs := allocationsOf(t, f)
	if amountValue(allocs[alice.UUID]["amount"]) != "-400" || amountValue(allocs[bob.UUID]["amount"]) != "-600" {
		t.Errorf("allocations = %v, want Alice 400, Bob 600", allocs)
	}
}

func TestAddExactSplitMustSumToTheTotal(t *testing.T) {
	a, f, stderr := newFakeApp(t)
	if code := a.run(t.Context(), []string{"add", "1000", "Dinner", "--exact", "Alice=400,Bob=500"}); code == 0 {
		t.Fatal("exit code = 0; exact shares that do not sum to the total must fail")
	}
	if len(f.entries) != 0 {
		t.Error("nothing should have been posted")
	}
	if !strings.Contains(stderr.String(), "1000") {
		t.Errorf("the error should name the total: %q", stderr.String())
	}
}

func TestAddUnknownMemberNamesTheOptions(t *testing.T) {
	a, f, stderr := newFakeApp(t)
	if code := a.run(t.Context(), []string{"add", "1000", "Dinner", "--paid-by", "Carol"}); code == 0 {
		t.Fatal("exit code = 0, want non-zero for an unknown member")
	}
	if len(f.entries) != 0 {
		t.Error("nothing should have been posted")
	}
	msg := stderr.String()
	if !strings.Contains(msg, "Carol") || !strings.Contains(msg, "Alice") || !strings.Contains(msg, "Bob") {
		t.Errorf("error should name the bad member and the real ones: %q", msg)
	}
}

func TestAddUUIDIsPassedThrough(t *testing.T) {
	a, f, stderr := newFakeApp(t)
	const u = "0f1e2d3c-4b5a-4968-8777-665544332211"
	stdoutOf(t, a, stderr, "add", "1000", "Taxi", "--uuid", u)
	if got := f.entries[0]["uuid"]; got != u {
		t.Errorf("uuid = %v, want %s", got, u)
	}
}

func TestAddJSONEmitsTheID(t *testing.T) {
	a, _, stderr := newFakeApp(t)
	out := stdoutOf(t, a, stderr, "add", "--json", "1000", "Taxi")
	var got struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if got.ID != 555000111 {
		t.Errorf("id = %d", got.ID)
	}
}

const secondRegistry = `{"id":777000001,"title":"Trip","currency":"JPY","status":"READ_WRITE",
	"public_identifier_token":"tTRIP0001","membership_uuid_active":null,"memberships":[],"all_registry_entry":[]}`

func TestAddRefusesToGuessBetweenSeveralTricounts(t *testing.T) {
	a, f, stderr := newFakeApp(t)
	f.extra = secondRegistry
	if code := a.run(t.Context(), []string{"add", "1000", "Taxi"}); code == 0 {
		t.Fatal("exit code = 0; with two tricounts followed the command must ask which")
	}
	if len(f.entries) != 0 {
		t.Error("nothing should have been posted")
	}
	msg := stderr.String()
	for _, want := range []string{"Taiwan", "Trip", "102257091", "777000001"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should mention %q: %q", want, msg)
		}
	}
}

func TestAddInSelectsByTitleOrID(t *testing.T) {
	for _, selector := range []string{"taiwan", "102257091"} {
		t.Run(selector, func(t *testing.T) {
			a, f, stderr := newFakeApp(t)
			f.extra = secondRegistry
			stdoutOf(t, a, stderr, "add", "1000", "Taxi", "--in", selector)
			if len(f.entries) != 1 {
				t.Fatalf("entries posted = %d, want 1", len(f.entries))
			}
		})
	}
}

func TestAddCategoryMapsAppCategoriesAndKeepsLabels(t *testing.T) {
	cases := []struct{ flag, category, custom string }{
		{"groceries", "GROCERIES", ""},
		{"Food & Drink", "FOOD_AND_DRINK", ""},
		{"Internet 🛜", "OTHER", "Internet 🛜"},
	}
	for _, tc := range cases {
		t.Run(tc.flag, func(t *testing.T) {
			a, f, stderr := newFakeApp(t)
			stdoutOf(t, a, stderr, "add", "1000", "Thing", "--category", tc.flag)
			e := f.entries[0]
			if e["category"] != tc.category {
				t.Errorf("category = %v, want %q", e["category"], tc.category)
			}
			got, _ := e["category_custom"].(string)
			if got != tc.custom {
				t.Errorf("category_custom = %q, want %q", got, tc.custom)
			}
		})
	}
}

func TestPayPostsAReimbursement(t *testing.T) {
	a, f, stderr := newFakeApp(t)
	out := stdoutOf(t, a, stderr, "pay", "253", "--from", "Bob", "--to", "Alice", "--on", "2026-10-11")

	if len(f.entries) != 1 {
		t.Fatalf("entries posted = %d, want 1", len(f.entries))
	}
	e := f.entries[0]
	alice, _ := f.memberNamed("Alice")
	bob, _ := f.memberNamed("Bob")
	// On the wire a transfer is a positive BALANCE entry whose recipient
	// allocation carries the whole amount; see TestCreateReimbursement.
	if e["type_transaction"] != "BALANCE" {
		t.Errorf("type_transaction = %v, want BALANCE", e["type_transaction"])
	}
	if e["membership_uuid_owner"] != bob.UUID {
		t.Errorf("payer = %v, want Bob", e["membership_uuid_owner"])
	}
	allocs := allocationsOf(t, f)
	if amountValue(allocs[alice.UUID]["amount"]) != "253" {
		t.Errorf("Alice's allocation = %v, want the whole 253", allocs[alice.UUID])
	}
	if !strings.HasPrefix(e["date"].(string), "2026-10-11") {
		t.Errorf("date = %v", e["date"])
	}
	if !strings.Contains(out, "555000111") {
		t.Errorf("stdout should carry the new transaction id: %q", out)
	}
}

func TestPayDefaultsFromToTheLinkedMember(t *testing.T) {
	a, f, stderr := newFakeApp(t)
	stdoutOf(t, a, stderr, "pay", "100", "--to", "Bob")
	alice, _ := f.memberNamed("Alice")
	if got := f.entries[0]["membership_uuid_owner"]; got != alice.UUID {
		t.Errorf("payer = %v, want the linked member Alice", got)
	}
}

func TestPayNeedsARecipient(t *testing.T) {
	a, f, stderr := newFakeApp(t)
	if code := a.run(t.Context(), []string{"pay", "100"}); code == 0 {
		t.Fatal("exit code = 0; a reimbursement without --to must fail")
	}
	if len(f.entries) != 0 {
		t.Error("nothing should have been posted")
	}
	if !strings.Contains(stderr.String(), "--to") {
		t.Errorf("the error should mention --to: %q", stderr.String())
	}
}

func TestRemoveDeletesTheTransaction(t *testing.T) {
	a, f, stderr := newFakeApp(t)
	out := stdoutOf(t, a, stderr, "remove", "2106922472")
	if len(f.deletes) != 1 || !strings.HasSuffix(f.deletes[0], "/registry/102257091/registry-entry/2106922472") {
		t.Errorf("deletes = %v, want the entry under its tricount", f.deletes)
	}
	if !strings.Contains(out, "2106922472") {
		t.Errorf("stdout should name what was removed: %q", out)
	}
}

func TestRemoveRejectsANonNumericID(t *testing.T) {
	a, f, stderr := newFakeApp(t)
	if code := a.run(t.Context(), []string{"remove", "Ramen"}); code == 0 {
		t.Fatal("exit code = 0, want non-zero for a non-numeric transaction id")
	}
	if len(f.deletes) != 0 {
		t.Error("nothing should have been deleted")
	}
	if !strings.Contains(stderr.String(), "Ramen") {
		t.Errorf("stderr should name the bad argument: %q", stderr.String())
	}
}
