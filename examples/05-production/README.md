# 05 · Production

A small public feedback API, set up the way it should run in production. Anyone can read and post
notes, so the app protects itself: it sheds load under pressure, sends security headers, lets only
your web app call it from a browser, limits how fast one client can post, caps request bodies and
shuts down without dropping requests.

## What you learn

- **One `AppOptions` block.** The whole configuration is the struct given to `owlkei.New`. Nothing
  protects the app until you set it: every protection is off by default.
- **Protections in a fixed order.** They run before routing and before your own middlewares. Load
  shedding runs first, so a request refused under overload costs as little as possible; then the
  security headers and CORS for the browser origin you allow. `Origins` is one list for CORS, CSRF
  and the WebSocket handshake. Behind a load balancer, set `Proxy` so the rate limit sees the real
  client address.
- **Two rate limits.** A global `throttler` guard gives every client 100 requests a minute. The
  controller binds a tighter one, 5 posts a minute, to `CREATE_feedback` only.
- **A body limit.** `HTTP.MaxBodyBytes` refuses a body over 16 KiB with `413`.
  `ShutdownTimeout` gives running requests 10 seconds to finish after `SIGINT` or `SIGTERM`.
- **An access log.** `IsAccessLog: true` writes one line per request.
- **Bounded memory.** Anyone can post, so the inbox keeps the newest 500 notes: a flood of posts can't
  fill a small server's memory.
- **What Owlkei Go already does for you.** Server timeouts, a 10 MiB body limit, a fixed `500` for
  any panic that is not an exception, and graceful shutdown are always on.

## Run it

```sh
go run .                                     # http://localhost:3000
WEB_ORIGIN=https://app.example.com go run .  # the origin your web app is served from
```

```sh
curl -i localhost:3000/feedback              # security headers on every answer
curl -X POST localhost:3000/feedback -H 'Content-Type: application/json' \
  -d '{"message":"The checkout page is slow"}'
```

Post six times within a minute: the sixth answers `429` with `Retry-After`.

`Strict-Transport-Security` is sent only over HTTPS: serve with `Server.TLS`, or put the app behind a
proxy that terminates TLS.

`go test ./...` checks the headers, the CORS preflight, the body limit, the post limit and a clean
shutdown, on a real listener.

## Read next

[Configuration](../../README.md#configuration) in the main README covers every `AppOptions` field
with its default and its `OK_*` environment variable, and has the
[production checklist](../../README.md#production-checklist) to copy. The rate limit is the
[`throttler`](../../guards/throttler/README.md) guard.
