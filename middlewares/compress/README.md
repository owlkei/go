# Compress Middleware

*`compress` gzips text responses (JSON, HTML, CSS, JavaScript, SVG) for clients that accept it, with a bounded amount of memory and nothing to configure.*

- [Compress Middleware](#compress-middleware)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [`Compress` Struct](#compress-struct)
    - [MinSize](#minsize)
  - [`Compress` Methods](#compress-methods)
    - [NewMiddleware](#newmiddleware)
    - [Use](#use)
  - [What is compressed](#what-is-compressed)
  - [Rules](#rules)
  - [Memory and CPU](#memory-and-cpu)
  - [Security](#security)
  - [Benchmarks](#benchmarks)

## Key Features
- gzip from the standard library at `gzip.BestSpeed`: about 3 times the throughput of the default level on JSON
- Compresses only what gains: text types, at least `MinSize` bytes, not already encoded, not a range
- `Vary: Accept-Encoding` on every response, so caches keep both versions apart
- At most 8 gzip writers per instance; when all are busy a response is sent uncompressed instead of waiting
- Files served by `SERVE_…` routes that are not compressed keep `sendfile`
- No allocation per response in steady state

## Usage

```go
package main

import (
	"github.com/dangduoc08/ginject/internal/core"
	"github.com/dangduoc08/ginject/middlewares/compress"
)

func main() {
	app := core.New()

	app.BindGlobalMiddlewares(compress.Compress{})

	app.Create(
		core.NewModuleBuilder().Build(),
	)
}
```

A JSON response of 16 KiB to a request with `Accept-Encoding: gzip` goes out with
`Content-Encoding: gzip`, `Vary: Accept-Encoding` and no `Content-Length` (the body is streamed).

## `Compress` Struct

### MinSize
Type: `int`

Default: `1024`

Required: `false`

The smallest body that is compressed, in bytes. Smaller bodies gain little and cost a writer reset
(about 10 µs). `0` means the default; a negative value panics.

```go
compress.Compress{MinSize: 4096}
```

Level, types and the writer count are fixed.

## `Compress` Methods

### NewMiddleware
```go
func (instance Compress) NewMiddleware() common.MiddlewareFn
```

Validates the options once; the framework calls it at `Create()`. It panics with
`invalid compress: ...` for a negative `MinSize`.

### Use
```go
func (instance Compress) Use(r *http.Request, w http.ResponseWriter, next ctx.Next)
```

Makes `Compress` a `common.MiddlewareFn`. It compiles the options on every call, so use it in
tests only; the framework always calls `NewMiddleware`. Called outside an app, it only calls the
next layer.

## What is compressed

A request is a candidate when its method is not `HEAD`, it has no `Range` and no `Upgrade`
header, and `Accept-Encoding` names `gzip` (any case) with a q-value other than 0 within its first
256 bytes.

Its response is compressed when all of these hold, checked at the first write, flush or at the end:

- the status is not `1xx`, `204`, `206` or `304` (error responses are compressed too);
- no `Content-Encoding` and no `Content-Range` header is set;
- `Cache-Control` has no `no-transform`;
- the media type is `text/*` (except `text/event-stream`), `application/json`,
  `application/javascript`, `application/xml`, `application/wasm`, `image/svg+xml`, or ends in
  `+json` or `+xml` (case and parameters ignored);
- the body is at least `MinSize` bytes (a declared `Content-Length`, or the bytes written so far);
- a gzip writer is free.

Without a `Content-Type`, the type is detected from the first bytes (as `net/http` would) and set
on the response, compressed or not.

## Rules
- Every response that passes `compress` gets `Vary: Accept-Encoding`, added to an existing `Vary`,
  also when it is not compressed.
- A compressed response has `Content-Encoding: gzip`, no `Content-Length` and no `Accept-Ranges`; a
  strong `ETag` gets the `W/` prefix. The body is one gzip stream that decodes to the bytes written.
- With an unknown length, up to `MinSize` bytes are held back. A response that ends below `MinSize`
  is sent as written, with its `Content-Length`. A declared `Content-Length` below `MinSize` is
  kept.
- `Flush` decides with what is buffered (without the `MinSize` condition), then flushes the gzip
  stream and the connection, so streaming responses keep working.
- An uncompressed response forwards `ReadFrom`, so `http.ServeFile` keeps `sendfile`.
- A request with an `Upgrade` header (WebSocket) is passed through untouched.
- Header values are shared between responses (built once at startup). Change one only with
  `Header().Set`, `Add` or `Del`, never by writing into the slice in place
  (`w.Header()["Content-Encoding"][0] = "…"`): that changes every other response and is a data race.
- The request and its context are passed on unchanged, and the body is never read.

## Memory and CPU
- Idle: nothing after two garbage collections.
- Peak: 8 writers of about 0.8 MiB each (about 6.5 MiB), plus up to `MinSize` bytes per response
  being decided. Writers are reused through a pool; a response that finds all 8 in use is sent
  uncompressed rather than waiting or allocating a ninth.
- Bind `loadshed` before `compress` to bound the responses in flight.

## Security
- BREACH: compressing a secret next to text an attacker controls, over TLS, can leak the secret
  through the response size. Do not bind `compress` to routes that reflect request input next to a
  secret (a token in a page). `csrf` uses no token, so its pages hold no such secret.
- `Accept-Encoding` is read up to 256 bytes; the level is fixed at `BestSpeed`.

## Benchmarks

Machine-dependent; captured on an Intel i7-9750H, Go 1.27.4, when this README was written
(`go test -run='^$' -bench . -benchmem -count 5 -cpu 1,2 ./middlewares/compress`, median shown).
`bytes-out/byte-in` is the compression ratio, `cpu-ns/op` the process CPU per response.

| Benchmark | GOMAXPROCS 1 | GOMAXPROCS 2 | B/op | allocs/op | bytes-out/byte-in | cpu-ns/op |
|---|---|---|---|---|---|---|
| `Compress_JSON2K` | 6.56 µs | 6.08 µs | 670 | 5 | 0.05029 | 6357 |
| `Compress_JSON16K` | 9.81 µs | 9.33 µs | 656 | 5 | 0.009644 | 9667 |
| `Compress_JSON256K` | 72.15 µs | 70.99 µs | 658 | 5 | 0.003899 | 7.154e+04 |
| `Compress_NotCandidate` | 914 ns | 704 ns | 609 | 5 | 1 | 824.2 |
| `Compress_TooSmall` | 1.49 µs | 1.18 µs | 628 | 7 | 1 | 1314 |
| `Compress_NoWriterFree` | 393 ns | 354 ns | 64 | 2 | 1 | 365.5 |

Each compress benchmark serves a whole request through an app (5 allocations are the app's own, see `Compress_NotCandidate`): compressing adds none in steady state. `TooSmall` adds the `Content-Length` of the held-back body (2 allocations).
