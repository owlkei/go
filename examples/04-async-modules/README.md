# 04 · Async modules

A stock service that starts before its warehouse does. Most apps wait for every dependency at start,
and one slow database keeps the whole app down. Here the warehouse module is imported with
`AsyncImports`: the app serves at once, `GET /stock/{sku}` answers `503` until the warehouse answers,
then switches on by itself. `GET /health` never waits.

The warehouse is simulated: it refuses connections for `WAREHOUSE_DELAY` (5 s by default) after the
app starts.

## What you learn

- **`AsyncImports`.** `NewWarehouseModule(ctx)` returns `(owlkei.Module, error)` and runs in the
  background. `Create()` does not wait for it.
- **Who waits.** `StockController` injects `Warehouse`, so its routes stay registered but answer `503`
  until the warehouse module is ready. `HealthController` injects nothing from it and answers at once.
- **Transient and permanent errors.** A refused connection is transient: `owlkei.Retry` tries again with
  backoff (1 s, then up to 10 s) until it works or the app shuts down. A bad `WAREHOUSE_URL` is
  permanent: the factory returns the error at once, the module fails, and the route answers `503`
  without `Retry-After` until you fix the value and restart.
- **What clients see.** While the module is pending, the `503` carries `Retry-After` with the seconds
  until the next attempt, so a polite client knows when to try again.
- **What the log shows.** `ModulePending` at start, one `ModuleRetry` per failed attempt, then
  `ModuleReady` (or `ModuleFailed` with the cause).
- **Controllers stay in a static module.** The factory's module holds providers only: a module that an
  async factory returns can't declare controllers, because its routes must exist from the start.

## Run it

```sh
go run .                                  # http://localhost:3000, the warehouse is up after 5 s
WAREHOUSE_DELAY=20s go run .              # a slower warehouse
WAREHOUSE_URL=ftp://warehouse go run .    # a bad URL: the module fails at once
```

Call it while it starts:

```sh
curl -i localhost:3000/health            # 200 ok from the first request
curl -i localhost:3000/stock/green-tea   # 503 with Retry-After, then 200 {"quantity":30,"sku":"green-tea"}
curl -i localhost:3000/stock/kettle      # 404 once the warehouse is up
```

`go test ./...` checks both paths: a warehouse that comes up late, and a bad URL that fails for good.

## Read next

[05 · Production](../05-production/README.md): security headers, limits and a clean shutdown.
