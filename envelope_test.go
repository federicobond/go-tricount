package tricount

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDecodeEnvelope(t *testing.T) {
	body := []byte(`{"Response":[
		{"Token":{"token":"sess-abc"}},
		{"UserPerson":{"id":79290957,"display_name":"tricount participant"}}
	]}`)

	type token struct {
		Token string `json:"token"`
	}
	tokens, err := decodeEnvelope[token](body, "Token")
	if err != nil {
		t.Fatalf("decodeEnvelope: %v", err)
	}
	if len(tokens) != 1 || tokens[0].Token != "sess-abc" {
		t.Fatalf("tokens = %+v", tokens)
	}

	type person struct {
		ID int64 `json:"id"`
	}
	people, err := decodeEnvelope[person](body, "UserPerson")
	if err != nil {
		t.Fatalf("decodeEnvelope: %v", err)
	}
	if len(people) != 1 || people[0].ID != 79290957 {
		t.Fatalf("people = %+v", people)
	}

	missing, err := decodeEnvelope[person](body, "Nope")
	if err != nil {
		t.Fatalf("decodeEnvelope for an absent key: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("absent key returned %d items, want 0", len(missing))
	}
}

func TestDecodeEnvelopeMultiple(t *testing.T) {
	body := []byte(`{"Response":[
		{"Registry":{"id":1}},
		{"Registry":{"id":2}},
		{"Registry":{"id":3}}
	]}`)
	type reg struct {
		ID int64 `json:"id"`
	}
	regs, err := decodeEnvelope[reg](body, "Registry")
	if err != nil {
		t.Fatalf("decodeEnvelope: %v", err)
	}
	if len(regs) != 3 || regs[0].ID != 1 || regs[2].ID != 3 {
		t.Fatalf("regs = %+v", regs)
	}
}

func TestDecodeEnvelopeMalformed(t *testing.T) {
	if _, err := decodeEnvelope[struct{}]([]byte(`not json`), "Registry"); err == nil {
		t.Error("malformed JSON should error")
	}
	type reg struct {
		ID int64 `json:"id"`
	}
	if _, err := decodeEnvelope[reg]([]byte(`{"Response":[{"Registry":{"id":"not a number"}}]}`), "Registry"); err == nil {
		t.Error("a type mismatch inside the envelope should error")
	}
}

func TestDecodeID(t *testing.T) {
	id, err := decodeID([]byte(`{"Response":[{"Id":{"id":104488759}}]}`))
	if err != nil {
		t.Fatalf("decodeID: %v", err)
	}
	if id != 104488759 {
		t.Errorf("id = %d, want 104488759", id)
	}

	if _, err := decodeID([]byte(`{"Response":[]}`)); err == nil {
		t.Error("an empty Response should error")
	}
}

func TestFirstValue(t *testing.T) {
	m := map[string]int{"RegistryMembershipNonUser": 42}
	v, ok := firstValue(m)
	if !ok || v != 42 {
		t.Errorf("firstValue = %v, %v; want 42, true", v, ok)
	}
	if _, ok := firstValue(map[string]int{}); ok {
		t.Error("firstValue on an empty map should report false")
	}
}

func TestAPITime(t *testing.T) {
	var at apiTime
	if err := json.Unmarshal([]byte(`"2026-03-01 10:57:38.087586"`), &at); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := time.Date(2026, 3, 1, 10, 57, 38, 87586000, time.UTC)
	if !at.Equal(want) {
		t.Errorf("parsed %v, want %v", at.Time, want)
	}

	out, err := json.Marshal(apiTime{want})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got, expect := string(out), `"2026-03-01 10:57:38.087586"`; got != expect {
		t.Errorf("Marshal = %s, want %s", got, expect)
	}

	var noFrac apiTime
	if err := json.Unmarshal([]byte(`"2026-03-30 14:30:00"`), &noFrac); err != nil {
		t.Fatalf("Unmarshal without fraction: %v", err)
	}
	if noFrac.Hour() != 14 || noFrac.Minute() != 30 {
		t.Errorf("parsed %v", noFrac.Time)
	}

	for _, in := range []string{`null`, `""`} {
		var zero apiTime
		if err := json.Unmarshal([]byte(in), &zero); err != nil {
			t.Errorf("Unmarshal(%s): %v", in, err)
		}
		if !zero.IsZero() {
			t.Errorf("Unmarshal(%s) should give the zero time", in)
		}
	}

	var bad apiTime
	if err := json.Unmarshal([]byte(`"30/03/2026"`), &bad); err == nil {
		t.Error("an unrecognised layout should error")
	}
}
