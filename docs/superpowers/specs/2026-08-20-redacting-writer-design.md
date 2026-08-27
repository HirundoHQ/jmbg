# Redactor — In-Flight JMBG Redaction

**Date:** 2026-08-20
**Status:** Approved design, ready for implementation planning
**Repo:** the `jmbg` library (module `github.com/osiguraj-lako/jmbg`)
**Depends on:** the existing `Scanner` (`scanner.go`); pairs naturally with
the serialization/masking spec (`2026-08-20-serialization-masking-design.md`)
but does not require it.

## Context

`Scanner` detects JMBGs in byte streams and reports them — it says "this
is leaking". The Redactor prevents the leak: an `io.Writer` middleman that
passes a stream through unchanged except that every **valid** JMBG is
replaced with a mask before reaching the destination (a log file, an HTTP
response, stdout). Same detection semantics as `Scanner`: maximal runs of
exactly 13 ASCII digits, fully validated via `Parse`; a 16-digit card
number or an invalid checksum passes through untouched.

Standard library only; lives in the root package next to `scanner.go`.

## API (new `redactor.go`)

```go
// NewRedactor returns an io.WriteCloser that copies everything written to
// it into dst, replacing each valid JMBG with 13 asterisks
// ("*************" — length-preserving). If found is non-nil it is called
// for each redacted JMBG, with Match.Offset relative to the input stream
// (pre-redaction positions; redaction preserves length, so input and
// output offsets coincide).
//
// Writes may be split at any byte boundary; runs spanning writes are
// handled. Close flushes a digit run touching the end of the stream and
// must be called; it does not close dst.
func NewRedactor(dst io.Writer, found func(Match)) *Redactor
```

`Redactor` implements `io.WriteCloser`. Unlike `Scanner`, its `Write` and
`Close` CAN fail: errors from `dst.Write` are returned as-is.

Decisions fixed here:

- **Full 13-asterisk mask, not the `Masked()` partial shape** — the
  Redactor's job is preventing leaks in sinks that were never supposed to
  carry a JMBG; keeping partial digits there has no purpose. Length
  preservation keeps offsets and downstream formatting stable. A
  configurable replacement is YAGNI until someone asks.
- **`found` callback is optional (nil allowed)** — detection reporting is
  a bonus, redaction is the job.

## Behavior — the buffering rule

The only state is the current digit run (like `Scanner`) plus a possible
pending flush:

- Non-digit bytes: if a run of exactly 13 digits just ended, emit mask or
  digits first (see below), then forward the non-digit byte immediately.
- Digit bytes: hold in the run buffer while `runLen <= 13`. On the 14th
  digit the run is disqualified — flush the 13 held digits to dst and from
  then on forward further digits directly until the run ends (no JMBG is
  possible in a longer maximal run).
- Run ends at exactly 13 (non-digit byte or `Close`): validate via
  `Parse`; valid → write `"*************"` and call `found`; invalid →
  write the digits unchanged.

Consequences to state in godoc:

- At most 13 bytes are ever held back, so latency and memory are constant;
  the output is byte-identical to the input except for replaced JMBGs.
- A run held at a `Write` boundary is not yet forwarded to dst — callers
  that need everything flushed must `Close` (end of stream), the same
  contract `Scanner` has.

**Error handling:** every `dst.Write` error aborts the current call and is
returned. Track and honor short writes (loop until the slice is written or
error). After an error the Redactor makes no consistency promises —
document "stop using after an error", no self-repair logic.

## Testing

House rules: TDD red-first, table-driven, mutation checks,
`golangci-lint run` 0 issues, `make all` green, module coverage 100.0%.

**Redaction table** (assert exact output bytes AND reported matches; run
each case at chunk sizes 1, 4, and whole-input, like `TestScanner`):

- JMBG alone → 13 asterisks
- embedded in text → text preserved byte-for-byte around the mask
- two JMBGs → both masked
- 14-digit run → passes through completely untouched
- 12-digit run, invalid checksum run → untouched
- JMBG touching end of stream → masked only after `Close`
- multibyte text around the JMBG → non-ASCII bytes forwarded unchanged
- empty input, no digits → identity copy

**Property/fuzz** (`FuzzRedactor`): for any input and any chunking,
(a) output is identical to whole-write output, and (b) output equals the
input with exactly the `Scanner`-reported matches replaced by 13 asterisks
— cross-check against `Scan` on the same input.

**Error propagation:** a dst that fails on the Nth write → `Write`/`Close`
return that error; a dst that does short writes → output still complete
and correct (use an `iotest`-style half-writer).

**Mutation checks** (Edit-based revert):

1. Reset run state at the start of `Write` → chunked cases must fail.
2. Skip the `Parse` validation (mask any 13-digit run) → the
   invalid-checksum passthrough case must fail.
3. Drop the `Close` flush → the end-of-stream case must fail.

## Success Criteria

A fresh session implements `redactor.go` + tests from this spec; all gates
green; README's scanning section gains two sentences introducing the
Redactor ("Scanner detects, Redactor replaces") with a one-line example
wrapping a log destination: `logDst := jmbg.NewRedactor(file, nil)`.
