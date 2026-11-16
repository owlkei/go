# 0012: Logger API, asynchronous log pipeline, panic stack sampling

Status: Approved
Packages: `common`, `log`, `internal/logpipe` (new), `accesslog`, `core`. Roadmap:
`docs/roadmap/0004-trace-context-and-logging.md`, steps 4-6. Depends on design 0011 (the scope in
`context.Context`, see "Contract with design 0011"). Supersedes these rules of design 0010: L6, L7,
A1, A2, A4, A8, A9, A10; and the line shape of 0007 E3 (`UnhandledPanic`, `ErrorAfterResponse`).

## Goal
`s.Logger.From(ctx).Info(…)` writes the line with the IDs of the request, connection or message it
belongs to, with the built-in logger and with any logger given to `SetLogger`. While an app serves,
a log call never waits for stdout: it encodes on the caller and hands the bytes to a bounded queue
that one writer drains in batches; under overload Debug and Info are dropped and counted. A panic
flood costs a bounded amount of log and CPU.

Not reopened (roadmap Decisions and Background): `From` on the injected logger, two interfaces,
IDs as key-value arguments, encode on the caller, one writer per output, drop by level, flush after
the shutdown hooks and before `Fatal` exits, the access log without a worker, no user option.

Measured on Go 1.27.4, darwin/amd64, i7-9750H (12 threads), 3 runs, with throwaway probes outside
the repository (nothing was left in it). Compare numbers inside one table.

## Bugs found
| # | Where | Input | Today | Rule |
|---|---|---|---|---|
| 1 | `log/log.go:152-155`, `:125-127` | `Fatal` while stdout is stuck (a full pipe nobody reads) | The write never returns, `exit` is never reached: the process hangs instead of exiting | X1 |
| 2 | `core/fn.go:231-233`, `core/http.go:539` | Any non-exception panic | `panicStack()` is an argument, so the 4 KiB stack is captured (26 µs, 8 KiB) before the logger checks its level, even with a silent logger | S1-S3 bound it |
| 3 | `accesslog/access_log.go:199`, `:205` | Access log on, no traffic | The worker's ticker wakes the process once per second forever; the queue holds about 120 KiB (`1,024 × flushJob`) from `NewAccessLog` on | A5 |
| 4 | `accesslog/access_log.go:240-300` + `log` | One access line of 6 stages | 16.4 µs, 102 allocs, 7.5 KiB to format (3.0 µs in `lineArgs`, the rest encoding `[]map[string]color.Colored` by reflection). Hidden on the worker today, capped at about 65,000 lines/s, on the same core on a 1-vCPU box | A2 |
| 5 | `core/server.go:363-367` (latent, created by this design) | Startup on a terminal | `logBoostrap` prints the banner straight to `os.Stdout` after logger lines; with a queue the banner could come first | M4 |

## Public API

### `common`
```go
type LogAdapter interface {              // added: today's five methods, what SetLogger accepts
    Debug(msg string, args ...any)
    Info(msg string, args ...any)
    Warn(msg string, args ...any)
    Error(msg string, args ...any)
    Fatal(msg string, args ...any)
}

type Logger interface {                  // changed: gains From
    LogAdapter
    From(ctx context.Context) Logger
}
```

### `core`, `log`, `accesslog` (parameter types only)
```go
func (app *App) SetLogger(logger common.LogAdapter) *App                    // was common.Logger
func (app *App) Logger() common.Logger                                      // unchanged
func NewLog(opts *LogOptions) common.Logger                                 // unchanged
func WrapLogger(next common.LogAdapter, maskFields []string) common.Logger  // was common.Logger
type AccessLogConfig struct { Tracer *trace.Tracer; Logger common.LogAdapter } // was common.Logger
```

| Added | Why it is exported |
|---|---|
| `common.LogAdapter` | Users name it when they write an adapter for another log library; `SetLogger` and `WrapLogger` need a type for "five methods" |
| `common.Logger.From` | The approved API (roadmap Decisions) |

Nothing is removed. Nothing is added to `log`, `accesslog` or `core`. A `common.Logger` is a
`common.LogAdapter`, so every existing call (`SetLogger(log.NewLog(…))`, `WrapLogger(logger, …)`)
compiles unchanged. No clash for `From`: no route or event token, no method of a `common` embed
and no existing method in `core`, `ctx`, `common` or `routing` has that name.

### `internal/logpipe` (new, internal: not public API)
The seam between `log` (which encodes) and `core` (which owns the lifecycle). Listed so the tester
and builder share it:
```go
type Level uint8                                   // Debug, Info, Warn, Error, Fatal
type Sink struct{ … }
func Stdout() *Sink                                // the process-wide sink of os.Stdout
func New(w io.Writer) *Sink                        // a private sink (tests, newLog)
func (s *Sink) Write(level Level, record []byte, report func(dropped [4]uint64) []byte)
func (s *Sink) StartAsync()                        // counted
func (s *Sink) StopAsync(timeout time.Duration) bool // Flush, then undo one StartAsync
func (s *Sink) Flush(timeout time.Duration) bool   // true: everything accepted before the call is written
func SetStdout(w io.Writer) (restore func())       // tests of core only
```
`log` keeps `newLog(out io.Writer, isTerminal bool, opts *LogOptions) common.Logger` (a private
synchronous sink over `out`: existing tests are unchanged) and gains the unexported
`newLogSink(s *logpipe.Sink, isTerminal bool, opts *LogOptions) common.Logger`, which `NewLog`
calls with `logpipe.Stdout()`.

## Behavior

### Interfaces
- **I1** A type with the five methods and no `From` is accepted by `SetLogger` and `WrapLogger`.
  `SetLogger(nil)` and `WrapLogger(nil, …)` panic as today.
- **I2** `app.Logger()` and every injected `common.Logger` field implement `From`, whatever was
  given to `SetLogger`. Only fields of type `common.Logger` are injected; a `common.LogAdapter`
  field is not (one way to get the logger).
- **I3** Calls that do not use `From` behave exactly as before this design (same arguments reach an
  adapter, same bytes from the built-in logger).

### `From`
`fields` is the scope's key-value list from design 0011 (contract C2): alternating keys and values.

- **F1** `From(ctx)` with a nil `ctx`, a `ctx` without a scope, or a scope with no fields returns
  the receiver itself (`l.From(ctx) == l`). It never panics.
- **F2** Otherwise it returns a logger that behaves like the receiver (level, format, mask rules,
  `AddSource`) and adds `fields` to every record. The receiver is not modified.
- **F3** Built-in logger: `l.From(ctx).Info(msg, args...)` writes the bytes that
  `l.Info(msg, args...)` would write, with the fields added after the call's own attributes, in
  the order of `fields`, encoded and masked like any other pair (a field whose key matches a mask
  rule is `[REDACTED]`). For well-formed `args` this equals `l.Info(msg, append(args, fields...)...)`
  byte for byte, the time aside. Malformed `args` (`!BADKEY`) are handled as without `From`; the
  fields never pair with them.
- **F4** Adapter (a logger set through `SetLogger` / `WrapLogger`): the adapter's method is called
  once with `msg` and `fields` followed by the converted and masked `args` (fields first, so a
  malformed `args` cannot swallow one). The fields are masked by the wrapper's rules.
- **F5** Keys the caller also passes are not deduplicated (as slog).
- **F6** `l.From(a).From(b)` equals `l.From(b)` when `b` has a scope, and `l.From(a)` otherwise
  (scopes replace, they do not stack).
- **F7** `From(ctx).Debug(…)` below the level does no work on the arguments (0010 L3 holds).
- **F8** With `AddSource`, the source of `l.From(ctx).Info(…)` is the caller of `Info`, as without
  `From`.
- **F9** `From` on the same scope and the same logger returns a logger that writes the same output
  whether it comes from the cache or not, from any goroutine, during and after the request.
- **F10** `From(ctx).Fatal(…)` writes the record with the fields, then follows X1.

### Pipeline (the built-in logger; `internal/logpipe`)
A *record* is the bytes of one log call (one line in JSON and Text, several in Pretty). Constants
(unexported, no option): `maxRecord` 64 KiB, `queueLimit` 1 MiB, `softLimit` 80 % of it,
`batchSize` 64 KiB, `fullWait` 100 ms, `flushTimeout` 2 s, `reportInterval` 1 s. *Queued bytes*
are bytes accepted and not yet returned from the output's `Write`.

- **P1** The caller does the level check, conversion, masking and encoding; the record's time is
  the time of the call. Values are read during the call only: changing an argument afterwards never
  changes the line.
- **P2** Every logger built by `NewLog` writes to the one sink of `os.Stdout`; loggers on the same
  sink share its queue, limits and order.
- **P3** A record longer than `maxRecord` is cut on a UTF-8 boundary to at most `maxRecord` bytes
  and ends with `...[TRUNCATED]` and a newline (Pretty: a color reset first). A cut JSON record is
  not valid JSON.
- **P4** Synchronous mode (the state of a new sink): `Write` returns after the record has been
  passed to the output's `Write`. Nothing is dropped, no goroutine exists. This is today's behavior.
- **P5** Asynchronous mode (between `StartAsync` and the matching `StopAsync`): `Write` appends the
  record to the queue and returns without waiting for the output, except P8.
- **P6** One goroutine at most writes to an output at any time. It exists only while the queue is
  not empty; it passes whole records only, at most `batchSize` bytes per `Write` call (one record
  when the record alone is larger than what is left), in queue order.
- **P7** Debug and Info: when queued bytes plus the record exceed `softLimit`, the record is dropped
  and counted for its level. The caller does not wait.
- **P8** Warn and Error: accepted while queued bytes plus the record fit in `queueLimit`. Otherwise
  the caller waits for room for at most `fullWait`, then the record is dropped and counted. After
  one wait has timed out, Warn and Error that find no room are dropped at once (no wait) until the
  output completes a `Write`.
- **P9** Fatal is always accepted (the queue may exceed `queueLimit` by that record).
- **P10** Drops are reported by a record `WARN LogDropped` with one pair per level that dropped
  something, in the order `debug`, `info`, `warn`, `error`, the value the count since the last
  report. It is encoded by the logger whose record was dropped last (its format and mask rules),
  written by the writer when `reportInterval` has passed since the last report, when the queue runs
  empty, and by `Flush`. The report bypasses the limits. For each level: records written plus
  counts reported equal records submitted.
- **P11** An error from the output's `Write` discards that batch; the writer goes on with the next
  one and never retries. A panic of the output is recovered and treated the same.
- **P12** `Flush(timeout)` returns `true` once every record accepted before the call has been
  passed to the output and pending drop counts have been reported, `false` after `timeout`.
  It never drops anything.
- **P13** With the output blocked forever in asynchronous mode: no Debug or Info call waits, each
  Warn / Error call returns within `fullWait` (P8), queued bytes never exceed `queueLimit` plus one
  record, and exactly one goroutine is blocked.

### Ordering
- **O1** Two records accepted from the same goroutine on the same sink are written in call order,
  across loggers too (a user line and the access line of the same request).
- **O2** If call A returned before call B started, A is written before B. Concurrent calls are
  written in the order they were accepted; their timestamps may be out of order by the time between
  taking the time and being accepted.
- **O3** A dropped record leaves no gap marker; the `LogDropped` record is written where the writer
  is when it reports, not where the drops happened.
- **O4** Bytes user code writes to `os.Stdout` itself are not ordered with log records in
  asynchronous mode. Loggers given to `SetLogger` are called directly, on the caller, in every
  mode: their order and blocking are their own.

### Modes and the app lifecycle (`core`)
- **M1** `Create` calls `logpipe.Stdout().StartAsync()` as its last step, after the `OnReady`
  hooks. Before that every line is synchronous: a bootstrap panic loses nothing.
- **M2** `runShutdownHooks`, after the `OnShutdown` hooks, the access log's `Stop` and the stack
  sampler's last report (S6), calls `StopAsync(flushTimeout)` once per app that called `StartAsync`.
  Lines logged after it (after `Listen` or `Stop` returned) are synchronous.
- **M3** The mode is counted per sink: with two created apps in a process the sink stays
  asynchronous until both have shut down.
- **M4** `logStartup` flushes the stdout sink (`flushTimeout`) before it prints the banner.
- **M5** A logger used without an app (`log.NewLog` in a tool or a test) stays synchronous and
  starts no goroutine.

### Fatal and crashes
- **X1** `Fatal` on the built-in logger: the record is accepted (P9), the sink is flushed for at
  most `flushTimeout`, then `exit(1)`. In synchronous mode the write itself is bounded the same
  way (bug 1).
- **X2** `Fatal` on a wrapped adapter flushes the stdout sink for at most `flushTimeout`, then calls
  the adapter's `Fatal` once.
- **X3** What can be lost: records queued and not yet written when the process dies without
  passing through M2 or X1 (an unrecovered panic in a goroutine, `os.Exit` in user code, a second
  signal, `SIGKILL`, a runtime fatal error). At most `queueLimit` bytes; in practice what the
  writer has not written in the last batch. Recovered request panics are not in this case: they are
  Error records. This is the documented price of the approved asynchronous pipeline.
- **X4** An app served through `App` as an `http.Handler` under the user's own server is
  asynchronous from `Create` on; `app.Stop()` (state `serveCreated`) runs M2.

### Access log
- **A1** (replaces 0010 A1) One `Logger.Info` call per `complete` event, made by the listener on
  the emitting goroutine. Message and the pairs `id`, `transport`, `code`, `operation` as today,
  then one pair per stage (A2), `stages_dropped` when not zero, `total`.
- **A2** (replaces 0010 A2, A4; open question 2) A stage's value is one `string`: its events in
  arrival order, each `<short name> <duration> (<percent>%)`, joined by `, `
  (`"middleware", "CORS 40µs (4%), CSRF 60µs (6%)"`). Short name, duration format and percentages
  are today's. Every value of the line is a scalar (`color.Colored` is no longer used by the access
  log). **Colors stay in Pretty:** an access line carries an internal marker, and the Pretty format
  scans its values for the durations `formatDuration` writes and colors each by today's thresholds
  (`colorForDuration`), `total` included; JSON, Text and adapters get the plain string, at no cost.
  User records are never colored this way.
- **A3** With the built-in logger the line goes through the pipeline as an Info record (P7 applies:
  it is dropped under overload and counted in `LogDropped`). With an adapter the adapter is called
  directly on the request goroutine, as for any user log call.
- **A4** (replaces 0010 A9) A panic of the logger is recovered in the listener; the line is lost
  and the request is not affected.
- **A5** (replaces 0010 A8, A10) No worker, no queue, no ticker, no `AccessLog` / `dropped` record.
  `Stop()` removes the listener and discards pending requests; it returns at once. Events after
  `Stop` are ignored; a second `Stop` does nothing. 0010 A3, A5, A6, A7, A11, A12 are unchanged.

### Panic stack sampling (`core`; open question 5)
A *stack key* is a hash of the first 16 return addresses at the logging site (`runtime.Callers`):
the same panic reached the same way has the same key. Constants: 1 stack per key per second, 10
stacks per app per second; seconds are Unix seconds of an injected clock.

- **S1** The first non-exception panic of a key in a second is logged as today, with `stack`.
- **S2** Further panics of that key in the same second, and any panic once 10 stacks were logged in
  that second, are logged with every pair except `stack`; the stack text is not captured.
- **S3** Never more than 10 records with `stack` per app per second, whatever the number of keys.
  Two apps do not share a budget.
- **S4** Keys live in a fixed table of 32 slots; a key that finds its slot taken by another key
  replaces it. Memory does not grow with the number of distinct panics.
- **S5** For each second in which at least one stack was suppressed, one record
  `WARN PanicStacksSuppressed` with `stacks_suppressed`, n is logged when the second ends (a timer
  armed by the first suppression; no timer otherwise).
- **S6** Shutdown (M2) reports a pending count at once and stops the timer.
- **S7** Sampling applies to `UnhandledPanic` (HTTP, WS handshake) and `ErrorAfterResponse`. Both
  are logged through `logger.From(c.Context())` and no longer pass `id`: the scope's fields carry
  the request ID (open question 6).

## Contract with design 0011
Design 0011 (written in parallel) names the seam `internal/tracecontext`: `ScopeOf(ctx) *Scope`,
`(*Scope).AppendArgs(dst []any) []any`, the exported string fields of `Scope` and the `Key…`
constants. 0012 uses exactly those. What 0012 relies on, and the one thing it asks to add:

- **C1** `ScopeOf(nil)` returns nil and never panics; a context without a scope returns nil. One
  `Value` walk, 0 allocations, about 10 ns (0011 says 8 ns; measured here: a lookup through two
  wrappers plus an atomic load is 9.4 ns). `internal/tracecontext` does not import `log`,
  `accesslog` or `core`.
- **C2** `AppendArgs` on a scope returns an even number of elements, at most 10, keys first, in
  the fixed order of 0011 L1, empty values skipped. It allocates (one boxed string per value), so
  0012 calls it once per scoped logger, never per line; the built-in logger reads the fields and
  the `Key…` constants directly, in the same order with the same skipping. `fields` in the rules
  above is this list. A scope whose list is empty is "no scope" (F1).
- **C3** Fast path (H3): every value is a non-empty `string` of `[A-Za-z0-9._:-]`, at most 128
  bytes. 0011's IDs (hex) and `upstream` (S4: `A-Z a-z 0-9 - _ .`, 64 bytes) satisfy it. 0012
  still checks each value and takes the slow path for anything else, so a later field (`cause`)
  cannot break a line.
- **C4** A `*Scope` is immutable once a context carries it, safe for concurrent reads, never
  pooled and valid after the request ended.
- **C5 (asked of 0011, not in it yet)** One more field on `Scope`, reserved for package `log` and
  never touched by 0011: `LogSlot atomic.Value` (so a `Scope` is never copied after `With`). `log`
  stores its scoped logger there. Without it `From` still works and costs 2 allocations and about
  160 ns per call instead of per request; nothing else in this design changes.
- **C6** `core` needs `(*ctx.HTTPContext).Context()` (roadmap step 2) for S7. 0011's contract
  already hands the two panic lines to 0012 with "move to `From`, drop `id`": S7 does that.
- 0011 E7 renames the access line's ID keys; 0012 takes whatever keys E7 defines and changes only
  where and how the line is written (in A1, `id` reads "the ID pairs of 0011 E7").

## Performance

### Hot or cold
Hot: every log call, `From`, `Sink.Write`, the access log listener. Warm (a client can trigger
it): the panic path, the full-queue path. Cold: `NewLog`, `StartAsync`, `StopAsync`, `Flush`,
`Fatal`, the writer's report.

### Per-call cost
| Part | Today | Planned | Source |
|---|---|---|---|
| Info, 4 scalars, JSON, 1 goroutine | 2.9 µs, 2 allocs, 16 B (file write under a mutex) | 1.2 µs, 2 allocs, 16 B (encode 1.2 µs, hand-over under 50 ns) | prototype, `/dev/null` |
| Same, 12 goroutines | 2.2 µs per line (no scaling) | 0.39 µs per line (encode alone: 0.31 µs) | prototype |
| Same, output blocked 2 ms per write | 9.4 µs (roadmap) | 1.3 µs; the rest is dropped and counted | prototype |
| Same, synchronous mode (bootstrap, tools) | 2.9 µs | 2.9 µs (unchanged) | - |
| `From`, no scope | - | about 10 ns, 0 allocs | C1 |
| `From`, first call of a request | - | about 160 ns, 2 allocs, 192 B (lookup + suffix) | probe: 145 ns |
| `From`, later calls of the request | - | about 15 ns, 0 allocs | C1, C5 |
| Line through `From`, built-in | - | plain line + about 10 ns, 0 allocs (H3); slow path + 330 ns, + 1 alloc | probe |
| Line through `From`, adapter | - | + 1 alloc (the argument slice), about 60 ns | estimate |
| Access line, 6 stages, JSON | 16.4 µs, 102 allocs, 7.5 KiB on the worker | 3.4 µs, 26 allocs, 1.6 KiB on the request goroutine | probe of A2 |
| Access line, Pretty | 17.0 µs | 4.6 µs | probe of A2 |
| Panic, stack logged | 26 µs, 8 KiB | 26 µs + 1.6 µs for the key | probe |
| Panic, stack suppressed | 26 µs, 8 KiB | about 2 µs, 0 extra allocs (key 1.6 µs + a mutex) | probe: 16 frames between 0.85 µs (8) and 2.1 µs (32) |

Access log on the request goroutine: + 3.4 µs for a 6-stage request (the worker's 16 µs are gone,
so the CPU per logged request falls by about three quarters and the 65,000 lines/s ceiling
disappears). Step 8 measures Static and Chain again.

On two cores (`-cpu 2`), two goroutines logging in a tight loop (2.4 million lines per second) see
half of their Info lines dropped: the writer only gets a core at a preemption. That is P7 working;
real handlers yield.

### Idle cost and memory bound (small machines)
| Part | Idle | Worst case |
|---|---|---|
| Sink, never used | about 200 B, 0 goroutines, 0 timers | - |
| Sink, after use, queue empty | two buffers of at most 64 KiB each, 0 goroutines, 0 timers, 0 wakeups | 2 buffers of at most `queueLimit` each: 2 MiB, 1 goroutine |
| Encoders (H1) | freed by the GC | one per log call in progress, 512 B to 4 KiB kept each |
| `From` | nothing | 2 allocs per scope, freed with the scope |
| Access log | the shards of 0010 A7; no goroutine, no ticker, no queue (about 120 KiB and 1 wakeup per second less than today) | 0010 A7 unchanged |
| Stack sampler | under 1 KiB per app, 0 goroutines | 1 timer in a second with suppression |

Worst case of the pipeline on a 2-vCPU box: about 2.1 MiB per output, whatever the load, plus the
encode buffer of calls in progress (bounded by `maxRecord` after P3; a huge argument still needs
its bytes while it is encoded, as today). `queueLimit` is 1 MiB, not the roadmap's "about 4 MiB":
open question 4.

### What moves out of the call
The per-level writers, the encoders' handlers and the mask rules are built in `NewLog`. The scope
suffix is built once per scope (C5). The writer goroutine starts when the queue becomes non-empty
(a `go` statement: 0.27 µs, measured) and ends when it is empty: under load it never ends, at low
rates each burst pays it once.

### Heavy techniques (approving the design approves exactly these)
- **H1 `sync.Pool` of encoders, one pool per built-in logger.** An encoder is a byte buffer plus
  the slog handlers that write into it, so a call owns its record (level and scope suffix known)
  before the hand-over. Measured: 1.43 µs, 2 allocs, 0.3 KiB pooled against 1.95 µs, 7 allocs,
  1.0 KiB when built per call (−27 %, −5 allocs). Constraints: got and put back on the same
  goroutine inside one call, never referenced after `Put`; an encoder whose buffer grew over 4 KiB
  is not put back.
- **H2 Hand-managed batch buffers in the sink.** Two byte slices swapped between callers and the
  writer under one mutex, plus the offsets where a batch ends; callers append their record (one
  copy). Measured: the hand-over costs under 50 ns over encoding alone and reaches 0.39 µs per line
  at 12 goroutines (the alternative, one pooled buffer per line through a channel, needs a second
  copy and a buffer that crosses goroutines; not built). Constraints: capacity never above
  `queueLimit`; a buffer over 64 KiB is released when the queue runs empty; no lock-free ring.
- **H3 Scope suffix fast path (JSON and Text).** When every field is a plain string (C3) and no
  mask rule matches a key, the scoped logger keeps the encoded fields and splices them before the
  record's end. Measured: 330 ns and 1 allocation saved per line, 145 ns once per scope.
  Otherwise, and always in Pretty, the fields are added as attributes (slow path). Constraint:
  `TestFrom_FastPathEqualsSlowPath` (F3) over random arguments and fields.

Not used: `unsafe`, a lock-free queue, an encoder of our own. Type B idea for later: slog's
encoding (1.2 µs) is now the whole cost of a line; a hand-written JSON encoder is the next gain.
If H1 is refused: one shared handler per level and `slog.Handler.WithAttrs` per scope (577 ns and
6 allocs per scope and level, measured) replace H1 and H3.

## Concurrency
- **Sink:** one mutex `mu` guards the buffers, batch offsets, queued bytes, drop counts, mode count,
  the writer flag and the "stuck" flag of P8. Callers hold it for an append. The writer holds it to
  swap buffers and to account a finished batch, never during the output's `Write`. Waiters (P8,
  `Flush`, P4) wait on a channel the writer closes and replaces after each batch, with a timer; no
  `sync.Cond`.
- **Writer goroutine:** started by the `Write` that makes the queue non-empty (asynchronous mode),
  at most one per sink, ends when the queue is empty. In synchronous mode the caller runs the same
  drain loop itself, or waits for the caller that does. Nothing to stop at shutdown: `StopAsync`
  flushes, and a writer blocked in a stuck output is the one goroutine of P13.
- **Lock order:** a logger's pool → `Sink.mu`; nothing is called with `Sink.mu` held (the report
  function runs outside it).
- **`From`:** the scoped logger is immutable; the slot is an `atomic.Value` (two goroutines may
  both build it, one store wins, both results are equal: F9).
- **Access log:** the shards and their mutexes are unchanged; the listener formats and logs after
  releasing the shard lock.
- **Stack sampler:** one mutex per app around the table and the counters; the timer callback takes
  it to read and reset the count, and logs outside it.

## Security
- A client controls the request path (the access line's message), values user code logs and how
  often a route panics. Bounds: `maxRecord` per record (P3), `queueLimit` per output (P13), Debug
  and Info shed first (P7), 10 stacks per second per app (S3), 32 sampler slots (S4), at most one
  `LogDropped` and one `PanicStacksSuppressed` record per second while busy.
- Masking and escaping happen on the caller before the queue (P1): nothing unmasked is ever
  queued, and the scope fields go through the same rules (F3, F4). The fast path writes only
  values of C3's character set, so it cannot forge a line; anything else takes the encoder.
- A flood of Warn / Error with a stuck output cannot hold requests: P8 waits once, then drops.
- Records a client causes to be dropped are counted per level (P10); an attacker can hide Info
  lines by flooding, never Warn, Error or panic records while there is room.
- Adapters get no bound from the pipeline (they are called directly, by decision); the README's
  production section says so.

## Impact
- **Breaking (compile time):** a user type with the five methods no longer is a `common.Logger`;
  it is a `common.LogAdapter`. Passing it to `SetLogger` / `WrapLogger` still compiles; a variable
  or field typed `common.Logger` holding it does not (fix: `app.Logger()` or `log.WrapLogger`).
- **Breaking (output):** the access line's stage values (A2); `UnhandledPanic` and
  `ErrorAfterResponse` lose `id` and gain the scope's fields (S7), and most lose `stack` under a
  flood; the `AccessLog` / `dropped` record becomes `LogDropped`.
- **Behavior:** between `Create` and shutdown, built-in log lines reach stdout a moment after the
  call; tests that read stdout must stop the app or use a logger of their own.
- **Code:** `common/logger.go`; `log/log.go` (encoders, per-level writers, `From`),
  `log/log_mask.go` (`WrapLogger`, the adapter's `From`), `log/log_handler.go` (Pretty writes into
  the encoder); new `internal/logpipe`; `accesslog/access_log.go` (worker, queue, `lineArgs`);
  `core/app.go` (`SetLogger`, M1), `core/server.go` (M2, M4), `core/fn.go`, `core/http.go`,
  `core/ws.go` (sampler, S7).
- **Tests to rewrite:** `accesslog` tests of the worker, queue and drop report (0010 A8-A10) and of
  the stage values; `core` tests that assert `id` or `stack` on panic lines. Fakes typed
  `common.Logger` become `common.LogAdapter`.
- **Docs (step 7):** READMEs of `log` ("starts no goroutine", `From`, the pipeline, what can be
  lost), `accesslog` (no worker, line shape, cost), `common`, `core` (`SetLogger`, shutdown order,
  stack sampling, `app.Stop()` for embedders), a new `internal/logpipe/README.md`; `CLAUDE.md` and
  `.ginject/` (`package-reference.md`, `request-pipeline.md`, `metadata-concurrency-model.json`,
  `metadata-performance.json`, `anti-patterns-gotchas.md`: forgetting `From`).

## Test plan
Time is injected (`now`, the sampler's timer function, the sink's wait and intervals are
unexported fields); outputs are test writers: `gateWriter` (blocks each `Write` until released,
records every call), `failWriter`, `bytes.Buffer` behind a mutex. `Flush` is the barrier: no
sleeping, no polling. Concurrent tests run under `-race`.

| Rule | Test | Case |
|---|---|---|
| I1-I3 | `TestSetLogger_AcceptsFiveMethodAdapter`, `TestLogger_InjectedHasFrom`, `TestLogger_WithoutFromUnchanged` | zap-shaped double; built-in; arguments and bytes equal to before |
| F1 | `TestFrom_NilContextReturnsReceiver`, `TestFrom_NoScopeReturnsReceiver`, `TestFrom_EmptyFieldsReturnsReceiver` | nil, `context.Background()`, empty scope; built-in and adapter |
| F2, F3 | `TestFrom_BuiltinAddsFieldsAfterArgs_JSON` / `_Text` / `_Pretty`, `TestFrom_FastPathEqualsSlowPath`, `TestFrom_MalformedArgsKeepFields`, `TestFrom_MaskRuleMasksField` | each format; random args and fields incl. non-plain values, slog.Attr, odd lengths; a rule on `request` |
| F4, F5 | `TestFrom_AdapterGetsFieldsFirst`, `TestFrom_AdapterMasksFields`, `TestFrom_DuplicateKeyKept` | exact argument slice the double receives |
| F6-F10 | `TestFrom_SecondScopeReplaces`, `TestFrom_BelowLevelDoesNoWork`, `TestFrom_AddSourceIsCaller`, `TestFrom_ConcurrentSameScopeNoRace`, `TestFrom_AfterRequestStillHasFields`, `TestFrom_FatalWritesFieldsThenExits` | a `String` method that fails the test if called; 32 goroutines |
| P1 | `TestPipeline_ValueChangedAfterCallNotSeen` | mutate a map after the call, async, gate closed |
| P2, O1, O2 | `TestPipeline_TwoLoggersShareOrder`, `TestPipeline_SameGoroutineOrder`, `TestPipeline_HappensBeforeOrder`, `TestPipeline_ConcurrentWritersWholeRecords` | 2 loggers, 1 sink; 16 goroutines × 1,000 records, every record intact |
| P3 | `TestPipeline_LongRecordTruncated`, `TestPipeline_TruncateOnRuneBoundary`, `TestPipeline_RecordAtLimitNotTruncated` | 64 KiB − 1, exactly, + 1; multi-byte at the cut; Pretty reset |
| P4, M5 | `TestSink_SyncWriteReturnsAfterOutput`, `TestSink_SyncStartsNoGoroutine`, `TestSink_SyncConcurrentNoLoss` | buffer holds the record when `Write` returns |
| P5, P6 | `TestSink_AsyncDoesNotWaitForOutput`, `TestSink_BatchesWholeRecordsUpToBatchSize`, `TestSink_OneWriterAtATime`, `TestSink_WriterEndsWhenEmpty` | gate closed; sizes of each `Write`; goroutine count back to the start |
| P7 | `TestSink_DebugInfoDroppedAboveSoftLimit`, `TestSink_InfoAcceptedAtSoftLimit` | boundary: fits exactly / one byte over |
| P8 | `TestSink_WarnAcceptedUntilFull`, `TestSink_WarnWaitsThenDrops`, `TestSink_WarnAcceptedWhenRoomAppears`, `TestSink_NoWaitAfterTimeoutUntilProgress` | fake wait channel; release one batch |
| P9 | `TestSink_FatalAlwaysAccepted` | full queue |
| P10 | `TestSink_DropReportPerLevel`, `TestSink_DropReportOncePerInterval`, `TestSink_DropReportWhenIdle`, `TestSink_DropAccounting` | fake clock; written + reported = submitted per level |
| P11 | `TestSink_WriteErrorDiscardsBatch`, `TestSink_OutputPanicRecovered` | later records still written |
| P12 | `TestSink_FlushWaitsForAccepted`, `TestSink_FlushTimesOut`, `TestSink_FlushEmptyReturnsTrue` | gate closed → false |
| P13 | `TestSink_StuckOutputBoundsCallersAndMemory` | gate never opened, 10,000 calls: no caller blocked, queued ≤ limit + 1 record |
| O3, O4 | `TestSink_ReportPosition`, `TestSetLogger_AdapterCalledDirectly` | adapter called on the caller before the call returns |
| M1-M4 | `TestCreate_BootstrapLinesSynchronous`, `TestCreate_SwitchesToAsync`, `TestShutdown_FlushesAfterHooks`, `TestShutdown_LinesAfterStopSynchronous`, `TestSink_AsyncCountedPerStart`, `TestListen_BannerAfterStartupLines` | `logpipe.SetStdout`; a hook that logs; two apps |
| X1, X2, X4 | `TestFatal_FlushesThenExits`, `TestFatal_StuckOutputExitsAfterTimeout`, `TestWrapLogger_FatalFlushesStdoutSink`, `TestStop_WithoutServeFlushes` | the `exit` seam; fake timeout |
| A1-A5 | `TestAccessLog_LineOnEmittingGoroutine`, `TestAccessLog_StageValueIsOneString`, `TestAccessLog_AllValuesScalar`, `TestAccessLog_LoggerPanicRecovered`, `TestAccessLog_StartsNoGoroutine`, `TestAccessLog_StopReturnsAtOnce`, `TestAccessLog_BuiltinLineDroppedUnderOverloadIsCounted` | 0, 1, 6, 64 stages; repeated names; goroutine count |
| S1-S4 | `TestStackSampler_FirstOfKeyHasStack`, `TestStackSampler_SameKeySameSecondNoStack`, `TestStackSampler_NewSecondAllowsAgain`, `TestStackSampler_GlobalCap`, `TestStackSampler_RareKeyKeepsItsStackDuringFlood`, `TestStackSampler_TableBounded`, `TestStackSampler_ConcurrentNoRace`, `TestStackSampler_AppsIndependent` | fake clock; 11 distinct panic sites; 100 keys |
| S5-S7 | `TestStackSampler_SuppressedLinePerSecond`, `TestStackSampler_NoTimerWithoutSuppression`, `TestShutdown_ReportsSuppressedStacks`, `TestPanicLine_CarriesScopeFields`, `TestErrorAfterResponse_Sampled` | fake timer |
| Benchmarks | `BenchmarkLog_Info_Scalars_JSON_Async` (1 and parallel), `BenchmarkLog_Info_SlowOutput`, `BenchmarkFrom_NoScope` / `_First` / `_Cached`, `BenchmarkLog_From_Info_JSON`, `BenchmarkAccessLog_Line_6Stages`, `BenchmarkServeHTTP_PanicString` (flood) | targets: the "Planned" column |

## Open questions
1. **Name of the five-method interface: `common.LogAdapter`.** Alternatives: `LoggerAdapter`
   (longer), `BaseLogger` (says nothing about who implements it). **Recommendation: `LogAdapter`.**
2. **Access line shape (A2).** Without a worker the line is formatted on the request goroutine,
   and today's shape costs 16.4 µs and 102 allocations there. (a) One string per stage: 3.4 µs,
   every format, adapters included, no fast path; costs a breaking output change and the duration
   colors of Pretty. (b) Keep the shape and write a JSON-only fast path: about the same speed in
   JSON, 17 µs in Text and Pretty, one more heavy technique. (c) Keep the worker: contradicts the
   approved decision and keeps a goroutine and a queue. **Recommendation: (a).**
3. **Synchronous outside `Create` … shutdown (M1-M5).** The roadmap describes the pipeline for
   requests; this design keeps bootstrap, tools and lines after shutdown synchronous so a bootstrap
   panic and the last lines of `main` lose nothing, with no public flush method. Against: two
   modes to test. **Recommendation: accept.**
4. **`queueLimit` 1 MiB instead of "about 4 MiB".** 1 MiB is about 5,000 lines of 200 B, 16
   batches, and bounds the pipeline at about 2 MiB instead of 8 MiB on a small box. 4 MiB only
   rides out longer stalls of stdout before Info is dropped. **Recommendation: 1 MiB** (small
   machines first); the number is one constant.
5. **Stack sampling per stack key (S1-S5) instead of the roadmap's "10 per second per app".** With
   one budget a flooded panic uses all 10 stacks and hides a rarer one. Per key (1 per second) with
   the same global cap of 10 fixes that for 1.6 µs per panic (`runtime.Callers`, 16 frames, 0
   allocs) against 26 µs for a stack; a suppressed panic costs about 2 µs instead of about 0.4 µs
   with the plain counter. A cheaper key (route + type of the panic value, about 20 ns) cannot
   tell two panics of one route apart. **Recommendation: per stack key, 1 per second per key, 10
   per second per app.** Fallback: S1-S4 reduce to one counter.
6. **Panic lines through `From`, without `id` (S7).** They then carry the same fields as user
   lines (trace ID included) under the same keys. Against: changes the line of 0007 E3 and ties
   step 6 to 0011's `Context()`. **Recommendation: accept.** Fallback: keep `id`, no `From`.
7. **After one timed-out wait, Warn / Error are dropped without waiting until the output moves
   (P8).** The roadmap says "wait at most about 100 ms"; without this every error-logging request
   pays 100 ms for as long as stdout is stuck. **Recommendation: accept.**
8. **A truncated JSON record is not valid JSON (P3).** Keeping it valid means re-encoding or
   dropping the content. **Recommendation: accept** (64 KiB records are abnormal; the head with
   time, level and message survives).
9. **Heavy techniques H1, H2, H3** as listed. **Recommendation: approve all three.**

## Coordination

- 2026-11-16 (review of roadmap 0004): names the implementation added that the sections above do not
  list, all internal or unexported to users:
  - `logpipe.FlushTimeout` (2 s), exported in the internal package so `core` (shutdown, `SetStdout`)
    and `log` (`Fatal`) flush with the same bound.
  - `logpipe.AccessLine{Msg, Attrs}`, the internal seam through which `accesslog` hands a finished
    access line to the built-in logger (`LogAccess`), so Pretty can color it without reflection.
  - `color.ForDuration(d)` in `internal/color`, the duration colors of A2 (the `colorForDuration`
    above), shared by `accesslog`.
  - The sink keeps one buffer of at most 64 KiB once the queue is empty (the spare is always
    dropped) and regrows a released queue straight to 64 KiB. A truncated record gets the color
    reset only when its kept part holds an escape sequence.
- 2026-11-17 (step 8 measurements, approved by the user):
  - **Both buffers kept, up to 64 KiB each.** Dropping the spare made every burst allocate it
    again (Static with the access log: 8.8 KiB of garbage a request). The sink now keeps both
    buffers when the queue empties, releases only a buffer grown past 64 KiB, and grows an empty
    buffer from 4 KiB by doubling. Idle: at most 128 KiB, still no goroutine and no timer. Static
    with the access log: 1.9 KiB a request.
  - **Pretty draws stages as a tree again.** In an access line rendered by Pretty, a stage value
    whose every item is `<name> <duration> (<percent>%)` is drawn as a branch with one child per
    item (name as the key, `<duration> (<percent>%)` colored by `color.ForDuration`), as before
    0012. JSON, Text and adapters keep the one string of A2; nothing changes on the request path
    outside Pretty. Values that don't match (a masked stage) stay one string.
  - The +1 µs target of 0010 compared a worker that dropped lines it could not keep up with
    (`select … default`, about 16 µs a line, so at most one line in ten in the benchmark) with a
    pipeline that writes every line; the comparison is per request CPU at GOMAXPROCS 1 and 2,
    every line written.

## Decisions (2026-11-16)

Approved by the user: every open question answered as recommended, with one addition.

- Q1 the five-method interface is `common.LogAdapter`.
- Q2 one string per stage (A2), **plus colors kept in Pretty** by coloring durations when the access
  line is rendered (A2 as amended above; Pretty only, no cost elsewhere). The old shape
  (`[]map[string]color.Colored` per stage, encoded by reflection: 16.4 µs, 102 allocs, 7.5 KiB a
  line, hidden on the worker today) is recorded as a performance bug.
- Q3 synchronous logging outside `Create` … shutdown.
- Q4 the queue limit is 1 MiB per output.
- Q5 stack sampling per stack key: 1 per key per second, 10 per app per second (S1-S7).
- Q6 panic lines go through `From`, without `id`.
- Q7 after one timed-out 100 ms wait, Warn and Error drop without waiting until the output moves.
- Q8 a record cut at 64 KiB may be invalid JSON.
- Q9 heavy techniques H1 (`sync.Pool` of encoders), H2 (two swapped batch buffers) and H3 (scope
  suffix encoded once per request) approved.
