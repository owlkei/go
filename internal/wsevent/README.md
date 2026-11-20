# WSEvent

Internal package: applications declare WebSocket events with `SUBSCRIBE_…` methods on a controller that embeds `owlkei.WS` instead.

*Package `wsevent` is the WebSocket event table: it maps each event pattern of the app to its handler and handshake middlewares, and finds the handler for a topic a client subscribes or publishes to.*

- [WSEvent](#wsevent)
  - [Usage](#usage)
  - [Types](#types)
  - [Functions and methods](#functions-and-methods)
  - [Matching order](#matching-order)
  - [Cost](#cost)

## Usage

The framework builds one table per app while `Create()` registers the WebSocket controllers, then reads it for every `subscribe` and `publish` message.

```go
events := wsevent.NewWSEvent()
events.AddInjectableHandler("chat.*", handler)

item, pattern, ok := events.Match("chat.room1")
// item.Handler == handler, pattern == "chat.*", ok == true
```

## Types

| Type | Fields | Meaning |
|---|---|---|
| `WSEventItem` | `Handler any`, `Middlewares []ctx.WSHandler` | One event: its handler function and the middlewares bound to it |
| `WSEvent` | none exported | The table. Not safe for concurrent writes: it is filled during `Create()` and only read afterwards |

## Functions and methods

| Name | Description |
|---|---|
| `NewWSEvent() *WSEvent` | An empty table |
| `Add(pattern string, value WSEventItem)` | Stores an item under a pattern, replacing any item there |
| `AddMiddlewares(pattern string, middlewares ...ctx.WSHandler)` | Appends middlewares to a pattern's item |
| `AddInjectableHandler(pattern string, handler any)` | Sets a pattern's handler. Panics with `invalid handler: <kind> is not a handler` when `handler` is `nil` or not a function |
| `Match(topic string) (WSEventItem, string, bool)` | The item whose pattern matches `topic`, that pattern, and whether one was found. Only items with a handler match |

Patterns use the syntax of `internal/pattern`: `*` is exactly one segment, a trailing `**` is one or more (see `internal/pattern/MATCHING.md`).

## Matching order

The first rule that finds an item with a handler wins:

1. **Exact:** the topic itself is a registered pattern.
2. **Trailing `**`:** the longest prefix of the topic, cut at a `.`, that has a `prefix.**` pattern.
3. **Other wildcards:** registered complex patterns (`chat.*`, `*.created`, `tenant.*.user.**`), in registration order.
4. **Global:** the `**` pattern.

## Cost

- `Match` does one map lookup for an exact topic, one per `.` in the topic for trailing patterns, then one `pattern.Match` per complex pattern. It allocates nothing.
- The table holds one entry per pattern, built once at `Create()`.
