# grpcguard — JMBG Leak Detection for gRPC Traffic

**Date:** 2026-08-20
**Status:** Approved design, ready for implementation planning
**Repo:** the `jmbg` library (module `github.com/osiguraj-lako/jmbg`)

## Context

The `jmbg` library validates and parses Serbian citizen numbers and already
ships a streaming `Scanner` (`scanner.go`) that finds valid JMBGs in byte
streams: it considers maximal runs of exactly 13 ASCII digits, fully
validates each candidate via `Parse`, and reports `Match{JMBG, Offset}`
through a callback. The root package has zero external dependencies and this
must stay true.

The osiguraj-lako services (monorepo `~/Git/osiguraj-lako/osiguraj-lako`)
talk to each other over gRPC and their protos carry JMBG fields
(`proto/home/home.proto`, `proto/travel/travel.proto`,
`proto/accounts/accounts.proto`, `proto/uniqa/uniqa.proto` — e.g.
`string jmbg = 3 [(buf.validate.field).string.len = 13]`). The goal is a
tripwire that answers: **does any request or response between two services
carry a JMBG — especially in fields where it should not be** (free-text
notes, error messages, email bodies sent to the dispatcher)?

Key insight settled during design: inside a gRPC interceptor the request and
response are already **decoded `proto.Message` values**, so there is no need
to scan wire-format bytes (which has deterministic false negatives from
adjacent tag bytes). Walking the message with `protoreflect` and running the
existing `Scanner` over every string/bytes field is exact and also yields
the field path for reporting.

Integration points verified in the monorepo:

- Server side: both `olako-core/main.go` (~line 121) and
  `olako-dispatcher/main.go` (~line 104) build their servers with
  `grpc.NewServer(grpc.ChainUnaryInterceptor(unaryInterceptors...))` — a
  metrics interceptor already lives in that chain
  (`pkg/metrics/grpc_interceptor.go`), so the house pattern for
  interceptors exists.
- Client side: `olako-core/internal/repo/dispatcher/dispatcher.go` creates
  connections with
  `grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))`
  per call; `grpc.WithChainUnaryInterceptor(...)` is the insertion point.

## Goals

1. `ScanMessage`: find every valid JMBG in any `proto.Message`, reporting
   the proto field path and the match.
2. Unary client and server interceptors that scan requests and responses
   and report findings, without ever altering, delaying decisions on, or
   failing the RPC (audit-only).
3. Keep the root `jmbg` package dependency-free: everything lives in a new
   subpackage `grpcguard` whose imports (`google.golang.org/grpc`,
   `google.golang.org/protobuf`) are only compiled by consumers that import
   it.

## Non-Goals (v1)

- **Streaming interceptors** — the services use unary RPCs; add
  `StreamServerInterceptor`/`StreamClientInterceptor` later if needed.
- **Numeric field reconstruction** — a JMBG stored as `int64` has lost its
  leading zero; recovering 12-digit numbers by prepending `0` multiplies
  false positives. Documented limitation, same as the JSON-number case in
  the scanner.
- **Redaction or blocking** — v1 only detects and reports. A blocking mode
  or a redacting variant is a separate future design.
- **Wire-format scanning** — superseded by the protoreflect approach.

## Package API

New directory `grpcguard/` in the `jmbg` module, package `grpcguard`:

```go
// Direction says which side of the RPC a finding came from.
type Direction string

const (
    DirectionRequest  Direction = "request"
    DirectionResponse Direction = "response"
)

// Finding is one JMBG found in a gRPC message.
type Finding struct {
    Method    string     // full RPC method, e.g. "/dispatcher.EmailService/ContactForm"
    Direction Direction  // request or response
    Path      string     // proto field path, e.g. "insured_persons[2].jmbg"
    Match     jmbg.Match // Match.Offset is the byte offset WITHIN that field's value
}

// ScanMessage walks m recursively and calls found for every valid JMBG in
// its string and bytes fields. It never mutates m.
func ScanMessage(m proto.Message, found func(path string, match jmbg.Match))

// UnaryClientInterceptor reports findings in outgoing requests and their
// responses. report must be non-nil, is called synchronously, and must not
// panic; the RPC itself is never modified or failed by scanning.
func UnaryClientInterceptor(report func(Finding)) grpc.UnaryClientInterceptor

// UnaryServerInterceptor is the server-side equivalent.
func UnaryServerInterceptor(report func(Finding)) grpc.UnaryServerInterceptor
```

## Behavior

**ScanMessage walk** (via `protoreflect.Message.Range`, recursing):

- `string` field → run one fresh `jmbg.Scanner` over the value (`Write` +
  `Close`); a fresh scanner per value keeps `Match.Offset` relative to that
  field's value. A field containing exactly a JMBG is found because the
  whole string is one maximal 13-digit run.
- `bytes` field → same, over the raw bytes.
- message/group field → recurse with the field name appended to the path.
- repeated field → recurse per element, path `field[i]`.
- map field → scan string keys and walk/scan values; path `field["key"]`
  (key rendered with `%v`).
- numeric, bool, enum fields → skipped (see Non-Goals).
- Unset fields are not visited (`Range` semantics) — that is correct.

**Path syntax:** proto field names (not JSON names) joined with `.`;
list indices as `[2]`; map keys as `["key"]`. Top-level fields have no
leading dot.

**Interceptors:**

- Client: scan `req` before `invoker`, scan `reply` after it returns, only
  when its error is nil. Server: scan `req` before `handler`, scan the
  returned response when the handler error is nil.
- Findings get `Method` from the interceptor arguments (`method` /
  `info.FullMethod`) and the appropriate `Direction`.
- If `req`/`reply` does not implement `proto.Message`, skip silently.
- Scanning must be invisible to the RPC: no error is ever returned because
  of a finding, the message is never mutated, and the handler/invoker error
  is passed through untouched.
- `report` is called synchronously on the RPC goroutine — the caller keeps
  it fast (log line, counter increment) or dispatches to a channel
  themselves.

## Wiring (documentation examples, not part of this repo's code)

Server (`olako-core/main.go`, `olako-dispatcher/main.go`):

```go
unaryInterceptors = append(unaryInterceptors,
    grpcguard.UnaryServerInterceptor(func(f grpcguard.Finding) {
        logger.Warn("JMBG in gRPC traffic",
            "method", f.Method, "direction", string(f.Direction), "path", f.Path)
        // do NOT log f.Match.JMBG itself — that would be the leak
    }))
```

Client (`olako-core/internal/repo/dispatcher/dispatcher.go`):

```go
conn, err := grpc.NewClient(r.target,
    grpc.WithTransportCredentials(insecure.NewCredentials()),
    grpc.WithChainUnaryInterceptor(grpcguard.UnaryClientInterceptor(report)),
)
```

The godoc example in the package should show the server wiring and the
"log the path, never the value" rule.

## Dependencies

The module `go.mod` gains `google.golang.org/grpc` and
`google.golang.org/protobuf` (plus indirects). This mirrors how
`osiguraj-lako/logging` keeps its `fake/` subpackage in one module. The
root `jmbg` package must not import anything new — enforce by eye in review
and by the existing zero-dep claim in README (update README wording to
"zero dependencies in the core package").

## Testing

House rules from this repo apply: TDD (red first), table-driven tests,
`errors.Is`-style precise assertions, mutation checks on the load-bearing
logic, `golangci-lint run` clean (v2 config in repo root), `make all`
green.

**No protoc/buf toolchain is needed.** Build test messages from well-known
types that ship with `google.golang.org/protobuf`:

- `structpb.Struct` gives nested messages, string fields, lists, and maps
  in one type — covers most of the walk matrix.
- `wrapperspb.BytesValue` / `wrapperspb.StringValue` cover bytes and
  scalar-wrapper cases.

**ScanMessage table** (at minimum):

- top-level string field containing exactly a JMBG → found, path correct,
  offset 0
- JMBG embedded in a longer sentence → found with correct in-field offset
- nested message → dotted path
- list of strings → `field[i]` path per element
- map value and string map key → `field["k"]` path
- bytes field → found
- invalid-checksum 13-digit string → not reported
- numeric field holding 13 digits → not reported (documented skip)
- message with no strings at all → no findings, no panic
- nil message / empty message → no findings, no panic

**Interceptor tests** — call the returned interceptor functions directly
with fake `invoker`/`handler` closures (no network, no server):

- request finding carries Method + DirectionRequest
- response finding carries DirectionResponse
- handler/invoker error passes through unchanged; response is not scanned
  on error
- non-proto req/reply is skipped without panic
- RPC result (`resp`, `err`) is byte-for-byte what the handler returned

**Mutation checks** (temporarily break, assert red, revert — use Edit, not
`git checkout`):

1. Remove the recursion into nested messages → nested-path tests must fail.
2. Swap `DirectionRequest`/`DirectionResponse` in one interceptor → the
   direction assertions must fail.

**Verification gates before claiming done:** `make all`,
`golangci-lint run` with 0 issues, `go test -cover` reporting 100.0% of
statements for the module (every branch of the walk and both interceptors
are reachable from the test matrix above),
`go test -run '^$' -fuzz FuzzParse -fuzztime 5s ./` still passes (sanity
that root package is untouched).

## Open Questions Resolved During Design

- *Blocking vs audit:* audit-only in v1; the callback owner decides what to
  do. Blocking would need policy decisions (which fields are allowed —
  declared `jmbg` fields are legitimate) that don't belong in the library.
- *Where it lives:* subpackage of the `jmbg` module, not a separate module —
  matches the `logging`/`fake` house pattern and keeps versioning trivial.
- *Wire scanning:* rejected; interceptors see decoded messages, so
  protoreflect is strictly better here.

## Success Criteria

A fresh session can: implement `grpcguard/` from this spec with TDD, all
gates green; then, in the monorepo, wire the server interceptor into
olako-core and olako-dispatcher and the client interceptor into
olako-core's dispatcher repo, run the stack, exercise a call that carries a
JMBG (e.g. a travel policy request), and see a log line naming the method
and field path — with the JMBG value itself absent from the log.
