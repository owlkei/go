# 0002: Request hot path

Status: Done

## Goal
Every package a request passes through before and around the handler gets the deep dive: bugs fixed,
hot code made fast, tests and benchmarks rewritten. Measured against `docs/review/http-baseline.md`,
the framework's own cost per request drops sharply and allocations follow; the remaining gap is
`core`'s server and pipeline glue (roadmap 0003).

## Scope
- In: `internal/*` (`str`, `slice`, `num`, `color`, `crypto`, `runtime`, `ds`), `routing`,
  `versioning`, `ctx`, `exception`, `aggregation`, `common`, and handler injection in `core/fn.go`
  (see Decisions).
- Out: the rest of `core` (server, `handleRequest`, 404 path, lifecycle, production defaults: 0003),
  logging and tracing (0003), middlewares and modules (0004), WebSocket (0005).

## Steps
Every package step is the deep dive of roadmap 0001's Next section: bugs (each proved by a failing
test), hot / cold performance per `.ginject/conventions.md` › Performance, concurrency and security,
tests, benchmarks rewritten (`b.Loop`, `b.ReportAllocs`, `-count 5` + `benchstat` before and after in
the commit), README.

| # | Step | Path | Design | Depends on | Done when | State |
|---|---|---|---|---|---|---|
| 1 | Foundations: `internal/str`, `internal/slice`, `internal/num`, `internal/color`, `internal/crypto` (the per-request UUID: hot spot 2), `internal/runtime` | tdd | docs/design/0001-internal.md | - | deep dive done; package checks green | done |
| 2 | Trie and router: `internal/ds`, `routing`, `versioning` (route match is on every request) | tdd | docs/design/0002-routing.md | 1 | deep dive done; match benchmarks before → after | done |
| 3 | Request context: `ctx` (`Init` and the request ID: hot spots 2 and 4; JSON writing: 3; header writes: 5; query and body binding: 10) | tdd | docs/design/0003-ctx.md | 1 | deep dive done | done |
| 4 | Errors and results: `exception`, `aggregation` (cheaper exceptions and error bodies: part of hot spot 9) | tdd | docs/design/0004-exception.md | 3 | deep dive done | done |
| 5 | Pipeline layers and the route DSL: `common` (DSL parsing is cold; layer dispatch, the catch chain and interceptor closures are hot: hot spots 9 and 11) | tdd | docs/design/0005-common.md | 2, 3, 4 | deep dive done | done |
| 6 | Handler injection: `core/fn.go` (argument resolution and `reflect.Value.Call` per request: hot spot 1, the largest) | tdd | docs/design/0006-injection.md | 3, 5 | deep dive done for `fn.go`; the rest of `core` untouched | done |
| 7 | Measure: rerun `make bench-compare` and the load test, compare with the baseline | light | - | 1-6 | `docs/review/http-0002.md` with before → after per scenario | done |

## Decisions
- Breaking the public API is allowed; the architecture stays (Module → Controller → Provider, the
  layers, the request cycle). Designs mark any change to them for approval.
- Handler injection (`core/fn.go`) moves forward from roadmap 0003: it is the largest hot spot
  (17 % of CPU, about a third of the allocated objects), and the other steps can't be judged without
  it. The rest of `core` stays in 0003.
- Goals, not gates (measured in step 7): allocations per request halved in every scenario, and the
  Static scenario under 1 µs. The full target (Static under 500 ns and 4 allocations, every scenario
  within 1.5× of net/http) is for the end of 0003.
- Heavy techniques (`unsafe`, `sync.Pool`, hand-managed buffers, inlining restructures) appear only
  in a design, with the benchmark that justifies them; approving the design approves exactly those
  uses.
- Designs: three architect calls with Fable (steps 1-2, 3-4, 5-6); the user approves all six before
  `run`.
- The run commits on `master`, one commit (or a few) per step.
- Handler injection may report an unresolvable handler argument at `Create()` (a bootstrap panic)
  instead of on the first request to that route: approved by the user (2026-11-16). Handler
  signatures, injectable types and `Transform` order stay as they are.

## Log
- 2026-11-16: for this roadmap only, the user delegated design approval to Claude (the user was away).
  Heavy techniques are not self-approved: they are built without, and proposed with their benchmark
  for the user to decide. Every later roadmap returns to the normal flow (the user approves designs).
- Designs 0001-0006 approved under the delegation, decisions recorded in each file. Deferred heavy
  techniques for the user: 0002 H1 (caller-supplied parameter buffer), 0003 H1 (scratch buffer and
  kept encoder per context), 0003 H2 (shared read-only header slices), 0003 H3 (capacity kept across
  requests), 0004 H1 (hand-written error-body JSON). Run started from `b2d1070` on `master`.
- Step 1: done. Two lint issues in the tester's crypto tests (unchecked `UUID()` error in a
  benchmark, a De Morgan form) were fixed by the orchestrator with no change in what they assert.
  `F64ToAnyNum(+Inf, Float32)` now returns +Inf (only finite values beyond `MaxFloat32` saturate).
- Step 2: done. `Router.Match` static 271 → 39 ns (1 → 0 allocs), param 603 → 203 ns (2 → 1
  alloc; the remaining one is what H1 would remove), miss 240 → 66 ns (1 → 0). End to end: one
  allocation fewer per request (two on 404); time within the laptop's noise. No `core` test needed a
  change. The orchestrator fixed three unchecked `Insert` errors in a tester benchmark (errcheck).
- Step 3: done. End to end (`make bench`): Static 1.73 → 1.07 µs, Param 3.74 → 2.56, Query 5.85 →
  4.26, JSONBody 7.81 → 5.96, Chain 5.29 → 4.20, Exception 5.65 → 4.37, NotFound 3.39 → 2.41
  (geomean −28 % time, −15 % allocs). The tester fixed three of its own assertions (a FieldLevel
  count the design contradicts, an escaped-character setup) and four test lint findings. Builder
  deviations: one `defer` in `storeFile` (needed by B22); `Text` formats through a helper so `go
  vet` doesn't treat it as a printf wrapper. Proposal for later: typed number helpers in
  `internal/num`, since `F64ToAnyNumOK` returns `any` and allocates for floats and large ints.
- Step 4: done. Exception scenario 5.66 → 4.35 µs, 843 → 386 B, 27 → 12 allocs; constructors
  ~15 ns, 0 allocs; `Pipe` with two operators 81 → 12 ns, 0 allocs. `Error()` 1.9 → 8.6 ns (status
  text looked up per call, as designed). The tester split one E9 case: boxing an options value at
  a call site whose result escapes allocates at the caller, so that case is measured with a local
  result.
- Step 5: done. Chain 5.29 → 4.04 µs (33 → 25 allocs), Exception 3.90 → 3.52 µs; every route one
  allocation fewer (the boxed `WithValueKey` lookup is gone). Guard / interceptor / filter calls
  ~400 → ~4 ns. Interceptor middleware 95 ns, 2 allocs (design target 40 ns; the 192-byte
  aggregation and the nil-reset list, since 0003 H3 is deferred). `Construct` 41 → 223 ns (cold,
  `DeepEqual`). WS files of `common` and `core/read_loop.go` changed only for the shared API; the
  tester updated `core/read_loop_test.go` to the approved `BuildWSInterceptMiddleware` signature and
  added the design's three end-to-end `core` tests. `sample/shop` got its own context key type.
- Step 6: done. Handler plans built at `Create()`: Invoke_QueryPipe 1,537 → 350 ns (11 → 2
  allocs). End to end (10 alternating runs): Static 1.61 → 1.22 µs, Param 3.80 → 3.04, Query
  6.35 → 4.34 (37 → 28 allocs), JSONBody 8.99 → 6.46, Chain 4.57 → 4.03, Exception 4.01 → 2.70;
  geomean −22 %. FiveBuiltins misses the design's < 550 ns (738 ns). Chain uses about 120 B more
  per request with the same allocation count (suspected: `reflect.Call` copying the large
  controller receiver; unconfirmed, for 0003). Found for 0003: `SERVE_<x>_ANY` compiles to the
  glob `/<x>*/`, so it never serves `/<x>/...` — pre-existing (verified on `f036f50`).
- Step 7: done, `docs/review/http-0002.md`. Static 1,672 → 793 ns (goal under 1 µs: met);
  allocations halved only on Exception (−70 %; others −26 to −39 %); every scenario 48-65 % faster.
  The load test ran on a thermally throttled laptop, so it is reported relative to net/http.
- Review (`ginject-reviewer`, all commits of the run): no high-severity regression. Fixed: string
  binding still saturated some out-of-range numbers (decision 0001 Q3); routes differing only in
  parameter names registered silently and the served one changed per start (pre-existing); a nil
  `*Exception` passed to `JSON` leaked runtime text; a literal `$` route segment became an unnamed
  parameter; doc examples (`/:id`, `common/rest.go`); the report's Exception baseline footnoted; a
  README for the lookalike fixture.
