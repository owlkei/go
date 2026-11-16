# Trace context

*Package `tracecontext` gives every HTTP request, WebSocket connection and WebSocket message its IDs in the W3C Trace Context format, and carries them in the `context.Context` Ginject hands out.*

- [Trace context](#trace-context)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [Constants](#constants)
  - [`Scope` Fields](#scope-fields)
    - [TraceID](#traceid)
    - [Request](#request)
    - [Connection](#connection)
    - [Message](#message)
    - [Upstream](#upstream)
    - [Parent](#parent)
    - [LogSlot](#logslot)
  - [Functions](#functions)
    - [NewSpanID](#newspanid)
    - [NewTraceID](#newtraceid)
    - [ParseTraceparent](#parsetraceparent)
    - [NewRequest](#newrequest)
    - [NewMessage](#newmessage)
    - [With](#with)
    - [ScopeOf](#scopeof)
  - [`Scope` Methods](#scope-methods)
    - [AppendArgs](#appendargs)
    - [Traceparent](#traceparent)
    - [SpanID](#spanid)
    - [Headers](#headers)
  - [Security](#security)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

The package is internal: only code in the Ginject module can import it. Users read the IDs through
`c.GetID()`, the `X-Request-Id` and `Traceparent` response headers, the access log and (from design
0012) the scoped logger.

## Key Features
- W3C Trace Context: 128-bit trace IDs, 64-bit span IDs, `traceparent` parsed and written
- Time-ordered trace IDs (ULID layout in hex)
- Every random bit from `crypto/rand`, through small per-processor buffers and no lock
- The request ID is always ours; a client only continues the trace ID
- A scope is immutable and outlives the request: kept contexts read their own IDs forever

## Usage

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/dangduoc08/ginject/internal/tracecontext"
)

func main() {
	h := http.Header{}
	h.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	requestCtx, request := tracecontext.NewRequest(context.Background(), h, time.Now())
	fmt.Println(request.TraceID, request.Request, request.Upstream)
	fmt.Println(request.Traceparent())

	messageCtx, message := tracecontext.NewMessage(requestCtx, time.Now())
	fmt.Println(message.Connection, message.Message)

	derived, cancel := context.WithTimeout(messageCtx, time.Second)
	defer cancel()
	fmt.Println(tracecontext.ScopeOf(derived) == message)
	fmt.Println(message.AppendArgs(nil)...)
}
```

## Constants

The log argument keys, in the order `AppendArgs` writes them. They are final; `span_id` and `cause`
are reserved.

| Constant | Value |
|---|---|
| `KeyTraceID` | `trace_id` |
| `KeyRequest` | `request` |
| `KeyConnection` | `connection` |
| `KeyMessage` | `message` |
| `KeyUpstream` | `upstream` |

## `Scope` Fields

A `*Scope` is written once by `NewRequest` or `NewMessage`, then only read: share it with any
goroutine, at any time after the request. Never write to it and never copy it.

### TraceID
- Type: `string`
- Default: set by the constructor
- Required: no

32 lower-case hex characters: the continued trace (a valid incoming `traceparent`) or a new one.

### Request
- Type: `string`
- Default: `""` on a message scope
- Required: no

16 hex characters, always fresh: the request ID (`X-Request-Id`, `c.GetID()`) and the request's span
ID. On a WebSocket handshake it is the connection ID.

### Connection
- Type: `string`
- Default: `""` on a request scope
- Required: no

The connection ID (the handshake scope's `Request`) on a message scope.

### Message
- Type: `string`
- Default: `""` on a request scope
- Required: no

16 hex characters, always fresh: the message ID and its span ID.

### Upstream
- Type: `string`
- Default: `""`
- Required: no

The caller's ID on a request scope: the incoming `traceparent` parent ID, else a valid incoming
`X-Request-Id` (one value of 1 to 64 bytes in `A-Z a-z 0-9 - _ .`).

### Parent
- Type: `string`
- Default: `""`
- Required: no

The parent span: the incoming `traceparent` parent ID (request scope) or the connection ID (message
scope).

### LogSlot
- Type: `atomic.Value`
- Default: empty
- Required: no

Reserved for the log package (design 0012): the scoped logger cached once per scope. This package
never reads or writes it.

## Functions

### NewSpanID

Returns a new 64-bit span ID.

#### Returns
1. `string`: 16 characters in `0-9a-f`.

#### Usage

```go
id := tracecontext.NewSpanID()
```

#### Rules
- The result is 16 lower-case hex characters and never `0000000000000000`, even while the random source returns zeros.
- 10,000,000 IDs hold no duplicate; 16 goroutines making 100,000 IDs each make no duplicate.
- It costs one allocation (the string) and is safe for concurrent use.

### NewTraceID

Returns a new 128-bit trace ID: 48 bits of milliseconds, then 80 random bits.

#### Parameters
1. `now` `time.Time`: the creation time.

#### Returns
1. `string`: 32 characters in `0-9a-f`.

#### Usage

```go
traceID := tracecontext.NewTraceID(time.Now())
```

#### Rules
- The first 12 characters are `now.UnixMilli()` as a 48-bit big-endian number; times in the same millisecond share them.
- IDs made 1 ms apart sort as strings in creation order.
- The last 20 characters are random (every digit appears at every position) and are 10 consecutive bytes of the random stream.
- 10,000,000 IDs made in one millisecond hold no duplicate; parallel generation makes no duplicate.
- It costs one allocation (the string).

### ParseTraceparent

Reads a W3C `traceparent` value.

#### Parameters
1. `value` `string`: the header value.

#### Returns
1. `traceID` `string`: 32 hex characters.
2. `parentID` `string`: 16 hex characters.
3. `flags` `byte`: the trace flags.
4. `ok` `bool`: `false` when the value is refused; the other results are then zero.

#### Usage

```go
traceID, parentID, flags, ok := tracecontext.ParseTraceparent(r.Header.Get("Traceparent"))
```

#### Rules
- It accepts `VV-<32 hex>-<16 hex>-FF` in lower-case hex, with dashes at indexes 2, 35 and 52; the flags come back as a byte (`a5` is `0xa5`).
- Version `00` must be exactly 55 bytes. Another version needs at least 55 bytes, and byte 55 must be `-` when the value is longer; only the first 55 bytes are read.
- It refuses an empty or short value, upper-case hex, an all-zero trace or parent ID, version `ff`, a non-hex byte, a misplaced dash, spaces, tabs, CR, LF and other control bytes.
- Refusing a megabyte of junk allocates nothing.
- A 55-byte value returns substrings with no allocation; a longer value returns copies, so the IDs never keep a long header alive.
- A fuzz target checks that it never panics and that every accepted value follows the grammar.

### NewRequest

Builds the scope of an HTTP request (or a WebSocket handshake) and the context that carries it.

#### Parameters
1. `parent` `context.Context`: the request's context; `Deadline`, `Done`, `Err` and other values come from it.
2. `h` `http.Header`: the request headers (`nil` is a request without headers).
3. `now` `time.Time`: the request time, used for a new trace ID.

#### Returns
1. `context.Context`: `parent` with the scope.
2. `*Scope`: the scope.

#### Usage

```go
scopeCtx, scope := tracecontext.NewRequest(r.Context(), r.Header, time.Now())
r = r.WithContext(scopeCtx)
```

#### Rules
- `Request` is always fresh; no header sets it.
- One valid `Traceparent` value continues its trace: `TraceID` and `Parent` come from it. An absent, repeated or invalid value starts a new, time-ordered trace and leaves `Parent` empty.
- `Upstream` is the `traceparent` parent ID, else a valid single `X-Request-Id`, else `""`.
- The outgoing flags are the incoming flags `& 0x03`, or `03` for a trace we start.
- A parent that already carries a scope changes nothing: the new scope is built from the headers and is the nearest one.
- The context keeps the parent's values, deadline and cancellation; `WithCancel` on it starts no goroutine.
- It costs two allocations, with or without an incoming `traceparent`: the context that holds the scope and the 55-byte `traceparent` string.
- A new trace takes its trace ID tail and its request ID from one 18-byte random draw; a continued trace draws only the 8 bytes of the request ID. A draw whose ID or tail is all zeros is drawn again.

### NewMessage

Builds the scope of a WebSocket message from its connection's context.

#### Parameters
1. `parent` `context.Context`: the connection's context (the handshake request's).
2. `now` `time.Time`: the message time, used for a new trace ID.

#### Returns
1. `context.Context`: `parent` with the message scope; `context.Background()` with the message scope
   when `parent` carries no scope.
2. `*Scope`: the scope.

#### Usage

```go
messageCtx, scope := tracecontext.NewMessage(conn.Request().Context(), time.Now())
```

#### Rules
- With a connection scope in `parent`: the trace and flags are the connection's, `Connection` and `Parent` are its `Request`, `Message` is fresh, `Request` and `Upstream` are empty.
- Without one: a new trace with flags `03`, `Connection` and `Parent` empty. Its trace tail and message ID come from one 18-byte draw; under a connection the message draws 8 bytes.
- The message context is done when the connection's context ends.
- It costs at most three allocations.

### With

Returns `parent` carrying `s`.

#### Parameters
1. `parent` `context.Context`: the context to wrap (`nil` is `context.Background()`).
2. `s` `*Scope`: the scope.

#### Returns
1. `context.Context`: a context whose nearest scope is `s`.

#### Usage

```go
c = tracecontext.With(c, scope)
```

#### Rules
- `ScopeOf` of the result is `s`.

### ScopeOf

Returns the nearest scope in a context.

#### Parameters
1. `c` `context.Context`: any context.

#### Returns
1. `*Scope`: the scope, or `nil`.

#### Usage

```go
if s := tracecontext.ScopeOf(ctx); s != nil {
	fmt.Println(s.TraceID)
}
```

#### Rules
- It finds the scope through any number of `WithValue`, `WithCancel` and `WithTimeout` wrappers, with no allocation.
- `nil`, `context.Background()` and a context with other values return `nil`.

## `Scope` Methods

### AppendArgs

Appends the scope's IDs as key-value log arguments.

#### Parameters
1. `dst` `[]any`: the arguments to append to.

#### Returns
1. `[]any`: `dst` followed by `trace_id`, `request`, `connection`, `message`, `upstream` and their values, in this order.

#### Usage

```go
logger.Info("Saved", scope.AppendArgs([]any{"user", name})...)
```

#### Rules
- Empty values are skipped: an HTTP scope gives 4 or 6 elements, a message scope 6 (4 without a connection).
- Existing arguments stay in front.
- A nil scope returns `dst` unchanged (`nil` stays `nil`).

### Traceparent

Returns the outgoing `traceparent`: `00-<TraceID>-<SpanID()>-<flags>`, 55 bytes.

#### Returns
1. `string`: the value, or `""` on a scope built by hand.

#### Usage

```go
req.Header.Set("Traceparent", scope.Traceparent())
```

#### Rules
- It parses back with `ParseTraceparent` to the trace ID, the request (or message) as the span, and the flags.
- It allocates nothing.

### SpanID

Returns the scope's own span ID: `Request`, or `Message` when `Request` is empty.

#### Returns
1. `string`: 16 hex characters.

### Headers

Returns the prebuilt one-element values of the `X-Request-Id` and `Traceparent` response headers of a
request scope, so setting them costs no allocation. The slices are backed by the scope: never write
them in place. On a message scope the `X-Request-Id` value is `""` and the `Traceparent` value is the
message's `Traceparent()`.

#### Returns
1. `requestID` `[]string`: `[Request]`.
2. `traceparent` `[]string`: `[Traceparent()]`.

#### Rules
- The `traceparent` value is always `Traceparent()`.

#### Usage

```go
requestID, traceparent := scope.Headers()
w.Header()["X-Request-Id"] = requestID
w.Header()["Traceparent"] = traceparent
```

## Security
- IDs are identifiers, not secrets: they reach logs, response headers and other services. Never authorize by one.
- Every random bit comes from `crypto/rand`, so a client can't guess the next ID or force a collision.
- A client controls only the trace ID (through a valid `traceparent`) and `Upstream`. Both are checked against a fixed alphabet and length (at most 32 and 64 bytes) before they reach a header or a log line: no CR, LF, quotes or control bytes.
- Parsing reads at most 56 bytes, and repeated header values are refused without being parsed.
- A trace ID shows its creation time to the millisecond.

## Performance notes
- **Random sources in a `sync.Pool` (approved heavy technique H1, design 0013).** IDs copy their bytes out of a 512-byte source taken from a `sync.Pool` and put back before the function returns, so no goroutine waits on another and no lock is taken. A source holds 504 random bytes: it fills on its first read and refills with one `crypto/rand.Read` of exactly 504 bytes when fewer bytes are left than a read needs (the leftover is dropped). A new trace draws 18 bytes at once, so one system call serves 28 requests. A source is used by one goroutine at a time, so no random byte is handed out twice. Tests replace `crypto/rand.Reader` and check that a source returns the reader's stream in order across refills, that each ID is one run of that stream and that every reader call asks for 504 bytes.
- **Idle cost:** no goroutine, no timer. The pool keeps about one source per processor that made an ID recently (at most about 12 KiB with 12 processors) and empties itself after two garbage collections without traffic; the first ID after that costs one 512-byte allocation and one `crypto/rand.Read`.
- **`Traceparent()` is the header value.** The scope stores the `traceparent` once, in the array that `Headers` returns.
- **One string per scope.** The 55-byte `traceparent` is the only string a scope allocates: `TraceID` and `Request` (or `Message`) are substrings of it.
- **One context per scope.** The scope lives inside its context value (224 B with the string), together with the one-element header arrays that `Headers` returns.
- `ScopeOf` walks `Value` once and allocates nothing.

## Benchmarks

Results depend on the machine. They were captured when this README was generated, with `go test -run=^$ -bench=. -benchmem ./internal/tracecontext`:

```console
goos: darwin
goarch: amd64
pkg: github.com/dangduoc08/ginject/internal/tracecontext
cpu: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
BenchmarkNewSpanID-12                             	25177364	        41.13 ns/op	       0 B/op	       0 allocs/op
BenchmarkNewSpanID_Parallel-12                    	24549900	        43.85 ns/op	       0 B/op	       0 allocs/op
BenchmarkNewTraceID-12                            	19814092	        58.54 ns/op	       0 B/op	       0 allocs/op
BenchmarkNewTraceID_Parallel-12                   	40376914	        34.43 ns/op	       0 B/op	       0 allocs/op
BenchmarkScopeOf-12                               	128670400	         9.321 ns/op	       0 B/op	       0 allocs/op
BenchmarkNewRequest_OwnTrace-12                   	 6996877	       171.0 ns/op	     224 B/op	       2 allocs/op
BenchmarkNewRequest_ContinuesTrace-12             	 6039922	       197.7 ns/op	     224 B/op	       2 allocs/op
BenchmarkNewMessage-12                            	 9192940	       131.0 ns/op	     224 B/op	       2 allocs/op
BenchmarkScope_AppendArgs-12                      	20874886	        59.33 ns/op	      48 B/op	       3 allocs/op
BenchmarkParseTraceparent-12                      	24797931	        45.44 ns/op	       0 B/op	       0 allocs/op
BenchmarkParseTraceparent_FutureVersionLong-12    	17991895	        67.61 ns/op	      64 B/op	       1 allocs/op
BenchmarkParseTraceparent_Invalid-12              	189637357	         6.313 ns/op	       0 B/op	       0 allocs/op
BenchmarkParseTraceparent_OneMegabyte-12          	234890870	         5.112 ns/op	       0 B/op	       0 allocs/op
```

The ID benchmarks show no allocation because their result does not escape there.
