# Trace

*Package `trace` holds the trace events the framework emits for each request and WebSocket message, and the `Tracer` that delivers them to listeners such as the access log.*

- [Trace](#trace)
  - [Usage](#usage)
  - [`Event` Fields](#event-fields)
  - [Spans](#spans)
  - [Stages and transports](#stages-and-transports)
  - [`Tracer` Methods](#tracer-methods)
    - [Enabled](#enabled)
    - [Emit](#emit)
    - [Listen](#listen)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

Apps cannot subscribe to the trace yet: `core` holds one `Tracer` per app and the access log (`app.EnableAccessLog()`) is its listener. The devtools platform will add a public way to listen.

## Usage

```go
package main

import (
	"fmt"

	"github.com/dangduoc08/ginject/trace"
)

func main() {
	var tracer trace.Tracer
	stop := tracer.Listen(func(e trace.Event) {
		fmt.Println(e.Stage, e.Name, e.Duration)
	})
	defer stop()

	if tracer.Enabled() {
		tracer.Emit(trace.Event{ID: "1", Stage: trace.StageHandler, Name: "READ_users", Transport: trace.TransportHTTP})
	}
}
```

## `Event` Fields

| Field | Type | Set on |
|---|---|---|
| `ID` | `string` | every event: the request or message ID |
| `TraceID` | `string` | every event of a unit that has a trace scope: the 32-hex trace ID |
| `SpanID` | `string` | the span of the event: the unit's ID on `complete`, a fresh 16-hex ID on every other stage |
| `ParentID` | `string` | `complete`: the incoming `traceparent` parent ID or `""` (HTTP, handshake), the connection ID (WS message); every other stage: the unit's ID |
| `Stage` | `string` | every event, one of the stages below |
| `Name` | `string` | every stage event: the middleware, guard, interceptor, filter or pipe type, or the handler's method name |
| `Transport` | `string` | every event: `TransportHTTP` or `TransportWS` |
| `Duration` | `time.Duration` | every event; for `complete`, the wall-clock time of the request |
| `Operation` | `string` | `complete`: the HTTP method, or the WS message type |
| `Target` | `string` | `complete`: the URL path without the query string, or the WS topic |
| `Code` | `int` | `complete`: the status written; a `middleware` event that ended in a panic: the status of the error response |

## Spans

The events of one request (or WebSocket message) form a flat tree in the W3C Trace Context model:

- `complete` is the root span: `SpanID == ID`, and `ParentID` is the caller's span (HTTP) or the connection (WS message).
- Every other stage event is one child span with its own `SpanID` and `ParentID == ID`.
- The events of a unit are emitted on its goroutine in lifecycle order, the root last, and their span IDs all differ.
- Span IDs are generated only while a listener exists. Events emitted without a trace scope leave `TraceID`, `SpanID` and `ParentID` empty.
- `TraceID`, `SpanID` and `ParentID` reach listeners unchanged, and emitting an event with them allocates nothing.

## Stages and transports

- Stages: `StageMiddleware`, `StageGuard`, `StagePreInterceptor`, `StagePostInterceptor`, `StageExceptionFilter`, `StagePipe`, `StageHandler`, `StageComplete`.
- Transport values: `TransportHTTP` (`"HTTP"`), `TransportWS` (`"WS"`).
- `core` asks `Enabled()` once per request and emits nothing for a request that started without a listener.

## `Tracer` Methods

The zero value is ready to use and has no listener.

### Enabled

Reports whether at least one listener is registered.

#### Returns
- 1st value: `bool`

#### Usage

```go
if tracer.Enabled() {
	tracer.Emit(e)
}
```

#### Rules
- The zero `Tracer` is disabled; `Listen` enables it; the last `stop` disables it again.
- Allocates nothing.

### Emit

Calls every listener with the event.

#### Parameters
- 1st parameter: `Event`

- Description: The event, passed to each listener by value

#### Usage

```go
tracer.Emit(trace.Event{ID: id, Stage: trace.StageGuard, Name: "auth.Guard", Transport: trace.TransportHTTP, Duration: d})
```

#### Rules
- Listeners run synchronously, on the emitting goroutine, in registration order; they have run when `Emit` returns.
- Without a listener `Emit` does nothing. It allocates nothing either way.
- `Emit` does not recover: a listener that panics panics in the emitter, and later listeners are not reached.
- A listener may call `Listen`, a `stop` or `Emit`; a listener registered during an `Emit` is first called by the next `Emit`.

### Listen

Registers a listener and returns the function that removes it.

#### Parameters
- 1st parameter: `func(Event)`

- Description: The listener

#### Returns
- 1st value: `func()`

- Description: `stop`, which removes this registration

#### Usage

```go
stop := tracer.Listen(func(e trace.Event) { record(e) })
defer stop()
```

#### Rules
- `stop` removes exactly the registration that returned it: the same function registered twice is two registrations, and the order of the others is kept.
- Calling `stop` again does nothing.
- `Listen(nil)` registers nothing and returns a `stop` that does nothing.
- An `Emit` that started before `stop` returned may still call the listener once.
- `Listen`, `stop`, `Enabled` and `Emit` are safe for concurrent use.

## Performance notes

- The listeners are an immutable slice behind an `atomic.Pointer`: `Enabled` is one atomic load, `Emit` one load and direct typed calls, with no lock and no boxing of the event. An `Event` is 160 bytes, passed by value. `Listen` and `stop` copy the slice under a mutex (bootstrap and shutdown only).

## Benchmarks

Machine-dependent, captured when this README was written:

```sh
go test -run '^$' -bench . -benchmem ./trace/
```

```console
goos: darwin
goarch: amd64
cpu: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
BenchmarkTracer_Enabled-12          	1000000000	         1.066 ns/op	       0 B/op	       0 allocs/op
BenchmarkTracer_Emit-12             	145073950	         8.275 ns/op	       0 B/op	       0 allocs/op
BenchmarkTracer_Emit_Parallel-12    	901271748	         1.449 ns/op	       0 B/op	       0 allocs/op
```
