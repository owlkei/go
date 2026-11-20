# 0012: Security

Status: Draft (paused 2026-11-19; runs after roadmap 0013, App options, which took the steps that change the configuration API)

## Goal
With the configuration API final (roadmap 0013), the framework is audited and hardened where the API
doesn't change: personal pages aren't cached by shared caches, redirects stay on the site, a guard on a
page also guards its form, static files can't widen routes or leak source and secrets (a deep audit), and every sink (HTML, headers,
redirects, logs, paths, JSON) is reviewed. The REST side is audited again after the API review and the
SSR changes.

## Scope
- In: `internal/core` (pages, redirects, static routes, the middleware chain), `internal/ctx` (redirect,
  headers), `internal/render`, the built-in middlewares' ordering, `log` (control characters), CI
  (`gosec`), README and `.owlky/` (the security rules users must know), backlog › "Security roadmap"
  (every finding listed there).
- Out: sessions and authentication (their own roadmap: cookies, login, brute force); the storage
  roadmap's SQL rule (parameters only, decided); WebSocket hardening (the WebSocket roadmap); a
  pre-rendered page for middleware refusals under load (noted, later).

## Steps
| # | Step | Path | Design | Depends on | Done when | State |
|---|---|---|---|---|---|---|
| 1 | Audit: `go-security` over REST and SSR (the 0005 method), plus every sink of the injection review (HTML, `template.HTML` in data, headers and redirects with CR/LF, logs, paths of `assets/`, `public/` and uploads, template names, JSON); findings ranked | light | - | - | `docs/review/security-0012.md`; each finding mapped to a step below or to the backlog | todo |
| 1b | Static files deep audit (owner, 2026-11-19: serving files is where source and secrets leak): every way a file is served (`SERVE_…` with a handler-chosen root, `public/`, `/assets/*`, `/assets/_pages/…`, `embed.FS` with `all:`, `IsReload` from disk) against the list in Decisions › Static files; one test per attack (encoded and double-encoded traversal, `..\` and drive letters, NUL bytes, trailing dots and spaces, mixed case, Windows short names, symlinks in and out, hidden segments, `.git` / `.env` / `*.go` / `go.mod` / editor swap and backup files, source maps, directory index, `Range` and multi-range, large files and timeouts, MIME sniffing, uploaded `.html` / `.svg`), on Linux, macOS and Windows | tdd | docs/design/0021-security.md | 1 | the audit's report lists each attack with its test; no file outside the declared roots or of a forbidden kind is ever served | todo |
| 1c | Static files fixes: (a) the directories a `SERVE_` handler may return are declared in `AppOptions`; a root outside them answers 500 and is logged, never served (a tenant name such as `../../etc` can't move the root); (b) a guarded `SERVE_` route defaults to `Cache-Control: private, no-cache`; (c) `X-Content-Type-Options: nosniff` on every static response, whatever the headers stage says; (d) files that can run in a browser (`.html`, `.svg`, `.xml`, `.js` outside `assets/`) served by `SERVE_` go out as `Content-Disposition: attachment` with a CSP `sandbox` unless the route opts in; (e) source and secret files (`*.go`, `go.mod`, `go.sum`, `.env*`, `*.pem`, `*.key`, `*.map` unless enabled, editor and backup files such as `*~`, `*.swp`, `*.bak`, `*.orig`) are refused with 404 whatever the directory; (f) the startup log lists the public URLs (`public/`) | tdd | docs/design/0021-security.md | 1b | every finding of 1b of high or critical rank is fixed with its test | todo |
| 2 | Private caching: pages and HTML error pages answer `Cache-Control: private, no-cache` unless the handler sets its own | tdd | docs/design/0021-security.md | 1 | a personal page can't be stored by a shared cache by default | todo |
| 3 | Same-origin redirects: on page routes `c.Redirect` accepts only same-origin paths (no scheme, no `//host`, no CR/LF); another host needs an explicit call | tdd | docs/design/0021-security.md | 1 | `c.Redirect(form.Next)` can't send a user to another site | todo |
| 5 | A layer bound to a `RENDER_` also covers its `SUBMIT_` (the POST on the same URL) | tdd | docs/design/0021-security.md | 1 | `BindGuard(auth, c.RENDER_account)` also guards `POST /account` | todo |
| 7 | Static files: a `public/` name with `{` or `*` stops `Create()`; hidden files in `Dir` are logged; the static-route guard rule (Decisions › open) | tdd | docs/design/0021-security.md | 1 | a file name can't widen a route; the guard rule holds for `public/`, `/assets/*` and page assets | todo |
| 8 | Sink fixes found by step 1 that need code (header values with CR/LF, log control characters in every format, path normalization before matching) | tdd | docs/design/0021-security.md | 1 | every step-1 finding of high or critical rank is fixed with a test | todo |
| 9 | CI: `gosec` with the rules that matter for a framework (G201/G202 SQL strings, G1xx, G3xx, G4xx), baseline of accepted findings with reasons | light | - | 1 | CI fails on a new `gosec` finding | todo |
| 10 | Docs: a "Security" section in the README (what the defaults do, `template.HTML` disables escaping, `Bind` fills only `bind` fields, `public/` is served as is, `//go:embed all:web` ships dotfiles, the error format rule, redirects); `.owlky/`; the examples where a default changes | light | - | 2-9 | every default and every rule users must follow is documented | todo |
| 11 | Measure: `make bench` and the `bench/` scenarios against `docs/review/http-0009.md` | light | - | 2-8 | no scenario slower than the noise band; `docs/review/http-0012.md` | todo |

Path: `tdd` (tester writes failing tests, builder makes them pass) or `light` (one agent or the main
session writes tests and code together, for small steps). State: `todo`, `doing`, `done`, `blocked`.

## Decisions
- Runs after roadmap 0013 (App options), before the storage roadmap (owner, 2026-11-19).
- Error format (decided 2026-11-19): once a route matched, the route decides (page HTML, API JSON);
  before routing and without a route, `Accept` decides. Already the behavior; step 10 documents it.
- No input-sanitizing middleware: escaping belongs at the sink.
- **Static files (owner, 2026-11-19): audited deeply, leaks of source and secrets are the worst case.**
  Already in place (0005 found nothing): `os.OpenRoot` blocks traversal and symlinks leaving the root,
  hidden segments answer 404 (except `.well-known`), no directory listing. Not reviewed since: a root
  that the handler builds from user input (`"uploads/" + tenant`: `os.OpenRoot` only confines below the
  root it is given), uploaded `.html` / `.svg` served inline (stored XSS), guarded files cached by shared
  caches, MIME sniffing with the headers stage off, multi-range and large files, odd names per OS,
  `public/` names with `{` / `*` (step 7), the SSR asset and public routes (0008), `//go:embed all:`
  shipping dotfiles. Steps 1b and 1c.
- Moved to roadmap 0013 (2026-11-19): the built-in stages and their fixed order (former step 6), the
  SSR defaults (former step 4: all stages off, a `SecurityCheck` warning instead), shared `Origins` and
  the WebSocket Origin check. Steps keep their numbers; 4 and 6 are gone.
- Open, for the owner before the design:
  3. Static files and guards (step 7): no guard for `public/` and the shared `/assets/*`, the page's own
     guards for `/assets/_pages/<page>/…` (backlog proposal); or keep today's rule (global guards run for
     every static route). Recommended: the proposal.
- Commits follow `.claude/commands/commit.md`: no Claude attribution or trailers.

## Log
