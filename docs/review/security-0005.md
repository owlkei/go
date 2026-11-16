# Security review 0005, step 13: HTTP side (HEAD 453899c)

Scope: `core/http*.go`, `core/server.go`, `core/app.go` ServeHTTP, `ctx` body/form/file,
`internal/tracecontext`, `trace.Transport`, `log` masking,
`middlewares/{proxy,csrf,cors,helmet,hostcheck,httpsredirect,compress}`, `guards/throttler`,
`memorycache`, `modules/cache`, `modules/config`. `middlewares/loadshed` was skipped (rework in
progress) and is reviewed separately.

**Summary:** 0 critical, 0 high, 3 medium, 9 low. govulncheck: no vulnerabilities. gosec: 20
issues outside tests, all false positives.

## Vulnerabilities

| Scanner / source | Result |
|---|---|
| `govulncheck ./...` (v1.9.0; DB 2026-11-11) | No vulnerabilities, reachable or not (2 modules, go1.27.4 standard library). |
| `go list -m -u all` | Direct: `golang.org/x/net` v0.62.0, the latest (2026-10-27). Updates for `x/crypto`, `x/sys`, `x/term`, `x/text` exist but are outside the build graph; no advisory names them. |
| Advisories newer than the DB | x/net v0.62.0 fixes CVE-2026-78661 and CVE-2026-97045 (html, 2026-10-27); none after. |
| npm dev tooling (husky, lint-staged) | Not audited: no lockfile for `npm audit`. Low: never shipped. |

## Toolchain

Local, CI (`go-version-file`) and the latest release are go1.27.4 (2026-11-10 security release).
Nothing to do.

## gosec triage (20 issues outside tests)

All false positives: G115 casts bounded by construction (`throttler.go`, `memory_cache.go`,
`tracecontext/id.go`, `internal/num`, `logpipe/sink.go`, `devtool_builder.go`); G602 in
`accesslog/access_log.go` (stages capped at 64); G103 in `log/log_tag.go` (read-only view for
masking); G304 in `modules/config/dotenv.go` (path from configuration, not a request).

## Findings

### M1, medium: multipart temp files leak
`ctx/http_context.go:58`, `ctx/http_form.go:45`. `Init` stores `r.WithContext(...)`, a shallow copy;
`ParseMultipartForm` runs on the copy, and `net/http` removes temp files only for the original
request. With `MaxRequestBodyBytes` above 32 MiB (or no cap), repeated large multipart uploads fill
`$TMPDIR`. With the default 10 MiB cap, parts stay in memory. Fix: `MultipartForm.RemoveAll()` after
the request lifecycle.

### M2, medium: client-supplied forwarded host and scheme accepted
`middlewares/proxy/proxy.go:228-267`, used by `httpsredirect.go:64-73`. With `XForwarded`, a value of
`X-Forwarded-Host` / `-Proto` is accepted even when the trusted proxy never writes that header (nginx
passes a client's `X-Forwarded-Host` through by default). `X-Forwarded-Host: evil.example` yields a
cacheable `308 Location: https://evil.example/...` and poisoned links built from `proxy.Host(r)`; a
spoofed `X-Forwarded-Proto: https` turns off the redirect. Fix: accept host and scheme only when
their value count matches the walked `X-Forwarded-For` entries; document that the proxy must set or
strip them.

### M3, medium: a non-string log key breaks masking
`log/log_mask.go:162-163`, `log/log.go:368-370`. slog consumes one argument for a non-string key;
the masker skips two and checks the wrong pairs. `l.Error("login failed", err, "password", pw)`
prints the password in clear (built-in logger and `WrapLogger`). Fix: advance by one, as slog does.

### Low
- **L1** `LogValuer` groups are not masked (`log/log_mask.go:312-318`): walk the resolved group.
- **L2** Hops mode of `proxy` doesn't check the peer (`proxy.go:119-120`): an app reachable directly
  lets a client choose its IP. Documented; proposal: an optional peer prefix in hops mode.
- **L3** Throttler eviction: an attacker with a /48 (65,536 /64 keys) can push its own counters out
  of the 32,768-entry default store. Documented (0014 Q7); proposal: optional coarser IPv6 keys.
- **L4** Throttler not atomic across instances with a shared `Backend` (sliding window, token bucket):
  document, or a CAS hook on `cache.Cache` later.
- **L5** Package-level header value slices shared across responses (`helmet`, `cors`, `compress`,
  `core/http.go`): safe with `Set` / `Add`, a race if later code writes `h[k][0]` in place. Document.
- **L6** `ctx.Redirect` has no open-redirect guard (`ctx/http_context.go:142-147`): README warning;
  proposal: a local-redirect helper.
- **L7** `c.JSON` / `c.Text` format strings allow JSON injection (`ctx/http_context.go:153-157`):
  documented; proposal: escape string arguments (public API review).
- **L8** `trace.Transport` sends `traceparent` to every host, and incoming trace IDs and flags are
  adopted: document; proposal: a host allowlist.
- **L9** A static route with root `"."` answers 400 for every file (`core/http.go:451-458`):
  functional bug.

No finding in: static files (`serveFromRoot`), server defaults (timeouts, header and body limits),
error bodies, request IDs and `traceparent` validation, `hostcheck`, `httpsredirect` location
building, `csrf`, `cors`, `helmet`, `compress` (writer cap, `Range`, encoded responses),
`memorycache` (seeded `maphash`, bounded shards), `modules/config` (size caps, only keys exposed).

## Decisions (orchestrator, 2026-11-17)

- Fixed in roadmap 0005: M1, M2 (count-matched forwarded host and scheme, plus docs), M3, L1, L9.
- Documented in the READMEs (step 14): L2, L3, L4, L5, L6, L7, L8.
- Proposals for the public API review: L6 local-redirect helper, L7 escaped format arguments, L8
  host allowlist, L4 CAS hook, L3 coarser IPv6 keys.
- npm lockfile for `npm audit`: backlog (dev tooling only).

Sources: [Go release history](https://go.dev/doc/devel/release),
[x/net v0.62.0 advisory](https://seclists.org/oss-sec/2026/q4/407),
[GO-2026-4440](https://pkg.go.dev/vuln/GO-2026-4440).
