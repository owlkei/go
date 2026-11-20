# 0021: Security

Status: Approved
Packages: `internal/core`, `internal/ctx`, `internal/common`, `internal/render`, `internal/routing`, `log`, `.golangci.yml`, `.github/workflows/ci.yml`, `api/owlkei.txt`

Roadmap: `docs/roadmap/0012-security.md`, steps 1b, 1c, 2, 3, 5, 7, 8, 9. Configuration API: design 0022
(`AppOptions`, `HTTP.ServeDirs` declared there as D1, enforced here). Step 1 (the general audit) is a
review; its findings of high or critical rank land in the rule families below (see "Room for audit
findings").

## Goal

A static route can serve only files below a declared directory, never source or secrets, never an
uploaded file that runs in the browser. A personal page is not stored by a shared cache, `c.Redirect`
cannot leave the site, a layer bound to a page also covers its form post, a guarded page's CSS and JS
are guarded with it, and paths, headers and log lines cannot be bent by control characters. CI fails
on a new `gosec` finding.

## Public API

One exported addition, one field whose meaning becomes enforced, no removal.

```go
// internal/ctx/http_context.go: new
func (c *HTTPContext) RedirectExternal(url string)

// internal/core/options.go: godoc only, the field exists (design 0022 D1)
type HTTPOptions struct {
	MaxBodyBytes ByteSize
	// ServeDirs lists the directories SERVE_ handlers may return. A SERVE_ route needs at least one. Default: empty.
	ServeDirs []string
}
```

`api/owlkei.txt` gains exactly one line:
`reach ctx.HTTPContext, method (*ctx.HTTPContext) RedirectExternal(string)`.

- `RedirectExternal` is justified by the roadmap ("another host needs an explicit call"): a second
  name that a reviewer can grep for. `c.Redirect` and the injected `owlkei.Redirect` (`func(string)`,
  still `c.Redirect`) keep their signatures and become same-origin only (R1). No injectable type is
  added for the external form: it is called on `owlkei.HTTPContext`.
- No new option. Inline serving of active files is opted into by the handler's own
  `Content-Disposition` header (I3), like `Cache-Control` today. Source maps follow `Render.IsReload`
  (F3). Open questions 3 and 5 hold the alternatives that add API.

Unexported additions (names are the builder's; listed so the reviewer knows what to expect):
`internal/core/http_static.go` (name policy, forbidden names, content types, inline policy, root
resolution), `httpRoute.name`, `httpRoute.isOpen`, `HTTP.serveDirs`, `HTTP.workDir`,
`HTTP.routeByPageAsset`, `common.SubmitNameOf`, `render.Engine.PageAssetOwner`,
`render.Engine.Skipped`.

## Behavior

Shared terms:

- **Static response**: a response of a `SERVE_` route, a `public/` route, `/assets/*` or
  `/assets/_pages/*`.
- **Fixed 404**: status 404, body `notFoundBody` (`{"code":404,"error":"Not Found","message":"Not
  Found"}`), `Cache-Control` and `Etag` removed. Every name refusal below answers the fixed 404, so a
  refused file and a missing file are indistinguishable.
- **Control character**: a byte below `0x20`, or `0x7f`.

### N: path normalization before matching (step 8)

- **N1.** The checks of N2 and N3 run first in `App.ServeHTTP`, before the WebSocket path test, the
  pre-route stages and matching. Stages, middlewares, guards, handlers, the access log and the router
  all read the same `c.URL.Path`.
- **N2.** A path holding a control character (after the one decoding `net/http` did: `%00`, `%0a`,
  `%0d`, `%7f`…) answers 400 through `writeEarlyError` with the existing `invalidURLBody`
  (`{"code":400,"error":"Bad Request","message":"Invalid URL path"}`; HTML when `Render.FS` is set and
  `Accept` holds `text/html`). No stage, middleware or route runs.
- **N3.** A path that is not canonical is rewritten in place to its canonical form before anything
  reads it: empty segments removed (`//admin` → `/admin`), `.` and `..` resolved (`/x/../admin` →
  `/admin`, `/../../etc` → `/etc`), a trailing slash kept (`/a/./` → `/a/`), an empty path → `/`. The
  request's `URL` is cloned before the write (the caller's `*url.URL` is untouched), `RawPath` is
  cleared, `RequestURI` is left as received. A canonical path is not touched and not copied.
- **N4.** The framework decodes a path once (what `net/http` did) and never again: `%252e%252e`
  reaches the app as the literal segment `%2e%2e`, a `Param` value included.
- **N5.** Matching stays case-sensitive and literal: `/Admin` does not match the route `/admin`.
  Existing limits stay (`MaxPathLength`, `MaxPathSegments`: no match → 404).

### S: the name policy of static routes (step 1b, 1c)

- **S1.** One function decides whether a relative name may be served. It works on bytes, with `/` as
  the only separator, and gives the same answer on every OS, so Linux CI proves the Windows and macOS
  answers. A name is refused (fixed 404) when any segment:
  1. holds a control character or NUL;
  2. holds `\` or `:` (Windows separators, drive letters `C:`, NTFS streams `x.txt::$DATA`);
  3. starts with `.`, except the exact segment `.well-known` (today's hidden rule);
  4. ends with `.` or a space (Windows drops them: `main.go.` opens `main.go`);
  5. holds `~` followed by a digit (Windows short names: `GIT~1`, `SECRE~1.GO`);
  6. is, before its first `.` and ignoring ASCII case, a Windows device name: `CON`, `PRN`, `AUX`,
     `NUL`, `COM1`-`COM9`, `LPT1`-`LPT9`;
  7. is forbidden by F1 (the last segment only).
- **S2.** The policy runs on the request path before any file system call, in: `SERVE_` (D4: on the
  whole path below the declared directory), `/assets/*` and `/assets/_pages/*` (in `serveAsset`,
  replacing `hasHiddenSegment`). `public/` routes are exact static routes: their names pass the policy
  once at `Create()` (P2).
- **S3.** The policy also runs when the file tables are built at `Create()` (`assets/`, `public/`, page
  CSS and JS): a name it refuses is not registered and is reported by P3.
- **S4.** Files are opened as today: `SERVE_` through `os.Root` (no traversal, no symbolic link that
  leaves the root, D5), the others through the `fs.FS` of `Render.FS`. A directory answers its
  `index.html` on `SERVE_` routes only, else the fixed 404; no response lists a directory.
- **S5.** With `Render.IsReload` and an `embed.FS`, the disk directory is opened with
  `os.OpenRoot(dir)` and read through `root.FS()` instead of `os.DirFS(dir)`: a symbolic link under
  `web/` that leaves it is not followed (templates, `assets/`, `public/`, page files).
- **S6.** A directory entry that is a symbolic link (`d.Type()&fs.ModeSymlink != 0`) in `assets/`,
  `public/` or among page files is skipped at `Create()` and reported by P3 (an `embed.FS` never holds
  one; this covers `Render.FS: os.DirFS(…)`).

### F: forbidden files (step 1c e)

- **F1.** A file whose last segment, in ASCII lower case, matches the table is never served by any
  static route (fixed 404), whatever the directory and whatever the handler sets:

  | Kind | Match |
  |---|---|
  | Go source and module files | suffix `.go`; exact `go.mod`, `go.sum`, `go.work`, `go.work.sum` |
  | Environment files | prefix `.env` (already hidden by S1.3); suffix `.env` (`prod.env`) |
  | Keys and certificates bundles | suffix `.pem`, `.key`, `.p12`, `.pfx`; exact `id_rsa`, `id_dsa`, `id_ecdsa`, `id_ed25519` |
  | Editor and backup files | suffix `~`, `.swp`, `.swo`, `.bak`, `.orig`, `.rej`, `.old`, `.tmp`; prefix and suffix `#` (`#main.go#`) |
  | Database dumps | suffix `.sql`, `.sqlite`, `.sqlite3`, `.db` |
  | Source maps | suffix `.map`, unless F3 |

- **F2.** The table is one unexported function in `internal/core/http_static.go`
  (`isForbiddenFile(name string) bool`: a suffix `switch` on the lower-cased extension, no regexp, no
  allocation). There is no option to change it: a file of such a kind that must be downloadable is
  sent by a `READ_` handler that writes the bytes itself.
- **F3.** `.map` files are served only below `/assets/` (shared and page assets) and only when
  `Render.IsReload` is on. Everywhere else, and in production, F1 applies.

### D: `HTTP.ServeDirs` (step 1c a)

- **D1.** At `New`, each entry is cleaned and made absolute against the working directory read once at
  `New` (kept as `workDir`). `New` panics `invalid app: <detail>`:

  | Input | Detail |
  |---|---|
  | an entry that does not exist or is not a directory | `HTTP.ServeDirs entry <q> is not a directory` |
  | an entry equal to the working directory, an ancestor of it, or a file system root | `HTTP.ServeDirs entry <q> is the working directory or above it: serve a subdirectory` |
  | more than 64 entries | `HTTP.ServeDirs has <n> entries, at most 64 are allowed` |
  | an empty entry (design 0022 A3, kept) | `HTTP.ServeDirs has an empty entry` |

  `<q>` is the entry as written, quoted with `%q`. Nested and duplicate entries are allowed.
- **D2.** At `Create()`, a `SERVE_` route with an empty `ServeDirs` panics
  `invalid app: <Controller>.<SERVE_name> serves files, but HTTP.ServeDirs is empty: list the directories it may return`.
  Routes of async modules are checked the same way when they are registered at `Create()` (their
  routes exist behind the gate from the start).
- **D3.** Per request, the handler's result is resolved without a system call: a non-string or empty
  result → fixed 404 (as today); otherwise `filepath.Clean`, joined to `workDir` when relative. The
  root is accepted when it equals a declared directory or lies below one (byte comparison on a path
  boundary, on every OS; the longest declared directory wins).
- **D4.** A root that is not accepted, or that holds a control character, is never opened: the response
  is 500 with the fixed JSON `{"code":500,"error":"Internal Server Error","message":"Internal Server
  Error"}`, no `Cache-Control`, and one log line through `logger.From(ctx)`:
  `Error "ServeRootRefused" route=<Controller.SERVE_name> root=<the cleaned root, at most 256 bytes> note="the directory is not under HTTP.ServeDirs"`.
  Example: `return "uploads/" + tenant` with `tenant = "../../etc"` → `/etc` → 500, nothing read.
- **D5.** An accepted root is served through `os.Root`: the declared directory is opened with
  `os.OpenRoot`, the handler's root below it with `Root.OpenRoot(sub)` (skipped when the two are
  equal), the file below that. Consequences, each a test: a symbolic link leaving the handler's root is
  refused even when its target stays inside the declared directory (tenant A cannot link to tenant B);
  a handler root that is itself a link leaving the declared directory is refused; refusals are the
  fixed 404.
- **D6.** The name policy (S1) runs on `sub + "/" + <the path after the route's wildcard>`:
  `tenant = ".git"` or `tenant = "x\\..\\y"` answers the fixed 404.
- **D7.** A `SERVE_` route without a wildcard (the handler returns one file or directory) follows D3 to
  D6 too: the returned path is split into its declared directory and the name below it;
  `http.ServeFile` on a handler-chosen path is no longer used. The `"."` result of today's docs is
  refused by D1 (the working directory cannot be declared).

### M: content type, sniffing, ranges (step 1c c)

- **M1.** Every static response carries `X-Content-Type-Options: nosniff`, set by the serving code, with
  or without the `Headers` stage, on 200, 206 and 304.
- **M2.** `Content-Type` is always set by the framework before `http.ServeContent` runs, so the
  standard library never sniffs content: first a fixed table (`.css` `text/css; charset=utf-8`, `.js`
  and `.mjs` `text/javascript; charset=utf-8`, `.html` and `.htm` `text/html; charset=utf-8`, `.svg`
  `image/svg+xml`, `.json` and `.map` `application/json`, `.wasm` `application/wasm`, `.xml`
  `text/xml; charset=utf-8`, `.txt` `text/plain; charset=utf-8`), then `mime.TypeByExtension`, then
  `application/octet-stream`. The fixed table makes `.js` and `.css` independent of the OS registry
  (Windows may map `.js` to `text/plain`, which `nosniff` then blocks). A `Content-Type` the handler or
  a layer set is kept.
- **M3.** A `Range` header naming more than one range (it holds `,`) is ignored: the full file is sent
  with 200. Single ranges, `If-Range`, `If-Modified-Since` and `If-None-Match` behave as today.

### I: files that run in a browser (step 1c d)

- **I1.** An **active file** is a file served by a `SERVE_` route whose extension (ASCII lower case) is
  one of `.html`, `.htm`, `.xhtml`, `.xht`, `.shtml`, `.svg`, `.svgz`, `.xml`, `.xsl`, `.xslt`, `.js`,
  `.mjs`, or whose `Content-Type` from M2 is `text/html`, `application/xhtml+xml`, `image/svg+xml`,
  `text/xml`, `application/xml`, any `…+xml`, `text/javascript` or `application/javascript`. A
  directory's `index.html` is an active file. `public/` and `/assets/…` files are the developer's own
  and are not concerned.
- **I2.** By default an active file goes out with `Content-Disposition: attachment` (no `filename`
  parameter: the browser names the download after the URL, and no file name reaches a header) and
  `Content-Security-Policy: sandbox; default-src 'none'`, which replaces the policy of the `Headers`
  stage for this response. M1 applies. Status, ranges and caching are unchanged.
- **I3.** Opt-in per route: when `Content-Disposition` is already set on the response when the handler
  returns (by the handler or a layer), the framework keeps it, adds no CSP of its own, and serves the
  file as any other. `c.ResponseWriter.Header().Set("Content-Disposition", "inline")` in the `SERVE_`
  handler serves a built site (`SERVE_docs_ANY` returning `"dist"`) as before.
- **I4.** The first time a route applies I2, one line is logged (once per route and process, an
  `atomic.Bool` on the route):
  `Warn "ServeAttachment" route=<Controller.SERVE_name> file=<name, at most 256 bytes> note="files that run in a browser are sent as downloads; set Content-Disposition: inline in the handler when these files are yours, never for uploads"`.

### C: private caching (steps 2, 1c b)

- **C1.** A rendered page (`RENDER_`, a `SUBMIT_` that renders) and every HTML error page (template or
  built-in, from a page route or from `Accept`) answers `Cache-Control: private, no-cache` when no
  layer or handler set `Cache-Control` before. A value set before is kept as is.
- **C2.** A `SERVE_` route with at least one guard (global, controller-wide or bound to it) defaults to
  `Cache-Control: private, no-cache`; without a guard, `no-cache` as today. A value set before is kept.
- **C3.** Page CSS and JS of a page with at least one guard (G5): `private, no-cache`, or
  `private, max-age=31536000, immutable` for a `?v=<hash>` URL. Other assets and `public/` keep today's
  values (`no-cache`; `public, max-age=31536000, immutable` with `?v=`).
- **C4.** JSON errors keep no `Cache-Control` (`serveError` removes it, as today).

### R: redirects (step 3)

- **R1.** `c.Redirect(target)` accepts a same-origin path on every route (page and API; open question
  1). `target` is accepted when all hold: it is not empty; it starts with `/`; its second byte is
  neither `/` nor `\`; it holds no `\` and no control character anywhere. Accepted: `/`, `/bookings`,
  `/a?next=/b#top`, `/a?u=https://x.test`. Refused: `""`, `https://evil.test`, `//evil.test`,
  `/\evil.test`, `\\evil.test`, `/a\b`, `javascript:alert(1)`, `evil.test`, `bookings` (relative),
  `/a\r\nSet-Cookie: x=1`, `/\t/evil.test`.
- **R2.** `c.RedirectExternal(target)` accepts what R1 accepts, plus an absolute URL: `url.Parse`
  succeeds, the scheme is `http` or `https` (ASCII case ignored), the host is not empty, there is no
  user info, and the text holds no `\` and no control character. Refused: `javascript:…`, `data:…`,
  `//evil.test` (no scheme), `https://user@evil.test`, `https:///path`.
- **R3.** Status as today for both: the code set with `c.Status` when it is 300-399, else 303 on a page
  route and 301 elsewhere. An accepted target is sent with `http.Redirect` unchanged.
- **R4.** A refused target never reaches the `Location` header. The response is a redirect with the
  same status to `/`, and one line is logged through `logger.From(ctx)`:
  `Warn "RedirectRefused" method=<m> path=<request path> target=<first 256 bytes of the target> note="c.Redirect accepts a path of this site; another site needs c.RedirectExternal"`
  (for `RedirectExternal`: `note="c.RedirectExternal accepts a path or an http(s) URL"`). The logger
  escapes the target (L1).
- **R5.** The checks are byte loops over the target (no `url.Parse` in `c.Redirect`, one in
  `RedirectExternal` for an absolute URL); they run only when a handler redirects.

### G: guard coverage (steps 5, 7)

- **G1.** No layer moves on its own (owner, 2026-11-21: explicit, no magic). A **guard** bound to a
  `RENDER_` handler whose controller has the `SUBMIT_` of the same words must also be bound to that
  `SUBMIT_` (or to the whole controller); otherwise `Create()` panics:
  `invalid guard: <Guard> is bound to <Controller>.RENDER_<x> but not to <Controller>.SUBMIT_<x>, which posts to the same page: bind it to both, or to the controller`.
  The check runs once in `httpHandlerTargets` (`internal/common/fn.go`) when the controller's layers are
  collected; global guards and controller-wide bindings satisfy it.
- **G2.** Middlewares, interceptors and exception filters keep today's rule: bound per handler, no check
  (an interceptor of the page's `GET` must not run on its form's `POST` by accident).
- **G3.** A guard bound to a `SUBMIT_` only covers the POST only, and needs nothing on the `RENDER_`. A
  layer bound without handlers covers every route, as today.
- **G4.** No guard runs for a `public/` route nor for a shared asset (`/assets/<name>` outside
  `_pages`): global guards included (this replaces design 0018 R40 and the guard part of P24). The
  pre-route stages and the global middlewares still run. `throttler` bound as a global guard no longer
  counts these requests (`LoadShed` still covers them).
- **G5.** A page file (`/assets/_pages/<name>`) owned by a page template runs the guards of the
  `RENDER_` route bound to that template, in that route's order (global, controller-wide,
  route-bound), before any file is opened. A page file owned by a `layout.html` or an error page is
  shared: no guard runs. With `IsReload`, the owner is read from the current template set.
- **G6.** The guards receive the asset request: `c.URL.Path` is the asset URL and `c.Param()` is empty.
  A guard returning `false`, or panicking an exception below 500, answers the fixed 404 (not 403: the
  file of a guarded page and a missing file look the same). Another panic answers 500 as today.
- **G7.** When the owning route is behind an async module's gate that is not live, the page file
  answers what the gate answers for an API route (503 JSON, `Retry-After` while pending); when live,
  the live route's guards run.
- **G8.** An unknown page file name answers the fixed 404 without running a guard.
- **G9.** `SERVE_` routes keep their guards as any route: a protected file is served by a guarded
  `SERVE_` route (C2 makes it private), never from `public/`.

### P: `public/`, hidden files, startup log (steps 7, 1c f)

- **P1.** A file below `public/` whose path holds `{`, `}` or `*` stops `Create()`:
  `invalid render: public/<path>: a public file name can't hold '{', '}' or '*', which the router reads as a parameter or a wildcard`.
- **P2.** Every other `public/` name must pass S1; a name that does not is not registered (P3).
- **P3.** At `Create()`, when S3, S6 or P2 skipped files, one line:
  `Warn "StaticFilesSkipped" count=<n> files=<the first 20 paths below Dir, sorted> note="source, secret, backup, linked or oddly named files are never served"`.
  Hidden names are not in this list (P4).
- **P4.** At `Create()`, without `IsReload`, hidden entries below `Dir` (a name starting with `.`,
  except `.well-known`; a hidden directory counts once and is not walked) are reported in one line:
  `Warn "RenderHiddenFiles" count=<n> files=<the first 20, sorted> note="these ship inside the binary with //go:embed all: and are never served"`.
  No line when there are none. With `IsReload` nothing is logged (`.DS_Store` on a developer's disk).
- **P5.** At `Create()`, when `public/` holds files, one line:
  `Info "PublicFiles" count=<n> urls=<the first 50 URLs, sorted>`.

### H: response headers (step 8)

- **H1.** No header the framework writes takes text from a request or a file name without a check:
  `Location` (R1, R2), `Content-Disposition` (constant, I2), `Content-Security-Policy` (constant or
  from `AppOptions`, checked at `New`), `X-Request-Id` and `traceparent` (generated or validated by
  `tracecontext`), `Allow`, `Vary`, `Retry-After`, `Etag` (built at `Create()`).
- **H2.** A header value that a handler sets with CR, LF or NUL cannot split the response: over
  HTTP/1.1 the standard library writes CR and LF as spaces, over HTTP/2 it drops the field. The
  framework adds no per-response scan (open question 12); H2 is pinned by tests on both protocols so a
  Go upgrade that changes it fails CI.

### L: log lines (step 8)

- **L1.** In every format (`FormatJSON`, `FormatText`, `FormatPretty`), for the message, every key and
  every string value (errors and nested values included): one record cannot start a second record, and
  the output holds no raw control character, no C1 character (U+0080-U+009F), no line or paragraph
  separator (U+2028, U+2029) and no bidirectional control (U+202A-U+202E, U+2066-U+2069). They appear
  escaped (`\n`, `\u001b`, `\u202e`…); invalid UTF-8 is escaped or replaced, never copied.
  `FormatPretty` may break a value over lines, each continuation line starting with its tree prefix.
- **L2.** Known gaps to close (read from `log/log.go` `encoder.Write`, which escapes C1 for JSON only):
  `FormatJSON` leaves `0x7f` and the bidirectional controls raw. `FormatText` relies on
  `strconv.Quote` through `slog`; the L1 tests decide whether it needs code. `FormatPretty` already
  implements L1 (`appendEscaped`, `escapeKey`).
- **L3.** A logger given through `AppOptions.Logger` that is not Owlkei's receives the raw arguments:
  its escaping is its own. Documented in step 10.

### X: `gosec` in CI (step 9)

- **X1.** `gosec` runs as a `golangci-lint` linter, enabled in `.golangci.yml`, so the existing CI
  lint job, `make check`, `make lint`, the pre-commit hook and the Claude hooks all run it with no new
  tool or workflow step.
- **X2.** Rules: every `G1xx` (general: hard-coded credentials, `unsafe`, integer conversions, SSRF,
  decompression bombs, missing `ReadHeaderTimeout`…), `G2xx` (`G201`, `G202` SQL built from strings;
  `G203` unescaped template data; `G204` commands), `G3xx` (file paths and permissions, archive
  traversal), `G4xx` (crypto and TLS), `G5xx` (blocked imports) and `G6xx` (aliasing, slice bounds).
  One rule is off for the whole repository: `G104` (unchecked errors: `errcheck` already runs).
- **X3.** The baseline is the `linters.exclusions.rules` list of `.golangci.yml`: one entry per accepted
  finding or file (`path`, `linters: [gosec]`, `text: "G<nnn>"`), each preceded by a YAML comment with
  the reason. No `#nosec` and no `//nolint` comment enters Go code (the no-comment rule). One entry
  covers `_test.go` files for `G103`, `G110`, `G115`, `G301`, `G306`, `G404`. `issues.max-issues-per-linter`
  and `issues.max-same-issues` are set to `0` so nothing is hidden by a cap.
- **X4.** Starting point (dry run of `golangci-lint run --enable-only gosec`, 2026-11-21, capped at 50
  issues): `G115` in `internal/num`, `internal/memorycache`, `internal/tracecontext/id.go`,
  `internal/logpipe/sink.go` (bounded conversions: baseline with the bound as reason, or fix); `G304`
  in `internal/dotenv/dotenv.go` and `internal/apicheck` (developer-chosen paths); `G203` in
  `internal/render/parse.go` (the page asset tags the framework builds); `G602` in
  `internal/accesslog/access_log.go` and `internal/render/render.go` (review each: fix or baseline);
  `G301`, `G306` in `internal/apicheck/main.go`. The static serving code of this design must pass
  without a new entry except `G304` on the `os.Root` calls.
- **X5.** Done when a new finding fails `make check` and CI, and the baseline has no entry without a
  reason.

### Room for audit findings

A step-1 or step-1b finding of high or critical rank is fixed inside the family it belongs to, as a new
numbered rule added to this file before its test is written (`S8`, `F4`, `H3`…): a new refused name
shape goes in S1, a new leaking file kind in F1, a new header sink in H1, a new log sink in L1. A
finding that needs a public API or changes a rule above needs a superseding design.

## Amendment 1 (2026-11-21): audit findings H1 and M1

Security review `docs/review/security-0012.md` (general audit of roadmap 0012 step 1, `73e9bbd`)
found two issues of high and medium rank. Following "Room for audit findings", each one is a new
numbered rule in its family. Neither adds a public API.

- **S1.8 (finding H1).** A segment is refused (fixed 404) when it holds a code point that
  case-insensitive file systems (APFS, NTFS) fold to an ASCII letter: U+017F LATIN SMALL LETTER LONG S
  (`C5 BF`, folds to `s`), U+212A KELVIN SIGN (`E2 84 AA`, folds to `k`), U+0130 LATIN CAPITAL LETTER
  I WITH DOT ABOVE (`C4 B0`) and U+0131 LATIN SMALL LETTER DOTLESS I (`C4 B1`). Without it,
  `server.\u212Aey` passes F1 and opens `server.key` on macOS. The check is part of the S1 byte scan
  and does not allocate. It applies everywhere S2 and S3 apply: `SERVE_`, `/assets/*`,
  `/assets/_pages/*` and the file tables built at `Create()`. Other non-ASCII names (`café.txt`,
  `日本.txt`, `ß.txt`) stay legal. Tests: `TestStaticName_UnicodeFold`, `TestStaticAttack_UnicodeFold`.
- **H3 (finding M1).** The `301` that sends a `SERVE_` directory URL to its slash-terminated form
  builds `Location` from the cleaned absolute path (N3) plus `/` and the raw query
  (`/site/en?a=1` → `/site/en/?a=1`), never from a relative segment of the request. A path that does
  not start with exactly one `/`, or that holds `\`, answers the fixed 404 instead. A route parameter
  such as `%5C%5Cevil.test` can no longer produce `Location: \\evil.test/`, which browsers resolve
  to another host. Test: `TestServe_SlashRedirectSameOrigin`. H1 gains this `Location` sink.

## Performance

No rule adds work to a feature that is off; the request path gains one scan.

| Path | Cost | Measure |
|---|---|---|
| Every request (N1-N3) | one pass over the path bytes for control characters, `//` and dot segments, in the loop shape of today's `hasDotSegment`; 0 allocations for a canonical path; a non-canonical path pays one `path.Clean` and one `url.URL` copy | `BenchmarkServeHTTP_Static`, `BenchmarkServeHTTP_Chain`, `BenchmarkServeHTTP_NotFound` in `internal/core` and the `bench/` scenarios against `docs/review/http-0013.md`; expected +5 to 10 ns, inside the noise band. The router keeps its own `normalizePath` |
| Pages and HTML errors (C1) | a different constant slice; 0 | `BenchmarkRender_*` unchanged |
| `c.Redirect` (R) | one byte loop over the target, 0 allocations | `BenchmarkRedirect_SameOrigin` (new) |
| Layers (G1-G3) | resolved at `Create()`; 0 per request | none |
| `public/` and shared assets (G4, M1, M2) | fewer calls than today (no guard loop); one header assignment from a shared slice; the content type from a `switch`; S1 is one pass over the name | `BenchmarkAsset_Hashed`, `BenchmarkPublic_File` (new, run before the change too): not slower |
| Page assets (G5) | one map lookup name → route, then that route's guards | `BenchmarkAsset_PageGuarded` (new) |
| `SERVE_` (D3-D6, S1, F1, I1) | string work on the root and name (one `filepath.Clean`, at most 64 prefix compares, two passes over the name), then the system calls: one `Root.OpenRoot` more than today when the handler's root is below the declared one | `BenchmarkServe_File` (new, run before the change too), `-count 5`, `benchstat`; the system calls dominate |
| Log lines (L2) | JSON: two more `bytes.IndexByte` scans per line (`0x7f`, `0xe2`), the rewrite only when one is found | `BenchmarkLog_Info_Scalars_JSON`, `BenchmarkLog_Info_Struct_JSON` in `log`: within noise |
| `Create()` (D2, P1-P5, S3, S6) | cold: one more walk of `Dir` for hidden names, checks on names already listed | cold-path table of step 11 |

Idle memory: the absolute `ServeDirs` strings, one map entry per page file (name → route pointer), one
`atomic.Bool` per `SERVE_` route. No goroutine, pool, cache or `unsafe` is added; no file handle is
kept between requests.

## Concurrency

Everything this design stores is written at `New` or `Create()` and read-only afterwards
(`serveDirs`, `workDir`, `routeByPageAsset`, the forbidden and content-type tables). The one mutable
value is the per-route `atomic.Bool` of I4 (`CompareAndSwap`, so the line is logged once). Routes of
async modules reach `routeByPageAsset` through the existing gate (`route.gate.live.Load()`), under the
same rules as `handleRequest`. N3 writes a cloned `url.URL` owned by the request. No goroutine is
added, no lock is taken.

## Security

- **Attacker-controlled:** the request path, `Range`, the redirect target a handler passes on, the
  root a handler builds from input, file names and contents of uploads, any text that reaches a log.
- **Order of defenses for a served file:** N (one canonical path) → route → guards → D (root inside a
  declared directory) → S (name shape) → F (file kind) → `os.Root` (traversal and links) → M and I
  (how the browser may treat it) → C (who may store it).
- **Not solved here, documented in step 10:** a handler root built from input stays inside the declared
  directory but may still name another tenant (`tenant = "other"`): the app checks who may read which
  tenant. `Render.FS: os.DirFS(…)` follows symbolic links below its directory when a file is opened
  (S6 only skips links seen at `Create()`); `embed.FS` is the production recommendation.
  `//go:embed all:web` ships dotfiles in the binary (P4 names them). F1 is a net below a clean
  deployment, not a reason to declare a directory that holds source.
- **Information exposure:** refusals are the fixed 404 (S, F, G6, G8) so probing learns nothing; D4
  answers 500 because it is the app's bug and must be seen. Log lines carry the route name and a
  bounded, escaped path; never a file's content.
- **Limits:** 64 `ServeDirs` entries; logged names and targets cut at 256 bytes; lists in startup lines
  cut at 20 or 50 entries; multi-range requests are not served (M3).

## Impact

Behavior changes (README › Configuration › Behavior changes, step 10):

| Change | Who is affected | What to do |
|---|---|---|
| D1, D2: `SERVE_` needs `HTTP.ServeDirs`; `"."` and the working directory are refused | every app with a `SERVE_` route (README › Static files example, `internal/core` tests and benchmarks, `bench/`) | `HTTP: &owlkei.HTTPOptions{ServeDirs: []string{"public/assets"}}` |
| I2: `.html`, `.svg`, `.xml`, `.js` from `SERVE_` download instead of rendering | apps serving a built site through `SERVE_` | set `Content-Disposition: inline` in the handler (I3) |
| F1, S1: source, secret, backup, dump and oddly named files answer 404 on every static route; a traversal answers 404, not 400 | apps that offered such files | a `READ_` handler that writes the bytes |
| R1: `c.Redirect` to another site or with a relative path goes to `/` | OAuth and payment redirects | `c.RedirectExternal(url)`; start paths with `/` |
| C1, C2, C3: `private, no-cache` on pages, HTML errors, guarded files | CDN setups caching pages | set `Cache-Control` in the handler |
| G1: a guard on `RENDER_x` but not on its `SUBMIT_x` stops `Create()` | apps relying on P16 | bind the guard to both handlers, or to the controller |
| G4, G5: no guard on `public/` and shared assets; page files run their page's guards | a global auth guard no longer blocks `robots.txt` or shared CSS; a global `throttler` no longer counts them; a guard reading `c.Param()` sees none on page files | `LoadShed` for load; write guards that pass without route parameters, or check ownership in the handler |
| M2, M3: fixed content types, `application/octet-stream` for unknown extensions, no multi-range | clients using multi-range | single ranges |
| N2, N3: control characters in a path answer 400; middlewares see the canonical path | middlewares that compared raw paths | none |

Docs: `README.md` (Static files, Pages › Assets / Public files / Page CSS and JS / Status codes and
redirects, Guard, Configuration › HTTP and Behavior changes; the Security section is step 10),
`internal/core/README.md` (Static files, Pages), `internal/ctx/README.md` (redirects),
`internal/render/README.md`, `log/README.md`, `CLAUDE.md` (pipeline note on static routes, `ServeDirs`,
`invalid app:` and `invalid render:` lists), `.owlkei/request-pipeline.md`,
`.owlkei/anti-patterns-gotchas.md`. Design 0018 rules R40, P16 and P24 are superseded by G1 and G4;
design 0022 D1 is completed by D1 to D7. `examples/03-pages` keeps `ctx.Redirect("/bookings")` (R1
accepts it). CI gains the cross-OS job of the test plan.

## Test plan

Tests are internal-package tests named `Test<Subject>_<Behavior>`, one function per behavior. Static
tests build their trees in `t.TempDir()` and with `fstest.MapFS` / a test `embed.FS`.

### Step 8: N, H, L

| Rule | Test | Cases |
|---|---|---|
| N2 | `TestPath_ControlCharacterIs400` | `%00`, `%0a`, `%0d%0a`, `%7f`, `%09` in a static, parameter and unknown path; JSON and HTML by `Accept`; no middleware ran |
| N3 | `TestPath_CanonicalBeforeMiddlewares` | `//admin`, `/x/../admin`, `/./admin`, `/a//b/`, `/../..`: a global middleware and the handler read the canonical path; the caller's `URL` is unchanged; a canonical path is the same string (no copy) |
| N3 | `TestPath_WSPathAfterNormalization` | `/x/../ws` reaches the handshake |
| N4 | `TestPath_DecodedOnce` | `/files/%252e%252e/x`: `Param` holds `%2e%2e`; `/files/%2e%2e/x` is cleaned |
| N5 | `TestPath_CaseSensitive` | `/Admin` vs `/admin` → 404 |
| H1, H2 | `TestHeader_NoResponseSplitting` | a handler sets a value with `\r\nSet-Cookie: a=b`, a name with CR LF, a NUL; over HTTP/1.1 (raw connection read) and HTTP/2 (`httptest` TLS server): exactly one response, no injected header |
| L1, L2 | `TestLog_EscapesControlCharacters` | table × three formats × position (message, key, value, error text, nested map value): `\n`, `\r`, `\x00`, `\x1b[2J`, `\x7f`, U+0085, U+2028, U+2029, U+202E, U+2066, invalid UTF-8 `\xff`; asserts one record per write (Pretty: every later line starts with the tree prefix) and no raw forbidden byte sequence |
| L1 | `TestLog_ForgedRecord` | value `"x\n{\"level\":\"ERROR\"…}"` and `"x\nlevel=ERROR msg=…"` parse back as one record |

### Step 1b: the attack list

One test function per attack row, named `TestStaticAttack_<Row>`, each run against the five ways a file
is served: (a) `SERVE_` wildcard with a fixed root, (b) `SERVE_` wildcard with a root built from a path
parameter (`"uploads/" + tenant`), (c) `SERVE_` without a wildcard, (d) `public/`, (e) `/assets/*` and
`/assets/_pages/*`; (d) and (e) with an `embed.FS` using `all:`, and with `IsReload` from disk. Every
row asserts the status and that the body never holds the secret marker written into the target file.
OS column: `all` runs on Linux, macOS and Windows; an OS name means the file-system half of the test
is built only there (`_windows_test.go`, `_darwin_test.go`, `_unix_test.go`), while the name-policy
half runs everywhere.

| Row | Requests (examples) | Expect | OS |
|---|---|---|---|
| `EncodedTraversal` | `/s/%2e%2e/secret.txt`, `/s/..%2fsecret.txt`, `/s/%2e%2e%2f%2e%2e%2fetc/passwd`, `/s/a/../../secret.txt` | 404; cleaned by N3, never above the root | all |
| `DoubleEncodedTraversal` | `/s/%252e%252e/secret.txt`, `/s/%252e%252e%252fsecret.txt`, `/s/..%255c..%255csecret.txt` | 404; the literal name is looked up, nothing decoded twice | all |
| `Backslash` | `/s/..%5c..%5csecret.txt`, `/s/sub%5c.env`, `/s/sub%5csecret.go`, `/s/sub%5c..%5c.git%5cconfig` | 404 by S1.2 before any file call (on Windows these would open `.env` below `sub`) | all + windows |
| `DriveLetter` | `/s/C:/Windows/win.ini`, `/s/c:secret.txt`, `/s/x.txt::$DATA`, `/s/x.txt:stream`, `/s//server/share/x` | 404 by S1.2 / N3 | all + windows |
| `NUL` | `/s/a.txt%00.png`, `/s/%00`, tenant `a%00` | 400 by N2 | all |
| `TrailingDotSpace` | `/s/main.go.`, `/s/main.go%20`, `/s/.env.`, `/s/main.go....`, `/s/secret.key%20.` | 404 by S1.4 (on Windows they would open `main.go`) | all + windows |
| `MixedCase` | `/s/MAIN.GO`, `/s/Go.Mod`, `/s/.ENV`, `/s/Key.PEM`, `/s/APP.JS.MAP`, `/ASSETS/app.css`, `/s/.GIT/config` | 404 (F1 ignores ASCII case; hidden by the leading `.`; routes are case-sensitive) | all + darwin, windows (file created in lower case) |
| `ShortName` | `/s/GIT~1/config`, `/s/SECRET~1.GO`, `/s/ENV~1`, `/s/PROGRA~1/x` | 404 by S1.5 | all + windows (8.3 names enabled: skip when the volume has none) |
| `DeviceName` | `/s/CON`, `/s/nul.txt`, `/s/COM1`, `/s/aux.html`, `/s/LPT9.css` | 404 by S1.6; the request returns (no hang) | all + windows |
| `SymlinkOut` | a link in the root to a file and to a directory outside it; a link to a sibling tenant; a handler root that is a link out of the declared directory; a link below `web/` with `IsReload`; `Render.FS: os.DirFS` with a linked file | 404 (D5, S5, S6) | unix, darwin; windows when links can be created (else skip) |
| `SymlinkIn` | a link whose target stays inside the handler's root | 200 with the target's content | unix, darwin |
| `HiddenSegment` | `/s/.env`, `/s/.git/config`, `/s/css/.secret`, `/s/.well-known/security.txt` (200), `/s/.well-known/.env`, tenant `.git`, `/.env` from `public/`, `/assets/.DS_Store` | 404 except the `.well-known` file | all |
| `SourceAndSecrets` | `main.go`, `go.mod`, `go.sum`, `go.work`, `prod.env`, `server.pem`, `tls.key`, `id_rsa`, `store.p12`, `dump.sql`, `app.db`, each present in the served directory, `public/` and `assets/` | 404; at `Create()` they are in `StaticFilesSkipped` | all |
| `EditorBackup` | `index.html~`, `.index.html.swp`, `index.html.swp`, `app.js.bak`, `app.js.orig`, `#app.js#`, `config.old`, `x.tmp`, `x.rej` | 404 | all |
| `SourceMap` | `app.js.map` under `SERVE_`, `public/`, `/assets/` | 404; `/assets/app.js.map` is 200 only with `IsReload` | all |
| `DirectoryIndex` | a directory without `index.html`, with one, with a directory named `index.html`; `/assets/`, `/assets/_pages/`, `/assets/sub/` | 404 or the index; never a listing; `index.html` under `SERVE_` follows I2 | all |
| `RootOutsideServeDirs` | tenant `../../etc`, `..`, an absolute path, `a/../../b`, an undeclared sibling directory, a declared directory's prefix sibling (`uploads2` vs `uploads`) | 500 fixed body, `ServeRootRefused` logged once, no file opened | all |
| `Range` | `bytes=0-9`; `bytes=0-0,2-2`; 2,000 one-byte ranges; overlapping ranges; `bytes=-1`; a range past the end | 206 for one range; 200 full for several (M3); 416 past the end; response size never above the file size plus headers | all |
| `LargeFile` | a sparse 1 GiB file read by a client that stops reading; a HEAD on it | the handler goroutine ends at the server's write timeout; memory stays flat (no full read: `io.ReadSeeker` path); HEAD has no body | unix, darwin (sparse file; skipped with `-short`) |
| `MIMESniffing` | `x` (no extension) holding `<script>`, `x.unknown` holding HTML, `x.png` holding HTML, `x.txt` holding HTML | `application/octet-stream`, `image/png`, `text/plain`; `X-Content-Type-Options: nosniff` on each, with and without the `Headers` stage | all |
| `UploadedActiveFile` | `evil.html`, `evil.svg`, `evil.xml`, `evil.js`, `evil.xhtml`, `EVIL.HTM`, `evil.svgz` under a `SERVE_` uploads route | 200 with `Content-Disposition: attachment`, `Content-Security-Policy: sandbox; default-src 'none'`, `nosniff`; with the handler's `Content-Disposition: inline`: served inline, the stage's CSP kept | all |
| `EmbedAll` | an `embed.FS` built with `all:` holding `.env`, `.git/HEAD`, `assets/.secret`, `public/.htpasswd`, `_private/x` | none is reachable by any URL; `RenderHiddenFiles` names them | all |
| `PublicRouteSyntax` | `public/{id}.txt`, `public/*.txt`, `public/a/{x}/b.txt` | `Create()` panics with the P1 text | all (file names created with `fstest.MapFS`) |

The audit report (`docs/review/security-0012.md`) lists each row with its test name and result.

CI: a job `static` with `strategy.matrix.os: [ubuntu-latest, macos-latest, windows-latest]` running
`go test ./internal/core/... ./internal/render/... -run "TestStaticAttack|TestServeDirs|TestStaticName" -race`
(the full suite stays on Linux; open question 11).

### Step 1c: D, F, I, M, C2

| Rule | Test | Cases |
|---|---|---|
| S1 | `TestStaticName_Policy` | table of every S1 bullet, accepted neighbors (`a~b`, `.well-known/x`, `con-fig.txt`, `a b.txt`, `100%.png`), same result on every OS |
| F1, F2 | `TestStaticName_Forbidden` | every table row, upper and mixed case, near misses (`go.model`, `key.txt`, `mapfile`, `sql.txt`); 0 allocations |
| D1 | `TestNew_ServeDirsChecked` | missing directory, a file, `"."`, the parent, `/`, 65 entries, nested and duplicate entries accepted, relative and absolute, bound from `OK_HTTP__SERVE_DIRS` |
| D2 | `TestCreate_ServeNeedsServeDirs` | a `SERVE_` route with empty `ServeDirs` panics with the text; with an entry it does not; an async module's `SERVE_` route |
| D3, D4 | `TestServeDirs_RootRefused` | outside, sibling prefix, control character in the result; non-string and empty result → 404; `os.Chdir` after `New` does not move the roots |
| D5, D6, D7 | `TestServeDirs_SubRoot`, `TestServeDirs_NoWildcard` | root equal to and below the declared directory; hidden tenant; one-file handler inside and outside |
| M1, M2 | `TestStatic_NoSniffAlways`, `TestStatic_ContentTypes` | every static way, 200 / 206 / 304; the fixed table; a handler-set type kept |
| M3 | `TestStatic_MultiRangeIgnored` | as the `Range` row |
| I1-I4 | `TestServe_ActiveFileIsAttachment`, `TestServe_InlineOptIn`, `TestServe_AttachmentLoggedOnce` | every extension; type-based detection; `public/` and assets untouched; one log line under 50 concurrent requests |
| C2 | `TestServe_GuardedIsPrivate` | global, controller and route guard; no guard; handler value kept |

### Step 2: C1, C3

`TestPage_PrivateByDefault` (GET page, a rendering `SUBMIT_`, a handler-set `public, max-age=60`
kept), `TestPageError_PrivateByDefault` (template error page, built-in page, 404 by `Accept`, the
gate's 503 page), `TestAsset_GuardedPageIsPrivate` (with and without `?v=`), `TestAsset_SharedStaysPublic`.

### Step 3: R

`TestRedirect_SameOrigin` (every accepted and refused example of R1, on a page and an API route;
refused → `Location: /`, same status, one `RedirectRefused` line, target escaped and cut at 256),
`TestRedirect_External` (R2 table), `TestRedirect_Status` (R3: 303 page, 301 API, `c.Status(302)`),
`TestRedirect_InjectedFunc` (the `owlkei.Redirect` parameter follows R1), `TestRedirect_NoHeaderInjection`
(CR LF target over a raw HTTP/1.1 connection), `BenchmarkRedirect_SameOrigin` (0 allocations added).

### Step 5: G1-G3

`TestSubmit_GuardOnRenderOnlyPanics` (exact text), `TestSubmit_GuardOnBothOK`,
`TestSubmit_GuardOnControllerOK`, `TestSubmit_GlobalGuardOK`, `TestSubmit_GuardOnSubmitOnlyOK` (GET open),
`TestSubmit_RenderWithoutSubmitOK`, `TestSubmit_InterceptorOnRenderOnlyStaysGET` (G2: no panic, POST
does not run it). `TestSubmit_GuardBoundToRenderOnly` of design 0018 is replaced.

### Step 7: G4-G9, P

`TestPublic_NoGuardRuns`, `TestAsset_SharedNoGuardRuns` (a refusing global guard; global middleware
still counted), `TestAsset_PageRunsPageGuards` (global, controller-wide, route-bound; order),
`TestAsset_PageGuardRefusalIs404` (`false`, a 401 exception; a non-exception panic → 500),
`TestAsset_LayoutAndErrorFilesOpen`, `TestAsset_PageGuardSeesNoParams`, `TestAsset_PageBehindGate`
(pending 503 with `Retry-After`, failed 503, ready), `TestAsset_PageReloadOwner`,
`TestServe_GuardStillRuns`, `TestCreate_PublicRouteSyntaxPanics` (P1), `TestCreate_StaticFilesSkipped`
(P2, P3: content and cap of 20), `TestCreate_HiddenFilesLogged` (P4: with `all:`, none, `IsReload`
silent, `.well-known` excluded), `TestCreate_PublicFilesLogged` (P5: cap of 50). The 0018 tests
`TestAsset_GlobalLayersApply` and `TestPublic_GlobalLayersApply` keep their middleware half.

### Step 9: X

No Go test. Checked by: `make check` passes on the tree; a scratch file with
`db.Query("SELECT … " + name)` and one with `tls.Config{InsecureSkipVerify: true}` make
`golangci-lint run` fail (run by hand in the step, not committed); every baseline entry has a comment.

## Risks

| Risk | Mitigation |
|---|---|
| D2 and I2 break existing apps at upgrade (every `SERVE_` app must declare a directory; built sites download) | The `Create()` panic and the `ServeAttachment` line say what to write; Behavior changes table; examples and benchmarks updated in the same step |
| F1 blocks a legitimate file (a tutorial site offering `.go` files, a `.db` download) | Fixed 404 plus `StaticFilesSkipped` at `Create()` for table-built routes; documented escape: a `READ_` handler. No option on purpose |
| S1.5 and S1.6 refuse rare legal names on Linux (`report~1.pdf`, `con.txt`) | Accepted for one answer on every OS; listed in the docs |
| G5: a guard that authorizes by route parameter refuses its page's CSS and JS | Documented with the guard docs; open question 7 |
| R4 lets a client write `RedirectRefused` lines at will through an open `next` parameter | Bounded line (256 bytes), the log pipeline already drops under pressure; `LoadShed` |
| Windows and macOS runners make CI slower and can flake on links and 8.3 names | The matrix runs the static tests only; tests skip when the volume cannot create the fixture and say so |
| N3 changes what middlewares see | It removes a bypass (`//admin` against a prefix check); listed in Behavior changes |
| `gosec` noise on later work (`G115`, `G304`) | Baseline by file with reasons; `G104` off; rules reviewed when the storage roadmap adds SQL |

## Open questions

1. **`c.Redirect` same-origin on every route, or on page routes only (the roadmap's wording)?**
   Recommended: every route. One rule, and `c.Redirect(query.Get("next"))` in an API handler is the
   same hole. Page-only is one condition in R1 (`c.isPage`).
2. **A refused redirect: go to `/` with a `Warn` (R4), or answer 500?** Recommended: `/`. The visitor
   who followed a hostile link stays on a working site; the developer sees the line.
3. **Inline opt-in for active files: the handler's `Content-Disposition` header (I3, no API), or a
   controller call `instance.Inline(instance.SERVE_docs_ANY)`?** Recommended: the header. It follows the
   `Cache-Control` rule and adds nothing to `api/owlkei.txt`; `ServeAttachment` teaches it.
4. **A `SERVE_` route with empty `ServeDirs`: `Create()` panics (D2), or the route answers 500?**
   Recommended: the panic (no silent error; seen at startup, not by a user).
5. **Source maps: only with `Render.IsReload` (F3), or an option `HTTP.IsSourceMaps`?** Recommended: no
   option now; error trackers take uploaded maps. Add the option when an app needs public maps.
6. **G1 (decided 2026-11-21, owner): explicit binding checked at `Create()`**, guards only; no layer moves to a `SUBMIT_` on its own.
7. **Page files run every guard of their page, parameter-reading guards included (G5, G6)?**
   Recommended: yes, as decided in the roadmap, documented. Alternative: only global and
   controller-wide guards run, route-bound ones do not.
8. **`gosec` inside `golangci-lint` with the baseline in `.golangci.yml` (X1, X3), or the standalone
   tool with `#nosec` comments?** Recommended: `golangci-lint`. One tool in CI, hooks and `make check`,
   and no comment in Go code.
9. **A non-canonical path: rewrite in place (N3), redirect 308 to the canonical URL, or 400?**
   Recommended: rewrite. No extra round trip, safe for POST, and every layer sees one path.
10. **Refuse the working directory (and above) in `ServeDirs` (D1)?** Recommended: yes. It holds the
    source, `.env` and the binary; F1 is only a net. Today's documented `return "."` stops working.
11. **Add macOS and Windows CI runners for the static tests?** Recommended: yes, those tests only. Step
    1b asks for the three systems, and the Windows-only shapes (`\`, short names, trailing dots) are
    the ones Linux cannot prove on a real file system.
12. **Scan response header values for CR, LF and NUL in the framework (a per-response cost), or rely on
    `net/http` and pin it with tests (H2)?** Recommended: rely and pin. Splitting is already
    impossible; a scan buys only an earlier error.
