package tricount

import "time"

// TricountStatus is a tricount's read/write state.
type TricountStatus string

const (
	// StatusActive is a normal, writable tricount.
	StatusActive TricountStatus = "READ_WRITE"
	// StatusArchived is an archived, read-only tricount.
	StatusArchived TricountStatus = "READ_ONLY"
)

// Valid reports whether the status is one the library knows.
func (s TricountStatus) Valid() bool {
	return s == StatusActive || s == StatusArchived
}

// MemberStatus is a membership's state.
type MemberStatus string

// MemberActive is the status of a member in good standing.
const MemberActive MemberStatus = "ACTIVE"

// Valid reports whether the status is one the library knows.
func (s MemberStatus) Valid() bool { return s == MemberActive }

// TransactionStatus is a transaction's state.
type TransactionStatus string

// TransactionActive is the status of a live transaction.
const TransactionActive TransactionStatus = "ACTIVE"

// Valid reports whether the status is one the library knows.
func (s TransactionStatus) Valid() bool { return s == TransactionActive }

// TransactionKind distinguishes the three sorts of entry. It maps to the
// API's type_transaction field; the API's separate "type" field is always
// MANUAL and is never sent or exposed.
type TransactionKind string

const (
	// KindExpense is money one member spent on behalf of others.
	KindExpense TransactionKind = "NORMAL"
	// KindIncome is money a member received on behalf of others.
	KindIncome TransactionKind = "INCOME"
	// KindReimbursement is a direct transfer between two members.
	KindReimbursement TransactionKind = "BALANCE"
)

// Valid reports whether the kind is one the library knows.
func (k TransactionKind) Valid() bool {
	switch k {
	case KindExpense, KindIncome, KindReimbursement:
		return true
	}
	return false
}

// AllocationType says how an allocation's amount was arrived at.
type AllocationType string

const (
	// AllocationAmount is a fixed amount.
	AllocationAmount AllocationType = "AMOUNT"
	// AllocationRatio is a proportional share, with ShareRatio set.
	AllocationRatio AllocationType = "RATIO"
)

// Valid reports whether the type is one the library knows.
func (t AllocationType) Valid() bool {
	return t == AllocationAmount || t == AllocationRatio
}

// Category is an expense category. Values the library does not know are
// preserved verbatim when decoding, so a new server-side category does not
// break anything; Valid reports whether a value is a known one.
type Category string

// The categories the Tricount app offers.
const (
	CategoryUncategorized    Category = "UNCATEGORIZED"
	CategoryTravel           Category = "TRAVEL"
	CategoryEntertainment    Category = "ENTERTAINMENT"
	CategoryGroceries        Category = "GROCERIES"
	CategoryHealthcare       Category = "HEALTHCARE"
	CategoryInsurance        Category = "INSURANCE"
	CategoryRentAndUtilities Category = "RENT_AND_UTILITIES"
	CategoryFoodAndDrink     Category = "FOOD_AND_DRINK"
	CategoryShopping         Category = "SHOPPING"
	CategoryTransport        Category = "TRANSPORT"
	CategoryOther            Category = "OTHER"
	// CategoryGeneral appears on tricounts rather than on transactions.
	CategoryGeneral Category = "GENERAL"
)

// Valid reports whether the category is one the library knows.
func (c Category) Valid() bool {
	switch c {
	case CategoryUncategorized, CategoryTravel, CategoryEntertainment,
		CategoryGroceries, CategoryHealthcare, CategoryInsurance,
		CategoryRentAndUtilities, CategoryFoodAndDrink, CategoryShopping,
		CategoryTransport, CategoryOther, CategoryGeneral:
		return true
	}
	return false
}

// Member is one participant in a tricount.
type Member struct {
	// ID is the membership ID, needed only to delete the member.
	ID int64 `json:"id"`
	// UUID is the membership UUID, used everywhere else.
	UUID        string       `json:"uuid"`
	DisplayName string       `json:"display_name"`
	Status      MemberStatus `json:"status"`
}

// Allocation is one member's share of a transaction. Amounts are always
// positive.
type Allocation struct {
	MemberUUID string `json:"member_uuid"`
	Amount     Amount `json:"amount"`
	// LocalAmount is the share in the transaction's original currency, set
	// only on foreign-currency transactions.
	LocalAmount *Amount        `json:"local_amount"`
	Type        AllocationType `json:"type"`
	// ShareRatio is meaningful only when Type is AllocationRatio.
	ShareRatio int `json:"share_ratio"`
}

// Transaction is an expense, income or reimbursement. Amounts are always
// positive; the API's negative-for-expenses convention is confined to the
// wire layer.
type Transaction struct {
	ID          int64             `json:"id"`
	UUID        string            `json:"uuid"`
	Date        time.Time         `json:"date"`
	Description string            `json:"description"`
	Kind        TransactionKind   `json:"kind"`
	Status      TransactionStatus `json:"status"`
	Amount      Amount            `json:"amount"`
	// LocalAmount is the total in the original currency, set only on
	// foreign-currency transactions.
	LocalAmount *Amount `json:"local_amount"`
	// ExchangeRate is a decimal string, empty unless LocalAmount is set.
	ExchangeRate string `json:"exchange_rate"`
	// PayerUUID is the membership UUID of whoever paid, or received in the
	// case of income.
	PayerUUID      string       `json:"payer_uuid"`
	Allocations    []Allocation `json:"allocations"`
	Category       Category     `json:"category"`
	CategoryCustom string       `json:"category_custom"`
	AttachmentIDs  []int64      `json:"attachment_ids"`
}

// GalleryAttachment is an image attached to a tricount's gallery rather than
// to a particular transaction.
type GalleryAttachment struct {
	AttachmentID int64  `json:"attachment_id"`
	UUID         string `json:"uuid"`
	ContentType  string `json:"content_type"`
	// OriginalURL is the ORIGINAL entry of the attachment's URL list.
	OriginalURL string `json:"original_url"`
	// UploaderUUID is the membership UUID of whoever uploaded it.
	UploaderUUID string `json:"uploader_uuid"`
}

// Tricount is a shared-expense group.
type Tricount struct {
	ID    int64  `json:"id"`
	UUID  string `json:"uuid"`
	Title string `json:"title"`
	// Description can only be set when the tricount is created. The API
	// accepts updates to it and silently discards them.
	Description string `json:"description"`
	// Currency can only be set when the tricount is created. The API rejects
	// updates to it.
	Currency string         `json:"currency"`
	Emoji    string         `json:"emoji"`
	Category Category       `json:"category"`
	Status   TricountStatus `json:"status"`
	Created  time.Time      `json:"created"`
	// PublicToken is the sharing-link key, the tXXXX part of a
	// tricount.com/tXXXX URL. Anyone holding it can read and write.
	PublicToken string `json:"public_token"`
	// Members holds the active members, which is what every write path and
	// every split should use.
	Members []*Member `json:"members"`
	// FormerMembers holds memberships the API has marked deleted. It keeps
	// them separate from Members so a split never picks one, while still
	// letting MemberByUUID resolve the historical transactions they appear
	// in. Deleting a member does not remove them server-side.
	FormerMembers []*Member            `json:"former_members"`
	Transactions  []*Transaction       `json:"transactions"`
	Gallery       []*GalleryAttachment `json:"gallery"`
	// LinkedMemberUUID is which member this device counts as, the API's
	// membership_uuid_active. Empty when not linked.
	LinkedMemberUUID string `json:"linked_member_uuid"`
}

// MemberByName returns the active member with that display name, or nil.
// Former members are not considered, so a name freed by a deletion resolves to
// the current holder. Display names are not unique; the first match wins.
func (t *Tricount) MemberByName(name string) *Member {
	for _, m := range t.Members {
		if m.DisplayName == name {
			return m
		}
	}
	return nil
}

// MemberByUUID returns the member with that membership UUID, or nil. It
// searches former members too, so transactions belonging to someone who has
// since been removed still resolve.
func (t *Tricount) MemberByUUID(uuid string) *Member {
	for _, m := range t.Members {
		if m.UUID == uuid {
			return m
		}
	}
	for _, m := range t.FormerMembers {
		if m.UUID == uuid {
			return m
		}
	}
	return nil
}

// IsActiveMember reports whether the UUID names a current member, as opposed
// to one the API has marked deleted.
func (t *Tricount) IsActiveMember(uuid string) bool {
	for _, m := range t.Members {
		if m.UUID == uuid {
			return true
		}
	}
	return false
}

// TransactionByID returns the transaction with that ID, or nil.
func (t *Tricount) TransactionByID(id int64) *Transaction {
	for _, tx := range t.Transactions {
		if tx.ID == id {
			return tx
		}
	}
	return nil
}

// IsArchived reports whether the tricount is read-only.
func (t *Tricount) IsArchived() bool { return t.Status == StatusArchived }

// LinkedMember returns the member this device is linked to, or nil.
func (t *Tricount) LinkedMember() *Member {
	if t.LinkedMemberUUID == "" {
		return nil
	}
	return t.MemberByUUID(t.LinkedMemberUUID)
}

// SyncResult is what registry-synchronization returns: this device's
// tricounts, partitioned by state.
type SyncResult struct {
	Active   []*Tricount `json:"active"`
	Archived []*Tricount `json:"archived"`
}
