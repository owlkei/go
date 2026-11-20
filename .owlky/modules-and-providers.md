# Modules & Providers System

**Optimization**: Initialization order diagrams, state machines, decision trees for composition.

## 1. Module System Basics

The user-facing guide is `README.md` › Module lifecycle (kinds, factory shapes, `AsyncImports`, states,
hooks, `Retry`, with a timeline). The rules from the tests are in `internal/core/README.md` › Modules and
lifecycle (builder, fields, Module factories, AsyncImports and module states, Retry). Design 0020. This
page explains how the container resolves things.

### 1.1 Module Purpose

**Module** = composition root:
- Declare controllers (HTTP/WS handlers) and providers (services)
- Declare imports: `Imports(...)` (fail fast) and `AsyncImports(...)` (built in the background), each
  entry a Module or a module factory
- Lifecycle hooks: the `OnInit`, `OnReady`, `OnShutdown` fields
- Visibility: `IsGlobal` makes its providers visible to every module

### 1.2 Module Structure (`internal/core/module.go`)

```go
type Module struct {
    id       string
    Name     string
    prefixes []string

    pathStyle common.PathStyle

    imports         []any
    asyncImports    []any
    staticModules   []*Module
    moduleFactories []any
    providers       []Provider
    controllers     []Controller

    IsGlobal   bool
    OnInit     func()
    OnReady    func()
    OnShutdown func()
}

func (m *Module) Prefix(prefix string) *Module
func (m *Module) ID() string
```

`Create` never writes a `*Module`: the wiring results (routes, layers, provider instances) live in
the app's container (`internal/core/container.go`), so one module value can be given to any number of apps.

**No `exports` field, `Export(...)` method or `IsGlobal(...)` builder method exists.** There is no
export mechanism: a module sees its own providers, those of the modules it imports (at any depth),
and the global ones. `IsGlobal`, `OnInit`, `OnReady`, `OnShutdown` are set on the built `*Module`
(`m.IsGlobal = true`).

### 1.3 Module Tree

```
RootModule                    its providers are global
├── Imports:
│   ├── AuthModule            Providers: JWTService, AuthService
│   ├── UserModule            Controllers: UserController; Providers: UserService, UserRepository
│   └── ConfigModule          a Register(...) result (a Module); IsGlobal = true
├── Controllers: AppController
└── Providers: AppConfig
```

**Traversal**: `discover` walks the tree depth first at `Create()`, each module once per app.

---

## 2. Creating Modules

### 2.1 Static Module

```go
var UserModule = owlky.ModuleBuilder().
	Controllers(UserController{}).
	Providers(UserService{}, UserRepository{}).
	Build()

var AppModule = owlky.ModuleBuilder().
	Imports(UserModule).
	Build()
```

`Build()` resolves `Name` from the variable (`main.UserModule`), used in the `InstanceLoader` log line.

### 2.2 Module Factories

Two kinds of entries exist: a Module and a module factory. Global is a property (`IsGlobal`), async is how
an entry is imported, `Register` is only a function name. A factory has one of three shapes, checked by
`reflect.Type` at `Build()` (`internal/core/module_builder.go`, `checkImports`):

```go
func(deps...) owlky.Module
func(deps...) (owlky.Module, error)
func(ctx context.Context, deps...) (owlky.Module, error)
```

**Scope** (`internal/core/module_factory.go`): a factory listed by module `P` resolves each argument from
the providers visible in the other entries of `P`'s `Imports` / `AsyncImports` (siblings), then the
global providers (the root's, every `IsGlobal` module's at any depth), then `log.Logger` /
`common.Publisher`. `P`'s own providers are not in scope.

**Order**: `Create()` scans the static tree first (`scanGlobals`), then repeats: call, in discovery order,
the first factory not yet called whose arguments all resolve, and scan what it returns. So a factory may
come before the sibling it needs, or need what another factory returns. One factory left unresolved
panics `dependency injection: can't resolve argument of '<func>' …`; two or more panic
`dependency injection: module factories can't be ordered: '<a>' needs '<T>' → …`. A provider used as an
argument is constructed then, after the `OnInit` of its module (that hook then does not run again).

**Startup timeout**: every factory of the sync set runs on a helper goroutine; `Create()` waits for its
return, its panic or the deadline of `AppOptions.StartupTimeout` (default 30 s, one budget from the start
of `Create()`, also the deadline of the `ctx` it passes, canceled when `Create()` returns). An error, a
panic, a nil module or the deadline panics `invalid module: module factory '<label>' …`.

```go
type ReportService struct{}

func (instance ReportService) NewProvider() owlky.Provider { return instance }

func ReportModule(ctx context.Context, logger log.Logger) (owlky.Module, error) {
	logger.Info("ReportModule")
	return owlky.ModuleBuilder().Providers(ReportService{}).Build(), nil
}

var AppModule = owlky.ModuleBuilder().
	Imports(UserModule, ReportModule).
	Build()
```

A factory is called once per app (keyed by its code pointer: two closures of one literal are one
factory). It may be called by two apps at once: it must not write shared state without its own
protection.

### 2.3 Register Functions (built-in modules)

Every built-in module is a package-level `Register(opts *<X>ModuleOptions) *core.Module` that returns
a configured module (`config.Register`, `cache.Register`). It is a Module, not a factory: the caller runs
it, before `Create()`. A backend it creates is shared by every app that imports the returned module
value. A backend that does I/O connects in a user factory that returns `cache.Register(...)`.

### 2.4 AsyncImports (`internal/core/module_async.go`, `module_state.go`)

`AsyncImports(entries...)` takes the same entries as `Imports`. The **sync set** is the root plus every
module reachable through `Imports` only; each `AsyncImports` entry found in it, with its **unit** (the
modules only it brings in), is built on its own goroutine after `Create()`'s static wiring.

```
state per module: pending ──▶ ready
                     └──────▶ failed        (one way; ready never goes back)

M needs entry E  ⇔  a provider owned by E's unit (or by a module that needs E) is the type of a field
                    of M's providers, controllers, bound layers or pipes. Importing alone is not a need.
M ready  ⇔ every entry it needs is ready;  M failed ⇔ one of them failed.
```

- At `Create()`: modules that are not ready get no `OnInit`, providers or controllers; their controllers
  are probed (`NewController` on a throwaway instance, unready providers zero) so routes are checked and
  registered behind a closed `moduleGate` (one `atomic.Pointer`).
- Background per entry: wait for needed entries → factory (`ctx` canceled at shutdown, no deadline) →
  `OnInit`, providers, controllers per module of the unit → gate opens → `ready` → `OnReady`; then the
  pending dependents whose needs are all ready, the same way. Wiring is serialized (`wireMu`).
- A closed gate answers `503` (`Retry-After` from the next `Retry` attempt, else 5; none when failed),
  pages render `503.html`; WS replies `not ready: <topic>`.
- Refused: controllers in a module an async factory returns (the entry fails), one entry in both lists,
  a probed `NewController` that panics, app-wide layers injecting async providers, an `Imports` factory
  needing an async module, async modules that need each other.
- `owlky.Retry(ctx, opts, fn)` retries inside a factory; with the factory's `ctx` it reports attempts
  to the entry (`ModuleRetry`, devtool `attempts`, `Retry-After`).

---

## 3. Module Builder API

```go
func ModuleBuilder() *moduleBuilder
func (m *moduleBuilder) Imports(modules ...any) *moduleBuilder
func (m *moduleBuilder) AsyncImports(modules ...any) *moduleBuilder
func (m *moduleBuilder) Providers(providers ...Provider) *moduleBuilder
func (m *moduleBuilder) Controllers(controllers ...Controller) *moduleBuilder
func (m *moduleBuilder) Build() *Module
```

- Calls chain and accumulate: `Providers(P1{}).Providers(P2{})` lists both
- One entry in both `Imports` and `AsyncImports` panics `invalid module: '<label>' is in both Imports and AsyncImports`
- An import that is neither a `*Module` nor a module factory panics `invalid module: can't import '<type>' with Imports: an import is a owlky.Module or a module factory: func(deps...) owlky.Module, func(deps...) (owlky.Module, error) or func(ctx context.Context, deps...) (owlky.Module, error)` at `Build()` (one line per bad entry; `AsyncImports` entries name `AsyncImports`)
- `Prefix("v1")` on the built module prefixes its routes and its imports' routes; an importer's prefix goes in front (`/api/v1/ping`)

---

## 4. Provider System

### 4.1 Provider Interface

```go
type Provider interface {
    NewProvider() Provider
}
```

### 4.2 Provider Lifecycle (`internal/core/container.go`)

```
1. Declaration   ModuleBuilder().Providers(UserService{})
2. Discovery     discover(): visible keys per module; the first module that lists a type owns it
3. Construction  for each module in init order: OnInit(), then provide(key) for its providers:
                 the providers its exported fields name first, injectDependencies, NewProvider()
4. Injection     the NewProvider result, by value, into every field of that type
```

Key = package path + type (`genFieldKey`). Each provider type is constructed **once per app**,
however many modules list it. `app.Get(UserService{})` returns that instance after `Create`.

### 4.3 Provider with Initialization

```go
type DatabasePool struct {
	DB *sql.DB
}

func (instance DatabasePool) NewProvider() owlky.Provider {
	instance.DB, _ = sql.Open("postgres", "postgres://localhost/app")
	return instance
}
```

`NewProvider()` runs once per app, so it is the place to open a resource; close it in the module's
`OnShutdown`. Shared state belongs behind a pointer (the provider is copied into each field).

---

## 5. Dependency Injection Between Providers

### 5.1 Provider-to-Provider Injection

```go
type UserRepository struct {
	Pool DatabasePool
}

type UserService struct {
	Repository UserRepository
}
```

`provide(UserService)` sees the `UserRepository` field, constructs `UserRepository` first (which
constructs `DatabasePool`), then injects and calls `UserService.NewProvider()`.

### 5.2 Field Lookup Order

For each exported field (`injectDependencies`, `internal/core/fn.go`):
1. a provider of the module (own or imported)
2. a global provider
3. an interface the container holds (`log.Logger`, `common.Publisher`)
4. otherwise: a field whose type implements `Provider` panics `dependency injection: can't resolve dependency …`; any other field keeps the value it was given

An unexported field panics `dependency injection: can't set value to unexported '<field>' field of the <type> <kind>`.

### 5.3 Provider Injection in Controllers

```go
type UserController struct {
	owlky.HTTP
	UserService UserService
}

func (instance UserController) NewController() owlky.Controller {
	return instance
}
```

Fields are injected before `NewController()` is called; its result is the controller the routes use.

### 5.4 Circular Dependencies

Value providers can't form a cycle: a struct containing itself through another struct is an invalid
recursive type and doesn't compile. Move shared logic into a third provider.

---

## 6. Global Modules

```go
var InfraModule = func() owlky.Module {
	m := owlky.ModuleBuilder().Providers(DatabasePool{}, CacheService{}).Build()
	m.IsGlobal = true
	return m
}()
```

- The providers of the root module and of every `IsGlobal` module (at any import depth) are visible to every module
- A global module still has to be in the tree (imported somewhere) to be discovered
- Module factories' arguments resolve from their siblings' providers, then global providers (the root's and every `IsGlobal` module's, including those returned by factories called earlier), then `log.Logger` and `common.Publisher`

---

## 7. Module Initialization Order

The `Create()` sequence is in [startup-lifecycle-analysis.md](startup-lifecycle-analysis.md) § Phase 2.
Modules are initialized depth first: a module after every module it imports, imports in `Imports(…)`
order, each once per app. `OnReady` follows the same order at the end of `Create`; `OnShutdown` runs in
the exact reverse. Modules that are not ready at `Create()` run `OnInit` and `OnReady` in the background
when they are built, and `OnShutdown` only if they became ready, in the reverse of the ready order.

---

## 8. Module Best Practices

### 8.1 DO

- ✓ Keep modules focused (single responsibility)
- ✓ Use imports to declare dependencies; `IsGlobal` only for truly global services
- ✓ Open resources in a module factory (it can return an error and take a `ctx`), close them in `OnShutdown`
- ✓ Put a dependency that may be slow or down at start in `AsyncImports`, and retry transient errors
  with `owlky.Retry(ctx, …)`; return permanent errors (bad config) at once
- ✓ Keep shared mutable state behind a pointer, protected by a mutex or atomics

### 8.2 DON'T

- ✗ Change a module (`Prefix`, `IsGlobal`, a hook) after giving it to `Create`
- ✗ Call `NewProvider()` yourself (the instance would have no injected fields)
- ✗ Write shared state from a module factory without protection (two apps may call it at once)
- ✗ Retry in a goroutine of your own: return an error and the module is `failed` for good; return a
  module and connect later, and its routes open before the connection exists
- ✗ Use injected providers in `NewController` (gated controllers run it twice, the first time with zero
  providers)

---

## 9. Common Module Patterns

### 9.1 Dependency Hierarchy

```go
var InfraModule = func() owlky.Module {
	m := owlky.ModuleBuilder().Providers(DatabasePool{}).Build()
	m.IsGlobal = true
	return m
}()

var UserModule = owlky.ModuleBuilder().
	Controllers(UserController{}).
	Providers(UserService{}, UserRepository{}).
	Build()

var AppModule = owlky.ModuleBuilder().
	Imports(InfraModule, UserModule).
	Build()
```

`InfraModule` is initialized first, so `DatabasePool` exists before `UserRepository` asks for it (and
`provide` would construct it first anyway).
