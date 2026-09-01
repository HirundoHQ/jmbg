# JMBG

A Go library for validating and parsing Serbian unique master citizen numbers (JMBG - Jedinstveni Matični Broj Građana).

## Features

- Validation: length, digits, birth date, region, and checksum (modulo 11)
- Immutable `JMBG` value type with accessors for all components
- Birth date, age (including `AgeAt` for deterministic tests), gender, region
- Typed errors (`ErrLength`, `ErrDigits`, `ErrDate`, `ErrRegion`, `ErrChecksum`) for `errors.Is`
- JSON and `database/sql` round-tripping (`TextMarshaler`, `driver.Valuer`, `sql.Scanner`)
- `Masked()` and `slog.LogValuer` so logs never print the full number
- Streaming `Scanner` for finding JMBGs in files and HTTP bodies
- `Redactor` that masks JMBGs in-flight, so they never reach a log or response
- Zero external dependencies

## Installation

```bash
go get github.com/hirundohq/jmbg
```

## Usage

```go
import "github.com/hirundohq/jmbg"

if jmbg.Valid("0101000710009") {
    fmt.Println("valid")
}

j, err := jmbg.Parse("0101000710009")
if err != nil {
    // errors.Is(err, jmbg.ErrChecksum), jmbg.ErrDate, ...
    log.Fatal(err)
}

fmt.Println(j)                    // "0101000710009" (fmt.Stringer)
fmt.Println(j.BirthDate())        // 2000-01-01 (time.Time, midnight UTC)
fmt.Println(j.Age())              // age at the current time
fmt.Println(j.AgeAt(someMoment))  // age at any moment
fmt.Println(j.Gender())           // "Male" or "Female"
fmt.Println(j.IsAdult())          // 18+ at the current time
fmt.Println(j.Region().Name)      // "Belgrade"
fmt.Println(j.Region().Country)   // "Serbia"
fmt.Println(j.Unique())           // 0
fmt.Println(j.Checksum())         // 9
```

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
never be parsed back — `MarshalText` and `Value` return `ErrZero`, and one
unset field fails the whole payload. For optional values use `*JMBG`,
`sql.Null[JMBG]`, or — in JSON DTOs — the `omitzero` tag (Go 1.24+), which
simply drops the zero value:

```go
JMBG jmbg.JMBG `json:"jmbg,omitzero"`
```

`JMBG` is personal data, so it implements `slog.LogValuer` and `log/slog`
masks it automatically:

```go
logger.Info("user loaded", "jmbg", j)  // jmbg=0710*******15
fmt.Println(j.Masked())                // 0710*******15
```

**Caveat — the one that actually bites:** slog masks only a JMBG passed as
the attribute value itself. It does not walk into composite values, so a
JMBG nested in a struct, slice, or map is formatted with `%+v` and leaks:

```go
logger.Info("user", "jmbg", j)  // jmbg=0710*******15   masked
logger.Info("user", "user", u)  // user="{Name:Pera JMBG:0710003730015}"
```

`LogValue` is the per-value boundary; the stream is the catch-all. Point the
handler at a `Scanner` and every JMBG that actually reaches the log gets
caught, nested or not — slog serializes writes to its sink and ends each
record with a newline, so a `Scanner` is safe there and never sees a number
split across records:

```go
scanner := jmbg.NewScanner(func(m jmbg.Match) {
    alert("JMBG in log output:", m.JMBG.Masked())  // report masked, don't leak again
})
defer scanner.Close()

logger := slog.New(slog.NewTextHandler(io.MultiWriter(os.Stdout, scanner), nil))
```

**Caveat:** `String()` is deliberately unmasked — the JSON, database, and
scanner round-trips depend on the raw digits. `fmt.Println(j)` and `%v` in a
logger that does not understand `slog.LogValuer` print the full number; call
`Masked()` there.

### Scanning Text for JMBGs

`Scanner` finds valid JMBGs inside arbitrary byte streams — files, HTTP
bodies, logs — in a single pass with constant memory. Only maximal runs of
exactly 13 ASCII digits are considered, and every candidate is fully
validated:

```go
err := jmbg.Scan(file, func(m jmbg.Match) {
    fmt.Printf("found %s at byte %d\n", m.JMBG, m.Offset)
})
```

`Scanner` is an `io.WriteCloser`, so it also works as a tee on an
`http.ResponseWriter` — see the package examples for a leak-detection
middleware. Decompress gzip bodies and extract text from PDFs before
scanning.

**Caveat — a compressed body is not a safe one.** Scanning raw gzip bytes is
unreliable in both directions. Usually the digits are Huffman-coded and the
scanner misses them entirely. But a body too small to be worth compressing —
under roughly 262 bytes, where deflate emits a *stored*, uncompressed block —
travels verbatim, and the JMBG sits there in plain ASCII: a short JSON error
response carrying a JMBG leaks it through `Content-Encoding: gzip` exactly as
if it had never been compressed. Rely on neither outcome. Decode first, and
scan what comes out.

### Redacting JMBGs In-Flight

`Scanner` detects a leak; `Redactor` prevents it. It is an `io.WriteCloser`
that forwards a stream byte for byte, replacing each valid JMBG with 13
asterisks before it reaches the destination — same detection rules, so a
16-digit card number passes through untouched:

```go
logDst := jmbg.NewRedactor(file, nil)
logger := slog.New(slog.NewTextHandler(logDst, nil))
...
if err := logDst.Close(); err != nil {  // flushes a run touching the end
    return err                          // don't `defer ... .Close()` here
}
```

`Close` is required and its error is real: it flushes a run held at the end
of the stream, so discarding it can silently drop the final JMBG. Unlike
`Scanner`, whose `Write` never fails, a `Redactor` returns whatever the
destination returns.

Redaction preserves length, so byte offsets and downstream formatting stay
stable, and only the current digit run — at most 13 bytes — is ever held
back. To redact an `http.ResponseWriter`, embed it the way the leak-detection
middleware in the package examples does; a `*Redactor` is a plain
`io.Writer` and does not satisfy `http.ResponseWriter` on its own.

**Caveat — false positives cost data here.** Detection is the same as
`Scanner`'s, and about **one in 1,200** random 13-digit numerals happens to be
a valid JMBG. For `Scanner` that is a spurious alert; for `Redactor` it means
a 13-digit order ID or account number is occasionally replaced with
asterisks, and because length is preserved nothing downstream notices. Don't
point a `Redactor` at a stream whose 13-digit numbers matter.

## JMBG Format

13 digits, `DDMMYYYRRBBBC`: day, month, year (last 3 digits; `800–999` →
1800–1999 and `000–099` → 2000–2099, anything between is rejected as an
invalid date), region code, unique number (000–499 male, 500–999 female), and
a modulo-11 check digit (intermediate value 10 or 11 → check digit 0).

## Development

```bash
make all    # build, vet, test
make lint   # golangci-lint
```
