# 0001: Cleanup and HTTP baseline

Status: Done

## Goal
Remove gRPC, move to the latest Go and patched dependencies, then measure where HTTP stands: an
end-to-end benchmark of the request cycle, a load test, and a comparison with other Go frameworks.
These numbers are the baseline every later roadmap (0002-0004) is measured against.

## Scope
- In: gRPC and protobuf removal, the Go version, vulnerable dependencies, the HTTP benchmark and
  load test harness, the comparison module.
- Out: optimizing or fixing anything the measurements reveal (roadmaps 0002-0004), WebSocket
  (roadmap 0005), devtools beyond keeping what it inspects today.

## Steps
| # | Step | Path | Design | Depends on | Done when | State |
|---|---|---|---|---|---|---|
| 1 | Remove gRPC and protobuf only: delete `devtool.proto`, the `*.pb.go` files, the empty `Serve` and the `protoc` target. Keep everything devtools already inspects (`EnableDevtool`, the builder, `schema.go`, the snapshot types) as plain Go structs with the same names and fields | light | - | - | no `grpc` / `protobuf` in `go.mod`; `EnableDevtool` builds the same snapshot, covered by a test | done |
| 2 | Go 1.27.4 (latest) in `go.mod` | light | - | 1 | `go.mod` says 1.27.4; `make check` passes with it | done |
| 3 | Upgrade vulnerable dependencies (`golang.org/x/net` ≥ v0.60.0, `golang.org/x/text` ≥ v0.39.0, plus whatever the scan shows then) | light | - | 2 | `govulncheck ./...` reports nothing | done |
| 4 | End-to-end HTTP benchmark through `App`: static route, path params, query + JSON body binding, a global middleware + guard + interceptor chain, an exception path, a 404; with `b.Loop`, `b.ReportAllocs` | light | - | 3 | the benchmark runs with `make bench`; ns/op and allocs/op per scenario recorded | done |
| 5 | Load test against a small app with the same routes: requests per second, p50 / p99 latency, allocations per request, at several concurrency levels | light | - | 4 | `make load` runs it; results recorded | done |
| 6 | Comparison module `bench/` (its own `go.mod`): the same scenarios on `net/http` `ServeMux`, Gin, Echo, Chi and Fiber | light | - | 4 | `make bench-compare` runs it; results recorded | done |
| 7 | Baseline report: the numbers from steps 4-6, the top allocation and CPU sites from profiles (`-cpuprofile`, `-memprofile`), and the hot spots they point to for roadmap 0002 | light | - | 4-6 | `docs/review/http-baseline.md` | done |

## Decisions
- gRPC and protobuf leave; nothing else about devtools changes, so step 1 needs no design.
- Go: the latest release, 1.27.4. Users need Go 1.27 or newer.
- The comparison module in `bench/` has its own `go.mod`, so other frameworks never become
  dependencies of Ginject.
- Measurements run on the user's machine; the report states the machine, the Go version and the
  commit, and every number is the median of `-count 5`.
- This roadmap changes no framework behavior except removing gRPC.

## Next
The rest of the HTTP work, one roadmap each, written after the previous one is merged so its designs
build on the refactored code. Every package step is the same deep dive: bugs (each proved by a failing
test), hot / cold performance per `.ginject/conventions.md`, concurrency and security, tests, benchmarks
rewritten (`b.Loop`, `b.ReportAllocs`, `-count 5` + `benchstat`), README. Each roadmap ends by
rerunning this baseline.

- **0002 Request hot path** (5 designs): `internal/*`; `internal/ds` + `routing` + `versioning`; `ctx`;
  `exception` + `aggregation`; `common` (route DSL cold, layer dispatch hot).
- **0003 Server and production** (2 designs): `core` without WS (DI cold; `http.go` and handler
  injection hot) with production defaults (timeouts, header and body limits, keep-alive, graceful
  shutdown, panic containment, TLS, HTTP/2); `log` + `accesslog` + `trace` + `event`; tests for the
  `devtool` snapshot.
- **0004 Middlewares and modules** (3 designs): `cors` + `csrf` + `helmet` + `throttler`; `config` +
  `cache` + `memorycache` + `httpclient`; `storage` (known data-loss risks in transactions and
  compaction); then a security audit of the HTTP side, `/kb update`, and the final HTTP report.
- **0005 WebSocket**: `core/ws*.go`, `read_loop.go`, `write_loop.go`, `wsevent`, `memorybroker`,
  `pattern`, and Ginject's own WebSocket implementation replacing `golang.org/x/net/websocket`
  (decided 2026-11-16, `docs/roadmap/backlog.md`).

## Log
- 2026-11-16: run started on `roadmap/cleanup-and-baseline` from `9121a5f`. Files already modified
  before the run (`.claude/agents/ginject-builder.md`, `ginject-reviewer.md`,
  `.claude/commands/refactor.md`, `.ginject/conventions.md`, `CLAUDE.md`, `docs/design/README.md`:
  the hot / cold path rules) are left for the user to commit; this roadmap file is committed by the run.
- Step 1: the `make protoc` line in `CLAUDE.md` was removed in the working tree; it is part of the
  user's uncommitted `CLAUDE.md` changes. Found, not fixed (out of scope, for 0003): the snapshot
  builder dereferences `versioning` unconditionally, so `EnableDevtool` without `EnableVersioning`
  panics once a route exists.
- Step 3: with gRPC gone, `golang.org/x/text` is no longer required at all; `golang.org/x/net`
  (WebSocket) went to v0.62.0, the smallest version fixing all seven advisories (two newer than
  v0.60.0). `go.mod` now has a single dependency; `govulncheck`: no vulnerabilities found.
- Step 4: the benchmark lives in the `bench/` module (planned for step 6) instead of the main module,
  so all frameworks share one scenario table (`bench/scenario.go`) and a parity test proves each
  returns the expected status and body. It drives `App.ServeHTTP` with a reused request and a
  discarding `ResponseWriter`, so only the framework's own work is measured.
- Step 6 before step 5, so the load test server can serve every framework. Each framework uses its
  idiomatic recovery middleware (Ginject recovers by default). Fiber is measured on fasthttp's
  `RequestCtx` directly; its `Static` number includes `RequestCtx.Init`.
- Step 5: the load test uses `wrk` (installed locally) against `bench/cmd/server`, which serves any of
  the six frameworks; Ginject runs through `App.Listen`, its production path. Allocations per
  request come from step 4's benchmark (the load test can't see them). Client and server share the
  machine, so absolute numbers are a floor; comparisons between frameworks are fair.
- Review (`ginject-reviewer`, all commits of the run): no correctness, concurrency or security issue.
  Fixed: the Exception scenario now uses each framework's error mechanism (Echo `NewHTTPError`,
  Fiber `NewError`, Gin `AbortWithStatusJSON`) and its row was re-measured; the report's JSON hot spot
  named a `bytes.Clone` that doesn't exist (the copy is inside `encoding/json.Marshal`); `load.sh`
  now fails when the port is taken or its server never answers; Fiber's harness allocation is
  footnoted; `bench/README.md` added. Deferred by plan: READMEs for `core` and `devtool` (roadmaps
  0002-0003). Noted for the release notes: `LayerScope(n).String()` returns `"UNKNOWN"` for unknown
  values, where protobuf returned the number.
