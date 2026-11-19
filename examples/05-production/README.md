# 05 · Production

A small public feedback API, set up the way it should run in production. Anyone can read and post
notes, so the app protects itself: it sheds load under pressure, sends security headers, lets only
your web app call it from a browser, limits how fast one client can post, caps request bodies and
shuts down without dropping requests.

## What you learn

- **Global middlewares, in order.** `loadshed` first, so a request refused under overload costs as
  little as possible; then `helmet` for the security headers and `cors` for the browser origin you
  allow. Behind a load balancer, add `proxy` right after `loadshed` so the rate limit sees the real
  client address.
- **Two rate limits.** A global `throttler` guard gives every client 100 requests a minute. The
  controller binds a tighter one, 5 posts a minute, to `CREATE_feedback` only.
- **Server options.** `MaxRequestBodyBytes` refuses a body over 16 KiB with `413`.
  `ShutdownTimeout` gives running requests 10 seconds to finish after `SIGINT` or `SIGTERM`.
- **An access log.** `app.EnableAccessLog()` writes one line per request.
- **Bounded memory.** Anyone can post, so the inbox keeps the newest 500 notes: a flood of posts can't
  fill a small server's memory.
- **What Owlky Go already does for you.** Server timeouts, a fixed `500` for any panic that is not an
  exception, and graceful shutdown are on by default. You only set what differs.

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

`Strict-Transport-Security` is sent only over HTTPS: serve with `ListenTLS`, or put the app behind a
proxy that terminates TLS.

`go test ./...` checks the headers, the CORS preflight, the body limit, the post limit and a clean
shutdown, on a real listener.

## Read next

The [main README](../../README.md) covers every option. Each middleware and guard has its own
README under `middlewares/` and `guards/`.
