# 0018: Server-rendered HTML

Status: Approved (by delegation, 2026-11-18: roadmap 0008 › Decisions records that the maintainer
delegated the approval of this roadmap's designs)
Packages: root `owlkei` (3 aliases); `internal/common` (`Page`, `View`); `internal/core` (`EnableRender`,
`RenderOptions`, page routes, error output, the asset route); a new `internal/render` (template sets,
buffers, nonce, asset table); `internal/ctx` (hooks, three client messages); `internal/httphook` (CSP
hand-over); `internal/trace` (one stage); `internal/devtool`; `middlewares/helmet`; `sample/pages`.
Roadmap: `docs/roadmap/0008-server-rendered-html.md`, steps 2 to 9.

## Goal
A controller that embeds `owlkei.Page` serves HTML pages: its handlers return a `owlkei.View` (a template
name and its data) and the framework renders it with `html/template` inside a layout. Page routes use the
same route words, modules, pipes, guards, interceptors and filters as API routes, answer errors as HTML,
share a per-request CSP nonce with `helmet`, and serve their assets from the same `fs.FS` as the templates.
An app that never calls `EnableRender` behaves and costs exactly as before, except for the client text rule
(R24).

## Decisions in one table

| # | Decision | Why |
|---|---|---|
| D1 | The page route kind is a **controller role**: embed `owlkei.Page` instead of `owlkei.HTTP`. No new DSL word; `RENDER` stays unused and reserved | A page controller needs GET pages *and* POST form handlers. A `RENDER_x` word could only say GET, so it would be a second word for `READ` and leave form posts without a kind. With the role, `READ_bookings` is a GET page and `CREATE_bookings` is its form post: zero new vocabulary, and the kind is visible on the first line of the controller |
| D2 | A page handler returns `owlkei.View{Name, Data}`. No `HTTPContext.Render` method | One way to render. The result goes through interceptors like any result; a method would be a second way and would write before post-interceptors run |
| D3 | Status and redirect use what exists: `c.Status(code)` or the `(int, owlkei.View)` result form; `c.Redirect(url)` or the `owlkei.Redirect` parameter. `View` has no `Status` or `Redirect` field | Adding them would give status three ways and redirect three ways |
| D4 | On a page route the default status is 200 for every method and a redirect without an explicit 3xx status answers **303** | A re-rendered form is not `201 Created`; `301` after a POST is cached forever by browsers. API routes keep 201 and 301 |
| D5 | `RenderOptions` has two fields: `FS` and `IsReload`. Layouts, partials, the error page and assets are **file conventions** inside `FS`, not options | "Powerful inside, small outside": one `//go:embed web` carries everything; nothing to configure |
| D6 | Template functions are fixed: `nonce`, `asset`, `dict`. No user `FuncMap` | Formatting belongs on the data (`{{.StartsAt.Format "2 Jan 2006"}}` calls a method). `dict` is the one thing methods cannot do: pass several values to a partial. A user map is an option whose misuse (`template.HTML` helpers) removes the autoescaping this feature exists for |
| D7 | Assets live in `FS/assets` and are served at `GET /assets/*` by a built-in route of the existing `SERVE` kind. User `SERVE` handlers do not change (they still return a directory path) | `{{asset}}` needs the files' hashes at `Create()`, and a user `SERVE` handler's result is only known per request. The serving code (`serveFromRoot`'s rules: hidden segments, traversal, `index.html`) is reused |
| D8 | HTML or JSON: **a matched route that reached its guards decides by its kind; anything earlier (middleware refusals, 404, 405, 410) decides by `Accept`** | The roadmap fixes `Accept` for middleware refusals. `guards/throttler` is a guard, so its 429 follows the route kind, which is exact where `Accept` is a guess |
| D9 | D-028 rule 4 for both outputs: a cause's text never reaches a client; a 500 always says `Internal Server Error` and the detail is logged | Design 0017 left this to be decided here, for pages and APIs together. One function computes the three client values for JSON and HTML |
| D10 | `{{nonce}}` writes a per-app secret placeholder that the engine overwrites in the buffer with the request's nonce | Template functions are bound to a set, not to a request. The alternatives are a clone per request (milliseconds) or wrapping the user's data (`{{.Data.X}}` everywhere). In-place overwrite is allocation-free and keeps sets immutable |
| D11 | `IsReload` re-parses when a file's name, size or modification time changed, checked on each render. No watcher goroutine, no dependency | Zero cost when off (one branch), nothing running while idle |

## Public API

```go
package owlkei

type (
	Page          = common.Page
	View          = common.View
	RenderOptions = core.RenderOptions
)
```

```go
package common

type Page HTTP

func (p *Page) Prefix(v string, handlers ...any) *Page

type View struct {
	Name string
	Data any
}
```

```go
package core

type RenderOptions struct {
	FS       fs.FS
	IsReload bool
}

func (app *App) EnableRender(opts *RenderOptions) *App
```

| Addition | Why users need it |
|---|---|
| `owlkei.Page` | Embedded in a controller to declare that its handlers are page routes (the slot 0017 reserved: a seventh name in `fieldNameByRole`) |
| `(*Page).Prefix` | The same prefix call as `(*HTTP).Prefix`; `Page` is a defined type, so the method is declared again |
| `owlkei.View`, fields `Name`, `Data` | The result of a page handler: which template, with what data |
| `owlkei.RenderOptions`, fields `FS`, `IsReload` | Where the templates and assets are; whether to reload them in development |
| `App.EnableRender` | Turns rendering on, by the `Enable…(opts *XxxOptions)` rule |

Nothing else is exported. `internal/render` is not importable. `helmet` gains no field.

Exact lines `make api` adds to `api/owlkei.txt` (any other diff is a defect):

```
pkg owlkei, type Page = common.Page
pkg owlkei, type RenderOptions = core.RenderOptions
pkg owlkei, type View = common.View
reach common.Page, method (*common.Page) Prefix(string, ...any) *common.Page
reach common.View, field Data any
reach common.View, field Name string
reach core.App, method (*core.App) EnableRender(*core.RenderOptions) *core.App
reach core.RenderOptions, field FS fs.FS
reach core.RenderOptions, field IsReload bool
```

As an application writes it:

```go
//go:embed web
var webFS embed.FS

type BookingPages struct {
	owlkei.Page
	Bookings BookingService
}

func (p BookingPages) NewController() owlkei.Controller { return p }

func (p BookingPages) READ_bookings() owlkei.View {
	return owlkei.View{Name: "bookings/list", Data: owlkei.Map{"Bookings": p.Bookings.All()}}
}

func (p BookingPages) CREATE_bookings(c owlkei.HTTPContext, form BookingForm) owlkei.View {
	if len(form.Errors) > 0 {
		c.Status(http.StatusUnprocessableEntity)
		return owlkei.View{Name: "bookings/new", Data: form}
	}
	p.Bookings.Add(form)
	c.Redirect("/bookings")
	return owlkei.View{}
}

func main() {
	web, _ := fs.Sub(webFS, "web")
	app := owlkei.New()
	app.BindGlobalMiddlewares(helmet.Helmet{}, csrf.CSRF{})
	app.EnableRender(&owlkei.RenderOptions{FS: web})
	app.Create(Module)
	_ = app.Listen(8080, nil)
}
```

### File conventions inside `RenderOptions.FS`

| Path | Role | Template name |
|---|---|---|
| `layout.html` in any directory | Layout of the pages in that directory and below; the nearest one wins | none |
| `partials/**/*.html` (top-level `partials` only) | Partial, included with `{{template "partials/field" .}}` | path without `.html` |
| `error.html` (root) | Replaces the built-in error page | none (not a page: `View{Name: "error"}` is unknown) |
| `assets/**` | Static files at `/assets/…`; never parsed | none |
| every other `**/*.html` | Page | path without `.html`, `/`-separated: `bookings/list` |
| anything else; any path with a segment starting with `.` | Ignored | none |

| Composition rule | |
|---|---|
| Page body | The page file is the template `content`. It needs no `{{define}}` |
| Layout | Places the page with `{{template "content" .}}`. Its `{{block "title" .}}…{{end}}` blocks are replaced by a page's `{{define "title"}}…{{end}}` |
| No layout found | The page is rendered alone |
| Data | `View.Data` is `.` in the page and in its layout. A partial gets what the caller passes |
| Reference | A template is referenced by its file name only. A page set holds its layout, its page and the partials they reach |

### Template functions

| Function | Result | Notes |
|---|---|---|
| `{{nonce}}` | The request's CSP nonce: 32 lowercase hexadecimal characters (128 bits from `crypto/rand`) | The same value at every use in one response and in the `Content-Security-Policy` header (R33) |
| `{{asset "css/app.css"}}` | `/assets/css/app.css?v=<16 hex>`; with `IsReload`, `/assets/css/app.css` | `<16 hex>` is the first 8 bytes of the file's SHA-256. An unknown file fails the render (R12) |
| `{{dict "Name" "email" "Value" .Email}}` | `map[string]any` | An odd number of arguments or a non-string key fails the render |

## Behavior

Bootstrap panics follow `"<category>: <detail>"`, red (`color.FmtRed`). Tests match the uncolored fragment
given here. "Fixed 500" means status 500 with the 500 row of R24 in the route's output format.

### Step 2: templates and the page route kind

| # | Rule |
|---|---|
| R1 | In a controller that embeds `owlkei.Page`, every method named by the HTTP route words becomes a route with the same method, path, prefixes and version as under `owlkei.HTTP`, and is a **page route**. `BindGuard`, `BindInterceptor`, `BindMiddleware`, `BindExceptionFilter` and pipes work unchanged |
| R2 | `Create()` panics: a controller embedding both → `invalid controller: <Type> embeds both owlkei.HTTP and owlkei.Page`; a `Page` controller without `EnableRender` → `invalid render: <Type> embeds owlkei.Page, call EnableRender before Create`; a page handler (not `SERVE`) whose only or last result is not `owlkei.View` → `invalid handler: <Type>.<Method> must return owlkei.View`; an `HTTP` controller handler whose only or last result type is `owlkei.View` → `invalid handler: <Type>.<Method> returns owlkei.View, which needs a owlkei.Page controller` |
| R3 | `EnableRender(nil)` or a nil `FS` panics `invalid render: EnableRender needs RenderOptions.FS`; a second call panics `invalid render: EnableRender called twice`; after `Create` it panics `invalid app: EnableRender must be called before Create` |
| R4 | `Create()` parses every layout, page, partial and `error.html` once. A parse error, a reference to a template that does not exist, or an `html/template` escaping error panics `invalid render: <file path>: <error>`. No request is needed to find them |
| R5 | A page handler returning `View{Name: "bookings/list", Data: d}` answers the page inside its nearest layout with `Content-Type: text/html; charset=utf-8`, a `Content-Length`, and `Cache-Control: no-cache` unless the handler set `Cache-Control` |
| R6 | The status is 200 for every method (POST too) unless set by `c.Status(code)` or the `(int, owlkei.View)` form |
| R7 | Values in `Data` are escaped for their context by `html/template`: `<script>alert(1)</script>` in a text node arrives as `&lt;script&gt;…`, and inside an attribute, a URL or a script as that context requires |
| R8 | When the response is already written (a redirect, a direct write), the returned `View` is ignored and nothing more is written |
| R9 | A `View` whose `Name` is no page (unknown, empty, `error`, a partial, a layout, a path with `..`) with no response written → fixed 500 as HTML, and one `Error` log `RenderFailed` with `template` and `cause`. The name is looked up in a table built at `Create()`; no file is opened |
| R10 | A template that fails while executing (a missing field, a function error, a failed `dict` or `asset`) → fixed 500 as HTML and `RenderFailed`. No byte of the failed page reaches the client: the body holds no fragment of it and the status is 500, not 200 |
| R11 | A rendered page larger than 8 MiB fails like R10 (`cause` names the limit) |
| R12 | `{{asset "x"}}` for a file that is not in `FS/assets` fails the render (R10); at `Create()` it is found only if the page executes it without data |
| R13 | A result that is not a `View` after the interceptors (a post-interceptor replaced it) fails like R9. An interceptor that returns a `View` without calling the handler renders it |
| R14 | `IsReload: false`: a file changed in `FS` after `Create()` is never read; the response is the one parsed at `Create()`. `IsReload: true`: a render after a page, layout, partial or `error.html` was added, removed or changed (name, size or modification time) uses the new files. A reload that fails to parse answers fixed 500 as HTML with one `Error` log `RenderReloadFailed` (`file`, `cause`) until the file is fixed; the error text is never in the response |
| R15 | `HEAD` on a GET page answers the page's status and headers with no body |

### Step 3: errors

Format choice (R16 to R19 apply only when `EnableRender` was called; without it every error is JSON, byte
for byte as today apart from R24):

| # | Situation | Output |
|---|---|---|
| R16 | A matched **page route**, from its first guard on: a guard returning `false` (403), an exception or panic from a guard, pipe, interceptor, handler or filter that reaches the built-in filter | HTML |
| R17 | A matched API route (`owlkei.HTTP`), a user `SERVE` route or the asset route, from its first guard on | JSON |
| R18 | Before any guard: an exception or panic from a global or module middleware (`csrf` 403, `hostcheck` 421 and 400, `loadshed` 503, `httpsredirect` 400, any user middleware), and no route: 404, 405 (with its `Allow` header), 410 | By `Accept`: HTML when a value of the request's `Accept` header contains `text/html` (ASCII case-insensitive), else JSON. `*/*` alone → JSON. No `Accept` → JSON. `application/xhtml+xml` alone → JSON. q-values are not read. The response carries `Vary: Accept` |
| R19 | `guards/throttler` 429 | A guard: R16 or R17, with its `Retry-After` and rate headers kept |

| # | Rule |
|---|---|
| R20 | An HTML error is `error.html` rendered inside the root `layout.html` with the data `map[string]any{"Code": int, "Status": string, "Message": string, "RequestID": string}`, `Content-Type: text/html; charset=utf-8`, the error's status code, and the headers already set by layers (`Retry-After`, `Allow`) kept. `Content-Length`, `Content-Encoding`, `Etag` and `Last-Modified` set earlier are removed, as for JSON errors |
| R21 | Without `error.html` the built-in page is used: a complete HTML document with no layout, no inline style or script and no external resource (it passes any CSP), holding the code, the status text and the message, escaped |
| R22 | If rendering `error.html` or the layout around it fails, the built-in page is sent with the same code and `RenderFailed` is logged. An error response is never empty and never a half page |
| R23 | A user exception filter still runs first on a page route and may answer by itself; an exception it re-panics continues to the next filter and ends as R16 |
| R24 | Client text, for JSON (`code`, `error`, `message`) and HTML (`.Code`, `.Status`, `.Message`) alike: see the table below. The JSON shape and key order do not change |

| Source | Code sent | `error` / `.Status` | `message` / `.Message` | Logged |
|---|---|---|---|---|
| Exception without a cause, code 400–599 except 500 | its code | its description, else the status text | its message (empty → `Unhandled exception has occurred`, as today) | no |
| Exception with a cause (`error` or `ExceptionOptions.Cause`), code except 500 | its code | the status text | its message | `Debug` `ExceptionCause` (`method`, `path`, `code`, `cause`) |
| Exception with code 500, or a code outside 400–599 | 500 | `Internal Server Error` | `Internal Server Error` | `Error` `InternalServerError` (`method`, `path`, `message`, `cause`) when its message or cause differs from the fixed text |
| A panic that is not an exception | 500 | `Internal Server Error` | `Internal Server Error` | `UnhandledPanic` (exists) |
| A render failure | 500 | `Internal Server Error` | `Internal Server Error` | `RenderFailed` |

Three framework messages that carried `err.Error()` change to our own words, with the error kept as the
cause: body and form over the limit → `Request body too large` (413), a broken file upload
(`internal/ctx/http_file.go`) → `Invalid file upload` (400). WebSocket replies are not touched (the
WebSocket roadmap owns them).

### Step 4: forms

No new API and no template work: the DTO is a `Form` pipe (`Transform(owlkei.Form, owlkei.ArgumentMetadata) any`)
that calls `form.Bind(dto)`, reads the `[]owlkei.FieldLevel` and returns the DTO with its own
`Errors map[string]string`; the handler passes it to the view.

| # | Rule |
|---|---|
| R25 | A `Form` pipe on a page route binds `application/x-www-form-urlencoded` and `multipart/form-data` bodies with the same result as on an API route |
| R26 | Invalid input: the handler returns the form's view with status 422; the response holds each error message and each submitted value in its field, escaped (`"><script>` submitted as a name arrives as `&#34;&gt;&lt;script&gt;` inside `value="…"`) |
| R27 | Valid input: `c.Redirect("/bookings")` (or the `owlkei.Redirect` parameter) with no status set answers `303 See Other` with `Location: /bookings` and no page body from the returned `View`. An explicit 3xx set with `c.Status` is kept. On an API route the same call still answers 301 |
| R28 | A malformed form body (400) or one over `MaxRequestBodyBytes` (413) on a page route is an HTML error page (R16), with the messages of R24 |
| R29 | CSRF needs no token and no template helper: `csrf.CSRF{}` checks `Sec-Fetch-Site` / `Origin`. A cross-origin form POST with `Accept: text/html` is refused with a 403 HTML page (R18); the same-origin POST passes; the same refusal with `Accept: application/json` is JSON |

### Step 5: CSP nonce and assets

| # | Rule |
|---|---|
| R30 | A nonce is generated only for a response whose page set (page, layout or reached partial; or the error page's set) contains `{{nonce}}`. API routes, asset responses and pages without `{{nonce}}` generate none and their `Content-Security-Policy` is `helmet`'s static value, byte for byte as today |
| R31 | With `helmet` bound and its default policy, a page that uses `{{nonce}}` answers exactly `Content-Security-Policy: default-src 'self'; script-src 'self' 'nonce-<N>'; style-src 'self' 'nonce-<N>'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; script-src-attr 'none'; upgrade-insecure-requests` |
| R32 | For a custom `ContentSecurityPolicy`, for each of `script-src`, `style-src` (and `script-src-elem`, `style-src-elem` when present): see the table below |
| R33 | `<N>` is 32 lowercase hexadecimal characters, the same in the header and at every `{{nonce}}` of the body, and different on every response (1,000 responses → 1,000 values). A value submitted by a client never becomes a nonce |
| R34 | Without `helmet` (or with `ContentSecurityPolicy: helmet.Disabled`), `{{nonce}}` still renders a nonce and no CSP header is added |
| R35 | A `Content-Security-Policy` header set by the handler or by another middleware after `helmet` is left as it is |
| R36 | When `FS` has an `assets` directory, `GET` and `HEAD /assets/<path>` serve `FS/assets/<path>` with the content type of its extension. A directory, a missing file, or a path with a segment starting with `.` (except `.well-known`) or with `..` → 404 JSON (R17). Templates are not reachable through it |
| R37 | `IsReload: false`: the response carries `ETag: "<16 hex>"`. With `?v=<16 hex>` equal to the file's hash → `Cache-Control: public, max-age=31536000, immutable`. Without `v`, or with another value → `Cache-Control: no-cache`. `If-None-Match` with the ETag → 304 |
| R38 | `IsReload: true`: `Cache-Control: no-cache`, no `ETag`, the file is read from `FS` on each request, and `{{asset}}` adds no `?v=` |
| R39 | A user route with the same shape as `/assets/*` while `FS/assets` exists panics at `Create()` with the existing `route conflict:` message. Without an `assets` directory no route is added |
| R40 | Global middlewares and global guards apply to the asset route like to any route |

| Directive in the policy | Result on a page with a nonce |
|---|---|
| present, without `'unsafe-inline'` and without `'none'` | ` 'nonce-<N>'` appended to it |
| present with `'unsafe-inline'` or `'none'` | unchanged |
| absent, `default-src` present without `'unsafe-inline'` and without `'none'` | the directive is inserted right after `default-src` with `default-src`'s sources plus `'nonce-<N>'` |
| absent and `default-src` absent, or `default-src` holds `'unsafe-inline'` or `'none'` | unchanged |

### Step 6: observability

| # | Rule |
|---|---|
| R41 | With tracing on, each render emits one event: `Stage` = `render` (`trace.StageRender`), `Name` = the page name (`bookings/list`; `error` for an error page, `error (built-in)` for the built-in one), `Transport` = `HTTP`, `Duration` = the time to execute into the buffer and place the nonce, `Code` = 500 when the render failed, the trace ids of the request. It comes after the `handler` event and before `complete`. With tracing off no event is built and no clock is read |
| R42 | The devtool snapshot marks each page route: `HTTPComponent.Kind` = `page` (JSON `kind`; omitted for API routes) |
| R43 | The devtool snapshot has `render` (omitted without `EnableRender`): `is_reload`, `pages` (name, layout file), `partials`, `error_template` (`error.html` or `built-in`), `assets` (count), `renders` and `failures` (counters) |
| R44 | `helmet`'s snapshot gains `nonce_directives`: the directives that receive a nonce (`["script-src","style-src"]` for the default policy) |

## Performance

| Path | Kind | Cost |
|---|---|---|
| API route, `EnableRender` not called | hot | Unchanged: no new branch on the success path. The error path computes R24 (no allocation for the fixed 500 and for exceptions without a cause) |
| API route, `EnableRender` called | hot | One `isPage` flag read in `serveRoute`. `helmet` adds one pointer store per request (the precompiled nonce form handed to the writer through `internal/httphook`) |
| Page route | warm | Table lookup of the page set, one pooled buffer, `html/template` execution, one scan of the buffer when the set uses `{{nonce}}`, one `Write` |
| `Create()` | cold | Reads and parses every template once, executes each page set once into `io.Discard` with nil data to force escaping (so escaping errors are startup errors and no request pays the first-use escaping), hashes every asset file (SHA-256) |
| Idle | | No goroutine, timer or watcher, in production and with `IsReload` |

Expected numbers, to be measured in step 9 (`bench/`, `docs/review/http-0008.md`); they are estimates, not
gates:

| | JSON route (today) | Page route (layout + 10-row list, about 4 KB) |
|---|---|---|
| Framework work outside the encoder / `Execute` | as today | at most 3 allocations (the boxed `View`, the `Content-Length` value), 5 with a nonce (the CSP header value) |
| Encoder / template | `json.Marshal` | `html/template`: reflection per action; expect 10 to 40 µs and 100 to 400 allocations |
| Idle memory | none | Parse trees: about 2 to 3 times the source bytes of each page set (layout + page + the partials it reaches, not every partial); asset table about 120 bytes per file; the pool is empty after two GC cycles |
| Binary | | `html/template` is linked into every app once `core` imports it (about 1 MB); measured in step 9 |

Heavy techniques this design approves, each with its benchmark in `internal/render`:

| Technique | Where | Constraint | Benchmark |
|---|---|---|---|
| `sync.Pool` of `*bytes.Buffer` | the render buffer | A buffer grown past 256 KiB is dropped, not pooled. A write past 8 MiB fails the render (R11) | `BenchmarkRender_PooledBuffer` against a new buffer per render |
| In-place overwrite of a placeholder | `{{nonce}}` | The placeholder is 32 random hexadecimal characters drawn once per app, the same length and alphabet as a nonce, so every escaping context treats both alike. It is overwritten in every buffer of a set that uses `{{nonce}}` before any byte is written | `BenchmarkRender_Nonce` against a render without it; `TestRender_NonceEqualsDirectValue` proves the output equals a set whose function returns the nonce itself |

`loadshed`'s 503 for a browser renders the error page (about the cost of a small page) where JSON is a
fixed body. Step 9 measures it; if it matters, the fix is a pre-rendered built-in page for refusals, as a
new design.

## Concurrency

| State | Protection |
|---|---|
| Page sets, name table, asset table (`IsReload: false`) | Built in `Create()`, escaped there, never written again. Concurrent `Execute` on one set is safe in `html/template` |
| The same with `IsReload: true` | One `sync.RWMutex`: a render takes the read lock to check the fingerprint and pick its set; a change takes the write lock, re-parses, swaps. Renders in flight keep the set they picked. No lock exists in the production path |
| Render buffers | `sync.Pool`; a buffer belongs to one request from `Get` to `Put`, and is put back only after the `Write` returned |
| Nonce | Lives in the request's exchange; cleared by its reset |
| Placeholder | Written once in `EnableRender`, read-only after |
| `renders`, `failures` | `atomic.Int64` |
| `helmet`'s compiled nonce form | Built in `NewMiddleware()`, read-only; the shared static header slice is never mutated (a page with a nonce gets a new slice) |

No goroutine is started. Nothing to stop at shutdown.

## Security

| Risk | Decision |
|---|---|
| XSS through data | `html/template` contextual escaping (R7, R26). No user functions, so no helper can return `template.HTML` by accident; a value of type `template.HTML` in `Data` is the developer's explicit choice |
| Template name from a client | Names are keys of a table built at `Create()`; a request never opens a template file (R9) |
| Asset path traversal, dotfiles | The asset route serves `fs.Sub(FS, "assets")` through `SERVE`'s existing checks (R36). `fs.FS` rejects `..` by contract |
| Nonce quality and reuse | 128 bits from `crypto/rand` per response; never derived from the request; pages default to `Cache-Control: no-cache` so a shared cache does not replay a nonce (R5, R33) |
| Placeholder leak | It is overwritten before the only `Write`. A set that never calls `nonce` never contains it. A client cannot submit it without knowing it, and it is never logged or put in the snapshot |
| A nonce loosening a strict policy | `'none'` and `'unsafe-inline'` directives are not touched (R32) |
| Error detail to clients | R24: no cause text, no 500 detail, no template or parse error (R10, R14), no path echo. The request id is on the page so a user can quote it |
| `Accept` sniffing | Only selects the format of an error; both formats carry the same three values. `Vary: Accept` keeps caches correct |
| Stale assets | `immutable` only for a `?v=` that equals the file's hash at `Create()`; no guessing from file names (R37) |
| Memory by a large page | 8 MiB hard limit per render; 256 KiB pool retention |
| CSRF | Unchanged: Fetch-Metadata (`csrf.CSRF{}`), which must be bound by the app; the sample binds it |
| `IsReload` left on in production | Costs a directory walk per render and serves assets uncached; leaks nothing (errors still go to the log only) |

## Impact

| Area | Change |
|---|---|
| Existing apps | No call changes. Responses change only by R24: `error` no longer holds a cause's text, a 500 exception no longer sends its own message, three framework messages are reworded. Tests that pin the old text are updated in step 3 |
| `internal/core` | `fieldNameByRole["page"]`; `HTTPLayer.IsPage` → `httpRoute.isPage`; `writeResult` gains the page case next to `returnHTTP`; the built-in filter and `recoverRequest` / `answerNoRoute` call one error writer that picks the format (R16 to R18) |
| `internal/ctx` | Hooks only (package functions, per design 0017's hook rule): the page flag and the redirect default of a request. No new method on `HTTPContext` |
| `middlewares/helmet` | Compiles the nonce form of its policy in `NewMiddleware()`; `Use` hands it to the framework's writer. No new field, no new export |
| `internal/devtool` | `HTTPComponent.Kind`, `GetConfigurationResponse.Render` |
| Docs (step 8) | `README.md` (Pages section, the root alias table), a `render` reference (`internal/render/README.md`), `internal/core/README.md`, `middlewares/helmet/README.md` and `README.vi.md`, `middlewares/csrf/README.md` (forms), `.owlkei/request-pipeline.md`, `routing-system.md`, `handler-execution.md`, `package-reference.md`, `metadata-api-index.json`, `CLAUDE.md` (package map, controller roles, the `invalid render` and `invalid controller` categories, the error output rule) |

### Sample: `sample/pages` (step 7)

| File | Holds |
|---|---|
| `main.go` | `helmet`, `csrf`, `EnableRender` with `fs.Sub(embed, "web")`; `IsReload` and `os.DirFS("web")` when `PAGES_DEV=1` |
| `module.go`, `bookings_service.go` | The module and an in-memory provider |
| `bookings_pages.go` | `owlkei.Page` controller: list (`GET /bookings`), detail (`GET /bookings/{id}`, `NotFoundException` when missing), form (`GET /bookings/new`), submit (`POST /bookings`: 422 or 303). The builder checks each method name against `ParseFuncNameToURL` |
| `booking_form.go` | The `Form` pipe with `Bind`, `FieldLevel` and `Errors` |
| `web/layout.html`, `web/error.html`, `web/bookings/{list,detail,new}.html`, `web/partials/field.html` | Owlkei's HTML snippets (table, card, text field, button, alert) pasted as markup; an inline theme script with `nonce="{{nonce}}"`; `{{asset}}` links |
| `web/assets/owlky.min.css` | Copied from `owlkei/packages/styles/dist/owlky.min.css`: 221,798 bytes (29,817 gzip) |
| `web/assets/owlky.js` | Copied from `owlkei/packages/html/dist/owlky.js`, the CDN IIFE loader (it starts with `(function(){`): 133,808 bytes (41,851 gzip). `modules/` is not copied: the sample uses only components that `owlky.js` holds |
| `web/assets/OWLKY.md` | Source, date and the MIT license of the two copied files |
| `pages_test.go` | `TestPages_FormFlow`: form page 200 → invalid POST 422 with messages and values → valid POST 303 → the list shows the booking → unknown id is an HTML 404 → `owlky.js?v=<hash>` is immutable |

Owlkei Go's `go.mod` gains nothing. No Go file outside `sample/pages` names Owlkei.

## Test plan

Tests use `testing/fstest.MapFS` and `app.ServeHTTP` with `httptest`. Files: `internal/render/*_test.go`
(engine), `internal/core/http_page_test.go`, `http_page_error_test.go`, `http_page_form_test.go`,
`http_page_nonce_test.go`, `http_asset_test.go`, `trace_render_test.go`, `create_page_test.go`,
`middlewares/helmet/helmet_nonce_test.go`, `internal/devtool/devtool_render_test.go`, root
`surface_page_test.go`.

| Rule | Test | Cases |
|---|---|---|
| R1 | `TestPage_RoutesLikeHTTP`, `TestPage_LayersBind`, `TestSurface_PageController` | GET and POST handlers, `Prefix`, module prefix, version; a guard, an interceptor and a pipe on a page route; the root aliases compile |
| R2 | `TestCreate_PageAndHTTPPanics`, `TestCreate_PageWithoutEnableRenderPanics`, `TestCreate_PageHandlerMustReturnView`, `TestCreate_ViewNeedsPage` | each message; `(int, View)` and `SERVE` handlers accepted; `any` result refused |
| R3 | `TestEnableRender_NilPanics`, `TestEnableRender_TwicePanics`, `TestEnableRender_AfterCreatePanics` | nil options, nil `FS` |
| R4 | `TestCreate_TemplateParseErrorPanics`, `TestCreate_MissingPartialPanics`, `TestCreate_EscapeErrorPanics` | a page, a layout, a partial, `error.html`; the file path is in the message; an unclosed attribute context |
| R5 | `TestPage_RendersInsideLayout`, `TestPage_NearestLayoutWins`, `TestPage_NoLayout`, `TestPage_BlockOverride`, `TestPage_Headers`, `TestPage_KeepsHandlerCacheControl` | root and nested layout; a `title` block; nil `Data` |
| R6 | `TestPage_DefaultStatus200`, `TestPage_StatusFromContext`, `TestPage_StatusFromResult` | GET, POST; 422 both ways |
| R7 | `TestPage_EscapesByContext` | text, attribute, URL, script contexts; hostile strings |
| R8 | `TestPage_ViewIgnoredAfterResponse` | after `Redirect`, after a direct write |
| R9 | `TestPage_UnknownViewIs500` | unknown, empty, `error`, a partial name, `../x`; the log event; no file opened (an `FS` that counts `Open`) |
| R10 | `TestPage_ExecErrorSendsNoPartialPage` | failure after 10 KB of output: status 500, body has no marker from the page |
| R11 | `TestPage_OversizedPageIs500` | a range that outputs more than 8 MiB |
| R12 | `TestPage_UnknownAssetFails` | |
| R13 | `TestPage_InterceptorResult` | replaced by a string → 500; short-circuit with a `View` |
| R14 | `TestRender_NoReloadIgnoresChanges`, `TestRender_ReloadSeesChanges`, `TestRender_ReloadFailureIs500`, `TestRender_ReloadConcurrent` | change, add, remove; a broken file then fixed; 32 goroutines rendering while files change (`-race`) |
| R15 | `TestPage_Head` | |
| R16, R17 | `TestPageError_RouteKindDecides` | guard false, exception from guard, pipe, interceptor, handler, filter; page and API route; `Accept` is ignored here |
| R18 | `TestError_AcceptDecidesBeforeGuards`, `TestError_AcceptTable`, `TestError_VaryAccept`, `TestError_JSONWithoutEnableRender` | `csrf`, `hostcheck`, `loadshed`, a user middleware; 404, 405 with `Allow`, 410; `text/html`, `TEXT/HTML`, `text/html;q=0.9,*/*`, `*/*`, missing, `application/xhtml+xml`, two `Accept` lines |
| R19 | `TestPageError_ThrottlerFollowsRoute` | page and API route; `Retry-After` kept |
| R20 | `TestPageError_CustomTemplate`, `TestPageError_KeepsLayerHeaders`, `TestPageError_DropsBodyHeaders` | data keys; the layout around it |
| R21 | `TestPageError_BuiltIn` | valid document; no `<style>`, `<script>`, `<link>`; escaped message |
| R22 | `TestPageError_FallsBackToBuiltIn` | `error.html` fails; the layout fails with error data |
| R23 | `TestPageError_UserFilterFirst` | handles; re-panics |
| R24 | `TestClientText_Table`, `TestClientText_JSONShapeUnchanged`, `TestClientText_FrameworkMessages` | each row for JSON and HTML; the log events; body, form, file messages |
| R25 | `TestPageForm_BindsLikeAPI` | urlencoded, multipart |
| R26 | `TestPageForm_InvalidRerenders` | messages, kept values, hostile values |
| R27 | `TestPageForm_Redirect303`, `TestPageForm_ExplicitRedirectStatus`, `TestRedirect_APIStill301` | context method and parameter |
| R28 | `TestPageForm_BadBodyIsHTMLError` | malformed, over the limit |
| R29 | `TestPageForm_CSRF` | cross-origin HTML and JSON, same-origin |
| R30 | `TestNonce_OnlyWhenUsed` | API route, asset, page without `{{nonce}}`: header equals today's; `crypto/rand` not read (engine counter) |
| R31 | `TestNonce_DefaultPolicyHeader` | exact string |
| R32 | `TestHelmet_NonceDirectives` | each table row, `-elem` directives |
| R33 | `TestNonce_SameInHeaderAndBody`, `TestNonce_UniquePerResponse`, `TestRender_NonceEqualsDirectValue`, `TestNonce_Concurrent` | three uses in one page; 1,000 responses; 64 goroutines |
| R34, R35 | `TestNonce_WithoutHelmet`, `TestNonce_KeepsForeignCSP` | |
| R36 | `TestAsset_Serves`, `TestAsset_Refuses` | css, js, svg types; directory, missing, `.env`, `..%2f`, a template path |
| R37 | `TestAsset_ImmutableWithHash`, `TestAsset_NoCacheWithoutHash`, `TestAsset_ETag304` | right, wrong, missing `v` |
| R38 | `TestAsset_Reload` | |
| R39, R40 | `TestAsset_RouteConflict`, `TestAsset_NoDirNoRoute`, `TestAsset_GlobalLayersApply` | |
| R41 | `TestTrace_RenderEvent`, `TestTrace_RenderEventOnFailure`, `TestTrace_NoRenderEventWhenOff` | name, order, code |
| R42, R43 | `TestDevtool_PageKind`, `TestDevtool_RenderSnapshot` | with and without `EnableRender` |
| R44 | `TestHelmet_SnapshotNonceDirectives` | |
| API | `make api-check` | the nine lines above and nothing else |

## Open questions

None blocks the run. Decided here under the delegation; the maintainer can reverse each without moving a
public name:

| # | Decided | Alternative |
|---|---|---|
| 1 | D1: a role, no `RENDER` word | Roadmap 0008 lists "a DSL word" among the reserved slots; this design leaves that slot empty on purpose |
| 2 | D9: a 500 never sends the exception's own message (D-028 rule 4, as written) | Send the author's message for 500 too and hide only causes |
| 3 | D8: `throttler` follows the route kind, not `Accept` | The roadmap's scope lists it with the middleware refusals |
| 4 | D6: no user `FuncMap` | `RenderOptions.Funcs` later is one added field |
| 5 | R14: a template error is never shown in the browser, even with `IsReload` | Show it in development (better feedback, unsafe if the switch ships to production) |
| 6 | One layout per directory tree, no per-view layout choice | A `Layout` field on `View` later is one added field |
| 7 | R40: a global auth guard also covers `/assets/*` | The authentication roadmap decides whether assets are exempt |

## Amendment 1 (2026-11-18, approved by the maintainer): `RenderOptions.Dir`

`main` no longer needs `fs.Sub` or `os.DirFS`:

```go
//go:embed web
var web embed.FS

app.EnableRender(&owlkei.RenderOptions{FS: web, IsReload: os.Getenv("PAGES_DEV") == "1"})
```

| # | Rule |
|---|---|
| A1 | `RenderOptions.Dir` names the directory of `FS` that holds the templates and assets. Empty means `"web"`; `"."` means the root of `FS` |
| A2 | `EnableRender` panics `invalid render: RenderOptions.FS has no "<dir>" directory: embed it with //go:embed <dir>, or set Dir` when that directory is missing. Owlkei Go never creates files: a missing `web` directory already fails `go build` at the `//go:embed` line, and scaffolding belongs to a future CLI |
| A3 | With `IsReload` and an `embed.FS`, the directory `Dir` is read from disk with `os.DirFS(Dir)`, relative to the working directory, since embedded files never change. Any other `fs.FS` is used as given, so tests and custom file systems keep working. A missing directory on disk panics `invalid render: IsReload reads "<dir>" from disk, but there is no such directory under the working directory` |
| A4 | With `IsReload`, `Create()` logs one `Warn` `RenderReloadOn` (`dir`, `note`), so a reload left on in production shows in its log |

API: one more line, `reach core.RenderOptions, field Dir string`. Tests: `internal/core/render_dir_test.go`
(`TestRenderDir_*`).

## Amendment 2 (2026-11-18, approved by delegation: roadmap 0008 › "Pages by route"): pages by route

Status: Approved (by delegation, 2026-11-18: roadmap 0008 › Decisions). Roadmap steps 14 to 17 (18, 19 for
the migration). Paths come from design 0019 (kebab-case, `PathStyle`, `Path`), which lands first.

The route picks the template, as in Next.js. `owlkei.Page` and `owlkei.View` go away:

```go
type BookingPages struct {
	owlkei.HTTP
	Bookings BookingService
}

func (p BookingPages) RENDER_bookings() owlkei.Map {
	return owlkei.Map{"Bookings": p.Bookings.All()}
}

func (p BookingPages) RENDER_bookings_BY_id(param owlkei.Param) Booking {
	booking, ok := p.Bookings.Find(param.Get("id"))
	if !ok {
		panic(exception.NotFoundException("No booking has this number"))
	}
	return booking
}

func (p BookingPages) RENDER_new_OF_bookings() BookingForm { return BookingForm{Nights: "1"} }

func (p BookingPages) SUBMIT_new_OF_bookings(c owlkei.HTTPContext, form BookingForm) BookingForm {
	if len(form.Errors) == 0 {
		p.Bookings.Add(form)
		c.Redirect("/bookings")
	}
	return form
}
```

```
web/layout.html            web/layout.css
web/404.html               web/5xx.html
web/bookings/index.html    GET  /bookings
web/bookings/{id}.html     GET  /bookings/{id}
web/bookings/new.html      GET, POST /bookings/new     web/bookings/new.css  web/bookings/new.form.js
web/partials/field.html
web/assets/app.css         /assets/app.css
web/public/robots.txt      /robots.txt
```

### What changes in this design

| Part of 0018 | After amendment 2 |
|---|---|
| D1 (a role, no `RENDER` word) | Reversed: no role; `RENDER` and `SUBMIT` are operation words in a `owlkei.HTTP` controller. API and page routes may share one controller |
| D2, `owlkei.View` | Removed. The handler's result is the template's data; the template is bound to the route at `Create()` |
| D3 | Kept without `View`: status by `c.Status(code)` or the `(int, data)` result form; redirect by `c.Redirect` or the `owlkei.Redirect` parameter |
| D4 | `RENDER_`: 200. A `SUBMIT_` that renders: **422**. A redirect without an explicit 3xx: 303 |
| D5, file conventions | The table "File conventions" below replaces the old one: pages are named by their route, `error.html` → error pages by status, new `public/`, page CSS and JS |
| D6 | One more fixed function: `pageAssets` |
| D7 | The asset route also serves `/assets/_pages/…` |
| R1, R2 | Replaced by P1 to P9. The four `owlkei.Page` / `owlkei.View` panics are gone |
| R5, R7, R10 to R12, R15 | Kept, reading "the handler's result" for `View.Data` |
| R6 | Replaced by P7 and P14 |
| R8 | Kept: when the response is already written, the result is ignored |
| R9 | Removed: no name comes from a handler. A missing template is a `Create()` panic (P4) or a reload failure (P10) |
| R13 | Replaced by P6 |
| R14 | Kept; "`error.html`" reads "an error page"; P10 adds the template binding |
| R16, R17 | "Page route" now means a `RENDER_` or `SUBMIT_` route; every other route of a `owlkei.HTTP` controller is an API route. The public file routes (P24) and `/assets/_pages/…` answer errors as JSON (R17). R18, R19 unchanged |
| R20 to R22 | Replaced by P17 to P21 (`error.html` → `<code>.html` → `<N>xx.html` → built-in) |
| R23, R24 | Unchanged |
| R25 to R29 | Kept; the form post is a `SUBMIT_` (P12 to P16) |
| R36 to R40 | Kept; P33 and P34 add `_pages`; R39's "without an `assets` directory no route is added" becomes "…and without page CSS or JS" |
| R41 | `Name` is the template path without `.html` (`bookings/index`, `bookings/{id}`, `404`, `4xx`); `error (built-in)` unchanged |
| R42 | `kind` = `page` on `RENDER_` and `SUBMIT_` routes |
| R43 | `pages[]` gains `route` (final path) and `assets` (its CSS and JS files); `error_template` → `error_templates` (file names, absent when only the built-in page exists); new `public` (count) |
| Amendment 1 | Unchanged |

### Public API

Nothing is added. `RENDER`, `SUBMIT` and `pageAssets` are words, not identifiers. Exact lines `make api`
removes from `api/owlkei.txt` (with the six `+` lines of design 0019, any other diff is a defect):

```
-pkg owlkei, type Page = common.Page
-pkg owlkei, type View = common.View
-reach common.Page, method (*common.Page) Prefix(string, ...any) *common.Page
-reach common.View, field Data any
-reach common.View, field Name string
```

Internal (the tester may call it): `common.HTTPOperations` gains `RENDER` → `GET` and `SUBMIT` → `POST`,
so `ParseFuncNameToURL("RENDER_bookings", style)` returns `GET`, `/bookings/`, `""`;
`common.PageOperation(fnName string) string` returns `"RENDER"`, `"SUBMIT"` or `""`. `common.Page`,
`common.View`, `fieldNameByRole["page"]`, `checkViewResults`, `render.ErrUnknownPage` and
`render.ErrNoErrorPage`'s "no error.html" text go away; `HTTPLayer.IsPage` stays.

### File conventions inside `Dir` (replaces the table of "File conventions inside `RenderOptions.FS`")

| Path | Role |
|---|---|
| `**/layout.html` | Layout of the pages in that directory and below; the nearest one wraps the page (layouts do not nest) |
| `partials/**/*.html` (top level) | Partial, `{{template "partials/field" .}}` |
| `<400–599>.html`, `4xx.html`, `5xx.html` (root) | Error pages (P17) |
| `assets/**` | Static files at `/assets/…` with hashed URLs; never parsed. `assets/_pages` is reserved (P34) |
| `public/**` | Static files at the root URL under their own names; never parsed (P22) |
| Every other `**/*.html` | The page of the route with that path (P2). Without a route it stops `Create()` (P9) |
| `X.css`, `X.<any>.css`, `X.js`, `X.<any>.js` next to `X.html` (outside `assets/`, `public/`) | CSS and JS of that page, layout or error page (P28) |
| Any other file; any path with a segment starting with `.` (except `.well-known` under `public/` and `assets/`) | Ignored, never served |

### Behavior

Panics are raised by `Create()`, red, matched by tests on the uncolored text. `<T>.<M>` is the controller
type and method (`BookingPages.RENDER_bookings`); template paths are relative to `Dir`, with `/`.

#### Step 14: `RENDER_`

| # | Rule |
|---|---|
| P1 | In a `owlkei.HTTP` controller, a method named `RENDER` or `RENDER_<route>` is a page route: `GET` (and `HEAD`, R15) on the path design 0019 gives the same words after `READ` (resource words, `BY`, `AND`, `OF`, `PathStyle`, controller and module prefixes, `Path`). Guards, interceptors, pipes, filters and middlewares bind as on any route |
| P2 | Its template is the final path without the outer slashes, looked up among the files walked at `Create()` (compared byte for byte; no file system lookup by name): `<path>.html` or `<path>/index.html`. A parameter segment is written `{name}` in the file or directory name, with the route's parameter name. The table below lists the candidates |
| P3 | Without `EnableRender`: `invalid render: <T>.<M> is a page route, call EnableRender before Create` |
| P4 | No candidate exists: `invalid render: <T>.<M> needs a template: <a> or <b>` (the available candidates, one or two). Both exist: `invalid render: <T>.<M> has two templates: <a> and <b>`. No candidate is possible: `invalid render: <T>.<M> can have no template: <dir>/ is reserved` |
| P5 | `ANY`, `VERSION` or `FILE` in a `RENDER_` or `SUBMIT_` name: `invalid route: <W> in <Name>: a page route has no wildcard` / `has no version` / `has no file extension`. A final path with a `*` segment (from `Path` or a prefix): `invalid route: <T>.<M> is a page route and its path <route> holds a wildcard` |
| P6 | The data (`.` in the page and its layout) is the handler's only or last result after the interceptors, of any type: `owlkei.Map`, a struct, a slice, `nil`. A handler with no result renders with `nil`. A post-interceptor's replacement is the data; an interceptor that answers without calling the handler supplies it |
| P7 | The status of a `RENDER_` is 200 unless set by `c.Status(code)` or the `(int, data)` result form |
| P8 | With `EnableVersioning`, a page route answers whatever version the request carries, and a request without one (it is registered like the asset route, under the neutral version) |
| P9 | A `.html` file that is not a layout, a partial, an error page or the template of a route, outside `assets/` and `public/`: `invalid render: <file> has no RENDER_ route: add one, move it to partials/, or delete it`. So `error.html` and a misspelled `bookings/detial.html` stop the boot |
| P10 | `IsReload`: a reload re-binds every page route by P2. A route whose template is now missing or doubled, a parse error, or a set that lost its `{{pageAssets}}` (P32) fails the reload as R14 does (fixed 500 HTML, `RenderReloadFailed` with `file` and `cause`, until fixed). P9 and the orphan check of P30 are **not** repeated on reload: a file added on disk without a route is ignored until the restart that adds its handler |
| P11 | `owlkei.Page` and `owlkei.View` do not exist. A controller field named `Page` has no meaning to the framework |

| Final path | Candidates |
|---|---|
| `/` (`RENDER`, or only prefixes) | `index.html` (with prefixes: `<prefix>.html`, `<prefix>/index.html`) |
| `/bookings/` | `bookings.html`, `bookings/index.html` |
| `/bookings/{id}/` | `bookings/{id}.html`, `bookings/{id}/index.html` |
| `/admin/users/` (`Prefix("admin")` + `RENDER_users`) | `admin/users.html`, `admin/users/index.html` |
| `/court-bookings/` (`RENDER_court_bookings`; `/court_bookings/` as written) | `court-bookings.html`, `court-bookings/index.html` |
| Last segment `index` or `layout` (`/courts/layout/`) | `<path>/index.html` only: `courts/layout.html` is a layout, `x/index.html` belongs to `/x/` |
| First segment `partials`, `assets` or `public` | `<segment>.html` for the one-segment path (`/assets/` → `assets.html`); none below it (P4, third message) |
| One segment of three digits or a digit and `xx` (`RENDER_404`, `RENDER_5xx`), after all prefixes | Panic (P20) |

Every template file therefore serves exactly one route.

#### Step 15: `SUBMIT_`

| # | Rule |
|---|---|
| P12 | `SUBMIT` or `SUBMIT_<route>` is a page route: `POST` on the final path of the method `RENDER` / `RENDER_<route>` of the **same controller** with the same words, rendered with that route's template. `SUBMIT_new_OF_bookings` posts to `/bookings/new/` |
| P13 | No such `RENDER_`: `invalid handler: <T>.<SUBMIT name> needs <T>.<RENDER name>: a SUBMIT_ posts to the page that shows its form` |
| P14 | The handler redirected (`c.Redirect(url)` or the `owlkei.Redirect` parameter): 303 unless a 3xx was set, and the result is ignored (R8, R27). Otherwise the result is rendered (P6) with status **422**, unless set by `c.Status(code)` or `(int, data)` |
| P15 | A `SUBMIT_` has no path of its own: `Path` or a targeted `Prefix` naming it panics `invalid route: Path("<v>") is bound to '<Name>': a SUBMIT_ uses the path of its RENDER_` (the same text with `Prefix("<v>")`). `Path` and prefixes on its `RENDER_` move both |
| P16 | Layers are bound per handler as for any route: `BindGuard(g, instance.RENDER_x)` covers the GET only, `BindGuard(g)` both. `PUT` on the page's path answers 405 whose `Allow` holds `GET` and `POST`. Edit and delete forms post to the page that shows them (one `SUBMIT_` per page; a submit button's `name` tells them apart); there is no `_method` override. `CREATE_`, `UPDATE_`, `DELETE_` stay API routes: JSON errors, 201, 301 |

#### Step 16: error pages by status

| # | Rule |
|---|---|
| P17 | An HTML error with code `C` (R16, R18) renders the first that exists at the root of `Dir`: `<C>.html`, then `<N>xx.html` (`N` = `C` / 100), then the built-in page (R21). `error.html` has no role (P9). The table is built at `Create()`; a request opens no file |
| P18 | An error page is rendered inside the root `layout.html` with `.Code`, `.Status`, `.Message`, `.RequestID`, and R20's headers. It is parsed and escaped at `Create()` (R4) |
| P19 | When rendering the chosen error page or its layout fails, the built-in page is sent with the same code and `RenderFailed` (`template` = `404`, `4xx`…) is logged; the next file of P17 is not tried |
| P20 | Reserved names at the root of `Dir`: three digits `.html`, a digit and `xx.html`. A page route whose final path, after module and controller prefixes, is that one segment: `invalid render: <T>.<M> resolves to <name>.html, which is an error page, not a route`. Under a prefix (`admin/404.html`) the name is an ordinary page |
| P21 | A reserved name that no status can use (`200.html`, `302.html`, `3xx.html`, `600.html`): `invalid render: <file> is not an error page: use 400.html to 599.html, 4xx.html or 5xx.html` |

| Files at the root | 404 | 422 from a guard | 500 | 503 |
|---|---|---|---|---|
| none | built-in | built-in | built-in | built-in |
| `404.html` | `404.html` | built-in | built-in | built-in |
| `404.html`, `4xx.html`, `5xx.html` | `404.html` | `4xx.html` | `5xx.html` | `5xx.html` |
| `503.html`, `5xx.html` | built-in | built-in | `5xx.html` | `503.html` |

#### Step 17: `public/` and page CSS and JS

| # | Rule |
|---|---|
| P22 | Every file under `Dir/public` is served by `GET` and `HEAD` at its path below `public`: `public/robots.txt` → `/robots.txt`, `public/.well-known/security.txt` → `/.well-known/security.txt`. Files with a segment starting with `.` other than `.well-known` are not served. A directory has no index: `public/docs/index.html` is `/docs/index.html` only. `.html` files here are sent as they are, never parsed |
| P23 | Each file is a route of its own, added at `Create()` after the controllers' routes under the name `public/<path>`. A file whose method and path shape equal a route's: `route conflict: public/robots.txt method is conflicted with READ_robots_txt_FILE method`. With `EnableVersioning` the public routes are version-neutral |
| P24 | Response: the content type of the extension, `Cache-Control: no-cache`, `ETag: "<16 hex>"` (first 8 bytes of the SHA-256, hashed at `Create()`), 304 for a matching `If-None-Match`. `IsReload`: `no-cache`, no `ETag`, content read on each request; a file added after `Create()` answers 404 until the restart. Errors are JSON (R17); global middlewares and global guards apply (as R40); the routes appear in the startup route log and the devtool snapshot |
| P25 | Without a `public` directory nothing is added |
| P28 | In a directory outside `assets/` and `public/`, a file `F.css` or `F.js` belongs to the template `T.html` of the same directory (a page bound to a route, a `layout.html`, a root error page) with the longest `T` such that `F` = `T` or `F` starts with `T.`: `new.css`, `new.form.js` → `new.html`; `layout.css` → `layout.html`; `{id}.print.css` → `{id}.html`; `index.css` → `index.html` |
| P29 | `{{pageAssets}}` writes the tags of the set being rendered: the files of the layout that wraps the page, then the files of the page (or error page), each group sorted by file name (byte order). A `.css` file is `<link rel="stylesheet" href="U">`, a `.js` file `<script src="U" defer></script>`, joined by `\n`, with `U` = `/assets/_pages/<path in Dir>?v=<16 hex>` (`IsReload`: no `?v=`), each path segment percent-encoded (`{id}.css` → `%7Bid%7D.css`) and the value attribute-escaped. A set that owns no file writes nothing. The string is built once per page set, at `Create()` and at each reload |
| P30 | A `.css` or `.js` file there that belongs to no template (also any under `partials/`): `invalid render: <file> belongs to no template: name it after a page or layout of its directory (X.css, X.<name>.css), or move it to assets/` |
| P31 | The nonce is not involved: the tags are same-origin files, allowed by `'self'`. A page with `{{pageAssets}}` and no `{{nonce}}` draws no nonce (R30) |
| P32 | A set that owns at least one file and in which no template (layout, page, reached partial) calls `{{pageAssets}}`: `invalid render: <page file> has <first owned file>, but no template of the page calls {{pageAssets}}: add it to <layout file>` (without a layout: `…: add it to the page`) |
| P33 | `GET` and `HEAD /assets/_pages/<path>` serve exactly the files of P28 with R36 to R38 (`immutable` for a matching `?v=`, else `no-cache`; `ETag`; 304). Any other path under `_pages` (a template, a partial, an unowned file) is 404 JSON. The asset route exists when `assets/` exists **or** a page CSS or JS file does |
| P34 | A file or directory `assets/_pages` in `Dir`: `invalid render: assets/_pages is reserved for page CSS and JS`. `{{asset "_pages/…"}}` is an unknown asset (R12) |

`/bookings/new` in the tree above, with `layout.html` calling `{{pageAssets}}` in its `<head>`:

```html
<link rel="stylesheet" href="/assets/_pages/layout.css?v=…">
<link rel="stylesheet" href="/assets/_pages/bookings/new.css?v=…">
<script src="/assets/_pages/bookings/new.form.js?v=…" defer></script>
```

### Performance

| Path | Kind | Cost |
|---|---|---|
| A `RENDER_` / `SUBMIT_` request | warm | No more than today: the page set is found by the route's template path (a pointer kept on the route when `IsReload` is off, one map lookup in the current site when it is on); no `View` is boxed. `{{pageAssets}}` is a function returning a constant string: the 1 allocation of any template function call (step 2's measurement), nothing else |
| An HTML error | warm | One map lookup by code, one array index by class, then as today |
| A public file | warm | The trie match of any static route, then `http.ServeContent` from the `fs.FS`; no hashing per request |
| `/assets/_pages/…` | warm | One more branch in the asset handler (a prefix test), then the existing path |
| `Create()` | cold | One more walk result kept (CSS and JS names), SHA-256 of each public and page file, one route per public file |
| Idle | | No goroutine. Memory: one route item per public file (keep `public/` to the handful of files that need a fixed URL; bulk files belong in `assets/`), about 120 bytes per page CSS or JS file, one prebuilt tag string per page set |
| API routes, apps without `EnableRender` | hot | Unchanged |

No new heavy technique. `BenchmarkRender_*` and `bench/page_test.go` are re-run in step 18 against
`docs/review/http-0008.md` (expected: equal within noise).

### Concurrency

| State | Protection |
|---|---|
| Route → page set, error page tables, page asset table, tag strings (`IsReload: false`) | Built in `Create()`, read-only after |
| The same with `IsReload` | Part of the `site` swapped under the engine's existing `RWMutex`; a render in flight keeps the site it picked. Routes hold the template's **path**, resolved in the current site, never a set pointer that a reload would leave stale |
| Public file table | Built in `Create()`, read-only (the route list is fixed until restart, also with `IsReload`) |

No goroutine is added.

### Security

| Risk | Decision |
|---|---|
| Template chosen by a request | Impossible: bound to the route at `Create()` (stronger than R9's table lookup) |
| Templates, partials, `.map` or other files leaking through `/assets/_pages` | Only files of the page asset table (owned `.css` / `.js`) are served (P33) |
| `public/` | Serves what the developer put there, at the root URL: hidden segments refused except `.well-known`, no directory listing, no index, names fixed at `Create()` (a file written there at run time is not served). Nothing in it is escaped or gets a nonce: never write user content there |
| `//go:embed web` skips `.well-known` (names starting with `.` or `_`) | Verified with `go run`: only `//go:embed all:web` embeds it, which also embeds every dotfile under `web` into the binary. They are never served (hidden segment rules), but they ship: docs say so; backlog |
| A guard bound to `RENDER_x` only | Its `SUBMIT_x` is not covered (P16). Documented; backlog proposes a `Create()` check |
| `SUBMIT_` without `csrf` | Unchanged from R29: the app binds `csrf.CSRF{}`; backlog item "SSR without protection" |
| File names in tags | Percent-encoded and attribute-escaped although they come from the app's own `FS` (P29) |
| Page CSS and JS URLs reveal the template tree (`/assets/_pages/admin/…`, parameter names) and are fetched without the page's guards | Accepted for static files; a global guard covers them (R40). Backlog, for the authentication roadmap |
| Error pages | Same data as before, no cause text (R24). A `5xx.html` that fails falls back to the built-in page (P19) |
| Cost of a refusal | A browser 503 from `loadshed` still renders a template (0018 › Performance); unchanged, backlog |

### Impact and order

| Step | Work | Red tests first |
|---|---|---|
| 14 | `RENDER` in `HTTPOperations`; binding route → template in `internal/render` (walk result, candidates, P4, P9) and `container.bindHTTPController`; `writePage` takes any result; remove `Page`, `View`, `fieldNameByRole["page"]`, `checkViewResults`; neutral version; tests of steps 2 to 6 rewritten on `RENDER_` (same assertions) | `TestRender_*` of the plan, `make api-check` |
| 15 | `SUBMIT`: pairing, 422 default, P15 | `TestSubmit_*` |
| 16 | Error page tables, P20, P21; `error.html` → status files in tests | `TestErrorPage_*` |
| 17 | `public/` routes; page asset table, `pageAssets`, `_pages` serving | `TestPublic_*`, `TestPageAssets_*` |
| 18 | Migration list | `make check` |

Migration list (grep at `9fe673b`; counts are occurrences of `Page` / `View` / `error.html` / old page names):

| File | What |
|---|---|
| `aliases.go`, `internal/common/page.go`, `internal/common/http.go` | Remove `Page`, `View`; delete `page.go`; `HTTPOperations` gains two words |
| `internal/core/container.go` (17), `http_page.go` (5), `http.go`, `http_error.go`, `http_asset.go`, `default.go` | The code of the steps above; `errorTemplateName`, `errNotView` go |
| `internal/render/parse.go`, `render.go` | `errorFile`, `kindError`, `ErrUnknownPage`, `ErrNoErrorPage`, `Render(name, …)`, `RenderError`; per-set `pageAssets`; the page asset table |
| `internal/ctx/http_context.go`, `internal/devtool/devtool.go`, `devtool_builder.go` | The page flag stays; `Render.ErrorTemplate` → `ErrorTemplates`, `RenderPage.Route`, `RenderPage.Assets`, `Render.Public` |
| `internal/core/create_page_test.go` (12 tests), `http_page_test.go` (22), `http_page_error_test.go` (19), `http_page_form_test.go` (7), `http_page_nonce_test.go` (6), `http_page_reload_test.go` (4), `devtool_render_test.go` (4), `trace_render_test.go` (4), `http_asset_test.go` (10), `render_dir_test.go` (6) | Controllers embed `HTTP` and use `RENDER_` / `SUBMIT_`; map file systems name templates by route and `error.html` by status. Deleted: `TestCreate_PageAndHTTPPanics`, `TestCreate_PageHandlerMustReturnView`, `TestCreate_ViewNeedsPage`, `TestPage_UnknownViewIs500`; `TestCreate_PageWithoutEnableRenderPanics` and `TestPage_InterceptorResult` change meaning (P3, P6) |
| `internal/render/render_test.go` (8), `render_nonce_test.go` (9), `render_bench_test.go` (2) | The engine's new entry points |
| `surface_page_test.go`, `bench/page_test.go`, `guards/throttler/throttler_page_test.go`, `middlewares/helmet/helmet_nonce_test.go` (11), `internal/devtool/devtool_render_test.go` | The same rewrite |
| `sample/pages/bookings_pages.go` | The four handlers as at the top of this amendment |
| `sample/pages/web` | `bookings/list.html` → `bookings/index.html`; `bookings/detail.html` → `bookings/{id}.html`; `error.html` → `4xx.html` and `5xx.html`; `bookings/new.html`: `action="/bookings/new"`; a `public/robots.txt`; a small `layout.css` with `{{pageAssets}}` in `layout.html` to show P29 |
| `sample/pages/pages_test.go`, `sample/pages/README.md` | The POST goes to `/bookings/new`; the new file names |
| `README.md` › Pages (36 occurrences: "Render a page", "File conventions", "Status codes and redirects", "Error pages", "Forms", "Assets"), › "Routes from method names" (`RENDER`, `SUBMIT` rows), › Static files, the package reference (`Page`, `View` rows out) | Rewritten on this amendment; every example compiles |
| `internal/render/README.md` (15), `internal/core/README.md` (14), `internal/common/README.md` (`Page` and `View` sections out), `internal/devtool/README.md`, `internal/trace/README.md`, `exception/README.md`, `middlewares/csrf/README.md`, `middlewares/helmet/README.md` and `README.vi.md` | The same |
| `.owlkei/routing-system.md`, `request-pipeline.md`, `handler-execution.md`, `package-reference.md`, `metadata-api-index.json`, `anti-patterns-gotchas.md` | Pages by route, error pages by status, the new panics |
| `CLAUDE.md` | Package map (aliases row), "Controller" and "Page" bullets, the error output paragraph, the panic list (`invalid controller:` goes; `invalid handler:` and `invalid render:` get the new messages) |
| `docs/roadmap/backlog.md` › "Shared layout data for pages" | Wording only (`View.Data` → the handler's result) |
| `api/owlkei.txt` | `make api` (the five `-` lines) |

### Test plan

Files: `internal/core/http_render_test.go`, `create_render_test.go`, `http_submit_test.go`,
`http_error_page_test.go`, `http_public_test.go`, `http_page_assets_test.go`, `internal/render/*_test.go`,
`sample/pages/pages_test.go`. Tests use `fstest.MapFS` with `Dir: "."` and `app.ServeHTTP`.

| Rule | Test | Cases |
|---|---|---|
| P1 | `TestRender_IsGetPage`, `TestRender_MixedController`, `TestRender_LayersBind` | GET, HEAD; a `READ_` JSON route and a `RENDER_` in one controller; guard, interceptor, pipe |
| P2 | `TestRender_TemplateByRoute` | each row of the candidates table: root, `.html` form, `index.html` form, `{id}` file and `{id}` directory, module prefix, controller prefix, kebab and as-written, `Path`, `index` / `layout` last segment, `assets.html` |
| P3, P4 | `TestCreate_RenderWithoutEnableRenderPanics`, `TestCreate_RenderMissingTemplatePanics`, `TestCreate_RenderTwoTemplatesPanics`, `TestCreate_RenderReservedDirPanics` | each message; a wrong parameter name in the file (`{slug}.html` for `{id}`) is "missing" |
| P5 | `TestCreate_PageRouteWordsPanic`, `TestCreate_PageRouteWildcardPathPanics` | `ANY`, `VERSION`, `FILE` on `RENDER_` and `SUBMIT_`; `Path("/docs/*")` |
| P6, P7 | `TestRender_DataIsResult`, `TestRender_NoResult`, `TestRender_StatusForms`, `TestRender_InterceptorData` | map, struct, slice, nil, no result; `c.Status`, `(int, data)`; replaced and short-circuited results; hostile strings stay escaped (R7) |
| P8 | `TestRender_VersionNeutral` | header versioning: with a version, another version, none → 200 |
| P9 | `TestCreate_UnboundTemplatePanics` | a stray page, `error.html`; a file under `assets/` and `public/` does not panic |
| P10 | `TestRender_ReloadRebinds`, `TestRender_ReloadMissingTemplateIs500`, `TestRender_ReloadIgnoresNewUnboundFile` | move `x.html` to `x/index.html`; delete then restore; add a stray file |
| P11 | `make api-check`, `TestSurface_PagesByRoute` | the root package has no `Page`, `View`; the example of this amendment compiles |
| P12, P13 | `TestSubmit_PostsToRenderPath`, `TestCreate_SubmitWithoutRenderPanics` | with prefixes, a parameter, `Path` on the `RENDER_`; a `RENDER_` in another controller does not count |
| P14 | `TestSubmit_Rerenders422`, `TestSubmit_Redirect303`, `TestSubmit_StatusOverride` | values and messages kept and escaped (R26); context method and parameter; `c.Status(200)` |
| P15, P16 | `TestCreate_SubmitPathPanics`, `TestSubmit_GuardBoundToRenderOnly`, `TestSubmit_OtherMethodIs405` | `Path`, targeted `Prefix`; the guard does not run on POST; `Allow` |
| P17 | `TestErrorPage_ByStatus` | the four rows of the table × the four codes; a guard refusal, a 404 with `Accept: text/html`, a handler panic |
| P18, P19 | `TestErrorPage_DataAndLayout`, `TestErrorPage_FailureFallsBackToBuiltIn`, `TestCreate_ErrorPageParseErrorPanics` | a failing `404.html` with `4xx.html` present → built-in |
| P20, P21 | `TestCreate_RenderReservedNamePanics`, `TestCreate_UnusableErrorNamePanics` | `RENDER_404`, `RENDER_5xx`; a prefix that removes the conflict; a `Path("/404")`; `200.html`, `3xx.html` |
| P22, P24, P25 | `TestPublic_ServesAtRoot`, `TestPublic_Refuses`, `TestPublic_ETag304`, `TestPublic_Reload`, `TestPublic_NoDirNoRoute`, `TestPublic_GlobalLayersApply` | `robots.txt`, `.well-known/security.txt`, a nested file, raw `.html`; `.env`, a directory, `..%2f`, POST → 405 |
| P23 | `TestPublic_RouteConflict`, `TestPublic_VersionNeutral` | against a `READ_…_FILE` route and a `RENDER_` route; the message |
| P28, P29 | `TestPageAssets_Tags`, `TestPageAssets_Ownership`, `TestPageAssets_EncodedNames`, `TestPageAssets_Reload` | the example above byte for byte; longest owner; `{id}.css`; a set without files writes nothing; no `?v=` with `IsReload` |
| P30, P32, P34 | `TestCreate_OrphanPageAssetPanics`, `TestCreate_MissingPageAssetsCallPanics`, `TestCreate_ReservedPagesDirPanics` | a typo, a file under `partials/`; with and without a layout; the call inside a partial counts |
| P31 | `TestPageAssets_NoNonce` | `crypto/rand` not read (the engine counter of R30) |
| P33 | `TestPageAssets_Served`, `TestPageAssets_OnlyOwnedFiles`, `TestPageAssets_RouteWithoutAssetsDir` | immutable with `?v=`, `no-cache` without, 304; a template path and a partial → 404 |
| Sample | `TestPages_FormFlow` | form 200 → invalid POST `/bookings/new` 422 → valid POST 303 → list → unknown id is `4xx.html` → `/robots.txt` → page CSS linked after the layout's |

### Open questions

None blocks the run. Decided here under the delegation:

| # | Decided | Alternative |
|---|---|---|
| 1 | P9: a template without a route stops `Create()` | Ignore it with one `Warn` |
| 2 | P14: a `SUBMIT_` that renders answers 422 by default | 200, and the handler sets 422 as today |
| 3 | P12: `SUBMIT_` pairs with the `RENDER_` of the same controller by its words | Pair by final URL across controllers |
| 4 | P5: `FILE` is refused on pages, with `ANY` and `VERSION` | Allow `RENDER_feed_xml_FILE` (template `feed.xml.html`, still `text/html`) |
| 5 | The candidates table: `index` and `layout` as last segment use the directory form; a reserved directory blocks only what lies inside it | Refuse those URLs outright |
| 6 | P29: one function writes CSS and JS together, scripts with `defer` | Two functions (`pageStyles`, `pageScripts`) |
| 7 | P24: a public file added on disk with `IsReload` needs a restart | A catch-all route for `public/`, which could no longer report conflicts at `Create()` |
