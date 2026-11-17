# Access Log

*Package `accesslog` writes one log line per HTTP request or WebSocket message, with the time each middleware, guard, interceptor, pipe, handler and filter took. The line is formatted and logged on the request goroutine (a few microseconds), with no worker, queue or timer of its own.*

- [Access Log](#access-log)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [The line](#the-line)
  - [`AccessLogConfig` Parameters](#accesslogconfig-parameters)
    - [Tracer](#tracer)
    - [Logger](#logger)
  - [Functions](#functions)
    - [NewAccessLog](#newaccesslog)
    - [Stop](#stop)
  - [Limits and overload](#limits-and-overload)
  - [Cost per request](#cost-per-request)
  - [Internals](#internals)
  - [Benchmarks](#benchmarks)

## Key Features
- One switch: `app.EnableAccessLog()`. The app creates and stops the access log.
- `total` is the wall-clock time of the request; each stage shows its time and its share.
- Bounded memory whatever arrives: 64 stages per request, 16,384 pending requests.
- No goroutine, no ticker, no queue: with the built-in logger the line goes through the log's output pipeline, so a slow stdout never slows a request and lines dropped under overload are counted in `LogDropped`.
- Every value of the line is a string or an int, so any log library can read it.
- The query string is never logged.

## Usage

```go
package main

import (
	"github.com/dangduoc08/ginject/internal/core"
	"github.com/dangduoc08/ginject/log"
)

func main() {
	app := core.New()
	app.SetLogger(log.NewLog(&log.LogOptions{LogFormat: log.JSONFormat}))
	app.EnableAccessLog()
	app.Create(core.NewModuleBuilder().Build())
	_ = app.Listen(8080, nil)
}
```

## The line

One `Logger.Info` call per request, made by the goroutine that emitted the `complete` event (the request's), before the event returns. The message is the request's path (or the WebSocket topic), cut to 2,048 bytes on a rune boundary and followed by `...` when longer. The arguments, in order:

| Key | Value |
|---|---|
| `trace_id` | the trace ID (32 hex characters, the `Traceparent` header's trace) |
| `request` | HTTP only: the request ID (the `X-Request-Id` header) |
| `ip` | HTTP only: the client's address (`trace.Event.ClientIP`, which `proxy` resolves behind trusted proxies; the connection's peer otherwise), as a string in its short form (`203.0.113.7`, `2001:db8::1`); left out when unknown |
| `connection` | WebSocket only: the connection ID (the handshake's request ID); left out when the message has none |
| `message` | WebSocket only: the message ID |
| `transport` | `HTTP` or `WS` |
| `code` | the status written (an `int`; `0` for WebSocket messages) |
| `operation` | the HTTP method, or the WebSocket message type |
| one key per stage | `middleware`, `guard`, `pre_interceptor`, `pipe`, `handler`, `post_interceptor`, `exception_filter`, in the order their first event arrived; the value is one string with one `<name> <duration> (<percent>%)` entry per event, in arrival order, joined by `, ` (`"CORS 40µs (4%), CSRF 60µs (6%)"`). The name is the part after the last `.` (`cors.CORS` → `CORS`) |
| `stages_dropped` | only when a request had more than 64 stage events: how many were not kept |
| `total` | the wall-clock time of the request |

A JSON line:

```json
{"time":"2026-11-16T10:00:00+07:00","level":"INFO","msg":"/api/v1/users","trace_id":"019a8b3c4d5e7f3a9c1e2b4d6f8a0c2e","request":"a0eef3e5b7154c2e","transport":"HTTP","code":200,"operation":"GET","middleware":"CORS 120µs (4%)","handler":"READ_users 2ms (68%)","total":"2.9ms"}
```

In the pretty format of the built-in logger each stage is a branch of the tree with one row per event (the name as the key, `<duration> (<percent>%)` as the value), and each duration, `total` included, is printed in a color picked by its value: gray below 10 ms, then green (10 ms), blue (50 ms), yellow (100 ms), orange (300 ms) and red from 1 s. JSON, Text and a logger you bring get the plain string, and the logger you bring receives exactly the pairs above. Your own lines are never colored this way, even when a value looks like a duration.

- Durations have two decimals of their unit (`1.23µs`, `1.23ms`, `1.23s`) and are written as they are below 1 µs (`500ns`).
- A percentage is the share of the sum of the stage durations, not of `total`. The largest remainders receive the extra points (ties go to the earlier entry), so the shares sum to exactly 100; they are all 0 when the sum is 0.
- The ID keys are the same as the scoped logger's (`trace_id`, `request`, `connection`, `message`), so access lines join user logs on them. An empty ID is left out. Stage events are joined to their line by the event's `ID`; span IDs play no part.
- A request without stage events (a 404) logs the fixed pairs and `total`.
- The client IP is personal data in many jurisdictions (GDPR). Keep the access log under the same retention rules as your other personal data, or mask it in your log pipeline. Behind a load balancer, bind `proxy` with your proxies' addresses, or every line shows the load balancer's address.
- Events of different requests never mix, whatever the interleaving.

## `AccessLogConfig` Parameters

### Tracer
Type: `*trace.Tracer`

Default: none

Required: `true`

The tracer whose events the access log listens to. `core` passes the app's tracer.

### Logger
Type: `log.LogAdapter`

Default: none

Required: `true`

The logger that receives the lines. `core` passes the app's logger. A logger from `log.NewLog` writes the line through the log's output pipeline as an Info record; any other logger is called directly with the message and the pairs.

## Functions

### NewAccessLog

Listens to the tracer; it starts no goroutine. Users never call it: `app.EnableAccessLog()` does.

#### Parameters
- 1st parameter: `*AccessLogConfig`

- Description: The tracer and the logger

#### Returns
- 1st value: `*AccessLog`

#### Usage

```go
al := accesslog.NewAccessLog(&accesslog.AccessLogConfig{Tracer: tracer, Logger: logger})
defer al.Stop()
```

#### Rules
- A nil config, tracer or logger panics with `invalid access log: a tracer and a logger are required`.
- The logger is called on the emitting goroutine, before `Emit` returns, once per `complete` event, in order.
- A `log.LogAdapter` that is not a `log.Logger` is accepted.
- No goroutine is started, while running or after `Stop`.

### Stop

Removes the listener and discards pending requests; it returns at once, whatever the logger of a line in progress does. `core` calls it once at shutdown, after the server has drained and the `OnShutdown` hooks have run, and then flushes the log's output pipeline.

#### Usage

```go
al.Stop()
```

#### Rules
- Events emitted after `Stop` are ignored, and pending requests (stage events without `complete`) are discarded; nothing is logged after `Stop` returned.
- A second `Stop` does nothing.

## Limits and overload

- **64 stage events per request.** Later ones are counted and the line carries `stages_dropped`.
- **16,384 pending requests** (64 shards of 256). A new request that finds its shard full evicts that shard's oldest pending request; an evicted request that completes later is logged without stages. A new request is always traced.
- **The log's output pipeline bounds the rest.** With the built-in logger the line is an Info record: under overload it is dropped and counted in `WARN LogDropped` like any Info line (see `log/README.md`, Output pipeline). A logger you bring gets no bound from `Ginject`.
- **A broken logger cannot break a request.** A panic of the logger is recovered in the listener; that line is lost and the next ones are logged.

A team that needs a complete audit trail writes it in a middleware or an interceptor: the access log trades completeness for never waiting on stdout.

## Cost per request

Measured with `BenchmarkAccessLog_Line_6Stages` (6 stage events and `complete`, the built-in logger writing to a discarding output), darwin/amd64, Intel Core i7-9750H, a busy machine:

| Logger | Time | Allocations |
|---|---|---|
| Built-in, JSON | about 4 µs | 4 (the request's record, the line's text, its attributes, slog's record) |
| Built-in, Pretty | about 8 µs | 39 |
| A logger you bring | about 1.5 µs plus its own cost | 16 (the arguments are boxed into `[]any`) |

All of it runs on the request goroutine. The worker goroutine of earlier versions took about 16 µs and 102 allocations per line on another goroutine and capped the access log at about 65,000 lines per second.

## Internals

- Pending requests live in 64 shards picked from two bytes of the ID, each a mutex, a map and an eviction list; a stage is kept as `stage, name, duration`, the first 8 inline in the request's record.
- The `complete` event takes the record out of its shard; the line is formatted and logged after the shard's lock is released.
- The texts of every stage and of `total` are written into one buffer (durations appended without `time.Duration.String`'s allocation) and turned into one string; each value is a slice of it.
- With the built-in logger the line is passed as `slog` attributes (no boxing) through an internal method that also marks it as an access line, which the Pretty format colors and draws as a tree (it splits each stage string into its entries; nothing changes for other formats). Any other logger gets `Info(msg, args...)`.
- An atomic flag, read first by the listener, ignores events after `Stop`.

## Benchmarks

Machine-dependent, captured when this README was written. The `Request` benchmarks emit the events of one request to a logger that does nothing; the `Line` benchmarks use the built-in logger.

```sh
go test -run '^$' -bench . -benchmem ./accesslog/
```

```console
goos: darwin
goarch: amd64
cpu: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
BenchmarkAccessLog_Request_1Stage-12              	 1506247	       777.7 ns/op	     776 B/op	      10 allocs/op
BenchmarkAccessLog_Request_6Stages-12             	  653250	      1992 ns/op	    1072 B/op	      16 allocs/op
BenchmarkAccessLog_Request_6Stages_Parallel-12    	 1000000	      1452 ns/op	    2240 B/op	      18 allocs/op
BenchmarkAccessLog_Line_6Stages-12                	  319255	      4516 ns/op	    1426 B/op	       4 allocs/op
BenchmarkAccessLog_Line_6Stages_Pretty-12         	  143613	      8317 ns/op	    3770 B/op	      39 allocs/op
BenchmarkFormatDuration-12                        	26621779	        46.36 ns/op	       8 B/op	       1 allocs/op
BenchmarkColorForDuration-12                      	401468427	         2.875 ns/op	       0 B/op	       0 allocs/op
BenchmarkShortName-12                             	162431871	         7.422 ns/op	       0 B/op	       0 allocs/op
```
