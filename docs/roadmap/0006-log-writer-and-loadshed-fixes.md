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
| 1 | Log writer: stop starting a writer goroutine per line (`internal/logpipe` `go s.drain()`); batching delay or another wake-up rule (backlog "Log writer batching delay"); Fatal and shutdown still flush at once | tdd | docs/design/0016-log-writer-wakeup.md | - | at GOMAXPROCS 1 with the access log on, p99 within 2× of Gin's with its logger on in the same run (was 95-308 ms against 4-40 ms); CPU per access line at 2 CPUs no higher than at 1; no line lost; existing log tests green | todo |
| 2 | `loadshed`: probes and learning while long-held requests are in flight (probe admissions counted on their own, not against total in flight) | tdd | docs/design/0014-middlewares.md (revision 3) | - | with one or more long-held requests on 1 vCPU the baseline is learned, probes finish, no 1 s blackout; L12 and L13 still pass | todo |
| 3 | `loadshed`: queue in arrival order (the fast path yields to waiters), or L2 and the README corrected if the design shows the cost is not worth it | tdd | docs/design/0014-middlewares.md (revision 3) | - | a test with arrivals during a wake shows waiters served first; L12 and L13 still pass; admitted cost within 60 ns | todo |
| 4 | `compress`: a late write after `Done` can't reach a pooled writer (core's inner writer cleared or restored before pooling) | light | - | - | a test writing after the exchange ends neither panics nor reaches another response | done |
| 5 | `.env`: cap the total expanded size, stated in the README | light | - | - | a file that expands past the cap fails `Register` with a clear error; README states the cap | done |
| 6 | Measure again: `bench/` load test at GOMAXPROCS 1, 2 and all with logging on, plus loadshed and logpipe benchmarks; `docs/review/http-0006.md` | light | - | 1-5 | the report, compared with `http-0005.md` | todo |
| 7 | Docs: READMEs and knowledge base for every changed behavior | light | - | 1-6 | every changed rule documented as user-facing reference | todo |

## Decisions
- Proposed by Claude after the roadmap 0005 review and approved by the user (2026-11-17), before
  the public API review.
- The adaptive `loadshed` default stays; L12 and L13 thresholds are kept.
- Measurement rules of roadmap 0005 stand: release builds, JSON logs to a real output, logger
  parity, GOMAXPROCS 1, 2 and all, CPU per request.
- No new exported name unless a design shows one is needed.
- Designs: two architect passes in parallel (they share no code): 0016 for the log writer, revision
  3 of 0014 for `loadshed`.

## Log
- 2026-11-17: roadmap approved by the user.
- 2026-11-17: the maintainer delegated design approval for this roadmap to the orchestrator (stop only for a new exported name or a trade-off the maintainer should own). 0014 revision 3 (steps 2-3) approved with the recommended answers; a backlog item added for long-lived requests holding slots.
- 2026-11-17: steps 4 and 5 done (main session). Step 4 fixed in core instead of compress: `runHooks` ends the response (inner writer nil), so a late write gets `errResponseEnded` for every hook, not only compress. Step 5: 16 MiB for all values of a `.env` file after expansion (the default `memorycache` size, 16 × the file cap).
