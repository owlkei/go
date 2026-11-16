# HTTP hook

*Package `httphook` lets a built-in middleware put a writer in front of the response and run code when the exchange ends, without changing the request lifecycle.*

- [HTTP hook](#http-hook)
  - [Why it exists](#why-it-exists)
  - [Usage](#usage)
  - [Types](#types)
    - [Hook](#hook)
    - [Writer](#writer)
  - [Functions](#functions)
    - [Of](#of)
  - [Rules](#rules)
  - [Cost](#cost)

The package is internal: only code in the Ginject module can import it. `compress` and `loadshed`
use it; users never see it.

## Why it exists

Ginject's middleware chain is linear: `next()` only marks that the next layer may run, and the
handler runs after `Use` returns. A middleware therefore cannot see the handler's writes or run
code after it. `compress` must wrap the response writer, and `loadshed` must release its slot when
the exchange ends. The framework's response writer (the `w` every middleware receives) implements
`Writer` for that.

## Usage

```go
package mymiddleware

import (
	"net/http"
	"time"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/httphook"
)

type timer struct{}

func (timer) Done(elapsed time.Duration) {}

type Timing struct{}

func (Timing) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	if hw := httphook.Of(w); hw != nil {
		hw.OnDone(timer{})
	}
	next()
}
```

## Types

### Hook

```go
type Hook interface{ Done(elapsed time.Duration) }
```

`Done` runs once when the exchange ends. `elapsed` is the time since the request started.

### Writer

```go
type Writer interface {
	Inner() http.ResponseWriter
	SetInner(w http.ResponseWriter)
	OnDone(h Hook)
}
```

| Method | Meaning |
|---|---|
| `Inner` | The writer the response goes to now: the server's `http.ResponseWriter`, or the last one set |
| `SetInner` | Puts a writer in front: every later `Header`, `WriteHeader`, `Write`, `Flush`, `ReadFrom`, `Hijack` and `Unwrap` of the framework's writer goes to it |
| `OnDone` | Registers a hook for the end of this exchange |

## Functions

### Of

```go
func Of(w http.ResponseWriter) Writer
```

Returns `w` as a `Writer` when it is the framework's writer, else `nil` (any other
`http.ResponseWriter`, `nil` included). A middleware called outside an app then degrades to a
no-op: it calls `next()` and does nothing else.

## Rules
- Hooks run exactly once per exchange, last registered first, with the time since the request
  started.
- They run after the request lifecycle returned (the response, an error response included, is
  written) and before the `complete` trace event; for a request with no route, after the `404`;
  on the WebSocket path, when the handshake ends, accepted or refused; and before
  `http.ErrAbortHandler` is re-panicked.
- After the hooks ran the writer is reset: the next request on the pooled exchange has no hook and
  writes to its own `http.ResponseWriter`.
- The status the framework records is the one the layer passed to `WriteHeader`, whatever writer
  is in front.

## Cost
With no hook registered the request path pays one length check and allocates nothing. The hook
list keeps its capacity across pooled exchanges, so registering a hook allocates only the first
time; the clock is read once, and only when a hook exists.
