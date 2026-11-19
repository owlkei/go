# sample/async

A stock service that starts before its warehouse does. The warehouse client is a module factory in
`AsyncImports`: the app serves at once, `GET /stock/{sku}` answers `503` until the warehouse answers,
then switches on by itself. `GET /health` never waits.

The warehouse is simulated: it refuses connections for `WAREHOUSE_DELAY` after start.

## Run it

```sh
go run ./sample/async                              # http://localhost:8081, the warehouse is up after 5 s
WAREHOUSE_DELAY=20s go run ./sample/async          # a slower warehouse
WAREHOUSE_URL=ftp://warehouse go run ./sample/async # a bad URL: the module fails at once
PORT=3000 go run ./sample/async                    # another port
```

Then call it while it starts:

```sh
curl -i localhost:8081/stock/abc   # 503 with Retry-After, then 200 {"quantity":30,"sku":"abc"}
curl -i localhost:8081/health      # 200 ok from the first request
```

## Routes

| Route | Handler | Module | Answers |
|---|---|---|---|
| `GET /stock/{sku}` | `StockController.READ_stock_BY_sku` | `StockModule` | `503` while the warehouse module is `pending` or `failed`, then the quantity as JSON |
| `GET /health` | `HealthController.READ_health` | `Module` (root) | `ok`, always |

## What it shows

- **An async module factory.** `NewWarehouseModule(ctx)` returns `(owlky.Module, error)`.
  `StockModule` lists it in `AsyncImports`, so `Create()` does not wait for it.
- **Controllers in a static module.** The factory's module has providers only. `StockController` sits
  in `StockModule`, which imports the factory: controllers in a module that an async factory returns
  are refused.
- **Transient and permanent errors.** A bad `WAREHOUSE_URL` or `WAREHOUSE_DELAY` is returned at once:
  the module is `failed` and the stock route answers `503` without `Retry-After` until you fix the value
  and restart. A refused connection is transient: `owlky.Retry` tries again with backoff (1 s, then up
  to 10 s) until it works or the app shuts down.
- **What clients see.** While the module is `pending`, the `503` carries `Retry-After` with the seconds
  until the next attempt. The health route is in the root module and injects nothing from the
  warehouse, so it is never gated.
- **What the log shows.** `ModulePending` at `Create()`, one `ModuleRetry` per failed attempt, then
  `ModuleReady` (or `ModuleFailed` with the cause).
