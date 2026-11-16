# Log

*Package `log` is the built-in logger of `Ginject`: a `common.Logger` on top of `log/slog` with a JSON, text or pretty format, `log` struct tags and field masking that also apply to a logger you bring.*

- [Log](#log)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [`LogOptions` Parameters](#logoptions-parameters)
    - [Level](#level)
    - [LogFormat](#logformat)
    - [AddSource](#addsource)
    - [MaskFields](#maskfields)
  - [Formats](#formats)
  - [Functions](#functions)
    - [NewLog](#newlog)
    - [WrapLogger](#wraplogger)
  - [How values are written](#how-values-are-written)
  - [Struct tags](#struct-tags)
  - [Masking](#masking)
  - [Limits and guarantees](#limits-and-guarantees)
  - [Other exported names](#other-exported-names)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

## Key Features
- One logger per call: `NewLog` builds a new, independent logger from its options every time.
- JSON when stdout is not a terminal, the pretty tree when it is, with no configuration.
- Errors, `fmt.Stringer`s and `slog.LogValuer`s are written through their method, never expanded.
- Masking by path, case-insensitive, at any depth: `password` masks `Password`, `user.password` and a `password` inside a slice of structs.
- A call never panics because of a value and never forges a line: control characters are escaped.
- The tags and masking also apply to your own logger through `WrapLogger`.

## Usage

```go
package main

import (
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/log"
)

type User struct {
	Name     string `log:"name"`
	Password string `log:"password"`
}

func main() {
	app := core.New()
	app.SetLogger(log.NewLog(&log.LogOptions{
		Level:      log.DebugLevel,
		LogFormat:  log.JSONFormat,
		MaskFields: []string{"password"},
	}))
	app.Create(core.ModuleBuilder().Build())

	app.Logger().Info("SignedIn", "user", User{Name: "duoc", Password: "secret"})
}
```

The line holds `"user":{"name":"duoc","password":"[REDACTED]"}`.

## `LogOptions` Parameters

`NewLog` copies the options: later changes to the struct or to its `MaskFields` slice have no effect, and the options are never modified.

### Level
Type: `slog.Level`

Default: `log.InfoLevel` (the zero value of `slog.Level`)

Required: `false`

The lowest level that is written: `DebugLevel`, `InfoLevel`, `WarnLevel`, `ErrorLevel` or `FatalLevel`. The value is used as given.

### LogFormat
Type: `logFormat`

Default: `log.PrettyFormat` when stdout is a terminal (a character device), `log.JSONFormat` otherwise

Required: `false`

`log.JSONFormat`, `log.TextFormat` or `log.PrettyFormat`. A format you set wins whatever the output is. Use JSON in production: a container then gets machine-readable lines without ANSI codes.

### AddSource
Type: `bool`

Default: `false`

Required: `false`

Adds the file and line of the code that called the logger method, also through `WrapLogger`: slog's `source` in JSON and Text, a last row `source` with `file:line` in Pretty. Without it no caller lookup is made.

### MaskFields
Type: `[]string`

Default: `nil`

Required: `false`

The paths to mask, see [Masking](#masking).

## Formats

The level is written `DEBUG`, `INFO`, `WARN`, `ERROR` or `FATAL` in all three formats.

| Format | Line |
|---|---|
| `JSONFormat` | `{"time":"2026-11-16T10:00:00+07:00","level":"INFO","msg":"SignedIn","user":{"name":"duoc"}}` |
| `TextFormat` | `time=2026-11-16T10:00:00.000+07:00 level=INFO msg=SignedIn user=map[name:duoc]` |
| `PrettyFormat` | an empty line, the colored level label, the time (`2006-01-02 15:04:05`, local), the message in brackets, then one row per argument as a tree (map keys sorted, `null` for nil) |

Output is stdout. To write elsewhere, bring a logger with `app.SetLogger(log.WrapLogger(myLogger, fields))`.

## Functions

### NewLog

Builds a new logger from the options. Output goes to stdout.

#### Parameters
- 1st parameter: `*LogOptions`

- Description: The options; `nil` means level Info, the default format, no source and no mask rule

#### Returns
- 1st value: `common.Logger`

#### Usage

```go
logger := log.NewLog(&log.LogOptions{LogFormat: log.TextFormat})
```

#### Rules
- Every call returns a new logger; the options of each call apply to the logger it returns, and the `MaskFields` of one call mask on that logger only.
- JSON is one object per line with slog's keys (`time`, `level`, `msg`, then the arguments); Text is slog's `key=value` line.
- A call below the level writes nothing, calls no method of an argument and allocates nothing.
- Every record is one `Write` to the output; JSON and Text records of concurrent calls never interleave.
- `Fatal` writes the record at the `FATAL` level, then exits the process with status 1, whatever the level.
- Arguments are `key, value` pairs; a `slog.Attr` argument is one pair; a trailing key without a value is written under `!BADKEY`, and so is a pair whose key is not a string (`"!BADKEY":42`).

### WrapLogger

Returns a logger that converts and masks the arguments (see [How values are written](#how-values-are-written) and [Masking](#masking)), then calls the same method of `next` once.

#### Parameters
- 1st parameter: `common.Logger`

- Description: The logger to wrap

- 2nd parameter: `[]string`

- Description: The paths to mask; `nil` masks nothing

#### Returns
- 1st value: `common.Logger`

#### Usage

```go
logger := log.WrapLogger(myLogger, []string{"password", "token.*"})
```

#### Rules
- A logger from `NewLog` or `WrapLogger` with no fields is returned as it is.
- A logger from `NewLog` or `WrapLogger` with fields becomes one logger with the rules of both, not a second layer: each call converts and masks once and reaches the innermost logger once.
- Any other logger is wrapped; each method calls the same method of that logger once, with structs as `map[string]any` and changed slices as `[]any`.
- `maskFields` is copied.
- A value that needs no conversion and no masking is passed on as the same value, and when no argument needs any, the argument slice itself is passed on.
- An error, a `fmt.Stringer` or a `slog.LogValuer` reaches a wrapped logger unchanged, and none of its methods is called.
- `WrapLogger(nil, fields)` panics with `invalid logger: WrapLogger needs a logger`.
- Safe for concurrent calls.

## How values are written

- A value whose type, or pointer type, implements `error`, `fmt.Stringer` or `slog.LogValuer` is written through that method (`Error()`, `String()`, the resolved `LogValue()`), in every format: a `*url.URL` is its URL, never its fields. A nil pointer or nil interface is written as `null` without calling a method.
- Any other struct becomes a map of its fields (see [Struct tags](#struct-tags)); pointers are followed.
- Maps with string or integer keys become `map[string]any` (integer keys in decimal) when one of their values changes; slices and arrays become `[]any` when one of their elements changes.
- `time.Time` and `time.Duration` keep slog's formatting.

## Struct tags

- A field tagged `log:"<name>"` is logged under `<name>`, also when it is unexported.
- An untagged exported field is logged under its Go name; an untagged unexported field is not logged.
- `log:"-"` skips a field, even an exported one.
- Nested structs are expanded the same way.
- A struct that implements `fmt.Stringer`, `error` or `slog.LogValuer` is not expanded.

## Masking

A value's path is its key, then the keys under it joined by `.` (`user.password`). Elements of a slice or array have the path of the slice. A matched value is replaced by `[REDACTED]`. Matching ignores ASCII case.

| Rule | Path | Masked |
|---|---|---|
| `password` | `password`, `Password`, `user.password` | yes |
| `password` | `mypassword`, `userpassword` | no |
| `user.password` | `user.password`, `a.user.password` | yes |
| `user.password` | `admin.password` | no |
| `password.*` | `password.person.value` | yes, the leaves under it; the structure is kept |
| `password.*` | `password` | no |

- A rule without `*` that matches a struct, map or slice replaces it whole.
- Masking reaches struct fields, map values, slice and array elements, `slog.Attr` arguments and the attributes of a `slog.Group`, at any depth up to the limits below.
- A tagged field is matched by its tag name.
- An error, a `Stringer` or a `LogValuer` is masked only when its own path matches; its text is never searched.
- Never masked: the message, the keys, the text of an error or a `Stringer`, and the values of a map whose keys are neither strings nor integers.

## Limits and guarantees

- Nesting is followed 16 levels deep; a value below that is written `[MAX_DEPTH]`. After 1,000 values inside one argument, each remaining value is written `[TRUNCATED]`. A value that contains itself ends.
- A panic raised by a value's own `String`, `Error` or `LogValue` is recovered: the call writes one line with the original level and message and a `log_error` pair with the recovered value, and the logger keeps working.
- In Pretty, every control character of the message and of the keys is written escaped (`\n`, `\r`, `\t`, `\x1b`, the C1 controls U+0080 to U+009F; the separators U+2028 and U+2029 and the bidirectional controls U+202A to U+202E and U+2066 to U+2069 as `\u2028`), so a value cannot forge a line, reorder the text on screen or send terminal escape codes. In string values a line feed starts a new, indented line (a stack trace stays readable); everything else is escaped.
- JSON and Text rely on slog's escaping; JSON also escapes the C1 controls.
- A logger is immutable and safe for concurrent use; it starts no goroutine and opens no file.

## Other exported names

- `DebugLevel`, `InfoLevel`, `WarnLevel`, `ErrorLevel`, `FatalLevel` (`slog.Level`; `FatalLevel` is `12`).
- `TextFormat`, `JSONFormat`, `PrettyFormat` (`logFormat`).

## Performance notes

- **Level first.** A call below the level returns before touching its arguments (about 5 ns, no allocation).
- **No copy when nothing changes.** Scalars and errors take a path without reflection; through `WrapLogger` the argument slice is passed on as it is when no value needs work.
- **The record is built once.** The built-in logger builds the `slog.Record` itself and looks up the caller only with `AddSource`.
- **No `ReplaceAttr` per attribute.** The four slog levels need none; only `Fatal` uses a handler that renames its level.
- **Struct fields are read once per type** (name, index, tag) and kept in a `sync.Map`. Measured against reading them on every call: a struct logged as JSON takes 2.7 µs instead of 4.8 µs (−44 %) with 16 allocations instead of 28, and a tagged struct through `WrapLogger` 0.53 µs instead of 1.66 µs (−68 %).
- **A recover only when a method can run.** The scalar path has no `defer`; arguments with values whose methods may be called go through one deferred `recover`.

## Benchmarks

Machine-dependent, captured when this README was written:

```sh
go test -run '^$' -bench . -benchmem ./log/
```

```console
goos: darwin
goarch: amd64
cpu: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
BenchmarkLog_Info_Scalars_JSON-12                        	  981242	      1223 ns/op	     144 B/op	       3 allocs/op
BenchmarkLog_Info_Struct_JSON-12                         	  422803	      2836 ns/op	     601 B/op	      16 allocs/op
BenchmarkLog_Info_Struct_JSON_WithMaskRule-12            	  397663	      3616 ns/op	     657 B/op	      20 allocs/op
BenchmarkLog_BelowLevel-12                               	284131327	         4.836 ns/op	       0 B/op	       0 allocs/op
BenchmarkLog_ErrorWithStack_JSON-12                      	  336297	      3657 ns/op	     176 B/op	       2 allocs/op
BenchmarkLog_AccessLine_Pretty-12                        	  153910	      8847 ns/op	    4192 B/op	      51 allocs/op
BenchmarkLog_Info_LargeMap_Pretty-12                     	  115861	     10201 ns/op	    5896 B/op	      51 allocs/op
BenchmarkWrapLogger_Info_TaggedStruct-12                 	 2077896	       579.0 ns/op	     568 B/op	       9 allocs/op
BenchmarkWrapLogger_Info_TaggedStruct_WithMaskRule-12    	 1544994	       795.6 ns/op	     600 B/op	      11 allocs/op
BenchmarkWrapLogger_Info_NoStructArgs-12                 	57461472	        22.52 ns/op	       0 B/op	       0 allocs/op
```
