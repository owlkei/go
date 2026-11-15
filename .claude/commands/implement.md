---
description: Implement a new feature or upgrade existing code, with a design the user approves first, then tests, a security pass and docs.
argument-hint: "<feature description | file | package>"
---

# Implement: $ARGUMENTS

`$ARGUMENTS` is a description, a file or a package. Nothing given: the file open in the IDE, else the one
most recently discussed.

## 1. Understand

Read the target and its package tests. Read the `CLAUDE.md` sections the feature touches and the matching
`.ginject/` page. Classify the task: **new** (does not exist) or **upgrade** (incomplete, insecure,
untested).

## 2. Design, then wait for approval

Present, briefly, as numbered decisions:

- the public API: exported types and signatures;
- the invariants that must hold (validation, auth, limits) and the panics or errors it adds (bootstrap
  panics follow `"<category>: <detail>"` in `color.FmtRed`, see `CLAUDE.md`);
- concurrency: which goroutines exist, what shared state they touch, the primitive that protects it, the
  lock order, and how each goroutine stops (stop method or context);
- callers outside the target that change, and any new dependency (the standard library comes first).

**Stop and wait for the user's approval.** Skip the wait only for a change with no public API and no
caller impact (a private helper, a test), and say so.

## 3. Build

- Follow the package's conventions (naming, DI through embedded fields, panic policy). No comments in Go
  code: the repository keeps Go sources comment-free.
- No features, abstractions or error handling beyond the approved design.
- Each package owns its output; compute static config once (constructor or `sync.Once`), never per
  request; assign output fields only after every normalization step.

## 4. Security pass

Check what applies and fix it: input validated at the boundary; body and payload sizes bounded
(`http.MaxBytesReader`); timeouts on outbound calls; `crypto/rand` for tokens and
`subtle.ConstantTimeCompare` for secrets; paths cleaned and kept inside their root; no secrets in code;
no internal errors or paths leaked to clients; no TLS verification turned off. Shared state protected,
locks released before callbacks, I/O and channel operations, goroutines that exit on cancellation.

## 5. Tests

In `<file>_test.go`, with `t.Error(test.DiffMessage(actual, expected, "desc"))` from `internal/test`. For
every exported function or method: the happy path, zero / nil / empty input, boundaries, invalid input
(an error, not a panic, on the request path), hostile input where it applies (oversized, `../`, null
bytes), concurrent use under `-race` when it touches shared state, and one rejection test per invariant
from step 2. A failing test means fixing the code, unless the test itself is wrong.

Add a benchmark in `<file>_bench_test.go` only for code on a request hot path or for a perf claim, and
record `go test -run='^$' -bench=. -benchmem ./<pkg>`.

## 6. Verify and document

- `go vet`, `golangci-lint run` and `go test -race` on the touched packages, then `make check` once at the
  end (the Stop hook also runs the package checks).
- **Every package you added or changed code in has a `README.md`** (except `sample/**`, the demo app):
  if it has none, create it by following `/doc`'s steps for that package (structure of
  `modules/config/README.md`, rules only from tests, benchmarks only from a real run); if it has one,
  update it for the new or changed API.
- Update `CLAUDE.md` / `.ginject/` when the change alters a concept they describe
  (`/kb update <package>`).

## Report

In the language the user writes in, once: what changed (item, new or upgraded, file), the invariants and
how each is tested, tests added, benchmark numbers if any, lint fixes, and anything left for the user to
decide.
