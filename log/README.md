# Log

*Package `log` is the built-in logger of `Ginject`: a `common.Logger` on top of `log/slog` with a JSON, text or pretty format, `log` struct tags and field masking that also apply to a logger you bring, `From(ctx)` for the IDs of the request a line belongs to, and an output that never makes a request wait for stdout while an app serves.*

- [Log](#log)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [`LogOptions` Parameters](#logoptions-parameters)
    - [Level](#level)
    - [LogFormat](#logformat)
    - [AddSource](#addsource)
    - [MaskFields](#maskfields)
  - [Formats](#formats)
  - [Find the logs of a request](#find-the-logs-of-a-request)
  - [Functions](#functions)
    - [NewLog](#newlog)
    - [WrapLogger](#wraplogger)
    - [From](#from)
  - [How values are written](#how-values-are-written)
  - [Struct tags](#struct-tags)
  - [Masking](#masking)
  - [Limits and guarantees](#limits-and-guarantees)
  - [Output pipeline](#output-pipeline)
  - [Other exported names](#other-exported-names)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

## Key Features
- One logger per call: `NewLog` builds a new, independent logger from its options every time.
- JSON when stdout is not a terminal, the pretty tree when it is, with no configuration.
- Errors, `fmt.Stringer`s and `slog.LogValuer`s are written through their method, never expanded.
- Masking by path, case-insensitive, at any depth: `password` masks `Password`, `user.password` and a `password` inside a slice of structs.
- A call never panics because of a value and never forges a line: control characters are escaped.
- The tags and masking also apply to your own logger through `WrapLogger`.
- `logger.From(ctx)` adds the trace ID and the request, connection or message ID of `ctx` to every line, with the built-in logger and with your own.
- While an app serves, a call encodes its line and hands it to a bounded queue: a slow or stuck stdout never blocks a request; under overload Debug and Info are dropped first and counted.

## Usage

```go
package main

import (
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/log"
)

type User struct {
	Name     string `log:"name"`
	Password string `log:"password"`
}

func main() {
	app := core.New()
	app.SetLogger(log.NewLog(&log.LogOptions{
		Level:      log.DebugLevel,
		LogFormat:  log.JSONFormat,
		MaskFields: []string{"password"},
	}))
	app.Create(core.ModuleBuilder().Build())

	app.Logger().Info("SignedIn", "user", User{Name: "duoc", Password: "secret"})
}
```

The line holds `"user":{"name":"duoc","password":"[REDACTED]"}`.

## `LogOptions` Parameters

`NewLog` copies the options: later changes to the struct or to its `MaskFields` slice have no effect, and the options are never modified.

### Level
Type: `slog.Level`

Default: `log.InfoLevel` (the zero value of `slog.Level`)

Required: `false`

The lowest level that is written: `DebugLevel`, `InfoLevel`, `WarnLevel`, `ErrorLevel` or `FatalLevel`. The value is used as given.

### LogFormat
Type: `logFormat`

Default: `log.PrettyFormat` when stdout is a terminal (a character device), `log.JSONFormat` otherwise

Required: `false`

`log.JSONFormat`, `log.TextFormat` or `log.PrettyFormat`. A format you set wins whatever the output is. Use JSON in production: a container then gets machine-readable lines without ANSI codes.

### AddSource
Type: `bool`

Default: `false`

Required: `false`

Adds the file and line of the code that called the logger method, also through `WrapLogger`: slog's `source` in JSON and Text, a last row `source` with `file:line` in Pretty. Without it no caller lookup is made.

### MaskFields
Type: `[]string`

Default: `nil`

Required: `false`

The paths to mask, see [Masking](#masking).

## Formats

The level is written `DEBUG`, `INFO`, `WARN`, `ERROR` or `FATAL` in all three formats.

| Format | Line |
|---|---|
| `JSONFormat` | `{"time":"2026-11-16T10:00:00+07:00","level":"INFO","msg":"SignedIn","user":{"name":"duoc"}}` |
| `TextFormat` | `time=2026-11-16T10:00:00.000+07:00 level=INFO msg=SignedIn user=map[name:duoc]` |
| `PrettyFormat` | an empty line, the colored level label, the time (`2006-01-02 15:04:05`, local), the message in brackets, then one row per argument as a tree (map keys sorted, `null` for nil) |

Output is stdout, through the [output pipeline](#output-pipeline). To write elsewhere, bring a logger with `app.SetLogger(myLogger)` (or `log.WrapLogger(myLogger, fields)` to add mask rules).

## Find the logs of a request

Every request gets two IDs, and every line logged with `From(ctx)` carries them:

| Key | What it is | Where the client sees it |
|---|---|---|
| `trace_id` | 32 hex characters (128 bits). The ID of the whole chain of calls: a service you call through `trace.Transport` logs the same `trace_id`. A trace that Ginject starts is unique: 48 bits of milliseconds, then 80 random bits, so its first 12 characters also tell when it started. | the second field of the `Traceparent` response header |
| `request` | 16 hex characters (64 bits), always generated by this service. The ID of one request in one service. | the `X-Request-Id` response header, and the third field of `Traceparent` |
| `upstream` | the caller's ID: the parent ID of an incoming `traceparent` (the caller's `request` when it is a Ginject service), else a valid incoming `X-Request-Id` | (not sent back) |

A response looks like this:

```http
HTTP/1.1 200 OK
X-Request-Id: 00f067aa0ba902b7
Traceparent: 00-019a8b3c4d5e7f3a9c1e2b4d6f8a0c2e-00f067aa0ba902b7-03
```

Use them as follows:

- **Search by `trace_id`** to see everything one user action caused, across every service it
  reached. It is the best key when you have it.
- **Search by `request`** to see one request in one service, when all you have is an
  `X-Request-Id` (from a client's bug report or a proxy log). A random 64-bit ID can repeat across
  billions of requests, so search within a time window around the request, or together with its
  `trace_id`: the pair is unique.
- A client that sends a valid `traceparent` continues its own trace, so several requests can share
  one `trace_id` (a retry, or a client that reuses a value). The pair `trace_id` and `request`
  still names exactly one request.
- The access log (`app.EnableAccessLog()`) writes one line per request with the same keys, so the
  access line and your own lines of a request match on the same query.

With the JSON format, filter the lines with `jq`:

```sh
jq -c 'select(.trace_id == "019a8b3c4d5e7f3a9c1e2b4d6f8a0c2e")' app.log
jq -c 'select(.request == "00f067aa0ba902b7")' app.log
```

In Grafana Loki, parse the JSON line and filter on the key:

```logql
{app="shop"} | json | trace_id = "019a8b3c4d5e7f3a9c1e2b4d6f8a0c2e"
{app="shop"} | json | request = "00f067aa0ba902b7"
```

### Why `From(ctx)` matters

Only a line logged through `From(ctx)` carries the IDs. A plain `s.Logger.Info(...)` in a service
has no `trace_id` and no `request`, so no query can tie it to the request it belongs to. Pass the
request's context to your services and log through it:

```go
func (s OrderService) Place(ctx context.Context, order Order) {
	s.Logger.From(ctx).Info("OrderPlaced", "order", order.ID)
}
```

In a handler, the context is `c.Context()`. It stays valid after the handler returns, so a
goroutine you start from the handler still logs the request's IDs. The framework's own lines about
a request (`UnhandledPanic`, `ErrorAfterResponse` and the access log) already carry them.

## Functions

### NewLog

Builds a new logger from the options. Output goes to stdout.

#### Parameters
- 1st parameter: `*LogOptions`

- Description: The options; `nil` means level Info, the default format, no source and no mask rule

#### Returns
- 1st value: `common.Logger`

#### Usage

```go
logger := log.NewLog(&log.LogOptions{LogFormat: log.TextFormat})
```

#### Rules
- Every call returns a new logger; the options of each call apply to the logger it returns, and the `MaskFields` of one call mask on that logger only.
- JSON is one object per line with slog's keys (`time`, `level`, `msg`, then the arguments); Text is slog's `key=value` line.
- A call below the level writes nothing, calls no method of an argument and allocates nothing.
- Every record is written whole; records of concurrent calls never interleave, and records of one goroutine keep their call order, across loggers too.
- Every logger from `NewLog` writes to the one stdout pipeline. Without an app (a tool, a test) it is synchronous: the line is written before the call returns and no goroutine is started.
- Values are read during the call only: changing an argument afterwards never changes the line.
- `Fatal` writes the record at the `FATAL` level, flushes the pipeline for at most 2 seconds, then exits the process with status 1, whatever the level. A stuck stdout cannot hold it.
- Arguments are paired exactly like `log/slog`: a `string` key takes the next argument as its value, a `slog.Attr` is one pair, a trailing key without a value is written under `!BADKEY`, and a non-string, non-`Attr` argument is written alone under `!BADKEY` (`"!BADKEY":42`), so the argument after it starts the next pair. Masking follows the same pairing: in `Error("login failed", err, "password", pw)` the password is masked.

### WrapLogger

Returns a logger that converts and masks the arguments (see [How values are written](#how-values-are-written) and [Masking](#masking)), then calls the same method of `next` once.

#### Parameters
- 1st parameter: `common.LogAdapter`

- Description: The logger to wrap: any type with `Debug`, `Info`, `Warn`, `Error` and `Fatal` (zap's `SugaredLogger` shape)

- 2nd parameter: `[]string`

- Description: The paths to mask; `nil` masks nothing

#### Returns
- 1st value: `common.Logger`

#### Usage

```go
logger := log.WrapLogger(myLogger, []string{"password", "token.*"})
```

#### Rules
- A logger from `NewLog` or `WrapLogger` with no fields is returned as it is.
- A logger from `NewLog` or `WrapLogger` with fields becomes one logger with the rules of both, not a second layer: each call converts and masks once and reaches the innermost logger once.
- Any other logger is wrapped; each method calls the same method of that logger once, with structs as `map[string]any` and changed slices as `[]any`.
- `maskFields` is copied.
- A value that needs no conversion and no masking is passed on as the same value, and when no argument needs any, the argument slice itself is passed on.
- An error, a `fmt.Stringer` or a `slog.LogValuer` reaches a wrapped logger unchanged, and none of its methods is called, except that with mask rules a `slog.LogValuer` is resolved once to mask a group it returns (a panic in `LogValue` is recovered and the value passed on unchanged).
- `WrapLogger(nil, fields)` panics with `invalid logger: WrapLogger needs a logger`.
- A wrapped logger is called directly, on the caller, in every mode: its order and blocking are its own.
- `Fatal` on a wrapped logger flushes the stdout pipeline for at most 2 seconds, then calls the wrapped `Fatal` once.
- Safe for concurrent calls.

### From

`From(ctx context.Context) common.Logger` is a method of every `common.Logger` (`NewLog`, `WrapLogger`, the logger `Ginject` injects). It returns the same logger with the fields of the request, WebSocket connection or message that `ctx` belongs to: `trace_id`, `request`, `connection`, `message`, `upstream`, in that order, empty ones skipped. Inside a handler, a guard or a service called with it, pass `c.Context()`.

#### Parameters
- 1st parameter: `context.Context`

- Description: A context from `Ginject` (`c.Context()`, or a context derived from it); `nil` is allowed

#### Returns
- 1st value: `common.Logger`

#### Usage

```go
package users

import (
	"context"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
)

type UserService struct {
	Logger common.Logger
}

func (instance UserService) NewProvider() core.Provider {
	return instance
}

func (instance UserService) SignIn(ctx context.Context, name string) {
	instance.Logger.From(ctx).Info("SignedIn", "name", name)
}
```

The built-in JSON logger writes `{"time":"…","level":"INFO","msg":"SignedIn","name":"duoc","trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","request":"00f067aa0ba902b7"}`.

#### Rules
- A `nil` context, a context without a scope, or a scope with no field returns the receiver itself; `From` never panics.
- Built-in logger: the fields come after the call's own arguments, encoded and masked like any other pair (a mask rule on `request` writes `[REDACTED]`). The line equals `Info(msg, append(args, fields...)...)` byte for byte, the time aside; a malformed argument (`!BADKEY`) never pairs with a field. Pretty prints them as rows after the arguments.
- Wrapped logger: the wrapped method is called once with the message, the fields first, then the converted and masked arguments; the fields are masked by the rules of every wrapper.
- Keys you also pass are kept twice, as slog does.
- Scopes replace, they do not stack: `l.From(a).From(b)` is `l.From(b)`, and `l.From(a).From(context.Background())` is `l.From(a)`.
- Below the level nothing is done: no method of an argument is called.
- With `AddSource`, the source is the caller of `Info`, as without `From`.
- The receiver is not modified. The scoped logger is kept with the request's scope: a later `From` on the same scope and logger returns it without allocating, from any goroutine, during and after the request. Two loggers on one scope never mix their formats or mask rules.
- `From(ctx).Fatal(…)` writes the record with the fields, then exits as `Fatal` does.

## How values are written

- A value whose type, or pointer type, implements `error`, `fmt.Stringer` or `slog.LogValuer` is written through that method (`Error()`, `String()`, the resolved `LogValue()`), in every format: a `*url.URL` is its URL, never its fields. A nil pointer or nil interface is written as `null` without calling a method.
- Any other struct becomes a map of its fields (see [Struct tags](#struct-tags)); pointers are followed.
- Maps with string or integer keys become `map[string]any` (integer keys in decimal) when one of their values changes; slices and arrays become `[]any` when one of their elements changes.
- `time.Time` and `time.Duration` keep slog's formatting.

## Struct tags

- A field tagged `log:"<name>"` is logged under `<name>`, also when it is unexported.
- An untagged exported field is logged under its Go name; an untagged unexported field is not logged.
- `log:"-"` skips a field, even an exported one.
- Nested structs are expanded the same way.
- A struct that implements `fmt.Stringer`, `error` or `slog.LogValuer` is not expanded.

## Masking

A value's path is its key, then the keys under it joined by `.` (`user.password`). Elements of a slice or array have the path of the slice. A matched value is replaced by `[REDACTED]`. Matching ignores ASCII case.

| Rule | Path | Masked |
|---|---|---|
| `password` | `password`, `Password`, `user.password` | yes |
| `password` | `mypassword`, `userpassword` | no |
| `user.password` | `user.password`, `a.user.password` | yes |
| `user.password` | `admin.password` | no |
| `password.*` | `password.person.value` | yes, the leaves under it; the structure is kept |
| `password.*` | `password` | no |

- A rule without `*` that matches a struct, map or slice replaces it whole.
- Masking reaches struct fields, map values, slice and array elements, `slog.Attr` arguments and the attributes of a `slog.Group`, at any depth up to the limits below.
- A tagged field is matched by its tag name.
- An error, a `Stringer` or a `LogValuer` is masked only when its own path matches; its text is never searched.
- A `LogValuer` that resolves to a group (`slog.GroupValue`) is masked by path like any group: a rule `password` or `user.password` masks `password` inside `"user", valuer`. A wrapped logger then receives the masked group as a `slog.Value`; an unmasked one still reaches it unchanged.
- Never masked: the message, the keys, the text of an error or a `Stringer`, and the values of a map whose keys are neither strings nor integers.

## Limits and guarantees

- Nesting is followed 16 levels deep; a value below that is written `[MAX_DEPTH]`. After 1,000 values inside one argument, each remaining value is written `[TRUNCATED]`. A value that contains itself ends.
- A panic raised by a value's own `String`, `Error` or `LogValue` is recovered: the call writes one line with the original level and message and a `log_error` pair with the recovered value, and the logger keeps working.
- In Pretty, every control character of the message and of the keys is written escaped (`\n`, `\r`, `\t`, `\x1b`, the C1 controls U+0080 to U+009F; the separators U+2028 and U+2029 and the bidirectional controls U+202A to U+202E and U+2066 to U+2069 as `\u2028`), so a value cannot forge a line, reorder the text on screen or send terminal escape codes. In string values a line feed starts a new, indented line (a stack trace stays readable); everything else is escaped.
- JSON and Text rely on slog's escaping; JSON also escapes the C1 controls.
- A logger is immutable and safe for concurrent use; it opens no file. A record longer than 64 KiB is cut (see [Output pipeline](#output-pipeline)).

## Output pipeline

Every logger from `NewLog` writes to one process-wide stdout pipeline, so lines of all loggers share one order and one memory bound.

- **Synchronous** without an app, during `app.Create` (its `OnReady` hooks included) and after shutdown: a call returns after its line was written, nothing is dropped and no goroutine exists. A bootstrap panic loses no line.
- **Asynchronous** from the end of `Create` until the shutdown hooks ran: the caller encodes and masks its line, appends it to a queue and returns. One writer goroutine, alive only while it writes, writes whole records in batches of at most 64 KiB, in queue order.
- **Latency:** while the app serves, a line reaches stdout up to about 1 ms after the call (sooner once 16 KiB are queued). The writer is woken about once per millisecond instead of once per line, which keeps the tail latency of a one-CPU app low. `Fatal` writes at once, and the shutdown flush writes everything without waiting.
- **Overload:** the queue holds at most 1 MiB. Debug and Info lines are dropped once the queue would pass 80 % of it; Warn and Error are kept while they fit, otherwise the caller waits at most 100 ms for room, then the line is dropped. After one wait timed out, Warn and Error are dropped at once until stdout accepts a write again. Fatal is always kept.
- **Drops are reported:** a `WARN LogDropped` line with one count per level that dropped something (`debug`, `info`, `warn`, `error`), written at most once per second while busy and when the queue runs empty, where the writer is (not where the lines were dropped). Lines written plus counts reported equal lines logged.
- **Long records:** a record over 64 KiB is cut on a UTF-8 boundary and ends with `...[TRUNCATED]` and a line feed (Pretty resets the color first). A cut JSON record is not valid JSON; the head with the time, level and message is kept.
- **Errors:** a failed or panicking `Write` of stdout discards that batch, never retried; the next one is written.
- **Shutdown:** the app flushes the pipeline (at most 2 seconds) after its `OnShutdown` hooks; `Fatal` flushes before it exits.
- **What can be lost:** lines still queued when the process dies without shutdown or `Fatal` (an unrecovered panic in a goroutine, `os.Exit` in your code, `SIGKILL`): at most 1 MiB, in practice the last batch plus up to about 1 ms of lines still waiting for the writer (below 16 KiB). Bytes you write to `os.Stdout` yourself are not ordered with log lines while the app serves. When stdout is stuck and the shutdown flush timed out, lines logged afterwards block their caller until stdout accepts a write again; `Fatal` stays bounded (it waits at most 2 seconds, then exits).
- **Memory:** about 300 B while unused; two buffers and one stopped timer of at most 64 KiB each once the queue is empty again; at most two 1 MiB buffers and one goroutine with stdout stuck.
- Loggers given to `app.SetLogger` are not part of the pipeline: they are called directly.

## Other exported names

- `DebugLevel`, `InfoLevel`, `WarnLevel`, `ErrorLevel`, `FatalLevel` (`slog.Level`; `FatalLevel` is `12`).
- `TextFormat`, `JSONFormat`, `PrettyFormat` (`logFormat`).

## Performance notes

- **Level first.** A call below the level returns before touching its arguments (about 5 ns, no allocation).
- **Encode on the caller, hand over in one append.** Each logger keeps a `sync.Pool` of encoders (a byte buffer plus the slog handlers that write into it); the record is encoded there, then copied once into the pipeline's queue. An encoder whose buffer grew past 4 KiB is not put back. The pipeline swaps two buffers between callers and the writer under one mutex and never holds it during a `Write` of stdout.
- **`From` once per request.** The first `From` on a scope builds the scoped logger (about 300 ns, 2 allocations) and keeps it in the scope; later calls are a lookup (about 6 ns, no allocation). When every field is a plain ID (`[A-Za-z0-9._:-]`, at most 128 bytes) and no mask rule matches a key, JSON and Text keep the fields encoded and splice them before the end of the line, so a scoped line costs what a plain one does; otherwise, and always in Pretty, the fields are added as attributes.
- **No copy when nothing changes.** Scalars and errors take a path without reflection; through `WrapLogger` the argument slice is passed on as it is when no value needs work.
- **The record is built once.** The built-in logger builds the `slog.Record` itself and looks up the caller only with `AddSource`.
- **No `ReplaceAttr` per attribute.** The four slog levels need none; only `Fatal` uses a handler that renames its level.
- **Struct fields are read once per type** (name, index, tag) and kept in a `sync.Map`. Measured against reading them on every call: a struct logged as JSON takes 2.7 µs instead of 4.8 µs (−44 %) with 16 allocations instead of 28, and a tagged struct through `WrapLogger` 0.53 µs instead of 1.66 µs (−68 %).
- **A recover only when a method can run.** The scalar path has no `defer`; arguments with values whose methods may be called go through one deferred `recover`.

## Benchmarks

Machine-dependent, captured when this README was written. `Async` and `SlowOutput` run on an asynchronous pipeline (a discarding output, or one that sleeps 2 ms per write: most of those lines are dropped above the soft limit); the others write synchronously to a discarding output. `BenchmarkFrom_First` includes the benchmark's own scope and context (2 of the 4 allocations).

```sh
go test -run '^$' -bench . -benchmem ./log/
```

```console
goos: darwin
goarch: amd64
cpu: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
BenchmarkLog_Info_Scalars_JSON-12                        	  797635	      1283 ns/op	     144 B/op	       3 allocs/op
BenchmarkLog_Info_Struct_JSON-12                         	  420738	      2959 ns/op	     601 B/op	      16 allocs/op
BenchmarkLog_Info_Struct_JSON_WithMaskRule-12            	  379197	      3378 ns/op	     658 B/op	      20 allocs/op
BenchmarkLog_BelowLevel-12                               	292739292	         4.031 ns/op	       0 B/op	       0 allocs/op
BenchmarkLog_ErrorWithStack_JSON-12                      	  311721	      4006 ns/op	     176 B/op	       2 allocs/op
BenchmarkLog_AccessLine_Pretty-12                        	  306849	      3942 ns/op	    1552 B/op	      13 allocs/op
BenchmarkLog_Info_LargeMap_Pretty-12                     	  120024	      9467 ns/op	    3082 B/op	      48 allocs/op
BenchmarkWrapLogger_Info_TaggedStruct-12                 	 1695045	       712.0 ns/op	     568 B/op	       9 allocs/op
BenchmarkWrapLogger_Info_TaggedStruct_WithMaskRule-12    	 1377609	       867.6 ns/op	     600 B/op	      11 allocs/op
BenchmarkWrapLogger_Info_NoStructArgs-12                 	44116102	        28.43 ns/op	       0 B/op	       0 allocs/op
BenchmarkLog_Info_Scalars_JSON_Async_1-12                	  844498	      1404 ns/op	     146 B/op	       3 allocs/op
BenchmarkLog_Info_Scalars_JSON_Async_12-12               	 2296998	       522.8 ns/op	     193 B/op	       3 allocs/op
BenchmarkLog_Info_Scalars_JSON_Sync_1-12                 	  780621	      1289 ns/op	     144 B/op	       3 allocs/op
BenchmarkLog_Info_SlowOutput_1-12                        	  873627	      1221 ns/op	     149 B/op	       3 allocs/op
BenchmarkLog_Info_SlowOutput_12-12                       	 3131480	       412.5 ns/op	     146 B/op	       3 allocs/op
BenchmarkFrom_NoScope-12                                 	320341690	         3.692 ns/op	       0 B/op	       0 allocs/op
BenchmarkFrom_First-12                                   	 4286910	       280.7 ns/op	     536 B/op	       4 allocs/op
BenchmarkFrom_Cached-12                                  	207618955	         5.747 ns/op	       0 B/op	       0 allocs/op
BenchmarkLog_From_Info_JSON-12                           	 1672680	       716.5 ns/op	      64 B/op	       1 allocs/op
BenchmarkLog_From_Info_JSON_Adapter-12                   	11163579	       107.3 ns/op	     192 B/op	       2 allocs/op
```
