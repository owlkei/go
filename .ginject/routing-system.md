# Routing System

**Optimization**: Decision trees, pseudo-algorithms, explicit rules. Minimal explanatory prose.

## 1. Route Naming Convention (Method Names → HTTP Routes)

### 1.1 Base Tokens (HTTP Methods)

| Token | HTTP Method | Example |
|-------|-------------|---------|
| `READ` | GET | `READ()` → `GET /` |
| `CREATE` | POST | `CREATE()` → `POST /` |
| `UPDATE` | PUT | `UPDATE()` → `PUT /` |
| `MODIFY` | PATCH | `MODIFY()` → `PATCH /` |
| `DELETE` | DELETE | `DELETE()` → `DELETE /` |
| `PREFLIGHT` | OPTIONS | `PREFLIGHT()` → `OPTIONS /` |

**Rule**: Exactly one base token per method name. If missing, method is NOT a route handler.

### 1.2 Separator Tokens (Path Construction)

| Token | Meaning | Example |
|-------|---------|---------|
| `BY` | Path parameter | `READ_BY_ID` → `GET /{id}` |
| `AND` | Additional path segment | `READ_BY_ID_AND_NAME` → `GET /{id}/{name}` |
| `OF` | Sub-resource | `READ_OF_POSTS` → `GET /posts` |
| `ANY` | Wildcard segment | `READ_ANY` → `GET /*` |
| `FILE` | File serving | `READ_FILE` → `GET /*` (static files) |

**Examples**:
- `READ_BY_ID` → `GET /{id}`
- `READ_BY_ID_AND_PAGE` → `GET /{id}/{page}`
- `READ_OF_COMMENTS` → `GET /comments`
- `CREATE_COMMENT` → `POST /comment`
- `DELETE_BY_ID_AND_VERSION` → `DELETE /{id}/{version}`
- Parameters of one resource keep their declaration order for any count: `READ_x_BY_a_AND_b_AND_c` → `GET /x/{a}/{b}/{c}/`, `READ_m_BY_a_AND_b_OF_u_BY_d` → `GET /u/{d}/m/{a}/{b}/`
- `READ_ANY` → `GET /*` (catch-all)
- `READ_FILE_ANY` → `GET /*` (static file serving)

### 1.3 Versioning Token

| Token | Meaning | Example |
|-------|---------|---------|
| `VERSION_X` | API version | `READ_VERSION_1` → `GET /v1/...` |
| `VERSION_X_BY_ID` | Versioned with param | `READ_VERSION_2_BY_ID` → `GET /v2/{id}` |

**Rule**: Version tag is appended AFTER all other tokens. Examples:
- `READ_VERSION_1` → version 1 of READ
- `READ_BY_ID_VERSION_2` → version 2 of READ/{id}
- `CREATE_OF_USERS_VERSION_3` → version 3 of POST /users

### 1.4 Reserved/Special Tokens

| Token | Special Meaning |
|-------|-----------------|
| `PREFLIGHT` | Generates OPTIONS method |
| `SERVE` | Static file serving (GET only) |
| `FILE` | File upload/download marker |

---

## 2. Route Matching Algorithm

Code: `routing/router.go` (`Router.Match`), `internal/ds/trie.go` (`Trie.Find`). Design: `docs/design/0002-routing.md`.
There is no regexp, no `:id` syntax and no conflict detection between parameter routes.

### 2.1 Route syntax (registration)

Method names become routes with `{name}` parameters (`READ_BY_ID` → `GET /{id}`) and `*` for `ANY`
(`READ_ANY` → `GET /*`, `READ_ANY_FILE_HTML` → `GET /*.html`). Each route is normalized with
`str.Enclose` (white space removed, `//` and `**` collapsed), then compiled segment by segment:

| Route segment | Trie segment | Matches in a request |
|---|---|---|
| `{name}` (whole segment, non-empty name without `{` `}`) | `$` parameter | exactly one non-empty segment, captured |
| `*` | wildcard | one segment in the middle; zero or more segments at the end of a route |
| other text with `*` (`in*.html`, `*.css`, `{feed*Id}` is a parameter, not a glob) | glob | one segment; each `*` = any run of bytes, empty included |
| anything else | static | that exact segment |

Bootstrap panics (`color.FmtRed`), from `Add`, `For` and `AddInjectableHandler`:
- `invalid route: <route>: parameter must be a whole segment like {name}` — `/files/{name}.json`, `/a/{}`, `/a/{x}{y}`, `/a/{x`.
- `invalid route: <route>: more than 64 segments`.

`$` and `*` are reserved: a route cannot hold a literal `$` or `*` segment.

### 2.2 Match decision tree

```
Router.Match(method, path, version, params):
    1. tree = the trie of exactly (method, version)      linear scan of a short slice
       none → nil                                       (core retries with versioning.NeutralVersion)
    2. len(path) > 4096 → nil
    3. path starts with "." or contains "/." → if > 64 segments → nil; path = path.Clean("/" + path)
       (".." above the root stays at the root; nothing else in the request is rewritten)
    4. fully static route? map[path minus one trailing slash, "" → "/"] → item, params unchanged
    5. trie.Find(path, params):
         > 64 non-empty segments → nil
         walk the non-empty segments between "/" (repeated and trailing slashes ignored)
```

A miss is a miss: a root route answers only `/`, `""` and `//`, never an unknown path (404).

### 2.3 Trie precedence (with backtracking)

At each node the candidates for the next segment are tried in this order; the first whose subtree
matches the whole rest of the path wins, otherwise the walk backs up and tries the next one:

```
1. static child (one map lookup)
2. glob children, most literal bytes first, then pattern byte order (independent of insert order)
3. parameter child (captures the segment)
4. wildcard as one segment (continue in the wildcard's subtree)
5. wildcard as the end (zero or more remaining segments)
Path ended at a node: the node's own route, else its end wildcard.
```

Examples: `/users/all/x` + `/users/{id}/y` → `/users/all/y` is the second (`["all"]`);
`/x/*` + `/x/*.html` → `/x/a.html` is the glob; `/f/{id}` + `/f/*.html` → `/f/a.html` is the glob;
`/lv1/*` + `/lv1/lv2/lv3/pong` → `/lv1/lv2/lv3/extra` is `/lv1/*`;
`/a/{x}/x` + `/a/*` → `/a/1/y` is `/a/*` with no parameter values.

Request segments are never interpreted: `/users/$` and `/users/*` give the values `$` and `*` to a
parameter route, and match no static route.

**Cost bound**: each trie node is visited at most once per lookup (a node sits at a fixed depth), so a
lookup costs at most the size of that method's tree plus one glob test per visited glob child.

### 2.4 Concurrency

Routes are registered during `App.Create()` only; the router is read-only afterwards and `Match` keeps
no state. Registering while serving is a data race and unsupported.

---

## 3. RouterItem Structure

```go
type RouterItem struct {
    Method       string            // "GET", "POST", etc.
    Version      string            // API version ("" when unversioned)
    Pattern      string            // MethodRouteVersionToPattern(method, route, version): "/users/{id}/|v2|/[GET]/"
    Index        int               // dense and unique, in creation order (0, 1, 2…)
    HandlerIndex int               // position of the main handler in Handlers
    Handlers     []ctx.HTTPHandler // global middlewares + For handlers + main handler
    ParamKeys    map[string][]int  // name → positions in the parameter values
    ParamNames   []string          // ParamNames[i] names the i-th value; nil without parameters
    Injectable   any               // the handler given to AddInjectableHandler, else nil
}
```

**Usage**:
1. `Router.Match()` returns the shared `*RouterItem` (read-only; the pointer is stable across later `Use` / `For` / `Add`) and the parameter values, or nil.
2. `item.Handlers` run in order (`core/http.go`); a nil entry is the injectable main handler.
3. `core` copies `item.ParamKeys` and the values into `HTTPContext.ParamKeys` / `ParamValues`.

The root route's pattern is `/||/[GET]/` whether it was registered as `""` or `/`.

---

## 4. Path Parameter Extraction

### 4.1 Named parameters

**Route**: `READ_BY_ID` → `GET /{id}`; `READ_BY_USER_ID_OF_FRIENDS_BY_FRIEND_ID` style names give several.

**In handler**:
```go
func (c UserController) READ_BY_ID(param ctx.Param) string {
    return "User " + param.Get("id")
}
```

**Mechanism**:
1. Registration: each `{name}` segment becomes `$` in the trie; `ParamNames` lists the names in path order, `ParamKeys` maps a name to its positions (a repeated name has several: `/s/{a}/t/{b}/{a}` → `{"a": [0, 2], "b": [1]}`).
2. Match: one value per `$` on the matched route's path, in path order; values captured on abandoned branches are dropped, so `len(values) == len(item.ParamNames)` always.
3. Values are substrings of the request path, unchanged (spaces, `*`, `$` kept). The slice is the `params` argument plus the values; with values it is a fresh allocation (core passes nil).
4. `ctx.Param()` builds its map from `ParamKeys` + `ParamValues`.

### 4.2 Wildcards and globs

`*` and globs capture nothing: there is no `param["*"]`. A file route (`SERVE`, `READ_FILE…`) reads
the rest of the URL itself (`core/http.go` `serveContent`, from the route's `serveIndex`). A
directory answers its `index.html` or 404, never a listing. `core` registers routes with
`AddInjectableHandler` and finds them with `Match`, passing `c.ParamValues[:0]` as the buffer that
`Find` appends to (design 0002 H1); it no longer uses `Use`, `For` or `RouterItem.Handlers`.

---

## 5. Version-Specific Routes

### 5.1 Versioning Mechanism

**Pattern**: `READ_VERSION_1` → `GET /v1/...`

**Registration**:
```go
// Method names define versions implicitly
func (c *UserController) READ_VERSION_1() { /* v1 handler */ }
func (c *UserController) READ_VERSION_2() { /* v2 handler */ }
```

**Behavior**:
- Same path ("/users"), different handlers per version
- Version tag added to URL prefix automatically
- Request "/v1/users" → routes to READ_VERSION_1
- Request "/v2/users" → routes to READ_VERSION_2

### 5.2 Version Precedence

If both versioned and unversioned routes exist:
- Versioned routes take precedence
- Unversioned falls back if version not matched

---

## 6. Static File Serving

### 6.1 FILE Token

**Pattern**: `READ_FILE_ANY` → `GET /*` (static file serving)

**Behavior**:
- Serves files from filesystem
- Path after route prefix = file path
- Example: "GET /static/img.png" → serves from `static/img.png`

**Router Handling**:
- Marks route as static file (uses `http.FileServer` under hood)
- No dependency injection in static routes
- Bypasses normal handler pipeline

---

## 7. Route Registration Process

### 7.1 Auto-Discovery at app.Create()

**Process**:
```
1. Collect all controllers from module tree
2. For each controller:
   a. Reflect on all methods
   b. For each method:
      i. Check if method name matches route pattern
      ii. If yes: extract HTTP method, path, version
      iii. Register in routing.Router
   c. Build handler chain (middleware + handler)
3. Detect conflicts (panic if found)
4. Build trie for fast matching
```

### 7.2 Handler Chain Building

**For route: `READ_BY_ID` in UserController**:

```
1. Create RouterItem
2. Add global middlewares (if any)
3. Add module middlewares (scoped to this route's module)
4. Add route guard (if any)
5. Add route interceptor pre-handler
6. Add actual handler method
7. Add route interceptor post-handler
8. Store in item.Handlers[]
```

**Execution**: Handlers array executed sequentially via `ctx.HTTPHandler` chain

---

## 8. Controller Registration

### 8.1 Controller Discovery

**Requirement**: Embed `common.HTTP` or `common.WS`

```go
type UserController struct {
    common.HTTP  // Makes this an HTTP controller
    UserService  // Dependency to inject
}

func (c UserController) NewController() Controller {
    return c
}

// Routes automatically discovered:
func (c UserController) READ_BY_ID() { /* GET /{ID} */ }
func (c UserController) CREATE() { /* POST / */ }
```

### 8.2 Instance Lifecycle

- Controller constructed once per app at `Create()`: fields with provider types are injected, then `NewController()` is called
- Every request runs the handler method on that value (a value receiver gets a copy per call)

---

## 9. Routing Edge Cases & Rules

### 9.1 Matching Precedence

Static, then glob, then parameter, then wildcard, with backtracking: §2.3.

### 9.2 Ambiguous Routes

Only an identical pattern (same method, route and version) panics at `app.Create()` with
`route conflict: ...` (`common/http.go`, `AddHandlerToRouterMap`). Two routes that differ only in
parameter names (`/{id}` and `/{name}`) share one trie node: the one registered last answers.
Overlapping shapes (`/{id}` beside `/all` or `/*.html`) are legal and resolved by §2.3.

### 9.3 Path Parameter Naming

**Rule**: All `:paramName` tokens must match method name

**Valid**:
```go
// Method name: READ_BY_ID_AND_NAME
// Routes to: GET /{id}/{name} ✓
func (c *UserController) READ_BY_ID_AND_NAME() { ... }
```

**Invalid**:
```go
// Method name defines params, but handler expects different names
// Routes to: GET /{id} (only :id extracted)
// Param map won't have "userId" → error at runtime
func (c *UserController) READ_BY_ID(params ctx.Param) {
    // This fails:
    id := params.Get("userId")  // NOT "id"
}
```

---

## 10. MethodRouteVersionToPattern() Function

**Purpose**: Convert method name → HTTP method + route + version

**Input**: "READ_BY_ID_VERSION_1"

**Output**:
```
httpMethod = "GET"
route = "/{id}"
version = "1"
pattern = "GET /{id}" (used for routing lookup)
```

**Algorithm**:
1. Extract base token (READ, CREATE, etc.) → HTTP method
2. Extract version token (VERSION_X) if present
3. Parse remaining tokens (BY, AND, OF, ANY) → build route path
4. Combine: METHOD + " " + ROUTE = pattern

