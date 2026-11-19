# 0013: App options

Status: Approved

## Goal
An app's configuration lives in one place: `owlky.New(&owlky.AppOptions{…})`, or the same struct filled
from `OK_*` environment variables and `.env`. Code is bound with the `Bind…` methods; running takes no
configuration (`app.Listen()`). The eight built-in middlewares become framework stages that run in a
fixed order, configured in `AppOptions` and off by default. One `Origins` list serves CORS, CSRF and
the WebSocket handshake, and a startup warning names a missing protection. This roadmap produces the
final configuration API; the security roadmap (0012, after this one) audits and fixes what doesn't
change it.

## Scope
- In: `internal/core` (`New`, the app's configuration, the server, the pre-routing chain), the root
  package (`AppOptions` and its groups, `ByteSize`, `OptionsFromEnv`), `middlewares/*` moved to
  `internal/`, `internal/trace` (`StagePreRoute`), `modules/config` (its `.env` parser and typed binding
  moved to an internal package both use), every caller (tests, `examples/`, `bench/`), README,
  `.owlky/`, CLAUDE.md, `api/owlky.txt`.
- Out: the security roadmap's work (0012: the audit, sink fixes, page caching, redirects, `RENDER_` /
  `SUBMIT_` guard coverage, static files, `gosec`); WebSocket limits (`MaxConnectionsPerIP`, message
  sizes: the WebSocket roadmap); accurate trace timings (backlog, its own work); the `OK_ENV` mode
  switch (undecided); gRPC.

## Steps
| # | Step | Path | Design | Depends on | Done when | State |
|---|---|---|---|---|---|---|
| 1 | `AppOptions` and `New(opts *AppOptions)` (`nil` = today's defaults): `Logger`, `StartupTimeout`, `AccessLog`, `Devtool`, `Render`, `Versioning`, `WS` move in; `SetLogger`, `SetStartupTimeout`, `EnableAccessLog`, `EnableDevtool`, `EnableRender`, `EnableVersioning`, `EnableWS` are removed; every option is checked at `New` (conflicts panic `invalid app:`) | tdd | docs/design/0022-app-options.md | - | the old configuration methods are gone; every feature works through `AppOptions` | todo |
| 2 | `ByteSize` (plain bytes, `KB`/`MB`/`GB` = 1000ⁿ, `KiB`/`MiB`/`GiB` = 1024ⁿ, constants for code) | tdd | docs/design/0022-app-options.md | - | sizes parse and print both ways; `b` is refused | todo |
| 3 | Server and HTTP groups: `Server{Port, ShutdownTimeout, TLS{CertFile, KeyFile, Config}, IsH2C}`; `Listen()` without arguments, `Serve(listener)`; `ListenTLS` removed; `HTTP.MaxBodyBytes` (`ByteSize`) applies to every request, also when `App` runs in the user's own `http.Server` | tdd | docs/design/0022-app-options.md | 1, 2 | `app.Listen()` serves HTTP or HTTPS from `AppOptions`; the body limit holds through `ServeHTTP` alone | todo |
| 4 | Built-in stages: `middlewares/*` move to `internal/`; `AppOptions` gets their groups (`Proxy`, `Hosts`, `Origins`, `LoadShed`, `HTTPSRedirect`, `Headers`, `CORS`, `CSRF`, `Compress`), one `Origins` list serving CORS and CSRF instead of a list each; the chain is built at `New` in the fixed order loadshed → proxy → hostcheck → httpsredirect → headers → CORS → CSRF → compress → user middlewares → routing; a stage is off until configured; the WebSocket handshake gets the same stages it gets today | tdd | docs/design/0022-app-options.md | 1 | binding order no longer matters; an app that configures nothing runs no stage | todo |
| 5 | WebSocket Origin check: the handshake refuses an `Origin` that is neither same-origin nor in `Origins` (cross-site WebSocket hijacking), as a `pre-route` stage of the handshake | tdd | docs/design/0022-app-options.md | 4 | a handshake from another site is refused unless listed | todo |
| 6 | `SecurityCheck`: one `Warn` at startup when the app has `SUBMIT_` routes without CSRF or HTML pages without security headers, with the docs link; silenced by turning the protection off explicitly; no behavior change | tdd | docs/design/0022-app-options.md | 4 | the warning appears exactly in those two cases | todo |
| 7 | Trace: the built-in stages emit `trace.StagePreRoute` (`"pre-route"`, `Name` = the stage, `Code` = the refusal status); `StageMiddleware` is for user middlewares only; the pretty access log shows a `pre-route` group | tdd | docs/design/0022-app-options.md | 4 | a refused request's trace ends at the stage that refused it | todo |
| 8 | `OptionsFromEnv()`: `OK_<PATH>` with `__` between levels and `PORT`; types from the fields (slices `a,b,c`, durations, `ByteSize`, enums); `.env` through the parser shared with `modules/config`; environment > `.env`, code assigned afterwards wins; every error stops `New` (bad value, bad element, empty value, unknown `OK_*` with the closest name); the startup log names each value's source, never a secret | tdd | docs/design/0022-app-options.md | 1-6 | an app configured only by `OK_*` variables starts the same as one configured in code; a typo stops it | todo |
| 9 | Callers: every test, `examples/`, `bench/`, README and package README example on the new API (about 370 `New()` and 600 configuration calls) | light | - | 1-8 | `make check` green; no removed name left | todo |
| 10 | Docs: a Configuration section (start here, a production checklist, one part per group, each field's default and variable), the behavior changes (built-in stages before user middlewares), CLAUDE.md's principle "Safe defaults" → "Explicit protection", `.owlky/`, `api/owlky.txt` | light | - | 9 | every `AppOptions` field is documented with its variable; the README examples compile | todo |
| 11 | Measure: `make bench` and the `bench/` scenarios against `docs/review/http-0009.md` | light | - | 4, 9 | no scenario slower than the noise band; `docs/review/http-0013.md` | todo |

Path: `tdd` (tester writes failing tests, builder makes them pass) or `light` (one agent or the main
session writes tests and code together, for small steps). State: `todo`, `doing`, `done`, `blocked`.

## Decisions
Agreed with the owner on 2026-11-19 (backlog › "App options", "Less configuration in `main`"):
- `AppOptions` (D-032 naming, never `Config`); groups at the top level, a feature adds a group; empty
  means off or today's default; no field depends on another; godoc on every field with its default.
- Configuration in `New`, code in `Bind…`, running in `Listen()` / `Serve(listener)`. `Create` (wire)
  and `Listen` (serve) stay separate; no one-line `owlky.Run`.
- No public built-in middlewares; their internal shape stays the middleware shape unless a benchmark
  shows a flat check loop is faster (internal, free to change). User middlewares keep their API and run
  after the built-in stages.
- Every built-in stage is off by default. Safety comes from docs, `examples/05-production`, later the CLI
  templates, and the `SecurityCheck` warning (step 6).
- Order: this roadmap first, then the security roadmap (0012), so the audit reviews the final API.
- Environment: prefix `OK_`, `__` between levels, `PORT` the one exception, code wins, strict errors,
  `.env` only, `ByteSize` with standard units.
- Breaking changes are free (nothing published); no deprecation aliases.
- Commits follow `.claude/commands/commit.md`: no Claude attribution or trailers.

## Log
