---
description: Improve existing code without changing its public API. Default mode is clarity (no behavior change); `optimize` mode targets allocations, ns/op, races and deadlocks, measured with benchmarks.
argument-hint: "[optimize] <file or package>"
---

# Refactor: $ARGUMENTS

If the first word of `$ARGUMENTS` is `optimize`, use **optimize** mode; otherwise **clarity** mode. The rest
is the target: a file or a package. No target: the file open in the IDE, else the one most recently
discussed. Ask if it is still ambiguous.

## Rules (both modes)

- The public API stays: exported names, signatures and interface implementations. A change that would
  touch callers outside the target is proposed to the user first, not applied.
- No new features, abstractions or error handling beyond the goal.
- No comments in Go code: the repository keeps Go sources comment-free (commit `5babd36`).
- Formatting is automatic (the gofmt hook); spend the effort on meaning.
- Tests use `t.Error(test.DiffMessage(actual, expected, "desc"))` from `internal/test`.
- Each package owns its output: don't leave artifacts (double slashes, trailing separators,
  off-by-one bounds) for callers to normalize.

## Steps

1. Read the target fully, and the tests of its package.
2. Baseline: `go test -race -count=1 ./<pkg>/...` must pass. If it fails before you start, stop and report.
   Optimize mode: also `go test -run='^$' -bench=. -benchmem ./<pkg>` and keep the numbers. If the code
   you will change has no benchmark, add one in `<file>_bench_test.go` with realistic input sizes first;
   if it has no tests, add them in `<file>_test.go` first.
3. Analyze, by mode (below).
4. Apply one change at a time and rerun the package's tests after each. A change that breaks a test is
   reverted and noted, never "fixed" by editing the test.
5. Finish with `go vet`, `golangci-lint run` and `go test -race` on the package (the Stop hook does the
   same). Optimize mode: run the benchmarks again.
6. Report in the language the user writes in: one table of changes (file, lines, what, why), then lint
   fixes if any, then the test result. Optimize mode adds ns/op and allocs/op before → after per change.

## Clarity mode

Logic must not change on any code path; skip a change that would and note it.

- **Split** a file over about 400 lines that mixes responsibilities (public API, HTTP path, WS path, shared
  helpers) into files of one package, moving functions only, and test before continuing.
- **Naming:** Go conventions, consistent acronyms (`userID`, `httpURL`), predicate booleans (`isEnabled`,
  `ok`), no shadowing of builtins.
- **Structure:** guard clauses instead of nesting, no `else` after `return`, named constants for repeated
  literals, declarations next to their first use, extraction only when the new function has one obvious job.
- **Idioms:** `range` loops, `any`, composite literals, `if` instead of a one-case `switch`, the standard
  library over hand-written equivalents (`strings.CutPrefix`, `slices`, `maps`).

## Optimize mode

Follow `.owlky/conventions.md` › Performance. First classify the target: **hot** (per request or
message, or client-triggerable) or **cold**. Race, deadlock and security fixes are always applied.

- **Cold code:** type A fixes only (obvious waste); readability wins every tie.
- **Hot code:** keep every change whose gain is stable across `-count 5` runs or removes an
  allocation, however small; drop it when the delta is within noise (`benchstat`). Each new fast path
  gets a test proving it equals the slow path.
- **Heavy techniques** (`unsafe`, `sync.Pool`, hand-managed buffers, restructuring to inline): never
  apply them. List each as a proposal in the report, with the benchmark that justifies it and the
  constraint it brings, for the user to approve.
- **Hot-path waste:** work repeated per call that could run once (config joins, map builds, regexp
  compiles, reflection: move them to construction, `Create()` or `sync.Once`), `fmt` and `defer` on the
  request path, pre-sized slices, `strings.Builder` over `+=` in loops.
- **Correctness:** dead or redundant conditions, missing bounds checks, normalization that runs after its
  result was already captured into an output struct.
- **Concurrency:** every access to shared state under the same lock or atomic; listener lists copied into
  a private slice before the lock is released; fire-once listeners removed in the same critical section
  that takes them; no I/O, callbacks or channel operations while holding a lock; `defer Unlock` right after
  `Lock` unless the lock must drop before a blocking call; no RLock-to-Lock upgrade assumption;
  `WaitGroup.Add` before the goroutine starts. Types that own a lock get a concurrent test that must pass
  under `-race` (pattern: `TestWSConnection_TrySend_ConcurrentSendsNoRace` in `core/ws_connmgr_test.go`).
- **Security:** injection through string building, `math/rand` where `crypto/rand` is needed, unvalidated
  input in paths or exec, disabled TLS verification.
