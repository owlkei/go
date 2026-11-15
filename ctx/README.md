# Ctx

*Package `ctx` holds the per-request context of `Ginject`: it reads the request (query, body, form, files, route parameters, headers), binds it into structs and writes the response, for HTTP and WebSocket.*

- [Ctx](#ctx)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [Concurrency and lifetime](#concurrency-and-lifetime)
  - [Security](#security)
  - [`HTTPContext`](#httpcontext)
    - [Fields](#fields)
    - [NewHTTPContext, Init and Reset](#newhttpcontext-init-and-reset)
    - [SetID and GetID](#setid-and-getid)
    - [Status](#status)
    - [Text](#text)
    - [JSON](#json)
    - [JSONP](#jsonp)
    - [Redirect](#redirect)
    - [Body](#body)
    - [Form](#form)
    - [File](#file)
    - [Query](#query)
    - [Header](#header)
    - [Param](#param)
    - [AddAggregation and Aggregations](#addaggregation-and-aggregations)
  - [`WSContext`](#wscontext)
  - [Request data types](#request-data-types)
    - [Body methods](#body-methods)
    - [Query, Form, Header and Param methods](#query-form-header-and-param-methods)
    - [File.Bind](#filebind)
    - [WSPayload methods](#wspayload-methods)
  - [Binding](#binding)
    - [Bind tags](#bind-tags)
    - [BinderOf](#binderof)
    - [Binder methods](#binder-methods)
    - [BindStruct, BindStrArr and BindFile](#bindstruct-bindstrarr-and-bindfile)
    - [GetTagParams and GetTagParamIndex](#gettagparams-and-gettagparamindex)
    - [Binding rules](#binding-rules)
  - [`FieldLevel`](#fieldlevel)
  - [`DataFile`](#datafile)
  - [Other types and constants](#other-types-and-constants)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

## Key Features
- One binding plan per struct type, built once and cached: a warm bind walks no type and parses no tag.
- Hostile input never panics with a runtime error: wrong JSON types, `null`, out-of-range numbers and unsupported field kinds leave the field zero.
- Malformed bodies and forms become `400` / `413` exceptions, never a `500`.
- The accessors (`Body`, `Form`, `Query`, `Header`, `Param`) never return `nil` and parse once per request.
- `JSON` output is byte-identical to `json.Marshal`, and an encoding error writes nothing.

## Usage

Handlers receive the context or the request data types through injection:

```go
package main

import (
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
)

type SearchDTO struct {
	Term string `bind:"q"`
	Page int    `bind:"page"`
}

type SearchController struct {
	common.HTTP
}

func (instance SearchController) NewController() core.Controller {
	return instance
}

func (instance SearchController) READ_search(c *ctx.HTTPContext, q ctx.Query) ctx.Map {
	dto := q.BindValue(SearchDTO{}).(SearchDTO)
	return ctx.Map{"term": dto.Term, "page": dto.Page, "request": c.GetID()}
}

func main() {
	app := core.New()
	app.Create(
		core.ModuleBuilder().
			Controllers(SearchController{}).
			Build(),
	)
	if err := app.Listen(3000); err != nil {
		panic(err)
	}
}
```

`GET /search?q=go&page=2` answers `{"page":2,"request":"…","term":"go"}`.

## Concurrency and lifetime

- A context belongs to one request and one goroutine. `core` takes it from a pool, calls `Init`, runs the pipeline, calls `Reset` and puts it back. Don't keep the context, or anything that aliases it (`ParamValues`, `Aggregations()`), after the request ends.
- `GetID` is a pure read after `Init`: goroutines started by a handler may log it.
- A `*Binder` is immutable and safe for concurrent use. `BinderOf` may be called from any goroutine.

## Security

- `core` caps the request body (`http.MaxBytesReader`) before `ctx` reads it. `Body`, `Form` and `File` turn the limit error into a `413` exception.
- The JSONP callback is reduced to `[A-Za-z0-9_$.[]]`, cut to 128 bytes and served with `X-Content-Type-Options: nosniff`.
- `Redirect` passes the target to `http.Redirect`, which escapes it. It doesn't check where the target points: validate a user-supplied URL (such as `//evil.example`) in the handler.
- `JSON` and `Text` with a format string insert `args` unescaped: pass trusted values only.
- The request ID is a correlation ID, not a secret: it is not cryptographically random.

## `HTTPContext`

### Fields

#### Request
Type: `*http.Request` (embedded)

The request. Set by `Init`.

#### ResponseWriter
Type: `http.ResponseWriter` (embedded)

The response writer. Set by `Init`.

#### ParamKeys
Type: `map[string][]int`

Default: `nil`

Route parameter names and their indexes in `ParamValues`. Written by the router.

#### ParamValues
Type: `[]string`

Default: `nil`

Route parameter values. Written by the router; valid only during the request.

#### Next
Type: `Next`

Default: `nil`

Calls the next handler of the pipeline.

#### Code
Type: `int`

Default: `200`

The status code that `Text`, `JSON`, `JSONP` and `Redirect` write.

#### Timestamp
Type: `time.Time`

The time `Init` ran.

### NewHTTPContext, Init and Reset

`NewHTTPContext() *HTTPContext` returns a context with `Code` set to `200`.

`Init(w http.ResponseWriter, r *http.Request)` sets `Timestamp`, `ResponseWriter`, `Request` and the request ID, and nothing else.

`Reset()` drops every reference the context holds (request, writer, parsed input, parameters, aggregations, `Next`) and sets `Code` back to `200`.

```go
c := ctx.NewHTTPContext()
c.Init(w, r)
defer c.Reset()
```

#### Rules
- `Code` is `200` after `NewHTTPContext` and after `Reset`; `Init` doesn't change it.
- After `Reset`, every field except `Timestamp` is zero: `ParamValues` is `nil`.
- A context reused after `Reset` has no trace of the previous request: ID, code, body, form, files, query, header, parameters, aggregations and `Next`.

### SetID and GetID

`SetID()` generates the request ID; `GetID() string` returns it. `Init` calls `SetID`.

#### Rules
- The ID is 16 lowercase hexadecimal characters and never empty.
- `SetID` keeps the ID it already has until `Reset`.
- The `X-Request-Id` request header is ignored.
- 10,000 consecutive IDs hold at least 9,990 distinct values; IDs generated concurrently differ.

### Status

`Status(code int) *HTTPContext` sets `Code` and returns the context, so it chains: `c.Status(201).JSON(data)`.

### Text

`Text(data string, args ...any)` writes `Content-Type: text/plain; charset=utf-8`, the status `Code` and `data`.

#### Parameters
1. `data` `string`: the body, or a format string when `args` are given.
2. `args` `...any`: optional format arguments.

#### Usage

```go
c.Text("100% sure")
c.Text("hello %s", "joe")
```

#### Rules
- Without `args` the body is `data` as is (`100% sure %s` stays unchanged); with `args` it is `fmt.Sprintf(data, args...)`.
- `Content-Type` holds exactly one value, replacing one set earlier.

### JSON

`JSON(data any, args ...any)` writes `Content-Type: application/json`, the status `Code` and the encoded `data`.

#### Parameters
1. `data` `any`: a raw JSON `string`, a `JSONAppender`, or any value `encoding/json` can encode.
2. `args` `...any`: format arguments for a raw JSON string.

#### Usage

```go
c.JSON(ctx.Map{"ok": true})
c.Status(201).JSON(user)
c.JSON(`{"rate":"100%"}`)
c.JSON(`{"greeting":"%s"}`, "hi")
```

#### Rules
- The body equals `json.Marshal(data)` byte for byte, with no trailing newline, for maps, structs, slices, pointers and `nil`.
- A `string` is raw JSON: used as is without `args` (so `%` is safe), formatted with `args`, then validated and compacted.
- A `JSONAppender` writes what `AppendJSON` appends, even when the value also implements `json.Marshaler`.
- When encoding fails (an unsupported type, an invalid raw string, a failing `MarshalJSON`), `JSON` panics with an `exception.Exception` of code `500` whose chain holds the encoding error, and nothing is written: no header, no status, no byte.
- A context keeps working after a failed `JSON`.
- `Content-Type` holds exactly one value; changing it on one response never reaches another.

### JSONP

`JSONP(data any, args ...any)` writes `data` as `/**/ typeof <cb> === 'function' && <cb>(<json>);` with `Content-Type: text/javascript; charset=utf-8` and `X-Content-Type-Options: nosniff`, where `<cb>` is the `callback` query parameter.

#### Rules
- `<json>` is what `JSON` would write; U+2028, U+2029, `<`, `>` and `&` stay escaped.
- The callback is read from the cached `Query()`, so a change made to it before `JSONP` is used.
- Every character outside `[A-Za-z0-9_$.[]]` is removed and the result is cut to 128 bytes: `alert(document.cookie)//` becomes `alertdocument.cookie`.
- When nothing is left, the response is the plain `JSON` response.
- An encoding error panics with a `500` exception and writes nothing.

### Redirect

`Redirect(url string)` answers with `http.Redirect`.

#### Rules
- The status is `Code` when it is 3xx, else `301`; `Code` then holds the status sent.
- The `Location` header is the given URL.

```go
c.Status(http.StatusFound).Redirect("/login")
```

### Body

`Body() Body` returns the JSON body of the request, parsed on the first call.

#### Returns
1. `Body`: the decoded JSON object, never `nil`.

#### Rules
- The body is parsed when the media type (the part before `;`, trimmed, compared case-insensitively) is `application/json` or `application/*+json`; `text/plain; note=application/json` is not JSON.
- `null`, an empty body and a body of JSON whitespace give an empty `Body`.
- The body is read once and cached: the second call returns the same map.
- Malformed JSON or a top-level value that is not an object panics with an `exception.Exception` of code `400` and message `Invalid JSON body`; `errors.As` reaches the `encoding/json` error.
- A body over the limit panics with code `413`; `errors.As` reaches the `*http.MaxBytesError`.
- Another read error panics with code `400` and message `Cannot read request body`.

### Form

`Form() Form` returns the form values of a urlencoded or multipart request, parsed on the first call.

#### Rules
- Forms are parsed for exactly `application/x-www-form-urlencoded` and `multipart/form-data` (case-insensitive, parameters ignored); any other request gives an empty `Form`.
- `Form` never returns `nil` and is computed once, also without a form content type.
- A malformed form panics with an `exception.Exception` of code `400` and message `Invalid form body`; a form over the body limit with code `413`; `errors.As` reaches the cause.
- A multipart body is parsed once for `Form` and `File`, in either order.

### File

`File() File` returns the uploaded files of a multipart request.

#### Rules
- `File` parses the multipart form itself (same limits and errors as `Form`): a handler that injects only `ctx.File` gets the files.
- Without files the result has length `0`.

### Query

`Query() Query` returns `URL.Query()`, cached for the request.

### Header

`Header() Header` returns the request header.

### Param

`Param() Param` returns the route parameters built from `ParamKeys` and `ParamValues`, once per request.

#### Rules
- Values follow the order of each key's indexes and are copied: the map stays valid when the router reuses `ParamValues`, and writing to it never writes to `ParamValues`.
- Appending to one parameter never overwrites another's values.
- An index outside `ParamValues` is skipped.
- `Param` never returns `nil`.

### AddAggregation and Aggregations

`AddAggregation(a *aggregation.Aggregation)` appends to the request's aggregation list; `Aggregations() []*aggregation.Aggregation` returns it.

#### Rules
- The list keeps the order of the calls and is empty on a new context.
- `Reset` empties it: a reused context starts with no aggregation.

## `WSContext`

The context of a WebSocket message. It embeds `*websocket.Conn` and has the fields `Next Next` and `Timestamp time.Time`.

| Method | Description |
|--------|-------------|
| `NewWSContext() *WSContext` | Returns an empty context. |
| `Init(conn *websocket.Conn)` | Sets `Timestamp`, the connection and a 16-character request ID. |
| `Reset()` | Zeroes every field except `Timestamp`: ID, connection, payload, context, send function, aggregations and `Next`. |
| `SetID()` / `GetID() string` | Same rules as `HTTPContext`. |
| `WSPayload() WSPayload` / `SetWSPayload(p WSPayload)` | The message payload; `nil` before `SetWSPayload`. |
| `Send(data any)` / `SetSend(fn func(data any))` | Sends through the function set by `SetSend`; does nothing without one. |
| `Context() context.Context` / `SetContext(ctx context.Context)` | `context.Background()` until `SetContext` and after `Reset`. |
| `AddAggregation(a *aggregation.Aggregation)` / `Aggregations() []*aggregation.Aggregation` | Same rules as `HTTPContext`. |

## Request data types

`Body` and `WSPayload` are `map[string]any`; `Query`, `Form`, `Header` and `Param` are `map[string][]string`; `File` is `map[string][]*multipart.FileHeader`.

### Body methods

| Method | Description |
|--------|-------------|
| `Get(k string) any` | Value at a dotted path (`"user.address.city"`), `nil` when absent. |
| `Set(k string, v any)` | Sets the value at a dotted path, creating the maps on the way. |
| `Has(k string) bool` | Reports whether the path is present, whatever its value. |
| `Del(k string)` | Removes the key at the path. |
| `Bind(s any) (any, []FieldLevel)` | Binds the body into a copy of the struct `s`. |
| `BindValue(s any) any` | `Bind` without the field levels. |

```go
b := ctx.Body{"user": map[string]any{"name": "joe"}}
b.Get("user.name")      // "joe"
b.Set("user.age", 30.0) // {"user": {"name": "joe", "age": 30}}
b.Has("user.email")     // false
```

#### Rules
- Paths split on `.`; segments that are blank after trimming are ignored (`a. .b`, `a..b`, `.a.b`, `a.b.` all mean `a.b`); kept segments are not trimmed (`" a"` is its own key).
- `Get` of an empty or blank path returns the body itself (the same map).
- `Get` returns `nil` when an intermediate segment is missing or is not an object.
- `Has` is `true` for a key whose value is `null`.
- `Del` of a missing path is a no-op; `Set` overwrites a key that held a nested map.
- `Get` and `Has` don't allocate, and a key without a dot equals a direct map read.

### Query, Form, Header and Param methods

| Method | Description |
|--------|-------------|
| `Get(k string) string` | First value of the key, `""` when absent (also on a `nil` `Param`). |
| `Set(k, v string)` | Replaces the values with `v`. |
| `Add(k, v string)` | Appends `v`. |
| `Del(k string)` | Removes the key. |
| `Has(k string) bool` | Reports whether the key is present. |
| `Bind(s any) (any, []FieldLevel)` | Binds the values into a copy of the struct `s`. |
| `BindValue(s any) any` | `Bind` without the field levels. |

`Header` methods use the canonical MIME form of the key: `Has("content-type")` finds `Content-Type`, and `Header.Bind` looks each tag up in its canonical form (`bind:"x-token"` reads `X-Token`) while `FieldLevel.Tag()` keeps the tag as written.

### File.Bind

`Bind(s any) any` binds uploaded files into fields of type `*DataFile` (the file at the tag's index) or `[]*DataFile` (every file of the key).

#### Rules
- When `s` implements `IsValid(*DataFile) bool` and a file is refused, `Bind` panics with a `400` exception.
- When `s` implements `Store(*DataFile, multipart.File)`, `Bind` opens each file, calls `Store` and closes the file, also when `Store` panics (the panic still reaches the caller).

```go
type UploadDTO struct {
	Avatar *ctx.DataFile   `bind:"avatar"`
	Photos []*ctx.DataFile `bind:"photos"`
}

dto := c.File().Bind(UploadDTO{}).(UploadDTO)
```

### WSPayload methods

`Bind(s any) (any, []FieldLevel)` and `BindValue(s any) any` bind like `Body`, with the same plan and rules.

## Binding

### Bind tags

A field binds when it is exported and has a `bind` tag. The first comma-separated part names the key (`TagBind` is `"bind"`); for string-array sources an `.N` suffix picks the value at index `N` (`bind:"ids.1"`).

```go
type CreateUserDTO struct {
	Name    string            `bind:"name"`
	Age     int               `bind:"age"`
	Tags    []string          `bind:"tags"`
	Address struct {
		City string `bind:"city"`
	} `bind:"address"`
}
```

### BinderOf

`BinderOf(t reflect.Type) (*Binder, error)` returns the binding plan of a struct type, built on the first call and cached per type.

#### Parameters
1. `t` `reflect.Type`: a struct type.

#### Returns
1. `*Binder`: the plan, the same pointer for every call with the same type.
2. `error`: `ErrBindTarget` when `t` is `nil` or not a struct type (a pointer, an `int`, a slice, a map, an interface), with a `nil` `*Binder`.

#### Usage

```go
b, err := ctx.BinderOf(reflect.TypeOf(CreateUserDTO{}))
if err != nil {
	panic(err)
}
dto, fls := b.Struct(map[string]any{"name": "joe"})
```

#### Rules
- Concurrent calls for one type all return the one stored `*Binder`.
- Self-referential types (`Children []Node`, `Next *Node`) build without recursing forever and bind to any depth the data holds.
- `ErrBindTarget.Error()` is `ctx: bind target must be a struct`.

### Binder methods

| Method | Description |
|--------|-------------|
| `StrArr(d map[string][]string) (any, []FieldLevel)` | Binds string-array values (query, form, parameters). Equals `BindStrArr`. |
| `Header(h Header) (any, []FieldLevel)` | `StrArr` with canonical header keys. Equals `Header.Bind`. |
| `Struct(d map[string]any) (any, []FieldLevel)` | Binds a decoded JSON object. Equals `BindStruct` and `Body.Bind`. |
| `File(f File) (map[string][]*DataFile, any)` | Binds uploaded files. Equals `BindFile`. |

The returned `any` holds a value of the struct type, not a pointer.

### BindStruct, BindStrArr and BindFile

- `BindStruct(d map[string]any, fls *[]FieldLevel, s any, parentNS string, parentTag string) (any, []FieldLevel)`: binds a JSON object into the struct type of `s` (a value or a `reflect.Type`), appending to `*fls`; `parentNS` and `parentTag` prefix the namespaces and nested tags.
- `BindStrArr(d map[string][]string, fls *[]FieldLevel, s any) (any, []FieldLevel)`: binds string-array values, appending to `*fls`.
- `BindFile(f File, s any) (map[string][]*DataFile, any)`: binds files and returns the files per bound key.

They are thin wrappers over `BinderOf`. Every `Bind` and `BindValue` method, and these three functions, panic with an error for which `errors.Is(err, ErrBindTarget)` is true when `s` is not a struct value (`&dto`, `nil`, `42`).

### GetTagParams and GetTagParamIndex

- `GetTagParams(v string) []string` splits a tag on `,`, trims each part and drops empty ones: `" name , ,limit=3,"` → `["name", "limit=3"]`.
- `GetTagParamIndex(v string) (int, string)` splits `field.N` into the index and the field: `"integers_1.3"` → `3, "integers_1"`; without a dot, or with a negative index, the index is `0`.

### Binding rules

From a JSON object (`Body`, `WSPayload`, `Binder.Struct`):
- Named basic types and slices of them bind like their underlying type (`type Page int` binds `2` as `Page(2)`, also in `FieldLevel.Value()`).
- A value of the wrong JSON type for a scalar or struct field leaves the field zero and adds no `FieldLevel`.
- An `any` field bound from `null` stays `nil` with one `FieldLevel` whose `IsValue()` is `true` and `Value()` is `nil`; `*any` points to a `nil` interface.
- A number outside the range of the target integer, `float32` or `complex64` type, or a negative number for an unsigned type, is a mismatch. Fractions truncate toward zero (`1.9` → `1`).
- A slice or map with a wrong-type or out-of-range element becomes empty; in nested slices (`[][]int`) the element, a `null` element and a `null` row are skipped.
- Slices and maps of unsupported element kinds (`[]*Address`, `map[string]*Address`, `[]chan int`) stay zero with no `FieldLevel`.
- In a map of maps, an inner value that is not an object is skipped and the other keys bind.
- Slices of structs and maps of structs bind each element from its object; `[]any` and `map[string]any` pass through unchanged.

From string arrays (`Query`, `Form`, `Header`, `Param`, `Binder.StrArr`):
- Scalars parse the value at the tag's index with `strconv`; slices parse every value; named types bind like their underlying type.

Field levels:
- One `FieldLevel` per bound or absent tagged field, in declaration order, with nested entries before their parent; an absent key gives `IsValue() == false`.
- Nested entries carry dotted names: `NestedTag()` `addr.street` or `phones.0.type`, `Namespace()` `fieldLevelDTO.Addr.addrEntryDTO.Street` or `fieldLevelDTO.Phones.0.phoneEntryDTO.Type`.
- Binding a type a second time (from the cache) gives the same value and field levels as the first build.

## `FieldLevel`

Describes one bound field, for validation. All fields are unexported; read them with the getters:

| Method | Returns |
|--------|---------|
| `Tag() string` | The tag key as written (`x-token`). |
| `NestedTag() string` | The dotted path of tags from the root (`addr.street`). |
| `Namespace() string` | The dotted path of type and field names (`DTO.Addr.Address.Street`). |
| `Field() string` | The Go field name. |
| `Index() int` | The tag's `.N` index (string-array sources), else `0`. |
| `Kind() reflect.Kind` | The field's kind. |
| `Type() reflect.Type` | The field's type. |
| `Value() any` | The bound value (`nil` when not bound). |
| `IsValue() bool` | `true` when the key was present and bound. |

A zero `FieldLevel` returns zero values from every getter.

## `DataFile`

An uploaded file bound by `File.Bind`.

| Field | Type | Description |
|-------|------|-------------|
| `FileHeader` | `*multipart.FileHeader` (embedded) | The standard file header; `Open()` reads the file. |
| `Index` | `int` | Position of the file among the key's files. |
| `Size` | `int64` | Size in bytes. |
| `Total` | `int` | Number of files bound for the key. |
| `Key` | `string` | The form key. |
| `Filename` | `string` | The base name sent by the client. |
| `Type` | `string` | The part's `Content-Type`. |
| `Dest` | `string` | Free for the application (for example where `Store` saved the file). |

## Other types and constants

| Name | Definition |
|------|------------|
| `Context` | `interface { Reset(); SetID(); GetID() string }`, implemented by both contexts. |
| `JSONAppender` | `interface { AppendJSON(dst []byte) []byte }`: a value that writes its own JSON. |
| `Map` | `map[string]any`. |
| `ErrFunc` | `func(error)`. |
| `HTTPHandler` | `func(*HTTPContext)`. |
| `WSHandler` | `func(*WSContext)`. |
| `Next` | `func()`. |
| `Redirect` | `func(string)`. |
| `RequestID` | `"X-Request-Id"`, the response header name `core` writes the ID under. |
| `TagBind` | `"bind"`. |
| `ErrBindTarget` | `errors.New("ctx: bind target must be a struct")`. |

## Performance notes

- **Binding plan.** `BinderOf` resolves, once per struct type, each field's index, tag, canonical header key, namespace suffix and a tree of value kinds. A warm `Bind` costs one `sync.Map` load (about 14 ns) and walks only the plan.
- **No field levels when not needed.** `BindValue` skips the `[]FieldLevel` and every namespace string; it costs half of `Bind` or less (`Body`: 382 ns against 962 ns for five fields with a nested struct).
- **Typed collections.** Unnamed scalar slices and maps (`[]int`, `map[string]string`) are built with typed Go code through a pointer to the field, instead of `reflect.MakeSlice` / `SetMapIndex` (which box every map key). Named element types take the reflect path; tests check over random input that both give equal elements.
- **Field level capacity.** The plan counts the field levels of nested struct fields, so `Bind` allocates the slice once.
- **Response headers** are assigned to the header map directly (`h["Content-Type"] = []string{…}`), without `Header.Set`'s canonicalization.
- **JSON.** A `Map` or `map[string]any` is encoded through a pointer, which `encoding/json` serves a few percent faster; every other value is passed as is. A `JSONAppender` skips `encoding/json`.
- **Numbers** go through `num.F64ToAnyNumOK`, whose boxed result allocates for values that are not small integers.

## Benchmarks

Results depend on the machine. They were captured when this README was generated (2026-11-16), with `go test -run=^$ -bench=. -benchmem ./ctx` (first of five runs):

```console
goos: darwin
goarch: amd64
cpu: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
BenchmarkBindStruct-12                    	 1215940	       984.2 ns/op	    1384 B/op	      15 allocs/op
BenchmarkBindStruct_PointerFields-12      	 3318337	       365.7 ns/op	     296 B/op	       5 allocs/op
BenchmarkBinder_Struct-12                 	 1291992	       929.1 ns/op	    1384 B/op	      15 allocs/op
BenchmarkBody_Bind-12                     	 1247530	       962.7 ns/op	    1384 B/op	      15 allocs/op
BenchmarkBody_BindValue-12                	 3145050	       381.6 ns/op	     240 B/op	       3 allocs/op
BenchmarkBinderOf_Warm-12                 	81902294	        14.18 ns/op	       0 B/op	       0 allocs/op
BenchmarkBindStrArr-12                    	 2197648	       540.2 ns/op	     184 B/op	       8 allocs/op
BenchmarkBinder_StrArr-12                 	 1895290	       631.9 ns/op	     952 B/op	       9 allocs/op
BenchmarkGetTagParams_Single-12           	40735502	        29.85 ns/op	      16 B/op	       1 allocs/op
BenchmarkGetTagParams_Multiple-12         	13706646	        86.01 ns/op	      64 B/op	       1 allocs/op
BenchmarkGetTagParamIndex_WithDot-12      	83117953	        14.37 ns/op	       0 B/op	       0 allocs/op
BenchmarkGetTagParamIndex_NoDot-12        	305625696	         3.929 ns/op	       0 B/op	       0 allocs/op
BenchmarkBodyBind_SliceInt-12             	 3682188	       308.2 ns/op	     248 B/op	       5 allocs/op
BenchmarkBodyBind_SliceStruct-12          	 1000000	      1019 ns/op	    1168 B/op	      16 allocs/op
BenchmarkBodyBind_MapInt-12               	 2976180	       404.4 ns/op	     392 B/op	       4 allocs/op
BenchmarkBodyBind_MapStruct-12            	 1602337	       749.0 ns/op	     816 B/op	      10 allocs/op
BenchmarkBodyGet_Shallow-12               	70524915	        16.49 ns/op	       0 B/op	       0 allocs/op
BenchmarkBodyGet_Deep-12                  	12523936	        98.61 ns/op	       0 B/op	       0 allocs/op
BenchmarkBodyHas_Shallow-12               	77327365	        15.38 ns/op	       0 B/op	       0 allocs/op
BenchmarkBodySet_Deep-12                  	12601377	       103.1 ns/op	       0 B/op	       0 allocs/op
BenchmarkBodyDel_Missing-12               	20962118	        58.08 ns/op	       0 B/op	       0 allocs/op
BenchmarkHTTPContext_BodyJSON-12          	  279868	      4028 ns/op	    1657 B/op	      33 allocs/op
BenchmarkHTTPContext_Init-12              	10443634	       118.2 ns/op	      16 B/op	       1 allocs/op
BenchmarkHTTPContext_Reset-12             	10782450	       115.7 ns/op	      16 B/op	       1 allocs/op
BenchmarkHTTPContext_Text-12              	21880407	        54.83 ns/op	      32 B/op	       2 allocs/op
BenchmarkHTTPContext_TextWithArgs-12      	15389197	        77.88 ns/op	      16 B/op	       1 allocs/op
BenchmarkHTTPContext_JSON_Map-12          	 1369316	       876.2 ns/op	     144 B/op	      10 allocs/op
BenchmarkHTTPContext_JSON_Struct-12       	 2129136	       574.0 ns/op	     224 B/op	       4 allocs/op
BenchmarkHTTPContext_JSON_Slice50-12      	   74990	     15965 ns/op	    4164 B/op	       4 allocs/op
BenchmarkHTTPContext_JSON_Appender-12     	15344856	        77.92 ns/op	      80 B/op	       3 allocs/op
BenchmarkHTTPContext_JSON_RawString-12    	 3032109	       394.7 ns/op	     144 B/op	       6 allocs/op
BenchmarkHTTPContext_JSONP-12             	 1225665	       979.0 ns/op	     224 B/op	      12 allocs/op
BenchmarkHTTPContext_Redirect-12          	 4669641	       259.6 ns/op	     160 B/op	       2 allocs/op
BenchmarkHTTPContext_SetID-12             	31716030	        37.67 ns/op	      16 B/op	       1 allocs/op
BenchmarkHTTPContext_FormURLEncoded-12    	  389557	      2869 ns/op	    7043 B/op	      27 allocs/op
BenchmarkForm_Bind-12                     	 6289000	       190.0 ns/op	     176 B/op	       4 allocs/op
BenchmarkForm_BindValue-12                	11435869	       104.8 ns/op	      32 B/op	       2 allocs/op
BenchmarkHTTPContext_Header-12            	10263790	       116.7 ns/op	      16 B/op	       1 allocs/op
BenchmarkHeader_Bind-12                   	 6060043	       196.3 ns/op	     176 B/op	       4 allocs/op
BenchmarkHeader_BindValue-12              	11640246	       105.2 ns/op	      32 B/op	       2 allocs/op
BenchmarkHTTPContext_Param-12             	 4281499	       280.0 ns/op	     432 B/op	       3 allocs/op
BenchmarkParam_Bind-12                    	 6118668	       196.0 ns/op	     176 B/op	       4 allocs/op
BenchmarkParam_BindValue-12               	10277520	       116.2 ns/op	      32 B/op	       2 allocs/op
BenchmarkHTTPContext_Query-12             	 2726178	       439.5 ns/op	     448 B/op	       5 allocs/op
BenchmarkQuery_Bind-12                    	 3935589	       311.3 ns/op	     312 B/op	       5 allocs/op
BenchmarkQuery_BindValue-12               	 7465959	       161.6 ns/op	      48 B/op	       2 allocs/op
```
