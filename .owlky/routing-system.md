# Routing System

**Optimization**: Decision trees, pseudo-algorithms, explicit rules. Minimal explanatory prose.

## 1. Route Naming Convention (Method Names → HTTP Routes)

Code: `internal/common/fn.go` (`ParseFuncNameToURL(fnName, style)`), `internal/common/http.go`
(`PathStyle`, `Path`, `AddHandlerToRouterMap`). Design: `docs/design/0019-route-dsl.md`. Full rule
list: `internal/common/README.md` › Route DSL.

### 1.1 Grammar

```
<OPERATION>[_<resource>][_BY_<param>[_AND_<param>…]][_ANY][_OF_<resource>…][_VERSION_<version>]
```

- First word = operation; no operation → not a route, never checked (`findUser`, `Helper_FB`).
- `OF` separates resources; the parent comes first in the path.
- Resource words (before `BY`/`AND`) of one resource are joined by the controller's `PathStyle`:
  `-` (`PathStyleKebab`, default) or `_` (`PathStyleAsWritten`).
- After the operation, the only upper-case words allowed are `BY`, `AND`, `OF`, `ANY`, `FILE`,
  `VERSION` (HTTP) and `ANY`, `ALL` (WebSocket). Digits and caseless letters don't count.

### 1.2 Operations

| Token | HTTP method | Example |
|-------|-------------|---------|
| `READ` | GET (+ HEAD) | `READ` → `GET /`; `READ_users` → `GET /users` |
| `CREATE` | POST (201) | `CREATE_users` → `POST /users` |
| `UPDATE` | PUT | `UPDATE_users_BY_id` → `PUT /users/{id}` |
| `MODIFY` | PATCH | `MODIFY_users_BY_id` → `PATCH /users/{id}` |
| `DELETE` | DELETE | `DELETE_users_BY_id` → `DELETE /users/{id}` |
| `PREFLIGHT` | OPTIONS | `PREFLIGHT_users` → `OPTIONS /users` |
| `RENDER` | GET page (+ HEAD), needs `EnableRender` | `RENDER_bookings` → `GET /bookings`, template `bookings.html` or `bookings/index.html` |
| `SUBMIT` | POST on the path of the `RENDER_` with the same words, same controller | `SUBMIT_new_OF_bookings` → `POST /bookings/new` |
| `SERVE` | static files (GET) | `SERVE_ANY_OF_assets` → `GET /assets/*` |

`common.HTTPOperations` maps them; `common.PageOperation(name)` returns `RENDER`, `SUBMIT` or `""`.

### 1.3 Route words

| Token | Meaning | Example |
|-------|---------|---------|
| `BY` | first parameter of the current resource | `READ_users_BY_id` → `/users/{id}` |
| `AND` | one more parameter (interchangeable with `BY` for the parser) | `READ_files_BY_dir_AND_name` → `/files/{dir}/{name}` |
| `OF` | parent resource, placed before | `READ_posts_BY_post_id_OF_users_BY_user_id` → `/users/{user_id}/posts/{post_id}` |
| `ANY` | `*`, glued to neighbours (`READ_me_ANY_bers` → `/me*bers`); a segment of its own after parameters (`READ_files_BY_name_ANY` → `/files/{name}/*`) and at the end of a `SERVE` resource (`SERVE_static_ANY` → `/static/*`) | `READ_ANY` → `/*` |
| `FILE` | the joint before the preceding word becomes `.`, that word lower-cased | `READ_robots_txt_FILE` → `/robots.txt`; `READ_site_map_xml_FILE` → `/site-map.xml` |
| `VERSION` | the rest of the name is the version (joined with `_`, never kebab-cased) | `READ_users_VERSION_2` → `GET /users`, version `2` |
| `ALL` | WebSocket only, last word: `**` | `SUBSCRIBE_chat_ALL` → `chat.**` (websocket-system.md) |

### 1.4 Words, parameters, style

| Name | Kebab (default) | As written |
|---|---|---|
| `READ_user_profile` | `/user-profile` | `/user_profile` |
| `READ_user_profile_OF_big_orgs` | `/big-orgs/user-profile` | `/big_orgs/user_profile` |
| `READ_userProfile`, `READ_Users`, `READ_404` | unchanged (words copied as written) | same |
| `READ_user__profile`, `READ__users` | `/user-profile`, `/users` (empty words dropped) | `/user_profile`, `/users` |
| `READ_users_BY_user_id`, `READ_users_BY_user__id` | `/users/{user_id}`, `/users/{user__id}` (parameter name verbatim) | same |
| `READ_new_OF_bookings` | `/bookings/new` (non-resource segment through `OF`) | same |
| `READ_edit_OF_bookings_BY_id` | `/bookings/{id}/edit` | same |
| `READ_BY_id_OF_users` | `/users/{id}` (no empty segment) | same |

- `PathStyle` applies to resource words only: never to `Prefix` strings, `Path`, parameter names,
  versions or WebSocket events. It changes nothing at request time (`GET /user_profile` → 404 under
  kebab).
- Set per module: `ModuleBuilder().PathStyle(owlky.PathStyleAsWritten)`. A controller takes the
  first explicit style in this order: its own module, then each importer in discovery order
  (depth first, `Imports` order, the order module prefixes stack). None → kebab. An invalid value
  panics `invalid module: PathStyle needs owlky.PathStyleKebab or owlky.PathStyleAsWritten`.
- Deep nesting: put the shared part in `instance.Prefix("users/{user_id}/posts/{post_id}")`;
  prefixes may hold `{param}` segments and are never rewritten.

### 1.5 Exact path (`Path`)

`instance.Path("/oauth2/token", instance.CREATE_token)` in `NewController()` replaces the path the
name spells. The operation still gives the method, `VERSION` the version, and controller and module
prefixes still apply; the path is never kebab-cased and may hold `{name}` and `*`. The name is still
checked (§1.6). Panics, all `invalid route:`: no handler; a handler that is not an HTTP route of the
controller; two `Path` for one handler; `?` or `#` in the path; `Path` or targeted `Prefix` on a
`SUBMIT_` (`… a SUBMIT_ uses the path of its RENDER_`).

### 1.6 Names that panic at `Create()` (`invalid route: …`)

| Name | Message after `invalid route: ` |
|---|---|
| `READ_orgs_FB`, `READ_logo_PNG_FILE`, `READ_users_CREATE_x`, `READ_ALL`, `SUBSCRIBE_chat_V2` | `<W> in <Name> is upper case but not a route word; write it in lower case or camelCase` |
| `READ_users_BY`, `READ_users_BY_id_AND`, `DELETE_BY_ID_AND_VERSION` | `<BY\|AND> in <Name> has no parameter name` |
| `READ_files_BY_name_ANY_x`, `READ_users_BY_id_FILE` | `<W> in <Name> comes after a parameter; start the next resource with OF` |
| `READ_OF_users`, `READ_users_OF`, `READ_a_OF_OF_b` | `OF in <Name> needs a resource or a parameter on both sides` |
| `READ_txt_FILE`, `READ_FILE`, `READ_FILE_ANY` | `FILE in <Name> needs a name and an extension before it` |
| `READ_logo_png_FILE_FILE` | `FILE twice in one resource of <Name>` |
| `READ_logo_ANY_FILE` | `FILE in <Name> needs a word as the extension, not ANY` |
| `SERVE_x_ANY_ANY`, `READ_ANY_ANY_FILE` | `ANY twice in a row in <Name>; one ANY already matches any text` |
| `RENDER_files_ANY`, `SUBMIT_x_VERSION_2`, `RENDER_feed_xml_FILE` | `<W> in <Name>: a page route has no wildcard` / `has no version` / `has no file extension` |
| `SUBSCRIBE_chat_ALL_x` | `ALL in <Name> must be the last word` |

---

## 2. Route Matching Algorithm

Code: `internal/routing/router.go` (`Router.Match`), `internal/ds/trie.go` (`Trie.Find`). Design: `docs/design/0002-routing.md`.
There is no regexp, no `:id` syntax and no conflict detection between parameter routes.

### 2.1 Route syntax (registration)

Method names become routes with `{name}` parameters (`READ_BY_ID` → `GET /{id}`) and `*` for `ANY`
(`READ_ANY` → `GET /*`, `READ_ANY_html_FILE` → `GET /*.html`). Each route is normalized with
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
2. `item.Handlers` run in order (`internal/core/http.go`); a nil entry is the injectable main handler.
3. `core` hands `item.ParamKeys` and the values to `ctx.HTTPSetParams` (the context keeps them unexported; `ctx.HTTPParamValues` reads them).

The root route's pattern is `/||/[GET]/` whether it was registered as `""` or `/`.

---

## 4. Path Parameter Extraction

### 4.1 Named parameters

**Route**: `READ_BY_ID` → `GET /{ID}`; `READ_friends_BY_friend_id_OF_users_BY_user_id` → `GET /users/{user_id}/friends/{friend_id}` gives several.

**In handler**:
```go
func (c UserController) READ_users_BY_id(param owlky.Param) string {
    return "User " + param.Get("id")
}
```

**Mechanism**:
1. Registration: each `{name}` segment becomes `$` in the trie; `ParamNames` lists the names in path order, `ParamKeys` maps a name to its positions (a repeated name has several: `/s/{a}/t/{b}/{a}` → `{"a": [0, 2], "b": [1]}`).
2. Match: one value per `$` on the matched route's path, in path order; values captured on abandoned branches are dropped, so `len(values) == len(item.ParamNames)` always.
3. Values are substrings of the request path, unchanged (spaces, `*`, `$` kept). The slice is the `params` argument plus the values; with values it is a fresh allocation (core passes nil).
4. `ctx.Param()` builds its map from the keys and values set by `ctx.HTTPSetParams`.

### 4.2 Wildcards and globs

`*` and globs capture nothing: there is no `param["*"]`. A file route (`SERVE`) reads
the rest of the URL itself (`internal/core/http.go` `serveContent`, from the route's `serveIndex`). A
directory answers its `index.html` or 404, never a listing. `core` registers routes with
`AddInjectableHandler` and finds them with `Match`, passing `ctx.HTTPParamValues(c)[:0]` as the buffer that
`Find` appends to (design 0002 H1); it no longer uses `Use`, `For` or `RouterItem.Handlers`.

---

## 5. Version-Specific Routes

Code: `internal/versioning`, `internal/core/http.go`. The version is **not** part of the path: it is
read from the query, a header, the `Accept` media type or a custom extractor
(`app.EnableVersioning(&owlky.VersioningOptions{…})`), and each route is registered per
(method, path, version).

- `READ_users_VERSION_1` and `READ_users_VERSION_2` are two routes on `GET /users`.
- `VERSION_NEUTRAL` (`versioning.NeutralVersion`) answers every version and requests without one;
  `Match` retries with the neutral version.
- With versioning on, a route without `VERSION` no longer answers (404, or 410 for a request without
  a version when `DefaultVersion` is empty).
- Page routes (`RENDER_`, `SUBMIT_`) can't carry `VERSION` (§1.6) and are registered neutral
  (`AddHandlerToRouterMapWithPageVersion`), like the asset and public file routes.

---

## 6. Static File Serving

### 6.1 `SERVE`

`SERVE_ANY_OF_assets` (or `SERVE_assets_ANY`) → `GET /assets/*`; the handler returns the directory.
The rest of the URL is the file path, opened through `os.Root` (`http.ServeFileFS`); a directory
answers its `index.html` or 404, never a listing; hidden segments 404 except `.well-known`. The
handler gets injected parameters like any route.

### 6.2 Built-in routes of `EnableRender` (design 0018)

| Route | Added when | Serves |
|---|---|---|
| `GET /assets/*` (`addAssetRoute`, `internal/core/http_asset.go`) | `Dir/assets` exists or a page CSS / JS file exists | `Dir/assets` with hashed URLs, and `/assets/_pages/<path>` for page CSS / JS |
| `GET /<path>` per file of `Dir/public` (named `public/<path>`) | `Dir/public` exists | the file as it is, `no-cache` + `ETag`; version-neutral |

A user route of the same method and shape panics `route conflict: …` (`SERVE_ANY_OF_assets`;
`public/robots.txt method is conflicted with READ_robots_txt_FILE method`). Errors on these routes
are JSON.

### 6.3 Page routes

`RENDER_` / `SUBMIT_` routes are HTTP routes with `HTTPLayer.IsPage` and `HTTPLayer.Template` (the
template path bound at `Create()`, `internal/render` `Bind`). Template = final path (after module and
controller prefixes, `PathStyle`, `Path`) as `<path>.html` or `<path>/index.html`; missing, double,
reserved (`partials/`, `assets/`, `public/`) or error-page names (`RENDER_404`) panic
`invalid render: …`; a final path with `*` panics `invalid route: … holds a wildcard`; a `SUBMIT_`
without its `RENDER_` panics `invalid handler: …`. Details: `internal/core/README.md` › Pages.

---

## 7. Route Registration Process

At `app.Create()` (`internal/core/container.go`, `bindHTTPController`):

```
1. discover the module tree: module prefixes and the PathStyle of each controller (styleByController)
2. for each controller method:
     common.AddHandlerToRouterMap(&HTTP, modulePrefixes, style, name, handler)
       parse the name (panics of §1.6) → exact Path → controller prefixes → module prefixes
     page route (RENDER_/SUBMIT_) → EnableRender check, SUBMIT_ pairing, render.Bind(route) → template
3. check final patterns per app: route conflict: …
4. plan each handler's arguments once (fn_plan.go); register with routing.AddInjectableHandler
5. add the asset route and the public file routes; render.CheckBindings (an .html without a route panics)
```

The pipeline per request (middlewares, guards, interceptors, handler, filters) is built from the
plan, not from `RouterItem.Handlers`: request-pipeline.md.

---

## 8. Controller Registration

### 8.1 Controller Discovery

**Requirement**: Embed `common.HTTP` (API and page routes) and/or `common.WS` (the role names are
`fieldNameByRole` in `internal/core`). There is no page embed: `RENDER_` / `SUBMIT_` methods of an
HTTP controller are the page routes, and API and page routes may share a controller.

```go
type UserController struct {
    owlky.HTTP // makes this an HTTP controller
    UserService  UserService
}

func (c UserController) NewController() owlky.Controller {
    c.Prefix("v1", c.READ_users)
    c.Path("/me", c.READ_profile)
    return c
}

func (c UserController) READ_users() []string     { return nil } // GET /v1/users
func (c UserController) READ_users_BY_id() string { return "" }  // GET /users/{id}
func (c UserController) READ_profile() string     { return "" }  // GET /me
func (c UserController) CREATE() string           { return "" }  // POST /
```

### 8.2 Instance Lifecycle

- Controller constructed once per app at `Create()`: fields with provider types are injected, then `NewController()` is called
- Every request runs the handler method on that value (a value receiver gets a copy per call)

---

## 9. Routing Edge Cases & Rules

### 9.1 Matching Precedence

Static, then glob, then parameter, then wildcard, with backtracking: §2.3.

### 9.2 Ambiguous Routes

Two routes of one app with the same method, version and shape (parameter names don't count:
`/{id}` and `/{name}` conflict) panic at `Create()` with
`route conflict: <name> method is conflicted with <name> method` (`internal/core/container.go`,
per app; `SERVE_static_ANY` and `SERVE_ANY_OF_static` are the same route). An exact path equal to
another route's path conflicts the same way. Overlapping shapes (`/{id}` beside `/all` or
`/*.html`) are legal and resolved by §2.3.

### 9.3 Path Parameter Naming

A parameter is read by the exact text after `BY` / `AND` in the method name (or by the `{name}` of a
`Prefix` or `Path`):

```go
// READ_users_BY_user_id → GET /users/{user_id}
func (c UserController) READ_users_BY_user_id(param owlky.Param) string {
    return param.Get("user_id") // param.Get("userId") or ("id") is ""
}
```

---

## 10. `ParseFuncNameToURL`

`common.ParseFuncNameToURL(fnName string, style common.PathStyle) (method, route, version string)`:

```
ParseFuncNameToURL("READ_users_BY_id_OF_posts_BY_post_id_VERSION_2", PathStyleKebab)
  → "GET", "/posts/{post_id}/users/{id}/", "2"
ParseFuncNameToURL("RENDER_new_OF_bookings", PathStyleKebab) → "GET", "/bookings/new/", ""
ParseFuncNameToURL("findUser", PathStyleKebab)             → "", "", ""
```

The route is always enclosed in `/`. `routing.MethodRouteVersionToPattern(method, route, version)`
builds the router key (`/users/{id}/|2|/[GET]/`). A name that starts with an operation but breaks a
rule of §1.6 panics.
