# Backlog

Decided work that has no roadmap yet. A roadmap that takes an item links back to it and removes it
from here.

## Own WebSocket implementation (after HTTP is production ready)

Decided 2026-11-16: Ginject replaces `golang.org/x/net/websocket` (its last dependency; its own
documentation points to other packages) with an implementation of its own, once the HTTP roadmaps
(0003 to 0005) are done. It becomes part of the WebSocket roadmap (0006).

- **Why:** no dependency left in `go.mod`; hot-path control by the rules of `.ginject/conventions.md`
  (buffer reuse, low allocations); backpressure, limits, trace and devtools hooks at the frame
  level; ownership of every line.
- **Scope, phase 1 (RFC 6455):** the handshake through `http.Hijacker`; framing (masking, 7 / 16 /
  64-bit lengths), fragmented messages, control frames (ping, pong, close) and the close handshake;
  UTF-8 validation of text messages; frame and message size limits; read and write deadlines.
- **Scope, phase 2 (RFC 7692):** `permessage-deflate` with `compress/flate`, with a decompressed-size
  limit (decompression bombs). Phase 2 may come later than phase 1.
- **Verification:** the Autobahn TestSuite on CI (Docker; every core case passes before the
  implementation counts as done), fuzz tests of the frame parser (no panic, bounded memory), and
  benchmarks against `coder/websocket` and `gorilla/websocket` in `bench/`.
- **Joins the remaining WebSocket work:** acks, backpressure, limits, hooks, shutdown, broker.

## Public API surface (after roadmap 0005)

Decided 2026-11-16 (Product principles in `CLAUDE.md`): Ginject exports about 607 functions, types and
methods outside `sample/` and `bench/`, most of them framework internals (`common.Construct`,
`BuildHTTPInterceptMiddleware`, the `common.InsertedRoutes` global, `routing` internals,
`ctx.BinderOf`, the devtool builder). A dedicated roadmap, after 0005 and before WebSocket: review
every exported name, move internals to `internal/` or unexport them, collapse duplicate ways of doing
one job, and rewrite the docs around the resulting surface.

## MCP (with the devtools platform)

Decided 2026-11-16: Ginject gets MCP support when development of the devtools platform starts, in
three forms:

1. **A developer MCP:** tools AI coding agents query to write correct Ginject code (concepts, the
   route DSL, injectable types, examples, scaffolding of modules / controllers / guards, a handler
   signature check, explanations of bootstrap panics). Lives outside the framework's code.
2. **A runtime MCP module** (for example `modules/mcp`, `app.EnableMCP()`): an app's routes exposed as
   MCP tools, with input schemas from the DTOs (the devtool snapshot already computes them), the
   Streamable HTTP transport, auth through guards, nothing exposed unless chosen, rate limits.
   Implemented by Ginject itself (JSON-RPC over HTTP), to keep `go.mod` free of dependencies.
3. **A devtools MCP:** AI agents read a running app through the devtools platform (routes, DI graph,
   traces of failed requests).

## Heavy techniques waiting for approval

Designed and measured in the roadmap 0002 designs, not built (each needs the maintainer's approval
per use). Details and expected gains: `docs/review/http-0002.md` › What is left.

| Design | Technique |
|---|---|
| 0003 H1 | one scratch buffer and a kept JSON encoder per pooled context |
| 0003 H2 | shared read-only header value slices |
