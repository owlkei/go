# 0010: Observability (`log`, `accesslog`, `trace`, `event`)

Status: Approved
Packages: `log`, `accesslog`, `trace`, `event` (removed, see Q1), `internal/color`; the trace call
sites of `core`. Roadmap: `docs/roadmap/0003-server-and-production.md`, step 4. Depends on designs
0007 (dispatch: who emits which event, the `UnhandledPanic` line), 0008 (shutdown) and 0009
(`SetLogger`, `log.NewLog` no longer a singleton, G3 and G5).

## Goal
The access log costs a small, bounded amount on the request goroutine and can no longer grow
without limit, drop lines silently or take the process down. The logger shows errors, cannot be
used to forge log lines, masks what a mask rule names wherever the value sits, and survives any
value it is given. The trace events get a typed, allocation-free carrier that the devtools platform
can listen to later.

Measured on Go 1.27.4, darwin/amd64, i7-9750H, medians of 5 runs. Another session ran benchmarks
on the machine at the same time, so compare numbers inside one table, not across tables.

## Bugs found
Each was reproduced with a throwaway probe (nothing was left in the repository). Line numbers are
today's `master`; `core` moves with design 0007.

| # | Where | Input | Today | Rule |
|---|---|---|---|---|
| 1 | `log/log_handler.go:110-113`, `:148-151`, `:357-359`; reached from the network through `accesslog/access_log.go:81` (the request path is the log message) | Pretty format, a message or string value with `\n` or `ESC` (`GET /a%0A%1B[2K FAKE [admin login ok]`) | Written raw: a forged line and live terminal escape codes. JSON and Text are safe (slog escapes) | F2-F3 |
| 2 | `log/log_handler.go:215-223`, `:274-285` | Pretty format (the default), `logger.Error("failed", "error", errors.New("boom"))` | The value is printed empty (also `fmt.Errorf`): the error's struct has no exported field and its pointer-receiver `Error` is not seen | V2 |
| 3 | `log/log_tag.go:13-19`, `log/log_mask.go:76-99` | Any format, `"url", u` with `u *url.URL` (a pointer-receiver `Stringer`) | Expanded into a field map: `User:{user pw true}` shows the URL's password | V2 |
| 4 | `log/log_mask.go:43-47`, `:76-121` | Rule `password`; the secret inside `[]User{…}`, `[]map[string]any{…}`, `map[int]User{…}`, `slog.String("password", …)`, `slog.GroupValue(…)` | Not masked: slices, arrays, non-string maps and `slog.Attr` arguments are never visited | M4 |
| 5 | `log/log_mask.go:147-152` | Rule `password`; a struct field `Password string` without a tag, or the key `"Password"` | Not masked: rules are case-sensitive and an untagged field is named like the Go field | M2 |
| 6 | `log/log_mask.go:76`, `log/log_handler.go:292` | A value that points to itself (`n.Next = n`), any format | The call never returns and memory grows without bound (both the mask layer and the Pretty handler recurse): the probe processes held 1.8 GB and 3.6 GB within about a minute and had to be killed | V5 |
| 7 | `log/log.go:122-136`, `log/log_handler.go:95` | `AddSource: true` | JSON / Text report `log/log.go:127` for every line; Pretty prints no source at all | L5 |
| 8 | `log/log.go:25`, `:15-18` | `LogOptions.Transports` | Never read: no file is opened, no per-transport level exists | removed |
| 9 | `log/log.go:66`, `:61-78` | `LogOptions{Level: -1}`; any options value | `-1` is silently turned into Info; the caller's struct is modified | L2 |
| 10 | `log/log_mask.go:26-30` | `logger.Debug("x", "user", u)` with level Info | The reflection and masking run before the level check: 1.56 µs, 15 allocs for a line that is dropped | L3 |
| 11 | `log/log_handler.go:69-76`, `:78-93` | `slog.New(log.NewPrettyHandler(…)).With("k", "v")`; a level other than the five | `With` returns the embedded `TextHandler` (plain text from then on); an unknown level prints no label and no time | removed (the handler is unexported) |
| 12 | `accesslog/access_log.go:96-108` + `log/log_handler.go:60-67` | JSON format with the access log on | `"middleware":[{"CORS":{"Text":"120µs (4%)","Color":"\u001b[90m"}}]`: ANSI codes in JSON | A4 |
| 13 | `accesslog/access_log.go:70`, `:80` | A request whose `complete` event says 250 ms with one 1 ms stage; a 404 | `total 1ms`; the 404 logs `total 0s`: the wall-clock duration of the `complete` event is ignored | A3 |
| 14 | `accesslog/access_log.go:61` | Stage events whose `complete` never arrives (100,000 IDs) | 100,000 entries stay in the map for good; one request may also add any number of stages | A6-A7 |
| 15 | `accesslog/access_log.go:55-58` | 5,000 `complete` events with a logger that takes 1 ms | 1,026 lines logged, 3,974 dropped with no trace | A8 |
| 16 | `accesslog/access_log.go:44`, `:68-83` | A custom logger whose `Info` panics | The worker goroutine has no `recover`: the process exits with status 2 | A9 |
| 17 | `accesslog/access_log.go:44-46` | 50 `NewAccessLog` calls; shutdown | 50 goroutines and listeners that can never be stopped; the lines still queued at exit are lost | A10 |
| 18 | `event/event.go:71-80` | `Off(name, b)` where `a` and `b` are closures of the same function literal | Removes `a` (functions are compared by code pointer) | Q1 / EV2 |
| 19 | `event/event.go:101-115` | `Emit` from many goroutines | Takes the write lock and copies the listener slice on every emit: all requests queue on one mutex (97 ns alone, 209 ns with 12 goroutines) | T5 / EV1 |
| 20 | `event/event.go:117-122` | `Once(f)` then `On(g)` | `g` runs before `f`: once-listeners always run last | Q1 / EV3 |
| 21 | `core/app.go:330`, `:428`, `:454-459`, `core/fn_plan.go:320` | HTTP guard, interceptor, filter, handler and post-interceptor events | `Transport` is empty (only middleware and `complete` set it) | T7 |
| 22 | `log/log_handler.go:200`, `:210-213`, `:232`, `log/log_tag.go` | The file rule | `dereference`, `kv`, `stringerType`, `errorType` live in `log_handler.go` and are used by `log_mask.go` and `log_tag.go` | moved to `log/fn.go` |

Also noted: `core/app.go:243` wraps a `log.NewLog` logger a second time (every struct is converted
twice: 1.75 µs / 28 allocs instead of 1.2 µs / 14); design 0009 G3 already stops that. Nothing can
listen to trace events from outside `core` today (`app.event` is unexported and has no getter).

## Public API
Every exported change. "Hide by default" applied: the net change is 5 names added, 31 removed
(plus one config field swapped).

### `trace`
```go
type Tracer struct{ /* unexported */ }             // added; the zero value is ready to use

func (t *Tracer) Enabled() bool                    // added
func (t *Tracer) Emit(e Event)                     // added
func (t *Tracer) Listen(fn func(Event)) (stop func()) // added
```
`Event` (8 fields) and the `Stage…` constants, `TransportHTTP`, `TransportWS` are unchanged.

| Added | Why it is exported |
|---|---|
| `Tracer`, `Enabled`, `Emit`, `Listen` | `core` emits, `accesslog` listens, the devtools platform will listen: three packages need them. They replace the 12 names of `event` for this job |

Removed (breaking): `EventName` (the bus topic; there is no bus), `TransportGQL`, `TransportRPC`
(no code emits them).

### `event` (breaking, Q1)
The package is deleted: `NewEvent`, `Event` and its methods `On`, `Once`, `Off`, `Emit`,
`HasListeners`, `ListenerCount`, `EventNames`, `RemoveAllListeners`, `SetMaxListeners`, `Reset`.
After this design nothing in the framework imports it, and `memorybroker` is the in-process
publish / subscribe tool users are given.

### `accesslog` (breaking)
```go
type AccessLogConfig struct {
    Tracer *trace.Tracer   // was Event *event.Event
    Logger common.Logger
}
func NewAccessLog(cfg *AccessLogConfig) *AccessLog   // unchanged signature
func (al *AccessLog) Stop()                          // added
```
`Stop` is exported because `core` (another package) must flush the queue at shutdown, and the
conventions ask every type with a goroutine for a stop method. Users never call it:
`app.EnableAccessLog()` stays the only user-facing switch.

### `log` (breaking)
```go
func NewLog(opts *LogOptions) common.Logger                          // unchanged signature (0009 G5)
func WrapLogger(next common.Logger, maskFields []string) common.Logger // unchanged signature

type LogOptions struct {
    Level      slog.Level
    LogFormat  logFormat    // zero value: see L4 (Q2)
    AddSource  bool
    MaskFields []string
}
```
Unchanged: `DebugLevel`, `InfoLevel`, `WarnLevel`, `ErrorLevel`, `FatalLevel`, `TextFormat`,
`JSONFormat`, `PrettyFormat`, the `log:"name"` / `log:"-"` struct tag.

| Removed | Why |
|---|---|
| `LogTransport`, `LogOptions.Transports` | Dead (bug 8). Output is stdout; redirect it, or bring a logger with `SetLogger` (Q3) |
| `LogOptions.TimeFormat` | Only the Pretty format reads it: an option whose effect depends on another option (Q3) |
| `PrettyHandler`, its method `Handle` and the promoted `slog.TextHandler` methods, `NewPrettyHandler`, `PrettyHandlerOptions` | Not needed to use the logger (`LogFormat: log.PrettyFormat` is the way), and broken as a general `slog.Handler` (bug 11). Becomes the unexported `prettyHandler` (Q4) |
| `Color`, `Colored`, `Colored.String`, `ColorGray`, `ColorGreen`, `ColorBlue`, `ColorYellow`, `ColorOrange`, `ColorRed` | Used only by `accesslog`. Moved to `internal/color` as `Colored{Text string; Color Color}`, `Color`, `Gray`, `Green`, `Blue`, `Yellow`, `Orange`, `Red` (Q4) |

`common.Logger` is unchanged (five methods).

## Behavior

### `trace`: the tracer
- **T1** The zero `Tracer` has no listener: `Enabled()` is `false`, `Emit` does nothing.
- **T2** `Listen(fn)` registers `fn` and returns `stop`. From then on `Enabled()` is `true` and every
  `Emit(e)` calls the listeners synchronously, on the emitting goroutine, in registration order,
  with `e` by value.
- **T3** `stop()` removes exactly the registration that returned it (the same function registered
  twice is two registrations). Calling it again does nothing. After the last one, `Enabled()` is
  `false`.
- **T4** `Listen(nil)` registers nothing and returns a `stop` that does nothing.
- **T5** `Enabled` and `Emit` take no lock and allocate nothing. A listener may call `Listen`, a
  `stop` or `Emit`; a listener registered during an `Emit` is first called by the next `Emit`.
- **T6** `Emit` does not recover: a listener that panics panics in the emitter. Listeners are
  framework code (the access log's cannot panic: a lock, a map, an append). A public way to listen,
  when the devtools platform adds one, wraps the user's function with a `recover` at registration.
- **T7** What `core` emits (the contract of design 0007 E8, completed): every event has `ID`,
  `Stage`, `Name`, `Transport` (`HTTP` or `WS`, on every stage: bug 21) and `Duration`. The
  `complete` event also has `Operation` (the method, or the WS message type), `Target` (the URL
  path without the query string, or the WS topic) and `Code`; a middleware event that ended in a
  panic has `Code` (0007 E8). `core` asks `Enabled()` once per request (0007 P4) and emits nothing
  when it was `false`.

### `accesslog`
`pending` means: a request that sent at least one stage event and no `complete` yet.

- **A1** One `Logger.Info` call per `complete` event, made on the worker goroutine. The message is
  the event's `Target`, cut to 2,048 bytes (on a rune boundary) followed by `...` when longer. The
  arguments, in order: `"id"`, ID, `"transport"`, Transport, `"code"`, Code (an `int`),
  `"operation"`, Operation, then one pair per stage (A2), then `"total"`, text (A3).
- **A2** Stages appear in the order their first event arrived; the key is the stage name, the value
  a `[]map[string]color.Colored` with one single-entry map per event, in arrival order. The map key
  is the event's `Name` after its last `.` (`cors.CORS` → `CORS`). `Text` is
  `<duration> (<percent>%)`. The duration format (two decimals of the unit: `1.23µs`, `1.23ms`,
  `1.23s`, raw below 1 µs), the percentages (shares of the sum of the stage durations, largest
  remainder, exactly 100 in total, all 0 when the sum is 0) and the color thresholds (10, 50, 100,
  300, 1000 ms) are today's.
- **A3** `total` is the `Duration` of the `complete` event (the wall-clock time of the request), in
  the same duration format. A request without stage events (a 404) logs the four fixed pairs and
  `total`. Change: today `total` is the sum of the stages (bug 13).
- **A4** In the JSON and Text formats a `color.Colored` is written as its `Text` only
  (`{"CORS":"120µs (4%)"}`); the Pretty format prints the text in its color.
- **A5** Events of different IDs never mix, whatever the interleaving and the goroutines.
- **A6** A request keeps at most 64 stage events (`maxStagesPerRequest`); later ones are counted,
  and the line carries `"stages_dropped"`, n before `"total"`.
- **A7** At most 16,384 requests are pending (`maxPending`: 64 shards of 256). A new request that
  finds its shard full evicts that shard's oldest pending request. An evicted request that
  completes later is logged without stages. `(*AccessLog).pendingLen() int` (unexported, for tests)
  returns the number of pending requests.
- **A8** The queue between requests and the worker holds 1,024 lines (`flushQueueSize`). A
  `complete` event that finds it full is dropped and counted; requests never wait for the logger.
  The worker reports the count with `Logger.Warn("AccessLog", "dropped", n)`, at most once per
  second, and resets it; `Stop` reports what is left. Lines logged plus lines reported as dropped
  equal the `complete` events received.
- **A9** A panic of the logger on the worker is recovered; that line counts as dropped and the
  worker goes on.
- **A10** `Stop()` removes the listener, lets the worker pass every queued line to the logger, and
  returns when it has finished or after 2 seconds, whichever comes first. The worker goroutine then
  ends. Events emitted after `Stop` are ignored; pending requests are discarded. A second `Stop`
  does nothing. `core` calls it once, after the server has drained and the `OnShutdown` hooks have
  run (design 0008 G2-G4), so the last lines are written before the process exits.
- **A11** `NewAccessLog` with a nil config, tracer or logger panics
  `invalid access log: a tracer and a logger are required` (bootstrap, `color.FmtRed`).
- **A12** WebSocket `complete` events produce the same line (`transport` `WS`, `operation` the
  message type, the message the topic).

### `log`: construction and levels
- **L1** `NewLog(opts)` returns a new, independent logger on every call (0009 G5). `nil` means
  the defaults: level Info, the default format (L4), no source, no mask rule.
- **L2** `NewLog` and `WrapLogger` copy what they need: they never modify `opts` or `maskFields`,
  and later changes to them have no effect. A `Level` is used as given (`-1` is no longer special).
- **L3** A call below the level writes nothing and does no work on its arguments: no method of an
  argument (`String`, `Error`, `LogValue`) is called and nothing is allocated by the logger.
- **L4** Formats. `JSONFormat`: one JSON object per line, slog's keys (`time`, `level`, `msg`, then
  the arguments). `TextFormat`: slog's `key=value` line. `PrettyFormat`: the tree of today (F1).
  The level is written `DEBUG`, `INFO`, `WARN`, `ERROR` or `FATAL` in all three. With `LogFormat`
  zero the format is Pretty when stdout is a terminal (a character device) and JSON otherwise (Q2).
  The unexported `newLog(out io.Writer, isTerminal bool, opts *LogOptions) common.Logger` is what
  `NewLog` calls with `os.Stdout`; tests use it.
- **L5** With `AddSource` the line names the file and line of the code that called the `Logger`
  method, in all three formats (JSON and Text: slog's `source`; Pretty: a last row `source` with
  `file:line`), also when the logger went through `WrapLogger`. Without it no caller lookup is made.
- **L6** Every record is one `Write` to the output; records of concurrent calls never interleave.
- **L7** `Fatal` writes the record at the `FATAL` level, then calls the unexported `exit` variable
  (`os.Exit`) with 1.

### `log`: values (all formats, and what a wrapped custom logger receives)
- **V1** Arguments are `key, value` pairs as in slog; a `slog.Attr` argument is one pair; a
  trailing key without a value and a non-string key are passed on as they are (slog prints
  `!BADKEY`).
- **V2** A value whose type, or pointer type, implements `error`, `fmt.Stringer` or
  `slog.LogValuer` is never expanded: it is written through that method (`Error()`, `String()`,
  the resolved `LogValue()`), in every format. A wrapped custom logger receives it unchanged. A nil
  pointer or nil interface is written as null without calling a method.
- **V3** Any other struct becomes a `map[string]any`: exported fields under their Go name, a field
  tagged `log:"name"` under `name` (unexported fields too, Q5), `log:"-"` and untagged unexported
  fields left out. Pointers are followed. Maps with string or integer keys become
  `map[string]any` (integer keys in decimal) when one of their values changes; slices and arrays
  become `[]any` when one of their elements changes.
- **V4** A value that needs no conversion and no masking is passed on as the same value; when no
  argument needs any, the argument slice itself is passed on (no copy).
- **V5** Nesting is followed 16 levels deep (the argument's value is level 0); a value below that
  is replaced by the string `[MAX_DEPTH]`. After 1,000 values visited inside one argument, each
  remaining value is replaced by `[TRUNCATED]`. So a value that contains itself ends (bug 6).
- **V6** A logger call never panics because of a value. A panic raised by a value's own method
  while the line is built or written is recovered; the call writes one line with the original
  level and message and the pair `"log_error"`, the recovered value as text, and returns.

### `log`: masking
A value's path is its key, then the keys under it joined by `.` (`user.password`). Elements of a
slice or array have the path of the slice.

- **M1** Rules (unchanged syntax). A rule without `*` matches a path that equals it or ends with
  `.` + the rule (`password` matches `password` and `user.password`, not `mypassword`;
  `user.password` matches `a.user.password`). A rule with `*` segments matches the whole path as
  today (`password.*` matches `password.person.value`, not `password`).
- **M2** Matching ignores ASCII case: the rule `password` masks the key `Password` and an untagged
  field `Password` (bug 5).
- **M3** A matched value is replaced by the string `[REDACTED]`. A rule without `*` that matches a
  struct, map or slice replaces it whole; a `*` rule masks the leaves under it and keeps the
  structure.
- **M4** Masking reaches every value of V3: struct fields, map values, slice and array elements,
  `slog.Attr` arguments and the attributes of a `slog.GroupValue`, at any depth up to V5 (bug 4).
  A value of V2 is masked only when its own path matches; its text is never searched.
- **M5** The message and the keys are never masked. Documented limits: an error text, a `Stringer`
  text and a map with keys that are neither strings nor integers are not looked into.
- **M6** `WrapLogger(l, fields)`:
  `l` from `NewLog` or `WrapLogger` and no `fields` → `l` itself (0009 G3);
  `l` from `NewLog` or `WrapLogger` with `fields` → one logger with the rules of both, not a second
  layer (each call converts and masks once, and reaches the innermost logger once);
  a custom `l` → a logger that applies V1-V6 and M1-M5, then calls the same method of `l` once.
- **M7** `WrapLogger(nil, …)` panics `invalid logger: WrapLogger needs a logger` (bootstrap,
  `color.FmtRed`).
- **M8** The fast paths of V4 (nothing to do; scalars only) produce exactly what the general path
  produces.

### `log`: the Pretty format
- **F1** The layout is today's: an empty line, the level label, the time
  (`2006-01-02 15:04:05`, local), the message in brackets, then one row per argument with the tree
  branches, aligned values, sorted map keys, `null` for nil. The existing layout tests keep passing
  against the unexported handler.
- **F2** In the message and in keys, every control character is written escaped and never raw:
  `\n`, `\r`, `\t` as those two characters, any other byte below 0x20, 0x7F and the runes U+0080
  to U+009F as `\x1b`-style hex (bug 1).
- **F3** In string values (and the texts of V2) the same escaping applies, except a line feed: it
  ends the output line, and the next one starts with the row's indentation (at least two spaces),
  so a stack trace stays readable and no value can start a line at column 0, where records start.
- **F4** An `internal/color.Colored` value is printed as its `Text` in its `Color`. The JSON and
  Text formats rely on slog's escaping (bug 1 does not exist there); a test pins it.

### Contract with designs 0007, 0008 and 0009
What `log` gives the dispatch (0007 E3-E4) for
`Logger.Error("UnhandledPanic", "id", id, "method", m, "path", p, "panic", value, "stack", stack)`:

1. **It cannot fail** (V5, V6): any panic value is safe to pass as it is, including a value that
   contains itself and a value whose `Error` or `String` panics. `core` needs no `fmt.Sprint` and
   no `recover` around the call.
2. **It is cheap and has a known cost.** Strings, integers and an `error` take the fast path of V4:
   no reflection, no copy of the arguments. Measured today for an error, an ID and a 1.4 KiB stack:
   3.1 µs / 3 allocs (JSON), 1.25 µs / 5 allocs (Pretty); after P5-P7 an estimated 2.5 µs /
   2 allocs (JSON). The stack is `core`'s to capture and to cap (0007: 4 KiB); `log` adds no stack
   helper and no new exported name.
3. **The value is shown as a developer expects** (V2, V3): an `error` by `Error()` (today empty in
   Pretty, bug 2), a string as it is, a struct by its fields with tags and mask rules applied.
4. **The stack is readable and safe** (F3): one escaped string in JSON and Text, indented lines in
   Pretty.
5. **It is written before `Error` returns** (L6): no queue, so a crash right after loses nothing.
6. **The request ID has one key, `id`**, in `UnhandledPanic`, `ErrorAfterResponse` and the access
   log line (A1), so a line can be joined with its request.

What 0009 gets: `NewLog` holds no package state and starts no goroutine (L1), so two apps are
independent and `NewLog(nil)` is safe as the default of `app.Logger()`; `WrapLogger` follows G3
exactly (M6), and `SetLogger(log.WrapLogger(myLogger, fields))` masks in one pass. What 0008 gets:
`(*AccessLog).Stop` for the end of the shutdown (A10). What 0007 gets from `trace`: `Enabled()` at
1.4 ns replaces the one `HasListeners` per request (P4 there), and `Emit` replaces
`event.Emit(trace.EventName, …)` one for one (T7).

## Performance

### Hot or cold
| Code | Path | Why |
|---|---|---|
| `Tracer.Enabled`, `Tracer.Emit` | hot | every request; every layer of a traced request |
| The access log's listener (stage and `complete` events) | hot | on the request goroutine when the access log is on |
| The access log's worker (formatting, the logger call) | warm | once per request, off the request goroutine; its allocations still cost the garbage collector |
| `Logger.Debug / Info / Warn / Error`, masking, the three formats | hot | user code logs per request; the `UnhandledPanic` line is client-triggered |
| `Tracer.Listen`, `stop`, `NewAccessLog`, `Stop`, `NewLog`, `WrapLogger`, `Fatal` | cold | bootstrap and shutdown |

### Today
| What | Time | Bytes | Allocs |
|---|---|---|---|
| `HasListeners` (`event/event.go:132`) | 17.8 ns; 51.9 ns with 12 goroutines | 0 | 0 |
| `Emit` of one `trace.Event`, one listener | 96.8 ns; 208.6 ns with 12 goroutines | 128 | 2 |
| Access log, one request of 6 stages + `complete`, unique IDs (bus, listener, worker) | 2.9 µs; 3.9 µs with 12 goroutines | 5.1 KiB | 49 (60) |
| Worker, one line: `stageArgs` + masking + JSON handler | 2.3 + 8.9 µs | 3.6 + 1.2 KiB | 45 + 23 |
| `Info` with 4 scalar pairs, JSON | 1.2 µs | 256 | 2 |
| `Info` with a 4-field struct through the mask layer (no handler) | 1.2 µs | 840 | 14 |
| `Debug` with that struct, level Info (dropped) | 1.56 µs | 904 | 15 |

The whole app (`bench/`, a silent logger), access log off → on. One app per process: until design
0009 lands, a second app in the process answers 404 everywhere and its numbers mean nothing.

| Scenario | Off | On | Extra |
|---|---|---|---|
| Static | 801 ns, 9 allocs | 2,507 ns, 30 allocs | +1.7 µs, +21 allocs |
| Chain | 2,849 ns, 25 allocs | 10,310 ns, 90 allocs | +7.5 µs, +65 allocs |
| Exception | 1,872 ns, 10 allocs | 5,964 ns, 45 allocs | +4.1 µs, +35 allocs |
| NotFound | 2,212 ns, 23 allocs | 4,031 ns, 33 allocs | +1.8 µs, +10 allocs |
| Chain, 12 goroutines | 1,471 ns | 12,960 ns | 8.8× |
| Static, 12 goroutines | 357 ns | 2,576 ns | 7.2× |

### Planned changes
Expected: throwaway prototypes of the tracer and of the access log's listener, same machine.

| # | Change | Type | Today | Expected |
|---|---|---|---|---|
| P1 | `Tracer`: the listeners are an immutable slice behind an `atomic.Pointer`; `Enabled` is one atomic load, `Emit` a load and direct typed calls | A | 17.8 ns (51.9 parallel) per check; 96.8 ns, 2 allocs per event | 1.4 ns (0.2 parallel); 8.7 ns, 0 allocs (1.5 ns parallel) |
| P2 | The access log's pending requests live in 64 shards (a mutex and a map each, picked from two bytes of the ID); a stage is kept as `stage, name, duration` (40 B) in a record whose first 8 stages are inline | A | one mutex for all requests; a 112 B event per stage; 4 slice growths for 6 stages | request side, 6 stages + `complete`: 0.65-1.1 µs alone (the worker is woken for each line), 0.2-0.6 µs with 12 goroutines; 1 alloc (352 B). One stage + `complete`: 0.4-0.7 µs |
| P3 | The worker: no map of stages (a linear search over at most 8 stage names), sized slices, one buffer for the texts of a line | A | 45 allocs in `stageArgs` | about 25 (the `[]map[string]Colored` shape of A2 sets the floor) |
| P4 | Level check before any argument work (L3) | A | 1.56 µs, 15 allocs for a dropped line | a few ns, 0 allocs |
| P5 | No copy of the arguments when nothing changes (V4): scalars only, or no rule and no struct | A | 1 alloc (128 B for 4 pairs) on every call | 0 (the scan costs about 16 ns for 4 pairs) |
| P6 | The built-in logger builds the `slog.Record` itself and asks for the caller only with `AddSource` (L5) | A | `slog.Logger` looks the caller up on every call | 1.37 µs instead of 2.05 µs in the same run (−33 %) |
| P7 | The level label needs no `ReplaceAttr` for the four slog levels; only `Fatal` uses a handler that has it | A | `ReplaceAttr` runs for every attribute of every record | 2.05 µs instead of 2.41 µs in the same run (−15 %) |
| P8 | Struct fields (name, index, tag) are read once per type and kept in a `sync.Map` | B | `reflect.Type.Field` and `Tag.Lookup` per field per call: 1.2 µs, 14 allocs for 4 fields | estimated 0.7 µs, 9 allocs; the builder keeps it only if the benchmark shows at least −20 % |
| P9 | The Pretty handler: no `defer` in `Handle`; converted values arrive as maps and scalars, so its own reflection walks less | A | 9.2 µs, 49 allocs for an access log line | to be measured; no target (a development format) |

Targets for the step's "done when" (the builder measures with `bench/`, before and after, and the
numbers go into `accesslog/README.md`): with the access log on, Static costs at most +1 µs and
Chain at most +2.5 µs over the same build with it off, with at most 2 extra allocations on the
request goroutine per request; with 12 goroutines the on / off ratio of Chain is under 2×.

Every fast path has an equality test (M8). New benchmarks use `b.Loop` and `b.ReportAllocs`:
`BenchmarkTracer_Enabled`, `…_Emit`, `…_Emit_Parallel`; `BenchmarkAccessLog_Request_1Stage`,
`…_6Stages`, `…_6Stages_Parallel` (unique IDs), `BenchmarkAccessLog_WorkerLine`;
`BenchmarkLog_Info_Scalars_JSON`, `…_Struct_JSON`, `…_BelowLevel`, `…_ErrorWithStack_JSON`,
`…_AccessLine_Pretty`; the existing ones are converted when touched.

### Heavy techniques
None is proposed.

- **Rejected, H1: reusing the pending records** through a free list per shard. Prototype: 0 allocs
  instead of 1, 956 ns against 1,095 ns when the worker sleeps between lines, but 846 ns against
  684 ns when the worker runs at the same time (it takes the shard locks to give records back).
  No stable gain, more state: not built.
- **Inherited, for approval (Q5):** `log/log_tag.go:46` reads an unexported field that carries a
  `log` tag through `reflect.NewAt` and `unsafe.Pointer`. It is not a speed technique (it is the
  only way to read such a field); it is memory-safe because the struct is addressable (the caller's
  pointer, or a copy made at `log_tag.go:22-26`).

## Concurrency
- **`Tracer`:** no goroutine. Writers (`Listen`, `stop`) hold one mutex and publish a new slice;
  readers load the pointer. An `Emit` that loaded the old slice may call a listener once more after
  its `stop` returned: listeners must accept that (the access log's does, A10).
- **`AccessLog`:** one worker goroutine per instance, ended by `Stop`. State: 64 shards (each a
  mutex guarding its map and its eviction order), the queue (a buffered channel that is never
  closed; the worker leaves on a separate quit signal after draining), an atomic drop counter, an
  atomic stopped flag read first by the listener. Lock order: a shard lock is the only lock held at
  a time; it is released before the queue send, and the logger is never called under a lock. The
  listener runs on request goroutines and never blocks: the send is a `select` with `default`.
- **`log`:** a logger is immutable after construction and safe for concurrent use. No goroutine,
  no file, no timer: there is no transport or file writer to stop (bug 8), the only output is the
  writer given at construction. JSON and Text serialize the `Write` inside slog; the Pretty handler
  formats into its buffer and writes under one mutex (a development format: one line at a time is
  acceptable). The field cache of P8 is a `sync.Map` keyed by `reflect.Type`.
- Masking builds new maps and slices and never writes to the caller's values. Logging a value that
  another goroutine modifies is the caller's data race, as with any reader.
- Tests under `-race`: `TestTracer_ConcurrentListenStopEmit_NoRace`,
  `TestAccessLog_ConcurrentRequests_NoRace`, `TestAccessLog_StopDuringEmit_NoRace`,
  `TestLog_ConcurrentCalls_NoRace` (the three formats, with a mask rule).

## Security
- **Log forging and terminal escapes** (bug 1): F2-F3 for Pretty, slog's escaping for JSON and
  Text, pinned by tests with `\n`, `\r`, `ESC` and a C1 control in the message, a key and a value.
  The access log's message is client-controlled (the path), so this is reachable without any user
  code. The README says: use JSON in production (and L4 picks it when stdout is not a terminal).
- **Masking** closes the bypasses of bugs 3-5 (M2, M4, V2). Its limits are stated, not hidden (M5):
  it is a key-based safety net, not a scanner of texts.
- **Limits on what a client can make the process do:** 64 stages per request, 16,384 pending
  requests, 1,024 queued lines, a 2,048-byte target (A1, A6-A8): the access log's memory is bounded
  (about 6 MiB at the pending cap with up to 8 stages each, about 45 MiB if every pending request
  held 64) whatever arrives. Under overload it loses
  lines and says how many; it never slows a request (Q6). A logged value is walked at most 16 deep
  and 1,000 values wide (V5), which also bounds the cost of logging a decoded request body.
- **The query string is never in the access log** (`Target` is the path, T7): tokens in query
  parameters stay out. The request ID is generated by the framework (`ctx/context.go:17-22`), never
  taken from a header, so it cannot carry forged text.
- **A broken logger or listener cannot kill the process** from the worker (A9). A panic value and
  its stack go to the logger only (0007); mask rules apply to a struct or map panic value.
- No secret is stored by these packages; nothing here reads the environment or the network.

## Impact
- **`core`** (on top of 0007-0009): the field `event *event.Event` becomes `tracer trace.Tracer`;
  every `HasListeners(trace.EventName)` becomes `Enabled()` and every `Emit(trace.EventName, e)`
  becomes `Emit(e)` (today: `core/app.go:128-150`, `:210-213`, `:323-345`, `:421-471`,
  `core/fn.go:162-169`, `core/fn_plan.go:122-127`, `:299`, `:320`, `:359`, `core/ws.go:27`, `:74`;
  WebSocket files change only in these lines). Every emitted event sets `Transport` (T7).
  `initAccessLog` passes `Tracer: &app.tracer`; the shutdown path calls `accesslog.Stop()` (A10).
- **Callers:** `sample/main.go` compiles unchanged for `log` (it sets `Level` and `LogFormat`).
  Nothing outside `log` and `accesslog` uses the removed names (checked with `grep`).
- **File rule:** `trace/trace.go` (events), `trace/tracer.go`; `accesslog/access_log.go` stays the
  package's single file; `log/log.go` (options, `NewLog`, the logger and its five methods),
  `log/log_mask.go` (`WrapLogger`, rules), `log/log_tag.go` (struct fields), `log/log_handler.go`
  (the Pretty handler), `log/fn.go` (helpers shared by those files, bug 22: a pure move, its own
  commit); `internal/color/colored.go`.
- **Tests that change:** `TestNewLog_Singleton`, `TestLoadLogOptions_LevelMinusOneDefaultsToInfo`
  and the `TimeFormat` tests go; `TestAccessLog_TotalEqualsSumOfStageDurations` becomes A3; the
  `event` tests go with the package; the `TestPrettyHandler_…` tests use the unexported handler.
- **Docs:** new `log/README.md`, `accesslog/README.md`, `trace/README.md` (below); `core/README.md`
  › Trace events; `.ginject/package-reference.md` (`event`, `log`, `trace`, `accesslog`),
  `.ginject/architecture-core-concepts.md:505`, `.ginject/metadata-concurrency-model.json:40`,
  `.ginject/metadata-api-index.json`; `internal/color/README.md` (the `Colored` type).

### README content
- **`log/README.md`:** what it is; quick start (`app.SetLogger(log.NewLog(&log.LogOptions{…}))`);
  the options table and defaults; the three formats with one sample line each and "JSON in
  production"; how values are written (V2-V3) and the `log` tag; masking: rule syntax with a table
  of rule / path / masked or not, what is never masked (M5); a custom logger with `WrapLogger`;
  limits (V5); guarantees (never panics, one write per line, safe for concurrent use); performance
  notes (P4-P8) and the benchmark table.
- **`accesslog/README.md`:** enabled with `app.EnableAccessLog()`; the line (keys, a Pretty and a
  JSON sample); what `total` and the percentages mean; the limits and what happens under overload
  (A6-A8, the `AccessLog dropped` warning); shutdown (A10); the measured cost per request (the
  targets above, with the builder's numbers); internals in five lines (shards, queue, worker).
- **`trace/README.md`:** the event and its fields per stage (T7); the stages and transports; the
  `Tracer` (zero value, `Listen` / `stop`, synchronous listeners, no `recover`); cost (P1); a note
  that apps cannot subscribe yet (the devtools platform will add the way).

## Test plan
| Rule | Tests | Cases |
|---|---|---|
| T1-T4 | `TestTracer_ZeroValueDisabled`, `…_ListenEnablesAndReceives`, `…_ListenersRunInOrder`, `…_StopRemovesOnlyItsRegistration`, `…_StopTwice`, `…_LastStopDisables`, `…_ListenNil` | the same function registered twice; stop of the first keeps the second |
| T5-T6 | `…_EmitAllocatesNothing` (`testing.AllocsPerRun`), `…_ListenInsideListener`, `…_StopInsideListener`, `…_ListenerPanicPropagates`, `…_ConcurrentListenStopEmit_NoRace` | |
| T7 | in `core`: `TestTrace_EveryEventCarriesTransport` (HTTP and WS, each stage), `TestTrace_CompleteTargetHasNoQuery`, `TestTrace_NothingEmittedWithoutListener` | a route with every layer and a pipe |
| A1-A3 | `TestAccessLog_LineArgumentsInOrder`, `…_StagesInArrivalOrder`, `…_ShortName`, `…_TotalIsCompleteDuration`, `…_NoStagesLogsFixedPairsAndTotal`, `…_LongTargetIsCut`; the existing `TestDistributePercentages_…`, `TestFormatDuration_…`, `TestColorForDuration_…` | complete 250 ms with one 1 ms stage → `total` `250ms`; a 3,000-byte target; a target cut inside a multi-byte rune |
| A4 | in `log`: `TestLog_ColoredIsPlainTextInJSON`, `…_InText`, `TestPretty_ColoredUsesItsColor` | no `\u001b` in the JSON line |
| A5 | `TestAccessLog_SeparatesInterleavedIDs`, `…_ConcurrentRequests_NoRace` | 8 goroutines × 500 requests: every line has its own stages only |
| A6-A7 | `…_StagesCappedAt64`, `…_PendingIsBounded`, `…_EvictedRequestLogsWithoutStages`, `…_NewRequestStillTracedWhenFull` | 100 stages → 64 kept, `stages_dropped` 36; 100,000 IDs without `complete` → `pendingLen() <= 16384` |
| A8-A9 | `…_FullQueueDropsAndCounts`, `…_DroppedPlusLoggedEqualsCompleted`, `…_RequestNeverBlocksOnSlowLogger`, `…_LoggerPanicIsRecovered` | a logger blocked on a channel: 5,000 emits return at once; after release and `Stop`, logged + reported = 5,000 |
| A10-A12 | `…_StopFlushesQueue`, `…_StopTwice`, `…_EventsAfterStopIgnored`, `…_StopReturnsWhenLoggerBlocks` (under 3 s), `…_StopEndsWorker`, `…_StopDuringEmit_NoRace`, `TestNewAccessLog_NilPanics` (config, tracer, logger), `…_WSLine` | |
| L1-L3 | `TestNewLog_NewLoggerEachCall` (0009), `…_NilOptions`, `…_DoesNotModifyOptions`, `…_LaterOptionChangesIgnored`, `TestLog_BelowLevelWritesNothing`, `…_BelowLevelCallsNoMethod`, `…_BelowLevelAllocatesNothing` | a `Stringer` that counts its calls |
| L4-L7 | `TestLog_JSONLine`, `…_TextLine`, `…_LevelLabels` (five levels × three formats), `…_DefaultFormatTerminal`, `…_DefaultFormatNotTerminal`, `…_AddSourceIsCaller` (three formats, and through `WrapLogger`), `…_OneWritePerRecord` (a writer that counts), `…_FatalLogsThenExits` (`exit` replaced) | |
| V1-V4 | `TestLog_AttrArgument`, `…_OddArguments`, `…_NonStringKey`, `…_ErrorShowsText` (three formats; `errors.New`, `fmt.Errorf`), `…_PointerStringerNotExpanded` (`*url.URL` with a password), `…_LogValuerResolved`, `…_NilPointerIsNull`, `…_TypedNilError`, `…_StructBecomesMap`, `…_TagRenames`, `…_TagDash`, `…_TaggedUnexportedField`, `…_IntegerKeyMap`, `…_UnchangedValueIsSameValue`, `…_ScalarArgsNotCopied` | |
| V5-V6 | `…_SelfReferenceEnds` (with a 2 s timeout), `…_DepthLimit` (17 levels), `…_NodeLimit` (2,000 keys), `…_PanickingStringerRecovered`, `…_PanickingErrorRecovered`, `…_PanickingLogValuerRecovered` | each in the three formats and through a wrapped custom logger |
| M1-M5 | the existing `TestWrapLogger_…` rule tests, plus `…_CaseInsensitive` (key, untagged field, rule in upper case, a `*` rule), `…_MasksInsideSlice`, `…_InsideArray`, `…_InsideSliceOfMaps`, `…_InsideIntKeyMap`, `…_AttrArgument`, `…_GroupValue`, `…_StringerMaskedByItsPath`, `…_MessageNeverMasked`, `…_KeysNeverMasked` | |
| M6-M8 | `…_BuiltInWithoutFieldsIsSame`, `…_BuiltInWithFieldsMasksOnce`, `…_TwiceMergesRules` (the custom logger is called once per line), `…_CustomLoggerGetsMaps`, `…_NilPanics`, `TestLog_FastPathEqualsGeneralPath` (random arguments: scalars, structs, maps, slices, with and without rules, against the general path forced) | |
| F1-F4 | the existing `TestPrettyHandler_…` layout tests; `TestPretty_MessageControlCharsEscaped`, `…_KeyControlCharsEscaped`, `…_ValueEscapeCharEscaped`, `…_ValueNewlineIsIndented` (no output line starts at column 0 with the value's text), `…_C1ControlEscaped`, `TestLog_JSONEscapesControlChars`, `…_TextEscapesControlChars` | `"/a\n\x1b[2K FAKE"`, `"x\r\nERROR forged"`, U+0085 |

## If Q1 is refused: the bus stays
`trace.Tracer` is not added; `core` and `accesslog` keep `event.Event`, which gets these rules
instead (the rest of the design is unchanged, the access log's listener asserts
`args[0].(trace.Event)` as today):

- **EV1** The listeners are an immutable snapshot behind an `atomic.Pointer`; `HasListeners`,
  `Emit`, `ListenerCount` and `EventNames` take no lock. Prototype: `HasListeners` 16 ns (3.2 ns
  with 12 goroutines); `Emit` keeps 2 allocs and 128 B per trace event (the event is boxed into
  `...any`); a request of 6 stages + `complete` 2.05 µs and 16 allocs, against 1.1 µs and 2 with the
  tracer in the same run.
- **EV2** `On` and `Once` return a `func()` that removes that registration; `Off` is removed
  (bug 18: two closures cannot be told apart).
- **EV3** Listeners run in registration order, once-listeners in their place (bug 20); a
  once-listener runs at most once under concurrent emits.
- **EV4** A listener panic is recovered (one `defer` per `Emit`: +3 ns in the prototype) and
  reported on stderr as today; the other listeners still run.

If Q1 is accepted but the package should stay as a utility, apply EV2-EV3 only (it is then cold).

## Open questions
1. **A typed tracer instead of the event bus, and `event` deleted (T1-T7).** The bus costs 2
   allocations per event (boxing) and a string-keyed lookup per check, which no rewrite of the bus
   removes; the tracer costs 0 allocs and 9 ns, is type-safe for listeners, and is 4 exported names
   instead of 12. Against: one more breaking removal, and the roadmap named `event` as the carrier.
   **Recommendation: accept, and delete `event`** (nothing else uses it; `memorybroker` covers
   publish / subscribe). The fallback is the section above.
2. **Default format by output (L4):** Pretty on a terminal, JSON otherwise, so a container gets
   machine-readable logs without ANSI codes and without configuration (design 0008 S4 hides the
   banner the same way). Today the default is always Pretty. **Recommendation: accept.**
3. **`LogOptions`: drop `Transports` and `TimeFormat`, add nothing.** `Transports` never worked;
   implementing it means files, rotation and their shutdown, which the process manager does
   better. `TimeFormat` only affects Pretty. An `Output io.Writer` field would be the small escape
   hatch (stderr, a file), at the price of one more option. **Recommendation: remove both, no
   `Output`** (stdout only; anything else is a custom logger behind `WrapLogger`).
4. **Unexport the Pretty handler and move `Colored` to `internal/color`.** Users lose
   `slog.New(log.NewPrettyHandler(…))` and colored values in their own lines; nobody in the
   repository uses either. **Recommendation: accept** (13 fewer exported names, bug 11 gone).
5. **Keep reading tagged unexported fields with `unsafe` (V3)?** Removing it would make `log` tags
   behave like `json` tags (exported fields only) and take `unsafe` out of `log`; keeping it
   preserves a tested behavior. **Recommendation: keep**, as an approved use.
6. **Under overload the access log drops lines (A8) instead of slowing requests.** That is today's
   behavior made visible. Blocking would make it complete but lets a slow log pipe stall the
   server. **Recommendation: drop and report**; a team that needs an audit trail writes it in a
   middleware or interceptor.
7. **Mask rules ignore case (M2).** A rule now also masks keys it did not match before
   (`Password`, `PASSWORD`). **Recommendation: accept**: the unmasked default is the dangerous one.

## Decisions (2026-11-16)

Approved by the user: every open question answered as recommended.

- Q1 the typed `trace.Tracer` replaces the event bus; the `event` package is deleted.
- Q2 Pretty on a terminal, JSON otherwise.
- Q3 `Transports` and `TimeFormat` removed; no `Output` field.
- Q4 the Pretty handler is unexported and `Colored` moves to `internal/color`.
- Q5 the `unsafe` read of tagged unexported fields (V3) is kept as an approved use.
- Q6 under overload the access log drops lines and reports the count.
- Q7 mask rules ignore case.
