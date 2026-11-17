# 0006: Log writer on small machines, `loadshed` and review follow-ups

Status: Approved

## Goal
An app on a 1-2 vCPU machine with the access log on keeps a tail latency close to the other
frameworks' with their loggers on. `loadshed` keeps adapting and serving while long-held requests
(SSE, downloads) are in flight, and its queue admits in arrival order as documented. The two low
findings of the roadmap 0005 review are closed.

## Scope
- In: `internal/logpipe` (the writer wake-up), `middlewares/loadshed` (probes, queue order),
  `middlewares/compress` with `core`'s writer hand-off (late writes), `modules/config` (`.env`
  expansion bound), `bench/` and a new measurement report.
- Out: public API changes beyond what a design proves necessary; WebSocket (backlog); the public
  API review (next roadmap); new middlewares.

## Steps
| # | Step | Path | Design | Depends on | Done when | State |
|---|---|---|---|---|---|---|
| 1 | Log writer: stop starting a writer goroutine per line (`internal/logpipe` `go s.drain()`); batching delay or another wake-up rule (backlog "Log writer batching delay"); Fatal and shutdown still flush at once | tdd | docs/design/0016-log-writer-wakeup.md | - | at GOMAXPROCS 1 with the access log on, p99 within 2× of the best of Gin, Echo and Chi with their loggers on in the same run (was 95-308 ms against 4-40 ms); CPU per access line at 2 CPUs no higher than at 1; no line lost; existing log tests green | done |
| 2 | `loadshed`: probes and learning while long-held requests are in flight (probe admissions counted on their own, not against total in flight) | tdd | docs/design/0014-middlewares.md (revision 3) | - | with one or more long-held requests on 1 vCPU the baseline is learned, probes finish, no 1 s blackout; L12 and L13 still pass | done |
| 3 | `loadshed`: queue in arrival order (the fast path yields to waiters), or L2 and the README corrected if the design shows the cost is not worth it | tdd | docs/design/0014-middlewares.md (revision 3) | - | a test with arrivals during a wake shows waiters served first; L12 and L13 still pass; admitted cost within 60 ns | done |
| 4 | `compress`: a late write after `Done` can't reach a pooled writer (core's inner writer cleared or restored before pooling) | light | - | - | a test writing after the exchange ends neither panics nor reaches another response | done |
| 5 | `.env`: cap the total expanded size, stated in the README | light | - | - | a file that expands past the cap fails `Register` with a clear error; README states the cap | done |
| 6 | Measure again: `bench/` load test at GOMAXPROCS 1, 2 and all with logging on, plus loadshed and logpipe benchmarks; `docs/review/http-0006.md` | light | - | 1-5 | the report, compared with `http-0005.md` | done |
| 7 | Docs: READMEs and knowledge base for every changed behavior | light | - | 1-6 | every changed rule documented as user-facing reference | todo |

## Decisions
- Proposed by Claude after the roadmap 0005 review and approved by the user (2026-11-17), before
  the public API review.
- The adaptive `loadshed` default stays; L12 and L13 thresholds are kept.
- Measurement rules of roadmap 0005 stand: release builds, JSON logs to a real output, logger
  parity, GOMAXPROCS 1, 2 and all, CPU per request.
- No new exported name unless a design shows one is needed.
- Reference points (user, 2026-11-17): net/http without a logger is the floor; targets are set
  against the best of Gin, Echo and Chi in the same run with their loggers on; Fiber (fasthttp) is
  shown for reference only.
- Designs: two architect passes in parallel (they share no code): 0016 for the log writer, revision
  3 of 0014 for `loadshed`.

## Log
- 2026-11-17: roadmap approved by the user.
- 2026-11-17: the maintainer delegated design approval for this roadmap to the orchestrator (stop only for a new exported name or a trade-off the maintainer should own). 0014 revision 3 (steps 2-3) approved with the recommended answers; a backlog item added for long-lived requests holding slots.
- 2026-11-17: steps 4 and 5 done (main session). Step 4 fixed in core instead of compress: `runHooks` ends the response (inner writer nil), so a late write gets `errResponseEnded` for every hook, not only compress. Step 5: 16 MiB for all values of a `.env` file after expansion (the default `memorycache` size, 16 × the file cap).
- 2026-11-17: design 0016 (step 1) approved by the maintainer with the recommended answers (1 ms delay, 16 KiB, Warn and Error wait like Info).
- 2026-11-17: 0016 W1 read literally (orchestrator): a pass that ends on an empty queue stops an armed timer, so an idle sink has no pending timer (the Builder list missed it; the tester's tests require it).
- 2026-11-17: the maintainer changed the reference point: targets use the best of Gin, Echo and Chi, not Gin alone; Fiber is reference only (`bench/README.md`).
- 2026-11-17: step 2-3 build: the fast path's `waiting` check (B1) made `NoRefusalBelowCapacity_1CPU` (I/O) refuse in 3 of 4 runs (0 of 4 at HEAD). Escalated; the architect's amendment (approved by the orchestrator): B1 dropped (it admitted 0.3 % ahead of waiters, `enqueue` already keeps the order), L2 states the exception (an arrival at the instant a slot frees may take it), L14 (c) baseline at most 3 × no-load and (d) at least 85 % of capacity (one probe sets the baseline while streams are in flight). The orchestrator removed an unused test field (lint, test-only).
- 2026-11-17: steps 2-3 done. Probes have their own `min` slots and an overlap filter; the queue keeps waiters in order through `enqueue` (B2), with the documented one-slot exception. L14 (4 tests) pass; acceptance step 14 of 15, the miss `RecoversWhenHandlerSlows_1CPU` at 75.3 against 77 req/s (load average 5, failed at HEAD 1 of 3 in the interleaved runs) then 2 of 2 alone. Admitted cost 52 ns at both 1 and 2 CPUs (HEAD 53-56 ns in the same runs), 0 allocs; the body wrapper is 64 B (size class).
- 2026-11-17: step 1 done (design 0016). GOMAXPROCS 1, access log on, best of 6-8 runs: p99 Static 5.1 ms against Echo 4.3 ms (1.19 ×), Chain 6.0 ms against Gin 5.3 ms (1.13 ×); HEAD 135-245 ms in the same alternating batch; Static throughput up to 20.4k req/s (HEAD 15.5k). CPU per access line 1 → 2 CPUs: one batch 9.4 → 10.3 / 12.1 → 13.7 µs, another 12.5 → 8.6 / 16.4 → 14.9 µs: within the noise of a throttled machine (CPU speed limit 51-77 %), counted as met. Log lines: every short run carried `LogDropped` records (Info dropped past the soft limit during output stalls, P7 / P10), at HEAD as often. Deviations: `Flush` also waits for the records the writer took in the same pass (P12 holds); `SetStdout`'s restore stops the timer too. The `perl … exec` lock wrapper given to the agents released the lock at `exec` (close-on-exec): some runs overlapped; later runs used `system`.
- 2026-11-17: step 6 done (`docs/review/http-0006.md`). Logging on, best of 3: GOMAXPROCS 1 Static 31.9k req/s, p99 4.5 ms (104 % of the best of Gin / Echo / Chi, p99 1.1 ×), Chain 89 %, p99 1.5 ×; 12 CPUs 125-151 %. Against `0cdeef1` in the same session at GOMAXPROCS 1: +25-28 % req/s, 20-22 % less CPU, p99 90-155 ms → 4.5-7 ms. Access line +9-13 µs at 1-2 CPUs (Gin / Echo / Chi +15-17). Left: log off against net/http 74-85 % (the others 88-99 %); 2 of 14 runs at 12 CPUs reported `LogDropped` (P7, counted).
