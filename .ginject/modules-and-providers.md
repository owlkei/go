# Modules & Providers System

**Optimization**: Initialization order diagrams, state machines, decision trees for composition.

## 1. Module System Basics

The user-facing reference is `core/README.md` › Modules and lifecycle (builder, fields, `Prefix`,
hooks, rules from the tests). This page explains how the container resolves things.

### 1.1 Module Purpose

**Module** = composition root:
- Declare controllers (HTTP/WS handlers) and providers (services)
- Declare imports (other modules, or module funcs)
- Lifecycle hooks: the `OnInit`, `OnReady`, `OnShutdown` fields
- Visibility: `IsGlobal` makes its providers visible to every module

### 1.2 Module Structure (`core/module.go`)

```go
type Module struct {
    id       string
    Name     string
    prefixes []string

    imports        []any
    staticModules  []*Module
    dynamicModules []any
    providers      []Provider
    controllers    []Controller

    IsGlobal   bool
    OnInit     func()
    OnReady    func()
    OnShutdown func()
}

func (m *Module) Prefix(prefix string) *Module
func (m *Module) ID() string
```

`Create` never writes a `*Module`: the wiring results (routes, layers, provider instances) live in
the app's container (`core/container.go`), so one module value can be given to any number of apps.

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
│   └── ConfigModule          a module func or Register(...) result; IsGlobal = true
├── Controllers: AppController
└── Providers: AppConfig
```

**Traversal**: `discover` walks the tree depth first at `Create()`, each module once per app.

---

## 2. Creating Modules

### 2.1 Static Module

```go
var UserModule = core.ModuleBuilder().
	Controllers(UserController{}).
	Providers(UserService{}, UserRepository{}).
	Build()

var AppModule = core.ModuleBuilder().
	Imports(UserModule).
	Build()
```

`Build()` resolves `Name` from the variable (`main.UserModule`), used in the `InstanceLoader` log line.

### 2.2 Module Funcs

`Imports` also accepts a function whose type starts with `func(` and returns `*core.Module`. The app
calls it once (per app, whatever the number of importers) while discovering the tree, **before** it
discovers the importing module's other imports. Its arguments resolve from the global providers known
at that point (the root module's providers, and those of `IsGlobal` modules discovered earlier), then
`common.Logger` and `common.Publisher`; a provider used this way is constructed then. An unresolvable
argument panics `dependency injection: can't resolve argument of '<func>' …`: an `IsGlobal` module
imported next to the module func is not visible to it yet.

```go
type ReportService struct{}

func (instance ReportService) NewProvider() core.Provider { return instance }

func ReportModule(logger common.Logger) *core.Module {
	logger.Info("ReportModule")
	return core.ModuleBuilder().Providers(ReportService{}).Build()
}

var AppModule = core.ModuleBuilder().
	Imports(UserModule, ReportModule).
	Build()
```

A module func may be called by two apps at once: it must not write shared state without its own
protection.

### 2.3 Register Functions (built-in modules)

Every built-in module is a package-level `Register(opts *<X>ModuleOptions) *core.Module` that returns
a configured module (`config.Register`, `cache.Register`). A backend it creates is shared by every app
that imports the returned module value.

---

## 3. Module Builder API

```go
func ModuleBuilder() *moduleBuilder
func (m *moduleBuilder) Imports(modules ...any) *moduleBuilder
func (m *moduleBuilder) Providers(providers ...Provider) *moduleBuilder
func (m *moduleBuilder) Controllers(controllers ...Controller) *moduleBuilder
func (m *moduleBuilder) Build() *Module
```

- Calls chain and accumulate: `Providers(P1{}).Providers(P2{})` lists both
- An import that is neither a `*Module` nor a module func panics `invalid module: can't pass '<type>' type as module` at `Build()`
- `Prefix("v1")` on the built module prefixes its routes and its imports' routes; an importer's prefix goes in front (`/api/v1/ping`)

---

## 4. Provider System

### 4.1 Provider Interface

```go
type Provider interface {
    NewProvider() Provider
}
```

### 4.2 Provider Lifecycle (`core/container.go`)

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

func (instance DatabasePool) NewProvider() core.Provider {
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

For each exported field (`injectDependencies`, `core/fn.go`):
1. a provider of the module (own or imported)
2. a global provider
3. an interface the container holds (`common.Logger`, `common.Publisher`)
4. otherwise: a field whose type implements `Provider` panics `dependency injection: can't resolve dependency …`; any other field keeps the value it was given

An unexported field panics `dependency injection: can't set value to unexported '<field>' field of the <type> <kind>`.

### 5.3 Provider Injection in Controllers

```go
type UserController struct {
	common.HTTP
	UserService UserService
}

func (instance UserController) NewController() core.Controller {
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
var InfraModule = func() *core.Module {
	m := core.ModuleBuilder().Providers(DatabasePool{}, CacheService{}).Build()
	m.IsGlobal = true
	return m
}()
```

- The providers of the root module and of every `IsGlobal` module (at any import depth) are visible to every module
- A global module still has to be in the tree (imported somewhere) to be discovered
- Module funcs' arguments resolve only from the global providers discovered before the call (the root's, and earlier `IsGlobal` modules'), then `common.Logger` and `common.Publisher`

---

## 7. Module Initialization Order

The `Create()` sequence is in [startup-lifecycle-analysis.md](startup-lifecycle-analysis.md) § Phase 2.
Modules are initialized depth first: a module after every module it imports, imports in `Imports(…)`
order, each once per app. `OnReady` follows the same order at the end of `Create`; `OnShutdown` runs in
the exact reverse.

---

## 8. Module Best Practices

### 8.1 DO

- ✓ Keep modules focused (single responsibility)
- ✓ Use imports to declare dependencies; `IsGlobal` only for truly global services
- ✓ Open resources in `NewProvider()` or `OnInit`, close them in `OnShutdown`
- ✓ Keep shared mutable state behind a pointer, protected by a mutex or atomics

### 8.2 DON'T

- ✗ Change a module (`Prefix`, `IsGlobal`, a hook) after giving it to `Create`
- ✗ Call `NewProvider()` yourself (the instance would have no injected fields)
- ✗ Write shared state from a module func without protection (two apps may call it at once)

---

## 9. Common Module Patterns

### 9.1 Dependency Hierarchy

```go
var InfraModule = func() *core.Module {
	m := core.ModuleBuilder().Providers(DatabasePool{}).Build()
	m.IsGlobal = true
	return m
}()

var UserModule = core.ModuleBuilder().
	Controllers(UserController{}).
	Providers(UserService{}, UserRepository{}).
	Build()

var AppModule = core.ModuleBuilder().
	Imports(InfraModule, UserModule).
	Build()
```

`InfraModule` is initialized first, so `DatabasePool` exists before `UserRepository` asks for it (and
`provide` would construct it first anyway).
