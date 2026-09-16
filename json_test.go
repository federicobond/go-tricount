package tricount

import (
	"encoding/json"
	"testing"
	"time"
)

// keysOf marshals v and returns its top-level JSON keys. Decoding into a
// tagged struct would not do: encoding/json matches keys case-insensitively,
// so it cannot tell "display_name" from "DisplayName".
func keysOf(t *testing.T, v any) map[string]bool {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshalling %T: %v", v, err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("%T did not marshal to an object: %v\n%s", v, err, raw)
	}
	keys := make(map[string]bool, len(obj))
	for k := range obj {
		keys[k] = true
	}
	return keys
}

func assertKeys(t *testing.T, v any, want ...string) {
	t.Helper()
	got := keysOf(t, v)
	for _, k := range want {
		if !got[k] {
			t.Errorf("%T is missing key %q; got %v", v, k, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%T marshalled %d keys, want %d: got %v", v, len(got), len(want), got)
	}
}

// The public types marshal with snake_case keys, matching the convention of
// the API they describe. The names are the library's own, not the API's: a
// tricount's sharing token is public_token here and public_identifier_token
// on the wire, because the domain model deliberately differs from it.
func TestPublicTypesMarshalSnakeCase(t *testing.T) {
	amount := MustParseAmount("10.00", "EUR")

	t.Run("Tricount", func(t *testing.T) {
		assertKeys(t, &Tricount{Currency: "EUR", Created: time.Now()},
			"id", "uuid", "title", "description", "currency", "emoji",
			"category", "status", "created", "public_token", "members",
			"former_members", "transactions", "gallery", "linked_member_uuid")
	})

	t.Run("Member", func(t *testing.T) {
		assertKeys(t, &Member{}, "id", "uuid", "display_name", "status")
	})

	t.Run("Transaction", func(t *testing.T) {
		assertKeys(t, &Transaction{Amount: amount},
			"id", "uuid", "date", "description", "kind", "status", "amount",
			"local_amount", "exchange_rate", "payer_uuid", "allocations",
			"category", "category_custom", "attachment_ids")
	})

	t.Run("Allocation", func(t *testing.T) {
		assertKeys(t, Allocation{Amount: amount},
			"member_uuid", "amount", "local_amount", "type", "share_ratio")
	})

	t.Run("GalleryAttachment", func(t *testing.T) {
		assertKeys(t, &GalleryAttachment{},
			"attachment_id", "uuid", "content_type", "original_url", "uploader_uuid")
	})

	t.Run("Settlement", func(t *testing.T) {
		assertKeys(t, &Settlement{}, "id", "items")
	})

	t.Run("SettlementItem", func(t *testing.T) {
		assertKeys(t, SettlementItem{Amount: amount},
			"payer_uuid", "receiver_uuid", "amount", "status")
	})

	t.Run("Transfer", func(t *testing.T) {
		assertKeys(t, Transfer{Amount: amount}, "from", "to", "amount")
	})

	t.Run("SyncResult", func(t *testing.T) {
		assertKeys(t, &SyncResult{}, "active", "archived")
	})

	t.Run("Error", func(t *testing.T) {
		assertKeys(t, &Error{}, "status_code", "description", "response_id", "retry_after")
	})
}

// Amount already marshalled as the API writes it, and keeps doing so.
func TestAmountMarshalsAsValueAndCurrency(t *testing.T) {
	assertKeys(t, MustParseAmount("1.00", "EUR"), "value", "currency")
}
