# 0016: Log writer wake-up (addendum to 0012)

Status: Approved (maintainer, 2026-11-17: open questions 1-5 as recommended — 1 ms and 16 KiB, Warn and Error wait like Info, snapshot counters, Async_1 artifact accepted, up to 1 ms more lost on an unclean exit, documented)
Packages: `internal/logpipe`, `internal/inspect` (README table only). Roadmap:
`docs/roadmap/0006-log-writer-and-loadshed-fixes.md`, step 1. Amends design 0012: rule P6, the
"Writer goroutine" paragraph of its Concurrency section, the sink rows of its idle-cost table and
X3. Every other 0012 rule (P1-P5, P7-P13, O1-O4, M1-M5, X1, X2, X4) is unchanged.

## Goal
With the access log on, a 1-vCPU app keeps a tail latency close to Gin, Echo and Chi with their
loggers on. To get there, the stdout sink wakes its writer about once per millisecond (or per
16 KiB) instead of once per line. Fatal, `Flush` and shutdown still write everything at once. An
idle sink still runs nothing. Nothing changes for users except that a line reaches stdout up to
1 ms later.

## Problem
`docs/review/http-0005.md` › "The GOMAXPROCS 1 tail". At GOMAXPROCS 1, 64 connections, Static,
JSON access log to a file, the p99 is 95-308 ms against 4-40 ms for the other frameworks with their
loggers on. The cause is in `Sink.Write` (`internal/logpipe/sink.go`). Every record that finds no
writer running starts one with `go s.drain()`. At GOMAXPROCS 1 the writer ends after each line,
because nothing else runs while it writes. So nearly every access line starts a goroutine (trace:
2.0 new goroutines per request against 1.0 with the log off).

A new goroutine goes into the P's `runnext` slot. That slot was holding the `net/http` background
reader that `serve` had just started for the same connection, so the reader moves to the tail of
the run queue, behind every other ready connection. When the handler returns, `finishRequest`
waits for that reader in `abortPendingRead`: 2.4 ms at p50, 75 s of blocked time in 2 s of trace.

Any wake-up made from the request goroutine has this effect. A channel send or `sync.Cond.Signal`
to a parked goroutine calls `goready`, which also uses `runnext`. So the fix has to wake the writer
less often, not differently.

## Decision
A **delayed wake** (the backlog's "Log writer batching delay", made precise):

- In asynchronous mode, the record that makes an empty queue non-empty arms one reusable timer
  (`time.AfterFunc`) for `wakeDelay` = 1 ms. When the timer fires, its goroutine becomes the
  writer.
- The writer starts at once, without waiting for the timer, when the queue holds `wakeSize` =
  16 KiB or more, when a Fatal record is accepted, or when a `Flush` waits.
- After writing what it took (one swapped buffer), the writer writes again at once only if
  `wakeSize` more has queued or a `Flush` waits. Otherwise it re-arms the timer for what is left and
  ends. Under steady load that is one goroutine and one `write` system call per millisecond, or
  per 16 KiB, whichever comes first.
- Nothing runs while the queue is empty: no goroutine and no pending timer. The only cost is one
  stopped `*time.Timer` kept by a sink that has been used asynchronously.

## Measurements
Prototyped in a scratch copy of the repository; nothing was written to the repository itself.
Each wake-up rule was a branch of `Sink.Write` and `drainLocked`, picked by an environment variable
read at start (scratch only). Measured on the same i7-9750H laptop as
`http-0005.md` (12 logical CPUs, Go 1.27.4, darwin/amd64) with the same methods:
`bench/load.sh` release build (`-trimpath -ldflags="-s -w"`), JSON access log to a file on disk,
64 connections, `wrk` with 4 threads, 3 s runs. Runs went in batches of 4-6 that alternated the
variant order between rounds (the machine was shared with another measurement through a lock),
and each batch started only at `CPU_Speed_Limit = 100`. Gin with its JSON logger, and both
frameworks with logging off, ran in the same batches. Runs that ended below a 100 % limit, and two
batches that ran throttled to 22-31 %, are left out. The machine was busier than for
`http-0005.md` (load average about 4: an editor, a browser and another agent's tests). Compare
cells only within this table, not with the report's absolute rates.

Variants (all with the 0012 limits and buffers):

- **base:** today, `go s.drain()` when no writer runs.
- **park:** one long-lived writer parked on a channel, woken per burst.
- **inline:** the caller writes the queue itself under the lock when no writer runs.
- **yield:** the delayed wake, plus a writer that stays alive while the P is busy. It calls
  `runtime.Gosched()` between passes (the global run queue, no `runnext`) and ends when a yield
  returns within 20 µs or finds the queue empty.
- **delay:** this design, 1 ms / 16 KiB.
- **delay 4 ms:** the same, 4 ms / 16 KiB.

Every cell is the best of 2-6 runs (requests per second, lowest p99 and lowest CPU taken
separately). "Line cost" is the CPU per request minus the same framework's log-off CPU in the same
rounds: Owlkei Go 25.9 / 30.6 µs (1 core, Static / Chain) and 29.8 / 34.8 µs (2 cores), Gin 23.3 µs
(1) and 22.6 µs (2).

| GOMAXPROCS | Scenario | Variant | Requests/s | p99 (best, all runs) | CPU / request | Line cost | Writer wakes / line |
|---|---|---|---|---|---|---|---|
| 1 | Static | Gin | 28.8k | 8.2 ms (8.2, 23.6, 65.8) | 34.2 µs | +10.9 µs | - |
| 1 | Static | base | 23.4k | 90.6 ms (90.6, 92.2, 104.8) | 42.5 µs | +16.6 µs | 0.97 |
| 1 | Static | park | 22.9k | 97.2 ms | 43.4 µs | +17.5 µs | 0.97 |
| 1 | Static | inline | 18.3k | 8.7 ms | 54.0 µs | +28.1 µs | 1.00 |
| 1 | Static | yield | 30.8k | 5.2 ms (5.2, 28.1) | 32.3 µs | +6.4 µs | 0.02 |
| 1 | Static | **delay** | 28.3k | **6.1 ms** (6.1, 16.4, 35.4) | 34.6 µs | **+8.7 µs** | 0.033 |
| 1 | Static | delay 4 ms | 27.4k | 8.9 ms (8.9, 45.4) | 34.0 µs | +8.1 µs | 0.021 |
| 1 | Chain | Gin | 25.0k | 5.6 ms (5.6, 6.9, 69.0) | 39.4 µs | - | - |
| 1 | Chain | base | 18.7k | 108.7 ms (108.7, 139.8, 183.1) | 53.2 µs | +22.6 µs | 0.95 |
| 1 | Chain | park | 17.6k | 121.5 ms | 55.8 µs | +25.2 µs | 0.96 |
| 1 | Chain | inline | 13.6k | 58.5 ms | 70.1 µs | +39.5 µs | 1.00 |
| 1 | Chain | yield | 19.6k | 10.0 ms (10.0, 54.1) | 48.5 µs | +17.9 µs | 0.02 |
| 1 | Chain | **delay** | 24.2k | **6.0 ms** (6.0, 15.8, 19.2) | 41.1 µs | **+10.5 µs** | 0.038 |
| 1 | Chain | delay 4 ms | 21.4k | 8.7 ms (8.7, 52.0) | 45.9 µs | +15.3 µs | 0.033 |
| 2 | Static | Gin | 51.5k | 6.8 ms (6.8, 12.8, 53.7, 65.3) | 38.3 µs | +15.7 µs | - |
| 2 | Static | base | 50.0k | 3.7 ms (3.7, 4.5, 34.1, 40.5) | 39.6 µs | +9.8 µs | 0.32 |
| 2 | Static | park | 46.5k | 4.7 ms | 42.3 µs | +12.5 µs | 0.32 |
| 2 | Static | inline | 39.0k | 4.1 ms | 50.8 µs | +21.0 µs | 0.83 |
| 2 | Static | yield | 54.8k | 3.3 ms | 36.0 µs | +6.2 µs | 0.02 |
| 2 | Static | **delay** | 51.1k | **3.7 ms** (3.7, 3.8, 8.2, 88.6) | 37.6 µs | **+7.8 µs** | 0.025 |
| 2 | Static | delay 4 ms | 51.1k | 3.7 ms (3.7, 4.3, 7.2, 25.0) | 38.5 µs | +8.7 µs | 0.018 |
| 2 | Chain | Gin | 43.1k | 4.5 ms (4.5, 5.4, 5.8, 37.1) | 44.8 µs | - | - |
| 2 | Chain | base | 32.4k | 6.8 ms (6.8, 7.2, 13.3, 24.7) | 60.0 µs | +25.2 µs | 0.35 |
| 2 | Chain | park | 35.2k | 5.8 ms | 55.3 µs | +20.5 µs | 0.32 |
| 2 | Chain | inline | 28.6k | 6.3 ms | 68.5 µs | +33.7 µs | 0.80 |
| 2 | Chain | yield | 35.3k | 44.5 ms | 52.2 µs | +17.4 µs | 0.02 |
| 2 | Chain | **delay** | 43.3k | **4.3 ms** (4.3, 4.5, 6.8, 37.8) | 44.5 µs | **+9.7 µs** | 0.037 |
| 2 | Chain | delay 4 ms | 41.2k | 4.2 ms (4.2, 4.4, 47.5) | 48.0 µs | +13.2 µs | 0.029 |
| all (12) | Static | Gin | 40.7k | 9.1 ms (9.1, 11.1, 12.9, 15.1) | 103.6 µs | - | - |
| all (12) | Static | base | 80.3k | 3.4 ms (3.4, 3.8, 11.8, 40.7) | 87.0 µs | - | 0.11 |
| all (12) | Static | yield | 71.8k | 5.2 ms (5.2, 10.7, 53.0) | 81.1 µs | - | 0.02 |
| all (12) | Static | **delay** | 81.8k | **3.4 ms** (3.4, 3.5, 6.0, 21.9) | 85.8 µs | - | 0.020 |
| all (12) | Static | delay 4 ms | 80.8k | 3.4 ms (3.4, 3.5, 7.2, 31.2, 42.9, 44.5) | 83.3 µs | - | 0.016 |

Reading the table:

- **The roadmap targets are met by `delay`.** At GOMAXPROCS 1 the p99 is 6.1 ms (Static) and
  6.0 ms (Chain) against Gin's 8.2 ms and 5.6 ms, where `base` is 91-109 ms. The line costs
  +8.7 / +10.5 µs at one core and +7.8 / +9.7 µs at two, so two cores are no higher than one
  (`base`: +16.6 / +22.6 µs and +9.8 / +25.2 µs). The line now costs less than Gin's on both
  core counts, although it is two to four times longer.
- **Writer wakes fall from about one per line to one per 25-50 lines**, and output `Write`
  calls fall with them (`writes / line` equals `wakes / line` within 0.001 for `delay`).
- **Single-run p99s are noisy on this machine for every framework.** Gin itself ranges from
  5.6 to 69 ms in one cell. Only best-of-runs comparisons hold, which is the method of
  `http-0005.md`. The acceptance run (Builder list, item 5) uses it too.
- **Twelve cores:** `delay` equals `base` (81.8k / 80.3k requests per second, 3.4 ms) and stays
  about twice Gin's rate.

**Lines written.** In every `delay` run the log file held at least one line per request `wrk`
counted, 0.5 s after the run, without a flush. One `delay 4 ms` run at GOMAXPROCS all and one
`yield` run there ended up to 1,262 lines short. The 4 ms run's log held a `LogDropped`
record: Info records past `softLimit` during an output stall, counted and reported (P7, P10),
not lost. A separate stress test of the prototype found no stuck record: 200 rounds of 24
writers at GOMAXPROCS 12 against a 20 µs output, with no `Flush`, accounted for every record as
written or reported dropped within 500 ms, in `base`, `delay` and `delay 4 ms`. The existing
`internal/logpipe`, `log` and `accesslog` tests pass under `-race` with the `delay` prototype
unchanged.

**Micro-benchmarks** (`internal/logpipe`, 5 alternating runs, `-cpu 1,2`, `base` → `delay`):
`BenchmarkSink_Write_Async_1` 56.5 → 72.1 ns (+28 %, p = 0.008) and 85 → 189 B/op at GOMAXPROCS
1. The other nine rows are unchanged (p ≥ 0.095); `Async_12-2` drops 30 % → 12 % of its records.
`Async_1` is one goroutine in a tight loop: the writer only runs at preemptions, so a burst grows
the queue past 64 KiB, and that buffer is released and grown again (the amortized bytes the README
already explains). The loop never yields, unlike a handler. Under load the same path costs 8 µs
less per request (above). Open question 4.

## Rejected options
| Option | What it is | Measured | Why not |
|---|---|---|---|
| **Keep today's wake (`base`)** | `go s.drain()` per idle-to-busy edge | p99 91-109 ms at 1 core; line +16.6 / +22.6 µs | The problem |
| **Long-lived parked writer (`park`)** | One goroutine parked on a 1-slot channel, woken when the queue becomes non-empty | 97 / 122 ms p99 at 1 core, the same as `base` | `goready` also takes `runnext` (`runtime.ready(gp, …, next=true)`), as `http-0005.md` predicted. It also keeps a goroutine and its stack alive while idle |
| **Write inline when uncontended (`inline`)** | The caller drains the queue under the lock when no writer runs (Gin's model) | p99 8.7 ms on Static but 58.5 ms on Chain at 1 core; 18.3k / 13.6k requests per second; line +28 / +40 µs, the most expensive | One `write` system call per line on the request goroutine. It also breaks P5 and P13: with a stuck stdout (a full pipe), every request that logs blocks forever. That is the hang 0012 exists to prevent |
| **Yielding writer (`yield`)** | The delayed wake, plus a writer that stays runnable through `runtime.Gosched()` while the P is busy, so it never needs a `runnext` wake under load | Best p99 and CPU at 1 core on Static (5.2 ms, +6.4 µs), but 10-54 ms and +17.9 µs on Chain, and 44.5 ms on Chain at 2 cores; one run at GOMAXPROCS all came up 1,262 lines short | Erratic across scenarios, and it depends on scheduler details (global-queue polling every 61 ticks, `wakep` in `Gosched`, a 20 µs "P is idle" guess). It needs a busy-check heuristic that is hard to test deterministically, and a runnable goroutine while busy. The gain over `delay` is inside the noise |
| **`runtime.Gosched()` in the caller or at the writer's start** | Yield right after `go s.drain()`, or as the writer's first statement | Not built | The background reader was already moved to the tail of the run queue when the `go` statement ran. Yielding afterwards cannot put it back in `runnext` |
| **Longer delay (`delay 4 ms`)** | The same design, 4 ms | No better than 1 ms in any cell (p99 8.9 / 8.7 ms at 1 core against 6.1 / 6.0), wakes / line 0.02-0.03 because the 16 KiB size rule fires first | Four times the log latency and X3 window for no measured gain (Open question 1) |
| **Netpoll wake-up** | The writer parked in a read of an `os.Pipe`; the waker writes one byte. Netpoll wakes go through `injectglist`, which never uses `runnext` | Not built | Two file descriptors per sink, a system call per wake, delivery only when the scheduler polls the network (up to 10 ms through `sysmon` under load), and pipes are not pollable on Windows. Too much mechanism for what the delay already achieves |
| **Size-only trigger (no timer)** | Wake only at 16 KiB; flush the rest on the next record | Not built | A quiet app would keep its last lines queued until the next request or the shutdown: it breaks liveness (W11) without a timer anyway |

## Public API
None. No exported name is added to any public package.

Internal (`internal/logpipe`, importable inside the module only):
```go
func (s *Sink) Snapshot() inspect.Snapshot // implements inspect.Snapshotter (W13)
```
`internal/logpipe` imports `internal/inspect`. `inspect` imports nothing from the module, so there
is no cycle.

Unexported additions:
- The constants `wakeDelay = time.Millisecond` and `wakeSize = 16 << 10`.
- `Sink` fields: `timer wakeTimer`, `armed bool`, `flushing int`, `wakes uint64`, `writes uint64`,
  and the test seam `newTimer func(d time.Duration, f func()) wakeTimer` (default: a
  `time.AfterFunc` timer).
- `type wakeTimer interface{ Reset(d time.Duration) bool; Stop() bool }` (`*time.Timer` satisfies
  it).

`New` sets `newTimer` to the real timer. Tests build a sink with `New` and then replace
`newTimer` (the same package-internal seam pattern as `now` and `after`).

## Behavior
Terms are those of 0012: the *queue* is the records accepted and not yet taken by the writer, the
*writer* is the one goroutine that passes records to the output, and a *pass* is one swapped buffer
written in batches of at most `batchSize`. *Arming* means calling the timer's `Reset(wakeDelay)`,
creating the timer through `newTimer` on first use. The rules apply to asynchronous mode unless
they say otherwise.

### Waking the writer
- **W1** An asynchronous sink whose queue is empty has no writer goroutine and no armed timer,
  before and after any number of bursts. `Snapshot` reports `writing` false and `timer_armed`
  false.
- **W2** A Debug, Info, Warn or Error record accepted into an empty queue, with no writer
  running, arms the timer once. The output gets no `Write` until the timer fires. When it fires,
  the output gets one `Write` holding every record accepted so far, in order (up to `batchSize`
  each, as P6).
- **W3** A record accepted while the timer is armed neither arms it again nor moves its deadline.
  The first record's deadline holds: a record waits at most `wakeDelay` for the wake, plus the time
  to write what is queued before it. With the fake timer, `Reset` is called once per burst.
- **W4** A record accepted with no writer running that leaves `wakeSize` bytes or more queued
  starts the writer at once, whether or not the timer is armed. One byte less does not. Boundary
  test: records totalling `wakeSize − 1` bytes cause no `Write` before a fire; one more byte causes
  one.
- **W5** A Fatal record starts the writer at once in both modes (as today; X1's flush follows).
- **W6** `Flush` with records queued and no writer running starts the writer at once. While at
  least one `Flush` waits, the writer does not stop for the delay (W7): it writes until the queue
  is empty. `Flush` returns `true` without the timer firing. `StopAsync` flushes the same way.
- **W7** At the end of a pass:
  - If the queue is empty, the writer ends as today (drop report first, P10).
  - If records are queued, less than `wakeSize` bytes, and no `Flush` waits, the writer ends and
    arms the timer if it is not armed. Those records are written by the next fire, the size rule
    or a flush.
  - If `wakeSize` bytes or more are queued, or a `Flush` waits, the writer takes the next buffer
    at once.

  Test with `gateWriter`: hold the first `Write`, accept 3 small records, release. The output has
  1 `Write` and the timer is armed. Fire it: a second `Write` holds the 3 records.
- **W8** A fire that finds the queue empty, or a writer already running, does nothing: no `Write`
  and no goroutine left behind. A fire can be stale (`Stop` or `Reset` raced with the timer). It
  then starts the writer at most early, never twice, and never loses the next deadline.
- **W9** Synchronous mode (P4) never arms the timer: every record is written before `Write`
  returns, as today.
- **W10** When the last `StopAsync` makes the count zero, it stops the timer and clears `armed`.
  If records are still queued (accepted between its flush and the decrement) and no writer runs,
  it starts the writer, which drains to empty because the sink is synchronous. A queued record
  never waits for a timer that was stopped. `SetStdout`, which makes the stdout sink
  synchronous, does the same after its flush. Test: with the gate held, accept a record during
  `StopAsync`'s flush (from `onWrite`), release. After `StopAsync` returns, `Flush` returns `true`
  at once and the fake timer was stopped.
- **W11** Liveness: with a working output and no further call, every accepted record is passed to
  the output. With the real timer, one Info record reaches the output without any `Flush`
  (the test waits on `gateWriter.entered` with a 2 s bound).

### Unchanged rules, restated where the wake changes their wording
- **P6 (amended)** One goroutine at most writes to an output at any time. It runs only while it
  writes: it starts by W2, W4, W5 or W6 and ends by W7. It passes whole records only, at most
  `batchSize` bytes per `Write` (one record when that record alone is larger), in queue order.
- **P10 (wording)** "When the queue runs empty" is the end of the pass that empties it. A drop
  always happens with at least `softLimit` queued, so the writer is already running or woken by W4.
- **X3 (amended)** Records lost when the process dies without passing through M2 or X1 also
  include those waiting for the wake: at most `wakeDelay` of records, below `wakeSize` bytes (about
  20 lines at 20,000 lines per second). This adds to "what the writer has not written in the last
  batch". Recovered panics are Error records and are written within the same bound.
- P1-P5, P7-P9, P11-P13, O1-O4, M1-M5, X1, X2 and X4 are unchanged. In particular P13 still holds
  with a stuck output: at most one goroutine is blocked, and a fire while it is blocked does
  nothing (W8).

### Inspection
- **W12** The hot-path cost of an asynchronous `Write` stays one append under the mutex. On top of
  that, at most one `Reset` per burst, and two plain counter increments per wake and per output
  `Write` (under the mutex the writer already holds). Nothing is allocated per record or per wake
  once the timer exists.
- **W13** `(*Sink).Snapshot()` implements `inspect.Snapshotter`. It is safe from any goroutine at
  any time, takes the mutex once, builds a new value, and nothing calls it on the log path. It
  returns `Name` `"logpipe"` and these fields, in this order:

  | Key | Type | Meaning |
  |---|---|---|
  | `wake_delay` | `time.Duration` | `wakeDelay` |
  | `wake_size` | `int` | `wakeSize` |
  | `batch_size` | `int` | `batchSize` |
  | `queue_limit` | `int` | `queueLimit` |
  | `soft_limit` | `int` | `softLimit` |
  | `async` | `int` | the async count (0: synchronous) |
  | `queued` | `int64` | bytes accepted and not yet written |
  | `writing` | `bool` | a writer runs |
  | `timer_armed` | `bool` | the wake timer is armed |
  | `wakes` | `int64` | writers started since the sink was made (timer, size, Fatal, flush) |
  | `writes` | `int64` | `Write` calls made on the output |
  | `dropped_debug`, `dropped_info`, `dropped_warn`, `dropped_error` | `int64` | records dropped and not yet reported |

  `wakes` and `writes` count since the sink was made and never reset. `writes ÷ lines` is the
  batching ratio the devtools show.

## Performance
Hot path: `Sink.Write` in asynchronous mode. The change removes about 0.3 µs (a `go` statement)
and one scheduler wake-up per line. It adds at most one `timer.Reset` (about 50-100 ns) per wake,
which is per millisecond or per 16 KiB, not per line.

| Measure (this design's prototype) | Before | After |
|---|---|---|
| CPU per access line, GOMAXPROCS 1, Static / Chain | +16.6 / +22.6 µs | +8.7 / +10.5 µs |
| CPU per access line, GOMAXPROCS 2, Static / Chain | +9.8 / +25.2 µs | +7.8 / +9.7 µs |
| p99 at GOMAXPROCS 1, Static / Chain (64 connections) | 91 / 109 ms | 6.1 / 6.0 ms |
| Writer wakes and output `Write`s per line | 0.95-0.97 (1 core), 0.32-0.35 (2), 0.11 (12) | 0.020-0.038 |
| `BenchmarkSink_Write_Async_1` (tight loop, 1 core) | 56.5 ns, 85 B/op | 72.1 ns, 189 B/op (Open question 4) |
| Other `BenchmarkSink_Write_*` rows | - | unchanged (p ≥ 0.095) |
| Allocations per record | 0 (amortized buffers aside) | 0; one `*time.Timer` per sink for its lifetime |

Idle and memory (small machines):

| Part | Before (0012) | After |
|---|---|---|
| Sink never used | about 300 B, no goroutine, no timer | the same, plus about 40 B of fields |
| Sink after async use, queue empty | two buffers of at most 64 KiB each, no goroutine, no timer | the same plus one stopped `*time.Timer` (about 100 B); no goroutine, nothing pending, no wake-ups |
| Sink under load | one goroutine started per line at GOMAXPROCS 1 | at most `1 / wakeDelay` + bytes ÷ `wakeSize` writer goroutines per second (about 1,000 + 2,500 at 40 MB/s) |
| Worst case | 2 buffers of about 1 MiB, one goroutine | unchanged |

The queue is never larger than before. A delayed wake only holds records that the old writer would
have taken a moment earlier, and limits P7 / P8 still count them.

No heavy technique is added: no `unsafe`, no `sync.Pool`, no new buffer. H2 (the two swapped
buffers of 0012) is reused as is.

## Concurrency
- **One mutex, as today.** `Sink.mu` also guards `armed`, `flushing`, `wakes`, `writes` and the
  timer value. `Reset` and `Stop` are called with `mu` held. `time.Timer` methods never block, and
  the callback runs on its own goroutine, so this cannot deadlock.
- **The writer flag decides, not the timer.** `writing` stays the only proof that a writer runs.
  Any wake (fire, size, Fatal, flush) sets it under `mu` before writing and returns at once if it
  was already set. `armed` is a hint that avoids a `Reset` per record. A fire clears it under `mu`
  before looking at the queue. A stale fire (the callback already started when `Reset` or `Stop`
  ran) therefore costs at most one early pass (W8).
- **The timer goroutine is the writer.** `time.AfterFunc` runs the callback on a new goroutine.
  The callback takes `mu`, and if the queue is non-empty and no writer runs, it becomes the writer
  itself: no second `go` statement. The size, Fatal and flush wakes keep `go s.drain()`, because
  their caller must not write.
- **Flush and the delay.** `flushing` is incremented under `mu` before `Flush` starts or waits for
  the writer, and decremented when `Flush` returns, timed out or not. The writer reads it at each
  end of pass (W7). A flush that times out leaves the remaining records to the next pass, which
  then follows W7 again.
- **Lock order** is unchanged: a logger's pool → `Sink.mu`. The timer callback takes only
  `Sink.mu`.
- **How goroutines stop.** The writer ends by W7 (asynchronous) or when the queue is empty
  (synchronous). The timer has no goroutine until it fires. The last `StopAsync` (and
  `SetStdout`) stops it and hands any leftover record to a writer (W10). A writer blocked in a
  stuck output is still the one goroutine of P13.
- **Scheduler effect, stated so nobody "fixes" it back.** The timer's goroutine still takes
  `runnext` when it starts, so it delays one request's background reader about once per wake.
  That is one wake per 25-50 lines at GOMAXPROCS 1 (measured 0.033-0.038 wakes per line), instead
  of one per line. This residue is what remains in the GOMAXPROCS 1 p99 above. Waking a parked
  goroutine instead would not help: `goready` also uses `runnext` (`park` above). Netpoll wakes are
  the only ones that don't, and they were rejected.

## Security
- A client controls how many lines are logged and how big they are. The size rule (W4) bounds what
  a burst can hold back: at most `wakeSize` bytes wait for the timer. The P7 / P8 limits, the
  `maxRecord` cut and the 1 MiB bound are unchanged. A flood cannot keep the writer asleep: the
  deadline is set by the first record of a burst and is never moved (W3).
- A flood cannot make the sink start goroutines per line any more. It starts at most
  `1 / wakeDelay` + bytes ÷ `wakeSize` per second.
- Nothing new is read from the environment or from requests. The prototype's switches were
  scratch-only.
- X3 widens by at most `wakeDelay` of records on an unclean exit. Clean exits, `Fatal` and
  shutdown lose nothing (W5, W6, W10).

## Impact
- **Users:** none in the API. In asynchronous mode a line reaches stdout up to about 1 ms later
  than before. Tests that read stdout already stop the app or flush (0012 Impact).
- **Code:** `internal/logpipe/sink.go` (wake rules, timer, `flushing`, counters, `Snapshot`).
  `log`, `core` and `accesslog` are unchanged: they already call `Flush` / `StopAsync` where W5,
  W6 and W10 need them.
- **Tests:** the existing `internal/logpipe` tests stay. `TestSink_AsyncDoesNotWaitForOutput`,
  `TestSink_BatchesWholeRecordsUpToBatchSize`, `TestSink_OneWriterAtATime`,
  `TestSink_WriterEndsWhenEmpty` and the P8 / P10 tests run with the real timer and pass with the
  prototype unchanged (checked under `-race`). New tests use the fake timer (Tester list).
- **Docs (roadmap step 7):** `internal/logpipe/README.md` (Key Features "No idle cost",
  `Write` rules, Limits, Concurrency, Performance notes, Benchmarks, a new Inspection section),
  `internal/inspect/README.md` (a `logpipe` row in the snapshots table, noting it is not a
  middleware), the `log` README's production section ("a line reaches stdout within about 1 ms";
  X3), `.owlkei/metadata-concurrency-model.json` and `.owlkei/metadata-performance.json` (the
  writer's lifetime), and `docs/roadmap/backlog.md` (remove "Log writer batching delay").
- **Measurement (roadmap step 6):** `docs/review/http-0006.md` measures again with the same
  method and compares with `http-0005.md` and with the table above.

## Builder list
1. `internal/logpipe/sink.go`:
   - Add `wakeDelay`, `wakeSize`, `wakeTimer`, and the `Sink` fields above. `New` sets `newTimer`
     to `func(d time.Duration, f func()) wakeTimer { return time.AfterFunc(d, f) }`. The first arm
     calls `s.timer = s.newTimer(wakeDelay, s.fire)`, which arms it. Later arms call
     `s.timer.Reset(wakeDelay)`.
   - `Write`, asynchronous path: after `appendLocked`, if `writing`, return. Else if `level ==
     Fatal` or `len(s.queue) >= wakeSize`, set `writing`, count a wake and `go s.drain()`. Else
     `armLocked()`.
   - `armLocked()`: if `!armed`, set it and `Reset(wakeDelay)` (or create).
   - `fire()`: lock, clear `armed`, return if `writing` or the queue is empty, else set
     `writing`, count a wake and run `drainLocked()` on this goroutine.
   - `drainLocked()`: at the top of every pass after the first, apply W7 when `s.async > 0`. When
     `s.async == 0` (synchronous caller, or after the last `StopAsync`), drain to empty as today.
   - `Flush`: `flushing++` under `mu` before the existing start-or-wait logic, `flushing--` on
     every return path (no `defer` on the hot path; `Flush` is cold, so a `defer` is fine there).
   - `StopAsync` and `SetStdout`: after the flush, when the count reaches 0, `Stop` the timer,
     clear `armed`, and if the queue is non-empty and no writer runs, set `writing` and
     `go s.drain()` (W10).
   - Count `wakes` where `writing` is set by a wake, and `writes` before each `write(out, …)`.
   - `Snapshot()` as W13.
2. No change to `log`, `core`, `accesslog`.
3. Benchmarks in `sink_bench_test.go`: keep the existing ones and add
   `BenchmarkSink_Write_Async_Burst` (bursts of 20 records then an idle gap, the per-burst arm
   cost).
4. Docs as listed in Impact, in step 7 (not in this step), except the `internal/logpipe` README
   rules that the tests cite.
5. Acceptance (roadmap step 1 "Done when"): run `bench/load.sh` as in Measurements, Owlkei Go and Gin
   alternating, at GOMAXPROCS 1 and 2. Record in the roadmap Log: p99 within 2× of Gin's, CPU per
   request at 2 no higher than at 1, and lines ≥ requests.

## Tester list
All in `internal/logpipe` (internal test package), with a `fakeTimer` helper in
`helpers_test.go`. It records `Reset` and `Stop` calls, holds the callback, and has a `fire()`
method that runs the callback synchronously on a new goroutine and waits for it to return (as
`AfterFunc` would). Install it with `s.newTimer = fake.new` right after `New`. No sleeps and no
polling. The barriers are `gateWriter.entered`, `Flush`, and the fake's `fire()` returning.

| Rule | Test | Case |
|---|---|---|
| W1 | `TestSink_IdleAsyncHasNoWriterNoTimer` | start async, 3 bursts each flushed; goroutine count back to the start, fake not armed, `Snapshot` `writing` / `timer_armed` false |
| W2 | `TestSink_FirstRecordArmsTimerNoWrite` | one Info record: 0 output `Write`s, fake armed with 1 ms; `fire()` → 1 `Write` with the record |
| W2 | `TestSink_FireWritesEveryQueuedRecordInOrder` | 50 records then `fire()`: output bytes equal the 50 in order, whole records |
| W3 | `TestSink_ArmedTimerNotResetByLaterRecords` | 10 records: `Reset` / create called once |
| W4 | `TestSink_SizeStartsWriterAtWakeSize`, `TestSink_BelowWakeSizeWaitsForTimer` | totals of `wakeSize − 1` (no `Write` before `fire()`) and exactly `wakeSize` (one `Write` without a fire, seen on `entered`) |
| W5 | `TestSink_FatalStartsWriterAtOnce` | async and sync: Fatal record reaches the output without a fire |
| W6 | `TestSink_FlushWritesWithoutTimer`, `TestSink_FlushDrainsPastDelay`, `TestSink_StopAsyncWritesWithoutTimer` | fake never fired: `Flush` true and output holds everything; gate held during the first pass, 3 more records, release: `Flush` still true with all records |
| W7 | `TestSink_EndOfPassRearmsBelowWakeSize`, `TestSink_EndOfPassContinuesAtWakeSize` | the gate case of W7; the same with `wakeSize` queued during the held pass → 2nd `Write` without a fire |
| W8 | `TestSink_FireOnEmptyQueueDoesNothing`, `TestSink_FireWhileWritingDoesNothing`, `TestSink_StaleFireNoSecondWriter` | `fire()` after a flush; `fire()` with the gate held (`maxInflight` stays 1, `entered` count unchanged); two `fire()`s back to back |
| W9 | `TestSink_SyncNeverArmsTimer` | sync mode, 100 records: fake never created or reset; existing P4 tests unchanged |
| W10 | `TestSink_LastStopAsyncStopsTimer`, `TestSink_StopAsyncWritesRecordAcceptedDuringFlush`, `TestSetStdout_StopsTimer` | armed, then `StopAsync`: `Stop` called, not armed; two starts: the first stop keeps it, the second stops it; the `onWrite` case of W10 |
| W11 | `TestSink_RealTimerWritesWithoutFlush` | real timer, one record, wait on `entered` (2 s bound) |
| W12 | `BenchmarkSink_Write_Async_1` / `_12` (existing), `BenchmarkSink_Write_Async_Burst` | no more allocs per op than before; `-cpu 1,2` |
| W13 | `TestSink_Snapshot_Fields`, `TestSink_Snapshot_CountsWakesAndWrites`, `TestSink_Snapshot_ConcurrentWithWrites` | keys and order; after `fire()` of 10 records `wakes` 1, `writes` 1; `-race`, 8 writers + a snapshot loop |
| P13 | `TestSink_StuckOutputBoundsCallersAndMemory` (existing) + `TestSink_StuckOutputFireAddsNoGoroutine` | gate never opened; `fire()` 5 times: goroutine count +1 at most |
| No loss | `TestSink_ConcurrentWritersWholeRecords` (existing) + `TestSink_RandomFiresNoLossNoReorder` | 16 goroutines × 1,000 records with `fire()` from another goroutine at random points, then `Flush`: every record once, per-goroutine order kept |

## Open questions
1. **`wakeDelay` = 1 ms, `wakeSize` = 16 KiB.** 4 ms measured no better in any cell and widens
   the log latency and the X3 window fourfold. 250 µs would bring back more wakes (up to 4,000 per
   second, about one per 7 lines at 1 core) and was not measured: the load-sharing between the
   lock holders cut the matrix. 64 KiB at 1 ms was measured once (29.6k, 12.2 ms at 1 core;
   52.3k, 3.4 ms at 2 cores): no clear gain, and four times the bytes held back.
   **Recommendation: 1 ms and 16 KiB**, both unexported constants, shown in the snapshot (W13).
2. **Warn and Error wait for the wake like Info.** Alternative: Warn and Error start the writer
   at once. That brings back one `runnext` wake per error line, and an error flood (a failing
   dependency) would put the GOMAXPROCS 1 tail back exactly when the app is in trouble. Being
   1 ms later costs an error line nothing, and Fatal, `Flush` and shutdown still write at once.
   **Recommendation: all four levels wait; Fatal does not.**
3. **The sink implements `inspect.Snapshotter` (W13) with two new counters.** `wakes` and
   `writes` are plain increments under the mutex the path already holds. They let the devtools
   show the batching ratio and spot a regression back to one wake per line. No devtools code
   reads it yet: `core` would reach it through `logpipe.Stdout()` when the devtools roadmap
   lands. **Recommendation: accept.** Alternative: only the parameters and the state, no
   counters.
4. **`BenchmarkSink_Write_Async_1` gets slower (+16 ns, +104 B/op amortized).** It is one
   goroutine that never yields, so the queue grows past 64 KiB between preemptions and its
   buffer is released and grown again. A handler yields, and the load test shows 8 µs less CPU
   per request. Options: (a) accept and document the benchmark as a tight-loop artifact; (b) ask
   the builder to keep the buffers when a pass ends with records left (W7's early end) as well,
   and measure again. (b) touches the memory bound of 0012 (Coordination, "Both buffers kept, up
   to 64 KiB each") only if it keeps a larger buffer. **Recommendation: (a)**, with (b) allowed
   if it stays within the 64 KiB rule and the benchmark shows it.
5. **X3 widens by up to 1 ms of records on an unclean exit** (a crash, `os.Exit` in user code,
   `SIGKILL`). That is about 20 lines at 20,000 lines per second, and never more than 16 KiB
   waiting for the timer. **Recommendation: accept** and state it in the `log` README's
   production section.
