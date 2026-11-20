# Routing

Internal package: applications declare routes with controller method names (`READ_users_BY_id`) instead.

*Package `routing` holds the HTTP router of Owlkei Go: it registers routes with their handler chains at bootstrap and matches each request to one route by method, version and path.*

- [Routing](#routing)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [Constants and variables](#constants-and-variables)
  - [`RouterItem` Fields](#routeritem-fields)
  - [`Router` Fields](#router-fields)
  - [`Router` Methods](#router-methods)
    - [NewRouter](#newrouter)
    - [Add](#add)
    - [AddInjectableHandler](#addinjectablehandler)
    - [For](#for)
    - [Use](#use)
    - [Match](#match)
  - [Functions](#functions)
    - [MethodRouteVersionToPattern](#methodrouteversiontopattern)
    - [PatternToMethodRouteVersion](#patterntomethodrouteversion)
    - [ParseToParamKey](#parsetoparamkey)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

`core` builds one router per app from the controllers' method names; you rarely use it directly.

## Key Features
- One route tree per method and version: a route answers only for the method and version it was registered for
- Precedence static > glob > parameter > wildcard, with backtracking: every registered route is reachable
- Request paths are matched as sent (no rewriting of spaces or stars); `.` and `..` are resolved
- Limits checked first: 4,096 bytes and 64 segments per path
- No allocation for a static route or a miss; none for a route with parameters when the caller passes a buffer with enough capacity (one without)

## Usage

```go
package main

import (
	"fmt"
	"net/http"

	"go.owlkei.com/owlkei/internal/ctx"
	"go.owlkei.com/owlkei/internal/routing"
)

func main() {
	r := routing.NewRouter()
	r.Add(http.MethodGet, "/users/all", "", func(c *ctx.HTTPContext) {})
	r.Add(http.MethodGet, "/users/{userId}/friends/{friendId}", "", func(c *ctx.HTTPContext) {})

	item, params := r.Match(http.MethodGet, "/users/7/friends/9", "", nil)
	fmt.Println(item.Pattern, item.ParamNames, params)

	item, _ = r.Match(http.MethodGet, "/users/all/", "", nil)
	fmt.Println(item.Pattern)

	item, _ = r.Match(http.MethodPost, "/users/all", "", nil)
	fmt.Println(item == nil)
}
```

Output:

```console
/users/{userId}/friends/{friendId}/||/[GET]/ [userId friendId] [7 9]
/users/all/||/[GET]/
true
```

## Constants and variables

| Name | Type | Value |
|---|---|---|
| `SERVE` | `string` | `"SERVE"`: the file-serving operation, routed as `GET` |
| `MaxPathLength` | `int` (untyped constant) | `4096`: longer request paths never match |
| `MaxPathSegments` | `int` (untyped constant) | `64`: the most segments of a request path or a route |
| `OperationsMapHTTPMethods` | `map[string]string` | each HTTP method to itself, and `SERVE` to `GET` |
| `HTTPMethods` | `[]string` | the nine HTTP methods and `SERVE` |

Route syntax: segments are split on `/`. `{name}` is a parameter and must be a whole segment; `*` is a wildcard (one segment in the middle, zero or more at the end); other text with `*` is a glob inside one segment (`*.html`, `in*.html`); everything else is static. `$` and `*` are reserved: a route cannot hold a literal `$` or `*` segment. Routes are normalized with white space removed and `//` and `**` collapsed.

## `RouterItem` Fields

A `RouterItem` is one route for one method and version. `Match` returns a pointer to it: it is shared, read-only, and stays the same across later `Use`, `For` and `Add` calls on that route.

### Method
Type: `string`

The HTTP method, such as `GET`.

### Version
Type: `string`

The version the route was registered for; `""` when unversioned.

### Pattern
Type: `string`

`MethodRouteVersionToPattern(Method, route, Version)`, such as `/users/{id}/||/[GET]/`. `core` keys its per-route maps with it.

### Index
Type: `int`

Dense and unique per item, in creation order: 0 for the first item, then 1, 2, and so on. Never reused.

### HandlerIndex
Type: `int`

The position of the main handler (the one `Add` set) in `Handlers`; `-1` when `Add` was never called.

### Handlers
Type: `[]ctx.HTTPHandler`

The chain to run: the global middlewares registered before the item existed, then the handlers of `For`, `Use` and `Add` in call order. A nil entry is the injectable main handler of `AddInjectableHandler`.

### ParamKeys
Type: `map[string][]int`

Each parameter name to its positions in the parameter values. `/s/{a}/t/{b}/{a}` gives `{"a": [0, 2], "b": [1]}`. Nil without parameters.

### ParamNames
Type: `[]string`

`ParamNames[i]` is the name of the i-th parameter value: `["a", "b", "a"]` for the route above. Nil without parameters.

### Injectable
Type: `any`

The handler given to `AddInjectableHandler`, else nil.

## `Router` Fields

### GlobalMiddlewares
Type: `[]ctx.HTTPHandler`

Default: empty

The handlers given to `Use`, in order. A new item starts with them.

### InjectableHandlers
Type: `map[string]any`

Default: empty

The handlers given to `AddInjectableHandler`, keyed by `RouterItem.Pattern`.

## `Router` Methods

Register every route during bootstrap, then only call `Match`. `Match` is safe for any number of goroutines; registering while requests are served is a data race and is not supported.

### NewRouter

Returns an empty router.

#### Returns
1. `*Router`: a router with no route.

#### Usage

```go
r := routing.NewRouter()
```

### Add

Sets the main handler of a route. The item is created when it does not exist yet, starting with the global middlewares.

#### Parameters
1. `method` `string`: the HTTP method.
2. `route` `string`: the route, such as `/users/{id}`.
3. `version` `string`: the version, `""` for none.
4. `handler` `ctx.HTTPHandler`: the main handler.

#### Returns
1. `*Router`: the router itself.

#### Usage

```go
r.Add(http.MethodGet, "/users/{id}", "", func(c *ctx.HTTPContext) {})
```

#### Rules
- Panics with `invalid route: <route>: parameter must be a whole segment like {name}` for `/files/{name}.json`, `/files/pre{name}`, `/a/{}`, `/a/{x}{y}`, `/a/{b/c}`, `/a/{x` and `/a/x}`.
- Panics with `invalid route: <route>: more than 64 segments` for 65 segments; 64 are accepted.
- `{feed*Id}` is a valid parameter named `feed*Id`.
- A second `Add` on the same route replaces the first main handler and moves the new one to the end of the chain.

### AddInjectableHandler

Registers a route whose main handler `core` calls with injected arguments: it adds a nil main handler and records `handler`.

#### Parameters
1. `method` `string`: the HTTP method.
2. `route` `string`: the route.
3. `version` `string`: the version.
4. `handler` `any`: a function.

#### Returns
1. `*Router`: the router itself.

#### Usage

```go
r.AddInjectableHandler(http.MethodGet, "/users/{userId}", "", func(param ctx.Param) string {
	return param.Get("userId")
})
```

#### Rules
- Sets `RouterItem.Injectable` and `InjectableHandlers[item.Pattern]` to `handler`; items created with `Add` keep `Injectable` nil.
- Panics with `invalid handler: ...` for nil or a value that is not a function.
- Panics with `invalid route: ...` like `Add`.

### For

Returns a function that appends handlers to a route for each of the given methods.

#### Parameters
1. `methodInclusions` `[]string`: the methods.
2. `route` `string`: the route.
3. `version` `string`: the version.

#### Returns
1. `func(handlers ...ctx.HTTPHandler) *Router`: appends `handlers` to each item, creating it when needed.

#### Usage

```go
r.For([]string{http.MethodGet, http.MethodPost}, "/users", "")(func(c *ctx.HTTPContext) {})
```

#### Rules
- Panics with `invalid route: ...` like `Add`.

### Use

Appends global middlewares: to every existing item and to `GlobalMiddlewares`, so later items start with them.

#### Parameters
1. `handlers` `...ctx.HTTPHandler`: the middlewares.

#### Returns
1. `*Router`: the router itself.

#### Usage

```go
r.Use(func(c *ctx.HTTPContext) {})
```

#### Rules
- `Use`, then `Add` on every method, `For`, `Use` again: the chain is `[use 1, use 2, add, for, use 3]`.

### Match

Returns the route that matches a request, and its parameter values.

#### Parameters
1. `method` `string`: the request method.
2. `path` `string`: the request path, such as `URL.Path`.
3. `version` `string`: the request version, `""` for none.
4. `params` `[]string`: values to keep in front of the captured ones, and the buffer the values are appended to; nil is fine.

#### Returns
1. `*RouterItem`: the matched item, or nil.
2. `[]string`: `params` plus one value per route parameter, in path order (written into `params`' backing array when its capacity is enough, otherwise a new slice); `params` unchanged on a miss. The result shares the buffer: it is valid until the caller reuses it.

#### Usage

```go
item, params := r.Match(http.MethodGet, "/users/7", "", nil)
if item != nil {
	fmt.Println(params[item.ParamKeys["id"][0]])
}
```

#### Rules
- Root: a route registered as `/` or `""` matches `/`, `""` and `//`. It does not match `/nope/at/all`, `/users` or `/nope`.
- Request bytes are not rewritten: `/users/{id}` gives `["a b"]` for `/users/a b` and `["a**b"]` for `/users/a**b`; `/us ers/1` misses.
- A literal `$` is a value: `/users/$` gives `["$"]`, `/users/$/friends/123` gives `["$", "123"]`.
- Every match returns exactly `len(item.ParamNames)` values after `params`.
- `Match(m, p, v)` equals `Match(m, path.Clean("/"+p), v)`: trailing and repeated slashes are ignored (`//users///7//` gives `["7"]`), `.` and `..` are resolved (`/g/../f/1` matches `/f/{id}`), and `..` above the root stays at the root (`/../../f/1` matches `/f/{id}`, `/..` is the root).
- Only routes of exactly that method and version take part: with `GET /items/all` and `POST /items/{id}`, `POST /items/all` is the `POST` route with `["all"]`; with `/a/all` in version `1` and `/a/{id}` in version `2`, `/a/all` in version `2` is `/a/{id}`. An unknown method or version misses.
- Precedence: static, then glob, then parameter, then wildcard, with backtracking (see `internal/ds`); for example `/lv1/foo/bar/` matches `/lv1/*`, `/lv1/lv2/file/index.html/` matches `lv1/lv2/file/in*.html` next to `/lv1/lv2/{id2}`.
- A path of 4,096 bytes can match; 4,097 bytes miss. 64 segments can match; 65 miss.
- `Match` returns the same item and values as a walk of the trie alone (without the static-route map).
- No allocation for a static route (with or without a trailing slash) or a miss.
- Safe for concurrent calls.

## Functions

### MethodRouteVersionToPattern

Builds the key of a route: the route, then `|version|/`, then `[method]/`.

#### Parameters
1. `method` `string`: the method.
2. `route` `string`: the route; normalized like a registered route.
3. `version` `string`: the version.

#### Returns
1. `string`: the pattern, such as `/users/{userId}/|v2|/[POST]/`.

#### Usage

```go
pattern := routing.MethodRouteVersionToPattern(http.MethodGet, "/users/{id}", "")
```

#### Rules
- `""` and `/` give the same root pattern: `/||/[GET]/`, `/|v2|/[GET]/`.
- An empty method gives `[]`: `/feeds/all/||/[]/`.

### PatternToMethodRouteVersion

Splits a pattern back into method, route and version.

#### Parameters
1. `pattern` `string`: a pattern built by `MethodRouteVersionToPattern`.

#### Returns
1. `string`: the method.
2. `string`: the route without its trailing slash (`""` for the root).
3. `string`: the version.

#### Usage

```go
method, route, version := routing.PatternToMethodRouteVersion("/users/$/|v2|/[POST]/")
```

#### Rules
- Round trip for any method without `/`, `[` or `]` (also `LINK`, outside `HTTPMethods`) and any version without `/` or `|`.
- A segment such as `[POST]` stays in the route: the method is the last bracket.
- A string that is not a pattern (`""`, `/`, `/a/b/`, `[GET]/`, `/[GET]/`, `/a/|v|/[GET`) returns `"", "", ""` and never panics.

### ParseToParamKey

Replaces each `{name}` with `$` and returns the positions of each name.

#### Parameters
1. `str` `string`: a route.

#### Returns
1. `string`: the route with `$` for each parameter.
2. `map[string][]int`: each name to its positions, or nil without parameters.

#### Usage

```go
route, keys := routing.ParseToParamKey("/users/{userId}/friends/{friendId}/")
```

#### Rules
- `/a/{x}/b/{y}/{x}/` gives `/a/$/b/$/$/` and `{"x": [0, 2], "y": [1]}`.
- Without `{`, it returns the input and nil.

## Performance notes

- Request paths are walked as they are: no copy, no normalization. `path.Clean` runs only when the path starts with `.` or contains `/.` (the only inputs it changes besides slashes, which the walk ignores). A test compares both over 10,000 random paths.
- One route tree per (method, version), found by a linear scan of a short slice; the trie end holds the `*RouterItem`, so there is no pattern-keyed lookup and no struct copy.
- Fully static routes also sit in a map per tree keyed by the canonical path; the request path minus one trailing slash is probed first. A static child is always the first candidate of the trie, so the map gives the same answer (tested against the trie alone over 10,000 random paths).
- Parameter values are substrings of the request path, appended to the caller's buffer at the end of the match (a new slice only when the buffer is too small). `core` passes `ctx.HTTPParamValues(c)[:0]`, so a parameter route allocates nothing for its values on a warm context.
- `PatternToMethodRouteVersion` and `ParseToParamKey` parse by index, without regexps.

## Benchmarks

Results depend on the machine. They were captured when this README was generated, with `go test -run=^$ -bench=. -benchmem ./routing` (1,000 random routes plus the measured one):

```console
goos: darwin
goarch: amd64
pkg: go.owlkei.com/owlkei/internal/routing
cpu: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
BenchmarkPatternToMethodRouteVersion-12        	36271851	        33.14 ns/op	       0 B/op	       0 allocs/op
BenchmarkParseToParamKey-12                    	 2631632	       451.9 ns/op	     480 B/op	       7 allocs/op
BenchmarkMethodRouteVersionToPattern-12        	 5004204	       240.2 ns/op	      60 B/op	       3 allocs/op
BenchmarkRouterAdd-12                          	  435902	      2758 ns/op	    1851 B/op	      22 allocs/op
BenchmarkRouterUse-12                          	  228550	      5899 ns/op	    1608 B/op	     101 allocs/op
BenchmarkRouterMatch_Static-12                 	30792842	        39.22 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterMatch_StaticTrailingSlash-12    	30277532	        39.53 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterMatch_Param-12                  	10369945	       115.5 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterMatch_ParamNoBuffer-12          	 8469910	       142.1 ns/op	      16 B/op	       1 allocs/op
BenchmarkRouterMatch_Wildcard-12               	10409584	       115.2 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterMatch_Glob-12                   	 9083280	       131.9 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterMatch_NoMatch-12                	18149378	        66.28 ns/op	       0 B/op	       0 allocs/op
BenchmarkRouterMatch_DotSegments-12            	 8482810	       143.2 ns/op	      32 B/op	       2 allocs/op
BenchmarkRouterMatch_Parallel-12               	24128798	        46.54 ns/op	      16 B/op	       1 allocs/op
```
