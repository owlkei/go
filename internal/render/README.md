# Render

Internal package: applications use `ginject.Page`, `ginject.View`, `ginject.RenderOptions` and `App.EnableRender` instead.

*Render is a part of `Ginject` framework: it turns the `.html` files of an `fs.FS` into `html/template` sets at `Create()`, renders a page by name into a pooled buffer, and holds the asset table and the template functions.*

- [Render](#render)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [File conventions](#file-conventions)
  - [Template functions](#template-functions)
  - [`Engine`](#engine)
    - [New](#new)
    - [Load](#load)
    - [Render](#render-1)
    - [RenderError](#rendererror)
    - [Release](#release)
    - [Assets, AssetHashes, IsReload](#assets-assethashes-isreload)
    - [Snapshot, Counters, NonceDraws](#snapshot-counters-noncedraws)
  - [`Buffer`](#buffer)
  - [`FileError`](#fileerror)
  - [Errors](#errors)
  - [How it works](#how-it-works)
  - [Concurrency](#concurrency)
  - [Performance notes](#performance-notes)

## Key Features
- Every layout, page, partial and `error.html` is read and parsed once, at `Load`; a request never opens a file
- Parse errors, references to templates that do not exist and `html/template` escaping errors are startup errors that name the file
- A page is executed into a pooled buffer: a failed or oversized page leaves nothing to send, so the caller never writes half a page
- With `isReload`, a render after a template file was added, removed or changed uses the new files; nothing runs while idle
- A page set that calls `{{nonce}}` gets a fresh 128-bit nonce per render, the same at every use; a set that never calls it draws none

## Usage

`internal/core` is the only caller: `App.EnableRender` calls `New`, `App.Create` calls `Load`, a page route calls `Render`, writes the bytes and calls `Release`, and an HTML error calls `RenderError` (falling back to its built-in page on any error).

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
		"bookings/list.html": {Data: []byte(`<p>{{.Who}}</p>`)},
	}
	engine := render.New(site, false)
	if err := engine.Load(); err != nil {
		panic(err)
	}

	buf, err := engine.Render("bookings/list", map[string]any{"Who": "<reader>"})
	if err != nil {
		panic(err)
	}
	fmt.Println(string(buf.Bytes()))
	engine.Release(buf)
}
```

Output: `<main><p>&lt;reader&gt;</p></main>`.

## File conventions

| Path | Role | Template name |
|---|---|---|
| `layout.html` in any directory | Layout of the pages in that directory and below; the nearest one wins | none |
| `partials/**/*.html` (top-level `partials` only) | Partial, included with `{{template "partials/field" .}}` | path without `.html` |
| `error.html` (root) | The error page set, inside the root `layout.html` | none: it is not a page |
| `assets/**` | Static files; never parsed | none |
| every other `**/*.html` | Page | path without `.html`, `/`-separated: `bookings/list` |
| anything else; any path with a segment starting with `.` | Ignored | none |

- The page file is the template `content`; a layout places it with `{{template "content" .}}`.
- A page's `{{define "title"}}` replaces the layout's `{{block "title" .}}`; a page without it keeps the block's default.
- A page without a layout is rendered alone.
- A page set holds its layout, its page and the partials they reach, not every partial.

## Template functions

| Function | Result |
|---|---|
| `{{nonce}}` | The render's nonce: 32 lowercase hexadecimal characters from `crypto/rand`, the same at every use in one render, different on every render. The set writes a per-engine placeholder of the same length and alphabet, which `Render` overwrites in the buffer (see [How it works](#how-it-works)) |
| `{{asset "css/app.css"}}` | `/assets/css/app.css?v=<16 hex>`, the first 8 bytes of the file's SHA-256; with `isReload`, `/assets/css/app.css`. A file that is not in `assets/` fails the render |
| `{{dict "Name" "email" "Value" .Email}}` | `map[string]any`; an odd number of arguments or a non-string key fails the render |

## `Engine`

The template sets of one `fs.FS`, the asset table and the buffer pool.

### New

`New(fsys fs.FS, isReload bool) *Engine` creates an engine. It reads nothing; it draws the `nonce` placeholder.

#### Parameters

| Position | Type | Description |
|---|---|---|
| 1 | `fs.FS` | The templates and the `assets` directory |
| 2 | `bool` | Re-parse when a template file changed (development) |

#### Returns

| Position | Type | Description |
|---|---|---|
| 1 | `*Engine` | The engine, not loaded yet |

### Load

`Load() error` lists every file under `assets/` (hidden segments skipped, `.well-known` kept) and hashes each one (not with `isReload`), reads and parses every template file, builds each page set and executes it once into `io.Discard` with `nil` data, so `html/template` escapes it now. Call it once, before the first `Render`.

#### Returns

| Position | Type | Description |
|---|---|---|
| 1 | `error` | `nil`, or a `*FileError` naming the file: a parse error, a missing template, an escaping error, or an unknown asset reached with `nil` data |

#### Rules
- A parse error in a page, a layout (also one no page uses), a partial (also an unused one) or `error.html` fails `Load` with the file path in the message.
- `{{template "partials/nope" .}}` for a partial that does not exist fails `Load`.
- An unclosed attribute (`<a href='{{.X}}`) is an escaping error and fails `Load`.
- `notes.txt`, `.secret.html`, `.git/config.html` and `assets/page.html` are never parsed.
- Other errors of the `nil`-data execution (a failed `dict`, a missing field) are not startup errors: they fail the render.

### Render

`Render(name string, data any) (*Buffer, error)` executes the page `name` with `data` into a pooled buffer.

#### Parameters

| Position | Type | Description |
|---|---|---|
| 1 | `string` | The page name (`bookings/list`) |
| 2 | `any` | `.` in the page and its layout |

#### Returns

| Position | Type | Description |
|---|---|---|
| 1 | `*Buffer` | The whole page; give it back with `Release` |
| 2 | `error` | `ErrUnknownPage`, `ErrPageTooLarge`, an execution error, or, with `isReload`, the `*FileError` of a reload that failed |

#### Rules
- A name that is no page (unknown, empty, `error`, a partial, a layout, `../x`, `real.html`, `/real`) returns `ErrUnknownPage` without opening a file.
- A page larger than 8 MiB returns `ErrPageTooLarge`; 8,000,000 bytes render.
- Without `isReload`, a file changed or added after `Load` is never read.
- With `isReload`, a changed size or modification time, an added page and a removed page are seen by the next render; a file that fails to parse returns its `*FileError` until it is fixed.
- Concurrent renders while files change each see one whole state.

### RenderError

`RenderError(data any) (*Buffer, error)` executes `error.html`, inside the root `layout.html` when there is one, with `data` into a pooled buffer. `internal/core` passes `map[string]any{"Code": int, "Status": string, "Message": string, "RequestID": string}`.

#### Returns

| Position | Type | Description |
|---|---|---|
| 1 | `*Buffer` | The whole error page; give it back with `Release` |
| 2 | `error` | `ErrNoErrorPage` when `FS` has no `error.html`, `ErrPageTooLarge`, an execution error, or, with `isReload`, the `*FileError` of a reload that failed |

#### Rules
From the tests (`internal/core/http_page_error_test.go`):
- `error.html` holding `{{.Code}}` renders `.Code` as an `int`; inside a root layout the layout wraps it, and a nested layout never does.
- An execution error in `error.html` or in the layout around it returns the error and no buffer: the caller sends its built-in page, never a half page.

#### Nonce rules
From the tests (`render_nonce_test.go`):
- The output with `{{nonce}}` in a layout (script, string, style) and a page (URL query, attribute, text) equals the output of the same set whose `nonce` returns that value directly.
- A nonce called only by a reached partial or by the layout is placed; a set that reaches no `{{nonce}}` has none and draws nothing (`NonceDraws` unchanged).
- `error.html` that calls `{{nonce}}` gets one from `RenderError`.
- 1,000 renders give 1,000 nonces; 64 goroutines never share one.
- Data shaped like a nonce stays data: it never becomes the nonce.
- With `isReload`, a file that starts calling `{{nonce}}` gets one at the next render.
- A render with `{{nonce}}` costs at most 2 allocations more than the same set whose `nonce` returns the value directly.

### Release

`Release(b *Buffer)` gives a buffer back to the pool. A buffer grown past 256 KiB is dropped. Call it after the bytes were written.

### Assets, AssetHashes, IsReload

| Method | Returns |
|---|---|
| `Assets() fs.FS` | `fs.Sub(FS, "assets")`, or `nil` when `FS` has no `assets` directory at `Load` |
| `AssetHashes() map[string]string` | A copy of the table: asset path inside `assets/` (`css/app.css`) to its 16-hex hash. Empty with `isReload` |
| `IsReload() bool` | The `isReload` given to `New` |

`internal/core` serves the asset route from them.

### Snapshot, Counters, NonceDraws

| Method | Returns |
|---|---|
| `Snapshot() Snapshot` | `IsReload`, `Pages` (`Page{Name, Layout}`: the page name and its layout file, `""` without one), `Partials` (sorted names), `HasErrorPage`, `Assets` (the number of non-hidden files under `assets/`) |
| `Counters() (renders, failures int64)` | Renders that succeeded and renders that failed (an unknown page, an execution error, a page too large), since `New` |
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
| `ErrUnknownPage` | No page with this name |
| `ErrPageTooLarge` | The page grew past the 8 MiB limit |
| `ErrNoErrorPage` | `RenderError` was called and the `fs.FS` has no `error.html` |

## How it works

1. `Load` walks the `fs.FS` in lexical order, skips `assets/` and hidden paths, and parses each template file once on its own with the template functions. The trees are kept; the files are not.
2. For each page, it finds the nearest `layout.html`, follows the `{{template}}` calls of the layout and the page to the partials they reach, and checks that every called name is defined in the set.
3. It adds copies of the trees to a new set (partials, then the layout, then the page as `content`, so the page's `define`s win) and executes the set once into `io.Discard` with `nil` data. `html/template` escapes a set at its first execution, so no request pays for it and every escaping error is found here. An escaping error names the file of the failing node; one at the end of the layout is blamed on the page when the page alone also fails.
4. While building a set, `Load` walks its trees for a call of `nonce` and marks the set.
5. `Render` looks the name up in the table (`RenderError` takes the `error.html` set), takes a buffer from the pool and executes the set into it. On an error the buffer goes back to the pool and nothing is returned.
6. For a marked set, `Render` reads 16 bytes from `crypto/rand`, hex-encodes them into the buffer's own array, and overwrites every occurrence of the placeholder in the output with them. Template functions belong to a set, not to a request, so the set itself can only write a constant: overwriting it in place keeps the sets immutable and costs one scan of the page.

## Concurrency

- Without `isReload`, the sets and the tables are built in `Load` and never written again; `html/template` executes one set from many goroutines safely. No lock is taken.
- With `isReload`, each render walks the `fs.FS` for the name, size and modification time of every template file, compares that fingerprint under a read lock, and on a change takes the write lock, re-parses and swaps the sets. Renders in flight keep the set they picked.
- A buffer belongs to one render from `Render` to `Release`; its nonce lives in it and is cleared by `Release`.
- The placeholder is drawn in `New` and read-only after; the counters are `atomic.Int64`.
- No goroutine, timer or watcher is started.

## Performance notes

- The buffer pool (`sync.Pool` of `*Buffer`) is approved by design 0018; buffers past 256 KiB are dropped so the pool keeps no large page.
- The `nonce` placeholder has the length and alphabet of a real nonce, so every escaping context treats both alike.
- `Render` allocates nothing of its own on the success path; the cost is `html/template`'s execution.
- The nonce overwrite is the second technique approved by design 0018 (in-place overwrite of a placeholder). The random bytes and the hex text sit in the pooled `Buffer`, and `Nonce()` builds the string only when asked, so placing a nonce allocates nothing: `BenchmarkRender_Nonce` (40 rows, Intel Core i7-9750H, Go 1.27.4): 246 allocations without `{{nonce}}`, 260 with two calls in the layout, all 14 from `html/template` escaping the two calls.
- A new pooled buffer starts with 4 KiB, about one page, so a buffer the pool dropped (after a GC, or under `-race`, where `sync.Pool` drops puts on purpose) costs two allocations instead of one per doubling.
