# Log

*Package `log` is the built-in logger of `Ginject`: a `common.Logger` on top of `log/slog` with a pretty, text or JSON format, `log` struct tags and field masking that also apply to a logger you bring.*

- [Log](#log)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [`LogOptions` Parameters](#logoptions-parameters)
    - [Level](#level)
    - [LogFormat](#logformat)
    - [TimeFormat](#timeformat)
    - [AddSource](#addsource)
    - [MaskFields](#maskfields)
    - [Transports](#transports)
  - [Functions](#functions)
    - [NewLog](#newlog)
    - [WrapLogger](#wraplogger)
    - [NewPrettyHandler](#newprettyhandler)
  - [Struct tags](#struct-tags)
  - [Other exported types](#other-exported-types)
  - [Benchmarks](#benchmarks)

## Key Features
- One logger per call: `NewLog` builds a new logger from its options every time.
- Structs are logged field by field, named by their `log` tag; `log:"-"` skips a field.
- Masking by path: `password` masks every `password` key at any depth, `token.*` every leaf under `token`.
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

### Level
Type: `slog.Level`

Default: `log.InfoLevel` (the zero value of `slog.Level`)

Required: `false`

The lowest level that is written: `DebugLevel`, `InfoLevel`, `WarnLevel`, `ErrorLevel` or `FatalLevel`. `-1` is read as `InfoLevel`.

### LogFormat
Type: `logFormat`

Default: `log.PrettyFormat`

Required: `false`

`log.TextFormat` (slog's text handler), `log.JSONFormat` (slog's JSON handler) or `log.PrettyFormat` (colored, one value per line).

### TimeFormat
Type: `string`

Default: `time.DateTime`

Required: `false`

The time layout of the pretty format.

### AddSource
Type: `bool`

Default: `false`

Required: `false`

Adds the source file and line to each record.

### MaskFields
Type: `[]string`

Default: `nil`

Required: `false`

The paths to mask, see [WrapLogger](#wraplogger).

### Transports
Type: `[]*LogTransport`

Default: `nil`

Required: `false`

Not read: every format writes to stdout. `LogTransport` has `Filename string` and `Level slog.Level`.

## Functions

### NewLog

Builds a new logger from the options and wraps it with its `MaskFields` (see [WrapLogger](#wraplogger)). Output goes to stdout.

#### Parameters
- 1st parameter: `*LogOptions`

- Description: The options; `nil` means the defaults

#### Returns
- 1st value: `common.Logger`

#### Usage

```go
logger := log.NewLog(&log.LogOptions{LogFormat: log.TextFormat})
```

#### Rules
- Every call returns a new logger, also for `nil` options.
- The options of each call apply to the logger it returns: a text logger writes text lines (`msg=hello`), a JSON logger writes JSON lines (`"msg":"hello"`).
- The `MaskFields` of one call mask on that logger only.
- Without options, the format is `PrettyFormat` and the time layout `time.DateTime`.

### WrapLogger

Wraps any `common.Logger` so that struct arguments are expanded by their `log` tags and the values at `maskFields` paths are replaced with `[REDACTED]`.

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
- `WrapLogger(l, nil)` returns `l` unchanged when `l` came from `NewLog` or `WrapLogger`; any other logger is wrapped and every call is forwarded to it.
- An exact rule (`password`) masks that key at the top level and as the last segment of a nested path (`user.password`), never a key that only ends with the text (`userpassword`).
- A multi-segment rule (`user.password`) masks only its own suffix, not `admin.password`.
- A struct value under an exact rule is masked whole, without looking inside.
- A wildcard rule (`password.*`) masks every leaf nested under the path, keeps the structure, reaches into tagged structs, and does not mask the bare `password` key.
- Several rules apply together; keys no rule names are passed through.
- A trailing key with no value and a value whose key is not a `string` pass through unchanged.
- Safe for concurrent calls.

### NewPrettyHandler

`NewPrettyHandler(out io.Writer, opts *PrettyHandlerOptions) *PrettyHandler` returns the `slog.Handler` of the pretty format. `PrettyHandlerOptions` holds `TimeFormat string` and the embedded `slog.HandlerOptions`. `(*PrettyHandler).Handle` writes one record: the level, the message (an empty message is skipped), then each attribute on its own line, slices, structs and maps as indented children (map keys sorted), `nil` as `null`.

## Struct tags

- A field tagged `log:"<name>"` is logged under `<name>`, also when it is unexported.
- An untagged exported field is logged under its Go name; an untagged unexported field is not logged.
- `log:"-"` skips a field, even an exported one.
- Nested structs are expanded the same way.
- A struct that implements `fmt.Stringer`, `error` or `slog.LogValuer` is not expanded.

## Other exported types

- `Color` (`string`) with `ColorGray`, `ColorGreen`, `ColorBlue`, `ColorYellow`, `ColorOrange`, `ColorRed`, and `Colored` (`Text string`, `Color Color`, `String() string`): a value printed in its own color by the pretty format.
- `DebugLevel`, `InfoLevel`, `WarnLevel`, `ErrorLevel`, `FatalLevel` (`slog.Level`; `FatalLevel` is `12`) and `TextFormat`, `JSONFormat`, `PrettyFormat`.

## Benchmarks

Machine-dependent, captured when this README was written (darwin/amd64, Intel Core i7-9750H @ 2.60GHz):

```sh
go test -run '^$' -bench . -benchmem ./log/
```

| Benchmark | ns/op | B/op | allocs/op |
|-----------|-------|------|-----------|
| BenchmarkPrettyHandler_Handle_NoAttrs | 195.8 | 0 | 0 |
| BenchmarkPrettyHandler_Handle_WithAttrs | 485.8 | 128 | 1 |
| BenchmarkLoadLogOptions_Default | 2.299 | 0 | 0 |
| BenchmarkLoadLogOptions_Custom | 1.099 | 0 | 0 |
| BenchmarkPrettyHandler_Handle_NestedStages | 5982 | 3056 | 49 |
| BenchmarkPrettyHandler_Handle_LargeMap | 6015 | 3496 | 48 |
| BenchmarkNewPrettyHandler | 1220 | 848 | 28 |
| BenchmarkWrapLogger_Info_TaggedStruct | 996.2 | 840 | 14 |
| BenchmarkWrapLogger_Info_TaggedStruct_WithMaskRule | 1082 | 824 | 13 |
| BenchmarkWrapLogger_Info_NoStructArgs | 60.49 | 128 | 2 |
| BenchmarkJSONHandler_Handle_NoStructAttrs_Unwrapped | 417.9 | 0 | 0 |
