package tricount

// This file holds the API's JSON shapes and their conversion to the domain
// types. Every quirk of the API lives here and nowhere else:
//
//   - list elements are wrapped in a single-key object, for example
//     {"RegistryMembershipNonUser": {...}};
//   - expenses carry negative amounts, while the domain is always positive;
//   - responses nest a member under "membership" while requests send a flat
//     "membership_uuid";
//   - aliases use display_name/pointer on responses and name/type/value on
//     requests.

// wireRegistry is a Registry as the API returns it.
type wireRegistry struct {
	ID                    int64   `json:"id"`
	UUID                  string  `json:"uuid"`
	Title                 string  `json:"title"`
	Description           string  `json:"description"`
	Currency              string  `json:"currency"`
	Emoji                 *string `json:"emoji"`
	Category              string  `json:"category"`
	Status                string  `json:"status"`
	Created               apiTime `json:"created"`
	PublicIdentifierToken string  `json:"public_identifier_token"`
	MembershipUUIDActive  *string `json:"membership_uuid_active"`

	Memberships                  []map[string]wireMembership        `json:"memberships"`
	AllRegistryEntry             []map[string]wireEntry             `json:"all_registry_entry"`
	AllRegistryGalleryAttachment []map[string]wireGalleryAttachment `json:"all_registry_gallery_attachment"`
}

func (w wireRegistry) toDomain() (*Tricount, error) {
	t := &Tricount{
		ID:          w.ID,
		UUID:        w.UUID,
		Title:       w.Title,
		Description: w.Description,
		Currency:    w.Currency,
		Category:    Category(w.Category),
		Status:      TricountStatus(w.Status),
		Created:     w.Created.Time,
		PublicToken: w.PublicIdentifierToken,
	}
	if w.Emoji != nil {
		t.Emoji = *w.Emoji
	}
	if w.MembershipUUIDActive != nil {
		t.LinkedMemberUUID = *w.MembershipUUIDActive
	}

	for _, wrapper := range w.Memberships {
		m, ok := firstValue(wrapper)
		if !ok {
			continue
		}
		member := m.toDomain()
		// The API retains deleted memberships rather than removing them.
		// Keeping them out of Members stops them being resent or split
		// against, while FormerMembers keeps their history resolvable.
		if member.Status == MemberActive {
			t.Members = append(t.Members, member)
		} else {
			t.FormerMembers = append(t.FormerMembers, member)
		}
	}
	for _, wrapper := range w.AllRegistryEntry {
		e, ok := firstValue(wrapper)
		if !ok {
			continue
		}
		tx, err := e.toDomain()
		if err != nil {
			return nil, err
		}
		t.Transactions = append(t.Transactions, tx)
	}
	for _, wrapper := range w.AllRegistryGalleryAttachment {
		g, ok := firstValue(wrapper)
		if !ok {
			continue
		}
		t.Gallery = append(t.Gallery, g.toDomain())
	}
	return t, nil
}

// wireAliasResponse is the alias shape the API returns.
type wireAliasResponse struct {
	DisplayName string `json:"display_name"`
	Pointer     struct {
		Type  string `json:"type"`
		Value string `json:"value"`
		Name  string `json:"name"`
	} `json:"pointer"`
}

type wireMembership struct {
	ID     int64             `json:"id"`
	UUID   string            `json:"uuid"`
	Status string            `json:"status"`
	Alias  wireAliasResponse `json:"alias"`
}

func (w wireMembership) toDomain() *Member {
	name := w.Alias.DisplayName
	if name == "" {
		name = w.Alias.Pointer.Name
	}
	status := MemberStatus(w.Status)
	if status == "" {
		status = MemberActive
	}
	return &Member{ID: w.ID, UUID: w.UUID, DisplayName: name, Status: status}
}

// wireMembershipRef is the nested membership reference that appears inside
// entries and allocations. Only the UUID matters to us.
type wireMembershipRef struct {
	UUID string `json:"uuid"`
}

type wireAllocation struct {
	Amount      Amount  `json:"amount"`
	AmountLocal *Amount `json:"amount_local"`
	Type        string  `json:"type"`
	ShareRatio  *int    `json:"share_ratio"`

	// MembershipUUID is the request form.
	MembershipUUID string `json:"membership_uuid"`
	// Membership is the response form: a single-key wrapper object.
	Membership map[string]wireMembershipRef `json:"membership"`
}

func (w wireAllocation) memberUUID() string {
	if w.MembershipUUID != "" {
		return w.MembershipUUID
	}
	if ref, ok := firstValue(w.Membership); ok {
		return ref.UUID
	}
	return ""
}

func (w wireAllocation) toDomain() Allocation {
	a := Allocation{
		MemberUUID: w.memberUUID(),
		Amount:     w.Amount.Abs(),
		Type:       AllocationType(w.Type),
	}
	if a.Type == "" {
		a.Type = AllocationAmount
	}
	if w.AmountLocal != nil {
		local := w.AmountLocal.Abs()
		a.LocalAmount = &local
	}
	if w.ShareRatio != nil {
		a.ShareRatio = *w.ShareRatio
	}
	return a
}

type wireAttachmentRef struct {
	ID int64 `json:"id"`
}

type wireEntry struct {
	ID              int64   `json:"id"`
	UUID            string  `json:"uuid"`
	Date            apiTime `json:"date"`
	Description     string  `json:"description"`
	TypeTransaction string  `json:"type_transaction"`
	Status          string  `json:"status"`
	Amount          Amount  `json:"amount"`
	AmountLocal     *Amount `json:"amount_local"`
	ExchangeRate    string  `json:"exchange_rate"`
	Category        string  `json:"category"`
	CategoryCustom  string  `json:"category_custom"`

	// MembershipUUIDOwner is the request form.
	MembershipUUIDOwner string `json:"membership_uuid_owner"`
	// MembershipOwned is the response form: a single-key wrapper object.
	MembershipOwned map[string]wireMembershipRef `json:"membership_owned"`

	Allocations []wireAllocation    `json:"allocations"`
	Attachment  []wireAttachmentRef `json:"attachment"`
}

func (w wireEntry) payerUUID() string {
	if w.MembershipUUIDOwner != "" {
		return w.MembershipUUIDOwner
	}
	if ref, ok := firstValue(w.MembershipOwned); ok {
		return ref.UUID
	}
	return ""
}

func (w wireEntry) toDomain() (*Transaction, error) {
	kind := TransactionKind(w.TypeTransaction)
	if kind == "" {
		kind = KindExpense
	}
	status := TransactionStatus(w.Status)
	if status == "" {
		status = TransactionActive
	}

	tx := &Transaction{
		ID:             w.ID,
		UUID:           w.UUID,
		Date:           w.Date.Time,
		Description:    w.Description,
		Kind:           kind,
		Status:         status,
		Amount:         w.Amount.Abs(),
		ExchangeRate:   w.ExchangeRate,
		PayerUUID:      w.payerUUID(),
		Category:       Category(w.Category),
		CategoryCustom: w.CategoryCustom,
	}
	if w.AmountLocal != nil {
		local := w.AmountLocal.Abs()
		tx.LocalAmount = &local
	}
	for _, a := range w.Allocations {
		tx.Allocations = append(tx.Allocations, a.toDomain())
	}
	for _, att := range w.Attachment {
		tx.AttachmentIDs = append(tx.AttachmentIDs, att.ID)
	}
	return tx, nil
}

type wireGalleryAttachment struct {
	MembershipUUID string `json:"membership_uuid"`
	Attachment     struct {
		ID          int64  `json:"id"`
		UUID        string `json:"uuid"`
		ContentType string `json:"content_type"`
		URLs        []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"urls"`
	} `json:"attachment"`
}

func (w wireGalleryAttachment) toDomain() *GalleryAttachment {
	g := &GalleryAttachment{
		AttachmentID: w.Attachment.ID,
		UUID:         w.Attachment.UUID,
		ContentType:  w.Attachment.ContentType,
		UploaderUUID: w.MembershipUUID,
	}
	for _, u := range w.Attachment.URLs {
		if u.Type == "ORIGINAL" {
			g.OriginalURL = u.URL
			break
		}
	}
	return g
}

// wireSyncRef names a tricount by its sharing token in a synchronization
// request.
type wireSyncRef struct {
	PublicIdentifierToken string `json:"public_identifier_token"`
}

// wireSyncRequest is the registry-synchronization body. All three arrays must
// be present, even when empty.
type wireSyncRequest struct {
	AllRegistryActive   []wireSyncRef `json:"all_registry_active"`
	AllRegistryArchived []wireSyncRef `json:"all_registry_archived"`
	AllRegistryDeleted  []wireSyncRef `json:"all_registry_deleted"`
}

// wireSyncResponse is the registry-synchronization reply. Unlike every other
// endpoint, the registries inside it are bare rather than wrapped in a
// {"Registry": ...} object.
type wireSyncResponse struct {
	AllRegistryActive   []wireRegistry `json:"all_registry_active"`
	AllRegistryArchived []wireRegistry `json:"all_registry_archived"`
}

func wireRegistriesToDomain(ws []wireRegistry) ([]*Tricount, error) {
	out := make([]*Tricount, 0, len(ws))
	for _, w := range ws {
		t, err := w.toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// wireAllocationRequest is an allocation as the API accepts it: a flat
// membership_uuid, not the nested membership object it returns.
type wireAllocationRequest struct {
	MembershipUUID string  `json:"membership_uuid"`
	Amount         Amount  `json:"amount"`
	AmountLocal    *Amount `json:"amount_local,omitempty"`
	Type           string  `json:"type"`
	ShareRatio     *int    `json:"share_ratio,omitempty"`
}

// wireEntryRequest is a registry-entry body. The API's own "type" field is
// deliberately absent: sending it is rejected as superfluous.
type wireEntryRequest struct {
	UUID                string                  `json:"uuid"`
	Description         string                  `json:"description"`
	Amount              Amount                  `json:"amount"`
	AmountLocal         *Amount                 `json:"amount_local,omitempty"`
	ExchangeRate        string                  `json:"exchange_rate,omitempty"`
	MembershipUUIDOwner string                  `json:"membership_uuid_owner"`
	Allocations         []wireAllocationRequest `json:"allocations"`
	TypeTransaction     string                  `json:"type_transaction"`
	Status              string                  `json:"status"`
	Date                apiTime                 `json:"date"`
	Category            string                  `json:"category,omitempty"`
	CategoryCustom      string                  `json:"category_custom,omitempty"`
	Attachment          []wireAttachmentRef     `json:"attachment,omitempty"`
}
