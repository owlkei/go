# sample/pages

A small booking site rendered on the server: Ginject page routes, `html/template` views and
[Owlky](https://github.com/dangduoc08/owlky)'s framework-free HTML components. It has no JavaScript of
its own apart from one inline line that picks the light or dark theme.

## Run it

```sh
go run ./sample/pages                 # http://localhost:8080/bookings
PORT=3000 go run ./sample/pages       # another port
cd sample/pages && PAGES_DEV=1 go run .   # read web/ from disk and reload templates on each change
```

`PAGES_DEV=1` reads `web/` relative to the working directory, so run it from `sample/pages`.

```sh
go test ./sample/pages                # TestPages_FormFlow drives the whole flow
```

## Pages

| Route | Handler | Template | Answers |
|---|---|---|---|
| `GET /bookings` | `RENDER_bookings` | `web/bookings/index.html` | The list (an Owlky table), or an info alert when it is empty |
| `GET /bookings/new` | `RENDER_new_OF_bookings` | `web/bookings/new.html` | The form (Owlky text fields and buttons) |
| `POST /bookings/new` | `SUBMIT_new_OF_bookings` | `web/bookings/new.html` | `422` with the form, its messages and the submitted values; or `303 See Other` to the list |
| `GET /bookings/{id}` | `RENDER_bookings_BY_id` | `web/bookings/{id}.html` | The booking (an Owlky card); `exception.NotFoundException` → `web/4xx.html` with `404` |
| `GET /robots.txt` | none | `web/public/robots.txt` | The file as it is, `Cache-Control: no-cache` and an `ETag` |

## What it shows

- **Pages by route.** `BookingPages` embeds `ginject.HTTP`. Each `RENDER_` route renders the template its
  path names and returns the template's data. `SUBMIT_new_OF_bookings` posts to the page that shows the
  form: when it renders, it answers `422`; `c.Redirect("/bookings")` answers `303`.
- **A form pipe.** `BookingForm` binds the body with `Form.Bind`, reads the `[]ginject.FieldLevel` for
  missing fields and fills `Errors`; the handler returns the form, which the page shows again.
- **File conventions.** `web/layout.html` wraps every page; pages override its `title` block.
  `web/partials/field.html` is one text field, called with `dict`. `web/4xx.html` renders the client
  errors of a page route (here the `404`, and a `403` from `csrf` for a browser), `web/5xx.html` the
  server errors. `web/public/robots.txt` is served at `/robots.txt`.
- **Page CSS.** `web/layout.css` belongs to the layout: `{{pageAssets}}` in the layout's `<head>` links it
  at `/assets/_pages/layout.css?v=<hash>`, after the shared stylesheet. A page can have its own files the
  same way, such as `web/bookings/new.css` next to `web/bookings/new.html`.
- **Template functions.** `{{asset "owlky.js"}}` writes `/assets/owlky.js?v=<hash>`, served with
  `Cache-Control: immutable`. `{{nonce}}` gives the inline script the request's CSP nonce, which `helmet`
  adds to `script-src` and `style-src`.
- **Security defaults.** `html/template` escapes every value for its context; `csrf.CSRF{}` refuses
  cross-origin form posts; `helmet` sends a strict policy with no `'unsafe-inline'`.

## Files

| Path | Holds |
|---|---|
| `main.go` | `helmet`, `csrf`, `EnableRender` on the embedded `web/` (`RenderOptions.Dir` defaults to `web`; `PAGES_DEV=1` sets `IsReload`, which reads `web/` from disk), `PORT` (default `8080`) |
| `module.go`, `bookings_service.go` | The module and the in-memory store |
| `bookings_pages.go` | The page routes |
| `booking_form.go` | The form pipe and its validation |
| `web/` | Layout and its CSS, pages named by their routes, the field partial and the error pages; Owlky markup with its `data-ok-*` attributes |
| `web/public/` | `robots.txt`, served at the root URL |
| `web/assets/` | `owlky.min.css` and `owlky.js`, copied from Owlky's build; source and license in `OWLKY.md` |
