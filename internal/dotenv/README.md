# Dotenv

Internal package: applications use `modules/config` (`.env` with typed binding) or `owlkei.OptionsFromEnv()` instead.

*Package `dotenv` is the strict `.env` parser shared by `modules/config` and `OptionsFromEnv`: one syntax, bounded input, and errors that name the file and the line but never a value.*

- [Dotenv](#dotenv)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [Functions](#functions)
  - [Rules](#rules)

## Key Features

- One parser for both readers of `.env`, so a file means the same to the module and to `OK_*` options
- Bounded: a file larger than 1 MiB is refused before it is parsed
- Errors read `<path>:<line>: …` and never contain a value
- Variable expansion (`${NAME}`) only when the caller asks for it

## Usage

```go
data, err := dotenv.ReadFile(".env")
if err != nil {
	return err
}
file, err := dotenv.Parse(".env", data)
if err != nil {
	return err
}
for _, key := range file.Keys() {
	value, line, _ := file.Value(key)
	_, _ = value, line
}
```

## Functions

| Function | What it does |
|---|---|
| `ReadFile(path string) ([]byte, error)` | Reads a regular file of at most 1 MiB; the error starts with the path. |
| `Parse(path string, data []byte) (*File, error)` | Parses the content; `path` is only used in errors. A leading byte order mark is skipped. |
| `(*File).Keys() []string` | The keys of the file. |
| `(*File).Value(key string) (value string, line int, ok bool)` | The literal value of a key (no expansion) and its line. |
| `Load(path string, isExpand bool) (map[string]any, error)` | `ReadFile` + `Parse` for `modules/config`: the values by key, expanded when `isExpand` is true; errors start with `invalid config: `. |

## Rules

- The syntax is the one of [`modules/config` › `.env` File Format](../../modules/config/README.md#env-file-format).
- `Parse` and `Value` never expand a variable: `B=${A}` gives the text `${A}`. `Load` expands when asked.
- A syntax error names the path and the line, never the value of the line.
- `Load` keeps the module's error format (`invalid config: <path>:<line>: …`).
