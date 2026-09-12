package tricount

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestCreateSettlement(t *testing.T) {
	var method, path string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.Write([]byte(`{"Response":[{"Id":{"id":555}}]}`))
	})
	tri := entryTestTricount()

	id, err := c.CreateSettlement(context.Background(), tri)
	if err != nil {
		t.Fatalf("CreateSettlement: %v", err)
	}
	if id != 555 {
		t.Errorf("id = %d", id)
	}
	if method != http.MethodPost {
		t.Errorf("method = %s, want POST", method)
	}
	if path != "/v1/user/79290957/registry/1/registry-settlement" {
		t.Errorf("path = %q", path)
	}
}

func TestCreateSettlementSurfacesNotFound(t *testing.T) {
	// The endpoint is reported to 404. The error must reach the caller as
	// ErrNotFound rather than being swallowed.
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"Error":[{"error_description":"Not found"}]}`))
	})
	tri := entryTestTricount()
	if _, err := c.CreateSettlement(context.Background(), tri); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetSettlement(t *testing.T) {
	var path string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Write([]byte(`{"Response":[{"RegistrySettlement":{
			"id":555,
			"all_settlement_item":[
				{"amount":{"value":"15.00","currency":"EUR"},
				 "membership_uuid_payer":"u-bob",
				 "membership_uuid_receiver":"u-alice",
				 "status":"PENDING"}
			]
		}}]}`))
	})
	tri := entryTestTricount()

	s, err := c.GetSettlement(context.Background(), tri, 555)
	if err != nil {
		t.Fatalf("GetSettlement: %v", err)
	}
	if path != "/v1/user/79290957/registry/1/registry-settlement/555" {
		t.Errorf("path = %q", path)
	}
	if s.ID != 555 {
		t.Errorf("ID = %d", s.ID)
	}
	if len(s.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(s.Items))
	}
	item := s.Items[0]
	if item.PayerUUID != "u-bob" || item.ReceiverUUID != "u-alice" {
		t.Errorf("item = %+v", item)
	}
	if got := item.Amount.String(); got != "15.00" {
		t.Errorf("amount = %q, want \"15.00\"", got)
	}
	if item.Status != PaymentPending {
		t.Errorf("status = %q, want PENDING", item.Status)
	}
}

func TestSettlementValidation(t *testing.T) {
	var called bool
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	ctx := context.Background()
	if _, err := c.CreateSettlement(ctx, nil); !errors.Is(err, ErrInvalidRequest) {
		t.Error("a nil tricount should be rejected")
	}
	if _, err := c.GetSettlement(ctx, entryTestTricount(), 0); !errors.Is(err, ErrInvalidRequest) {
		t.Error("a zero settlement id should be rejected")
	}
	if called {
		t.Error("validation failures must not reach the network")
	}
}
