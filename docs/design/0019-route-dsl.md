# 0019: Route DSL: kebab-case paths, `PathStyle`, exact paths, `ALL`

Status: Approved (by delegation, 2026-11-18: roadmap 0008 › Decisions)
Packages: root `owlkei` (1 alias, 2 constants); `internal/common` (the parser, `PathStyle`, `(*HTTP).Path`);
`internal/core` (`ModuleBuilder.PathStyle`, the style of each controller, the `Create()` panics);
`sample/`, `bench/` and the docs (migration).
Roadmap: `docs/roadmap/0008-server-rendered-html.md`, steps 11 to 13 (and 18, 19 for the migration).
Related: design 0018 amendment 2 (pages by route) uses the paths this design produces.

## Goal
A method name gives the URL most APIs use: `READ_user_profile` answers `GET /user-profile`. An app that
wants `_` in its URLs says so once on its root module (`PathStyleAsWritten`), and a reusable module that
sets its own style keeps its URLs in every host. One route whose path no name can spell gets it written
by hand with `Path`. A name the parser would misread stops `Create()` instead of serving another URL.

## Decisions in one table

The maintainer's decisions (roadmap 0008 › "Route DSL", "Pages by route") are not reopened. This design
adds the ones marked "design".

| # | Decision | By | Why |
|---|---|---|---|
| D1 | `_` between two words of one resource is `-` in the path. camelCase, parameter names, versions, `Prefix` strings and exact paths are never rewritten | maintainer | Go identifiers cannot hold `-`, and most URLs use it |
| D2 | `PathStyle` on the module builder, values `PathStyleKebab` (default) and `PathStyleAsWritten`, inherited through imports like `Prefix`; an explicit setting wins | maintainer | One line on the root module switches an app; a reusable module's URLs never change with its host |
| D3 | A word in upper case that is not a route word stops `Create()` | maintainer | A future route word can never change an existing route silently |
| D4 | `ALL` is the WebSocket `**` | maintainer | `ANY` became one segment when `pattern` moved to glob syntax |
| D5 | A module imported by two parents with different styles gets **one** style: the first explicit setting met in the order `Prefix` stacks prefixes (the owning module, then each importer in discovery order: depth-first, `Imports` order) | design | Verified on `9fe673b`: with `Prefix`, such a module's controller is bound once and gets both parents' prefixes, the first-discovered parent innermost (`/root/b/a/m/ping`). There is one controller, so there is one style; the same walk decides it |
| D6 | The exact path is a method of the embed: `instance.Path("/oauth2/token", instance.CREATE_token)` | design | See "Why `Path`" below |
| D7 | The word before `FILE` is a resource word, so the upper-case rule applies to it: `READ_logo_png_FILE`, not `READ_logo_PNG_FILE` | design | The extension is lower-cased anyway; an upper-case spelling would be a second way to write the same route, and an exemption would be a hole in D3 |
| D8 | After the first word, the route words are `BY`, `AND`, `OF`, `ANY`, `FILE`, `VERSION` (HTTP) and `ANY`, `ALL` (WebSocket). An operation word later in a name (`READ_users_CREATE_x`) falls under D3 | design | One rule and one message instead of a second "misplaced operation" rule |
| D9 | Empty words (`__`, a `_` at either end) are dropped in a resource, as today; a **parameter name** is the exact text of the method name, `__` included | design | `__` is not an escape (maintainer). `param.Get("user__id")` must find the parameter of `BY_user__id`: "a parameter keeps its name as written" |
| D10 | A route word with nothing to act on stops `Create()` (B13 to B16) instead of being dropped | design | Today `READ_users_BY` registers `/users/{}`, `READ_OF_users` registers `/users//`, and `READ_files_BY_name_ANY_x` drops `ANY_x` silently. Same reason as D3; nothing in the repo's apps uses them (checked) |

### Why `Path` (D6)

| Spelling | Verdict |
|---|---|
| `instance.Path("/oauth2/token", instance.CREATE_token)` in `NewController()` | **Chosen.** The same shape as `Prefix(v, handlers...)` and `BindGuard(g, handlers...)`: a method of the embed, the handler passed as a method value, so a renamed handler fails to compile. `PathStyle` shapes paths from names, `Path` writes one by hand: the two names explain each other |
| A struct tag | Impossible: Go has no tags on methods |
| A comment directive (`//owlkei:path /oauth2/token`) | Needs source parsing at run time or a generator; invisible to `go vet`; a second mechanism |
| A map returned by a method (`Paths() map[string]string`) | Stringly typed handler names; a new controller method shape |
| An escape inside the name (`READ_oauth2__token`) | The maintainer dropped `__` escapes as hard to read |

`Path` replaces only what the name would spell. The operation word still gives the HTTP method, `VERSION`
still gives the version, and controller and module prefixes still apply, so a reusable module stays
mountable (the ORM analogy: a column override keeps the table prefix).

## Public API

```go
package owlkei

type PathStyle = common.PathStyle

const (
	PathStyleKebab     = common.PathStyleKebab
	PathStyleAsWritten = common.PathStyleAsWritten
)
```

```go
package common

type PathStyle int

const (
	PathStyleKebab PathStyle = iota + 1
	PathStyleAsWritten
)

func (r *HTTP) Path(v string, handlers ...any) *HTTP
```

```go
package core

func (m *ModuleBuilder) PathStyle(style common.PathStyle) *ModuleBuilder
```

| Addition | Why users need it |
|---|---|
| `owlkei.PathStyle` | The type of the builder's parameter; `<Type><Value>` constants need a named type (design 0017 › Naming rules) |
| `owlkei.PathStyleKebab` | The explicit setting of a reusable module that must keep `-` in every host. The zero value means "not set" and is not exported |
| `owlkei.PathStyleAsWritten` | APIs whose URLs keep `_` (`/payment_intents`) |
| `ModuleBuilder().PathStyle(style)` | Sets the style of a module and of what it imports |
| `(*HTTP).Path(v, handlers...)` | The exact path of one route (or of several handlers that share a path under different methods) |

Nothing else is exported. The words `ALL`, and `RENDER` / `SUBMIT` of design 0018 amendment 2, are text in
method names, not identifiers.

Exact lines `make api` adds to `api/owlkei.txt` for this design (amendment 2 of design 0018 removes five
more lines; any other diff is a defect):

```
+pkg owlkei, const PathStyleAsWritten common.PathStyle = 2
+pkg owlkei, const PathStyleKebab common.PathStyle = 1
+pkg owlkei, type PathStyle = common.PathStyle
+reach common.HTTP, method (*common.HTTP) Path(string, ...any) *common.HTTP
+reach common.PathStyle, type int
+reach core.ModuleBuilder, method (*core.ModuleBuilder) PathStyle(common.PathStyle) *core.ModuleBuilder
```

As an application writes it:

```go
var AppModule = owlkei.ModuleBuilder().
	PathStyle(owlkei.PathStyleAsWritten).
	Imports(BillingModule).
	Controllers(TokenController{}).
	Build().
	Prefix("api")

func (instance TokenController) NewController() owlkei.Controller {
	instance.Path("/oauth2/token", instance.CREATE_token)
	return instance
}

func (instance TokenController) CREATE_token() owlkei.Map  { return owlkei.Map{} }
func (instance TokenController) READ_payment_intents() []string { return nil }
```

`POST /api/oauth2/token` and `GET /api/payment_intents`.

### Internal signatures (not importable; the tester calls them)

```go
package common

func ParseFuncNameToURL(fnName string, style PathStyle) (method, route, version string)
func ParseWSFuncNameToEvent(fnName string) (string, bool)
func AddHandlerToRouterMap(r *HTTP, modulePrefixes []string, style PathStyle, fnName string, handler any)
```

| Function | Change |
|---|---|
| `ParseFuncNameToURL` | Gains `style`. Still returns `"", "", ""` for a name that does not start with an operation. For a name that does, it panics with the `invalid route:` messages of B9 and B13 to B16. `route` is still enclosed in `/` |
| `ParseWSFuncNameToEvent` | Same signature. Panics with B9 and B19 for a name that starts with `SUBSCRIBE_` |
| `AddHandlerToRouterMap` | Gains `style`; applies the exact path of B22 before the prefixes |

## Behavior

Bootstrap panics follow `"<category>: <detail>"`, red (`color.FmtRed`), raised during `Create()` (the
parser and `Path` run inside it). Tests match the uncolored text given here. In the messages, `<Name>` is
the method name and `<W>` the word. Routes are written as `ParseFuncNameToURL` returns them.

### Step 11: kebab-case and the upper-case rule

A name is split at `_`. The first word is the operation. `OF` separates resources; inside a resource, the
words before `BY` / `AND` are its **resource words**.

| # | Rule |
|---|---|
| B1 | With `PathStyleKebab`, the resource words of one resource are joined with `-`: `READ_user_profile` → `/user-profile/`. With `PathStyleAsWritten` they are joined with `_`: `/user_profile/`. `OF` still makes a new segment in both styles |
| B2 | A word is copied as written: camelCase (`READ_userProfile` → `/userProfile/`), digits (`READ_orders_2024` → `/orders-2024/`, `READ_404` → `/404/`, `READ_2fa_codes` → `/2fa-codes/`), mixed case (`READ_Users` → `/Users/`) and non-ASCII letters (`READ_é_cafés` → `/é-cafés/`). Nothing is lower-cased or split at a capital |
| B3 | A parameter name is the exact text of the method name between the `_` after `BY` / `AND` and the `_` before the next route word (or the end of the name), in both styles: `BY_user_id` → `{user_id}`, `BY_user__id` → `{user__id}`, `BY__id` → `{_id}`, `BY_ID` → `{ID}`. It is never kebab-cased and never checked by B9 |
| B4 | The version is unchanged: the words after `VERSION` joined with `_`, empty words dropped, never kebab-cased and never checked by B9 (`READ_user_profile_VERSION_v1_v1` → `/user-profile/`, version `v1_v1`; `VERSION_V2` is allowed) |
| B5 | `ANY` glues to its neighbours with no separator in both styles (`READ_me_ANY_bers` → `/me*bers/`); the other joints of the same resource follow B1 (`READ_big_me_ANY_bers_x` → `/big-me*bers-x/`) |
| B6 | In a `SERVE` name, an `ANY` that ends a resource after at least one word is still a segment of its own; the words before it follow B1: `SERVE_static_files_ANY` → `/static-files/*/`, `SERVE_ANY_OF_static_files` → `/static-files/*/` (the same route) |
| B7 | `FILE` turns the joint before the word that precedes it into `.` and lower-cases that word; the other joints follow B1: `READ_site_map_xml_FILE` → `/site-map.xml/`, `READ_a_b_FILE_c` → `/a.b-c/` |
| B8 | Empty words in a resource are dropped in both styles: `READ_user__profile` → `/user-profile/`, `READ__users` and `READ_users_` → `/users/` |
| B9 | A resource word (HTTP) or any word after `SUBSCRIBE` (WebSocket) that holds at least one upper-case letter and no lower-case letter (`unicode.IsUpper`, `unicode.IsLower`; digits and caseless letters do not count), and is not a route word of D8, panics `invalid route: <W> in <Name> is upper case but not a route word; write it in lower case or camelCase`. Refused: `READ_orgs_FB`, `READ_orgs_V2`, `READ_plan_B`, `READ_URL_shortener`, `READ_ÉCOLE`, `READ_users_CREATE_x`, `READ_ALL`, `READ_logo_PNG_FILE`, `SUBSCRIBE_chat_PERSON_ANY`. Allowed: `READ_orgsFB`, `READ_apiV2`, `READ_Users`, `READ_404`, `READ_BY_ID`, `READ_users_VERSION_V2` |
| B10 | A method whose first word is not an operation (`Helper_FB`, `findUser`) is not a route and is never checked |
| B11 | `Prefix` strings (module and controller) are never rewritten: `Prefix("payment_intents")` stays `/payment_intents/` under `PathStyleKebab`, and `Prefix("my-api")` stays under `PathStyleAsWritten` |
| B12 | The style changes nothing at request time: matching, parameters, the trailing slash rule, 404 and 405 are as before. `GET /user_profile` answers 404 when the route is `/user-profile` |

Malformed names (D10), all `invalid route:`:

| # | Name | Message after `invalid route: ` |
|---|---|---|
| B13 | `BY` or `AND` with no name before the next route word or the end (`READ_users_BY`, `READ_users_BY_OF_x`, `READ_users_BY_id_AND`, `DELETE_BY_ID_AND_VERSION`) | `<BY\|AND> in <Name> has no parameter name` |
| B14 | After the parameters of a resource, any word other than one `ANY` (B20), then `OF`, `VERSION` or the end (`READ_files_BY_name_ANY_x` → `x`, `READ_users_BY_id_FILE` → `FILE`) | `<W> in <Name> comes after a parameter; start the next resource with OF` |
| B15 | `OF` with no resource and no parameter before it, or none after it (`READ_OF_users`, `READ_users_OF`, `READ_a_OF_OF_b`, `SERVE_ANY_OF_`) | `OF in <Name> needs a resource or a parameter on both sides` |
| B16 | `FILE` with fewer than two words (or `ANY`) before it in its resource (`READ_txt_FILE`, `READ_FILE`, `READ_FILE_ANY`) | `FILE in <Name> needs a name and an extension before it` |

Not changed, listed so nobody "fixes" them: `BY` and `AND` are interchangeable for the parser
(`READ_users_AND_id` → `/users/{id}/`); `READ_users_VERSION` has an empty version; WebSocket words are
lower-cased (`SUBSCRIBE_Chat_Message` → `chat.message`, `SUBSCRIBE_chatRoom` → `chatroom`) and `PathStyle`
does not apply to events (`_` is the segment separator there); the route conflict check and its message.

### Step 12: `PathStyle`

| # | Rule |
|---|---|
| B17a | `ModuleBuilder().PathStyle(style)` records the style and returns the builder. A value other than the two constants (`PathStyle(0)`, `PathStyle(9)`) panics at the call: `invalid module: PathStyle needs owlkei.PathStyleKebab or owlkei.PathStyleAsWritten`. Called twice on one builder, the last call wins |
| B17b | The style of a **controller** is the explicit style of the first module that set one, in this order: the module that lists the controller, then each module that imports it (directly or through other modules) in the order `Create()` discovers them (depth-first, `Imports` order: the order in which module prefixes are stacked). No module set one → `PathStyleKebab` |
| B17c | Root `PathStyle(PathStyleAsWritten)`, modules without a style: `READ_payment_intents` is `/payment_intents/` in the root's own controllers and in every imported module, static or dynamic (`Register(opts)` functions and `func() owlkei.Module` values) |
| B17d | An imported module with `PathStyle(PathStyleKebab)` under an as-written root keeps `/payment-intents/`, and so do the modules **it** imports that set none (nearest explicit setting wins) |
| B17e | Two parents: `A` (`PathStyleAsWritten`) and `B` (`PathStyleKebab`) both import `M` (no style), the root (no style) imports them. `Imports(A, B)` → `M`'s routes are as written; `Imports(B, A)` → kebab. `M`'s controller is still bound once, with both parents' prefixes as today |
| B17f | The style applies to resource words only: not to `Prefix` (B11), `Path` (B22), parameters (B3), versions (B4) or WebSocket events |
| B17g | Two apps in one process that build their modules separately do not share a style (per-app state, like prefixes: `TestTwoApps_PrefixNotDoubled`) |

### Step 13: exact path, `ALL`, parser bugs

| # | Rule |
|---|---|
| B18 | `SUBSCRIBE_chat_ALL` → event `chat.**`; `SUBSCRIBE_ALL` → `**`; `SUBSCRIBE_chat_ANY_ALL` → `chat.*.**`. A client message `chat.room1.typing` reaches `SUBSCRIBE_chat_ALL`; `chat` alone does not (`**` is one or more segments). Precedence among patterns is `internal/wsevent`'s, unchanged |
| B19 | `ALL` anywhere but last (`SUBSCRIBE_chat_ALL_x`, `SUBSCRIBE_ALL_chat`) panics `invalid route: ALL in <Name> must be the last word`. In an HTTP name `ALL` is refused by B9 |
| B20 | Bug 1, `ANY` after a parameter: it is a wildcard segment after the parameters of that resource, for every operation: `READ_files_BY_name_ANY` → `/files/{name}/*/`, `READ_files_BY_dir_AND_name_ANY` → `/files/{dir}/{name}/*/`, `READ_files_BY_name_ANY_OF_x` → `/x/files/{name}/*/`, `READ_BY_name_ANY` → `/{name}/*/`. `GET /files/a/b/c` reaches the first with `name` = `a` |
| B21 | Bug 2, `__` in a parameter name: B3 (`READ_users_BY_user__id` → `/users/{user__id}/`, read with `param.Get("user__id")`). Bug 3, found while checking: `FILE` after `ANY` keeps the text before the wildcard: `READ_img_ANY_png_FILE` → `/img*.png/` (today `/*.png/`). Bug 4: a resource without words leaves no empty segment: `READ_BY_id_OF_users` → `/users/{id}/` (today `/users//{id}/`) |
| B22 | `instance.Path(v, handlers...)` in `NewController()`: for each handler, `v` (enclosed in `/`) replaces the path its name spells. Then the controller prefixes and the module prefixes apply as for any route. The method comes from the operation word and the version from `VERSION`. `v` is never kebab-cased; it may hold `{name}` parameters and `*`, checked by the router's existing `invalid route:` rules. `Path("/", h)` is the controller's root |
| B23 | The name of a handler with an exact path is still parsed with every rule above (B9, B13 to B16 apply) |
| B24 | `Path` panics, all `invalid route:` — no handler: `Path("<v>") needs at least one handler`; a handler that is not an HTTP route of the controller (a helper, a `SUBSCRIBE_` method, a non-function): `Path("<v>") is bound to '<name>', which is not a handler of the controller`; a second `Path` for the same handler: `<Name> has two exact paths: "<v1>" and "<v2>"`; `v` holding `?` or `#`: `Path("<v>") holds '<char>': a path has no query or fragment` |
| B25 | Two handlers with one exact path and different operations (`Path("/token", instance.READ_token, instance.CREATE_token)`) are two routes. The same operation twice is the existing `route conflict:` |
| B26 | Layers bound to a handler (`BindGuard(g, instance.CREATE_token)`) and targeted prefixes (`Prefix("v1", instance.CREATE_token)`) work unchanged with an exact path |

### Current → expected

"Today" is the output of `ParseFuncNameToURL` / `ParseWSFuncNameToEvent` at `9fe673b`, produced by a
throwaway test (deleted); "Kebab" and "As written" are the outputs after this design. `=` means "same as
today". `panic B9` stands for the rule's message.

| Name | Today | Kebab | As written | Rule |
|---|---|---|---|---|
| `READ` | `GET /` | = | = | |
| `READ_users` | `/users/` | = | = | |
| `READ_user_profile` | `/user_profile/` | `/user-profile/` | = | B1 |
| `READ_payment_intents` | `/payment_intents/` | `/payment-intents/` | = | B1 |
| `READ_userProfile` | `/userProfile/` | = | = | B2 |
| `READ_user__profile` | `/user_profile/` | `/user-profile/` | = | B8 |
| `READ__users`, `READ_users_` | `/users/` | = | = | B8 |
| `READ_profile_OF_users` | `/users/profile/` | = | = | B1 |
| `READ_user_profile_OF_big_orgs` | `/big_orgs/user_profile/` | `/big-orgs/user-profile/` | = | B1 |
| `READ_users_BY_user_id` | `/users/{user_id}/` | = | = | B3 |
| `READ_users_BY_user__id` | `/users/{user_id}/` | `/users/{user__id}/` | `/users/{user__id}/` | B3, B21 |
| `READ_users_BY__id` | `/users/{id}/` | `/users/{_id}/` | `/users/{_id}/` | B3 |
| `READ_users_BY_id__OF_big_orgs` | `/big_orgs/users/{id}/` | `/big-orgs/users/{id_}/` | `/big_orgs/users/{id_}/` | B1, B3 |
| `READ_BY_ID_AND_NAME` | `/{ID}/{NAME}/` | = | = | B3 |
| `READ_posts_BY_post_id_OF_users_BY_user_id` | `/users/{user_id}/posts/{post_id}/` | = | = | |
| `MODIFY_dm_events_OF_with_BY_participant_id_OF_dm_conversations_VERSION_V2` | `PATCH /dm_conversations/with/{participant_id}/dm_events/`, `V2` | `/dm-conversations/with/{participant_id}/dm-events/`, `V2` | = | B1, B4 |
| `READ_members_BY_user_name_AND_member_id_OF_club_users_BY_id` | `/club_users/{id}/members/{user_name}/{member_id}/` | `/club-users/{id}/members/{user_name}/{member_id}/` | = | B1, B3 |
| `READ_users_VERSION_2` | `/users/`, `2` | = | = | B4 |
| `READ_user_profile_VERSION_v1_v1` | `/user_profile/`, `v1_v1` | `/user-profile/`, `v1_v1` | = | B4 |
| `READ_users_VERSION_V_12` | `/users/`, `V_12` | = | = | B4 |
| `READ_users_VERSION` | `/users/`, `""` | = | = | |
| `READ_orders_2024` | `/orders_2024/` | `/orders-2024/` | = | B2 |
| `READ_404` | `/404/` | = | = | B2 |
| `READ_v2_users` | `/v2_users/` | `/v2-users/` | = | B2 |
| `READ_2fa_codes` | `/2fa_codes/` | `/2fa-codes/` | = | B2 |
| `READ_Users` | `/Users/` | = | = | B2 |
| `READ_orgsFB`, `READ_apiV2` | `/orgsFB/`, `/apiV2/` | = | = | B9 |
| `READ_é_cafés` | `/é_cafés/` | `/é-cafés/` | = | B2 |
| `READ_ANY` | `/*/` | = | = | |
| `READ_assets_ANY` | `/assets*/` | = | = | B5 |
| `READ_a_b_ANY` | `/a_b*/` | `/a-b*/` | = | B5 |
| `READ_me_ANY_bers` | `/me*bers/` | = | = | B5 |
| `READ_big_me_ANY_bers_x` | `/big_me*bers_x/` | `/big-me*bers-x/` | = | B5 |
| `SERVE_ANY` | `SERVE /*/` | = | = | |
| `SERVE_static_ANY`, `SERVE_ANY_OF_static` | `/static/*/` | = | = | B6 |
| `SERVE_a_b_ANY` | `/a_b/*/` | `/a-b/*/` | = | B6 |
| `SERVE_static_files_ANY`, `SERVE_ANY_OF_static_files` | `/static_files/*/` | `/static-files/*/` | = | B6 |
| `SERVE_assets_ANY_OF_public` | `/public/assets/*/` | = | = | |
| `SERVE_me_ANY_bers` | `/me*bers/` | = | = | B5 |
| `READ_robots_txt_FILE`, `READ_logo_png_FILE` | `/robots.txt/`, `/logo.png/` | = | = | B7 |
| `READ_site_map_xml_FILE` | `/site_map.xml/` | `/site-map.xml/` | = | B7 |
| `READ_my_logo_png_FILE_OF_static_files` | `/static_files/my_logo.png/` | `/static-files/my-logo.png/` | = | B7 |
| `READ_a_b_FILE_c` | `/a.b_c/` | `/a.b-c/` | = | B7 |
| `READ_ANY_html_FILE` | `/*.html/` | = | = | B7 |
| `READ_ANY_html_FILE_OF_members_OF_ANY_jpeg_FILE_VERSION_112____3` | `/*.jpeg/members/*.html/`, `112_3` | = | = | B4, B7 |
| `READ_img_ANY_png_FILE` | `/*.png/` | `/img*.png/` | `/img*.png/` | B21 |
| `READ_ANY_HTML_FILE`, `READ_logo_PNG_FILE` | `/*.html/`, `/logo.png/` | panic B9 (`HTML`, `PNG`) | panic B9 | B9, D7 |
| `READ_orgs_FB`, `READ_FB` | `/orgs_FB/`, `/FB/` | panic B9 (`FB`) | panic B9 | B9 |
| `READ_orgs_V2`, `READ_plan_B`, `READ_URL_shortener`, `READ_ÉCOLE` | `/orgs_V2/`, `/plan_B/`, `/URL_shortener/`, `/ÉCOLE/` | panic B9 | panic B9 | B9 |
| `READ_users_CREATE_x` | `/users_CREATE_x/` | panic B9 (`CREATE`) | panic B9 | B9, D8 |
| `READ_ALL` | `/ALL/` | panic B9 (`ALL`) | panic B9 | B9 |
| `READ_users_BY`, `READ_users_BY_OF_x` | `/users/{}/`, `/x/users/{}/` | panic B13 (`BY`) | panic B13 | B13 |
| `READ_users_BY_id_AND` | `/users/{id}/{}/` | panic B13 (`AND`) | panic B13 | B13 |
| `READ_files_BY_name_ANY_x` | `/files/{name}/` | panic B14 (`x`) | panic B14 | B14 |
| `READ_users_BY_id_FILE` | `/users/{id}/` | panic B14 (`FILE`) | panic B14 | B14 |
| `READ_OF_users`, `READ_a_OF_OF_b` | `/users//`, `/b//a/` | panic B15 | panic B15 | B15 |
| `READ_users_OF` | `/users/` | panic B15 | panic B15 | B15 |
| `READ_txt_FILE`, `READ_FILE` | `/txt/`, `/` | panic B16 | panic B16 | B16 |
| `READ_files_BY_name_ANY` | `/*/files/{name}/` | `/files/{name}/*/` | `/files/{name}/*/` | B20 |
| `READ_files_BY_dir_AND_name_ANY` | `/*/files/{dir}/{name}/` | `/files/{dir}/{name}/*/` | same | B20 |
| `READ_files_BY_name_ANY_OF_x` | `/x/*/files/{name}/` | `/x/files/{name}/*/` | same | B20 |
| `SERVE_files_BY_name_ANY` | `SERVE /*/files/{name}/` | `/files/{name}/*/` | same | B20 |
| `READ_BY_name_ANY` | `/*//{name}/` | `/{name}/*/` | same | B20 |
| `READ_BY_id_OF_users` | `/users//{id}/` | `/users/{id}/` | same | B21 |
| `RENDER`, `RENDER_bookings`, `SUBMIT_new_OF_bookings` | not a route | design 0018 amendment 2 | | |
| `SUBSCRIBE_chat_ALL` | `chat.all` | `chat.**` | (no style) | B18 |
| `SUBSCRIBE_ALL` | `all` | `**` | | B18 |
| `SUBSCRIBE_chat_ANY_ALL` | `chat.*.all` | `chat.*.**` | | B18 |
| `SUBSCRIBE_chat_ALL_x` | `chat.all.x` | panic B19 | | B19 |
| `SUBSCRIBE_chat_ANY` | `chat.*` | = | | |
| `SUBSCRIBE_chat_PERSON_ANY` | `chat.person.*` | panic B9 (`PERSON`) | | B9 |
| `SUBSCRIBE_chat_V2` | `chat.v2` | panic B9 (`V2`) | | B9 |
| `SUBSCRIBE_Chat_Message`, `SUBSCRIBE_chatRoom` | `chat.message`, `chatroom` | = | | |

## Performance

| Path | Kind | Cost |
|---|---|---|
| Parsing names, the style walk, `Path` | cold (`Create()`) | One pass over each method name; the style is one map write per controller in the loop that already stacks prefixes. Readability first: no fast path |
| A request | hot | **Unchanged.** Routes reach the trie as strings, as today; the style, the exact path and the checks leave nothing on the request path. No new branch, no allocation |
| Idle memory | | One `PathStyle` per module and per controller during `Create()`; nothing kept per route |

No heavy technique. `BenchmarkParseFuncNameToURL` (`internal/common/fn_bench_test.go`) keeps running with
the new signature; a regression there is not a gate (cold path).

## Concurrency

No goroutine and no shared mutable state. The style of a controller is written during `Create()` (single
goroutine) in the container, per app, and read only there. `ModuleBuilder.PathStyle` writes the builder
before `Build()`.

## Security

| Risk | Decision |
|---|---|
| A route served at another URL than the developer reads from the name | B9 and B13 to B16 stop `Create()`; nothing is dropped or guessed |
| Kebab-case changes every multi-word URL | Free (unpublished). Rules keyed on the old paths outside the app (proxy allowlists, WAF rules, `throttler` keys built from the path, dashboards) must follow: recorded in the backlog |
| `Path` text | Written by the developer, never from a request. `?` and `#` are refused (B24); the router's own checks (`$`, partial parameter segments, 64 segments) apply |
| Two spellings for one URL (`Path("/users")` and `READ_users`) | The existing `route conflict:` check, which runs on final patterns |
| `ALL` widens a WebSocket handler to every deeper topic | By design; the topic validation of the backlog's "Glob topic syntax in the WebSocket layer" still belongs to the WebSocket roadmap |

## Impact

| Area | Change |
|---|---|
| Existing apps | Every multi-word resource moves from `_` to `-` unless the root module sets `PathStyleAsWritten`. Names of the "panic" rows stop `Create()` |
| `internal/common` | `fn.go`: the parser rewritten around the grammar above (one loop per resource instead of string surgery on `_`), `TokenAll`; `http.go`: `PathStyle`, `Path`, `AddHandlerToRouterMap(…, style, …)` |
| `internal/core` | `module_builder.go` / `module.go`: the builder's style copied to the module; `container.go`: `styleByController` filled in `discover` next to `prefixesByController`, passed to `AddHandlerToRouterMap`; `checkViewResults` and `wsHandlerTargets` call the parser with any style |
| Root | `aliases.go`: `PathStyle` and its two constants |

### Order of implementation

| Step | Work | Red tests first |
|---|---|---|
| 11 | Parser: B1 to B16 with a `style` parameter (every caller passes `PathStyleKebab`); update the parser tests that pin `_` paths | `TestParseFuncNameToURL_*` of the test plan |
| 12 | `PathStyle` type, constants, alias, `ModuleBuilder.PathStyle`, the style walk | `TestPathStyle_*`, `TestSurface_PathStyle` |
| 13 | `Path` (B22 to B26), `ALL` (B18, B19), B20, B21 | `TestPath_*`, `TestParseWSFnNameToEvent_All*`, `TestParseFuncNameToURL_AnyAfterParam`, `TestWS_AllHandlesDeepTopic` |
| 18 | Migration list below | `make check` |

### Migration list (step 18; grep at `9fe673b`)

No handler of `sample/shop`, `sample/pages`, `sample/main.go` or `bench/owlkei.go` has a multi-word
resource, so their URLs do not move. What changes:

| File | What |
|---|---|
| `sample/benchmarks/controller.go` | `SUBSCRIBE_chat_PERSON_ANY` → `SUBSCRIBE_chat_person_ANY` (same event `chat.person.*`) |
| `internal/common/http_test.go` | Expected routes of `MODIFY_dm_events_OF_with_BY_participant_id_OF_dm_conversations_VERSION_V2`, `READ_members_BY_user_name_AND_member_id_OF_club_users_BY_id`, `CREATE_owned_lists_OF_users_BY_id_VERSION_`, `READ_users_all` become kebab; `READ_ANY_HTML_FILE_OF_members_OF_ANY_JPEG_FILE_VERSION_112____3` and `DELETE_image_PNG_FILE_OF_members_OF_users_BY_id_VERSION_NEUTRAL__` write the extension in lower case; all 14 parser calls gain the style |
| `internal/common/fn_test.go` | `READ_users_CREATE_x` now panics (B9); 9 parser calls gain the style |
| `internal/common/fn_serve_test.go` | `READ_a_b_ANY` → `/a-b*/`, `SERVE_a_b_ANY` → `/a-b/*/`; 6 calls |
| `internal/common/fn_bench_test.go`, `ws_bench_test.go` | The new signature; the `…_ALL` names now give `**` |
| `internal/core/create_conflicts_test.go` | `READ_cfs_all` is `/cfs-all/` (the shapes it compares stay distinct) |
| `internal/core/container.go`, `internal/common/fn.go`, `http.go` | The three callers of `ParseFuncNameToURL` (`checkViewResults`, `AddHandlerToRouterMap`, `wsHandlerTargets`) and the one caller of `AddHandlerToRouterMap` (`bindHTTPController`) |
| `README.md` › "Routes from method names" | "written as is" → the kebab rule; the row `READ_user_profile` is `GET /user-profile`; add `PathStyle`, `Path`, the upper-case rule and B13 to B16; `FILE` row; the module section gains `PathStyle`; the package reference tables gain the three names |
| `README.md` › WebSocket | `ALL` |
| `internal/common/README.md` › Route DSL | Tokens table (`ALL`, `TokenAll`), both parser sections (signature, every rule above; the rows `READ_users_CREATE_x`, `READ_ANY_HTML_FILE…`, `SERVE_a_b_ANY`, `MODIFY_dm_events…`), `Path`, `PathStyle`, `AddHandlerToRouterMap` |
| `internal/core/README.md` | `ModuleBuilder.PathStyle`; the `SERVE_ANY_OF_` mention; the panic list |
| `.owlkei/routing-system.md` | The examples `CREATE_COMMENT`, `CREATE_OF_COMMENTS`, `CREATE_OF_USERS_VERSION_3`, `DELETE_BY_ID_AND_VERSION`, `READ_ANY_FILE_HTML`, `READ_BY_USER_ID_OF_FRIENDS_BY_FRIEND_ID`, `READ_FILE`, `READ_FILE_ANY`, `READ_OF_COMMENTS`, `READ_OF_POSTS` are refused now: rewrite the page on the new rules |
| `.owlkei/metadata-api-index.json`, `INDEX.md`, `ai-guidance-quick-reference.md`, `websocket-system.md`, `anti-patterns-gotchas.md`, `package-reference.md`, `conventions.md` | `CREATE_OF_COMMENTS`, `SUBSCRIBE_chat_PERSON_ANY`; `ALL`; `PathStyle`, `Path`; the new `invalid route:` panics |
| `CLAUDE.md` | "REST Route Convention" (kebab, `PathStyle`, `Path`, the upper-case rule), the WebSocket example, the `invalid route:` and `invalid module:` lines of the panic list, the naming line (`owlkei.PathStyleKebab`) |
| `docs/roadmap/backlog.md` | "Glob topic syntax": `ALL` is done |
| `api/owlkei.txt` | `make api` (the six lines above) |
| Older designs (`0005`, `0007`, `0009`, `0014`, `0017`) | Not edited: they describe the code of their time |

## Test plan

Files: `internal/common/fn_route_test.go` (parser tables), `internal/common/ws_test.go` (existing file,
new cases), `internal/common/http_path_test.go`, `internal/core/create_pathstyle_test.go`,
`internal/core/create_path_test.go`, `internal/core/ws_all_test.go`, root `surface_pathstyle_test.go`.
Parser tests call `ParseFuncNameToURL(name, style)` and compare the three values; panic tests recover
and match the text. Core tests build modules, call `Create` and drive `app.ServeHTTP`.

| Rule | Test | Cases |
|---|---|---|
| B1, B2 | `TestParseFuncNameToURL_Kebab`, `TestParseFuncNameToURL_AsWritten` | every "Kebab" and "As written" cell of the table that is not a panic, both styles |
| B3, B21 | `TestParseFuncNameToURL_ParamNameVerbatim` | `user_id`, `user__id`, `_id`, `id_` before `OF`, `ID`; both styles |
| B4 | `TestParseFuncNameToURL_VersionUntouched` | `v1_v1`, `V2`, `112____3`, empty |
| B5, B6 | `TestParseFuncNameToURL_AnyJoints`, `TestParseFuncNameToURL_ServeAnyKebab` | glue, own segment, `OF` form equals name form |
| B7, B21 | `TestParseFuncNameToURL_FileKebab`, `TestParseFuncNameToURL_FileAfterAnyKeepsPrefix` | `site_map_xml`, `a_b_FILE_c`, `ANY_html`, `img_ANY_png` |
| B8 | `TestParseFuncNameToURL_EmptyWordsDropped` | `__`, leading, trailing |
| B9, B10 | `TestParseFuncNameToURL_UpperCaseWordPanics`, `TestParseFuncNameToURL_UpperCaseAllowed`, `TestParseWSFnNameToEvent_UpperCaseWordPanics`, `TestCreate_UpperCaseRoutePanics` | each refused and allowed name of B9; the exact message for `READ_orgs_FB`; a non-route method `Helper_FB` on a controller does not panic; `Create()` panics for a controller with `READ_orgs_FB` |
| B11 | `TestPathStyle_PrefixNotRewritten` | module and controller prefix with `_` under kebab, with `-` under as written |
| B12 | `TestPathStyle_OldSpellingIs404` | `/user_profile` → 404 JSON |
| B13 to B16 | `TestParseFuncNameToURL_MalformedPanics` | every name of the four rows, exact messages |
| B17a | `TestPathStyle_InvalidValuePanics`, `TestPathStyle_LastCallWins` | `PathStyle(0)`, `PathStyle(9)` |
| B17b to B17d | `TestPathStyle_DefaultKebab`, `TestPathStyle_RootAsWrittenReachesImports`, `TestPathStyle_ExplicitKebabWins`, `TestPathStyle_NearestSettingWins` | static module, `func() owlkei.Module`, a three-level chain |
| B17e | `TestPathStyle_TwoParentsFirstDiscoveredWins` | `Imports(A, B)` and `Imports(B, A)`; the route is registered once |
| B17f | `TestPathStyle_OnlyResourceWords` | a parameter `user_id`, a version `v1_v1`, a WebSocket event under as written |
| B17g | `TestTwoApps_PathStyleNotShared` | the same module values in two apps |
| B18, B19 | `TestParseWSFnNameToEvent_All`, `TestParseWSFnNameToEvent_AllNotLastPanics`, `TestWS_AllHandlesDeepTopic` | `chat.**`, `**`, `chat.*.**`; a client sends `chat.room1.typing` and `chat` |
| B20 | `TestParseFuncNameToURL_AnyAfterParam`, `TestHTTP_AnyAfterParamServes` | the five names; `GET /files/a/b/c` |
| B22, B23 | `TestPath_ReplacesNamePath`, `TestPath_KeepsPrefixesMethodVersion`, `TestPath_NotRewrittenByStyle`, `TestPath_NameStillChecked` | `/oauth2/token`; controller and module prefix; `VERSION_2`; `Path("/a_b")` under kebab; a parameter `{id}` read with `param.Get("id")`; `READ_orgs_FB` with a `Path` still panics |
| B24 | `TestPath_Panics` | each of the four messages |
| B25, B26 | `TestPath_TwoOperationsOnePath`, `TestPath_LayersAndTargetedPrefix` | GET and POST; a guard bound to the handler runs |
| API | `TestSurface_PathStyle`, `make api-check` | the root names compile in an application; the six lines |

## Open questions

None blocks the run. Decided here under the delegation; each can be reversed without moving a public
name except 3:

| # | Decided | Alternative |
|---|---|---|
| 1 | D10: malformed names panic (B13 to B16) | Keep today's leniency: delete the four rules; the "Today" outputs stay |
| 2 | D7: an upper-case extension before `FILE` is refused | Exempt the word before `FILE` from B9 |
| 3 | D6: the method is named `Path` and is variadic (`Path(v, handlers...)`) | `Route`, or exactly one handler per call |
| 4 | B22: prefixes still apply to an exact path | An exact path that ignores every prefix (then a reusable module cannot be mounted under one) |
| 5 | B9 counts `V2` and a single letter (`plan_B`) as upper case | Refuse only words of two or more letters and no digit |
| 6 | D5 / B17e: with two parents the first-discovered explicit style wins silently, as `Prefix` stacks silently | Panic when two importers disagree. Note for a later look: stacking both parents' prefixes (`/b/a/m/…`) is itself surprising; this design copies it, it does not bless it |
| 7 | WebSocket words stay lower-cased and `PathStyle` does not touch events | The WebSocket roadmap owns event naming |
