# Loadshed Middleware

*`loadshed` keeps your server fast when more requests arrive than it can serve: it lets a bounded number of requests run at once, holds a few more for a moment, and answers the rest `503 Service Unavailable` with `Retry-After` right away.*

- [Loadshed Middleware](#loadshed-middleware)
  - [Key Features](#key-features)
  - [Why you need it](#why-you-need-it)
  - [Usage](#usage)
  - [`LoadShed` Struct](#loadshed-struct)
    - [MaxInFlight](#maxinflight)
    - [MaxQueue](#maxqueue)
  - [`LoadShed` Methods](#loadshed-methods)
    - [NewMiddleware](#newmiddleware)
    - [Use](#use)
  - [What happens to a request](#what-happens-to-a-request)
  - [How the limit adapts](#how-the-limit-adapts)
  - [Defaults and where they come from](#defaults-and-where-they-come-from)
  - [Rules](#rules)
  - [Known limits](#known-limits)
  - [Loadshed or throttler](#loadshed-or-throttler)
  - [Memory and CPU](#memory-and-cpu)
  - [Benchmarks](#benchmarks)

## Key Features
- Nothing to configure: the limit follows the latency your handlers actually have, between bounds
  read from the machine (`GOMAXPROCS`, `GOMEMLIMIT`, the container's memory limit)
- Fails fast: at most 64 requests wait, for at most 100 ms; everything else gets `503` with
  `Retry-After: 1` at once, so clients retry instead of piling up
- A fixed limit when you want one: `LoadShed{MaxInFlight: 200}`
- Every slot is released exactly once, whatever the request did: returned, panicked, answered
  `404`, was refused by a later layer, or aborted
- Hard to steer: the time a client spends sending its body is not counted, one slow request
  weighs at most 32 baselines, and cheap requests cannot drag the reference latency down
- No goroutine and no timer while the server is idle; an admitted request costs a few atomic
  operations and no allocation (one small allocation for a request with a body in adaptive mode,
  or for a request admitted during a probe)

## Why you need it

A server that accepts every request slows down for everyone once it is past its capacity: each
new request takes a share of the CPU, memory and connections the others need, latency climbs
for all of them, and clients time out on work the server finishes anyway. Refusing the extra
requests early, with a status code clients and load balancers understand, keeps the admitted ones
fast (Google SRE Book, chapters 21 and 22, "Handling overload" and "Addressing cascading
failures").

An adaptive limit is only useful if clients cannot move it where they want. The adaptive mode
measures the server's own time, not the client's: time spent waiting for a slow upload or for a
client that reads the response slowly does not count as slowness. A flood of requests that do no
work does not lower the reference latency by more than the tolerance, and a few very slow
requests cannot take the limit down by more than half of what it has grown to until a probe
confirms that the server really is slower. See [How the limit adapts](#how-the-limit-adapts) and
[Known limits](#known-limits) for what remains.

## Usage

Bind `loadshed` first, so a refused request costs as little as possible:

```go
package main

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/middlewares/loadshed"
)

func main() {
	app := owlky.New()

	app.BindGlobalMiddlewares(loadshed.LoadShed{})

	app.Create(
		owlky.ModuleBuilder().Build(),
	)

	app.Listen(8080, nil)
}
```

Over capacity, the extra requests get:

```http
HTTP/1.1 503 Service Unavailable
Content-Type: application/json
Retry-After: 1

{"code":503,"error":"Service Unavailable","message":"Server is busy"}
```

Owlky Go calls `NewMiddleware` once at `Create()`. Two bindings of a `LoadShed` with the same
values share one limiter.

To limit WebSocket handshakes too, pass it to `EnableWS`:

```go
app.EnableWS(&owlky.WSOptions{Path: "/ws"}, loadshed.LoadShed{})
```

The slot of a handshake is released when the handshake ends, not when the connection closes.

## `LoadShed` Struct

### MaxInFlight
Type: `int`

Default: `0` (adaptive)

Required: `false`

How many requests may run at once. `0` lets the limit adapt by itself (see
[How the limit adapts](#how-the-limit-adapts)); a value from 1 to 1,000,000 is a fixed limit that
never changes. A negative value or a value above 1,000,000 panics at `Create()`.

```go
loadshed.LoadShed{MaxInFlight: 200}
```

Use a fixed limit when you know what your server can take (a load test told you), or for a
module whose routes are much slower or faster than the rest of the app.

### MaxQueue
Type: `int`

Default: `64`

Required: `false`

How many requests may wait for a free slot. `0` means 64; a negative value turns the queue off,
so a request over the limit is refused at once. A value above 65,536 panics at `Create()`. A
waiting request waits at most 100 ms.

```go
loadshed.LoadShed{MaxQueue: -1}
```

The wait and the `Retry-After` value are fixed.

## `LoadShed` Methods

### NewMiddleware
```go
func (instance LoadShed) NewMiddleware() owlky.MiddlewareFn
```

Validates the options and reads the machine's bounds once; the framework calls it at `Create()`.
It panics with `invalid loadshed: ...` for an option out of range.

### Use
```go
func (instance LoadShed) Use(r *http.Request, w http.ResponseWriter, next owlky.Next)
```

Makes `LoadShed` a `owlky.MiddlewareFn`. It builds a new limiter on every call, so use it in tests
only; the framework always calls `NewMiddleware`. Called outside an app, it only calls the next
layer.

## What happens to a request

1. A free slot: the request runs. Its slot is released when its exchange ends.
2. No free slot and fewer than `MaxQueue` requests waiting: it waits. The first slot that frees
   goes to the request that has waited longest; a request that arrives at the instant a slot
   frees may take it before a waiter does. This is rare and keeps bursts fast.
3. The queue is full, or 100 ms passed without a slot: `503`, `Retry-After: 1`, message
   `Server is busy`. No guard, interceptor, handler or exception filter runs.
4. The client went away while waiting (its request context ended): nothing is written.

## How the limit adapts

With `MaxInFlight: 0`, `loadshed` measures how long each request takes and compares it with how
long requests take when the server is not busy. When requests are being held back and they stay
close to that time, the limit grows; when they get slower, it shrinks. While nothing is held back,
the limit stays where it is. This is the gradient rule of Netflix's `concurrency-limits`
(Gradient2) and Envoy's adaptive concurrency filter.

- **Bounds.** The lowest limit is `GOMAXPROCS`. The highest is the memory limit divided by 4 MiB,
  between 64 and 4,096; the memory limit is the smallest of `GOMEMLIMIT` and, on Linux, the
  container's cgroup limit (`memory.max`, or `memory.limit_in_bytes` for cgroup v1). With no memory
  limit, the highest is 256 × `GOMAXPROCS`, between 64 and 4,096. The limit starts at
  8 × `GOMAXPROCS` within those bounds. For example, 1 vCPU and 512 MiB gives 1 / 8 / 128
  (lowest / start / highest), and 2 vCPU and 2 GiB gives 2 / 16 / 512.
- **Samples.** Every finished request is a sample, measured from the moment it got its slot, so
  the time it waited in the queue is left out. The time the handler spent inside `Read` on the
  request body is left out too: a client that uploads slowly does not look like a slow server.
  To measure it, `loadshed` replaces `r.Body` of an admitted request that has a body with a
  wrapper that forwards `Read` and `Close` unchanged. Requests that take longer than 30 s
  (streams, downloads) are ignored. Once a baseline is known, a sample counts for at most
  32 × the baseline, so one slow request cannot own a window. Samples are grouped in windows of
  4 × the current limit; the limit can change when a window closes.
- **Baseline.** The time requests take without contention. `loadshed` learns it from the average
  of samples that finished with at most `GOMAXPROCS` requests running (at least 8 in a window) and
  from probes, blending each new value with the previous one. While long-held requests (streams,
  downloads, long polls) are running, few samples finish with that few requests in flight, so the
  baseline comes from probes. A window that is faster on average
  lowers the baseline, but never below two thirds of the learned value, and it does not make the
  baseline younger. Two thirds is one over the tolerance: requests that do no work can take the
  tolerance away and nothing more. Until a baseline is known, the limit stays where it started.
- **Update.** The limit moves only at the end of a window in which a request was queued or
  refused. Without that, the limit held nothing back, so there is nothing to learn. With
  `gradient = min(1.5 × baseline / average, 1)` and `useful = limit × gradient`, the new limit is
  `0.8 × limit + 0.2 × (useful + √useful)`, within the bounds. One window takes at most 20 % off
  the limit, so one slow burst does not empty the server. For example, with a limit of 32 and a
  baseline of 10 ms, a window that averages 10 ms gives 33, and one that averages 60 ms gives 27.
- **No growth from much faster traffic.** A window that averages less than half of the baseline
  does not raise the limit: it is not the traffic the baseline was learned from. Growth resumes
  once quiet samples or probes have learned the new traffic.
- **Floor.** The limit does not fall below half of the highest value it has grown to, until a
  probe has finished with at least one request. A limit held by that floor asks for a probe. When
  the probe shows that the slowness does not come from concurrency (a slow route, slow clients),
  the baseline is relearned and the limit stops falling; when it does, the floor moves to half of
  the current limit and the limit goes on down. The first limit is not earned, so the first
  descent from the start value is not held.
- **Probe.** When requests were queued or refused and the baseline is unknown, older than 30 s,
  or the limit was held by the floor, `loadshed` admits only probe requests: at most `GOMAXPROCS`
  of them at once, and the total in flight stays within the limit. Requests that were already
  running when the probe started (a stream, for example) keep running and take none of the
  probe's slots. A probe request counts toward the probe only when no request from outside the
  probe finished while it ran, so the probe measures the server without contention. The probe
  ends after 8 such requests (or 1 s), and learns their average as the baseline. Probes start at
  least 30 s apart, and the window in which a probe runs does not move the limit. No probe starts
  while nothing is queued or refused. A probe that ends without such a request changes nothing,
  so the floor stays where it is.

All of this runs inside the request that finishes; there is no background goroutine and no timer.

## Defaults and where they come from

| Default | Value | Source |
|---|---|---|
| Refusal | `503` with `Retry-After: 1` | RFC 9110 § 15.6.4 and § 10.2.3 |
| Queue | 64 requests, 100 ms each | Google SRE Book ch. 22: short queues, fail fast |
| Limit | adaptive, gradient against a probed no-load baseline, tolerance 1.5, moved only while requests are held back | Netflix `concurrency-limits` (Gradient2), Envoy adaptive concurrency (minimum-latency probe) |
| Bounds | `GOMAXPROCS` to memory / 4 MiB (64-4,096) | one slot per core at least; about 4 MiB of memory per request in flight at most |
| First limit | 8 × `GOMAXPROCS` | low enough that the limit settles within seconds on a small machine; an I/O-bound app grows it while its first bursts are held back |
| Window | 4 × the current limit samples | enough samples to average out a few slow requests at any limit |
| Sample cap | 32 × the baseline | keeps Go's overload signal on few cores (a few requests 10-70 × slower) while one request cannot own a window |
| Lowest baseline | two thirds of the learned baseline (1 / tolerance) | requests that do no work can remove the tolerance and nothing more |
| Floor | half of the highest limit reached, until a probe finishes | a client that causes slowness cannot spiral the limit down to `GOMAXPROCS` |

## Rules
- At any instant, at most `limit` requests are between admission and the end of their exchange,
  also under heavy concurrency, while the adaptive limit changes and during a probe. During a
  probe, in addition, at most `GOMAXPROCS` of the requests that probe admitted are in flight at
  once; requests admitted before the probe, or by an earlier probe, count against the limit but
  not against those `GOMAXPROCS`.
- With a fixed limit, the limit is `MaxInFlight` forever.
- A request waits when, on arrival, every slot is taken and fewer than `MaxQueue` requests wait.
  Waiting requests are admitted in the order they started waiting: every freed slot goes to the
  longest waiter first, and a request never joins the queue ahead of one already in it. A request
  that arrives at the instant a slot frees, before the slot reaches a waiter, may take it at once.
  A request finds the queue full only after every free slot has gone to the requests waiting
  before it.
- A request that finds the queue full or waits 100 ms is refused with `503`, `Retry-After: 1` and
  the body `{"code":503,"error":"Service Unavailable","message":"Server is busy"}`
  (`Content-Type: application/json`). A refused request takes no slot.
- A request whose context ends while it waits gets no response and takes no slot.
- A slot is released after a normal response, a panic, a refusal by a later middleware, a
  middleware that answered by itself, a `404` and `http.ErrAbortHandler`. For a WebSocket
  handshake it is released when the handshake ends.
- Under a burst of 50 × the limit, every response is `200` or `503`, and every slot is free
  afterwards.
- The next layer receives the same `*http.Request` and the same context; the body is never read.
  In adaptive mode, `r.Body` of an admitted request that has a body (neither `nil` nor
  `http.NoBody`) is a wrapper whose `Read` and `Close` return exactly what the original body
  returns, including `*http.MaxBytesError`. A refused request, and every request in fixed mode,
  keeps its `r.Body`.
- Idle, there is no goroutine in either mode, and none is left after requests waited.
- Adaptive: the limit starts at 8 × `GOMAXPROCS`, never goes below `GOMAXPROCS` nor above the
  highest bound, and does not move without a baseline. It moves only at the close of a window of
  4 × limit samples in which a request was queued or refused: it shrinks by at most 20 % per slow
  window, grows on fast ones, and stays whatever the latency when nothing was held back. Samples
  over 30 s leave no trace, and neither the time spent in the queue nor the time spent inside
  `Read` on the request body is part of a sample.
- Adaptive: with a baseline known, a sample counts for at most 32 × the baseline. A faster window
  lowers the baseline to at most two thirds of the learned value and leaves its age. A window
  under half of the baseline does not grow the limit. The limit never falls below half of the
  highest value it grew to until a probe finishes with at least one request; the first limit does
  not count as grown.
- Adaptive: a probe starts only when requests were queued or refused, the baseline is unknown,
  older than 30 s or the limit was held by the floor, and no probe started in the last 30 s. While
  it runs, only the requests it admits enter. A request it admitted counts toward it only when no
  request it did not admit finished between that request's admission and the end of its exchange.
  It ends after 8 such requests or at the first admission 1 s after it started, and the window it
  ran in leaves the limit as it was. A probe that ends without such a request leaves the baseline
  and the floor as they were. A request a probe admitted that finishes after the probe ended is an
  ordinary sample.
- Adaptive, measured on 1 and 2 CPUs under 5 × the capacity: the limit stays within 35 % of its
  median for a CPU-bound handler and 20 % for an I/O-bound one, throughput is at least 95 % of the
  capacity, and the 99th percentile latency is at most 12 × (CPU-bound) and 3 × (I/O-bound) the
  latency without load. At 70 % of the capacity no request is refused.
- Adaptive, measured on 2 CPUs with an I/O-bound handler and legitimate traffic at 1.2 × the
  capacity: 64 clients sending requests that do no work in bursts, or 40 clients each spending 1 s
  in their request body, leave at least 90 % of the capacity served and the limit at least half
  of its value before the attack; 10 s after the attack the limit is back to at least 60 %. 40
  clients on a route that waits 1 s leave the limit at least 45 % of its value before.
- Adaptive, measured with long-held requests that run for the whole test (one or two on 1 CPU,
  three on 2 CPUs, CPU-bound, and one on 1 CPU, I/O-bound) under 5 × the capacity for 45 s: a
  probe never lasts 500 ms, no 500 ms pass without a served request after the first 2 s, the
  baseline is set and at most 3 × the latency without load, throughput is at least 85 % of the
  capacity, and the 99th percentile latency is at most 12 × the latency without load (CPU-bound)
  or the limit grows to at least 4 × its start (I/O-bound). Every slot is free afterwards.
- An admitted request allocates nothing, except the body wrapper (64 B) for a request with a body
  in adaptive mode, and a 16 B record for a request without a body admitted during a probe.

## Known limits
- One limit covers every route it is bound to. A mix of very fast and very slow routes blurs the
  latency signal: bind a fixed `LoadShed` to the module with the slow routes instead.
- The limit follows latency, so a dependency that slows down (a database, an API) lowers it. That
  is the intent: the server stops sending more work to something that is already struggling.
- Health checks are not exempt. A load balancer that sees `503` on its probe takes the instance
  out of rotation, which is what you want while it is overloaded.
- A handler that computes for milliseconds without ever reaching the Go scheduler (no I/O, no
  lock, no channel, no allocation that triggers the garbage collector) can hide overload from any
  in-flight limit, fixed or adaptive. Go runs such requests one after another, so the number in
  flight stays below the limit and the excess waits in Go's run queue instead of being refused.
  `loadshed` starts refusing once enough of them have been preempted to fill the limit, which on
  1 CPU leaves the limit moving by about 35 % around a small value. For such a server, set a fixed
  limit of about twice the CPUs: `LoadShed{MaxInFlight: 2 * runtime.GOMAXPROCS(0)}`. Handlers
  that wait on a database, a socket or a lock reach the scheduler and are not affected.
- `loadshed` protects one process. It does not know about other instances behind your load
  balancer.
- `max` bounds the number of requests in flight, not their memory. A burst of large requests at a
  limit already reached is not stopped by latency; bound the body size (the app's body limit) and
  the memory (`GOMEMLIMIT`) for that.
- The time a client takes to read the response after the handler returned is not sampled, so it
  cannot move the limit. Such a client still holds its slot until the server's write timeout,
  like any slow client: set `WriteTimeout` on your server.
- Requests that really take long in the handler and that a client can send at will (a slow
  search, a report) can cost half of the earned limit on top of the slots they hold, once they
  hold about a third of them. The floor moves down only when a probe learns, and while such
  requests keep finishing a probe may end without learning. Protect such routes with
  `guards/throttler`, or bind a fixed `LoadShed` to their module.
- After such an attack the baseline has learned the attack's traffic, so the limit can grow above
  its usual value until quiet samples or the next probes relearn it.
- A client that fills a whole probe with requests that do no work (most of what arrives during
  those few tens of milliseconds) halves the learned baseline once per 30 s. The floor bounds what
  follows to one halving of the limit, and the next learning without that client repairs it.
- A client that keeps every slot full for minutes with cheap requests that wait (not compute)
  teaches the baseline its own traffic, one halving per probe, and can then grow the limit up to
  `max`. A throttler on such routes prevents it.
- A long-held request (an SSE stream, a download, a long poll) holds its slot for its whole life,
  in both modes. Enough of them fill the limit, and then every other request waits and is
  refused. Bind `loadshed` to the modules without streams, or bind a fixed `LoadShed` sized for
  the streams to the streaming module.
- While long-held requests are in flight, samples are rarely quiet, so the baseline comes from
  probes: the first window under pressure probes, and the baseline is relearned at most every
  30 s while the pressure lasts. One probe of 8 requests then sets the baseline for up to 30 s,
  so a pause of the whole process during that probe (a long garbage collection, a busy host)
  makes the limit looser until the next probe.
- A probe that runs while requests from before it keep finishing (a slow route under attack) may
  collect no usable request in its second and end empty. That changes nothing: the floor and the
  baseline stay, as for any empty probe.

## Loadshed or throttler

`loadshed` limits **the whole server**: it refuses work when the server is overloaded, whoever
sends it, and answers `503`. `guards/throttler` limits **each client**: it refuses a client that
asks too much in a period of time, even when the server is idle, and answers `429`. Bind
`loadshed` to keep the server healthy, and the throttler on routes one client must not abuse
(logins, search, uploads).

## Memory and CPU
- Idle: about 230 B per instance (counters, the window, two mutexes, an empty queue). No goroutine,
  no timer.
- Each waiting request: one timer and one channel (about 200 B), at most `MaxQueue` of them.
- Admitted request: a few atomic operations and no allocation. The request that closes a window
  (every 4 × limit requests) also recomputes the adaptive limit.
- Probe: one 48 B record when it starts (at most one per 30 s), and one 16 B allocation per
  request without a body that it admits (a few to a few hundred per probe).
- Admitted request with a body, adaptive mode: one 64 B allocation (the body wrapper) and two
  clock reads per `Read` call, about 110 ns per call. A body is read in chunks of 4 KiB or more,
  so this is about 30 µs per MiB.
- Refused request: the cost of the HTTP layer's error response.

## Benchmarks

Machine-dependent; captured on a shared, busy Intel i7-9750H, Go 1.27.4, when this README was written
(`go test -run='^$' -bench 'Admitted|Shed$' -benchmem -count 5 -cpu 1,2 ./middlewares/loadshed`, median shown).

| Benchmark | GOMAXPROCS 1 | GOMAXPROCS 2 | B/op | allocs/op |
|---|---|---|---|---|
| `LoadShed_Admitted_Fixed` | 31.9 ns | 31.6 ns | 0 | 0 |
| `LoadShed_Admitted_Adaptive` | 51.7 ns | 52.1 ns | 0 | 0 |
| `LoadShed_Admitted_AdaptiveBody` | 374 ns | 379 ns | 64 | 1 |
| `LoadShed_Admitted_Parallel` | 54.5 ns | 128 ns | 0 | 0 |
| `LoadShed_Shed` | 454 ns | 455 ns | 16 | 1 |
| `LoadShed_App_Admitted` | 1.33 µs | 1.28 µs | 680 | 9 |

`Admitted_*` admit and release one request on the limiter alone, with a fixed or the adaptive limit;
`AdaptiveBody` does it for a request whose body is read in two calls (the body wrapper is its one
allocation); `Parallel` does it from every core at once, so the cores share the counters. `Shed` refuses a
request (the `Retry-After` header is its one allocation). `App_Admitted` serves a whole request
through an app; the allocations are the app's own.
