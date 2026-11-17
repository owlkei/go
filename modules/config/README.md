# Config Module

*Config module is a part of `Ginject` framework. It merges the process environment, `.env` files and any configuration you load yourself into one read-only-by-default service, and never prints a value.*

- [Config Module](#config-module)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [`ConfigModuleOptions` Parameters](#configmoduleoptions-parameters)
    - [IsGlobal](#isglobal)
    - [IsIgnoreEnvFile](#isignoreenvfile)
    - [IsOverride](#isoverride)
    - [IsExpandVariables](#isexpandvariables)
    - [EnvFilePaths](#envfilepaths)
    - [OnInit](#oninit)
    - [Loads (Custom Configuration Files)](#loads-custom-configuration-files)
    - [Hooks](#hooks)
  - [`.env` File Format](#env-file-format)
  - [Precedence](#precedence)
  - [Errors](#errors)
  - [`ConfigService`](#configservice)
  - [`ConfigService` Methods](#configservice-methods)
    - [Get](#get)
      - [Parameters](#parameters)
      - [Returns](#returns)
      - [Usage](#usage-1)
    - [Set](#set)
      - [Parameters](#parameters-1)
      - [Usage](#usage-2)
    - [Transform](#transform)
      - [Parameters](#parameters-2)
      - [Returns](#returns-1)
  - [Benchmarks](#benchmarks)

## Key Features
- Zero-dependency
- Strict `.env` parsing: a value is never cut short, and a bad line stops the boot with its line number
- Multiline values, quotes and escapes, `export` prefixes, CRLF files and a byte-order mark
- Variable expansion with cycle detection
- Extend for various file types, access nested values via namespace
- Secrets stay secret: no error, panic or printed service shows a value
- Safe for concurrent `Get` and `Set`

## Usage

First you need create a `.env` file in the root of your project:

```dosini
USERNAME=ginject # Support comments
PASSWORD=123$%^
```

Import config module into main module, by default config module will read `.env` in the root of your project (the nearest directory with a `go.mod`).
Create a provider and inject `ConfigService`.
Let's take a look in an example following `main.go`:

```go
package main

import (
	"fmt"

	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/modules/config"
)

type DBProvider struct {
	ConfigService config.ConfigService
}

func (dbProvider DBProvider) NewProvider() ginject.Provider {
	fmt.Println("USERNAME:", dbProvider.ConfigService.Get(("USERNAME")))
	fmt.Println("PASSWORD:", dbProvider.ConfigService.Get(("PASSWORD")))
	return dbProvider
}

func main() {
	app := ginject.New()

	app.Create(
		ginject.ModuleBuilder().
			Imports(
				config.Register(&config.ConfigModuleOptions{}),
			).
			Providers(DBProvider{}).
			Build(),
	)
}
```

Finally, run:
```shell
go run main.go
```

There are logs print out on console:
```console
USERNAME: ginject
PASSWORD: 123$%^
```

## `ConfigModuleOptions` Parameters

### IsGlobal
Type: `bool`

Default: `false`

Required: `false`

Set module as globally. By set to `true`, you don't need to import config module wherever you read env variables.

```go
config.Register(&config.ConfigModuleOptions{
  IsGlobal: true,
})
```

### IsIgnoreEnvFile
Type: `bool`

Default: `false`

Required: `false`

If you don't want to use `.env` file, instead would like to simply access `OS` environment variables from the runtime environment. set `IsIgnoreEnvFile: true`. No file is read, the default `.env` included. Setting it together with `EnvFilePaths` panics with `invalid config: IsIgnoreEnvFile is set with ENVFilePaths`.

```go
config.Register(&config.ConfigModuleOptions{
  IsIgnoreEnvFile: true,
})
```

### IsOverride
Type: `bool`

Default: `false`

Required: `false`

Let later sources win. By default the first source that defines a key wins: the process environment, then the files, then `Loads` (see [Precedence](#precedence)). With `IsOverride`, the last one wins, so a `.env` value overrides a variable already set on your machine. Values are never written to the process environment.

```go
config.Register(&config.ConfigModuleOptions{
  IsOverride: true,
})
```

### IsExpandVariables
Type: `bool`

Default: `false`

Required: `false`

Module support for environment variable expansion. With this technique, you can create nested environment variables, where one variable is referred to within the definition of another. `${NAME}` takes the value of `NAME` from the same file (defined before or after), else from the process environment. Only the braced form is expanded (`$NAME` stays as written), single-quoted values are never expanded, and `\$` in a double-quoted value is a literal `$`. Without `IsExpandVariables`, `${NAME}` stays as written. See the following example:

```dosini
HOST=localhost:27017
USER=admin
PWD=secret
DB=test
URI=mongodb://${USER}:${PWD}@${HOST}/${DB}
```

```go
config.Register(&config.ConfigModuleOptions{
  IsExpandVariables: true,
})
```

```go
fmt.Println("URI:", dbProvider.ConfigService.Get(("URI")))
```

Console:
```console
URI: mongodb://admin:secret@localhost:27017/test
```

### EnvFilePaths
Type: `[]string`

Default: `.env` in the nearest directory, from the working directory upward, that contains `go.mod`; else `.env` in the working directory

Required: `false`

By default, the module looks for a .env file in the root directory of your project, so `go test` in a package directory finds it too. A missing default file is not an error. To specify another paths for the .env files, set the `EnvFilePaths` property to list of files, as follows. Every file you list must exist, and the default file is then not read:

```go
config.Register(&config.ConfigModuleOptions{
  EnvFilePaths: []string{".env.sql", ".env.aws", "env.redis"},
})
```

### OnInit
Type: `ConfigOnInitFn` (`func()`)

Default: `nil`

Required: `false`

`OnInit` is a module's life cycle which is invoked before Config module was injected.

### Loads (Custom Configuration Files)
Type: `[]func() map[string]any`

Default: `nil`

Required: `false`

For complex projects, sometimes `.env` is not enough, you want to various file types in their project or simply you prefer using another files types. `Loads` is property that allows you pass load function into array. By this approach, you can easy using any files you want, as long as function return `map[string]any` type. Nested maps (`map[string]any`) and slices (`[]any`) are flattened to dotted keys, and the parent keys are kept. Nesting deeper than 32 levels panics with `invalid config: Loads[<index>]: nested deeper than 32 levels`.

Let's take a look in 2 below examples:

Firstly, create `configuration` function:
```go
func configuration() map[string]any {
	return map[string]any{
		"S3_BUCKET":  "This is your S3 bucket",
		"SECRET_KEY": "This is you secret key",
	}
}
```

Then, add into `Loads` property
```go
config.Register(&config.ConfigModuleOptions{
  Loads: []config.ConfigLoadFn{configuration},
})
```

Print `S3_BUCKET` and `SECRET_KEY` on console:
```console
This is your S3 bucket
This is you secret key
```

Now, let's see how can we read another file types. Here is an example of a configuration using `YAML` format:

We need install go package to encode and decode YAML values. For this example, we will use `yaml.v3`.

Install:
```shell
go get gopkg.in/yaml.v3
```

Create an sample `config.yaml`:
```yaml
database:
  host: localhost
  port: 5432
  name: mydatabase
  username: myuser
  password: mypassword

server:
  host: localhost
  port: 8080
  debug: true

logging:
  level: info
  file_path: /var/log/myapp.log
```

Edit `configuration` function:
```go
import (
	"os"

	"gopkg.in/yaml.v3"
)

func configuration() map[string]any {
	b, err := os.ReadFile("config.yaml")
	if err != nil {
		panic(err)
	}

	config := make(map[string]any)
	err = yaml.Unmarshal(b, &config)
	if err != nil {
		panic(err)
	}

	return config
}
```

You can accress variables via namespace:
```go
provider.ConfigService.Get(("database"))
// Will print: map[host:localhost name:mydatabase password:mypassword port:5432 username:myuser]
provider.ConfigService.Get(("database.port"))
// Will print: 5432
```

### Hooks
Type: `[]func(ConfigService)`

Default: `[]func(ConfigService)`

Required: `false`

Hooks is a property that allow you catch env map before config module instance is created. The idea of hook is give you ability to transforms or validations configuration to avoid mismatch configs before application start.

Hook functions run in order, inside `Register`, after every source is merged. All of them and every injected `ConfigService` share one state: what a hook `Set`s is seen by later hooks and by your providers.

```go
config.Register(&config.ConfigModuleOptions{
  Hooks: []config.ConfigHookFn{
    func(c config.ConfigService) {
      c.Set("ANONYMOUS", "John Doe")
    },
  },
}),
```
```go
provider.ConfigService.Get(("ANONYMOUS")) // Will print: John Doe
```

It is standard practice to throw an exception during application startup if required environment variables haven't been provided or if they don't meet certain validation rules.

In this example we will use `validator` package to validate struct tags. First, we need install library.

Install:
```shell
go get github.com/go-playground/validator/v10
```

Add env key like below sample:

```dosini
EMAIL=duocdevtest@gmail
```

Define a struct:

```go
type Config struct {
	EMAIL string `validate:"email"`
}
```

Finally, on `Hooks` property add function:

```go
config.Register(&config.ConfigModuleOptions{
  Hooks: []config.ConfigHookFn{
    func(c config.ConfigService) {
      errs := validator.New().Struct(Config{
        EMAIL: c.Get("EMAIL").(string),
      })

      if errs != nil {
        panic(errs)
      }
    },
  },
}),
```

This will throw exception:
```shell
panic: Key: 'Config.EMAIL' Error:Field validation for 'EMAIL' failed on the 'email' tag
```

## `.env` File Format

A file is UTF-8 text. A leading byte-order mark is skipped, and lines end with LF or CRLF.

```dosini
# A comment line
export APP_NAME=shop
DATABASE_URL=postgres://db/shop?sslmode=disable   # a comment after a blank
API_TOKEN=YWJj==
WINDOWS_PATH=C:\new\table
GREETING="Hello,\tworld\n"
PRIVATE_KEY="-----BEGIN KEY-----
MIIBOgIBAAJBAK
-----END KEY-----"
RAW='${NOT_EXPANDED} \n stays as written'
```

- **Lines:** an empty line, or one whose first non-blank character is `#`, is ignored. Every other line is `KEY=value`, optionally after `export `. A key is letters, digits and `_`, and does not start with a digit.
- **Unquoted values:** the rest of the line with the blanks around it removed. A `#` after a blank starts a comment; any other `#` belongs to the value (`pa#ss`). `=` is an ordinary character and backslashes are kept, so URLs, base64 padding and Windows paths stay whole. `KEY=` is an empty string.
- **Double-quoted values:** run to the next unescaped `"`, may span lines and keep inner blanks. The escapes are `\n`, `\r`, `\t`, `\\`, `\"` and `\$`; any other backslash stays as written.
- **Single-quoted values:** run to the next `'`, may span lines, and are taken literally: no escape, no expansion.
- After a closing quote, only blanks and a comment may follow.
- **Repeated keys:** the last value in a file wins.
- **Limits:** a file is at most 1 MiB and must be a regular file (a FIFO or a device is refused without being read); a value is at most 64 KiB after expansion, and all values of a file together at most 16 MiB after expansion; a NUL byte is refused.

## Precedence

Sources are merged in this order: the process environment, then the files in the order of `EnvFilePaths`, then `Loads` in their order. The first source that defines a key wins; with `IsOverride`, the last one wins. The process environment is read, never written: `os.Getenv` does not see values from files.

## Errors

Every failure happens in `Register`, at startup, as a panic `invalid config: <detail>` (in red on the terminal). A message names the file, the line and, for expansion, the variable name. It never contains a value or the text of a line, so a secret in a broken file does not reach your logs.

| Detail | Cause |
|---|---|
| `<path>: <os error>` | The file is missing or unreadable, for example `no such file or directory` or `permission denied`. |
| `<path>: not a regular file` | The path is a directory, a FIFO or a device. |
| `<path>: larger than 1 MiB` | The file is too large. |
| `<path>:<line>: expected KEY=value` | A line that is not a comment has no `=`. |
| `<path>:<line>: invalid key` | The key has a character other than letters, digits and `_`, or starts with a digit. |
| `<path>:<line>: unterminated quote` | A quote opened on that line is never closed. |
| `<path>:<line>: unexpected text after the closing quote` | Something other than blanks or a comment follows a quoted value. |
| `<path>:<line>: NUL byte` | The file holds a NUL byte. |
| `<path>:<line>: ${NAME} is not defined` | Expansion found no `NAME` in the file or the process environment. |
| `<path>:<line>: ${NAME} refers to itself` | Expansion found a cycle, of any length. |
| `<path>:<line>: value larger than 64 KiB` | A value, after expansion, is too large. |
| `<path>:<line>: values larger than 16 MiB in total after expansion` | The values of the file add up to more than 16 MiB once expanded, for example many copies of one large value. The line is the first value past the limit. |
| `IsIgnoreEnvFile is set with ENVFilePaths` | The two options contradict each other. |
| `Loads[<index>]: nested deeper than 32 levels` | A loaded map is nested too deeply, or contains itself. |
| `ConfigService was not created by Register` | `Set` was called on a zero `ConfigService{}`. |

## `ConfigService`

```go
type ConfigService struct {
	State *configState
}
```

The injectable provider. `State` is the configuration shared by every copy of the service; it has no field or method you can use. It is exported only because the framework injects exported fields. Printing a service, or a struct that holds one, with `fmt`, `encoding/json` or `log/slog` shows no key and no value.

## `ConfigService` Methods

### Get

Method help to get env value by key. Return `nil` if hasn't value. A zero `ConfigService{}` returns `nil` for every key. `Get` is one atomic load and one map lookup, safe from any goroutine at any time.

#### Parameters
- 1st parameter: `string`

- Description: Env key name


#### Returns
- 1st value: `interface{}`

- Description: Env key value

#### Usage

```go
port, _ := provider.ConfigService.Get("PORT").(string)
```

### Set

Method to set or update a configuration value by key. Every copy of the service sees the new value.

`Set` is safe from any number of goroutines, together with `Get` and `Transform`: a `Get` that runs at the same time sees the old or the new value. Each `Set` copies the whole configuration, so it is meant for hooks and startup code, not for every request. `Set` on a zero `ConfigService{}` panics with `invalid config: ConfigService was not created by Register`.

#### Parameters
- 1st parameter: `string`

- Description: Config key name

- 2nd parameter: `any`

- Description: Config value

#### Usage

```go
provider.ConfigService.Set("NEW_KEY", "new_value")
value := provider.ConfigService.Get("NEW_KEY") // Returns: "new_value"
```

### Transform

Binds configuration values to the `bind:"NAME"` fields of a struct and returns the filled struct. String values bind as they are; `bool`, integer and float values bind from their text (`true`, `8080`, `1.5`); maps, slices and `nil` values are skipped. It never panics on a value's type.

#### Parameters
- 1st parameter: `any`

- Description: The target struct or parameter to transform

#### Returns
- 1st value: `any`

- Description: Transformed configuration data

- 2nd value: `[]ginject.FieldLevel`

- Description: Field validation levels

#### Usage

```go
type Settings struct {
	Port  int    `bind:"PORT"`
	Debug bool   `bind:"DEBUG"`
	Name  string `bind:"APP_NAME"`
}

dto, _ := provider.ConfigService.Transform(Settings{})
settings := dto.(Settings)
```

## Benchmarks

Captured on darwin/amd64 (Intel Core i7-9750H, 12 threads, Go 1.27.4) with
`go test -run='^$' -bench . -benchmem -count 5 ./modules/config/`, medians. Numbers are
machine-dependent and were captured when this page was written (2026-11-17). `Register` runs once
at startup; `Get` is the only call meant for request paths.

| Benchmark | ns/op | allocs/op |
|-----------|-------|-----------|
| BenchmarkIsValidKey_Valid | 7.5 | 0 |
| BenchmarkIsValidKey_Invalid | 1.5 | 0 |
| BenchmarkFlatten | 794 | 9 |
| BenchmarkRegister_File (160 lines) | 165,100 | 1,212 |
| BenchmarkRegister_FileExpandVariables (160 lines) | 190,000 | 1,223 |
| BenchmarkConfigService_Get (12 threads) | 2.4 | 0 |
| BenchmarkConfigService_Set | 1,418 | 5 |
