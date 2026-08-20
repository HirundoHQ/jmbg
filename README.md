# JMBG

A Go library for validating and parsing Serbian unique master citizen numbers (JMBG - Jedinstveni Matični Broj Građana).

## Features

- Validation: length, digits, birth date, region, and checksum (modulo 11)
- Immutable `JMBG` value type with accessors for all components
- Birth date, age (including `AgeAt` for deterministic tests), gender, region
- Typed errors (`ErrLength`, `ErrDigits`, `ErrDate`, `ErrRegion`, `ErrChecksum`) for `errors.Is`
- Streaming `Scanner` for finding JMBGs in files and HTTP bodies
- Zero external dependencies

## Installation

```bash
go get github.com/osiguraj-lako/jmbg
```

## Usage

```go
import "github.com/osiguraj-lako/jmbg"

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

## JMBG Format

13 digits, `DDMMYYYRRBBBC`: day, month, year (last 3 digits; `YYY < 800` →
2000s, otherwise 1900s), region code, unique number (000–499 male, 500–999
female), and a modulo-11 check digit (intermediate value 10 or 11 → check
digit 0).

## Development

```bash
make all    # build, vet, test
make lint   # golangci-lint
```
