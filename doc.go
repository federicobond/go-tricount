// Package tricount is an unofficial Go client for the Tricount shared-expense
// app's private HTTP API.
//
// # Status
//
// This library talks to an API that bunq does not document or support. It was
// built by reverse engineering the Android app and may stop working without
// notice. Use it with tricounts you own or have explicit permission to modify.
//
// # There is no login
//
// Tricount has no email-and-password authentication. A client registers an
// anonymous "device" — an installation UUID and an RSA public key — and
// receives a session token:
//
//	creds, err := tricount.LoadOrGenerateCredentials("tricount_credentials.json")
//	if err != nil {
//		return err
//	}
//	c := tricount.NewClient(creds)
//
// Keep that credentials file. It is the identity your synced tricounts hang
// off, and its format is shared with the Python tricount-api package.
//
// Access to a particular tricount comes from its sharing token, the tXXXX part
// of a tricount.com/tXXXX link. Anyone holding the link can read and write the
// tricount, so treat those tokens as secrets.
//
//	t, err := c.JoinTricount(ctx, "tABC123xyz")
//
// GetTricount reads a tricount without syncing it; JoinTricount syncs it to
// your device, which is required before any change.
//
// # Amounts are always positive
//
// The API stores expenses as negative amounts. This library does not expose
// that: every Amount in the public API is positive, and the sign convention is
// applied internally. Amount is exact decimal arithmetic over math/big, so
// splits always sum to their total — including for zero-decimal currencies
// like JPY.
//
//	id, err := c.CreateExpense(ctx, t, tricount.Expense{
//		Description: "Dinner",
//		Amount:      tricount.MustParseAmount("120.00", "EUR"),
//		Payer:       t.MemberByName("Alice"),
//		Split:       tricount.SplitEqually(t.Members...),
//	})
//
// # Idempotent creates
//
// Setting an entry's UUID makes the create idempotent: the API keeps the UUID
// it is given and a repeat returns the original entry's ID rather than a
// duplicate, so a caller that derives the UUID from what it is recording can
// retry without writing twice. The first write wins. See CreateExpense.
//
// # Sessions are implicit
//
// Every method registers a session on demand and re-registers once if the
// session has expired, so Authenticate is optional. A Client is safe for
// concurrent use.
//
// # Removed members
//
// Deleting a member does not remove them server-side; the API keeps the
// membership with a DELETED status so their transactions still resolve.
// Tricount.Members therefore holds only active members — which is what splits
// and writes must use — while Tricount.FormerMembers holds the rest.
//
// # Errors
//
// API failures come back as *Error and match the package sentinels:
//
//	if errors.Is(err, tricount.ErrNotFound) { ... }
//
// Anything the library can reject without a round trip — a member who is not
// in the tricount, allocations that do not sum to the total, an amount in the
// wrong currency — wraps ErrInvalidRequest and is returned before any request
// is made.
//
// # What does not work
//
// Two documented corners of the API do nothing useful, both confirmed against
// the live API rather than assumed. The settlement endpoints return 404 "Route
// not found"; use Settle, which computes the plan locally. The gallery
// endpoints accept an upload and return a UUID but never list anything; use
// transaction attachments instead. Both are described on the methods
// themselves.
package tricount
