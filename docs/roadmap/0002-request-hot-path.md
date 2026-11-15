# 0002: Request hot path

Status: Running

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
| 1 | Foundations: `internal/str`, `internal/slice`, `internal/num`, `internal/color`, `internal/crypto` (the per-request UUID: hot spot 2), `internal/runtime` | tdd | docs/design/0001-internal.md | - | deep dive done; package checks green | todo |
| 2 | Trie and router: `internal/ds`, `routing`, `versioning` (route match is on every request) | tdd | docs/design/0002-routing.md | 1 | deep dive done; match benchmarks before → after | todo |
| 3 | Request context: `ctx` (`Init` and the request ID: hot spots 2 and 4; JSON writing: 3; header writes: 5; query and body binding: 10) | tdd | docs/design/0003-ctx.md | 1 | deep dive done | todo |
| 4 | Errors and results: `exception`, `aggregation` (cheaper exceptions and error bodies: part of hot spot 9) | tdd | docs/design/0004-exception.md | 3 | deep dive done | todo |
| 5 | Pipeline layers and the route DSL: `common` (DSL parsing is cold; layer dispatch, the catch chain and interceptor closures are hot: hot spots 9 and 11) | tdd | docs/design/0005-common.md | 2, 3, 4 | deep dive done | todo |
| 6 | Handler injection: `core/fn.go` (argument resolution and `reflect.Value.Call` per request: hot spot 1, the largest) | tdd | docs/design/0006-injection.md | 3, 5 | deep dive done for `fn.go`; the rest of `core` untouched | todo |
| 7 | Measure: rerun `make bench-compare` and the load test, compare with the baseline | light | - | 1-6 | `docs/review/http-0002.md` with before → after per scenario | todo |

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
