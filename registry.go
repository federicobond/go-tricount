package tricount

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// GetTricount fetches a tricount by its public sharing token — the tXXXX part
// of a tricount.com/tXXXX link — without syncing it to this device. The
// result is readable but not writable: call JoinTricount before mutating
// anything.
func (c *Client) GetTricount(ctx context.Context, publicToken string) (*Tricount, error) {
	if publicToken == "" {
		return nil, fmt.Errorf("%w: empty public token", ErrInvalidRequest)
	}
	found, err := c.fetchRegistries(ctx, url.Values{"public_identifier_token": {publicToken}})
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("tricount with token %q: %w", publicToken, ErrNotFound)
	}
	return found[0], nil
}

// GetTricountByID fetches a tricount this device already has access to.
func (c *Client) GetTricountByID(ctx context.Context, id int64) (*Tricount, error) {
	if id == 0 {
		return nil, fmt.Errorf("%w: tricount id is zero", ErrInvalidRequest)
	}
	found, err := c.fetchRegistries(ctx, url.Values{"registry_id": {strconv.FormatInt(id, 10)}})
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("tricount %d: %w", id, ErrNotFound)
	}
	return found[0], nil
}

// ListTricounts returns every tricount synced to this device, active and
// archived alike.
func (c *Client) ListTricounts(ctx context.Context) ([]*Tricount, error) {
	return c.fetchRegistries(ctx, nil)
}

func (c *Client) fetchRegistries(ctx context.Context, query url.Values) ([]*Tricount, error) {
	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		userPath: "/registry",
		query:    query,
	})
	if err != nil {
		return nil, err
	}
	wires, err := decodeEnvelope[wireRegistry](body, "Registry")
	if err != nil {
		return nil, err
	}
	return wireRegistriesToDomain(wires)
}

// TricountUpdate describes a change to a tricount's metadata. A nil field is
// left alone.
//
// Currency and Description are absent on purpose: both can only be set when
// the tricount is created. The API rejects a currency change outright and
// accepts a description change while silently discarding it.
type TricountUpdate struct {
	Title    *string
	Emoji    *string
	Category *Category
}

// CreateTricount creates a tricount and returns its ID. Currency and
// description cannot be changed afterwards, so get them right here.
func (c *Client) CreateTricount(ctx context.Context, title, currency, description string) (int64, error) {
	if title == "" {
		return 0, fmt.Errorf("%w: tricount title is empty", ErrInvalidRequest)
	}
	if currency == "" {
		return 0, fmt.Errorf("%w: tricount currency is empty", ErrInvalidRequest)
	}
	body, err := c.do(ctx, request{
		method:   http.MethodPost,
		userPath: "/registry",
		body: map[string]string{
			"title":       title,
			"currency":    currency,
			"description": description,
		},
	})
	if err != nil {
		return 0, err
	}
	return decodeID(body)
}

// UpdateTricount changes a tricount's title, emoji or category. Fields left
// nil are untouched, and an update with nothing set makes no request. On
// success the fields are refreshed on t.
func (c *Client) UpdateTricount(ctx context.Context, t *Tricount, upd TricountUpdate) error {
	if err := checkTricount(t); err != nil {
		return err
	}
	body := map[string]any{}
	if upd.Title != nil {
		if *upd.Title == "" {
			return fmt.Errorf("%w: tricount title cannot be set to empty", ErrInvalidRequest)
		}
		body["title"] = *upd.Title
	}
	if upd.Emoji != nil {
		body["emoji"] = *upd.Emoji
	}
	if upd.Category != nil {
		body["category"] = string(*upd.Category)
	}
	if len(body) == 0 {
		return nil
	}
	if err := c.putRegistry(ctx, t, body); err != nil {
		return err
	}
	if upd.Title != nil {
		t.Title = *upd.Title
	}
	if upd.Emoji != nil {
		t.Emoji = *upd.Emoji
	}
	if upd.Category != nil {
		t.Category = *upd.Category
	}
	return nil
}

// ArchiveTricount makes a tricount read-only.
func (c *Client) ArchiveTricount(ctx context.Context, t *Tricount) error {
	return c.setStatus(ctx, t, StatusArchived)
}

// UnarchiveTricount restores an archived tricount to writable.
func (c *Client) UnarchiveTricount(ctx context.Context, t *Tricount) error {
	return c.setStatus(ctx, t, StatusActive)
}

func (c *Client) setStatus(ctx context.Context, t *Tricount, status TricountStatus) error {
	if err := checkTricount(t); err != nil {
		return err
	}
	if err := c.putRegistry(ctx, t, map[string]any{"status": string(status)}); err != nil {
		return err
	}
	t.Status = status
	return nil
}

// DeleteTricount permanently deletes the tricount for everyone who has it,
// not just this device. To stop following a tricount without destroying it,
// use LeaveTricount.
func (c *Client) DeleteTricount(ctx context.Context, t *Tricount) error {
	if err := checkTricount(t); err != nil {
		return err
	}
	_, err := c.do(ctx, request{
		method:   http.MethodDelete,
		userPath: fmt.Sprintf("/registry/%d", t.ID),
	})
	return err
}

// putRegistry sends a PUT to the registry endpoint, which is the one the API
// uses for tricount metadata, status and every membership change.
func (c *Client) putRegistry(ctx context.Context, t *Tricount, body map[string]any) error {
	_, err := c.do(ctx, request{
		method:   http.MethodPut,
		userPath: fmt.Sprintf("/registry/%d", t.ID),
		body:     body,
	})
	return err
}

func checkTricount(t *Tricount) error {
	if t == nil {
		return fmt.Errorf("%w: tricount is nil", ErrInvalidRequest)
	}
	if t.ID == 0 {
		return fmt.Errorf("%w: tricount has no id", ErrInvalidRequest)
	}
	return nil
}

// JoinTricount syncs a tricount to this device by its sharing token, which is
// what makes it writable, and returns it fully populated.
//
// Anyone holding a sharing link can join; membership is not required. On
// joining, the API links this device to the member with the lowest ID — use
// LinkToMember to change that.
func (c *Client) JoinTricount(ctx context.Context, publicToken string) (*Tricount, error) {
	if publicToken == "" {
		return nil, fmt.Errorf("%w: empty public token", ErrInvalidRequest)
	}
	res, err := c.SyncTricounts(ctx, []string{publicToken}, nil)
	if err != nil {
		return nil, err
	}
	for _, t := range res.Active {
		if t.PublicToken == publicToken {
			// The synchronization response carries metadata but not
			// transactions, so read the whole thing back by ID.
			return c.GetTricountByID(ctx, t.ID)
		}
	}
	return nil, fmt.Errorf("tricount with token %q did not appear in the sync result: %w",
		publicToken, ErrNotFound)
}

// SyncTricounts sets which tricounts this device follows, by sharing token,
// and returns them partitioned into active and archived. Passing no tokens at
// all returns the current state without changing it.
func (c *Client) SyncTricounts(ctx context.Context, active, archived []string) (*SyncResult, error) {
	return c.sync(ctx, wireSyncRequest{
		AllRegistryActive:   syncRefs(active),
		AllRegistryArchived: syncRefs(archived),
		AllRegistryDeleted:  []wireSyncRef{},
	})
}

// LeaveTricount stops this device following the tricount. It does not delete
// anything for anyone else, and rejoining later with the same sharing token
// restores access. To destroy a tricount for everyone, use DeleteTricount.
func (c *Client) LeaveTricount(ctx context.Context, t *Tricount) error {
	if err := checkTricount(t); err != nil {
		return err
	}
	if t.PublicToken == "" {
		return fmt.Errorf("%w: leaving needs the tricount's public token", ErrInvalidRequest)
	}
	_, err := c.sync(ctx, wireSyncRequest{
		AllRegistryActive:   []wireSyncRef{},
		AllRegistryArchived: []wireSyncRef{},
		AllRegistryDeleted:  []wireSyncRef{{PublicIdentifierToken: t.PublicToken}},
	})
	return err
}

func (c *Client) sync(ctx context.Context, body wireSyncRequest) (*SyncResult, error) {
	raw, err := c.do(ctx, request{
		method:   http.MethodPost,
		userPath: "/registry-synchronization",
		body:     body,
	})
	if err != nil {
		return nil, err
	}
	syncs, err := decodeEnvelope[wireSyncResponse](raw, "RegistrySynchronization")
	if err != nil {
		return nil, err
	}
	res := &SyncResult{}
	for _, s := range syncs {
		activeDomain, err := wireRegistriesToDomain(s.AllRegistryActive)
		if err != nil {
			return nil, err
		}
		archivedDomain, err := wireRegistriesToDomain(s.AllRegistryArchived)
		if err != nil {
			return nil, err
		}
		res.Active = append(res.Active, activeDomain...)
		res.Archived = append(res.Archived, archivedDomain...)
	}
	return res, nil
}

func syncRefs(tokens []string) []wireSyncRef {
	refs := make([]wireSyncRef, 0, len(tokens))
	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		refs = append(refs, wireSyncRef{PublicIdentifierToken: tok})
	}
	return refs
}
