package tricount

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Expense is money one member spent, divided between members.
//
// Amount is positive: the API's negative-for-expenses convention is applied
// internally. A zero Date means now.
type Expense struct {
	// UUID, if set, makes the create idempotent; see CreateExpense.
	UUID        string
	Description string
	Amount      Amount
	// Payer is the member who paid.
	Payer *Member
	Split Split
	Date  time.Time

	Category Category
	// CategoryCustom is a free-form label with an emoji, such as
	// "Coffee ☕️". Setting it forces Category to OTHER, which is what the
	// API requires.
	CategoryCustom string
	AttachmentIDs  []int64

	// LocalAmount is the total in the currency actually spent, when that
	// differs from the tricount's. Amount stays in the tricount's currency.
	LocalAmount *Amount
	// ExchangeRate converts LocalAmount to Amount. Left empty, it is fetched.
	ExchangeRate string
}

// Income is money a member received on behalf of the group, divided between
// members. Amounts are positive and a zero Date means now.
type Income struct {
	// UUID, if set, makes the create idempotent; see CreateExpense.
	UUID        string
	Description string
	Amount      Amount
	// Receiver is the member who received the money.
	Receiver *Member
	Split    Split
	Date     time.Time

	Category       Category
	CategoryCustom string
	AttachmentIDs  []int64

	LocalAmount  *Amount
	ExchangeRate string
}

// Reimbursement is a direct transfer between two members that settles part of
// a debt. It has no split: the whole amount moves from From to To.
type Reimbursement struct {
	// UUID, if set, makes the create idempotent; see CreateExpense.
	UUID        string
	Description string
	Amount      Amount
	// From is the member paying, To the member being paid.
	From *Member
	To   *Member
	Date time.Time
}

// entrySpec is the one internal shape all three kinds reduce to.
type entrySpec struct {
	kind        TransactionKind
	uuid        string
	description string
	amount      Amount
	payer       *Member
	split       Split
	date        time.Time

	category       Category
	categoryCustom string
	attachmentIDs  []int64

	localAmount  *Amount
	exchangeRate string

	// allocations, when set, replace the split. Reimbursements use it.
	allocations []Allocation
}

func (e Expense) spec() entrySpec {
	return entrySpec{
		kind: KindExpense, uuid: e.UUID, description: e.Description, amount: e.Amount,
		payer: e.Payer, split: e.Split, date: e.Date,
		category: e.Category, categoryCustom: e.CategoryCustom,
		attachmentIDs: e.AttachmentIDs,
		localAmount:   e.LocalAmount, exchangeRate: e.ExchangeRate,
	}
}

func (i Income) spec() entrySpec {
	return entrySpec{
		kind: KindIncome, uuid: i.UUID, description: i.Description, amount: i.Amount,
		payer: i.Receiver, split: i.Split, date: i.Date,
		category: i.Category, categoryCustom: i.CategoryCustom,
		attachmentIDs: i.AttachmentIDs,
		localAmount:   i.LocalAmount, exchangeRate: i.ExchangeRate,
	}
}

// CreateExpense records an expense and returns its ID.
//
// Setting Expense.UUID makes the create idempotent: the API keeps the UUID it
// is sent, and a repeat returns the original entry's ID rather than a
// duplicate. The first write wins — a repeat with different content is
// ignored, not applied, so noticing a changed amount means reading and
// comparing. Undocumented; asserted by TestLiveEntryUUIDIsIdempotencyKey.
func (c *Client) CreateExpense(ctx context.Context, t *Tricount, e Expense) (int64, error) {
	return c.createEntry(ctx, t, e.spec())
}

// CreateIncome records money received on the group's behalf and returns its ID.
func (c *Client) CreateIncome(ctx context.Context, t *Tricount, i Income) (int64, error) {
	return c.createEntry(ctx, t, i.spec())
}

// CreateReimbursement records a direct transfer between two members and
// returns its ID.
func (c *Client) CreateReimbursement(ctx context.Context, t *Tricount, r Reimbursement) (int64, error) {
	spec, err := reimbursementSpec(t, r)
	if err != nil {
		return 0, err
	}
	return c.createEntry(ctx, t, spec)
}

func reimbursementSpec(t *Tricount, r Reimbursement) (entrySpec, error) {
	if err := checkTricount(t); err != nil {
		return entrySpec{}, err
	}
	if err := checkMember(t, r.From); err != nil {
		return entrySpec{}, fmt.Errorf("reimbursement payer: %w", err)
	}
	if err := checkMember(t, r.To); err != nil {
		return entrySpec{}, fmt.Errorf("reimbursement recipient: %w", err)
	}
	if r.From.UUID == r.To.UUID {
		return entrySpec{}, fmt.Errorf("%w: a reimbursement cannot pay %q to themselves",
			ErrInvalidRequest, r.From.DisplayName)
	}
	description := r.Description
	if description == "" {
		description = "Reimbursement"
	}
	// The API's shape for a transfer: the recipient's allocation takes the
	// whole amount and the payer's takes zero.
	return entrySpec{
		kind:        KindReimbursement,
		uuid:        r.UUID,
		description: description,
		amount:      r.Amount,
		payer:       r.From,
		date:        r.Date,
		allocations: []Allocation{
			{MemberUUID: r.To.UUID, Amount: r.Amount, Type: AllocationAmount},
			{MemberUUID: r.From.UUID, Amount: ZeroAmount(r.Amount.Currency()), Type: AllocationAmount},
		},
	}, nil
}

func (c *Client) createEntry(ctx context.Context, t *Tricount, spec entrySpec) (int64, error) {
	body, err := c.buildEntry(ctx, t, spec)
	if err != nil {
		return 0, err
	}
	raw, err := c.do(ctx, request{
		method:   http.MethodPost,
		userPath: fmt.Sprintf("/registry/%d/registry-entry", t.ID),
		body:     body,
	})
	if err != nil {
		return 0, err
	}
	return decodeID(raw)
}

// buildEntry validates a spec and renders it as a request body. Everything
// checkable locally is checked here, because the API's own rejections are
// opaque strings like "superfluous field".
func (c *Client) buildEntry(ctx context.Context, t *Tricount, spec entrySpec) (*wireEntryRequest, error) {
	if err := checkTricount(t); err != nil {
		return nil, err
	}
	if spec.description == "" {
		return nil, fmt.Errorf("%w: transaction description is empty", ErrInvalidRequest)
	}
	if err := checkMember(t, spec.payer); err != nil {
		return nil, fmt.Errorf("transaction payer: %w", err)
	}
	if spec.amount.Sign() <= 0 {
		return nil, fmt.Errorf("%w: transaction amount is %s; amounts are always positive",
			ErrInvalidRequest, spec.amount)
	}
	if spec.amount.Currency() != t.Currency {
		return nil, fmt.Errorf("%w: amount is in %s but tricount %d is in %s",
			ErrInvalidRequest, spec.amount.Currency(), t.ID, t.Currency)
	}

	allocations := spec.allocations
	if allocations == nil {
		var err error
		if allocations, err = spec.split.resolve(t, spec.amount); err != nil {
			return nil, err
		}
	}

	// Foreign currency: mirror the split into the local currency so each side
	// sums exactly to its own total.
	var localAllocations []Amount
	exchangeRate := spec.exchangeRate
	if spec.localAmount != nil {
		local := *spec.localAmount
		if local.Currency() == "" {
			return nil, fmt.Errorf("%w: local amount has no currency", ErrInvalidRequest)
		}
		if local.Sign() <= 0 {
			return nil, fmt.Errorf("%w: local amount is %s; amounts are always positive",
				ErrInvalidRequest, local)
		}
		if exchangeRate == "" {
			var err error
			exchangeRate, err = c.ExchangeRate(ctx, local.Currency(), t.Currency)
			if err != nil {
				return nil, fmt.Errorf("fetching the %s to %s rate: %w",
					local.Currency(), t.Currency, err)
			}
		}
		var err error
		if spec.allocations != nil {
			// Explicit allocations (reimbursements) are mirrored in proportion
			// to their own amounts.
			localAllocations, err = weightedLocal(local, allocations)
		} else {
			localAllocations, err = spec.split.resolveLocal(allocations, local)
		}
		if err != nil {
			return nil, err
		}
	}

	date := spec.date
	if date.IsZero() {
		date = time.Now()
	}

	uuid := spec.uuid
	if uuid == "" {
		generated, err := newUUID()
		if err != nil {
			return nil, err
		}
		uuid = generated
	} else if !validUUID(uuid) {
		return nil, fmt.Errorf("%w: entry UUID %q is not a UUID", ErrInvalidRequest, uuid)
	}

	// Expenses are stored negative; income and reimbursements positive.
	negate := spec.kind == KindExpense

	body := &wireEntryRequest{
		UUID:                uuid,
		Description:         spec.description,
		Amount:              signed(spec.amount, negate),
		MembershipUUIDOwner: spec.payer.UUID,
		TypeTransaction:     string(spec.kind),
		Status:              string(TransactionActive),
		Date:                apiTime{date},
	}
	if spec.localAmount != nil {
		local := signed(*spec.localAmount, negate)
		body.AmountLocal = &local
		body.ExchangeRate = exchangeRate
	}
	switch {
	case spec.categoryCustom != "":
		// A custom label requires the standard category to be OTHER.
		body.Category = string(CategoryOther)
		body.CategoryCustom = spec.categoryCustom
	case spec.category != "":
		body.Category = string(spec.category)
	}
	for _, id := range spec.attachmentIDs {
		body.Attachment = append(body.Attachment, wireAttachmentRef{ID: id})
	}

	for i, a := range allocations {
		wa := wireAllocationRequest{
			MembershipUUID: a.MemberUUID,
			Amount:         signed(a.Amount, negate),
			Type:           string(a.Type),
		}
		if a.Type == AllocationRatio {
			ratio := a.ShareRatio
			wa.ShareRatio = &ratio
		}
		if localAllocations != nil {
			local := signed(localAllocations[i], negate)
			wa.AmountLocal = &local
		}
		body.Allocations = append(body.Allocations, wa)
	}
	return body, nil
}

func signed(a Amount, negate bool) Amount {
	if negate {
		return a.Neg()
	}
	return a
}

// UpdateExpense replaces an existing expense.
//
// This is a full replacement, not a patch: the API requires every field in the
// body, so anything omitted from e is cleared. The usual pattern is to read
// the transaction from t.Transactions, change what you want, and write the
// whole thing back.
func (c *Client) UpdateExpense(ctx context.Context, t *Tricount, id int64, e Expense) error {
	return c.updateEntry(ctx, t, id, e.spec())
}

// UpdateIncome replaces an existing income entry. Like UpdateExpense, it is a
// full replacement.
func (c *Client) UpdateIncome(ctx context.Context, t *Tricount, id int64, i Income) error {
	return c.updateEntry(ctx, t, id, i.spec())
}

// UpdateReimbursement replaces an existing reimbursement. Like UpdateExpense,
// it is a full replacement.
func (c *Client) UpdateReimbursement(ctx context.Context, t *Tricount, id int64, r Reimbursement) error {
	spec, err := reimbursementSpec(t, r)
	if err != nil {
		return err
	}
	return c.updateEntry(ctx, t, id, spec)
}

// DeleteTransaction removes a transaction.
func (c *Client) DeleteTransaction(ctx context.Context, t *Tricount, id int64) error {
	if err := checkTricount(t); err != nil {
		return err
	}
	if id == 0 {
		return fmt.Errorf("%w: transaction id is zero", ErrInvalidRequest)
	}
	_, err := c.do(ctx, request{
		method:   http.MethodDelete,
		userPath: fmt.Sprintf("/registry/%d/registry-entry/%d", t.ID, id),
	})
	return err
}

func (c *Client) updateEntry(ctx context.Context, t *Tricount, id int64, spec entrySpec) error {
	if err := checkTricount(t); err != nil {
		return err
	}
	if id == 0 {
		return fmt.Errorf("%w: transaction id is zero", ErrInvalidRequest)
	}
	if spec.uuid != "" {
		return fmt.Errorf("%w: UUID is set only when creating an entry, not when updating one",
			ErrInvalidRequest)
	}
	body, err := c.buildEntry(ctx, t, spec)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, request{
		method:   http.MethodPut,
		userPath: fmt.Sprintf("/registry/%d/registry-entry/%d", t.ID, id),
		body:     body,
	})
	return err
}

// specFromTransaction rebuilds a request spec from a transaction that was read
// back from the API, so a caller can change one field without restating the
// rest. It is how attachment association works, since the API has no
// dedicated endpoint for that.
func specFromTransaction(t *Tricount, tx *Transaction) (entrySpec, error) {
	if err := checkTricount(t); err != nil {
		return entrySpec{}, err
	}
	if tx == nil {
		return entrySpec{}, fmt.Errorf("%w: transaction is nil", ErrInvalidRequest)
	}
	payer := t.MemberByUUID(tx.PayerUUID)
	if payer == nil {
		return entrySpec{}, fmt.Errorf("%w: transaction %d names payer %q, who is not in tricount %d",
			ErrInvalidRequest, tx.ID, tx.PayerUUID, t.ID)
	}
	for _, a := range tx.Allocations {
		if t.MemberByUUID(a.MemberUUID) == nil {
			return entrySpec{}, fmt.Errorf("%w: transaction %d allocates to %q, who is not in tricount %d",
				ErrInvalidRequest, tx.ID, a.MemberUUID, t.ID)
		}
	}

	allocations := make([]Allocation, len(tx.Allocations))
	copy(allocations, tx.Allocations)

	spec := entrySpec{
		kind:           tx.Kind,
		description:    tx.Description,
		amount:         tx.Amount,
		payer:          payer,
		date:           tx.Date,
		category:       tx.Category,
		categoryCustom: tx.CategoryCustom,
		allocations:    allocations,
		localAmount:    tx.LocalAmount,
		exchangeRate:   tx.ExchangeRate,
	}
	spec.attachmentIDs = append(spec.attachmentIDs, tx.AttachmentIDs...)
	return spec, nil
}
