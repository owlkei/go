# Security review 0012, step 1: general audit and review of 73e9bbd

Scope: the review of `git diff 7cb0634..73e9bbd` (design `docs/design/0021-security.md`) and the general
audit of roadmap 0012 step 1: REST and pages (the 0005 method), every sink (HTML and `template.HTML`,
headers and redirects with CR/LF, logs, paths of `assets/`, `public/`, `SERVE_` and uploads, template
names, JSON), the pre-route stages of roadmap 0013 (`internal/middlewares/*`, `AppOptions`,
`OptionsFromEnv`), and the WebSocket handshake. Static file serving was attacked by hand on macOS
(APFS, case-insensitive) with throwaway tests passed through `go test -overlay`. No test file was
added to the tree.

**Summary:** 0 critical, 1 high, 1 medium, 9 low. govulncheck: no vulnerabilities. gosec: 53 issues
outside tests, all in the reasoned baseline; the new static code has none. The design is implemented
rule by rule. Its one gap is the missing benchmarks (L9).

## Vulnerabilities

| Scanner / source | Result |
|---|---|
| `govulncheck ./...` (v1.9.0, DB 2026-11-11) | No vulnerabilities, reachable or not (go1.27.4 standard library, `golang.org/x/net` v0.62.0). |
| Advisories newer than the DB (pkg.go.dev/vuln, 2026-11-21) | Nothing for `golang.org/x/net` after GO-2026-6860 / GO-2026-6866 (2026-10-27, fixed in v0.62.0). GO-2026-6859 (`net/http`: a `Connection: close` race that can crash HTTP/1 servers using `MaxBytesReader`, which `App.ServeHTTP` uses) is fixed in go1.27.4. |
| `go list -m -u all` | `golang.org/x/net` v0.63.0 (published 2026-11-17, no advisory). `x/crypto`, `x/sys`, `x/term` and `x/text` have newer versions, but they are outside the build graph. Nothing to upgrade for security. |
| npm dev tooling (husky, lint-staged) | Not audited: `yarn` is not installed here. Low: these tools never ship. |

## Toolchain

The local Go version is go1.27.4. CI reads `go 1.27.4` from `go.mod` (`go-version-file`, no `toolchain`
line). The latest release on `go.dev/dl` is go1.27.4. Nothing to do.

## Tools on the tree

- `golangci-lint run ./...` (the repository config, gosec on): 0 issues. `go vet ./...`: clean.
- gosec without the baseline (a scratch config with `G104` off and tests excluded) reports 53 issues.
  Each one matches a commented baseline entry: G115 in `internal/num`, `memorycache`, `tracecontext/id.go`,
  `logpipe/sink.go`, `throttler`, `byte_size.go`, `loadshed/adaptive.go`, `access_log.go` and
  `devtool_builder.go`; G304 in `dotenv`, `apicheck` and the `loadshed` cgroup reader; G602 in
  `access_log.go` and `render.go`; G203 in `render/parse.go`, where the tags are built from
  `url.PathEscape` and `HTMLEscapeString`, which I checked; G103 in `log_tag.go`; G404 in `retry.go`;
  and G301 / G306 in `apicheck`. The design allowed G304 on the `os.Root` calls, but
  `http_static.go` raised no finding at all.
- `go test -race` on `internal/core`, `ctx`, `render`, `common`, `log` and `internal/middlewares/*`:
  pass (darwin/amd64).

## Findings

### H1, high: Unicode case folding defeats the forbidden-file table on case-insensitive volumes

`internal/core/http_static.go:126-151` (`isForbiddenFile`, `hasSuffixFold`) and `:89-105`
(`isServableSegment`).

F1 compares names with `strings.EqualFold` over a fixed byte length, so it only folds ASCII. APFS
(macOS) and, very likely, NTFS (Windows: its upcase table maps U+017F to `S`) open a file whose name
differs only by a code point that folds to an ASCII letter. Examples are U+212A KELVIN SIGN (folds to
`k`) and U+017F LATIN SMALL LETTER LONG S (folds to `s`). The name passes S1 and F1 because the
suffix is no longer `.key`. `os.Root` then opens the real file.

The exploit, reproduced on this machine (APFS), uses a declared `ServeDirs` directory that holds
`server.key`, `dump.sql` and `app.swp`:

```
GET /s/server.key             -> 404 (F1)
GET /s/server.%E2%84%AAey     -> 200 "SECRET-KEY"   (U+212A)
GET /s/SERVER.%E2%84%AAEY     -> 200 "SECRET-KEY"
GET /s/dump.%C5%BFql          -> 200 "SECRET-SQL"   (U+017F)
GET /s/app.%C5%BFwp           -> 200 "SECRET-SWP"
```

The forbidden kinds within reach are those whose names contain a `k` or an `s`: `.key`, `.bak`,
`.sql`, `.sqlite*`, `.swp`, `.swo`, `go.work`, and possibly `go.sum`, `id_rsa`, `id_dsa` and
`id_ecdsa`. In this run, `go.%C5%BFum` and `id_r%C5%BFa` answered 404, so the folding is not uniform.
Windows was not run.

Which routes are exposed:
- every `SERVE_` route on a macOS or Windows host;
- `/assets/*` with `IsReload` from disk, on development machines only.

`public/` and `/assets/*` built from `embed.FS` are not exposed: they are exact table lookups.

This breaks the roadmap's "Done when" for steps 1b and 1c: "no file … of a forbidden kind is ever
served". It also breaks the design's statement that one byte policy "gives the same answer on every
OS".

**Fix:** add a new rule S1.8, following the design's "Room for audit findings": a segment that holds
a code point whose case folding is ASCII is refused. The list is U+017F (`C5 BF`), U+212A (`E2 84 AA`),
U+0130 (`C4 B0`) and U+0131 (`C4 B1`). It is a byte scan with no allocation. Add a test,
`TestStaticAttack_UnicodeFold`: the name-policy half runs everywhere, and the file-system half runs on
darwin and windows, using the requests above against every way a file is served. A stricter
alternative is to refuse any non-ASCII byte in the last segment of a name that F1 would refuse once
folded with `unicode.SimpleFold`. It costs more and gains little.

### M1, medium: open redirect in the directory slash redirect of `SERVE_` routes

`internal/core/http_static.go:391-393` and `:416-422` (`redirectToSlash`).

When a `SERVE_` route reaches a directory that holds `index.html` and the URL lacks its trailing
slash, the framework answers `301 Location: <path.Base(URL path)>/`. This is a relative reference built
from the request path. Only the file name below the root is checked by S1, never the URL segment
that goes into `Location`. For a route without a wildcard, that segment can be a route parameter.

Reproduced with `SERVE_site_BY_lang` returning a fixed directory:

```
GET /site/%5C%5Cevil.test  -> 301 Location: \\evil.test/
```

Browsers read `\` as `/` in http(s) URLs, so `\\evil.test/` resolves to `//evil.test/`, another
host. The response is a 301, which browsers and shared caches keep. The same happens for a wildcard
route whose handler maps a parameter to a directory without using its text (`/files/{tenant}/*`,
`/files/%5C%5Cevil.test`, when the handler looks the tenant up). This `Location` sink is missing from
the H1 list of the design.

**Fix:** add a new rule H3: build the target as the absolute canonical path,
`ex.URL.Path + "/"` (N3 has already made it start with a single `/`), plus the query. Refuse with the
fixed 404 when the path holds `\` (or reuse `ctx.isLocalTarget`). Add a test,
`TestServe_SlashRedirectSameOrigin`, covering `%5C%5Cevil.test`, `%5Cevil.test`, `%2F%2Fevil.test` and
`@evil.test` on a parameter route.

### Low

- **L1** Windows device names are incomplete (`http_static.go:60`, `:107-124`). `CONIN$`,
  `CONOUT$`, `COM¹`-`COM³` and `LPT¹`-`LPT³` (superscript digits, which Windows and Go's
  `filepath.IsLocal` treat as devices) pass S1.6. On Windows, opening `CONIN$` from a service fails
  or blocks rather than leaking. **Fix:** extend S1.6, with test rows in `TestStaticName_Policy`.
- **L2** The F1 table leaves common secret kinds open. Examples: `*.tfstate` / `*.tfstate.backup`,
  `*.kdbx`, `*.jks` / `*.keystore`, `*.log`, `credentials.json`, `service-account*.json`, `*.ppk`, and
  backup archives (`backup.zip`, `site.tar.gz`). A `SERVE_` directory that holds them serves them.
  **Fix:** this is an owner decision because it widens F1 (design 0021 F2: no option). At minimum,
  add `.tfstate`, `.kdbx`, `.jks`, `.keystore` and `.ppk`, and document the others in step 10.
- **L3** The G1 check matches guards by type, not by instance (`internal/common/fn.go:288`, `:304`,
  `:314`). `BindGuard(Role{Name: "admin"}, RENDER_x)` together with
  `BindGuard(Role{Name: "user"}, SUBMIT_x)` passes, and the POST is then weaker than the page. G1 also
  covers only `SUBMIT_x`: a `CREATE_` route given the page's path with `instance.Path` takes the
  form's POST unguarded. **Fix:** document both in step 10. Optionally compare guard values with
  `reflect.DeepEqual` when the type is comparable.
- **L4** An unguarded `SERVE_` route answers `no-cache`, not `private, no-cache`
  (`http_static.go:407-412`), even when the app protects it with a middleware or inside the handler.
  `no-cache` lets a shared cache store the file. A CDN set to ignore `no-cache`, or to serve stale on
  error, can then hand a private file to another user. **Fix:** document in step 10 that authorization
  must be a guard (C2), or that the handler sets `Cache-Control: private`.
- **L5** `HTTPSRedirect` copies the request's `Host` into a cacheable 308 when neither `Hosts` nor
  `HTTPSRedirect.Host` is set (`internal/middlewares/httpsredirect/httpsredirect.go:71-77`). A cache
  that does not key on `Host` can be poisoned. The value is checked by `IsValidHost`, so CR/LF cannot
  get in. **Fix:** a `SecurityCheck` warning when `HTTPSRedirect` is on without `Hosts` or `Host`, or
  the production checklist (step 10).
- **L6** The CSRF branch for forwarded hosts ignores the scheme (`internal/middlewares/csrf/csrf.go:83`,
  `:110-124`). With a forwarded host and no `Sec-Fetch-Site` header, `Origin: http://app.test` is
  accepted for `https://app.test`. Browsers that omit `Sec-Fetch-Site` are old, and the attacker also
  needs a page served over plain HTTP on the same host. **Fix:** compare against `ClientScheme` as
  well.
- **L7** `c.JSONP` makes any JSON route readable from any site with the visitor's cookies
  (`internal/ctx/http_context.go:164`). This is how JSONP works; the callback is sanitized and the
  response carries `nosniff`. **Fix:** a warning in step 10: never use it on authenticated data.
- **L8** WebSocket message size is the `x/net/websocket` default of 32 MiB per frame. This is out of
  scope (the WebSocket roadmap) and is noted for it. The handshake itself is correct: stages, then the
  Origin check (same origin or `Origins`), then global middlewares, all after N1-N3.
- **L9** (review, not security) The benchmarks named in design 0021 › Performance are missing:
  `BenchmarkServe_File`, `BenchmarkAsset_PageGuarded` and `BenchmarkPublic_File` (no definition
  found). Without the "run before the change too" numbers, step 11 has no baseline for them. **Fix:**
  add them in step 11 and measure against `7cb0634`.

## Design conformance (0021)

These rules were checked in the code and their named tests exist and pass:

- N1-N5: `checkPath` / `withCleanPath` run before the WebSocket test and the stages, and the clone
  leaves the caller's URL untouched.
- S1-S6: name policy, hidden names, symbolic links skipped at `Create()`, `os.OpenRoot` with
  `IsReload`.
- F1-F3: `.map` only under `/assets/` with `IsReload`.
- D1-D7: `workDir` read once, byte prefix on a separator, the longest match wins, the root opened
  through `os.Root` and then `OpenRoot(sub)`, a 500 with one log line for a refused root.
- M1-M3, I1-I4 (one log line per route through `CompareAndSwap`).
- C1-C4, R1-R5, G1-G9 (the gate's 503 for page files, refusal as the fixed 404, a non-exception panic
  as 500), P1-P5, H2, L1-L2, X1-X5.

`api/owlkei.txt` gained exactly `RedirectExternal`.

What I checked in redirects: `isLocalTarget` refuses `//`, `/\`, `\`, control characters and
relative targets. `http.Redirect` runs `path.Clean` on what is accepted, so `/.//evil` becomes
`/evil`. `RedirectExternal` refuses user info, an empty host, and schemes other than http(s). No
bypass was found.

## Static attack rows (design 0021 test plan)

All the tests below are in `internal/core/http_static_test.go` and passed locally (darwin, `-race`).
CI runs them on ubuntu, macos and windows in the `static` job. The two rows added by this audit come
from my overlay probes and are not yet tests.

| Row | Test | Result |
|---|---|---|
| EncodedTraversal | `TestStaticAttack_EncodedTraversal` | pass |
| DoubleEncodedTraversal | `TestStaticAttack_DoubleEncodedTraversal` | pass |
| Backslash | `TestStaticAttack_Backslash` | pass |
| DriveLetter | `TestStaticAttack_DriveLetter` | pass |
| NUL | `TestStaticAttack_NUL` | pass |
| TrailingDotSpace | `TestStaticAttack_TrailingDotSpace` | pass |
| MixedCase | `TestStaticAttack_MixedCase` | pass (ASCII only, see H1) |
| ShortName | `TestStaticAttack_ShortName` | pass |
| DeviceName | `TestStaticAttack_DeviceName` | pass (incomplete list, see L1) |
| SymlinkOut | `TestStaticAttack_SymlinkOut` | pass |
| SymlinkIn | `TestStaticAttack_SymlinkIn` | pass |
| HiddenSegment | `TestStaticAttack_HiddenSegment` | pass |
| SourceAndSecrets | `TestStaticAttack_SourceAndSecrets` | pass |
| EditorBackup | `TestStaticAttack_EditorBackup` | pass |
| SourceMap | `TestStaticAttack_SourceMap` | pass |
| DirectoryIndex | `TestStaticAttack_DirectoryIndex` | pass (redirect target, see M1) |
| RootOutsideServeDirs | `TestStaticAttack_RootOutsideServeDirs` | pass |
| Range | `TestStaticAttack_Range` | pass |
| LargeFile | `TestStaticAttack_LargeFile` | pass |
| MIMESniffing | `TestStaticAttack_MIMESniffing` | pass |
| UploadedActiveFile | `TestStaticAttack_UploadedActiveFile` | pass |
| EmbedAll | `TestStaticAttack_EmbedAll` | pass |
| PublicRouteSyntax | `TestStaticAttack_PublicRouteSyntax` | pass |
| UnicodeFold (new) | none yet (proposed `TestStaticAttack_UnicodeFold`) | **fails** on APFS (H1) |
| SlashRedirect (new) | none yet (proposed `TestServe_SlashRedirectSameOrigin`) | **fails** (M1) |

## Checked with no finding

- **Static, other attacks tried:**
  - zero-width characters (`server.ke%E2%80%8Cy`: APFS does not ignore them, 404);
  - `é` and NFD forms (no ASCII fold);
  - `.WELL-KNOWN` (refused);
  - a tenant `../../etc`, a prefix sibling (`up2`), and an absolute root (500, nothing opened);
  - case on the declared directory (a byte compare, so it fails closed);
  - `%2F` inside segments (cleaned by N3, `RawPath` cleared);
  - multiple `Range` header lines (removed);
  - `evil.ſvg` (octet-stream with `nosniff`, downloaded);
  - a handler-set `Content-Type` on an active extension (still an attachment);
  - `index.html` under `SERVE_` (I2).

  Page files: the guard runs before any file is opened, the owner is looked up per template (one
  `RENDER_` per template), and the asset route has no global guard and no bypass through `/assets/`
  (other FS) or `public/` (exact routes).
- **HTML and templates:** template names come from routes at `Create()`, never from a request. The
  error page is chosen by status code. The only `template.HTML` the framework builds is `pageTags`
  (escaped). `template.HTML` in user data disables escaping, which is step 10's documentation item.
- **Headers:** `Content-Disposition` and CSP are constants. `Location` is safe on the R1/R2 paths but
  not in `redirectToSlash` (M1). CR/LF in handler values is pinned by `TestHeader_NoResponseSplitting`
  on HTTP/1.1 and HTTP/2. Request IDs and `traceparent` are validated.
- **Logs:** JSON escapes C0, `0x7f`, C1, U+2028/2029 and the bidi controls named by L1. Text relies
  on slog quoting, plus `0x7f`. Pretty uses `appendEscaped`. `RedirectRefused` and `ServeRootRefused`
  cut at 256 bytes, then escape.
- **JSON:** error bodies are fixed. The `c.JSON` format-string injection is known (0005 L7).
- **Pre-route stages (0013):**
  - `origin.Set`: lower-case entries only, bounded, wildcard part limited to `[a-z0-9.-]`.
  - CSRF: `CrossOriginProtection`, with `Origins "*"` refused at `New`.
  - `httpsredirect`: escaped path and query.
  - `hostcheck`, `proxy` (0005 M2 fix in place), `helmet` (the I2 CSP replaces the stage's CSP per
    response), `cors` (`*` with credentials refused), `compress`, `loadshed`.
  - `OptionsFromEnv`: secret fields masked in errors and in `AppOption` lines, unknown variables named
    without their value, `.env` from the working directory only.
- **WebSocket handshake:** the path is checked after N2/N3. The Origin check always runs when
  `WS` is on, and `x/net/websocket`'s own origin check is replaced by it on purpose (custom
  `Handshake`).

## Mapping to roadmap steps

| Finding | Goes to |
|---|---|
| H1 | Fix now, as rule S1.8 in design 0021 (S family) with `TestStaticAttack_UnicodeFold` (steps 1b/1c are reopened) |
| M1 | Fix now, as rule H3 in design 0021 (H family) with `TestServe_SlashRedirectSameOrigin` (step 8) |
| L1 | S1.6 extension, step 1c follow-up |
| L2 | Owner decision on F1, then step 1c or step 10 docs |
| L3, L4, L7 | Step 10 (docs) |
| L5 | Step 10 (production checklist) or backlog (`SecurityCheck`) |
| L6 | Backlog (csrf) |
| L8 | WebSocket roadmap |
| L9 | Step 11 |

Sources: [Go downloads (JSON)](https://go.dev/dl/?mode=json),
[Go vulnerability list](https://pkg.go.dev/vuln/list), [GO-2026-6859](https://pkg.go.dev/vuln/GO-2026-6859),
[x/net versions](https://pkg.go.dev/golang.org/x/net?tab=versions).
