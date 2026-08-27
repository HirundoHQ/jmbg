# JMBG Serialization & Log Masking

**Date:** 2026-08-20
**Status:** Approved design, ready for implementation planning
**Repo:** the `jmbg` library (module `github.com/osiguraj-lako/jmbg`)

## Context

`JMBG` is an immutable value type with only unexported fields (`jmbg.go`).
That protects the parse-validated invariant, but it also means
`encoding/json` marshals it as `{}` and `database/sql` cannot store or read
it — the value is **silently lost** the moment someone puts a `JMBG` in a
DTO or a DB row. This is effectively a bug, not an enhancement.

Separately, the type is PII: `logger.Info("user", "jmbg", j)` currently
prints the full citizen number. The osiguraj-lako `logging` package is
built on `log/slog`, which honors `slog.LogValuer` — implementing it makes
accidental logging emit a masked value automatically.

The root package has zero dependencies and must stay that way; everything
below uses only the standard library.

## Goals

1. Lossless, validated round-tripping through JSON (and any
   text-marshal-aware encoder) via `encoding.TextMarshaler` /
   `encoding.TextUnmarshaler`.
2. Database storage via `driver.Valuer` / `sql.Scanner`.
3. `Masked()` for safe display and `slog.LogValuer` so slog-based logging
   masks automatically.
4. Small documentation extras (see below).

## Non-Goals

- Custom JSON object shape (`{"value": ..., "region": ...}`) — the JMBG is
  its 13-digit string; components are derivable.
- YAML/XML-specific code — TextMarshaler covers encoders that honor it.
- Changing `String()` — it stays the raw 13 digits by design (`Format`
  round-trip, scanner tests, DB values depend on it). The masking boundary
  is the slog path and explicit `Masked()` calls, and this trade-off must
  be documented: `fmt.Println(j)` still prints the full number.

## API (all in a new `encoding.go`, masking in `mask.go`)

```go
// MarshalText implements encoding.TextMarshaler. The zero value returns an
// error — emitting an invalid JMBG would defeat the parse-validated
// invariant. Use *JMBG (or database/sql.Null[JMBG]) for optional values.
func (j JMBG) MarshalText() ([]byte, error)

// UnmarshalText implements encoding.TextUnmarshaler by delegating to Parse,
// so an invalid JMBG cannot enter the system; the error wraps the usual
// Err* sentinels (errors.Is works).
func (j *JMBG) UnmarshalText(b []byte) error

// Value implements driver.Valuer, storing the 13-digit string. The zero
// value returns an error (same rationale as MarshalText).
func (j JMBG) Value() (driver.Value, error)

// Scan implements sql.Scanner, accepting string and []byte and validating
// via Parse. Any other type (including nil / SQL NULL) returns an error —
// use sql.Null[JMBG] for nullable columns.
func (j *JMBG) Scan(src any) error

// Masked returns the JMBG with the middle digits hidden:
// "0710003730015" → "0710*******15" (first 4 and last 2 visible, seven
// asterisks between). The zero value returns "".
func (j JMBG) Masked() string

// LogValue implements slog.LogValuer, returning the masked form so
// slog-based loggers never print the full number by accident.
func (j JMBG) LogValue() slog.Value
```

Decisions fixed here:

- **Zero value marshals/Values as an error**, not as `""` — failing loudly
  beats emitting data that can never round-trip. Optionality is expressed
  with `*JMBG` or `sql.Null[JMBG]` (document both in godoc).
- **Mask shape is `DDMM*******XC`** (4 + 7×`*` + 2, length 13): birth
  day/month and the final two digits stay visible — enough to correlate in
  logs, not enough to reconstruct (year, region, and most of the unique
  number are hidden).
- `UnmarshalText`/`Scan` inherit `Parse`'s whitespace trimming — no extra
  behavior.

## Documentation extras (same change set)

- `doc.go` with a runnable package example (Parse → JSON round-trip →
  masked log line).
- Godoc note on `JMBG` that it is comparable with `==` (map keys, dedup).
- Godoc note on `Parse` that birth dates in the future are accepted (a
  `yyy < 800` value maps to 2000+ and may exceed today); callers who care
  check `j.BirthDate().After(time.Now())`. `Age()` then goes negative —
  documented, not "fixed", to keep Parse a pure function.
- Commit the fuzz corpora under `testdata/fuzz/FuzzParse/` and
  `testdata/fuzz/FuzzScanner/` (run each fuzzer ~60s locally first, then
  `git add testdata/fuzz`) so past findings become permanent regression
  tests.

## Testing

House rules apply: TDD red-first, table-driven, `errors.Is` assertions,
mutation checks, `golangci-lint run` 0 issues, `make all` green,
`go test -cover` at 100.0% of statements.

**Round-trip tests:**

- `json.Marshal` of a struct with a `JMBG` field → `"0710003730015"` in
  the output; `json.Unmarshal` back → equal value (`==`).
- `json.Unmarshal` of an invalid value → error satisfying
  `errors.Is(err, ErrChecksum)` (JSON wraps but preserves the chain).
- Map with `JMBG` keys marshals via TextMarshaler and unmarshals back.
- Zero value: `json.Marshal` fails; `(&JMBG{}).UnmarshalText(nil)` fails
  with `ErrLength`.
- `Value()`/`Scan()` direct tests: string, []byte, round-trip, invalid
  input → sentinel error, unsupported type and nil → error mentioning
  `sql.Null`.
- `sql.Null[JMBG]` works for NULL and non-NULL (compile-time + behavior
  test; no real DB needed).

**Masking tests:**

- Table: normal value → `0710*******15` shape (assert exact string and
  length 13); zero value → `""`.
- slog test: log through `slog.New(slog.NewTextHandler(&buf, nil))` with a
  `JMBG` attr; assert the buffer contains the masked form and does NOT
  contain the full number. This is the leak test — the negative assertion
  is the point.

**Mutation checks** (Edit-based revert, never `git checkout`):

1. Make `UnmarshalText` bypass `Parse` (assign raw string) → the
   invalid-JSON test must fail.
2. Widen the visible prefix in `Masked` (4 → 6) → the exact-string mask
   test must fail.
3. Make `LogValue` return `slog.StringValue(j.String())` → the slog
   negative assertion must fail.

## Success Criteria

A fresh session implements `encoding.go`, `mask.go`, `doc.go`, and tests
from this spec; all gates green; README gains a short "Serialization &
Logging" subsection showing the JSON round-trip and the masked log line,
with the `fmt.Println` caveat stated.
