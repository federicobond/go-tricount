# go-tricount

An unofficial Go client for the [Tricount](https://tricount.com) shared-expense
app's private HTTP API.

> **Not a bunq or Tricount product.** This talks to an undocumented,
> unsupported API discovered by reverse engineering the Android app, and it may
> break at any time. Use it only with tricounts you own or have explicit
> permission to modify.

```bash
go get github.com/federicobond/go-tricount
```

No dependencies beyond the standard library.

## Quickstart

```go
creds, err := tricount.LoadOrGenerateCredentials("tricount_credentials.json")
if err != nil {
	log.Fatal(err)
}
c := tricount.NewClient(creds)
ctx := context.Background()

// The tXXXX part of a tricount.com/tXXXX sharing link.
t, err := c.JoinTricount(ctx, "tABC123xyz")
if err != nil {
	log.Fatal(err)
}

alice := t.MemberByName("Alice")
_, err = c.CreateExpense(ctx, t, tricount.Expense{
	Description: "Dinner",
	Amount:      tricount.MustParseAmount("120.00", "EUR"),
	Payer:       alice,
	Split:       tricount.SplitEqually(t.Members...),
})
if err != nil {
	log.Fatal(err)
}

balances, err := t.Balances()
```

See [`example_test.go`](example_test.go) for ratio and exact splits, income,
reimbursements, foreign currency, attachments, settling up and error handling.

## Command line

```bash
go install github.com/federicobond/go-tricount/cmd/tricount-cli@latest
```

```
tricount-cli join tABC123xyz     # follow a tricount by its sharing token
tricount-cli join tABC --as Ana  # ... and be Ana, creating her if needed
tricount-cli list                # what this device follows, with ids
tricount-cli show <id>           # members and transactions
tricount-cli balances <id>       # each member's net position
tricount-cli settle <id>         # transfers that clear every balance
tricount-cli link <id> <name>    # set which member this device counts as
tricount-cli link --create <id> <name>   # ... adding them if they do not exist
tricount-cli whoami              # this device's identity and its links
```

Every command takes `--json`. Read commands emit their data; the ones that
change something emit what they acted on, so `join --json` gives you the
tricount and `link --json` the member. Keys are snake_case throughout,
matching the API's own convention and what the library's types marshal to.
Amounts encode as strings, so exact decimals survive a trip through `jq`.
Errors stay plain text on stderr — the exit code is the contract.

The device identity is created on first use in your OS config directory —
`~/Library/Application Support/tricount/credentials.json` on macOS,
`~/.config/tricount/credentials.json` on Linux — and `$TRICOUNT_CREDENTIALS`
overrides it. `tricount-cli whoami` prints the path it is using.

## There is no login

Tricount has no email-and-password authentication. The client registers an
anonymous device — a UUID plus an RSA public key — and gets a session token.
`LoadOrGenerateCredentials` creates that identity the first time and reuses it
afterwards.

**Keep the credentials file.** It is what your synced tricounts hang off. Its
format is deliberately the same as the Python `tricount-api` package's
`tricount_credentials.json`, so one file works with either library. If you lose
it, generate a new one and re-join your tricounts with their sharing tokens.

A tricount's sharing token grants **read and write** access to anyone who has
it. Treat those tokens as secrets.

## Design notes

**Amounts are always positive.** The API stores expenses as negative amounts;
this library keeps that in the wire layer. `Amount` is exact decimal arithmetic
over `math/big`, and splits use the largest-remainder method, so allocations
always sum to their total — including for zero-decimal currencies like JPY,
where the Python reference's independent rounding comes up short. EUR 25.51 split
two ways is stored by the real API as 12.76 and 12.75, which is what this
library sends.

**Creates can be idempotent.** Setting `Expense.UUID` makes the API keep that
UUID, and a repeat returns the original entry's ID rather than a duplicate — so
a caller that derives the UUID from what it is recording can retry without
writing twice. The first write wins: a repeat with different content is ignored.
Verified against the live API, not assumed.

**The public types marshal as snake_case.** `Tricount`, `Member`,
`Transaction` and the rest carry JSON tags following the API's convention, so
`json.Marshal` on a tricount gives you `display_name` rather than
`DisplayName`. The names are the library's own, not the wire's: a sharing
token is `public_token` here and `public_identifier_token` on the wire,
because the domain model deliberately differs from it.

**Sessions are implicit.** Every method registers a session on demand and
re-registers once on a 401. `Client` is safe for concurrent use.

**Validation happens before the request.** A member who is not in the tricount,
exact amounts that do not sum to the total, an amount in the wrong currency —
all rejected locally with an error that says what is wrong, because the API's
own rejections are opaque strings like `"superfluous field"`.

**Linking is per-device.** `LinkToMember` sets which member *this* device
counts as. Two devices following the same tricount hold independent links, so
one switching does not move the other — verified with a second registered
device, not assumed. A device that has just joined is auto-linked to the member
with the lowest ID.

**Removed members are not really removed.** Deleting a member leaves the
membership server-side with a `DELETED` status so their transactions still
resolve. `Tricount.Members` therefore holds only active members, which is what
splits and writes must use, and `Tricount.FormerMembers` holds the rest.
Resending a deleted membership makes the server create a duplicate rather than
restore it, so the library never does.

**Balances are computed locally.** `Balances` and `Settle` work from the
transaction list with exact arithmetic and verify that the balances sum to zero
before returning. Income is treated as the mirror image of an expense: a member
who receives money on the group's behalf owes it to the others. The Python
reference gives income the same sign as an expense; see `Balances`'s doc
comment.

## Coverage

| Area | Support |
|---|---|
| Device registration and sessions | yes |
| Read a tricount by sharing token or ID; list all | yes |
| Create, update, archive, unarchive, delete a tricount | yes |
| Join, sync, leave | yes |
| Add, rename, delete, link members | yes |
| Expenses, income, reimbursements | yes |
| Equal, ratio and exact splits | yes |
| Foreign-currency transactions and exchange rates | yes |
| Update and delete transactions | yes |
| Transaction attachments | yes |
| Locally computed balances and settlement plans | yes |
| Tricount gallery | **no** — the API accepts an upload and returns a UUID, then lists nothing. See `UploadGalleryAttachment`. |
| Server-side settlement endpoints | **no** — `POST /registry-settlement` returns 404 `Route not found`. Use `Settle`. |

Both "no" rows were confirmed against the live API, not assumed. The methods
are kept so a future change gets noticed; their doc comments say what happens.

Immutable after creation, because the API says so: a tricount's `Currency`
(rejected on update) and `Description` (accepted and silently discarded).

## Tests

```bash
go test ./...              # offline, no network at all
go test -tags=live ./...   # hits the real API
```

The offline suite never makes a network call — the `live` build tag keeps those
tests out of the default build entirely.

The live suite keeps a **persistent** fixture under `.tricount-live/`
(gitignored): `credentials.json` for the device identity and `fixture.json` for
a reusable throwaway tricount. Both are created only when missing and are never
overwritten. The fixture tricount is **never deleted** — each run tags what it
creates and deletes only that, and `TestMain` fails the run if the fixture has
gone missing. The first run prints the fixture's sharing link; keep it.

Members and transactions you add to the fixture yourself are safe: cleanup only
removes entities carrying a run tag, so your own participant persists across
runs.

| Variable | Effect |
|---|---|
| `TRICOUNT_LIVE_TOKEN` | run against your own tricount instead of the fixture |
| `TRICOUNT_LIVE_CREDENTIALS` | use a credentials file kept elsewhere |
| `TRICOUNT_RECORD=1` | save scrubbed responses to `testdata/recorded/` |
| `TRICOUNT_RESET_FIXTURE=1` | with `-run TestLiveResetFixture`, clear every tagged leftover |
| `TRICOUNT_PROBE=1` | with `-run TestLiveInspectFixture`, dump the fixture's members, transactions and balances; with `-run TestLiveProbeLinkScope`, register a second device and re-check that linking is per-device |

Recorded responses are scrubbed of the session token, encryption key, server
and client public keys, app id and sharing tokens before being written. They
are gitignored: they are a local aid for writing fixtures, not an artefact.

## Acknowledgements

The API was mapped by
[elrandar/tricount-api](https://github.com/elrandar/tricount-api), whose
`API.md` is the best reference available, and
[DoubleN96/tricount-api-guide](https://github.com/DoubleN96/tricount-api-guide).

## License

MIT — see [LICENSE](LICENSE).
