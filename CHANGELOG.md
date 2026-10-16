# Changelog

Notable changes to go-tricount, library and command line. Format per
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), versions per
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- `tricount-cli add`, `pay` and `remove`: an expense, a reimbursement, a
  deletion by transaction id. `--split` takes names or `name=shares`,
  `--exact` takes `name=amount`; `--category` is an app category when it
  names one, else a custom label; `--uuid` makes a retry safe. Flags go
  before or after the positionals.
- `tricount-cli use` sets the default tricount; `use` alone reports it,
  `use --unset` forgets it. Stored beside `credentials.json`.
- One rule for every command: an explicit id or title, else the `use`
  setting, else the only tricount followed. Otherwise the command stops and
  lists the candidates.
- The read commands take a title as well as an id, and nothing when one
  tricount is followed. `link` takes the member alone.
- Ctrl-C and SIGTERM cancel the request in flight.

### Changed

- Requires Go 1.26, the oldest release still supported.

### Documented

- An entry UUID is spent for good, in every tricount; derive keys from the
  tricount id too.
- `DeleteTricount` only archives. Nothing destroys a tricount or revokes a
  sharing token.
- `GetTricountByID` sees only followed tricounts; `GetTricount` finds the
  rest by token.

## [0.1.0] - 2026-09-16

Initial release: the client, exact decimal amounts, and a read-only
`tricount-cli`.

[Unreleased]: https://github.com/federicobond/go-tricount/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/federicobond/go-tricount/releases/tag/v0.1.0
