# Exception

*Exception is a part of `Ginject` framework: it defines the error value that guards, handlers and filters raise, with an HTTP or WebSocket code, a client message and an error text, and writes it as the standard JSON error body.*

- [Exception](#exception)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [`ExceptionOptions` Fields](#exceptionoptions-fields)
    - [Description](#description)
    - [Cause](#cause)
  - [Functions](#functions)
    - [NewException](#newexception)
    - [HTTP constructors](#http-constructors)
    - [WebSocket constructors](#websocket-constructors)
  - [`Exception` Methods](#exception-methods)
    - [Error](#error)
    - [Unwrap](#unwrap)
    - [Code](#code)
    - [Message](#message)
    - [StatusText](#statustext)
    - [AppendJSON](#appendjson)
    - [MarshalJSON](#marshaljson)
  - [Security](#security)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

## Key Features
- One constructor per HTTP status and WebSocket close code
- Building an exception allocates nothing for the common forms
- Wraps a cause for `errors.Is` and `errors.As`
- Writes `{"code":…,"error":…,"message":…}` itself: `c.JSON(ex)` and `json.Marshal(ex)` give the error body

## Usage

Panic with an exception from a handler or a guard. The default filter answers with its code and the JSON body:

```go
package main

import (
	"errors"

	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
)

var errNoRows = errors.New("no rows")

type UserController struct {
	ginject.HTTP
}

func (instance UserController) NewController() ginject.Controller {
	return instance
}

func (instance UserController) READ() string {
	panic(exception.NotFoundException("User not found", exception.ExceptionOptions{
		Description: "no user with this id",
		Cause:       errNoRows,
	}))
}

func main() {
	app := ginject.New()
	app.Create(ginject.ModuleBuilder().Controllers(UserController{}).Build())
	app.Listen(3000, nil)
}
```

`GET /` responds with status `404` and:

```json
{"code":404,"error":"no user with this id: no rows","message":"User not found"}
```

## `ExceptionOptions` Fields

Pass `ExceptionOptions` (or a non-nil `*ExceptionOptions`) as the first option of any constructor.

### Description
Type: `string`

Default: `""`

Required: `false`

The error text. When it is empty, the error text is the code's status text.

### Cause
Type: `error`

Default: `nil`

Required: `false`

The wrapped error. `Unwrap` returns it, and the error text becomes `<description or status text>: <cause text>`.

```go
exception.BadRequestException("Invalid input", exception.ExceptionOptions{
	Description: "validation failed",
	Cause:       err,
})
```

## Functions

### NewException

Builds an exception with any code. The constructors below call it with their code.

#### Parameters
- 1st parameter: `string`

- Description: The message for the client (`Message`)

- 2nd parameter: `int`

- Description: An HTTP status or a WebSocket close code (`Code`)

- Variadic parameter: `...any`

- Description: Options; only the first is read: a `string`, an `error`, `ExceptionOptions` or `*ExceptionOptions`

#### Returns
- 1st value: `Exception`

- Description: The exception, an immutable value

#### Usage

```go
ex := exception.NewException("Too many attempts", 429, "rate limited")
```

#### Rules
- With no option, `Error()` is the code's status text (`"Bad Request"` for 400, `"Policy Violation"` for 1008, `""` for an unknown code).
- A non-empty `string` option is the error text.
- An `error` option is the cause, and `Error()` is its text.
- `ExceptionOptions{Description: d}` gives `d`, or the status text when `d` is empty.
- `ExceptionOptions{Description: d, Cause: c}` gives `d + ": " + c.Error()`. With `d` empty it gives `<status text>: <cause>`, and with no status text either, the cause's text alone.
- A non-nil `*ExceptionOptions` is read like the value.
- `nil`, `""`, a nil `*ExceptionOptions` and any other type (a map, an `int`, a slice) count as no option.
- Only the first option is read; later options are ignored.
- Two exceptions built with the same message, code and no option, a string option or a description are equal with `==`.
- No option, a `string`, an `error` and an `ExceptionOptions` literal of constants make no allocation.

### HTTP constructors

Each has the shape `func XException(message string, opts ...any) Exception` and the options of `NewException`:

| Constructor | Code |
|---|---|
| `BadRequestException` | 400 |
| `UnauthorizedException` | 401 |
| `ForbiddenException` | 403 |
| `NotFoundException` | 404 |
| `MethodNotAllowedException` | 405 |
| `NotAcceptableException` | 406 |
| `RequestTimeoutException` | 408 |
| `ConflictException` | 409 |
| `GoneException` | 410 |
| `PreconditionFailedException` | 412 |
| `RequestEntityTooLargeException` | 413 |
| `UnsupportedMediaTypeException` | 415 |
| `TeapotException` | 418 |
| `MisdirectedRequestException` | 421 |
| `UnprocessableEntityException` | 422 |
| `TooManyRequestsException` | 429 |
| `InternalServerErrorException` | 500 |
| `NotImplementedException` | 501 |
| `BadGatewayException` | 502 |
| `ServiceUnavailableException` | 503 |
| `GatewayTimeoutException` | 504 |
| `HTTPVersionNotSupportedException` | 505 |

```go
panic(exception.ForbiddenException("Access denied"))
```

#### Rules
- Each constructor sets its code, and `StatusText()` is `http.StatusText` of that code.

### WebSocket constructors

Same shape, with WebSocket close codes:

| Constructor | Code | Status text |
|---|---|---|
| `GoingAwayException` | 1001 | Going Away |
| `ProtocolErrorException` | 1002 | Protocol Error |
| `UnsupportedDataException` | 1003 | Unsupported Data |
| `InvalidPayloadException` | 1007 | Invalid Frame Payload Data |
| `PolicyViolationException` | 1008 | Policy Violation |
| `MessageTooBigException` | 1009 | Message Too Big |
| `WSInternalErrorException` | 1011 | Internal Error |
| `NotSubscribedException` | 4001 | Not Subscribed |
| `TopicNotFoundException` | 4004 | Topic Not Found |

```go
panic(exception.TopicNotFoundException("Unknown topic"))
```

#### Rules
- Each constructor sets its code and status text; the reserved code 1006 has no text.

## `Exception` Methods

### Error

Returns the error text, built from the first option as described in [NewException](#newexception). It is the `error` field of the JSON body.

#### Returns
- 1st value: `string`

- Description: The error text

#### Usage

```go
text := exception.BadRequestException("m").Error()
```

#### Rules
- The zero `Exception{}` returns `""` and does not panic.
- Only the description-and-cause form allocates (one concatenation per call).

### Unwrap

Returns the cause: the `error` option or `ExceptionOptions.Cause`.

#### Returns
- 1st value: `error`

- Description: The cause, or `nil`

#### Usage

```go
if errors.Is(ex, sql.ErrNoRows) {
	return
}
```

#### Rules
- `errors.Is` and `errors.As` find the cause through the exception.
- Without a cause (no option, a string, a description alone, the zero value), `Unwrap()` and `errors.Unwrap(ex)` are `nil`.

### Code

#### Returns
- 1st value: `int`

- Description: The code given at construction, as is (`999` stays `999`)

#### Usage

```go
code := ex.Code()
```

### Message

#### Returns
- 1st value: `string`

- Description: The message given at construction

#### Usage

```go
message := ex.Message()
```

### StatusText

#### Returns
- 1st value: `string`

- Description: `http.StatusText` of the code, else the WebSocket close status text, else `""`

#### Usage

```go
text := exception.NewException("m", 404).StatusText()
```

#### Rules
- `404` gives `"Not Found"`, `1008` gives `"Policy Violation"`, an unknown code gives `""`.

### AppendJSON

Appends the error body `{"code":<Code()>,"error":<Error()>,"message":<Message()>}` to `dst`. `c.JSON(ex)` and `c.JSON(&ex)` call it, so they write this body.

#### Parameters
- 1st parameter: `[]byte`

- Description: The buffer to append to; may be `nil`

#### Returns
- 1st value: `[]byte`

- Description: `dst` with the body appended

#### Usage

```go
c.Status(ex.Code()).JSON(ex)
```

#### Rules
- The bytes equal `json.Marshal` of the struct `{Code int "code"; Error string "error"; Message string "message"}` (and of the map with the same keys): same key order and the same escaping, checked on hostile, random and fuzzed strings.
- `AppendJSON(dst)` equals `append(dst, body...)`, with or without spare capacity in `dst`.
- A body whose strings are plain printable ASCII is written with one allocation, the body itself (none when `dst` has room).
- `<`, `>`, `&`, U+2028 and U+2029 are escaped; quotes, backslashes and control characters can't break out of the string; invalid UTF-8 becomes U+FFFD. The result is always valid JSON.
- What `dst` already holds is kept.
- The fields are written as they are: the zero value gives `{"code":0,"error":"","message":""}`. Defaults for an empty field are the filter's job.

### MarshalJSON

Returns the same body as `AppendJSON(nil)`.

#### Returns
- 1st value: `[]byte`

- Description: The JSON body

- 2nd value: `error`

- Description: Always `nil`

#### Usage

```go
body, _ := json.Marshal(ex)
```

#### Rules
- `json.Marshal(ex)`, `json.Marshal(&ex)` and an exception nested in a map all give the body.

## Security

- The message and the error text go to the client as is. Put only client-safe text in an exception: wrapping an internal error as the `Cause` sends its text in the `error` field.
- A message can hold client input (a request path, a file name). The JSON escaping above keeps it inside its string, also when a page embeds the body.

## Performance notes

- An `Exception` stores its message, description, cause and code. It builds no error value at construction, and `Error()` derives the text on each call.
- `AppendJSON` writes the body by hand when both strings are printable ASCII without `"`, `\`, `<`, `>` or `&`: it grows `dst` once to the exact size, appends the keys as literals and the code with `strconv.AppendInt`. That is about 50 ns and 0 allocations into a buffer with room, 75 ns and 1 allocation into `nil` (before: about 450 ns and 2 allocations). Any other string sends the whole body through `encoding/json` (about 480 ns, 2 allocations), so escaping is `encoding/json`'s by construction. A new field in the body means changing this function.
- An `ExceptionOptions` with a non-constant field passed as an option is boxed into `any`, and the box escapes when the exception does: one allocation at the call site.
- An `Exception` is immutable, so a package-level exception can be shared between goroutines.

## Benchmarks

Benchmark results on darwin/amd64 (Intel Core i7-9750H @ 2.60GHz), Go 1.27.4, median of 5 runs:

| Benchmark | ns/op | B/op | allocs/op |
|-----------|-------|------|-----------|
| BenchmarkNewException_NoOpts | 22.83 | 0 | 0 |
| BenchmarkNewException_StringOpt | 25.73 | 0 | 0 |
| BenchmarkNewException_ErrorOpt | 24.46 | 0 | 0 |
| BenchmarkNewException_ExceptionOptions_Cause | 25.7 | 0 | 0 |
| BenchmarkNewException_DescriptionAndCause | 25.18 | 0 | 0 |
| BenchmarkException_StatusText | 3.363 | 0 | 0 |
| BenchmarkException_Error | 11.75 | 0 | 0 |
| BenchmarkException_ErrorsIs | 45.91 | 64 | 1 |
| BenchmarkException_AppendJSON (into a buffer with room) | 50.9 | 0 | 0 |
| BenchmarkException_AppendJSON_Escaped | 481 | 176 | 2 |
| BenchmarkAppendJSON_Plain (into `nil`) | 74.8 | 80 | 1 |
| BenchmarkAppendJSON_Escaped (into `nil`) | 505 | 272 | 3 |
| BenchmarkException_MarshalMap (the previous map body, for reference) | 1891 | 576 | 16 |
| BenchmarkBadRequestException | 19.17 | 0 | 0 |
| BenchmarkNotFoundException | 19.16 | 0 | 0 |
| BenchmarkInternalServerErrorException | 19.19 | 0 | 0 |
| BenchmarkTopicNotFoundException | 19.11 | 0 | 0 |
| BenchmarkPolicyViolationException | 18.57 | 0 | 0 |
| BenchmarkWSInternalErrorException | 18.56 | 0 | 0 |

*Note: These benchmarks are machine-dependent and were captured at documentation generation time (November 16, 2026).*
