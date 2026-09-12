package tricount

import (
	"os"
	"testing"
	"time"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return raw
}

func decodeFixtureTricount(t *testing.T, name string) *Tricount {
	t.Helper()
	regs, err := decodeEnvelope[wireRegistry](loadFixture(t, name), "Registry")
	if err != nil {
		t.Fatalf("decodeEnvelope: %v", err)
	}
	if len(regs) != 1 {
		t.Fatalf("fixture carried %d registries, want 1", len(regs))
	}
	tri, err := regs[0].toDomain()
	if err != nil {
		t.Fatalf("toDomain: %v", err)
	}
	return tri
}

func TestDecodeRegistryMetadata(t *testing.T) {
	tri := decodeFixtureTricount(t, "registry.json")

	if tri.ID != 102257091 {
		t.Errorf("ID = %d", tri.ID)
	}
	if tri.Title != "Taiwan" {
		t.Errorf("Title = %q", tri.Title)
	}
	if tri.Description != "Spring trip" {
		t.Errorf("Description = %q", tri.Description)
	}
	if tri.Currency != "JPY" {
		t.Errorf("Currency = %q", tri.Currency)
	}
	if tri.Emoji != "🍜" {
		t.Errorf("Emoji = %q", tri.Emoji)
	}
	if tri.Category != CategoryTravel {
		t.Errorf("Category = %q", tri.Category)
	}
	if tri.Status != StatusActive {
		t.Errorf("Status = %q", tri.Status)
	}
	if tri.IsArchived() {
		t.Error("READ_WRITE should not be archived")
	}
	if tri.PublicToken != "tABC123xyz" {
		t.Errorf("PublicToken = %q", tri.PublicToken)
	}
	want := time.Date(2026, 3, 1, 10, 57, 38, 87586000, time.UTC)
	if !tri.Created.Equal(want) {
		t.Errorf("Created = %v, want %v", tri.Created, want)
	}
}

func TestDecodeRegistryMembers(t *testing.T) {
	tri := decodeFixtureTricount(t, "registry.json")

	if len(tri.Members) != 2 {
		t.Fatalf("got %d members, want 2", len(tri.Members))
	}
	alice := tri.MemberByName("Alice")
	if alice == nil {
		t.Fatal("MemberByName(\"Alice\") returned nil")
	}
	if alice.ID != 438275026 {
		t.Errorf("Alice.ID = %d", alice.ID)
	}
	if alice.UUID != "aaaaaaaa-0000-4000-8000-000000000001" {
		t.Errorf("Alice.UUID = %q", alice.UUID)
	}
	if alice.Status != MemberActive {
		t.Errorf("Alice.Status = %q", alice.Status)
	}
	if tri.MemberByUUID(alice.UUID) != alice {
		t.Error("MemberByUUID did not return the same member")
	}
	if tri.MemberByName("Nobody") != nil {
		t.Error("MemberByName should return nil for an unknown name")
	}
	if linked := tri.LinkedMember(); linked != alice {
		t.Errorf("LinkedMember = %v, want Alice", linked)
	}
}

func TestDecodeExpenseIsPositiveInDomain(t *testing.T) {
	tri := decodeFixtureTricount(t, "registry.json")

	tx := tri.TransactionByID(900000001)
	if tx == nil {
		t.Fatal("TransactionByID(900000001) returned nil")
	}
	if tx.Kind != KindExpense {
		t.Errorf("Kind = %q, want %q", tx.Kind, KindExpense)
	}
	if tx.Description != "Ramen" {
		t.Errorf("Description = %q", tx.Description)
	}
	if got := tx.Amount.String(); got != "507" {
		t.Errorf("Amount = %q, want \"507\"", got)
	}
	if tx.Amount.Currency() != "JPY" {
		t.Errorf("Amount currency = %q", tx.Amount.Currency())
	}
	if tx.Status != TransactionActive {
		t.Errorf("Status = %q", tx.Status)
	}
	if tx.Category != CategoryFoodAndDrink {
		t.Errorf("Category = %q", tx.Category)
	}
	if tx.PayerUUID != "aaaaaaaa-0000-4000-8000-000000000001" {
		t.Errorf("PayerUUID = %q, want Alice's UUID", tx.PayerUUID)
	}
	want := time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)
	if !tx.Date.Equal(want) {
		t.Errorf("Date = %v, want %v", tx.Date, want)
	}

	if len(tx.Allocations) != 2 {
		t.Fatalf("got %d allocations, want 2", len(tx.Allocations))
	}
	if got := tx.Allocations[0].Amount.String(); got != "254" {
		t.Errorf("allocation 0 = %q, want \"254\"", got)
	}
	if got := tx.Allocations[1].Amount.String(); got != "253" {
		t.Errorf("allocation 1 = %q, want \"253\"", got)
	}
	if tx.Allocations[0].MemberUUID != "aaaaaaaa-0000-4000-8000-000000000001" {
		t.Errorf("allocation 0 member = %q", tx.Allocations[0].MemberUUID)
	}
	if tx.Allocations[1].MemberUUID != "bbbbbbbb-0000-4000-8000-000000000002" {
		t.Errorf("allocation 1 member = %q", tx.Allocations[1].MemberUUID)
	}
	if tx.Allocations[0].Type != AllocationAmount {
		t.Errorf("allocation type = %q", tx.Allocations[0].Type)
	}

	sum := ZeroAmount("JPY")
	for _, a := range tx.Allocations {
		var err error
		sum, err = sum.Add(a.Amount)
		if err != nil {
			t.Fatalf("summing allocations: %v", err)
		}
	}
	if !sum.Equal(tx.Amount) {
		t.Errorf("allocations sum to %s, want %s", sum, tx.Amount)
	}
}

func TestDecodeForeignCurrencyIncome(t *testing.T) {
	tri := decodeFixtureTricount(t, "registry.json")

	tx := tri.TransactionByID(900000002)
	if tx == nil {
		t.Fatal("TransactionByID(900000002) returned nil")
	}
	if tx.Kind != KindIncome {
		t.Errorf("Kind = %q, want %q", tx.Kind, KindIncome)
	}
	if got := tx.Amount.String(); got != "1000" {
		t.Errorf("Amount = %q, want \"1000\"", got)
	}
	if tx.LocalAmount == nil {
		t.Fatal("LocalAmount is nil")
	}
	if got := tx.LocalAmount.String(); got != "220.00" {
		t.Errorf("LocalAmount = %q, want \"220.00\"", got)
	}
	if got := tx.LocalAmount.Currency(); got != "TWD" {
		t.Errorf("LocalAmount currency = %q, want \"TWD\"", got)
	}
	if tx.ExchangeRate != "4.5454" {
		t.Errorf("ExchangeRate = %q", tx.ExchangeRate)
	}
	if tx.Category != CategoryOther {
		t.Errorf("Category = %q, want OTHER", tx.Category)
	}
	if tx.CategoryCustom != "Souvenirs 🎁" {
		t.Errorf("CategoryCustom = %q", tx.CategoryCustom)
	}
	if len(tx.AttachmentIDs) != 1 || tx.AttachmentIDs[0] != 12345 {
		t.Errorf("AttachmentIDs = %v, want [12345]", tx.AttachmentIDs)
	}
	if tx.PayerUUID != "bbbbbbbb-0000-4000-8000-000000000002" {
		t.Errorf("PayerUUID = %q, want Bob's UUID", tx.PayerUUID)
	}
}

func TestDecodeGallery(t *testing.T) {
	tri := decodeFixtureTricount(t, "registry.json")

	if len(tri.Gallery) != 1 {
		t.Fatalf("got %d gallery attachments, want 1", len(tri.Gallery))
	}
	g := tri.Gallery[0]
	if g.AttachmentID != 12345 {
		t.Errorf("AttachmentID = %d", g.AttachmentID)
	}
	if g.UUID != "eeeeeeee-0000-4000-8000-000000000005" {
		t.Errorf("UUID = %q", g.UUID)
	}
	if g.ContentType != "image/jpeg" {
		t.Errorf("ContentType = %q", g.ContentType)
	}
	if g.OriginalURL != "https://example.invalid/img.jpg" {
		t.Errorf("OriginalURL = %q", g.OriginalURL)
	}
	if g.UploaderUUID != "aaaaaaaa-0000-4000-8000-000000000001" {
		t.Errorf("UploaderUUID = %q", g.UploaderUUID)
	}
}

func TestEnumValidity(t *testing.T) {
	if !CategoryFoodAndDrink.Valid() {
		t.Error("FOOD_AND_DRINK should be valid")
	}
	if Category("NEWLY_INVENTED").Valid() {
		t.Error("an unknown category should report invalid")
	}
	if !KindExpense.Valid() || !KindIncome.Valid() || !KindReimbursement.Valid() {
		t.Error("all three transaction kinds should be valid")
	}
	if TransactionKind("WAT").Valid() {
		t.Error("an unknown kind should report invalid")
	}
}

func TestUnknownEnumValuesArePreserved(t *testing.T) {
	body := []byte(`{"Response":[{"Registry":{
		"id":1,"currency":"EUR","status":"READ_WRITE","category":"FUTURE_CATEGORY",
		"memberships":[],"all_registry_entry":[]
	}}]}`)
	regs, err := decodeEnvelope[wireRegistry](body, "Registry")
	if err != nil {
		t.Fatalf("decodeEnvelope: %v", err)
	}
	tri, err := regs[0].toDomain()
	if err != nil {
		t.Fatalf("toDomain: %v", err)
	}
	if tri.Category != Category("FUTURE_CATEGORY") {
		t.Errorf("Category = %q, want it preserved verbatim", tri.Category)
	}
	if tri.Category.Valid() {
		t.Error("an unknown category should report invalid but still decode")
	}
}

func TestArchivedStatus(t *testing.T) {
	body := []byte(`{"Response":[{"Registry":{
		"id":1,"currency":"EUR","status":"READ_ONLY","memberships":[],"all_registry_entry":[]
	}}]}`)
	regs, _ := decodeEnvelope[wireRegistry](body, "Registry")
	tri, err := regs[0].toDomain()
	if err != nil {
		t.Fatalf("toDomain: %v", err)
	}
	if !tri.IsArchived() {
		t.Error("READ_ONLY should be archived")
	}
	if tri.LinkedMember() != nil {
		t.Error("LinkedMember should be nil when nothing is linked")
	}
}
