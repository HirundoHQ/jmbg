# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make all            # build + vet + test (the default check before committing)
make lint           # golangci-lint (installs it if missing)
make govulncheck    # vulnerability scan

go test -run TestAgeAt ./...                        # one test
go test -run 'TestParseErrors/unknown_region' ./... # one table case (spaces in names become _)
go test -run '^$' -fuzz FuzzScanner -fuzztime 30s   # fuzz (FuzzParse, FuzzScanner, FuzzRedactor)
```

`Example*` functions in `example_test.go` and `example_scanner_test.go` are verified by their `// Output:` comments during a normal `go test`.

## Architecture

Single Go package at the repo root (`package jmbg`), module `github.com/osiguraj-lako/jmbg`.

**Zero external dependencies is a hard constraint.** `go.mod` has no `require` block and must keep none — the user has ruled out adding dependencies to this module. Verify with `go list -deps .` after any change.

**`Parse` is the single validation gate.** `JMBG` (`jmbg.go`) has only unexported fields and no exported constructor other than `Parse`, so any `JMBG` value in existence is parse-validated. The zero value is deliberately invalid. Every inbound path funnels through `Parse` to keep that true — `UnmarshalText` and `Scan` in `encoding.go` both delegate to it rather than assigning digits directly, and the mutation check that proves it is part of the test suite's job.

**Serialization (`encoding.go`)** gives `JMBG` `MarshalText`/`UnmarshalText` and `Value`/`Scan`, so it round-trips through `encoding/json` (as a value and as a map key) and `database/sql`. The zero value refuses to serialize — it returns `ErrZero` rather than emitting a `""` that could never be parsed back — and since marshaling is all-or-nothing, one unset field fails the whole payload. Optionality is `*JMBG`, `sql.Null[JMBG]`, or the `omitzero` JSON tag (Go 1.24+; skips the zero value without calling `MarshalText` — verified, and the best fit for JSON DTOs).

**Masking (`mask.go`)** is the PII boundary: `Masked()` renders `0710*******15` and `LogValue()` wires that into `log/slog`. `String()` stays unmasked on purpose.

Three layers compose here and none is sufficient alone:

1. **`LogValue` — per value.** slog resolves `LogValuer` only when the `JMBG` is the attribute value itself, so one nested in a struct, slice, or map falls back to `%+v` (TextHandler) or `MarshalText` (JSONHandler) and prints in full.
2. **`Scanner` — stream, detects.** Pointed at a log sink it catches every JMBG actually written, nested or not (`ExampleNewScanner_logSink`). The number still lands in the log; you just learn about it.
3. **`Redactor` (`redactor.go`) — stream, prevents.** Same detection rules, but the run is *held back* and replaced with 13 asterisks before reaching the sink. This is the one that actually closes the nested-leak hole.

Wrapping a slog sink is safe because slog serializes writes under a mutex and ends each record with a newline, so a digit run never spans records — but two separately-constructed handlers over one `Scanner`/`Redactor` each hold their own mutex, which would race.

`Redactor` differs from `Scanner` in one contract detail: `Scanner.Write` never fails (that is what lets it tee an `http.ResponseWriter` safely), while `Redactor.Write` returns whatever `dst` returns and promises nothing about consistency afterwards. On a `dst` error it returns the count of bytes that need no resend (best effort, per the `io.Writer` convention) rather than 0, so `bufio` retries do not duplicate the flushed prefix; a `dst` that reports an out-of-range count gets `errInvalidWrite` instead of a slice panic. `Close`'s error matters too — it flushes a run held at the end of the stream, so `defer r.Close()` can silently drop the final JMBG.

`Redactor.Write` is write-through, not buffered: it forwards spans straight out of the caller's slice and stages nothing, so only the ≤13-byte digit run is ever held. It returns `len(p)` even when every byte was held back, which is what keeps `io.Copy` and `io.MultiWriter` working.

Validation order in `Parse` — length → all-ASCII-digits → date → region → checksum — determines which sentinel error (`errors.go`) surfaces first; the tests pin that order. Errors are wrapped with `%w` plus detail, so callers use `errors.Is`.

Format rules that are easy to get wrong:

- **Year:** `YYY >= 800` → `1000+YYY` (1800–1999), `YYY <= 099` → `2000+YYY` (2000–2099). **100–799 is `ErrDate`** — it would place the birth in 2100–2799, which was never issued. `yearAt` returns `(year, ok)` and only `Parse` formats the reason, so the rejection stays off the streaming path.
- **Checksum:** modulo 11 with weights `7 6 5 4 3 2` twice; an intermediate value of 10 or 11 means the check digit is **0**, not 10/11.
- **Region:** `regions.go` is the authority — a code missing from the map is `ErrRegion`. Codes cover all ex-Yugoslav republics plus foreign-citizen codes 0–9.

**`digitRun` (`digitrun.go`)** is the detection state machine both `Scanner` and `Redactor` drive, so "what counts as a JMBG in a byte stream" — run length, validation, offset arithmetic — is defined once. `digitsInJMBG` is that single length, and `redactionMask` derives its width from it, so the two cannot drift apart. Change detection here, not in either caller.

**`Scanner` (`scanner.go`)** is a constant-memory `io.WriteCloser` that finds JMBGs in byte streams. Its defining rule: only **maximal runs of exactly 13 ASCII digits** are candidates. A 16-digit run is never searched for an embedded JMBG — an intentional false-negative trade-off for precision. `Close` is required so a run touching the end of the stream is checked, and the scanner stays usable afterward with offsets continuing. `Write`/`Close` never fail, which is what lets it tee an `http.ResponseWriter` without breaking the response. It sees only the bytes it is handed: gzip bodies and PDFs must be decoded first (`TestScannerGzipCaveat` locks this in).

## Testing conventions

Standard-library `testing` only — no assertion libraries. Tests are table-driven with `t.Run` subtests; most are in-package (white-box) with the public-API examples in `package jmbg_test`.

The fuzz targets encode the invariants worth preserving: `Valid` agrees with `Parse`, a failed `Parse` returns the zero `JMBG`, `String()` round-trips the trimmed input, scanner output is identical whether input arrives in one write or byte by byte, and `Redactor` output equals the input with exactly `Scanner`'s matches masked — `FuzzRedactor` uses `Scanner` as an independent oracle, so a divergence between the two implementations cannot hide.

Detection is precise but not exact: **about 1 in 1,200 random 13-digit numerals parses as a valid JMBG.** (It was 1 in 365 before the year window was narrowed to the two ranges actually issued; that change cut the rate 3.3x.) Harmless for `Scanner`, data loss for `Redactor`. Keep that in the docs of anything new that redacts.

Any new test fixture must be a genuinely checksum-valid JMBG. Reuse the known-good values already in the fuzz seed corpora rather than inventing digits — and check what a value actually *is* before labelling a case with it: `0710003730015` is male (unique 001), `2307988805080` is female (unique 508). A subtest named "female" that holds a male number asserts coverage that does not exist.

Time-dependent behavior is tested through `AgeAt(now)`; `Age()`/`IsAdult()` wrap it with `time.Now()`. Keep new time-sensitive logic on that same seam.

## Design docs

`docs/superpowers/specs/` holds approved designs; `docs/superpowers/plans/` holds the plans executed from them. Implemented: serialization/masking and the redacting `io.Writer`. Read the relevant spec before changing either; the trade-offs are argued there, not in the code.

**`2026-08-20-grpcguard-design.md` is shelved — do not implement it without asking.** It was built once and reverted. The design is sound (protoreflect over decoded messages works; the walk and both interceptors reached 100% coverage), but it cannot exist without adding `grpc` + `protobuf` to this module's `go.mod`, and the zero-dependency rule wins. Two facts worth keeping if it is ever revisited:

- Every `grpc` release that fixes **GO-2026-6061** (v1.81.0 and up) declares `go 1.25.0`, so it would also raise the module's toolchain floor. The newest release that keeps `go 1.24` is v1.80.0, which carries that vulnerability. There is no version that avoids both.
- If it is revived, a **separate nested module** (`grpcguard/go.mod`) is the only shape that keeps the root module clean. The spec explicitly chose a subpackage instead, so that decision would have to be reopened first.

One gap the fuzz-related specs did not anticipate: **Go's fuzzer only writes *failing* inputs to `testdata/`.** Coverage-expanding corpus entries live in the build cache; they were promoted to `testdata/fuzz/` by hand so they replay on every `go test`.

## Linting

`.golangci.yml` is golangci-lint **v2** schema. `regions.go` is exempt from `goconst` because the data table legitimately repeats country strings.

The `formatters:` block (`gofmt`, `gofumpt`, `goimports`) is **not** enforced by `golangci-lint run` — in v2 that block belongs to `golangci-lint fmt`. `make lint` therefore does not check formatting. Run `golangci-lint fmt --diff` to check it and `golangci-lint fmt` to apply it.

## Performance

`bench_test.go` covers both stream types across payload shapes that drive the
digit-run machine down different paths; `streaming_test.go` holds the
invariants those numbers depend on. Compare with `benchstat`:

```bash
go test -run '^$' -bench . -benchmem -count 10 > new.txt
```

Two things the benchmarks exist to catch:

- **Never call a function returning `Match` on the per-byte path.** `Match`
  embeds a `JMBG`, which is large; returning it per non-digit byte cost a 5x
  throughput regression once. `Scanner.Write` guards with `empty()` and
  `complete()` — both inline — so `digitRun.end` runs once per candidate run,
  not once per byte.
- **Keep formatted errors off the streaming path.** `parseDigits` returns the
  `Err*` sentinels bare and allocates nothing unless it succeeds; `Parse`
  adds the detail afterwards, only on failure. Before that split, formatting
  *why* a random 13-digit number was not a JMBG was 52% of all allocations —
  boxing three ints into `any` for one `fmt.Errorf` costs more than the whole
  validation. A stream of 13-digit identifiers went from 80 MB/s and 292k
  allocations per MiB to 401 MB/s and 217, and those 217 were exactly the
  JMBGs actually found. Narrowing the year window to the two ranges ever
  issued removed two thirds of those false positives with it: the same
  stream now runs at 653 MB/s and 72 allocations per MiB.
- `parseDigits` takes `*[13]byte` plus the caller's `original` string, so a
  direct `Parse` shares the string it already holds (staying allocation-free
  on success) while the streaming path passes `""` and builds one only after
  validation succeeds. Do not collapse those two paths.

One enormous digit run is the *cheapest* input, not the most expensive: past
the 13th digit the run is disqualified, so `parseDigits` is never called and
nothing allocates — 10 MiB of solid digits scans at ~1.9 GB/s. `digitRun`'s
counter saturates just past `len(digits)` for that shape; letting it run free
overflows a 32-bit `int` after 2 GiB of uninterrupted digits, at which point
`empty`, `complete` and `overlong` all read false and `push` indexes the array
negatively. `TestDigitRunLengthSaturates` guards it. The real worst case is
the opposite shape — back-to-back valid JMBGs — at ~235 MB/s and one
allocation per match.

`streaming_test.go` is the regression guard for buffering: it asserts a
`Redactor` never holds more than `digitsInJMBG` bytes back, that streaming
64 MiB allocates no more than streaming 1 MiB (measured: tens of bytes
either way; the leg was 1 GiB once, shrunk because the ratio is the proof
and the size only cost ~24s under `-race`), and that neither type aliases
the caller's slice.
