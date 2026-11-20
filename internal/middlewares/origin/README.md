# Origin

Internal package: applications list origins in `AppOptions.Origins` instead; the WebSocket Origin check runs by itself when `AppOptions.WS.Path` is set.

*Package `origin` compiles `AppOptions.Origins` once into the set that the `cors` and `csrf` stages read, and holds the `origin` stage that refuses a WebSocket handshake from another site (cross-site WebSocket hijacking).*

- [Origin](#origin)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [`Set`](#set)
  - [`Check`](#check)

## Key Features

- One list for CORS, CSRF and the WebSocket handshake
- Exact entries in a map, `scheme://*.host[:port]` wildcards, or `"*"` alone
- At most 256 entries, each at most 256 bytes
- The handshake check has no switch and no configuration: same origin and listed origins pass

## Usage

```go
set, err := origin.Parse("Origins", []string{"https://app.example.com", "https://*.example.com"})
if err != nil {
	return err
}
_ = set.Matches("https://admin.example.com")
stage := origin.NewCheck(set)
_ = stage
```

## `Set`

`Parse(field string, entries []string) (*Set, error)` checks and compiles the entries; `field` names the list in errors.

- An entry is `scheme://host[:port]` in lower case, `scheme://*.host[:port]`, or `"*"` as the only entry.
- Errors: `<field> has <n> entries, at most 256 are allowed`; `"*" must be the only entry of <field>`; `<field> entry "<entry>" must be scheme://host[:port] in lower case, scheme://*.host[:port] or "*" alone`.
- `Matches(origin string) bool`: an exact entry, a wildcard entry whose `*` stands for one or more labels, or any origin with `"*"`. An origin longer than 256 bytes never matches a list.
- `IsAny() bool` reports `"*"`; `Entries() []string` returns a copy of the list.

## `Check`

`NewCheck(set *Set) *Check` is the `origin` pre-route stage of the handshake.

- No `Origin` header passes (a client that is not a browser).
- The same origin passes: the `Origin`, in lower case, equals the client's scheme and host (as `owlky.ClientScheme` and `owlky.ClientHost` give them); a default port (`:443` for `https`, `:80` for `http`) is dropped on both sides.
- An `Origin` in the set passes.
- Anything else, `null` and an `Origin` longer than 256 bytes included, is refused with `403 Origin not allowed`, and the `refused` counter of `Snapshot()` grows.
