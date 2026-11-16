# Access Log

*Package `accesslog` writes one log line per HTTP request or WebSocket message, with the time each middleware, guard, interceptor, pipe, handler and filter took. Its cost on the request goroutine is small and bounded, and a slow logger never slows a request.*

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
- Bounded memory whatever arrives: 64 stages per request, 16,384 pending requests, 1,024 queued lines.
- Under overload, lines are dropped and the count is reported; requests never wait for the logger.
- The query string is never logged.

## Usage

```go
package main

import (
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/log"
)

func main() {
	app := core.New()
	app.SetLogger(log.NewLog(&log.LogOptions{LogFormat: log.JSONFormat}))
	app.EnableAccessLog()
	app.Create(core.ModuleBuilder().Build())
	_ = app.Listen(8080, nil)
}
```

## The line

One `Logger.Info` call per request, made on the access log's worker goroutine. The message is the request's path (or the WebSocket topic), cut to 2,048 bytes on a rune boundary and followed by `...` when longer. The arguments, in order:

| Key | Value |
|---|---|
| `id` | the request ID (the `X-Request-Id` header) |
| `transport` | `HTTP` or `WS` |
| `code` | the status written (an `int`; `0` for WebSocket messages) |
| `operation` | the HTTP method, or the WebSocket message type |
| one key per stage | `middleware`, `guard`, `pre_interceptor`, `pipe`, `handler`, `post_interceptor`, `exception_filter`, in the order their first event arrived; the value is a list with one `{name: "<duration> (<percent>%)"}` entry per event, in arrival order. The name is the part after the last `.` (`cors.CORS` → `CORS`) |
| `stages_dropped` | only when a request had more than 64 stage events: how many were not kept |
| `total` | the wall-clock time of the request |

A JSON line:

```json
{"time":"2026-11-16T10:00:00+07:00","level":"INFO","msg":"/api/v1/users","id":"a0eef3e5b7154c2e","transport":"HTTP","code":200,"operation":"GET","middleware":[{"CORS":"120µs (4%)"}],"handler":[{"READ_users":"2ms (68%)"}],"total":"2.90ms"}
```

In the pretty format each entry is printed in a color picked by its duration: gray below 10 ms, then green, blue (50 ms), yellow (100 ms), orange (300 ms) and red from 1 s. JSON and Text carry only the text.

- Durations have two decimals of their unit (`1.23µs`, `1.23ms`, `1.23s`) and are written as they are below 1 µs (`500ns`).
- A percentage is the share of the sum of the stage durations, not of `total`. The largest remainders receive the extra points (ties go to the earlier entry), so the shares sum to exactly 100; they are all 0 when the sum is 0.
- A request without stage events (a 404) logs the four fixed pairs and `total`.
- Events of different requests never mix, whatever the interleaving.

## `AccessLogConfig` Parameters

### Tracer
Type: `*trace.Tracer`

Default: none

Required: `true`

The tracer whose events the access log listens to. `core` passes the app's tracer.

### Logger
Type: `common.Logger`

Default: none

Required: `true`

The logger that receives the lines and the dropped-lines warning. `core` passes the app's logger.

## Functions

### NewAccessLog

Starts the worker goroutine and listens to the tracer. Users never call it: `app.EnableAccessLog()` does.

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
- The logger is called off the emitting goroutine.

### Stop

Removes the listener, lets the worker pass every queued line to the logger, and returns when it has finished or after 2 seconds, whichever comes first. `core` calls it once at shutdown, after the server has drained and the `OnShutdown` hooks have run, so the lines of the last requests (those made by the hooks too) are written before the process exits.

#### Usage

```go
al.Stop()
```

#### Rules
- Every queued line is passed to the logger, in order.
- The worker goroutine ends; events emitted after `Stop` are ignored, and pending requests (stage events without `complete`) are discarded.
- A second `Stop` does nothing.

## Limits and overload

- **64 stage events per request.** Later ones are counted and the line carries `stages_dropped`.
- **16,384 pending requests** (64 shards of 256). A new request that finds its shard full evicts that shard's oldest pending request; an evicted request that completes later is logged without stages. A new request is always traced.
- **1,024 queued lines.** A request that finds the queue full is dropped and counted. The worker reports the count with `Logger.Warn("AccessLog", "dropped", n)` at most once per second, and `Stop` reports what is left. Lines logged plus lines reported as dropped equal the requests seen.
- **One worker: about 65,000 lines per second.** Formatting and logging a line takes about 15 µs on the worker (`BenchmarkAccessLog_WorkerLine`). Beyond that rate the queue fills, and the extra lines are dropped and counted as above.
- **A broken logger cannot stop the worker.** A panic of the logger is recovered; that line counts as dropped.

A team that needs a complete audit trail writes it in a middleware or an interceptor: the access log trades completeness for never slowing a request.

## Cost per request

Measured with `bench/` (a silent logger), access log off and on, one app per run, medians of 5 runs on darwin/amd64, Intel Core i7-9750H. The "on" figures include the worker's line formatting, which runs on another goroutine at the same time.

| Scenario | Off | On | Extra |
|---|---|---|---|
| Static | 765 ns, 8 allocs | 1,907 ns, 22 allocs | +1.1 µs |
| Chain | 2,857 ns, 24 allocs | 5,605 ns, 63 allocs | +2.7 µs |
| Static, 12 goroutines | 337 ns | 790 ns | 2.3× |
| Chain, 12 goroutines | 1,509 ns | 2,339 ns | 1.55× |

On the request goroutine alone, a request of 6 stages and `complete` allocates at most 2 times (one record for its stages).

## Internals

- Pending requests live in 64 shards picked from two bytes of the ID, each a mutex, a map and an eviction list; a stage is kept as `stage, name, duration`, the first 8 inline in the request's record.
- The `complete` event takes the record out of its shard and sends it to a buffered channel with a non-blocking `select`. No lock is held during the send, and the logger is never called under a lock.
- One worker goroutine formats the lines (no map of stages, one buffer for the texts of a line) and calls the logger.
- An atomic flag, read first by the listener, ignores events after `Stop`; an atomic counter holds the dropped lines.

## Benchmarks

Machine-dependent, captured when this README was written. The `Request` benchmarks emit the events of one request; the worker runs at the same time, so its allocations are counted too.

```sh
go test -run '^$' -bench . -benchmem ./accesslog/
```

```console
goos: darwin
goarch: amd64
cpu: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
BenchmarkAccessLog_Request_1Stage-12              	 2912655	       388.2 ns/op	     597 B/op	       4 allocs/op
BenchmarkAccessLog_Request_6Stages-12             	 1956883	       645.6 ns/op	    1101 B/op	       8 allocs/op
BenchmarkAccessLog_Request_6Stages_Parallel-12    	 1639288	       842.4 ns/op	    1474 B/op	       4 allocs/op
BenchmarkAccessLog_WorkerLine-12                  	   87806	     15001 ns/op	    3992 B/op	      39 allocs/op
BenchmarkFormatDuration-12                        	27459612	        39.51 ns/op	       8 B/op	       1 allocs/op
BenchmarkColorForDuration-12                      	385296932	         2.935 ns/op	       0 B/op	       0 allocs/op
BenchmarkShortName-12                             	159170472	         7.722 ns/op	       0 B/op	       0 allocs/op
```
