# 03 · Pages

A small booking site rendered on the server: page routes (`RENDER_`, `SUBMIT_`), Go's `html/template`
and [Owlky UI](https://owlky.com)'s HTML components. One Go binary serves finished pages: no JSON API
and no client app to build. One inline script picks the light or dark theme; `owlky.js` adds the
components' behavior.

## What you learn

- **Pages by route.** `BookingPages` embeds `owlky.HTTP`. Each `RENDER_` route renders the template its
  path names and returns the template's data. `SUBMIT_new_OF_bookings` posts to the page that shows the
  form: when it renders, it answers `422`; `ctx.Redirect("/bookings")` answers `303`.
- **A form pipe.** `BookingForm` binds the body with `Form.Bind`, reads the `[]owlky.FieldLevel` for
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
- **Security defaults.** `html/template` escapes every value for its context; `CSRF: &owlky.CSRFOptions{IsEnabled: true}` refuses
  cross-origin form posts; `helmet` sends a strict policy with no `'unsafe-inline'`.

## Run it

```sh
go run .                    # http://localhost:3000/bookings
PAGES_DEV=1 go run .        # read web/ from disk and reload templates on each change
```

`go test ./...` drives the whole flow (the list, the form, a refused post, a valid post and the
redirect) and checks how the assets are served.

## Pages

| Route | Handler | Template | Answers |
|---|---|---|---|
| `GET /bookings` | `RENDER_bookings` | `web/bookings/index.html` | The list (an Owlky table), or an info alert when it is empty |
| `GET /bookings/new` | `RENDER_new_OF_bookings` | `web/bookings/new.html` | The form (Owlky text fields and buttons) |
| `POST /bookings/new` | `SUBMIT_new_OF_bookings` | `web/bookings/new.html` | `422` with the form, its messages and the submitted values; or `303 See Other` to the list |
| `GET /bookings/{id}` | `RENDER_bookings_BY_id` | `web/bookings/{id}.html` | The booking (an Owlky card); `exception.NotFoundException` → `web/4xx.html` with `404` |
| `GET /robots.txt` | none | `web/public/robots.txt` | The file as it is, `Cache-Control: no-cache` and an `ETag` |

## Read next

[04 · Async modules](../04-async-modules/README.md): start the app before its dependencies are ready.
