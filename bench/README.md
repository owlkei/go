# Bench

*`bench` measures Ginject's HTTP request cycle and compares it with `net/http`'s `ServeMux`, Gin,
Echo, Chi and Fiber on the same scenarios. It is a separate Go module, so the other frameworks
never become dependencies of Ginject.*

- [Bench](#bench)
  - [Scenarios](#scenarios)
  - [Usage](#usage)
    - [Go benchmarks](#go-benchmarks)
    - [Load test](#load-test)
  - [How the measurements stay fair](#how-the-measurements-stay-fair)
  - [Results](#results)

## Scenarios

Every framework implements the routes in `scenario.go`, and `scenario_test.go` checks that each one
returns the expected status and body:

| Scenario | Request | Response |
|---|---|---|
| `Static` | `GET /static` | `200`, text `ok` |
| `Param` | `GET /users/42` | `200`, JSON with the path parameter |
| `Query` | `GET /search?q=golang&page=3` | `200`, JSON with the query bound into a struct |
| `JSONBody` | `POST /users` with `{"name":"Ada","age":36}` | `201`, JSON with the bound body |
| `Chain` | `GET /chain` with `Authorization: Bearer bench` | `200`, a middleware sets a header, a guard checks the token, the handler's data is wrapped in `{"data": …}` |
| `Exception` | `GET /error` | `400` through the framework's error mechanism |
| `NotFound` | `GET /missing` | `404` |

## Usage

Run the targets from the repository root.

### Go benchmarks

```sh
make bench                      # Ginject only, 5 runs
make bench-compare              # all six frameworks, 5 runs
make bench-compare BENCH_COUNT=10
```

Compare two runs with `benchstat old.txt new.txt`.

### Load test

The load test needs [`wrk`](https://github.com/wg/wrk) on your `PATH`. It builds `cmd/server`,
serves one framework at a time on `PORT` (default `18080`) and runs `wrk` for `Static`, `Param`,
`JSONBody` and `Chain` at each connection count:

```sh
make load
FRAMEWORKS="ginject nethttp gin echo chi fiber" DURATION=10s CONNECTIONS="64 256" make load
```

It prints a Markdown table with requests per second, p50, p99 and the count of non-2xx responses.
Ginject is served through `App.Listen`, its production path; the others through
`http.ListenAndServe` or Fiber's `Listen`.

## How the measurements stay fair

- **Benchmarks** call `ServeHTTP` with one reused request whose body is reset each iteration and a
  `ResponseWriter` that discards the output, so they measure the framework's own work, not request
  parsing or the network.
- **Fiber** runs on fasthttp's `RequestCtx`, so each iteration also calls `RequestCtx.Init` and
  `Response.Reset`, about one allocation of harness work.
- **Recovery** is on everywhere: Ginject recovers by default, the others use their recovery
  middleware.
- **Errors** use each framework's own mechanism; `net/http` and Chi have none and write the `400`
  directly.
- **The load test** runs the client and the server on one machine, so absolute numbers are a floor;
  comparisons between frameworks on the same run are fair.

## Results

The baseline and the hot spots it points to are in `docs/review/http-baseline.md`.
