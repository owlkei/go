# 0012: Security

Status: Draft (paused 2026-11-19; runs after roadmap 0013, App options, which took the steps that change the configuration API)

## Goal
With the configuration API final (roadmap 0013), the framework is audited and hardened where the API
doesn't change: personal pages aren't cached by shared caches, redirects stay on the site, a guard on a
page also guards its form, static files can't widen routes or leak, and every sink (HTML, headers,
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
- Moved to roadmap 0013 (2026-11-19): the built-in stages and their fixed order (former step 6), the
  SSR defaults (former step 4: all stages off, a `SecurityCheck` warning instead), shared `Origins` and
  the WebSocket Origin check. Steps keep their numbers; 4 and 6 are gone.
- Open, for the owner before the design:
  3. Static files and guards (step 7): no guard for `public/` and the shared `/assets/*`, the page's own
     guards for `/assets/_pages/<page>/…` (backlog proposal); or keep today's rule (global guards run for
     every static route). Recommended: the proposal.
- Commits follow `.claude/commands/commit.md`: no Claude attribution or trailers.

## Log
