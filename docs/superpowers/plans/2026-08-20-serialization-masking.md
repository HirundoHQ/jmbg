# JMBG Serialization & Log Masking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `JMBG` round-trip losslessly through JSON and `database/sql`, and mask itself automatically in `log/slog` output.

**Architecture:** `JMBG` keeps its unexported fields and `Parse`-only construction; serialization is added as methods that funnel every inbound value through `Parse` (`UnmarshalText`, `Scan`) and every outbound value through the stored 13-digit string (`MarshalText`, `Value`). Masking is a separate concern in `mask.go`: `Masked()` plus `LogValue()` so `log/slog` never prints the full number. `String()` is intentionally left unmasked.

**Tech Stack:** Go 1.24 (language floor), standard library only — `encoding`, `encoding/json`, `database/sql`, `database/sql/driver`, `log/slog`.

**Spec:** `docs/superpowers/specs/2026-08-20-serialization-masking-design.md`

## Global Constraints

- **Zero external dependencies.** `go.mod` must keep an empty `require` block. Standard library only.
- **Do not change `String()`** — it stays the raw 13 digits. The JSON, database, and scanner round-trips depend on it.
- **Do not change `Parse`'s behavior.** `UnmarshalText` and `Scan` inherit its whitespace trimming and its `Err*` sentinel chain; add no extra validation.
- **The zero `JMBG` fails to serialize** — `MarshalText` and `Value` return an error rather than emitting `""`. Optionality is `*JMBG` or `sql.Null[JMBG]`.
- **Mask shape is exactly `DDMM*******XC`** — first 4 digits, seven `*`, last 2 digits, total length 13.
- Test house rules: TDD red-first, table-driven with `t.Run` subtests, `errors.Is` for sentinel assertions, standard-library `testing` only (no assertion libraries).
- Gates for every task: `make all` green, `golangci-lint run` reports 0 issues, `go test -cover ./...` reports **100.0% of statements** (it is 100.0% today and must stay there).
- Mutation checks are performed with the `Edit` tool and reverted with the `Edit` tool. **Never `git checkout`** to revert a mutation.
- Existing top-level test names are taken — notably `TestScan` (scanner_test.go:98). New `database/sql` tests must use `TestSQL*` names.

---

## File Structure

| File | Status | Responsibility |
|---|---|---|
| `mask.go` | create | `Masked()` and `LogValue()` — the PII display boundary |
| `mask_test.go` | create | Mask shape table + the slog leak test |
| `encoding.go` | create | `MarshalText`/`UnmarshalText`/`Value`/`Scan` + the shared `errZero` sentinel |
| `encoding_test.go` | create | JSON round-trip, map keys, `database/sql`, `sql.Null[JMBG]` |
| `doc.go` | create | Package documentation (moved out of `jmbg.go`), incl. serialization + logging sections |
| `example_test.go` | create | Package-level runnable `Example()` — Parse → JSON round-trip → masked log line |
| `jmbg.go` | modify | Remove package comment (moves to `doc.go`); add godoc notes on `JMBG` and `Parse` |
| `README.md` | modify | New "Serialization & Logging" subsection |
| `testdata/fuzz/FuzzParse/`, `testdata/fuzz/FuzzScanner/` | create | Committed fuzz corpora as permanent regression seeds |

---

### Task 1: Masking and slog integration

**Files:**
- Create: `mask.go`
- Test: `mask_test.go`

**Interfaces:**
- Consumes: `JMBG` and its unexported `original` field (`jmbg.go`); the `mustParse(t, input)` test helper (`jmbg_test.go:14`).
- Produces: `func (j JMBG) Masked() string` and `func (j JMBG) LogValue() slog.Value`. Task 4's `doc.go`, `example_test.go`, and README all reference the exact mask string `0710*******15`.

- [x] **Step 1: Write the failing tests**

Create `mask_test.go`:

```go
package jmbg

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

var _ slog.LogValuer = JMBG{}

func TestMasked(t *testing.T) {
	tests := []struct {
		name string
		jmbg JMBG
		want string
	}{
		{"male jmbg", mustParse(t, "0710003730015"), "0710*******15"},
		{"female jmbg", mustParse(t, "1505995700008"), "1505*******08"},
		{"zero value", JMBG{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.jmbg.Masked()
			if got != tt.want {
				t.Errorf("Masked() = %q, want %q", got, tt.want)
			}
			if tt.want != "" && len(got) != 13 {
				t.Errorf("len(Masked()) = %d, want 13", len(got))
			}
		})
	}
}

func TestLogValue(t *testing.T) {
	j := mustParse(t, "0710003730015")
	if got := j.LogValue().String(); got != "0710*******15" {
		t.Errorf("LogValue() = %q, want %q", got, "0710*******15")
	}
}

// TestLogValueDoesNotLeakThroughSlog is the leak test: the negative
// assertion is the point of it.
func TestLogValueDoesNotLeakThroughSlog(t *testing.T) {
	const full = "0710003730015"
	j := mustParse(t, full)

	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("user loaded", "jmbg", j)

	out := buf.String()
	if !strings.Contains(out, "0710*******15") {
		t.Errorf("log output %q does not contain the masked JMBG", out)
	}
	if strings.Contains(out, full) {
		t.Errorf("log output %q leaked the full JMBG", out)
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run: `go test -run 'TestMasked|TestLogValue' ./...`
Expected: FAIL — `j.Masked undefined`, `j.LogValue undefined`, and `JMBG does not implement slog.LogValuer`.

- [x] **Step 3: Write the minimal implementation**

Create `mask.go`:

```go
package jmbg

import "log/slog"

// Masked returns the JMBG with the middle digits hidden:
// "0710003730015" becomes "0710*******15". The birth day and month and the
// final two digits stay visible — enough to correlate entries in a log,
// not enough to reconstruct the number, since the year, the region, and
// most of the unique number are hidden. The result has the same length as
// the JMBG itself. The zero value returns "".
func (j JMBG) Masked() string {
	if j.original == "" {
		return ""
	}
	return j.original[:4] + "*******" + j.original[11:]
}

// LogValue implements slog.LogValuer, returning the masked form so that
// log/slog-based loggers never print a full citizen number by accident.
//
// Note that this covers the slog path only: String returns the raw digits
// by design, so fmt.Println(j) and %v in a non-slog logger still print the
// whole number. Call Masked explicitly on those paths.
func (j JMBG) LogValue() slog.Value {
	return slog.StringValue(j.Masked())
}
```

- [x] **Step 4: Run the tests to verify they pass**

Run: `go test -run 'TestMasked|TestLogValue' -v ./...`
Expected: PASS for `TestMasked` (3 subtests), `TestLogValue`, `TestLogValueDoesNotLeakThroughSlog`.

- [x] **Step 5: Mutation check 2 — widen the visible prefix**

Use `Edit` on `mask.go` to change `j.original[:4] + "*******"` to `j.original[:6] + "*******"`.

Run: `go test -run TestMasked ./...`
Expected: FAIL — `Masked() = "071000*******15", want "0710*******15"`.

Revert with `Edit` (change `[:6]` back to `[:4]`). **Do not use `git checkout`.**
Run: `go test -run TestMasked ./...` — Expected: PASS.

- [x] **Step 6: Mutation check 3 — make LogValue leak**

Use `Edit` on `mask.go` to change `return slog.StringValue(j.Masked())` to `return slog.StringValue(j.String())`.

Run: `go test -run TestLogValue ./...`
Expected: FAIL — both `TestLogValue` and the negative assertion in `TestLogValueDoesNotLeakThroughSlog` ("log output ... leaked the full JMBG").

Revert with `Edit` (back to `j.Masked()`). **Do not use `git checkout`.**
Run: `go test -run TestLogValue ./...` — Expected: PASS.

- [x] **Step 7: Run the gates**

```bash
make all
golangci-lint run
go test -cover ./...
```
Expected: build/vet/test green, 0 lint issues, `coverage: 100.0% of statements`.

- [x] **Step 8: Commit**

```bash
git add mask.go mask_test.go
git commit -m "feat: mask JMBG in log output via slog.LogValuer"
```

---

### Task 2: Text marshaling (JSON round-trip)

**Files:**
- Create: `encoding.go`
- Test: `encoding_test.go`

**Interfaces:**
- Consumes: `Parse` and the `Err*` sentinels (`jmbg.go`, `errors.go`); the `mustParse` helper.
- Produces: `func (j JMBG) MarshalText() ([]byte, error)`, `func (j *JMBG) UnmarshalText(b []byte) error`, and the unexported `errZero` sentinel that Task 3's `Value` reuses.

**Note on `errZero`:** the spec fixes *that* the zero value errors, not the error's identity. Keep the sentinel unexported — it is an implementation detail shared by `MarshalText` and `Value`, and in-package tests can still assert it with `errors.Is`. Do not add a new exported `Err*` to `errors.go`; that file documents errors returned by `Parse`.

- [x] **Step 1: Write the failing tests**

Create `encoding_test.go`:

```go
package jmbg

import (
	"encoding"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

var (
	_ encoding.TextMarshaler   = JMBG{}
	_ encoding.TextUnmarshaler = (*JMBG)(nil)
)

// jsonUser exercises JMBG as a struct field. It is comparable, so
// round-trips can be asserted with ==.
type jsonUser struct {
	Name string `json:"name"`
	JMBG JMBG   `json:"jmbg"`
}

func TestJSONRoundTrip(t *testing.T) {
	want := jsonUser{Name: "Pera Perić", JMBG: mustParse(t, "0710003730015")}

	b, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal: unexpected error: %v", err)
	}
	const wantJSON = `{"name":"Pera Perić","jmbg":"0710003730015"}`
	if string(b) != wantJSON {
		t.Errorf("json.Marshal = %s, want %s", b, wantJSON)
	}

	var got jsonUser
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal: unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("round-trip = %+v, want %+v", got, want)
	}
}

func TestJSONUnmarshalInvalid(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"bad checksum", `{"jmbg":"0710003730025"}`, ErrChecksum},
		{"too short", `{"jmbg":"123"}`, ErrLength},
		{"unknown region", `{"jmbg":"0710003660015"}`, ErrRegion},
		{"not digits", `{"jmbg":"01019907100ab"}`, ErrDigits},
		{"bad date", `{"jmbg":"3201990710009"}`, ErrDate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var u jsonUser
			err := json.Unmarshal([]byte(tt.input), &u)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("json.Unmarshal error = %v, want %v", err, tt.wantErr)
			}
			if u.JMBG != (JMBG{}) {
				t.Errorf("JMBG = %v, want the zero value after a failed unmarshal", u.JMBG)
			}
		})
	}
}

func TestJSONMapKey(t *testing.T) {
	j := mustParse(t, "0710003730015")

	b, err := json.Marshal(map[JMBG]string{j: "Pera"})
	if err != nil {
		t.Fatalf("json.Marshal: unexpected error: %v", err)
	}
	const wantJSON = `{"0710003730015":"Pera"}`
	if string(b) != wantJSON {
		t.Errorf("json.Marshal = %s, want %s", b, wantJSON)
	}

	var got map[JMBG]string
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal: unexpected error: %v", err)
	}
	if len(got) != 1 || got[j] != "Pera" {
		t.Errorf("round-trip = %v, want one entry keyed by %s", got, j)
	}
}

func TestMarshalTextZeroValue(t *testing.T) {
	b, err := JMBG{}.MarshalText()
	if !errors.Is(err, errZero) {
		t.Errorf("MarshalText error = %v, want errZero", err)
	}
	if b != nil {
		t.Errorf("MarshalText bytes = %q, want nil", b)
	}

	if _, err := json.Marshal(jsonUser{Name: "Pera"}); !errors.Is(err, errZero) {
		t.Errorf("json.Marshal of a zero JMBG: error = %v, want errZero", err)
	}
}

func TestErrZeroMentionsOptionalTypes(t *testing.T) {
	msg := errZero.Error()
	for _, want := range []string{"*JMBG", "sql.Null[JMBG]"} {
		if !strings.Contains(msg, want) {
			t.Errorf("errZero = %q, want it to mention %q", msg, want)
		}
	}
}

func TestUnmarshalText(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantErr error
		want    string
	}{
		{"valid", []byte("0710003730015"), nil, "0710003730015"},
		{"whitespace trimmed like Parse", []byte("  0710003730015  "), nil, "0710003730015"},
		{"nil", nil, ErrLength, ""},
		{"empty", []byte(""), ErrLength, ""},
		{"bad checksum", []byte("0710003730025"), ErrChecksum, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var j JMBG
			err := j.UnmarshalText(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("UnmarshalText error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if j != (JMBG{}) {
					t.Errorf("JMBG = %v, want the zero value after a failed unmarshal", j)
				}
				return
			}
			if j.String() != tt.want {
				t.Errorf("String() = %q, want %q", j.String(), tt.want)
			}
		})
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run: `go test -run 'TestJSON|TestMarshalText|TestUnmarshalText|TestErrZero' ./...`
Expected: FAIL to compile — `j.MarshalText undefined`, `j.UnmarshalText undefined`, `undefined: errZero`.

- [x] **Step 3: Write the minimal implementation**

Create `encoding.go`:

```go
package jmbg

import "errors"

// errZero is returned when a zero JMBG is serialized. Emitting it would
// produce a value that can never be parsed back, so serialization fails
// loudly instead.
var errZero = errors.New("jmbg: zero value cannot be serialized; use *JMBG or sql.Null[JMBG] for optional values")

// MarshalText implements encoding.TextMarshaler, emitting the 13-digit
// JMBG string. Any encoder that honors TextMarshaler — encoding/json
// included, both for values and for map keys — round-trips a JMBG
// losslessly.
//
// The zero value returns an error rather than an empty string: emitting a
// value that UnmarshalText could never accept would defeat the
// parse-validated invariant. Use *JMBG (or database/sql.Null[JMBG]) for
// optional values.
func (j JMBG) MarshalText() ([]byte, error) {
	if j.original == "" {
		return nil, errZero
	}
	return []byte(j.original), nil
}

// UnmarshalText implements encoding.TextUnmarshaler by delegating to
// Parse, so an invalid JMBG can never enter the system. The returned error
// wraps the usual Err* sentinels and can be tested with errors.Is, even
// through the wrapping that encoding/json adds. Leading and trailing
// whitespace is trimmed, exactly as Parse does. On failure the receiver is
// left untouched.
func (j *JMBG) UnmarshalText(b []byte) error {
	parsed, err := Parse(string(b))
	if err != nil {
		return err
	}
	*j = parsed
	return nil
}
```

- [x] **Step 4: Run the tests to verify they pass**

Run: `go test -run 'TestJSON|TestMarshalText|TestUnmarshalText|TestErrZero' -v ./...`
Expected: PASS.

- [x] **Step 5: Mutation check 1 — bypass `Parse` in `UnmarshalText`**

Use `Edit` on `encoding.go` to replace the body of `UnmarshalText` with a raw assignment:

```go
func (j *JMBG) UnmarshalText(b []byte) error {
	j.original = string(b)
	return nil
}
```

Run: `go test -run 'TestJSONUnmarshalInvalid|TestUnmarshalText' ./...`
Expected: FAIL — every `TestJSONUnmarshalInvalid` subtest reports `error = <nil>, want jmbg: ...`, and `TestUnmarshalText` fails on the invalid rows.

Revert with `Edit`, restoring the `Parse`-delegating body from Step 3. **Do not use `git checkout`.**
Run: `go test -run 'TestJSONUnmarshalInvalid|TestUnmarshalText' ./...` — Expected: PASS.

- [x] **Step 6: Run the gates**

```bash
make all
golangci-lint run
go test -cover ./...
```
Expected: green, 0 issues, `coverage: 100.0% of statements`.

- [x] **Step 7: Commit**

```bash
git add encoding.go encoding_test.go
git commit -m "feat: round-trip JMBG through encoding.TextMarshaler"
```

---

### Task 3: database/sql support

**Files:**
- Modify: `encoding.go` (append `Value` and `Scan`)
- Test: `encoding_test.go` (append)

**Interfaces:**
- Consumes: `errZero` and `UnmarshalText` from Task 2.
- Produces: `func (j JMBG) Value() (driver.Value, error)` and `func (j *JMBG) Scan(src any) error`.

**Toolchain note:** `sql.Null[T].Value()` delegates to `T`'s `driver.Valuer` (verified in the local `database/sql` source, guarded by the fix for Go issue 69728), and `sql.Null[T].Scan` reaches `(*JMBG).Scan` through `convertAssign`'s `dest.(Scanner)` branch. Both directions therefore work with no extra code.

- [x] **Step 1: Write the failing tests**

Append to `encoding_test.go` (and add `"database/sql"`, `"database/sql/driver"` to its import block):

```go
var (
	_ driver.Valuer = JMBG{}
	_ sql.Scanner   = (*JMBG)(nil)
)

func TestValue(t *testing.T) {
	j := mustParse(t, "0710003730015")

	v, err := j.Value()
	if err != nil {
		t.Fatalf("Value: unexpected error: %v", err)
	}
	if v != driver.Value("0710003730015") {
		t.Errorf("Value() = %#v, want the 13-digit string", v)
	}
}

func TestValueZeroValue(t *testing.T) {
	v, err := JMBG{}.Value()
	if !errors.Is(err, errZero) {
		t.Errorf("Value error = %v, want errZero", err)
	}
	if v != nil {
		t.Errorf("Value() = %#v, want nil", v)
	}
}

func TestSQLScan(t *testing.T) {
	tests := []struct {
		name string
		src  any
	}{
		{"string", "0710003730015"},
		{"bytes", []byte("0710003730015")},
		{"string with whitespace", "  0710003730015  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var j JMBG
			if err := j.Scan(tt.src); err != nil {
				t.Fatalf("Scan: unexpected error: %v", err)
			}
			if j.String() != "0710003730015" {
				t.Errorf("String() = %q, want %q", j.String(), "0710003730015")
			}
		})
	}
}

func TestSQLScanInvalidValue(t *testing.T) {
	tests := []struct {
		name    string
		src     any
		wantErr error
	}{
		{"string, bad checksum", "0710003730025", ErrChecksum},
		{"bytes, bad checksum", []byte("0710003730025"), ErrChecksum},
		{"string, too short", "123", ErrLength},
		{"bytes, unknown region", []byte("0710003660015"), ErrRegion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var j JMBG
			err := j.Scan(tt.src)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Scan error = %v, want %v", err, tt.wantErr)
			}
			if j != (JMBG{}) {
				t.Errorf("JMBG = %v, want the zero value after a failed scan", j)
			}
		})
	}
}

func TestSQLScanUnsupportedType(t *testing.T) {
	tests := []struct {
		name string
		src  any
	}{
		{"nil (SQL NULL)", nil},
		{"int", 42},
		{"time", date(2000, 1, 1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var j JMBG
			err := j.Scan(tt.src)
			if err == nil {
				t.Fatal("Scan: expected an error, got nil")
			}
			if !strings.Contains(err.Error(), "sql.Null[JMBG]") {
				t.Errorf("Scan error = %q, want it to point at sql.Null[JMBG]", err)
			}
		})
	}
}

func TestSQLRoundTrip(t *testing.T) {
	want := mustParse(t, "0710003730015")

	v, err := want.Value()
	if err != nil {
		t.Fatalf("Value: unexpected error: %v", err)
	}

	var got JMBG
	if err := got.Scan(v); err != nil {
		t.Fatalf("Scan: unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("round-trip = %v, want %v", got, want)
	}
}

func TestSQLNull(t *testing.T) {
	want := mustParse(t, "0710003730015")

	t.Run("value, non-NULL", func(t *testing.T) {
		v, err := sql.Null[JMBG]{V: want, Valid: true}.Value()
		if err != nil {
			t.Fatalf("Value: unexpected error: %v", err)
		}
		if v != driver.Value("0710003730015") {
			t.Errorf("Value() = %#v, want the 13-digit string", v)
		}
	})

	// A NULL column never reaches JMBG.Value, so the zero-value error does
	// not fire — that is the whole point of using sql.Null here.
	t.Run("value, NULL", func(t *testing.T) {
		v, err := sql.Null[JMBG]{}.Value()
		if err != nil {
			t.Fatalf("Value: unexpected error: %v", err)
		}
		if v != nil {
			t.Errorf("Value() = %#v, want nil", v)
		}
	})

	t.Run("scan, non-NULL", func(t *testing.T) {
		var got sql.Null[JMBG]
		if err := got.Scan("0710003730015"); err != nil {
			t.Fatalf("Scan: unexpected error: %v", err)
		}
		if !got.Valid || got.V != want {
			t.Errorf("Scan = %+v, want {V:%v Valid:true}", got, want)
		}
	})

	t.Run("scan, NULL", func(t *testing.T) {
		var got sql.Null[JMBG]
		if err := got.Scan(nil); err != nil {
			t.Fatalf("Scan: unexpected error: %v", err)
		}
		if got.Valid || got.V != (JMBG{}) {
			t.Errorf("Scan = %+v, want {V:zero Valid:false}", got)
		}
	})

	t.Run("scan, invalid", func(t *testing.T) {
		var got sql.Null[JMBG]
		if err := got.Scan("0710003730025"); !errors.Is(err, ErrChecksum) {
			t.Errorf("Scan error = %v, want %v", err, ErrChecksum)
		}
	})
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run: `go test -run 'TestValue|TestSQL' ./...`
Expected: FAIL to compile — `j.Value undefined`, `j.Scan undefined`, `JMBG does not implement driver.Valuer`.

- [x] **Step 3: Write the minimal implementation**

Append to `encoding.go`, and change its import block to:

```go
import (
	"database/sql/driver"
	"errors"
	"fmt"
)
```

```go
// Value implements driver.Valuer, storing the 13-digit JMBG string.
//
// The zero value returns an error, for the same reason MarshalText does:
// a database column that holds an unparseable JMBG is worse than a failed
// insert. Use *JMBG or database/sql.Null[JMBG] for nullable columns.
func (j JMBG) Value() (driver.Value, error) {
	if j.original == "" {
		return nil, errZero
	}
	return j.original, nil
}

// Scan implements sql.Scanner, accepting the string and []byte forms a
// driver may deliver a text column in and validating them through Parse.
// Any other type — including nil, which is how a SQL NULL arrives —
// returns an error; use database/sql.Null[JMBG] for nullable columns. On
// failure the receiver is left untouched.
func (j *JMBG) Scan(src any) error {
	switch s := src.(type) {
	case string:
		return j.UnmarshalText([]byte(s))
	case []byte:
		return j.UnmarshalText(s)
	default:
		return fmt.Errorf("jmbg: cannot scan %T into JMBG; use sql.Null[JMBG] for nullable columns", src)
	}
}
```

- [x] **Step 4: Run the tests to verify they pass**

Run: `go test -run 'TestValue|TestSQL' -v ./...`
Expected: PASS for all subtests.

- [x] **Step 5: Run the gates**

```bash
make all
golangci-lint run
go test -cover ./...
```
Expected: green, 0 issues, `coverage: 100.0% of statements`.

- [x] **Step 6: Commit**

```bash
git add encoding.go encoding_test.go
git commit -m "feat: store JMBG through database/sql driver.Valuer and sql.Scanner"
```

---

### Task 4: Documentation — doc.go, godoc notes, runnable example, README

**Files:**
- Create: `doc.go`, `example_test.go`
- Modify: `jmbg.go` (remove the package comment; add notes on `JMBG` and `Parse`), `README.md`

**Interfaces:**
- Consumes: `Parse`, `Masked` (Task 1), `MarshalText`/`UnmarshalText` (Task 2).
- Produces: no new API — documentation only. The `Example()` output is asserted by `go test`, so it is a real regression test on the mask shape and the JSON encoding.

- [x] **Step 1: Write the failing example test**

Create `example_test.go`:

```go
package jmbg_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/osiguraj-lako/jmbg"
)

// Example shows the whole lifecycle of a JMBG: parsing it, round-tripping
// it through JSON, and logging it without leaking the number.
func Example() {
	type user struct {
		Name string    `json:"name"`
		JMBG jmbg.JMBG `json:"jmbg"`
	}

	j, err := jmbg.Parse("0710003730015")
	if err != nil {
		panic(err)
	}

	b, err := json.Marshal(user{Name: "Pera Perić", JMBG: j})
	if err != nil {
		panic(err)
	}
	fmt.Println(string(b))

	var back user
	if err := json.Unmarshal(b, &back); err != nil {
		panic(err)
	}
	fmt.Println("round-trips:", back.JMBG == j)

	// slog masks the JMBG through LogValue; the timestamp is dropped here
	// only to keep the example output deterministic.
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
	logger.Info("user loaded", "jmbg", back.JMBG)

	// Output:
	// {"name":"Pera Perić","jmbg":"0710003730015"}
	// round-trips: true
	// level=INFO msg="user loaded" jmbg=0710*******15
}
```

- [x] **Step 2: Run the example to verify it passes**

Run: `go test -run 'Example$' -v ./...`
Expected: PASS. (This example depends only on Tasks 1–3, so it should already be green — if the output differs, the mask shape or the JSON encoding is wrong and must be fixed before continuing.)

- [x] **Step 3: Move the package comment into `doc.go`**

Create `doc.go`:

```go
// Package jmbg validates and parses Serbian unique master citizen numbers
// (JMBG - Jedinstveni Matični Broj Građana).
//
// JMBG format: DDMMYYYRRBBBC
//
//   - DD  - Day of birth (01-31)
//   - MM  - Month of birth (01-12)
//   - YYY - Year of birth (last 3 digits)
//   - RR  - Region code
//   - BBB - Unique number (000-499 male, 500-999 female)
//   - C   - Checksum digit
//
// # Serialization
//
// JMBG round-trips losslessly through any encoder that honors
// encoding.TextMarshaler and encoding.TextUnmarshaler — encoding/json
// included, as a value and as a map key — and through database/sql via
// driver.Valuer and sql.Scanner. Every inbound value goes through Parse,
// so an invalid JMBG cannot enter the system, and the resulting error
// still wraps the Err* sentinels.
//
// The zero JMBG is not a valid value and fails to serialize rather than
// emitting an empty string that could never be parsed back. Express
// optionality with *JMBG or database/sql.Null[JMBG].
//
// # Logging
//
// A JMBG is personal data. It implements slog.LogValuer, so log/slog-based
// loggers emit the masked form automatically:
//
//	logger.Info("user loaded", "jmbg", j) // jmbg=0710*******15
//
// String is deliberately left unmasked — it returns the raw 13 digits,
// which the JSON, database, and Scanner round-trips all depend on. The
// trade-off is that fmt.Println(j), and %v or %s in a logger that does not
// understand slog.LogValuer, still print the full number; call Masked
// explicitly on those paths.
package jmbg
```

Then use `Edit` on `jmbg.go` to delete the existing package comment block (lines 1–12, from `// Package jmbg validates...` through `//   - C   - Checksum digit`), leaving the file starting with `package jmbg`.

- [x] **Step 4: Add the godoc notes to `jmbg.go`**

Use `Edit` to replace the `JMBG` type comment:

```go
// JMBG is a parsed and validated JMBG number. The zero value is not a valid
// JMBG; obtain instances via Parse.
//
// JMBG is comparable: two values are equal with == exactly when they were
// parsed from the same 13 digits, so it can be used as a map key or
// deduplicated with a set.
type JMBG struct {
```

Use `Edit` to replace the `Parse` comment:

```go
// Parse parses and validates a JMBG string. Leading and trailing whitespace
// is ignored; the remainder must be exactly 13 ASCII digits. The returned
// error wraps one of the Err* sentinel errors and can be tested with
// errors.Is.
//
// Parse is a pure function of its input and has no notion of "now", so a
// birth date in the future is accepted: a YYY below 800 maps to the 2000s
// and may not have happened yet. Age then returns a negative number.
// Callers who care should check j.BirthDate().After(time.Now()) themselves.
func Parse(input string) (JMBG, error) {
```

- [x] **Step 5: Verify the docs build and nothing regressed**

```bash
go doc . | head -40
go doc . JMBG.Masked
make all
```
Expected: the package doc shows the Serialization and Logging sections; `make all` green. `go vet` flags a duplicate package comment if the one in `jmbg.go` was not removed.

- [x] **Step 6: Add the README subsection**

Use `Edit` on `README.md` to insert this subsection immediately after the closing fence of the main "Usage" code block and before `### Scanning Text for JMBGs`:

````markdown
### Serialization & Logging

`JMBG` round-trips losslessly through `encoding/json` — and any encoder that
honors `encoding.TextMarshaler` — works as a map key, and stores in a
database through `driver.Valuer`/`sql.Scanner`. Decoding always goes through
`Parse`, so an invalid number cannot enter the system:

```go
type User struct {
    Name string    `json:"name"`
    JMBG jmbg.JMBG `json:"jmbg"`
}

b, _ := json.Marshal(User{Name: "Pera Perić", JMBG: j})
// {"name":"Pera Perić","jmbg":"0710003730015"}

var u User
err := json.Unmarshal(b, &u)  // errors.Is(err, jmbg.ErrChecksum) on a bad number
```

The zero `JMBG` fails to marshal rather than emitting a value that could
never be parsed back — use `*JMBG` or `sql.Null[JMBG]` for optional values.

`JMBG` is personal data, so it implements `slog.LogValuer` and `log/slog`
masks it automatically:

```go
logger.Info("user loaded", "jmbg", j)  // jmbg=0710*******15
fmt.Println(j.Masked())                // 0710*******15
```

**Caveat:** `String()` is deliberately unmasked — the JSON, database, and
scanner round-trips depend on the raw digits. `fmt.Println(j)` and `%v` in a
logger that does not understand `slog.LogValuer` still print the full
number; call `Masked()` there.
````

Also add two bullets to the README "Features" list, after the typed-errors bullet:

```markdown
- JSON and `database/sql` round-tripping (`TextMarshaler`, `driver.Valuer`, `sql.Scanner`)
- `Masked()` and `slog.LogValuer` so logs never print the full number
```

- [x] **Step 7: Run the gates**

```bash
make all
golangci-lint run
go test -cover ./...
```
Expected: green, 0 issues, `coverage: 100.0% of statements`.

- [x] **Step 8: Commit**

```bash
git add doc.go example_test.go jmbg.go README.md
git commit -m "docs: document serialization, masking, and the String caveat"
```

---

### Task 5: Commit the fuzz corpora as regression seeds

**Files:**
- Create: `testdata/fuzz/FuzzParse/*`, `testdata/fuzz/FuzzScanner/*`

**Interfaces:**
- Consumes: `FuzzParse` (`jmbg_test.go:285`) and `FuzzScanner` (`scanner_test.go:153`).
- Produces: seed corpus files that every future `go test` run replays.

**Important:** `go test -fuzz` writes to `testdata/fuzz/<Target>/` only when it finds a *failing* input; inputs that merely expanded coverage land in the build cache. Promoting the cache corpus into `testdata` is what makes past findings permanent, which is the spec's stated intent.

- [x] **Step 1: Run each fuzzer for ~60s**

```bash
go test -run '^$' -fuzz FuzzParse -fuzztime 60s
go test -run '^$' -fuzz FuzzScanner -fuzztime 60s
```
Expected: both report `PASS` with no `failure while testing seed corpus entry` and no new file under `testdata/fuzz`.

**If a fuzzer fails, stop.** A crasher is a real bug in `Parse` or `Scanner`; report it and do not paper over it by committing the corpus.

- [x] **Step 2: Promote the cached corpus into `testdata`**

```bash
CORPUS="$(go env GOCACHE)/fuzz/github.com/osiguraj-lako/jmbg"
mkdir -p testdata/fuzz/FuzzParse testdata/fuzz/FuzzScanner
cp "$CORPUS"/FuzzParse/* testdata/fuzz/FuzzParse/ 2>/dev/null || true
cp "$CORPUS"/FuzzScanner/* testdata/fuzz/FuzzScanner/ 2>/dev/null || true
find testdata/fuzz -type f | wc -l
head -3 testdata/fuzz/FuzzParse/* | head -6
```
Expected: a non-zero file count, and each file starting with the line `go test fuzz v1`.

If the cache is empty for a target, say so plainly in the task report rather than fabricating corpus files.

- [x] **Step 3: Verify the seeds run and pass as regular tests**

```bash
go test -run 'FuzzParse|FuzzScanner' -v ./... 2>&1 | tail -20
go test -cover ./...
```
Expected: both fuzz targets PASS in seed mode; `coverage: 100.0% of statements`.

- [x] **Step 4: Commit**

```bash
git add testdata/fuzz
git commit -m "test: commit fuzz corpora as permanent regression seeds"
```

---

### Task 6: Final gate

**Files:** none — verification only.

- [x] **Step 1: Run the full gate set on a clean tree**

```bash
gofumpt -l . 2>/dev/null || true
make all
golangci-lint run
go test -cover ./...
make govulncheck
git status --short
```
Expected: `make all` green; `golangci-lint run` prints **0 issues**; coverage is **100.0% of statements**; `govulncheck` reports no vulnerabilities; `git status --short` is clean.

- [x] **Step 2: Confirm the dependency constraint held**

```bash
cat go.mod
```
Expected: still just the `module` and `go` lines — no `require` block.

- [x] **Step 3: Re-verify each mutation check was reverted**

```bash
grep -n 'original\[:4\]' mask.go
grep -n 'slog.StringValue(j.Masked())' mask.go
grep -n 'parsed, err := Parse(string(b))' encoding.go
```
Expected: one hit each. If any is missing, a mutation was left in place.

---

## Self-Review

**Spec coverage:**

| Spec requirement | Task |
|---|---|
| `MarshalText` / `UnmarshalText` | 2 |
| `Value` / `Scan` | 3 |
| `Masked()` | 1 |
| `LogValue()` / `slog.LogValuer` | 1 |
| Zero value errors, not `""` | 2 (MarshalText), 3 (Value) |
| Mask shape `DDMM*******XC`, length 13 | 1 |
| `UnmarshalText`/`Scan` inherit Parse's trimming | 2, 3 (explicit table rows) |
| `doc.go` + runnable package example | 4 |
| Godoc: `JMBG` comparable with `==` | 4 |
| Godoc: future birth dates accepted, `Age()` goes negative | 4 |
| Fuzz corpora committed under `testdata/fuzz/` | 5 |
| JSON round-trip test | 2 |
| Invalid JSON → `errors.Is(err, ErrChecksum)` | 2 |
| Map with `JMBG` keys | 2 |
| Zero value: marshal fails; `UnmarshalText(nil)` → `ErrLength` | 2 |
| `Value`/`Scan` direct tests incl. unsupported type + nil mentioning `sql.Null` | 3 |
| `sql.Null[JMBG]` NULL and non-NULL | 3 |
| Mask table incl. zero value → `""` | 1 |
| slog leak test with the negative assertion | 1 |
| Mutation check 1 (bypass `Parse`) | 2, Step 5 |
| Mutation check 2 (prefix 4 → 6) | 1, Step 5 |
| Mutation check 3 (`LogValue` returns `String()`) | 1, Step 6 |
| README "Serialization & Logging" with the `fmt.Println` caveat | 4 |
| `make all`, lint 0 issues, coverage 100.0% | every task + 6 |

No gaps.

**Type consistency:** `Masked() string`, `LogValue() slog.Value`, `MarshalText() ([]byte, error)`, `UnmarshalText(b []byte) error`, `Value() (driver.Value, error)`, `Scan(src any) error`, and the unexported `errZero` are used identically in every task that references them. The mask string `0710*******15` is identical in Tasks 1, 4, and the README. `TestSQL*` naming avoids the existing `TestScan` in `scanner_test.go`.
