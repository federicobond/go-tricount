package tricount

import (
	"context"
	"fmt"
	"net/http"
)

// PaymentStatus is the state of one settlement payment.
type PaymentStatus string

const (
	PaymentPending PaymentStatus = "PENDING"
	PaymentSettled PaymentStatus = "SETTLED"
)

// SettlementItem is one payment in a server-side settlement plan.
type SettlementItem struct {
	PayerUUID    string
	ReceiverUUID string
	Amount       Amount
	Status       PaymentStatus
}

// Settlement is a server-side settlement plan.
type Settlement struct {
	ID    int64
	Items []SettlementItem
}

// CreateSettlement asks the server to compute a settlement plan.
//
// It does not work. Probed against the real API on 2026-09-12, POST
// /registry-settlement returns 404 "Route not found." — the route is absent
// rather than forbidden, so this is not a permissions problem and a bunq
// account would not help. Use Settle, which computes an equivalent plan
// locally from the transaction list.
//
// The method is kept because it documents the shape the reverse-engineered
// reference described, and so a future change in the API gets noticed rather
// than assumed. TestLiveSettlementProbe re-checks it on every live run.
func (c *Client) CreateSettlement(ctx context.Context, t *Tricount) (int64, error) {
	if err := checkTricount(t); err != nil {
		return 0, err
	}
	body, err := c.do(ctx, request{
		method:   http.MethodPost,
		userPath: fmt.Sprintf("/registry/%d/registry-settlement", t.ID),
		body:     map[string]any{},
	})
	if err != nil {
		return 0, err
	}
	return decodeID(body)
}

// GetSettlement reads a settlement plan by ID.
//
// Untestable in practice: no settlement can be created, because
// CreateSettlement's route does not exist. See CreateSettlement.
func (c *Client) GetSettlement(ctx context.Context, t *Tricount, id int64) (*Settlement, error) {
	if err := checkTricount(t); err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, fmt.Errorf("%w: settlement id is zero", ErrInvalidRequest)
	}
	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		userPath: fmt.Sprintf("/registry/%d/registry-settlement/%d", t.ID, id),
	})
	if err != nil {
		return nil, err
	}
	wires, err := decodeEnvelope[wireSettlement](body, "RegistrySettlement")
	if err != nil {
		return nil, err
	}
	if len(wires) == 0 {
		return nil, fmt.Errorf("settlement %d: %w", id, ErrNotFound)
	}
	s := wires[0].toDomain()
	if s.ID == 0 {
		s.ID = id
	}
	return s, nil
}

type wireSettlementItem struct {
	Amount                 Amount `json:"amount"`
	MembershipUUIDPayer    string `json:"membership_uuid_payer"`
	MembershipUUIDReceiver string `json:"membership_uuid_receiver"`
	Status                 string `json:"status"`
}

type wireSettlement struct {
	ID                int64                `json:"id"`
	AllSettlementItem []wireSettlementItem `json:"all_settlement_item"`
}

func (w wireSettlement) toDomain() *Settlement {
	s := &Settlement{ID: w.ID}
	for _, i := range w.AllSettlementItem {
		s.Items = append(s.Items, SettlementItem{
			PayerUUID:    i.MembershipUUIDPayer,
			ReceiverUUID: i.MembershipUUIDReceiver,
			Amount:       i.Amount.Abs(),
			Status:       PaymentStatus(i.Status),
		})
	}
	return s
}
