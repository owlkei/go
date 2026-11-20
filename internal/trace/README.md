# Trace

Internal package: applications use `owlkei.TraceTransport` and `log.Logger.From(ctx)` instead.

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
  - [Outbound calls](#outbound-calls)
    - [Transport](#transport)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

Apps cannot subscribe to the trace yet: `core` holds one `Tracer` per app and the access log (`AppOptions.IsAccessLog`) is its listener. The devtools platform will add a public way to listen.

## Usage

```go
package main

import (
	"fmt"

	"go.owlkei.com/owlkei/internal/trace"
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
| `Name` | `string` | every stage event: the middleware, guard, interceptor, filter or pipe type, the handler's method name, or, on `render`, the template path without `.html` (`bookings/{id}`, or the error page: `404`, `4xx`) or `error (built-in)` |
| `Transport` | `string` | every event: `TransportHTTP` or `TransportWS` |
| `Duration` | `time.Duration` | every event; for `complete`, the wall-clock time of the request |
| `Operation` | `string` | `complete`: the HTTP method, or the WS message type |
| `Target` | `string` | `complete`: the URL path without the query string, or the WS topic |
| `Code` | `int` | `complete`: the status written; a `pre-route` event: the status the stage answered with when it ended the request (`503`, `421`, `400`, `403`, `308`, `204`), `0` when it called `next()`; a `middleware` event that ended in a panic: the status of the error response; a `render` event: `500` when the render failed, `0` otherwise |
| `ClientIP` | `netip.Addr` | `complete` of an HTTP request or handshake: the client's address as `owlkei.ClientIP` resolves it (the peer of `RemoteAddr` without `AppOptions.Proxy`); the zero `Addr` on every other event and when the address is unknown |

## Spans

The events of one request (or WebSocket message) form a flat tree in the W3C Trace Context model:

- `complete` is the root span: `SpanID == ID`, and `ParentID` is the caller's span (HTTP) or the connection (WS message).
- Every other stage event is one child span with its own `SpanID` and `ParentID == ID`.
- The events of a unit are emitted on its goroutine in lifecycle order, the root last, and their span IDs all differ.
- Span IDs are generated only while a listener exists. Events emitted without a trace scope leave `TraceID`, `SpanID` and `ParentID` empty.
- `TraceID`, `SpanID` and `ParentID` reach listeners unchanged, and emitting an event with them allocates nothing.

## Stages and transports

- Stages: `StagePreRoute` (`"pre-route"`: a built-in stage of `AppOptions`, `Name` the stage: `loadshed`, `proxy`, `hostcheck`, `httpsredirect`, `headers`, `cors`, `csrf`, `compress`, `origin`), `StageMiddleware` (user middlewares only), `StageGuard`, `StagePreInterceptor`, `StagePostInterceptor`, `StageExceptionFilter`, `StagePipe`, `StageHandler`, `StageRender`, `StageComplete`.
- `StageRender` (`"render"`): one event per HTML render of a page or an error page, after `handler` and before `complete`; its `Duration` is the time to execute the template and place the nonce. A JSON error emits none.
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

## Outbound calls

A service that calls another service over HTTP continues the incoming request's trace by wrapping its
`http.RoundTripper` with `Transport`. Any service that reads W3C Trace Context then joins your trace.
An Owlkei Go service you call logs the same `trace_id`, and its `upstream` is your request ID. To
search the logs of a whole chain, see [Find the logs of a request](../../log/README.md#find-the-logs-of-a-request).

`Transport` is not related to the `TransportHTTP` and `TransportWS` values of `Event.Transport`:
those name the protocol of a traced unit, while `Transport` is an `http.RoundTripper` for outbound
calls.

### Transport

Returns an `http.RoundTripper` that adds the `traceparent` header of the trace scope in the request's
context, then calls `base`.

#### Parameters
- 1st parameter: `http.RoundTripper`

- Description: The transport that sends the request. `nil` uses `http.DefaultTransport`, read at each call.

#### Returns
- 1st value: `http.RoundTripper`

- Description: The wrapper. It is safe for concurrent use and has a `CloseIdleConnections` method.

#### Usage

```go
var client = &http.Client{Transport: trace.Transport(nil), Timeout: 10 * time.Second}

func (instance UserController) READ_profile(c *ctx.HTTPContext) any {
	req, err := http.NewRequestWithContext(c.Context(), http.MethodGet, "https://accounts.example.com/me", nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return res.StatusCode
}
```

Build the request with the context Owlkei Go gives you (`c.Context()` of an HTTP or WebSocket context,
or any context derived from it with `context.WithTimeout`, `WithCancel` or `WithValue`). The context
keeps working after the request ended, for example in a goroutine you started from the handler.

#### Rules
- With a trace scope in the context, the request sent is a copy of yours with one added header,
  `Traceparent: 00-<trace ID>-<span ID>-<flags>`. The trace ID and the flags are the request's (a
  client's valid `traceparent` continues its own trace and flags). The span ID is your request ID
  (the `X-Request-Id` of your response), or the message ID for a WebSocket message.
- Every call made with the same context carries the same value, including each hop of a redirect
  that `http.Client` follows.
- Your request is never changed: its header map keeps its entries, gets no `Traceparent` key, and a
  `nil` header map stays `nil`. The copy has the same method, URL, body, length and context, and a
  deep copy of your headers.
- A `Traceparent` key you set yourself wins, whatever it holds, and the request passes through as it
  is. To send no trace header for one call, set the key with no value:
  `req.Header["Traceparent"] = nil`. Only the canonical key `Traceparent` is looked up.
- Without a trace scope (for example `context.Background()`), the request passes through as it is
  and the wrapper allocates nothing.
- Nothing else is added: no `tracestate`, no `X-Request-Id`, no forwarded client address.
- The response and the error of `base` are returned unchanged.
- `CloseIdleConnections` calls the same method of `base` (of `http.DefaultTransport` for a `nil`
  base) when it has one, so `http.Client.CloseIdleConnections` keeps working.

#### Security
- `Transport` has no host allowlist: the trace ID and your request ID go to every host you call
  with the request's context, third parties and redirect targets included. Set the key yourself,
  or opt out with an empty key, for hosts that must not see them. Use a separate `http.Client`
  without `Transport` for third-party APIs if you never want to share the IDs with them.
- Incoming trace IDs are adopted: a client that sends a valid `traceparent` chooses the trace ID
  and the flags (the sampled bit) that your service logs and forwards to the services it calls.
  Treat the trace ID as a correlation hint, never as proof of where a request came from, and
  don't let it decide authorization, billing or sampling quotas. Your request ID is always your
  own.
- The value is built only from IDs that Owlkei Go generated or validated (lower-case hex), so it can't
  inject a header.

## Performance notes

- The listeners are an immutable slice behind an `atomic.Pointer`: `Enabled` is one atomic load, `Emit` one load and direct typed calls, with no lock and no boxing of the event. An `Event` is 184 bytes, passed by value. `Listen` and `stop` copy the slice under a mutex (bootstrap and shutdown only).
- `Transport` without a trace scope, or with your own `Traceparent` key, is one context lookup and allocates nothing. With a scope it copies the request and its header map once per call, as the `http.RoundTripper` contract requires (5 allocations with three headers): small next to a network round trip. It holds only `base` and keeps nothing between calls.

## Benchmarks

Machine-dependent, captured when this README was written:

```sh
go test -run '^$' -bench . -benchmem ./trace/
```

```console
goos: darwin
goarch: amd64
cpu: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
BenchmarkTransport_NoScope-12           	179169152	         6.667 ns/op	       0 B/op	       0 allocs/op
BenchmarkTransport_ExistingHeader-12    	68880050	        17.88 ns/op	       0 B/op	       0 allocs/op
BenchmarkTransport_Scope-12             	 2589052	       462.2 ns/op	     784 B/op	       5 allocs/op
BenchmarkTracer_Enabled-12              	1000000000	         1.111 ns/op	       0 B/op	       0 allocs/op
BenchmarkTracer_Emit-12                 	137298514	         8.747 ns/op	       0 B/op	       0 allocs/op
BenchmarkTracer_Emit_Parallel-12        	857145403	         1.547 ns/op	       0 B/op	       0 allocs/op
```
