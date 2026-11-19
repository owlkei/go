# API Check

*API check is an internal tool of the `Owlky Go` repository. It lists the public API of the module in `api/owlky.txt` and fails when the code no longer matches that list, so every change to the surface users import is visible in review.*

- [API Check](#api-check)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [Public packages](#public-packages)
  - [Line format](#line-format)
  - [Rules](#rules)
  - [Run time](#run-time)

## Key Features
- Standard library only: `go list -export -deps -json`, `go/importer` and `go/types`
- Reads compiler export data, so no package is type-checked from source
- Lists the members of internal types that public names reach, transitively
- Fails on any importable package outside the allow list

## Usage

```sh
make api        # go run ./internal/apicheck -write: writes api/owlky.txt
make api-check  # go run ./internal/apicheck: compares, prints the difference, exits 1 on any
```

`make check` and the CI `lint` job run `make api-check` right after `go vet`. After an intended API change, run `make api` and commit `api/owlky.txt` with the change.

A difference prints one line per added (`+`) or removed (`-`) line:

```
+ pkg owlky/log, func Foo()
- pkg owlky/log, func Info(string)
+ pkg owlky/log, func Info(string, int)
```

## Public packages

A package of the module is public when its path has no `internal`, `examples` or `bench` element. Every public package must be in the allow list, or the tool prints:

```
api: package <import path> is importable: move it under internal/ or add it to the allow list
```

The list holds the root (`.`), `exception`, `log`, and every package below `middlewares/`, `modules/` and `guards/`. Until the packages of the public API redesign have moved, it also holds `accesslog`, `aggregation`, `common`, `core`, `ctx`, `devtool`, `memorybroker`, `memorycache`, `pattern`, `routing`, `trace`, `versioning` and `wsevent`; each move removes one.

## Line format

The file is sorted, one line per name. A package is named by the last element of the module path plus its relative path (`owlky`, `owlky/log`). Types print with their package name, and parameter names are dropped.

| Line | Example |
|---|---|
| Constant | `pkg owlky/x, const Version untyped int = 2` |
| Variable | `pkg owlky/x, var Default *x.Options` |
| Function | `pkg owlky/log, func Info(string)` |
| Type | `pkg owlky, type HTTPContext = *ctx.HTTPContext` (alias), `type Options struct`, `type Format int` |
| Struct field | `pkg owlky/ctx, type HTTPContext struct, Request *http.Request embedded` |
| Interface method | `pkg owlky/x, type Logger interface, Info(string, ...any)` |
| Method | `pkg owlky/x, method (*x.T) Do() error` |
| Reached field | `reach ctx.Ctx, field Request *http.Request` |
| Reached method | `reach ctx.Ctx, method (*ctx.Ctx) Status(int) *ctx.Result` |

A `reach` line lists an exported field or method of a type of the module that a listed line mentions and that has no `pkg` lines of its own (a type of an internal package, or an unexported type of a public package). Reached types are expanded again, so the members of types they mention are listed too. Types of other modules and of the standard library are not expanded.

## Rules

- With the file up to date, the check exits 0 and prints nothing.
- The written file is sorted.
- An added function prints a `+` line, a removed one a `-` line, and a changed signature both.
- A renamed parameter or an edited comment is not a change.
- A new method on a reached internal type is a change, also when the type is reached through another reached type.
- A new exported function of an internal package, or a type no public line reaches, is not a change.
- A public package outside the allow list fails with the message above; a package below an allowed `<dir>/*` prefix passes.
- Packages under `internal/`, `examples/` and `bench/` are not public.

## Run time

`make api-check` takes about 0.7 s with a warm build cache and about 18 s with an empty one (`go list -export` compiles every package once), measured on the development machine on 2026-11-18.
