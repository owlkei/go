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

| Route | Handler | Answers |
|---|---|---|
| `GET /bookings` | `READ_bookings` | The list (an Owlky table), or an info alert when it is empty |
| `GET /bookings/new` | `READ_new_OF_bookings` | The form (Owlky text fields and buttons) |
| `POST /bookings` | `CREATE_bookings` | `422` with the form, its messages and the submitted values; or `303 See Other` to the list |
| `GET /bookings/{id}` | `READ_bookings_BY_id` | The booking (an Owlky card); `exception.NotFoundException` → the `404` page |

## What it shows

- **A page controller.** `BookingPages` embeds `ginject.Page`, so its route words are page routes and
  each handler returns a `ginject.View`. Status and redirect use the context: `c.Status(422)`,
  `c.Redirect("/bookings")` (a page redirect answers `303`).
- **A form pipe.** `BookingForm` binds the body with `Form.Bind`, reads the `[]ginject.FieldLevel` for
  missing fields and fills `Errors`; the handler passes the form back to its view.
- **File conventions.** `web/layout.html` wraps every page; pages override its `title` block.
  `web/partials/field.html` is one text field, called with `dict`. `web/error.html` renders every error
  of a page route (here the `404`, and a `403` from `csrf` for a browser).
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
| `bookings_pages.go` | The page controller |
| `booking_form.go` | The form pipe and its validation |
| `web/` | Layout, pages, the field partial and the error page; Owlky markup with its `data-ok-*` attributes |
| `web/assets/` | `owlky.min.css` and `owlky.js`, copied from Owlky's build; source and license in `OWLKY.md` |
