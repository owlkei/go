# Bench

*`bench` measures Owlky Go's HTTP request cycle and compares it with `net/http`'s `ServeMux`, Gin,
Echo, Chi and Fiber on the same scenarios. It is a separate Go module, so the other frameworks
never become dependencies of Owlky Go.*

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
make bench                      # Owlky Go only, 5 runs
make bench-compare              # all six frameworks, 5 runs
make bench-compare BENCH_COUNT=10
```

Compare two runs with `benchstat old.txt new.txt`.

### Load test

The load test needs [`wrk`](https://github.com/wg/wrk) on your `PATH`. It builds `cmd/server` as a
release binary (`go build -trimpath -ldflags="-s -w"`), serves one framework at a time on `PORT`
(default `18080`) and runs `wrk` for each scenario at each connection count:

```sh
make load
FRAMEWORKS="owlky nethttp gin echo chi fiber" LOGS="on off" PROCS="1 2 all" DURATION=5s CONNECTIONS=64 make load
```

| Variable | Default | Effect |
|---|---|---|
| `FRAMEWORKS` | `owlky` | the servers to measure: `owlky`, `nethttp`, `gin`, `echo`, `chi`, `fiber` |
| `LOGS` | `on` | `on` turns the framework's request logger on, `off` turns it off; `nethttp` has no logger and runs once, as `none` |
| `PROCS` | `all` | the `GOMAXPROCS` values of the server; `all` leaves it unset (every core) |
| `DURATION` | `10s` | how long `wrk` runs per scenario |
| `CONNECTIONS` | `64 256` | the connection counts |
| `SCENARIOS` | `Static Param JSONBody Chain` | the scenarios |
| `THREADS` | `4` | `wrk` threads |
| `LOG_DIR` | `$TMPDIR` | where the server's stdout (the log) and stderr go, one file per framework and log setting |

It prints a Markdown table with requests per second, p50, p99, the count of non-2xx responses, the
requests `wrk` completed, the log lines written during the run, and the server's CPU time per
request. Owlky Go is served through `App.Listen`, its production path; the others through
`http.ListenAndServe` or Fiber's `Listen`.

With the log on, each framework writes one line per request to stdout, which goes to a file on disk:

| Framework | Request logger |
|---|---|
| Owlky Go | the built-in logger in JSON with the access log (`IsAccessLog: true`) |
| Gin | `gin.LoggerWithConfig` with a JSON formatter |
| Echo | `middleware.RequestLoggerWithConfig` writing through `slog`'s JSON handler |
| Chi | `middleware.Logger` (text lines: Chi has no JSON logger) |
| Fiber | `logger.New` with a JSON format |
| net/http | none |

The CPU time is the server process's user plus system time (`getrusage`), read before and after
each run: the server prints it to stderr when it receives `SIGUSR1`.

## How the measurements stay fair

- **Benchmarks** call `ServeHTTP` with one reused request whose body is reset each iteration and a
  `ResponseWriter` that discards the output, so they measure the framework's own work, not request
  parsing or the network.
- **Fiber** runs on fasthttp's `RequestCtx`, so each iteration also calls `RequestCtx.Init` and
  `Response.Reset`, about one allocation of harness work.
- **Recovery** is on everywhere: Owlky Go recovers by default, the others use their recovery
  middleware.
- **Errors** use each framework's own mechanism; `net/http` and Chi have none and write the `400`
  directly.
- **The load test** runs the client and the server on one machine, so absolute numbers are a floor;
  comparisons between frameworks on the same run are fair.
- **Logging:** the benchmarks run every framework without a request logger. The load test compares
  frameworks with their loggers on, and Owlky Go with its log off against net/http, which has no
  logger. The log lines column shows that a logger wrote every line: one that drops lines would
  otherwise look cheap.
- **Reference points:** net/http with no logger is the floor, the fastest a framework on net/http
  can be. The target to beat is the best of Gin, Echo and Chi in the same run, with the same
  `GOMAXPROCS` and their loggers on, not one chosen framework. Fiber is shown for reference only: it
  runs on fasthttp, not net/http (no HTTP/2, no `http.Handler` middlewares), so a goal set against it
  would compare transports rather than frameworks.

## Results

The baseline and the hot spots it points to are in `docs/review/http-baseline.md`; the latest
measurements, with logging on, in `docs/review/http-0005.md`.
