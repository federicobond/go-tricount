package tricount

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

func entryTestTricount() *Tricount {
	return &Tricount{
		ID:       1,
		Currency: "EUR",
		Status:   StatusActive,
		Members: []*Member{
			{ID: 1, UUID: "u-alice", DisplayName: "Alice"},
			{ID: 2, UUID: "u-bob", DisplayName: "Bob"},
		},
	}
}

type wireMoney struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

// capturedEntry is the request body as the API would receive it.
type capturedEntry struct {
	UUID                string     `json:"uuid"`
	Description         string     `json:"description"`
	Amount              wireMoney  `json:"amount"`
	AmountLocal         *wireMoney `json:"amount_local"`
	ExchangeRate        string     `json:"exchange_rate"`
	MembershipUUIDOwner string     `json:"membership_uuid_owner"`
	TypeTransaction     string     `json:"type_transaction"`
	Status              string     `json:"status"`
	Date                string     `json:"date"`
	Category            string     `json:"category"`
	CategoryCustom      string     `json:"category_custom"`
	Type                *string    `json:"type"` // must never be sent
	Allocations         []struct {
		MembershipUUID string     `json:"membership_uuid"`
		Amount         wireMoney  `json:"amount"`
		AmountLocal    *wireMoney `json:"amount_local"`
		Type           string     `json:"type"`
		ShareRatio     *int       `json:"share_ratio"`
		Membership     any        `json:"membership"` // response-only, never sent
	} `json:"allocations"`
	Attachment []struct {
		ID int64 `json:"id"`
	} `json:"attachment"`
}

func entryCapturingClient(t *testing.T, got *capturedEntry) (*Client, *string) {
	t.Helper()
	var path string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.Method + " " + r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(got); err != nil {
			t.Errorf("decoding entry body: %v", err)
		}
		w.Write([]byte(`{"Response":[{"Id":{"id":123456789}}]}`))
	})
	return c, &path
}

func TestCreateExpenseSendsNegativeAmounts(t *testing.T) {
	var got capturedEntry
	c, path := entryCapturingClient(t, &got)
	tri := entryTestTricount()

	id, err := c.CreateExpense(context.Background(), tri, Expense{
		Description: "Lunch",
		Amount:      MustParseAmount("25.50", "EUR"),
		Payer:       tri.MemberByName("Alice"),
		Split:       SplitEqually(tri.Members...),
		Date:        time.Date(2026, 3, 30, 14, 30, 0, 0, time.UTC),
		Category:    CategoryFoodAndDrink,
	})
	if err != nil {
		t.Fatalf("CreateExpense: %v", err)
	}
	if id != 123456789 {
		t.Errorf("id = %d", id)
	}
	if *path != "POST /v1/user/79290957/registry/1/registry-entry" {
		t.Errorf("request = %q", *path)
	}

	// The public API takes a positive 25.50; the wire gets -25.50.
	if got.Amount.Value != "-25.50" || got.Amount.Currency != "EUR" {
		t.Errorf("amount = %+v, want -25.50 EUR", got.Amount)
	}
	if got.TypeTransaction != "NORMAL" {
		t.Errorf("type_transaction = %q, want NORMAL", got.TypeTransaction)
	}
	if got.Status != "ACTIVE" {
		t.Errorf("status = %q, want ACTIVE", got.Status)
	}
	if got.Type != nil {
		t.Error("the type field must never be sent; the API rejects it as superfluous")
	}
	if got.MembershipUUIDOwner != "u-alice" {
		t.Errorf("owner = %q, want u-alice", got.MembershipUUIDOwner)
	}
	if got.Date != "2026-03-30 14:30:00.000000" {
		t.Errorf("date = %q", got.Date)
	}
	if got.Category != "FOOD_AND_DRINK" {
		t.Errorf("category = %q", got.Category)
	}
	if got.UUID == "" {
		t.Error("no client-generated uuid was sent")
	}

	if len(got.Allocations) != 2 {
		t.Fatalf("got %d allocations, want 2", len(got.Allocations))
	}
	for i, a := range got.Allocations {
		if a.Amount.Value != "-12.75" {
			t.Errorf("allocation %d = %q, want \"-12.75\"", i, a.Amount.Value)
		}
		if a.Type != "AMOUNT" {
			t.Errorf("allocation %d type = %q, want AMOUNT", i, a.Type)
		}
		if a.ShareRatio != nil {
			t.Errorf("allocation %d sent a share_ratio on an equal split", i)
		}
		if a.Membership != nil {
			t.Errorf("allocation %d sent the response-only membership object", i)
		}
	}
	if got.Allocations[0].MembershipUUID != "u-alice" || got.Allocations[1].MembershipUUID != "u-bob" {
		t.Errorf("allocation members = %q, %q",
			got.Allocations[0].MembershipUUID, got.Allocations[1].MembershipUUID)
	}
}

func TestCreateExpenseUnevenSplitSumsExactly(t *testing.T) {
	var got capturedEntry
	c, _ := entryCapturingClient(t, &got)
	tri := entryTestTricount()
	tri.Members = append(tri.Members, &Member{ID: 3, UUID: "u-carol", DisplayName: "Carol"})

	if _, err := c.CreateExpense(context.Background(), tri, Expense{
		Description: "Taxi",
		Amount:      MustParseAmount("10.00", "EUR"),
		Payer:       tri.Members[0],
		Split:       SplitEqually(tri.Members...),
	}); err != nil {
		t.Fatalf("CreateExpense: %v", err)
	}

	want := []string{"-3.34", "-3.33", "-3.33"}
	sum := ZeroAmount("EUR")
	for i, a := range got.Allocations {
		if a.Amount.Value != want[i] {
			t.Errorf("allocation %d = %q, want %q", i, a.Amount.Value, want[i])
		}
		parsed, err := ParseAmount(a.Amount.Value, a.Amount.Currency)
		if err != nil {
			t.Fatalf("re-parsing allocation: %v", err)
		}
		sum, _ = sum.Add(parsed)
	}
	if sum.String() != "-10.00" {
		t.Errorf("allocations sum to %s, want -10.00", sum)
	}
}

func TestCreateExpenseRatioSplit(t *testing.T) {
	var got capturedEntry
	c, _ := entryCapturingClient(t, &got)
	tri := entryTestTricount()

	if _, err := c.CreateExpense(context.Background(), tri, Expense{
		Description: "Hotel",
		Amount:      MustParseAmount("300.00", "EUR"),
		Payer:       tri.Members[0],
		Split: SplitByShares([]MemberShare{
			{tri.Members[0], 2},
			{tri.Members[1], 1},
		}),
	}); err != nil {
		t.Fatalf("CreateExpense: %v", err)
	}

	if got.Allocations[0].Amount.Value != "-200.00" || got.Allocations[1].Amount.Value != "-100.00" {
		t.Errorf("allocations = %q, %q",
			got.Allocations[0].Amount.Value, got.Allocations[1].Amount.Value)
	}
	for i, a := range got.Allocations {
		if a.Type != "RATIO" {
			t.Errorf("allocation %d type = %q, want RATIO", i, a.Type)
		}
		if a.ShareRatio == nil {
			t.Fatalf("allocation %d sent no share_ratio", i)
		}
	}
	if *got.Allocations[0].ShareRatio != 2 || *got.Allocations[1].ShareRatio != 1 {
		t.Errorf("share ratios = %d, %d", *got.Allocations[0].ShareRatio, *got.Allocations[1].ShareRatio)
	}
}

func TestCreateExpenseCustomCategoryForcesOther(t *testing.T) {
	var got capturedEntry
	c, _ := entryCapturingClient(t, &got)
	tri := entryTestTricount()

	if _, err := c.CreateExpense(context.Background(), tri, Expense{
		Description:    "Beans",
		Amount:         MustParseAmount("4.00", "EUR"),
		Payer:          tri.Members[0],
		Split:          SplitEqually(tri.Members[0]),
		Category:       CategoryGroceries, // overridden
		CategoryCustom: "Coffee ☕️",
	}); err != nil {
		t.Fatalf("CreateExpense: %v", err)
	}
	if got.Category != "OTHER" {
		t.Errorf("category = %q, want OTHER when a custom category is set", got.Category)
	}
	if got.CategoryCustom != "Coffee ☕️" {
		t.Errorf("category_custom = %q", got.CategoryCustom)
	}
}

func TestCreateExpenseDefaultsDateToNow(t *testing.T) {
	var got capturedEntry
	c, _ := entryCapturingClient(t, &got)
	tri := entryTestTricount()

	before := time.Now().Add(-time.Minute)
	if _, err := c.CreateExpense(context.Background(), tri, Expense{
		Description: "Now",
		Amount:      MustParseAmount("1.00", "EUR"),
		Payer:       tri.Members[0],
		Split:       SplitEqually(tri.Members[0]),
	}); err != nil {
		t.Fatalf("CreateExpense: %v", err)
	}
	parsed, err := time.Parse(apiTimeLayout, got.Date)
	if err != nil {
		t.Fatalf("parsing the sent date %q: %v", got.Date, err)
	}
	if parsed.Before(before) {
		t.Errorf("date %v is not recent; a zero Date should mean now", parsed)
	}
}

func TestCreateIncomeSendsPositiveAmounts(t *testing.T) {
	var got capturedEntry
	c, _ := entryCapturingClient(t, &got)
	tri := entryTestTricount()

	if _, err := c.CreateIncome(context.Background(), tri, Income{
		Description: "Refund",
		Amount:      MustParseAmount("100.00", "EUR"),
		Receiver:    tri.MemberByName("Bob"),
		Split:       SplitEqually(tri.Members...),
	}); err != nil {
		t.Fatalf("CreateIncome: %v", err)
	}
	if got.TypeTransaction != "INCOME" {
		t.Errorf("type_transaction = %q, want INCOME", got.TypeTransaction)
	}
	if got.Amount.Value != "100.00" {
		t.Errorf("amount = %q, want positive 100.00", got.Amount.Value)
	}
	if got.MembershipUUIDOwner != "u-bob" {
		t.Errorf("owner = %q, want the receiver u-bob", got.MembershipUUIDOwner)
	}
	for i, a := range got.Allocations {
		if a.Amount.Value != "50.00" {
			t.Errorf("allocation %d = %q, want positive 50.00", i, a.Amount.Value)
		}
	}
}

func TestCreateReimbursement(t *testing.T) {
	var got capturedEntry
	c, _ := entryCapturingClient(t, &got)
	tri := entryTestTricount()

	if _, err := c.CreateReimbursement(context.Background(), tri, Reimbursement{
		Description: "Settling up",
		Amount:      MustParseAmount("50.00", "EUR"),
		From:        tri.MemberByName("Alice"),
		To:          tri.MemberByName("Bob"),
	}); err != nil {
		t.Fatalf("CreateReimbursement: %v", err)
	}
	if got.TypeTransaction != "BALANCE" {
		t.Errorf("type_transaction = %q, want BALANCE", got.TypeTransaction)
	}
	if got.MembershipUUIDOwner != "u-alice" {
		t.Errorf("owner = %q, want the payer u-alice", got.MembershipUUIDOwner)
	}
	if got.Amount.Value != "50.00" {
		t.Errorf("amount = %q, want positive 50.00", got.Amount.Value)
	}
	if len(got.Allocations) != 2 {
		t.Fatalf("got %d allocations, want 2", len(got.Allocations))
	}
	if got.Allocations[0].MembershipUUID != "u-bob" || got.Allocations[0].Amount.Value != "50.00" {
		t.Errorf("first allocation = %+v, want u-bob 50.00", got.Allocations[0])
	}
	if got.Allocations[1].MembershipUUID != "u-alice" || got.Allocations[1].Amount.Value != "0.00" {
		t.Errorf("second allocation = %+v, want u-alice 0.00", got.Allocations[1])
	}
}

func TestCreateExpenseForeignCurrency(t *testing.T) {
	var got capturedEntry
	c, _ := entryCapturingClient(t, &got)
	tri := entryTestTricount()
	tri.Currency = "JPY"

	local := MustParseAmount("100.00", "USD")
	if _, err := c.CreateExpense(context.Background(), tri, Expense{
		Description:  "Hotel",
		Amount:       MustParseAmount("15000", "JPY"),
		Payer:        tri.Members[0],
		Split:        SplitEqually(tri.Members...),
		LocalAmount:  &local,
		ExchangeRate: "150",
	}); err != nil {
		t.Fatalf("CreateExpense: %v", err)
	}

	if got.Amount.Value != "-15000" || got.Amount.Currency != "JPY" {
		t.Errorf("amount = %+v, want -15000 JPY", got.Amount)
	}
	if got.AmountLocal == nil {
		t.Fatal("amount_local was not sent")
	}
	if got.AmountLocal.Value != "-100.00" || got.AmountLocal.Currency != "USD" {
		t.Errorf("amount_local = %+v, want -100.00 USD", *got.AmountLocal)
	}
	if got.ExchangeRate != "150" {
		t.Errorf("exchange_rate = %q, want \"150\"", got.ExchangeRate)
	}
	// Each side sums exactly to its own total rather than converting
	// per-member and drifting.
	wantJPY := []string{"-7500", "-7500"}
	wantUSD := []string{"-50.00", "-50.00"}
	for i, a := range got.Allocations {
		if a.Amount.Value != wantJPY[i] {
			t.Errorf("allocation %d amount = %q, want %q", i, a.Amount.Value, wantJPY[i])
		}
		if a.AmountLocal == nil {
			t.Fatalf("allocation %d sent no amount_local", i)
		}
		if a.AmountLocal.Value != wantUSD[i] {
			t.Errorf("allocation %d amount_local = %q, want %q", i, a.AmountLocal.Value, wantUSD[i])
		}
	}
}

func TestCreateExpenseFetchesMissingExchangeRate(t *testing.T) {
	var got capturedEntry
	var paths []string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/v1/user/79290957/exchange-rate" {
			w.Write([]byte(`{"Response":[{"ExchangeRate":{"currency_source":"USD","currency_target":"JPY","rate":"150","number_of_decimal":0}}]}`))
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"Response":[{"Id":{"id":1}}]}`))
	})

	tri := entryTestTricount()
	tri.Currency = "JPY"
	local := MustParseAmount("100.00", "USD")
	if _, err := c.CreateExpense(context.Background(), tri, Expense{
		Description: "Hotel",
		Amount:      MustParseAmount("15000", "JPY"),
		Payer:       tri.Members[0],
		Split:       SplitEqually(tri.Members[0]),
		LocalAmount: &local,
	}); err != nil {
		t.Fatalf("CreateExpense: %v", err)
	}
	if got.ExchangeRate != "150" {
		t.Errorf("exchange_rate = %q, want the fetched \"150\"", got.ExchangeRate)
	}
	var sawRateCall bool
	for _, p := range paths {
		if p == "/v1/user/79290957/exchange-rate" {
			sawRateCall = true
		}
	}
	if !sawRateCall {
		t.Error("an empty ExchangeRate should be fetched")
	}
}

func TestCreateTransactionValidation(t *testing.T) {
	tri := entryTestTricount()
	stranger := &Member{ID: 9, UUID: "u-stranger", DisplayName: "Stranger"}
	ctx := context.Background()

	cases := []struct {
		name    string
		expense Expense
	}{
		{"nil payer", Expense{
			Description: "x", Amount: MustParseAmount("1.00", "EUR"),
			Split: SplitEqually(tri.Members[0]),
		}},
		{"payer not in tricount", Expense{
			Description: "x", Amount: MustParseAmount("1.00", "EUR"),
			Payer: stranger, Split: SplitEqually(tri.Members[0]),
		}},
		{"zero amount", Expense{
			Description: "x", Amount: ZeroAmount("EUR"),
			Payer: tri.Members[0], Split: SplitEqually(tri.Members[0]),
		}},
		{"negative amount", Expense{
			Description: "x", Amount: MustParseAmount("-1.00", "EUR"),
			Payer: tri.Members[0], Split: SplitEqually(tri.Members[0]),
		}},
		{"wrong currency", Expense{
			Description: "x", Amount: MustParseAmount("100", "JPY"),
			Payer: tri.Members[0], Split: SplitEqually(tri.Members[0]),
		}},
		{"empty description", Expense{
			Amount: MustParseAmount("1.00", "EUR"),
			Payer:  tri.Members[0], Split: SplitEqually(tri.Members[0]),
		}},
		{"no split", Expense{
			Description: "x", Amount: MustParseAmount("1.00", "EUR"),
			Payer: tri.Members[0],
		}},
		{"split member not in tricount", Expense{
			Description: "x", Amount: MustParseAmount("1.00", "EUR"),
			Payer: tri.Members[0], Split: SplitEqually(stranger),
		}},
	}

	for _, tc := range cases {
		var called bool
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.Write([]byte(`{"Response":[{"Id":{"id":1}}]}`))
		})
		if _, err := c.CreateExpense(ctx, tri, tc.expense); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s: err = %v, want ErrInvalidRequest", tc.name, err)
		}
		if called {
			t.Errorf("%s: validation failure reached the network", tc.name)
		}
	}
}

func TestCreateReimbursementValidation(t *testing.T) {
	tri := entryTestTricount()
	alice := tri.MemberByName("Alice")
	ctx := context.Background()

	cases := []struct {
		name string
		r    Reimbursement
	}{
		{"same member both ends", Reimbursement{
			Amount: MustParseAmount("1.00", "EUR"), From: alice, To: alice,
		}},
		{"nil recipient", Reimbursement{
			Amount: MustParseAmount("1.00", "EUR"), From: alice,
		}},
		{"zero amount", Reimbursement{
			Amount: ZeroAmount("EUR"), From: alice, To: tri.Members[1],
		}},
	}
	for _, tc := range cases {
		var called bool
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			called = true
		})
		if _, err := c.CreateReimbursement(ctx, tri, tc.r); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s: err = %v, want ErrInvalidRequest", tc.name, err)
		}
		if called {
			t.Errorf("%s: validation failure reached the network", tc.name)
		}
	}
}

func TestUpdateExpenseSendsFullReplacement(t *testing.T) {
	var got capturedEntry
	var path string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.Method + " " + r.URL.Path
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"Response":[{"Id":{"id":900}}]}`))
	})
	tri := entryTestTricount()

	err := c.UpdateExpense(context.Background(), tri, 900, Expense{
		Description: "Updated lunch",
		Amount:      MustParseAmount("35.00", "EUR"),
		Payer:       tri.MemberByName("Bob"),
		Split:       SplitEqually(tri.Members...),
		Category:    CategoryFoodAndDrink,
	})
	if err != nil {
		t.Fatalf("UpdateExpense: %v", err)
	}
	if path != "PUT /v1/user/79290957/registry/1/registry-entry/900" {
		t.Errorf("request = %q", path)
	}
	if got.Description != "Updated lunch" {
		t.Errorf("description = %q", got.Description)
	}
	if got.Amount.Value != "-35.00" {
		t.Errorf("amount = %q, want -35.00", got.Amount.Value)
	}
	if got.MembershipUUIDOwner != "u-bob" {
		t.Errorf("owner = %q", got.MembershipUUIDOwner)
	}
	if got.TypeTransaction != "NORMAL" || got.Status != "ACTIVE" {
		t.Errorf("type_transaction = %q, status = %q", got.TypeTransaction, got.Status)
	}
	if len(got.Allocations) != 2 {
		t.Errorf("got %d allocations, want 2", len(got.Allocations))
	}
	if got.Date == "" {
		t.Error("date must be sent on an update")
	}
}

func TestUpdateIncomeAndReimbursement(t *testing.T) {
	var got capturedEntry
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"Response":[{"Id":{"id":901}}]}`))
	})
	tri := entryTestTricount()
	ctx := context.Background()

	if err := c.UpdateIncome(ctx, tri, 901, Income{
		Description: "Bigger refund",
		Amount:      MustParseAmount("120.00", "EUR"),
		Receiver:    tri.Members[1],
		Split:       SplitEqually(tri.Members...),
	}); err != nil {
		t.Fatalf("UpdateIncome: %v", err)
	}
	if got.TypeTransaction != "INCOME" || got.Amount.Value != "120.00" {
		t.Errorf("income update = %q %q", got.TypeTransaction, got.Amount.Value)
	}

	if err := c.UpdateReimbursement(ctx, tri, 902, Reimbursement{
		Amount: MustParseAmount("20.00", "EUR"),
		From:   tri.Members[0],
		To:     tri.Members[1],
	}); err != nil {
		t.Fatalf("UpdateReimbursement: %v", err)
	}
	if got.TypeTransaction != "BALANCE" {
		t.Errorf("reimbursement update type = %q", got.TypeTransaction)
	}
	if len(got.Allocations) != 2 {
		t.Errorf("got %d allocations, want 2", len(got.Allocations))
	}
}

func TestUpdateValidation(t *testing.T) {
	var called bool
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	tri := entryTestTricount()
	good := Expense{
		Description: "x", Amount: MustParseAmount("1.00", "EUR"),
		Payer: tri.Members[0], Split: SplitEqually(tri.Members[0]),
	}

	if err := c.UpdateExpense(context.Background(), tri, 0, good); !errors.Is(err, ErrInvalidRequest) {
		t.Error("a zero transaction id should be rejected")
	}
	if called {
		t.Error("validation failures must not reach the network")
	}
}

func TestDeleteTransaction(t *testing.T) {
	var method, path string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.Write([]byte(`{"Response":[{"Id":{"id":900}}]}`))
	})
	tri := entryTestTricount()

	if err := c.DeleteTransaction(context.Background(), tri, 900); err != nil {
		t.Fatalf("DeleteTransaction: %v", err)
	}
	if method != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", method)
	}
	if path != "/v1/user/79290957/registry/1/registry-entry/900" {
		t.Errorf("path = %q", path)
	}

	if err := c.DeleteTransaction(context.Background(), tri, 0); !errors.Is(err, ErrInvalidRequest) {
		t.Error("a zero transaction id should be rejected")
	}
}

func TestSpecFromTransactionRoundTrips(t *testing.T) {
	tri := entryTestTricount()
	tx := &Transaction{
		ID:          900,
		Description: "Ramen",
		Kind:        KindExpense,
		Status:      TransactionActive,
		Amount:      MustParseAmount("25.50", "EUR"),
		PayerUUID:   "u-alice",
		Category:    CategoryFoodAndDrink,
		Allocations: []Allocation{
			{MemberUUID: "u-alice", Amount: MustParseAmount("12.75", "EUR"), Type: AllocationAmount},
			{MemberUUID: "u-bob", Amount: MustParseAmount("12.75", "EUR"), Type: AllocationAmount},
		},
		AttachmentIDs: []int64{7},
	}

	spec, err := specFromTransaction(tri, tx)
	if err != nil {
		t.Fatalf("specFromTransaction: %v", err)
	}
	if spec.kind != KindExpense {
		t.Errorf("kind = %q", spec.kind)
	}
	if spec.payer == nil || spec.payer.UUID != "u-alice" {
		t.Errorf("payer = %+v", spec.payer)
	}
	if !spec.amount.Equal(tx.Amount) {
		t.Errorf("amount = %s, want %s", spec.amount, tx.Amount)
	}
	if len(spec.allocations) != 2 {
		t.Errorf("got %d allocations, want 2", len(spec.allocations))
	}
	if len(spec.attachmentIDs) != 1 || spec.attachmentIDs[0] != 7 {
		t.Errorf("attachmentIDs = %v", spec.attachmentIDs)
	}

	orphan := &Transaction{ID: 1, Description: "x", Kind: KindExpense,
		Amount: MustParseAmount("1.00", "EUR"), PayerUUID: "u-ghost"}
	if _, err := specFromTransaction(tri, orphan); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("err = %v, want ErrInvalidRequest", err)
	}
}

func TestCreateExpenseUsesProvidedUUID(t *testing.T) {
	var got capturedEntry
	c, _ := entryCapturingClient(t, &got)
	tri := entryTestTricount()

	_, err := c.CreateExpense(context.Background(), tri, Expense{
		UUID:        "3f2504e0-4f89-41d3-9a0c-0305e82c3301",
		Description: "Lunch",
		Amount:      MustParseAmount("25.50", "EUR"),
		Payer:       tri.MemberByName("Alice"),
		Split:       SplitEqually(tri.Members...),
	})
	if err != nil {
		t.Fatalf("CreateExpense: %v", err)
	}
	if got.UUID != "3f2504e0-4f89-41d3-9a0c-0305e82c3301" {
		t.Errorf("uuid = %q, want the one the caller chose", got.UUID)
	}
}

func TestCreateExpenseRejectsMalformedUUID(t *testing.T) {
	var got capturedEntry
	c, path := entryCapturingClient(t, &got)
	tri := entryTestTricount()

	_, err := c.CreateExpense(context.Background(), tri, Expense{
		UUID:        "not-a-uuid",
		Description: "Lunch",
		Amount:      MustParseAmount("25.50", "EUR"),
		Payer:       tri.MemberByName("Alice"),
		Split:       SplitEqually(tri.Members...),
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("err = %v, want ErrInvalidRequest", err)
	}
	if *path != "" {
		t.Errorf("a request was sent (%q); a malformed uuid must be caught locally", *path)
	}
}

func TestCreateIncomeAndReimbursementUseProvidedUUID(t *testing.T) {
	const chosen = "3f2504e0-4f89-41d3-9a0c-0305e82c3302"

	t.Run("income", func(t *testing.T) {
		var got capturedEntry
		c, _ := entryCapturingClient(t, &got)
		tri := entryTestTricount()

		_, err := c.CreateIncome(context.Background(), tri, Income{
			UUID:        chosen,
			Description: "Refund",
			Amount:      MustParseAmount("10.00", "EUR"),
			Receiver:    tri.MemberByName("Alice"),
			Split:       SplitEqually(tri.Members...),
		})
		if err != nil {
			t.Fatalf("CreateIncome: %v", err)
		}
		if got.UUID != chosen {
			t.Errorf("uuid = %q, want the one the caller chose", got.UUID)
		}
	})

	t.Run("reimbursement", func(t *testing.T) {
		var got capturedEntry
		c, _ := entryCapturingClient(t, &got)
		tri := entryTestTricount()

		_, err := c.CreateReimbursement(context.Background(), tri, Reimbursement{
			UUID:   chosen,
			Amount: MustParseAmount("10.00", "EUR"),
			From:   tri.MemberByName("Alice"),
			To:     tri.MemberByName("Bob"),
		})
		if err != nil {
			t.Fatalf("CreateReimbursement: %v", err)
		}
		if got.UUID != chosen {
			t.Errorf("uuid = %q, want the one the caller chose", got.UUID)
		}
	})
}

func TestUpdateRejectsProvidedUUID(t *testing.T) {
	var got capturedEntry
	c, path := entryCapturingClient(t, &got)
	tri := entryTestTricount()

	// An entry being updated is identified by its id. Accepting a UUID here
	// would suggest the caller can change that identity, which they cannot.
	err := c.UpdateExpense(context.Background(), tri, 42, Expense{
		UUID:        "3f2504e0-4f89-41d3-9a0c-0305e82c3303",
		Description: "Lunch",
		Amount:      MustParseAmount("25.50", "EUR"),
		Payer:       tri.MemberByName("Alice"),
		Split:       SplitEqually(tri.Members...),
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("err = %v, want ErrInvalidRequest", err)
	}
	if *path != "" {
		t.Errorf("a request was sent (%q); this must be caught locally", *path)
	}
}
