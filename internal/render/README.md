# Render

Internal package: applications use `RENDER_` / `SUBMIT_` routes, `ginject.RenderOptions` and `App.EnableRender` instead.

*Render is a part of `Ginject` framework: it turns the `.html` files of an `fs.FS` into `html/template` sets at `Create()`, binds each page route to the template its path names, renders a page by route into a pooled buffer, picks the error page of a status, and holds the asset, public file and page CSS and JS tables and the template functions.*

- [Render](#render)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [File conventions](#file-conventions)
  - [Template functions](#template-functions)
  - [`Engine`](#engine)
    - [New](#new)
    - [Load](#load)
    - [Bind](#bind)
    - [CheckBindings](#checkbindings)
    - [Render](#render-1)
    - [RenderError, ErrorTemplate](#rendererror-errortemplate)
    - [Release](#release)
    - [File tables](#file-tables)
    - [Snapshot, Counters, NonceDraws](#snapshot-counters-noncedraws)
  - [`Buffer`](#buffer)
  - [`FileError`](#fileerror)
  - [Errors](#errors)
  - [How it works](#how-it-works)
  - [Concurrency](#concurrency)
  - [Performance notes](#performance-notes)

## Key Features
- Every layout, page, partial and error page is read and parsed once, at `Load`; a request never opens a file
- The route picks the template: `Bind("/bookings/{id}")` finds `bookings/{id}.html` or `bookings/{id}/index.html` among the files walked at `Load`; a request path never does
- Parse errors, references to templates that do not exist, `html/template` escaping errors, a template without a route, a CSS or JS file that belongs to no template and a page set whose files no template links are startup errors that name the file
- Error pages by status: `<code>.html`, then `<N>xx.html`, then the caller's built-in page
- `{{pageAssets}}` writes the `<link>` and `<script>` tags of the CSS and JS files named after the layout and the page, built once per page set
- A page is executed into a pooled buffer: a failed or oversized page leaves nothing to send, so the caller never writes half a page
- With `isReload`, a render after a template file was added, removed or changed uses the new files; nothing runs while idle
- A page set that calls `{{nonce}}` gets a fresh 128-bit nonce per render, the same at every use; a set that never calls it draws none

## Usage

`internal/core` is the only caller: `App.EnableRender` calls `New`; `App.Create` calls `Load`, then `Bind` for each `RENDER_` and `SUBMIT_` route, then `CheckBindings`; a page route calls `Render` with its route, writes the bytes and calls `Release`; an HTML error calls `RenderError` with its status (falling back to its built-in page on any error).

```go
package main

import (
	"fmt"
	"testing/fstest"

	"github.com/dangduoc08/ginject/internal/render"
)

func main() {
	site := fstest.MapFS{
		"layout.html":        {Data: []byte(`<main>{{template "content" .}}</main>`)},
		"bookings/index.html": {Data: []byte(`<p>{{.Who}}</p>`)},
	}
	engine := render.New(site, false)
	if err := engine.Load(); err != nil {
		panic(err)
	}
	name, err := engine.Bind("/bookings/")
	if err != nil {
		panic(err)
	}
	if err := engine.CheckBindings(); err != nil {
		panic(err)
	}

	buf, err := engine.Render("/bookings/", map[string]any{"Who": "<reader>"})
	if err != nil {
		panic(err)
	}
	fmt.Println(name, string(buf.Bytes()))
	engine.Release(buf)
}
```

Output: `bookings/index <main><p>&lt;reader&gt;</p></main>`.

## File conventions

| Path | Role |
|---|---|
| `layout.html` in any directory | Layout of the pages in that directory and below; the nearest one wraps the page (layouts do not nest) |
| `partials/**/*.html` (top-level `partials` only) | Partial, included with `{{template "partials/field" .}}` (its path without `.html`) |
| `<400–599>.html`, `4xx.html`, `5xx.html` (root) | Error pages, inside the root `layout.html`. Another three-digit or `<digit>xx` name at the root (`200.html`, `3xx.html`, `600.html`) fails `Load` |
| `assets/**` | Static files; never parsed. `assets/_pages` is reserved |
| `public/**` | Static files served at the root URL; never parsed |
| `X.css`, `X.<any>.css`, `X.js`, `X.<any>.js` next to `X.html` (outside `assets/` and `public/`) | CSS and JS of that page, layout or root error page; the longest matching `X` owns the file |
| every other `**/*.html` | The page of the route with that path (`bookings/{id}.html` for `/bookings/{id}`) |
| anything else; any path with a segment starting with `.` | Ignored |

- The page file is the template `content`; a layout places it with `{{template "content" .}}`.
- A page's `{{define "title"}}` replaces the layout's `{{block "title" .}}`; a page without it keeps the block's default.
- A page without a layout is rendered alone.
- A page set holds its layout, its page and the partials they reach, not every partial.

### Template of a route

| Route (outer slashes ignored) | Candidates |
|---|---|
| `/` | `index.html` |
| `/bookings` | `bookings.html`, `bookings/index.html` |
| `/bookings/{id}` | `bookings/{id}.html`, `bookings/{id}/index.html` |
| last segment `index` or `layout` (`/courts/layout`) | `courts/layout/index.html` only |
| `/assets`, `/partials`, `/public` | `assets.html` (and so on) only |
| below `assets/`, `partials/`, `public/` | none: `ErrReservedDir` |
| one segment of three digits or `<digit>xx` (`/404`, `/5xx`) | none: `ErrErrorPageName`. `admin/404.html` is an ordinary page |

## Template functions

| Function | Result |
|---|---|
| `{{nonce}}` | The render's nonce: 32 lowercase hexadecimal characters from `crypto/rand`, the same at every use in one render, different on every render. The set writes a per-engine placeholder of the same length and alphabet, which `Render` overwrites in the buffer (see [How it works](#how-it-works)) |
| `{{asset "css/app.css"}}` | `/assets/css/app.css?v=<16 hex>`, the first 8 bytes of the file's SHA-256; with `isReload`, `/assets/css/app.css`. A file that is not in `assets/` fails the render |
| `{{dict "Name" "email" "Value" .Email}}` | `map[string]any`; an odd number of arguments or a non-string key fails the render |
| `{{pageAssets}}` | The tags of the set being rendered: the files of its layout, then those of its page or error page, each group sorted by path. A `.css` file is `<link rel="stylesheet" href="U">`, a `.js` file `<script src="U" defer></script>`, joined by a newline, with `U` = `/assets/_pages/<path>?v=<16 hex>` (no `?v=` with `isReload`), each path segment percent-encoded (`{id}.css` → `%7Bid%7D.css`). A set that owns no file writes nothing. The string is built once per set; it draws no nonce |

## `Engine`

The template sets of one `fs.FS`, the asset table and the buffer pool.

### New

`New(fsys fs.FS, isReload bool) *Engine` creates an engine. It reads nothing; it draws the `nonce` placeholder.

#### Parameters

| Position | Type | Description |
|---|---|---|
| 1 | `fs.FS` | The templates and the `assets` and `public` directories |
| 2 | `bool` | Re-parse when a template file changed (development) |

#### Returns

| Position | Type | Description |
|---|---|---|
| 1 | `*Engine` | The engine, not loaded yet |

### Load

`Load() error` lists every file under `assets/` and `public/` (hidden segments skipped, `.well-known` kept) and hashes each one (not with `isReload`), reads and parses every template file, assigns each page CSS and JS file to its owner and hashes it, builds each page and error page set and executes it once into `io.Discard` with `nil` data, so `html/template` escapes it now. Call it once, before `Bind`.

#### Returns

| Position | Type | Description |
|---|---|---|
| 1 | `error` | `nil`; a `*FileError` naming the file (a parse error, a missing template, an escaping error, an unknown asset reached with `nil` data); or an error whose text starts with the file |

#### Rules
From the tests (`render_test.go`, `render_pages_test.go`):
- A parse error in a page, a layout (also one no page uses), a partial (also an unused one) or an error page fails `Load` with the file path in the message.
- `{{template "partials/nope" .}}` for a partial that does not exist fails `Load`.
- An unclosed attribute (`<a href='{{.X}}`) is an escaping error and fails `Load`.
- `notes.txt`, `.secret.html`, `.git/config.html`, `assets/page.html` and `public/y.html` are never parsed.
- `200.html`, `302.html`, `399.html`, `600.html`, `000.html`, `1xx.html`, `3xx.html`, `6xx.html`: `<file> is not an error page: use 400.html to 599.html, 4xx.html or 5xx.html`.
- A file or directory `assets/_pages`: `assets/_pages is reserved for page CSS and JS`; `assets/pages` and `assets/_other.css` are fine.
- A `.css` or `.js` file outside `assets/` and `public/` that no template of its directory owns (`bookings/nw.css` next to `bookings/new.html`, `orphan.js`, any file under `partials/`): `<file> belongs to no template: name it after a page or layout of its directory (X.css, X.<name>.css), or move it to assets/`.
- A set that owns a file and in which no template (layout, page, reached partial) calls `{{pageAssets}}`: `<page> has <first file>, but no template of the page calls {{pageAssets}}: add it to <layout>` (without a layout: `…: add it to the page`). A set that owns no file needs no call.
- Other errors of the `nil`-data execution (a failed `dict`, a missing field) are not startup errors: they fail the render.

### Bind

`Bind(route string) (string, error)` binds a page route to its template and returns the template path without `.html` (`bookings/{id}`). Binding the same route twice returns the same name.

#### Rules
- The candidates are in [Template of a route](#template-of-a-route); they are looked up among the files walked by `Load`, byte for byte.
- No candidate exists: an error that `errors.Is` `ErrNoTemplate`, text `needs a template: bookings.html or bookings/index.html` (one candidate for `/`). A parameter named differently in the file (`{slug}.html` for `{id}`) is missing.
- Both exist: `ErrTwoTemplates`, `has two templates: bookings.html and bookings/index.html`.
- Below a reserved directory: `ErrReservedDir`, `can have no template: partials/ is reserved`.
- An error page name: `ErrErrorPageName`, `resolves to 404.html, which is an error page, not a route`. `40` and `4x` are ordinary pages.

### CheckBindings

`CheckBindings() error` reports every page template that no route bound, in path order, joined by newlines: `bookings/detail.html has no RENDER_ route: add one, move it to partials/, or delete it`. Layouts, partials, error pages and files under `assets/` and `public/` are never reported; `error.html` has no role and is reported.

### Render

`Render(route string, data any) (*Buffer, error)` executes the template bound to `route` with `data` into a pooled buffer.

`RenderPage(route string, data any) (*Buffer, string, error)` does the same and also returns the name of the template that rendered (`x/index.html` once a reload moved `x.html` there), or `""` when no set was found. `internal/core` names that template in trace events and `RenderFailed` logs.

#### Parameters

| Position | Type | Description |
|---|---|---|
| 1 | `string` | The route given to `Bind`; the outer slashes are not part of the key (`/a/b`, `a/b/` are the same) |
| 2 | `any` | `.` in the page and its layout |

#### Returns

| Position | Type | Description |
|---|---|---|
| 1 | `*Buffer` | The whole page; give it back with `Release` |
| 2 | `error` | `ErrNoTemplate`, `ErrPageTooLarge`, an execution error, or, with `isReload`, the `*FileError` of a reload that failed |

#### Rules
- A route that was never bound (`/`, `/layout`, `/partials/p`, `/a.html`, `/../a`) returns `ErrNoTemplate` without opening a file.
- A page larger than 8 MiB returns `ErrPageTooLarge`; 8,000,000 bytes render.
- Without `isReload`, a file changed or added after `Load` is never read.
- With `isReload`, a changed size or modification time of a template or page CSS or JS file is seen by the next render, and every bound route is bound again: `x.html` moved to `x/index.html` keeps answering. A bound template that is gone or doubled, a parse error, or a set that lost its `{{pageAssets}}` returns a `*FileError` until it is fixed. A new file without a route and an orphan CSS file are ignored until the restart.
- Concurrent renders while files change each see one whole state.

### RenderError, ErrorTemplate

`RenderError(code int, data any) (*Buffer, error)` executes the error page of `code` inside the root `layout.html` when there is one. `internal/core` passes `map[string]any{"Code": int, "Status": string, "Message": string, "RequestID": string}`. `ErrorTemplate(code int) string` names that page (`404`, `4xx`), or returns `""` when the built-in page serves the code.

#### Returns

| Position | Type | Description |
|---|---|---|
| 1 | `*Buffer` | The whole error page; give it back with `Release` |
| 2 | `error` | `ErrNoErrorPage` when no file serves `code`, `ErrPageTooLarge`, an execution error, or, with `isReload`, the `*FileError` of a reload that failed |

#### Rules
- The page of `code` is the first that exists at the root: `<code>.html`, then `<code/100>xx.html`. With `404.html`, `4xx.html` and `5xx.html`: 404 → `404`, 422 → `4xx`, 500 and 503 → `5xx`. With `503.html` and `5xx.html`: 404 and 422 → none, 500 → `5xx`, 503 → `503`.
- A code outside 400 to 599 has no page.
- An execution error in the chosen page or its layout returns the error and no buffer, counts as a failure, and the next file is not tried: the caller sends its built-in page, never a half page.
- The data is escaped by context, as in any page.

#### Nonce rules
From the tests (`render_nonce_test.go`):
- The output with `{{nonce}}` in a layout (script, string, style) and a page (URL query, attribute, text) equals the output of the same set whose `nonce` returns that value directly.
- A nonce called only by a reached partial or by the layout is placed; a set that reaches no `{{nonce}}` has none and draws nothing (`NonceDraws` unchanged).
- An error page that calls `{{nonce}}` gets one from `RenderError`.
- `{{pageAssets}}` draws no nonce.
- 1,000 renders give 1,000 nonces; 64 goroutines never share one.
- Data shaped like a nonce stays data: it never becomes the nonce.
- With `isReload`, a file that starts calling `{{nonce}}` gets one at the next render.
- A render with `{{nonce}}` costs at most 2 allocations more than the same set whose `nonce` returns the value directly.

### Release

`Release(b *Buffer)` gives a buffer back to the pool. A buffer grown past 256 KiB is dropped. Call it after the bytes were written.

### File tables

| Method | Returns |
|---|---|
| `Assets() fs.FS` | `fs.Sub(FS, "assets")`, or `nil` when `FS` has no `assets` directory at `Load` |
| `AssetHashes() map[string]string` | A copy of the table: asset path inside `assets/` (`css/app.css`) to its 16-hex hash. Empty with `isReload` |
| `Public() fs.FS` | `fs.Sub(FS, "public")`, or `nil` without a `public` directory |
| `PublicFiles() map[string]string` | A copy of the public files listed at `Load` (`.well-known/security.txt`) to their hash (`""` with `isReload`) |
| `PageAssets() map[string]string` | A copy of the page CSS and JS table (`bookings/new.css`) to its hash (`""` with `isReload`) |
| `PageAsset(p string) (string, bool)` | The hash of one page file in the current set, re-read with `isReload` |
| `Files() fs.FS` | The `fs.FS` given to `New` |
| `IsReload() bool` | The `isReload` given to `New` |

`internal/core` serves the asset route (`/assets/*`, `/assets/_pages/…`) and the public file routes from them.

### Snapshot, Counters, NonceDraws

| Method | Returns |
|---|---|
| `Snapshot() Snapshot` | `IsReload`; `Pages`, one per bound route (`Page{Name, Route, Layout, Assets}`: the template path, `/` + the route, the layout file or `""`, the CSS and JS files of the set, layout first); `Partials` (sorted names); `ErrorTemplates` (sorted file names, empty when only the built-in page exists); `Assets` (the number of non-hidden files under `assets/`); `Public` (the number under `public/`) |
| `Counters() (renders, failures int64)` | Renders that succeeded and renders that failed (an unbound route, an execution error, a page too large), since `New` |
| `NonceDraws() int64` | How many nonces were drawn from `crypto/rand` |

The devtool snapshot reads them.

## `Buffer`

The output of one render. `Bytes() []byte` and `Len() int` read it; `Write` refuses to grow it past 8 MiB. `Nonce() string` returns the nonce placed in it (32 lowercase hexadecimal characters), or `""` when its set never calls `{{nonce}}`; each call builds the string, so read it once.

## `FileError`

| Field | Type | Description |
|---|---|---|
| `File` | `string` | The file path inside the `fs.FS` (`sub/y.html`) |
| `Err` | `error` | The cause |

`Error()` returns `<File>: <Err>`; `Unwrap()` returns `Err`.

## Errors

| Value | Meaning |
|---|---|
| `ErrNoTemplate` | `Bind`: no candidate exists; `Render`: the route was never bound |
| `ErrTwoTemplates` | `Bind`: both candidates exist |
| `ErrReservedDir` | `Bind`: the route lies below `assets/`, `partials/` or `public/` |
| `ErrErrorPageName` | `Bind`: the route is one segment named like an error page |
| `ErrPageTooLarge` | The page grew past the 8 MiB limit |
| `ErrNoErrorPage` | `RenderError`: no file serves the code |

## How it works

1. `Load` walks the `fs.FS` in lexical order, skips `assets/`, `public/` and hidden paths, parses each template file once on its own with the template functions, and lists the CSS and JS files. The trees are kept; the files are not. Each CSS or JS file goes to the template of its directory with the longest name that equals its stem or prefixes it with a dot.
2. For each page, it finds the nearest `layout.html`, follows the `{{template}}` calls of the layout and the page to the partials they reach, and checks that every called name is defined in the set.
3. It adds copies of the trees to a new set (partials, then the layout, then the page as `content`, so the page's `define`s win) and executes the set once into `io.Discard` with `nil` data. `html/template` escapes a set at its first execution, so no request pays for it and every escaping error is found here. An escaping error names the file of the failing node; one at the end of the layout is blamed on the page when the page alone also fails.
4. Each set gets its own `pageAssets` function returning the set's tags, a constant string built before the set. While building a set, `Load` walks its trees for calls of `nonce` and `pageAssets`: the first marks the set, the second is required when the set owns a file.
5. `Bind` keeps the route's set in a table by route; `Render` looks the route up in it (`RenderError` looks the code up, then its class), takes a buffer from the pool and executes the set into it. On an error the buffer goes back to the pool and nothing is returned.
6. For a marked set, `Render` reads 16 bytes from `crypto/rand`, hex-encodes them into the buffer's own array, and overwrites every occurrence of the placeholder in the output with them. Template functions belong to a set, not to a request, so the set itself can only write a constant: overwriting it in place keeps the sets immutable and costs one scan of the page.

## Concurrency

- Without `isReload`, the sets and the tables are built in `Load` and never written again; `html/template` executes one set from many goroutines safely. No lock is taken.
- `Bind` and `CheckBindings` run in `Create`, before any request.
- With `isReload`, each render walks the `fs.FS` for the name, size and modification time of every template and page CSS or JS file, compares that fingerprint under a read lock, and on a change takes the write lock, re-parses, binds the routes again and swaps the whole site. Renders in flight keep the site they picked.
- A buffer belongs to one render from `Render` to `Release`; its nonce lives in it and is cleared by `Release`.
- The placeholder is drawn in `New` and read-only after; the counters are `atomic.Int64`.
- No goroutine, timer or watcher is started.

## Performance notes

- The buffer pool (`sync.Pool` of `*Buffer`) is approved by design 0018; buffers past 256 KiB are dropped so the pool keeps no large page.
- The `nonce` placeholder has the length and alphabet of a real nonce, so every escaping context treats both alike.
- `Render` allocates nothing of its own on the success path; the cost is `html/template`'s execution.
- The nonce overwrite is the second technique approved by design 0018 (in-place overwrite of a placeholder). The random bytes and the hex text sit in the pooled `Buffer`, and `Nonce()` builds the string only when asked, so placing a nonce allocates nothing: `BenchmarkRender_Nonce` (40 rows, Intel Core i7-9750H, Go 1.27.4): 246 allocations without `{{nonce}}`, 260 with two calls in the layout, all 14 from `html/template` escaping the two calls.
- A new pooled buffer starts with 4 KiB, about one page, so a buffer the pool dropped (after a GC, or under `-race`, where `sync.Pool` drops puts on purpose) costs two allocations instead of one per doubling.
