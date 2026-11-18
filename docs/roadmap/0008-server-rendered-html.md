# 0008: Server-rendered HTML and the route DSL

Status: Running

## Goal
A Ginject app serves finished HTML pages from the same controllers, modules and layers as its REST
routes: a page route renders an `html/template` with the handler's data, inside a layout, with
contextual escaping, a per-request CSP nonce and the same guards, CSRF protection and error
handling. Errors are HTML for pages and JSON for APIs. One binary carries its templates and assets,
and development reloads them from disk. A sample app shows pages built with Owlky's framework-free
HTML components.

## Scope
- In: the page route kind (the slot design 0017 left: a controller role, a DSL word, a result, `EnableRender(opts *ginject.RenderOptions)`); template loading (an `fs.FS`: `embed.FS` in production, the disk in development), parsing once, layouts and partials, a small fixed set of template functions; the render result and status codes; HTML error pages (404, 405, exceptions from handlers, guards and filters) for page routes, JSON for API routes; middleware refusals (`csrf` 403, `hostcheck` 421, `loadshed` 503, `throttler` 429) choosing HTML or JSON from `Accept`; forms (urlencoded and multipart through the existing binding) with validation errors rendered back into the form; a CSP nonce per request that `helmet` and the templates share; static assets served from an `fs.FS` with cache headers (the existing `SERVE` route kind where it fits); devtool and trace events for renders; docs and a sample.
- Out: a client-side framework or a JavaScript build step; a template engine other than `html/template`; streaming or partial page updates (HTMX-style fragments may come later); authentication (next roadmap: the sample uses no login); a dependency on Owlky's repository or package: Ginject stays Owlky-agnostic, and the sample uses Owlky's published HTML snippets and assets as plain files.

## Steps
| # | Step | Path | Design | Depends on | Done when | State |
|---|---|---|---|---|---|---|
| 1 | Design: the page route kind and its public names, the template set (loading, layouts, partials, functions, reload in development), the render result, the error output per route kind and per `Accept`, forms and validation errors, the CSP nonce, assets; idle and per-request cost | light | docs/design/0018-server-rendered-html.md | - | the design lists every exported addition with its reason, the behavior rules and the tests | done |
| 2 | Templates and the page route kind: `EnableRender`, the controller role, the DSL word, rendering a template inside its layout with the handler's data; parse once, reload from disk in development; render errors | tdd | docs/design/0018-server-rendered-html.md | 1 | a page route answers `text/html` from a template in an `embed.FS`; a template error is a 500 with a logged cause, never a half-written page | done |
| 3 | Errors as HTML: 404, 405 and exceptions on page routes render an error template (a built-in default, replaceable); API routes keep JSON; middleware refusals pick from `Accept` | tdd | docs/design/0018-server-rendered-html.md | 2 | each refusal and exception gives HTML for a browser request and JSON for an API request, as the design's table | done |
| 4 | Forms: a page route that receives a form re-renders it with the submitted values and the validation errors; CSRF covers it; redirect after a successful post | tdd | docs/design/0018-server-rendered-html.md | 2 | invalid input re-renders the form with messages and the values kept (escaped); valid input redirects with 303 | done |
| 5 | CSP nonce and assets: one nonce per request in `helmet`'s CSP and in templates; assets from an `fs.FS` with cache headers | tdd | docs/design/0018-server-rendered-html.md | 2 | an inline script with the request's nonce runs under the CSP, one without is blocked; hashed assets are immutable | done |
| 6 | Observability: render time and template name in trace events, devtool snapshot of templates and page routes | light | docs/design/0018-server-rendered-html.md | 2 | a render emits a stage event with its template; the snapshot lists the page routes | done |
| 7 | Sample: `sample/pages` (a small booking-style site: list, detail, form, error pages) with Owlky's HTML snippets and assets | light | - | 2-5 | the sample builds and serves its pages; a test drives the form flow | done |
| 8 | Docs: README section, a `render` reference, `.ginject/`, `CLAUDE.md` | light | - | 2-7 | every new name documented with a runnable example | done |
| 9 | Measure: a page route against a JSON route in `bench/` (time and allocations per request, idle memory) | light | - | 2-5 | numbers in `docs/review/http-0008.md` | done |
| 10 | Design, one architect pass: design 0019 (route DSL: kebab-case and its edge cases, `PathStyle` inherited like `Prefix` with an explicit setting winning, exact path for one route, `ALL`, the upper-case rule, the two parser bugs) and design 0018 amendment 2 (pages by route: `RENDER_`/`SUBMIT_`, template resolution after prefixes and `PathStyle`, no `ANY` or `VERSION` on pages, removal of `ginject.Page` and `ginject.View`, error pages by status, reserved names, `public/`, page CSS/JS with `{{pageAssets}}`), with the migration list of every name, file and doc | light | docs/design/0019-route-dsl.md, docs/design/0018-server-rendered-html.md | - | every rule with current → expected output and its test name; every exported addition and removal | done |
| 11 | DSL: kebab-case paths (`_` between the words of one resource → `-`; camelCase, parameters, versions and `Prefix` strings unchanged) and the upper-case rule | tdd | docs/design/0019-route-dsl.md | 10 | `READ_user_profile` → `/user-profile`; `READ_orgs_FB` panics; parser rule tests pass | done |
| 12 | DSL: `ModuleBuilder().PathStyle(…)` (`PathStyleKebab` default, `PathStyleAsWritten`), inherited by imports like `Prefix`, an explicit setting wins | tdd | docs/design/0019-route-dsl.md | 11 | a root `PathStyleAsWritten` gives `/payment_intents` to its own and imported modules; a module that sets kebab keeps `/payment-intents` | done |
| 13 | DSL: exact path for one route (the design's spelling); WebSocket `ALL`; the parser bugs (`ANY` after a parameter, `__` in a parameter name) | tdd | docs/design/0019-route-dsl.md | 11 | the design's expected outputs; `SUBSCRIBE_chat_ALL` handles `chat.room1.typing` | done |
| 14 | Pages by route: `RENDER_` (GET page; template `<path>.html` or `<path>/index.html` after prefixes and `PathStyle`; data result; no `ANY`/`VERSION`; missing or double template panics), removal of `ginject.Page` and `ginject.View` | tdd | docs/design/0018-server-rendered-html.md | 12 | `RENDER_bookings` renders `bookings/index.html`; a module `Prefix("admin")` page renders `admin/…`; the API file loses `Page`, `View` | todo |
| 15 | `SUBMIT_`: POST on the URL of its `RENDER_`, same template, 422 re-render or 303; a `SUBMIT_` without its `RENDER_` panics | tdd | docs/design/0018-server-rendered-html.md | 14 | the form flow of the sample passes with `SUBMIT_new_OF_bookings` | todo |
| 16 | Error pages by status (`<code>.html` → `<N>xx.html` → built-in, replacing `error.html`) and the reserved names check after prefixes | tdd | docs/design/0018-server-rendered-html.md | 14 | 404 uses `404.html`, 503 falls back to `5xx.html`; `RENDER_404` panics | todo |
| 17 | `public/` served at the root URL (`no-cache` + `ETag`, conflicts with routes panic) and page CSS/JS next to templates with `{{pageAssets}}` (orphans and a missing `{{pageAssets}}` panic) | tdd | docs/design/0018-server-rendered-html.md | 14 | `/robots.txt` from `public/`; `/bookings/new` links `new.css` and `new.form.js` after the layout's files | todo |
| 18 | Migrate every route name, template and test in the repo (`sample/`, `bench/`, docs) to the new DSL and pages; the startup route log and the devtool snapshot show final paths and page routes | light | - | 11-17 | `make check` green; samples serve what they document | todo |
| 19 | Docs: README (routes, pages), `internal/common` › Route DSL, `internal/render`, `.ginject/`, `CLAUDE.md`, Sổ tay; every example compiles | light | - | 18 | every new rule documented with an example | todo |

Path: `tdd` (tester writes failing tests, builder makes them pass) or `light` (one agent or the main
session writes tests and code together, for small steps). State: `todo`, `doing`, `done`, `blocked`.

## Decisions
- 2026-11-18, the maintainer: Claude approves this roadmap and its designs itself, runs it to the end,
  and leaves the work **uncommitted** for the maintainer to review in the morning.
- `html/template` only; no new module dependency.
- The public names go in the slots design 0017 reserved (a root alias for the page role, a DSL word,
  `EnableRender(opts *ginject.RenderOptions)`), by its naming rules; every addition goes through
  `api/ginject.txt`.
- Ginject does not depend on Owlky: it renders any HTML. The sample uses Owlky's HTML components as
  static files.
- Small machines first: templates are parsed once; nothing runs while idle; per-request cost is
  measured.

### Route DSL (merged from the draft roadmap 0009, 2026-11-18)
- 2026-11-18, discussed with the maintainer:
  - **`OF` stays.** The name reads as a sentence and groups handlers by the resource they return.
  - **Kebab-case by default.** Most URLs use `-`, and Go identifiers can't hold one, so without this
    rule a `-` can't be written at all. `__` as an escape for a literal `_` is dropped (an implicit rule
    hard to read).
  - **`PathStyle` on the module, inherited like `Prefix`.** Set once on the root module and every
    module of the app follows; a module that sets its own style keeps it, so a reusable module's URLs
    never change with its host. Values `PathStyleKebab` (default) and `PathStyleAsWritten` (keeps `_`
    as written, for APIs shaped like Stripe's or GitHub's).
  - **`ALL` for WebSocket `**`** (approved).
  - **Exact path for one route** (the per-field override of ORMs): in scope; the spelling is the
    design's.
- Ginject is not published: the URL changes are breaking and free; no compatibility switch beyond
  `PathStyleAsWritten`.
- Every exported addition goes through `api/ginject.txt`.

### Open questions of the route DSL (the design proposes, the maintainer decides)
1. Upper-case words that are not tokens (`READ_orgs_FB` → `/orgs-FB` today): keep them as path text,
   or refuse an all-upper-case segment at `Create()` so a future token can't change a route silently.
2. A module imported by two parents with different styles: follow what `Prefix` does in the same case.

- 2026-11-18, the maintainer: security is the roadmap right after this phase (backlog › "Security roadmap"). While this phase runs, every security-relevant point found is written there at once, not fixed on the spot unless it blocks the work.

### Pages by route (decided 2026-11-18, steps 14-17)
The maintainer wants pages like Next.js: the route picks the template.
- `RENDER_<route>` is a GET page; its template is the route's final path inside `Dir`: `<path>.html`
  or `<path>/index.html` (both → panic at `Create()`; none → panic naming both paths). `RENDER` → `index.html`.
- Parameters are written `{name}` in file and directory names (`bookings/{id}.html`).
- Module and controller prefixes are part of the path (as for REST routes): `Prefix("admin")` +
  `RENDER_users` → `admin/users.html`.
- No version on `RENDER_` (versioning is an API concern): panic at `Create()`.
- `PathStyle` applies: the file path follows the final URL (`RENDER_court_bookings` → `court-bookings/`).
- A template without a `RENDER_` handler is not a route (no file-only pages).
- `ginject.Page` goes away; a `RENDER_` handler returns its data (`ginject.Map` or any value).
- **Error pages by status:** `<code>.html` → `<N>xx.html` (`4xx.html`, `5xx.html`) → the built-in page, at
  the root of `Dir` only (nested error pages may come later). They replace `error.html`. Same data
  (`.Code`, `.Status`, `.Message`, `.RequestID`), inside the root layout, parsed at `Create()`.
- **Reserved names:** at the root, `<3 digits>.html` and `<digit>xx.html` are error pages (documented). A
  `RENDER_` whose resolved template (after module and controller prefixes) is one of them panics at
  `Create()`, like the existing `route conflict:` check (verified: `container.addRoute` receives the
  pattern after the prefixes are applied).
- **`public/`** at the root of `Dir`: its files are served at the root URL with their own names
  (`public/robots.txt` → `/robots.txt`, `favicon.ico`, `sitemap.xml`, `.well-known/…`), `no-cache` with an
  `ETag` (their URLs are fixed). A file whose URL equals a route panics `route conflict:` at `Create()`.
- **No `ANY` on pages:** a `RENDER_` with `ANY` panics at `Create()` (keeps pages explicit). API routes and
  `SERVE` keep `ANY`.
- **Upper-case words** (route DSL open question 1, decided): an all-upper-case segment between two `_` that
  is not a token panics at `Create()` (`invalid route: FB in READ_orgs_FB is upper case but not a route
  word; write it in lower case or camelCase`). Parameter names and versions are not checked; upper case
  inside a word (`orgsFB`, `apiV2`) is allowed. In the repo only `SUBSCRIBE_chat_PERSON_ANY`
  (`sample/benchmarks`) changes.
- **CSS and JS:** app-wide files live in `assets/` and are linked once in the root layout with `{{asset}}`.
  Files of one page or layout sit next to its template and are named after it: `X.css`, `X.<any>.css`,
  `X.js`, `X.<any>.js` belong to `X.html` in the same directory (`layout.css` to that directory's layout).
  The layout calls a new template function `{{pageAssets}}`, which writes the tags of the page being
  rendered: layouts from the outermost in, then the page, each group in alphabetical order. They are
  served under `/assets/_pages/…` (`_pages` reserved in `assets/`) with a hash and `immutable`
  (`no-cache` with `IsReload`); no nonce needed. At `Create()`: a CSS or JS file next to templates that
  belongs to no template panics, and a page with such files whose layout never calls `{{pageAssets}}`
  panics. The file lists are computed once per page set; nothing extra at render time.
- **Form POST (option 1, approved):** `SUBMIT_<route>` is a POST on the URL of `RENDER_<route>` and uses its
  template: data with errors re-renders it (422), success redirects (303). A `SUBMIT_` without its
  `RENDER_` panics at `Create()`. Edit and delete forms post to the page that shows them; no `_method`
  override. `ginject.View` goes away: page handlers always return data. `CREATE_`, `UPDATE_`, `DELETE_`
  stay API routes.

## Log
- 2026-11-18: design 0018 approved by delegation (its open questions are decided as it states). Run started at `402b10f` (roadmap 0007 pushed, CI green); by the maintainer's instruction nothing of this roadmap is committed.
- 2026-11-18: step 2: five red tests contradicted `html/template` or the documented 410 rule for unversioned requests under header versioning; the tests were corrected, the code kept. `BenchmarkRender_Nonce` shows about 14 extra allocations for `{{nonce}}` before step 5's in-place overwrite.
- 2026-11-18: steps 3-4: `TestPage_UnknownViewIs500` (step 2) expected a render failure to skip `error.html`, which R20 and R24 contradict; the test now expects the error page with the fixed 500 data. Allocations of the API paths (Static, Exception, NotFound) unchanged; one built-in filter fallback went from 20 to 18.
- 2026-11-18: steps 5-6: "`{{nonce}}` costs at most 2 allocations" is measured against a set whose `nonce` function returns the value directly (any template function call allocates in `html/template`). helmet hands its nonce form over through a new unexported-in-practice `httphook.CSPWriter` interface (internal package) instead of a new `Writer` method, so existing writer fakes keep compiling. Pooled render buffers start at 4 KiB.
- 2026-11-18: step 7: a pipe cannot hold unexported fields (the existing injection rule), so the sample's form DTO re-parses its date in the service; `html/template` strips JavaScript comments in inline scripts (documented). The built-in `/assets/*` route does not appear in the startup route log (left as is, noted for review).
- 2026-11-18: step 9: a page route of 10 rows costs 62.6 µs and 448 allocations against 8.2 µs and 40 for the same data as JSON; 92 % of the page's allocations are inside `html/template` execution. Idle heap +24.7 KiB with one page. `docs/review/http-0008.md`.
- 2026-11-18: review (one pass): no critical or high finding. Fixed: HTML error pages now send `Cache-Control: no-cache` (they may carry a nonce; 404/405/410 are heuristically cacheable), a reload failure on an error-only path logs `RenderReloadFailed` and counts as a failure, `{{asset}}` fails in reload mode when `assets/` did not exist at `Create()` (no route would serve it); the step 9 report gained the API-route comparison against `402b10f` and the binary size.
- 2026-11-18: amendment 1 of design 0018, approved by the maintainer: `RenderOptions.Dir` (default `web`), `IsReload` on an `embed.FS` reads `Dir` from disk, a `Warn` `RenderReloadOn` at start. `sample/pages/main.go` lost `fs.Sub` and `os.DirFS`. API: 10 lines instead of 9.
- 2026-11-18: the maintainer merged the draft roadmap 0009 (route DSL) into this roadmap as steps 10-17; the DSL is discussed further for pages before its design. Nothing is committed yet.
- 2026-11-18: steps 1-9 and amendment 1 committed (`f8168af`, `9fe673b`) to clean the tree; the second iteration (steps 10-19: route DSL and pages by route) starts.
- 2026-11-18: step 10: design 0019 (B1-B26) and design 0018 amendment 2 (P1-P34) written and approved by delegation. The architect's own decisions are listed in each design's Open questions (exact path as `instance.Path(path, handler)`, malformed names panic, a `.html` file without a route panics, `FILE` refused on pages, page routes version-neutral). Nine security findings added to the backlog.
- 2026-11-18: steps 11-13: three red tests met the existing versioning rule (a request without a version to an unversioned route answers 410); their routes became `VERSION_NEUTRAL` and the 'other version' check expects 405. `TestSurface_EmbeddableMembers` now lists `Path`. `SUBSCRIBE_chat_PERSON_ANY` → `SUBSCRIBE_chat_person_ANY`.
