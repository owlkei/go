# 0002: Trie, router and versioning

Status: Approved
Packages: `internal/ds`, `routing`, `versioning`; `internal/str` (delete `Segment`); the `Match`
call in `core/http.go:84-111`

Roadmap: `docs/roadmap/0002-request-hot-path.md`, step 2. Depends on design 0001.

## Goal
Route matching is correct for every path a client can send (today a root route swallows every 404,
and a `$` in a URL breaks parameters), bounded for hostile paths, and three to ten times cheaper:
no allocation and no per-request normalization. Version extraction parses the `Accept` header
properly.

## Public API

### internal/ds

```go
const MaxSegments = 64

var ErrTooManySegments = errors.New("ds: pattern has more than 64 segments")

type Trie[T any] struct { /* unexported */ }

func NewTrie[T any]() *Trie[T]
func (t *Trie[T]) Insert(pattern string, value *T) error
func (t *Trie[T]) Find(path string, params []string) (*T, []string)
func (t *Trie[T]) Len() int // number of patterns that hold a value
```

Breaking (internal): the trie is generic and holds the caller's value at each route end. Removed:
the exported fields `IsEnd`, `Raw`, `Children`, the type `Node`, the `sep` and `supportParams`
parameters (only `/` and `true` were ever used), `Remove` and `ToJSON` (no caller outside tests).
`matchWildcard` is renamed `matchGlob` (unexported, same results).

Pattern syntax of `Insert` (segments split on `/`, empty segments ignored, `""` and `"/"` are the
root): `$` is a parameter, `*` is a wildcard, any other segment containing `*` is a glob, everything
else is static. `value` must not be nil. Inserting a pattern again replaces its value.

### routing

```go
const (
    MaxPathLength   = 4096           // bytes of the request path
    MaxPathSegments = ds.MaxSegments // 64
)

type RouterItem struct {
    Method       string
    Version      string
    Pattern      string
    Index        int               // CHANGED: dense and unique per item, in creation order (0, 1, 2…)
    HandlerIndex int
    Handlers     []ctx.HTTPHandler
    ParamKeys    map[string][]int
    ParamNames   []string          // NEW: ParamNames[i] is the name of the i-th parameter value
    Injectable   any               // NEW: the handler given to AddInjectableHandler, else nil
}

// CHANGED (breaking): was
// Match(method, route, version string) (bool, string, map[string][]int, []string, []ctx.HTTPHandler)
func (r *Router) Match(method, path, version string, params []string) (*RouterItem, []string)

// unchanged signatures
func NewRouter() *Router
func (r *Router) Use(handlers ...ctx.HTTPHandler) *Router
func (r *Router) For(methodInclusions []string, route string, version string) func(handlers ...ctx.HTTPHandler) *Router
func (r *Router) Add(method, route, version string, handler ctx.HTTPHandler) *Router
func (r *Router) AddInjectableHandler(method, route, version string, handler any) *Router
func MethodRouteVersionToPattern(method, route, version string) string
func PatternToMethodRouteVersion(pattern string) (string, string, string)
func ParseToParamKey(str string) (string, map[string][]int)
```

- `Match` returns nil when nothing matches. The item is shared and read-only; its pointer stays the
  same across later `Use` / `For` / `Add` calls on that route.
- Removed (breaking): `Router.Group` and the exported constants `ADD`, `USE`, `FOR`, `GROUP` (the
  three that remain become unexported). `Group` has no caller and is wrong today
  (`router.go:198-211`: `HandlerIndex` is copied from the sub-router although the parent's global
  middlewares are put in front, so a later `Add` on that route replaces a middleware instead of the
  handler; `routePatterns` gets a duplicate entry per method). See Open questions.
- `Router.InjectableHandlers` and `GlobalMiddlewares` stay as they are (`core` reads them);
  `RouterItem.Injectable` holds the same value so that the injection design (0006) can drop the
  per-request map lookup of `core/http.go:121`.
- New bootstrap panic category in `routing/router.go`: `"invalid route: <detail>"` (in
  `color.FmtRed`, like `"invalid handler: ..."`).

Unexported seams the tests rely on (the builder keeps these names):
`(*Router).matchTrie(method, path, version string, params []string) (*RouterItem, []string)`: `Match`
without the static-route map.

**Offered to later designs:** `ParamNames` (0003: `Param.Get` without building a map per request),
`Index` (0005 / 0006 / roadmap 0003: slices indexed by route instead of the per-request string-keyed
maps `catchFnsByRoute`, `lastWildcardSlashIndexByRoute`, `InjectableHandlers`), the `params` buffer
(0003: keep its capacity in the pooled context instead of setting `ParamValues = nil` in `Reset`).

### versioning

No signature changes. The receiver is renamed `v` (it shadows the package name today). Behavior
changes: V3, V4.

## Behavior

### Trie (`Find`)

Segments of `path` are the non-empty pieces between `/`. `Find` never interprets a request segment:
only patterns have special segments.

- **T1 Root.** After `Insert("/", v)` (or `""`), `Find("/")`, `Find("")` and `Find("//")` return `v`.
  An empty trie returns nil for every path.
- **T2 Static.** `Insert("/feeds/all", v)`: `Find("/feeds/all")` → `v`; `/feeds` and `/feeds/all/x`
  → nil.
- **T3 Parameter.** `Insert("/users/$/friends/$", v)`: `Find("/users/7/friends/9", buf[:0])` →
  `v, ["7", "9"]`. A parameter matches exactly one non-empty segment.
- **T4 Literal `$` and `*` in a path.** With T3's pattern, `Find("/users/$/friends/*")` →
  `v, ["$", "*"]`. With only `Insert("/a/b", v)`, `Find("/a/*")` and `Find("/a/$")` → nil.
- **T5 Wildcard in the middle** (`/*/feeds`): matches exactly one segment and captures nothing.
- **T6 Wildcard at the end** (`/lv1/*`): matches zero or more remaining segments: `/lv1`, `/lv1/`,
  `/lv1/a`, `/lv1/a/b/c` all match; nothing is captured. A pattern can use both (`/a/*/b` and
  `/a/*` share the wildcard node).
- **T7 Glob** (`in*.html`, `image.*`, `*.html`): each `*` matches any run of bytes, empty included,
  inside one segment. `in*.html` matches `in.html` and `index.html`, not `in.htm` or `xin.html`.
  `ab*ba` does not match `aba`.
- **T8 Precedence with backtracking.** At each node the candidates for a segment are tried in this
  order, and the first one whose subtree matches the whole rest of the path wins: static child,
  glob children, parameter child, wildcard as one segment, wildcard as the end (T6). When the path
  ends at a node, that node's value wins over its end wildcard.
  - `/users/all/x` + `/users/$/y`: `Find("/users/all/y")` → the second, `["all"]` (today: no match).
  - `/x/*` + `/x/*.html`: `Find("/x/a.html")` → the glob (today: the wildcard, `trie.go:134`).
  - `/f/$` + `/f/*.html`: `Find("/f/a.html")` → the glob, no parameter (today: the parameter;
    the glob route is unreachable).
  - `/lv1/*` + `/lv1/lv2/lv3/pong`: `Find("/lv1/lv2/lv3/extra")` → `/lv1/*` (kept).
  - `/a/$/x` + `/a/*`: `Find("/a/1/y")` → `/a/*` with **no** parameter values.
- **T9 Parameter values belong to the match.** The returned slice is `params` plus exactly one value
  per `$` on the matched pattern's path, in path order; values captured on abandoned branches are
  dropped. On a miss it returns `nil, params` (unchanged length).
- **T10 Order independence.** The result of `Find` does not depend on the order of `Insert` calls.
  Glob children of a node are tried by number of literal bytes, most first, then by pattern in
  byte order: `/g/a*` + `/g/*bc`, `Find("/g/abc")` → `/g/*bc` whichever was inserted first.
  (Today it is insertion order, and `core/module.go:347` registers from a map iteration.)
- **T11 Empty segments.** `Find("//a///b/")` equals `Find("/a/b")`. Dot segments are plain text for
  the trie (the router resolves them: R6).
- **T12 Buffer.** `Find` appends to `params` and allocates only when its capacity is too small.
  `AllocsPerRun` = 0 for a static match, and for a parameter match with a large enough buffer.
- **T13 Limits.** A path with more than `MaxSegments` segments → `nil, params`. `Insert` of a
  pattern with more than `MaxSegments` segments returns `ErrTooManySegments` and changes nothing.
- **T14.** `Len()` counts patterns that hold a value; inserting the same pattern twice counts once.

### Router

Bugs first (each is reproduced today by the input given):

- **R1 Root route (bug).** `Add(GET, "/", "", h)` or `Add(GET, "", "", h)`, then
  `Match(GET, "/", "", nil)` and `Match(GET, "", "", nil)` return that item.
  Today: `trie.go:54` never inserts a pattern without a segment, so `Match(GET, "/", "")` on a
  router built with `Add(GET, "/", …)` returns false.
- **R2 A miss is a miss (bug).** With only a root route registered, `Match(GET, "/nope/at/all", "",
  nil)` returns nil.
  Today: `core/module.go:355` registers the root route as `""`; a miss leaves `matchedRaw == ""` and
  `router.go:181` looks up `routerItemByPattern[""]`, which is the root item. End to end, an app with
  a `READ()` handler answers **every** unknown `GET` path with the root handler and 200 instead of
  404 (verified with `httptest`: `/nope/at/all` → `200 root`).
- **R3 Request bytes are not rewritten (bug).** With `Add(GET, "/users/{id}", …)`:
  `Match(GET, "/users/a b", …)` → params `["a b"]`; `"/users/a**b"` → `["a**b"]`;
  `"/us ers/1"` → nil.
  Today: `router.go:178` runs `str.Enclose` on the request path, which strips white space and
  collapses `**`: the three give `["ab"]`, `["a*b"]` and a match.
- **R4 Literal `$` (bug).** With `/users/{id}` and `/users/{userId}/friends/{friendId}`:
  `Match(GET, "/users/$", …)` → params `["$"]`; `Match(GET, "/users/$/friends/123", …)` →
  `["$", "123"]`.
  Today: `trie.go:120` finds the parameter node under the map key `"$"` as an exact child and
  captures nothing: the first returns no values (`ctx/http_param.go:15` then panics with index out of
  range: a 500 any client can trigger), the second returns `["123"]`, so `userId` reads the friend's
  ID.
- **R5 Invariant.** For every match, `len(params) - len(input params) == len(item.ParamNames)`.
- **R6 Normalization.** `Match(m, p, v, …)` equals `Match(m, path.Clean("/"+p), v, …)` for every
  `p`: a trailing slash and repeated slashes are ignored, `.` and `..` segments are resolved
  (`/g/../f/1` matches `/f/{id}` with `["1"]`), `..` above the root stays at the root. Nothing else
  is changed (R3).
- **R7 Matching is per method and version.** Only routes registered for exactly that method and
  version take part, with the trie rules T1-T11:
  - `GET /items/all` + `POST /items/{id}`: `Match(POST, "/items/all", "", …)` → the `POST` item,
    `["all"]` (today: no match, `router.go:181-186`).
  - `GET /a/all` version `1` + `GET /a/{id}` version `2`: `Match(GET, "/a/all", "2", …)` → the
    version 2 item.
  - `Match(DELETE, …)` on a path registered only for `GET` → nil. An unknown method or version → nil.
  The fallback to `versioning.NeutralVersion` stays in `core` (a second `Match` call).
- **R8 Limits.** `len(path) > MaxPathLength` → nil. More than `MaxPathSegments` segments → nil. Both
  are checked before any other work on the path.
- **R9 Route validation at registration.** `Add`, `For` and `AddInjectableHandler` panic with
  `"invalid route: <route>: <reason>"` when:
  - a segment contains `{` or `}` but is not exactly `{name}` with a non-empty name free of `{`,
    `}` and `/`: `/files/{name}.json`, `/a/{}`, `/a/{x}{y}`, `/a/{b/c}`
    (reason: `parameter must be a whole segment like {name}`). Today these register silently:
    `/files/{name}.json` never matches `/files/a.json`, and `/files/$.json` matches it with no value.
  - the route has more than `MaxPathSegments` segments (reason: `more than 64 segments`).
  `{feed*Id}` stays valid (the name is free text). Routes still go through `str.Enclose` (white space
  stripped, `//` and `**` collapsed), as today.
- **R10 Item fields.** `Pattern == MethodRouteVersionToPattern(method, route, version)`;
  `ParamKeys` as today (`/s/{a}/t/{b}/{a}` → `{"a": [0, 2], "b": [1]}`); `ParamNames` →
  `["a", "b", "a"]` (nil without parameters); `Index` is 0 for the first item created, 1 for the
  next, never reused; `AddInjectableHandler` sets `Injectable` and
  `InjectableHandlers[item.Pattern]`, and still panics with `"invalid handler: <kind> is not a
  handler"` for nil or a non-function.
- **R11 Handler chains (kept; the existing cases of `TestRouterMiddleware` stay the spec).** An item
  exists once `Add` or `For` names its method, route and version. A new item starts with the global
  middlewares registered so far; `Use` appends to every existing item and to the global list; `For`
  appends; `Add` appends the main handler, or replaces the one a previous `Add` put there and moves
  it to the end.
- **R12 Static fast path equals the trie.** For every path, `Match` and `matchTrie` return the same
  item and the same values.
- **R13 Allocations.** `AllocsPerRun` = 0 for `Match` on a static route (with or without a trailing
  slash) and on a parameter route when `cap(params)` is large enough; 0 for a miss.
- **R14 Patterns.** `MethodRouteVersionToPattern(m, "", v) == MethodRouteVersionToPattern(m, "/", v)`
  (`"/|v|/[m]/"`; today the first is `"||/[GET]/"`, which `PatternToMethodRouteVersion` cannot
  parse: slice bounds panic at `fn.go:37`).
- **R15 Pattern round trip.** `PatternToMethodRouteVersion(MethodRouteVersionToPattern(m, r, v))`
  returns `m`, the route without its trailing slash (`""` for the root, as today), and `v`, for any
  method token without `/`, `[` or `]`, and any version without `/` or `|`: also for a method outside
  `HTTPMethods` (`"LINK"`: panics today, `fn.go:38`) and a route with a segment such as `[POST]`
  (today the regexp at `fn.go:11` takes the leftmost `/[POST]` and returns method `POST` for a `GET`
  pattern). A string that is not a pattern returns `"", "", ""` without panicking.
- **R16.** `ParseToParamKey` keeps its results for valid input (`"/a/{x}/b/{y}/{x}/"` →
  `"/a/$/b/$/$/"`, `{"x": [0, 2], "y": [1]}`; no `{` → the input and nil).

### Versioning (`GetVersion`)

- **V1 Query (kept).** The first value of the query key `Key`; empty or absent → `DefaultVersion`.
- **V2 Header (kept).** The header `Key` (any case); empty or absent → `DefaultVersion`.
- **V3 Media type (bug).** The `Accept` header is read as media ranges separated by `,`, each with
  parameters separated by `;`. The version is the value of the first parameter, left to right, whose
  name equals `Key` (ASCII case-insensitive, spaces around the name and value ignored) and whose
  value is not empty; a value in double quotes loses the quotes. None → `DefaultVersion`.
  Today, `versioning.go:57`, a substring search for `Key + "="`, with `Key = "v"`:

  | `Accept` | Today | Expected |
  |---|---|---|
  | `application/json;v=2` | `2` | `2` |
  | `application/json;rev=7` | `7` | default |
  | `application/json;v=2, text/html` | `2, text/html` | `2` |
  | `application/json; v="2"` | `"2"` | `2` |
  | `text/html, application/json;V=3` | default | `3` |
  | `application/json; charset=utf-8; v = 4` | default | `4` |
  | `application/json;v=2;q=0.9` | `2` | `2` |
  | `application/json;v=` | default | default |

- **V4 Custom (bug).** A nil `Extractor`, or one that returns `""`, gives `DefaultVersion`
  (`versioning.go:53-54` returns the empty string today, unlike the three other types).
- **V5.** An unknown `Type` gives `DefaultVersion`. `GetTypeString` keeps its five results.
- **V6.** `GetVersion` allocates nothing for header, media type and custom (`AllocsPerRun` = 0;
  the query type is 0 once the context has parsed the query).

## Performance

**Hot:** `Trie.Find`, `matchGlob`, `Router.Match` (once or twice per request,
`core/http.go:84-86`), `Versioning.GetVersion` (once per request when versioning is on,
`core/http.go:81`). A miss is hot too: any client can send unknown paths.
**Cold** (bootstrap): `Insert`, `Add`, `For`, `Use`, `AddInjectableHandler`, both pattern
functions, `ParseToParamKey`, `GetTypeString`, `Len`.

Where the time goes today (`Match`, 1,000 routes + the measured one; quick run `-count 3`):

| Cost | Evidence |
|---|---|
| `str.Enclose(path.Clean(route), '/')` on every call (`router.go:178`) | 79 ns + 16 B for `/users/all`, 175 ns + 48 B for a four-segment path; always 1 alloc |
| up to four map lookups per segment under reserved keys (`trie.go:120-138`: the segment, `"$"`, `"*"` twice) | `TrieFind_Static` 78 ns for two segments |
| `routerItemByPattern[raw]` (hashes the whole pattern) + a `RouterItem` copied by value, once or twice (`router.go:181-183`) | 29 ns each |
| `make([]string, 0, 4)` for parameter values (`trie.go:131`) | 64 B, 1 alloc |

Planned (a prototype of O1-O4 without globs, wildcards, limits and the method scan was measured in a
scratch copy; allow 5-15 ns for those):

- **O1** Walk the request path as it is: split on `/` by index, skip empty segments. `path.Clean`
  runs only when the path contains `"/."` (the only inputs it can change besides slashes; it returns
  its input without allocating when nothing changes). Type B fast path, proven by R6.
- **O2** One tree per (method, version), found by a linear scan of a short slice (methods ×
  versions, typically under ten entries; cheaper than a map for that size). The trie end holds the
  `*RouterItem`: no pattern-keyed map lookup, no struct copy. This is also what makes R7 correct.
- **O3** A map of fully static routes per tree, keyed by the canonical path (`/users/all`, root
  `/`), probed with the request path minus one trailing slash before the trie. Correct because a
  static child is always the first candidate (T8). Type B fast path, proven by R12.
- **O4** Trie nodes keep the parameter child, the wildcard child and the sorted glob list in their
  own fields; the children map holds static segments only: one map lookup per segment (also fixes
  R4).
- **O5** `Find` appends parameter values to the caller's slice (H1).
- **O6** `PatternToMethodRouteVersion` and `ParseToParamKey` parse by index; both package-level
  regexps go (cold, type A; `PatternToMethodRouteVersion` is 175 ns today).
- **O7** `GetVersion` media type: scan the header by index, compare the name in place; no
  `Key + "="` concatenation per request.

| Benchmark | Now | Prototype | Target (asserted by `benchstat` in the commit) |
|---|---|---|---|
| `RouterMatch_Static` (`/users/all/`) | 238 ns, 16 B, 1 alloc | 18 ns, 0 allocs (86 ns when the map misses and the trie is walked) | ≤ 40 ns, 0 allocs |
| `RouterMatch_Param` (4 segments, 1 value) | 543 ns, 112 B, 2 allocs | 120 ns, 0 allocs with a buffer; 163 ns, 16 B, 1 alloc without | ≤ 160 ns, 0 allocs |
| `RouterMatch_NoMatch` | 203 ns, 24 B, 1 alloc | 49 ns, 0 allocs | ≤ 80 ns, 0 allocs |
| `TrieFind_Static` / `_WithParam` / `_DeepParam` / `_NoMatch` | 78 / 154 / 242 / 116 ns; 0 / 1 / 1 / 0 allocs | not measured alone | each ≤ 60 % of today, 0 allocs with a buffer |
| `GetVersion_MediaType` | 60 ns, 0 allocs | not prototyped | ≤ 60 ns, 0 allocs (correctness first: V3 does more work) |
| `GetVersion_Query` / `_Header` / `_Custom` | 14 / 38 / 3 ns, 0 allocs | unchanged | same |

Per request this removes about 200-400 ns and 1-2 allocations in every baseline scenario
(Static: 1,672 ns, 13 allocs today), and about twice that on a 404, where `core` calls `Match` twice.

**Heavy techniques (approving this design approves exactly these):**

- **H1 Caller-supplied parameter buffer** (hand-managed buffer). `Find` and `Match` append to
  `params` instead of allocating. Benchmark: 163 ns + 1 alloc → 120 ns + 0 allocs on the parameter
  route (prototype above). Constraints: the values are substrings of the request path (as today);
  the returned slice shares the buffer, so it is valid only until the owner reuses it. In step 2
  `core/http.go` passes `c.ParamValues[:0]`; because `HTTPContext.Reset` still sets `ParamValues =
  nil`, one small allocation remains per parameter request (16 B instead of 64 B) until design 0003
  keeps the capacity. `ctx.Param()` copies the values into its own map (`ctx/http_param.go:13-17`),
  so handlers never see the shared slice unless they read `HTTPContext.ParamValues` directly: 0003
  documents that it must not be kept after the request.

No `unsafe`, no `sync.Pool`, no inlining restructure. `Find` recurses to backtrack; the depth is at
most `MaxSegments`.

Recorded, not done: `QueryVersion` makes the context parse the whole query on every request
(`ctx/http_query.go:11`, `url.ParseQuery`); a single-key scan of the raw query would avoid it, but
no baseline scenario enables versioning, so there is no benchmark to justify it yet (0003).

## Concurrency
The router and its tries are written only during `App.Create()` (one goroutine) and are read-only
afterwards. `Match`, `Find` and `GetVersion` keep no state between calls and write nothing shared:
they are safe for any number of concurrent requests, without locks. Registering a route while
requests are served is a data race, as today, and stays unsupported (README). The `params` buffer is
owned by the caller (one per pooled context). `Versioning` is read-only after `EnableVersioning`.

## Security
- **Limits before speed (R8, T13).** The path is client-controlled and bounded only by
  `http.Server.MaxHeaderBytes` (1 MB by default, about 500,000 segments). `MaxPathLength` (4,096
  bytes) and `MaxPathSegments` (64) are checked first; beyond them the request does not match and
  `core` answers 404.
- **Bounded backtracking.** The trie is a tree and `Find` visits a node at most once per call, so a
  lookup costs at most the size of that method's route tree plus one glob test per glob child
  visited, whatever the path. Glob matching is linear in the segment for each `*`.
- **No interpretation of request segments (T4, R3, R4):** closes the client-triggered 500 and the
  shifted parameter values.
- **R2** closes the root route answering for every unknown path (a handler reached under URLs it was
  never registered for, past any path-based assumption in front of the app).
- `$` and `*` are reserved in route patterns: a route cannot contain a literal `$` or `*` segment
  (unchanged; README).
- Decoded `%2F` in `URL.Path` is a separator, as today (matching on `RawPath` is out of scope).
- V3 stops other `Accept` parameters (`rev=`, `q=` neighbors) from choosing the version.

## Impact
- `core/http.go:84-111`: adapt to the new `Match` (item nil check; `c.ParamKeys = item.ParamKeys`,
  `c.ParamValues = values`, `item.Pattern`, `item.Handlers`; pass `c.ParamValues[:0]`). No other
  change in `core`; its maps keyed by pattern keep working because `Pattern` is unchanged for every
  route except the root's spelling (R14, which every caller builds through the same function).
- `internal/str`: delete `Segment`, its test and benchmark (no caller left).
- Behavior changes users can see: R1-R4, R7 (requests that returned 404 or the wrong route now
  match), T8 (glob before parameter and wildcard), R9 (new bootstrap panic), V3, V4. V4 interacts
  with `core/http.go:90`: an empty version on a matched route answers 410; with V4 a custom
  extractor returning `""` uses `DefaultVersion` first.
- Tests to rewrite: `internal/ds/*_test.go`, `routing/*_test.go` (the `Match` signature; the
  `ToJSON`, `Group` and `Remove` tests are deleted), `versioning/versioning_test.go`.
- Docs: `README.md` in `internal/ds`, `routing`, `versioning` (pattern syntax, precedence table,
  limits, reserved characters, "not safe to register while serving", performance notes O1-O5 and H1,
  benchmark table). `.owlky/routing-system.md` §2-4 describes regexp matching, `:id` syntax,
  conflict detection between parameter routes and a `param["*"]` capture that do not exist: rewrite
  from this design. `CLAUDE.md` › Bootstrap-time Error Convention: add `"invalid route: ..."`
  (`routing/router.go`). `.owlky/package-reference.md`, `metadata-performance.json`.
- Found while reading, owned by other designs: `ctx/http_param.go:15` indexes `ParamValues` without
  a length check (0003; R5 makes it safe); `core/module.go:347` registers routes from a map
  iteration (roadmap 0003; T10 removes the visible effect); `PatternToMethodRouteVersion` returns
  `""` for the root route, which `core/app.go:664` patches for the log (0005 may make it `"/"`).

## Test plan

Internal test packages, one function per behavior, `test.DiffMessage`.

| Rule | Test | Cases |
|---|---|---|
| T1 | `TestTrie_Find_Root`, `TestTrie_Find_EmptyTrie` | `"/"`, `""`, `"//"`; inserted as `"/"` and as `""` |
| T2 | `TestTrie_Find_Static`, `TestTrie_Find_StaticPrefixIsMiss`, `TestTrie_Find_StaticLongerIsMiss` | |
| T3 | `TestTrie_Find_Param`, `TestTrie_Find_TwoParams` | |
| T4 | `TestTrie_Find_LiteralDollarIsValue`, `TestTrie_Find_LiteralStarIsText` | |
| T5 | `TestTrie_Find_WildcardMiddleOneSegment` | zero and two segments miss |
| T6 | `TestTrie_Find_WildcardEndZeroSegments`, `_WildcardEndManySegments`, `_WildcardMiddleAndEnd` | |
| T7 | `TestMatchGlob_Prefix`, `_Suffix`, `_Middle`, `_TwoStars`, `_Overlap`, `TestTrie_Find_Glob` | the existing `TestMatchWildcard` table, one function per shape |
| T8 | `TestTrie_Find_BacktracksStaticToParam`, `_GlobBeforeWildcard`, `_GlobBeforeParam`, `_ParamBeforeWildcard`, `_FallsBackToOuterWildcard`, `_NodeValueBeforeEndWildcard` | the five examples |
| T9 | `TestTrie_Find_DropsAbandonedParams`, `_MissKeepsParams`, `_AppendsAfterExisting` | a buffer that already holds one value |
| T10 | `TestTrie_Find_InsertOrderIndependent`, `TestTrie_Find_GlobOrder` | the same patterns inserted in every permutation of 4 |
| T11 | `TestTrie_Find_SkipsEmptySegments` | |
| T12 | `TestTrie_Find_StaticNoAlloc`, `_ParamNoAllocWithBuffer` | `AllocsPerRun` |
| T13 | `TestTrie_Find_TooManySegments`, `TestTrie_Insert_TooManySegments` | 64 passes, 65 fails |
| T14 | `TestTrie_Len`, `TestTrie_Insert_ReplacesValue` | |
| R1 | `TestRouter_Match_Root`, `TestRouter_Match_RootRegisteredEmpty` | |
| R2 | `TestRouter_Match_MissWithRootRoute` | `/nope/at/all`, `/users` |
| R3 | `TestRouter_Match_KeepsSpaces`, `_KeepsStars`, `_SpaceInStaticIsMiss` | |
| R4 | `TestRouter_Match_LiteralDollarParam`, `_LiteralDollarDeep` | |
| R5 | `TestRouter_Match_ParamCountInvariant` | 5,000 random paths over the segments `a b all $ * 1` against ten routes, fixed seed |
| R6 | `TestRouter_Match_TrailingSlash`, `_RepeatedSlashes`, `_DotSegments`, `_DotDotAboveRoot`, `TestRouter_Match_FastEqualsCleaned` | the last: 10,000 random paths over `a b / . $ * space`, `Match(p)` against `Match(path.Clean("/"+p))` |
| R7 | `TestRouter_Match_MethodAware`, `_VersionAware`, `_UnknownMethod`, `_UnknownVersion`, plus the existing `TestRouterMatchSamePathDifferentMethodAndVersion` cases | |
| R8 | `TestRouter_Match_PathTooLong`, `_TooManySegments` | 4,096 and 4,097 bytes; 64 and 65 segments |
| R9 | `TestRouter_Add_PanicsOnPartialParam`, `_PanicsOnEmptyParam`, `_PanicsOnTwoParamsInSegment`, `_PanicsOnTooManySegments`, `TestRouter_Add_AllowsStarInParamName`, `TestRouter_For_PanicsOnPartialParam` | the message contains `invalid route: ` and the reason |
| R10 | `TestRouter_Item_Pattern`, `_ParamKeysAndNames`, `_IndexDense`, `_PointerStableAfterUse`, `TestAddInjectableHandler_SetsInjectable`, the two existing panic tests | |
| R11 | `TestRouterMiddleware` (existing cases, adapted to the new `Match`) | |
| R12 | `TestRouter_Match_StaticEqualsTrie` | every registered static route with and without a trailing slash, and 10,000 random paths: `Match` against `matchTrie` |
| R13 | `TestRouter_Match_StaticNoAlloc`, `_ParamNoAllocWithBuffer`, `_MissNoAlloc` | |
| R14 | `TestMethodRouteVersionToPattern_RootSpellings` | plus the existing table |
| R15 | `TestPatternToMethodRouteVersion_RoundTrip`, `_UnknownMethod`, `_MethodLikeSegment`, `_NotAPattern`, `_Root` | |
| R16 | `TestParseToParamKey` (existing) | |
| existing | `TestRouterMatch` | all 19 cases stay valid (adapted); they pin the current precedence that T8 keeps |
| V1 | `TestGetVersion_Query`, `_QueryAbsentUsesDefault` | existing |
| V2 | `TestGetVersion_Header`, `_HeaderKeyCaseInsensitive` | |
| V3 | `TestGetVersion_MediaType`, `_MediaTypeIgnoresOtherParams`, `_MediaTypeStopsAtComma`, `_MediaTypeUnquotes`, `_MediaTypeNameCaseInsensitive`, `_MediaTypeSecondRange`, `_MediaTypeEmptyValueUsesDefault` | the table of V3 |
| V4 | `TestGetVersion_CustomEmptyUsesDefault`, `_CustomNilExtractor` | |
| V5 | `TestGetVersion_UnknownType`, `TestGetTypeString` | |
| V6 | `TestGetVersion_NoAlloc` | header, media type, custom |
| Concurrency | `TestRouter_Match_ConcurrentNoRace` | 8 goroutines × 2,000 matches on one router, each with its own buffer, under `-race` |

Benchmarks (all `for b.Loop()` + `b.ReportAllocs()`, 1,000 random routes plus the measured one, as
today): `BenchmarkRouterMatch_Static`, `_StaticTrailingSlash`, `_Param` (with a reused buffer),
`_ParamNoBuffer`, `_Wildcard`, `_Glob`, `_NoMatch`, `_DotSegments` (the slow path),
`_Parallel` (per-goroutine buffer and sink); `BenchmarkRouterAdd`, `BenchmarkRouterUse` (setup
outside the loop with `b.StopTimer` kept); `BenchmarkTrieFind_Static`, `_WithParam`, `_DeepParam`,
`_NoMatch`, `_Backtrack`; `BenchmarkTrieInsert`; `BenchmarkMatchGlob`;
`BenchmarkPatternToMethodRouteVersion`, `BenchmarkParseToParamKey`,
`BenchmarkMethodRouteVersionToPattern`; the four `BenchmarkGetVersion_*`. Delete
`BenchmarkTrieRemove`. Done when `benchstat` (`-count 5`, before → after) meets the targets above and
`make bench` shows the scenario change.

## Open questions
1. **Precedence (T8): static, then glob, then parameter, then wildcard, with backtracking.** Today
   it is static, parameter, wildcard, glob, without backtracking, which leaves a glob route
   unreachable next to a parameter or wildcard sibling. Recommendation: the order above (most
   specific first, every registered route reachable). It changes which handler answers only for
   apps that register a glob beside a parameter or wildcard at the same position.
2. **Limits: 4,096 bytes and 64 segments, answered with 404, as constants.** Recommendation: accept
   as constants now; roadmap 0003 (production defaults) can make them `App` options and answer 414.
3. **Remove `Router.Group`?** Unused and wrong (see Public API). Recommendation: remove. Keeping it
   means a rule "the group item's `HandlerIndex` counts the parent's global middlewares" and tests.
4. **Reject `/files/{name}.json` at bootstrap (R9) or support parameters inside a segment?**
   Recommendation: reject now (it silently never matched); a glob `/files/*.json` covers the
   routing need.
5. **V4: an empty result of a custom extractor uses `DefaultVersion`.** Today it reaches `core` as
   an empty version, which answers 410 on a matched route. Recommendation: use the default, like
   the three other types.
6. **Delete `Trie.Remove` and `Trie.ToJSON`** (no caller; `ToJSON` is only printed by a skipped
   test). Recommendation: delete; a devtool view of the route tree can be built from the router's
   items when it is needed.

## Decisions (2026-11-16)

Approved by Claude under the user's one-time delegation for roadmap 0002 (see its Log). Heavy
techniques are **not** approved: they are built without and listed as proposals for the user.
The open questions above are answered as follows.

- Q1 precedence static > glob > param > `*`, with bounded backtracking: yes.
- Q2 limits as constants (4,096 bytes, 64 segments), answered with 404; configurability and 414 are
  roadmap 0003: yes.
- Q3 remove `Router.Group`, unexport `ADD/USE/FOR`: yes.
- Q4 reject partial-segment params (`/files/{name}.json`) at bootstrap with a new
  `"invalid route: ..."` panic category: yes; add the category to `CLAUDE.md`'s list.
- Q5 a custom extractor returning `""` uses `DefaultVersion`: yes.
- Q6 delete `Trie.Remove` and `Trie.ToJSON`: yes.
- **H1 (caller-supplied parameter buffer): not approved.** `Find` / `Match` return freshly allocated
  parameter values (one allocation per parameter request). Shape the internal API so H1 can be added
  later without another breaking change, and keep the benchmark that measures it.
