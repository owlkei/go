# Color

*Package `color` wraps text in ANSI escape codes for terminal output: bootstrap panics, the startup log and the pretty log format (access-line durations included).*

- [Color](#color)
  - [Usage](#usage)
  - [Functions](#functions)
    - [Fmt functions](#fmt-functions)
  - [Colored](#colored)
    - [ForDuration](#forduration)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

The package is internal: only code in the Owlkei Go module can import it.

## Usage

```go
package main

import (
	"fmt"

	"go.owlkei.com/owlkei/internal/color"
)

func main() {
	fmt.Println(color.FmtGreen("status: %s %d", "ok", 200))
	fmt.Println(color.FmtRed("100%"))
}
```

## Functions

### Fmt functions

Each function returns the open code, the text and the close code.

| Function | Open code | Close code |
|---|---|---|
| `FmtWhite` | `\x1b[97m` | `\x1b[0m` |
| `FmtBlue` | `\x1b[34m` | `\x1b[0m` |
| `FmtGreen` | `\x1b[32m` | `\x1b[0m` |
| `FmtCyan` | `\x1b[36m` | `\x1b[0m` |
| `FmtYellow` | `\x1b[33m` | `\x1b[0m` |
| `FmtRed` | `\x1b[31m` | `\x1b[0m` |
| `FmtMagenta` | `\x1b[35m` | `\x1b[0m` |
| `FmtPurple` | `\x1b[38;5;129m` | `\x1b[39m` |
| `FmtOrange` | `\x1b[38;5;208m` | `\x1b[0m` |
| `FmtDim` | `\x1b[2m` | `\x1b[0m` |
| `FmtBold` | `\x1b[1m` | `\x1b[0m` |
| `FmtItalic` | `\x1b[3m` | `\x1b[0m` |
| `FmtBGBlue` | `\x1b[44m` | `\x1b[0m` |
| `FmtBGGreen` | `\x1b[42m` | `\x1b[0m` |
| `FmtBGRed` | `\x1b[41m` | `\x1b[0m` |
| `FmtBGYellow` | `\x1b[43m` | `\x1b[0m` |
| `FmtBGGrey` | `\x1b[47m` | `\x1b[49m` |
| `FmtBGDim` | `\x1b[48;5;236m` | `\x1b[0m` |

#### Parameters
1. `format` `string`: the text, or a `fmt` format when arguments follow.
2. `a` `...any`: the format arguments.

#### Returns
1. `string`: the colored text.

#### Usage

```go
color.FmtRed("%s: %s", "core", "duplicate provider")
```

#### Rules
- Each function returns `<open> + text + <close>` with the codes in the table.
- Without arguments, the format is used verbatim: `FmtRed("100%")` → `"\x1b[31m100%\x1b[0m"`, and verbs such as `%d %s` stay as written.
- With arguments, the text is `fmt.Sprintf(format, a...)`.

## Colored

`Colored{Text string; Color Color}` is a text with its color.

| `Color` constant | Code |
|---|---|
| `Gray` | `\x1b[90m` |
| `Green` | `\x1b[32m` |
| `Blue` | `\x1b[34m` |
| `Yellow` | `\x1b[33m` |
| `Orange` | `\x1b[38;5;208m` |
| `Red` | `\x1b[31m` |

- The pretty log format prints `Text` in `Color`, then resets.
- `Colored` implements `slog.LogValuer` (its `Text`), so the JSON and Text log formats write only the text, never the ANSI code.

### ForDuration

`ForDuration(d time.Duration) Color` picks the color of a duration: `Gray` below 10 ms, `Green` below 50 ms, `Blue` below 100 ms, `Yellow` below 300 ms, `Orange` below 1 s, `Red` from 1 s. The pretty log format colors the durations of access lines with it.

```go
c := color.ForDuration(120 * time.Millisecond)
```

#### Rules
- Each threshold belongs to the next color: 10 ms is `Green`, 1 s is `Red` (tested through the access log's `colorForDuration`).

## Performance notes

The package runs on the cold path only (startup, panics, log handler construction), so every function shares one helper. Results that escape the caller, as they do in real use, allocate the same as before the helper.

## Benchmarks

Results depend on the machine. They were captured when this README was generated, with `go test -run=^$ -bench=. -benchmem ./internal/color`:

```console
goos: darwin
goarch: amd64
cpu: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
BenchmarkFmtGreen-12     	 6980971	       168.6 ns/op	      40 B/op	       2 allocs/op
BenchmarkFmtRed-12       	 9730675	       123.7 ns/op	      80 B/op	       2 allocs/op
BenchmarkFmtBGDim-12     	 9880976	       121.3 ns/op	      48 B/op	       2 allocs/op
BenchmarkFmtPurple-12    	 8653387	       138.8 ns/op	      48 B/op	       2 allocs/op
```
