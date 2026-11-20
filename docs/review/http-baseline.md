# HTTP baseline (roadmap 0001)

The numbers every later HTTP roadmap is measured against.

- **Machine:** Intel Core i7-9750H (6 cores, 12 threads), 16 GB, macOS 26.7.3.
- **Go:** 1.27.4. **Commit:** `764824a` (branch `roadmap/cleanup-and-baseline`).
- **Rerun:** `make bench-compare` (Go benchmarks, `BENCH_COUNT=5` by default) and
  `FRAMEWORKS="owlkei nethttp gin echo chi fiber" DURATION=5s make load`.
- **Scenarios** (`bench/scenario.go`, the same on every framework; a parity test checks status and
  body): `Static` (`GET /static`, text), `Param` (`GET /users/:id`, JSON), `Query` (`GET /search?q&page`,
  bound into a struct, JSON), `JSONBody` (`POST /users`, JSON body bound into a struct, 201), `Chain`
  (middleware + guard + interceptor around a JSON handler), `Exception` (the handler fails with 400
through each framework's error mechanism: a panicked exception in Owlkei Go, `echo.NewHTTPError` in
Echo, `fiber.NewError` in Fiber, `AbortWithStatusJSON` in Gin; net/http and Chi have none, so they
write the 400 directly),
  `NotFound` (unknown route, 404).

## Go benchmarks: the framework's own cost

Median of 5 runs, `ServeHTTP` with a reused request and a discarding `ResponseWriter`, so network and
request parsing are excluded.

| Scenario | Owlkei Go | net/http | Gin | Echo | Chi | Fiber | Owlkei Go ÷ net/http |
|---|---|---|---|---|---|---|---|
| Static | 1,672 ns | 120 ns | 176 ns | 351 ns | 458 ns | 544 ns | 13.9× |
| Param | 4,304 ns | 1,542 ns | 1,217 ns | 1,348 ns | 1,947 ns | 1,575 ns | 2.8× |
| Query | 6,292 ns | 2,583 ns | 2,625 ns | 2,712 ns | 2,911 ns | 2,231 ns | 2.4× |
| JSONBody | 9,252 ns | 3,315 ns | 3,552 ns | 3,881 ns | 3,857 ns | 4,417 ns | 2.8× |
| Chain | 6,093 ns | 1,775 ns | 2,156 ns | 2,122 ns | 2,276 ns | 3,371 ns | 3.4× |
| Exception ² | 7,933 ns | 1,302 ns | 1,155 ns | 1,424 ns | 1,544 ns | 1,032 ns | 6.1× |
| NotFound | 3,403 ns | 1,074 ns | 120 ns | 1,522 ns | 584 ns | 1,152 ns | 3.2× |

Allocations per request:

| Scenario | Owlkei Go | net/http | Gin | Echo | Chi | Fiber |
|---|---|---|---|---|---|---|
| Static | 13 (304 B) | 1 | 1 | 3 | 3 | 1 ¹ |
| Param | 27 (1.2 KB) | 9 | 9 | 10 | 12 | 9 |
| Query | 46 (1.7 KB) | 16 | 17 | 17 | 18 | 13 |
| JSONBody | 52 (2.2 KB) | 17 | 18 | 19 | 19 | 14 |
| Chain | 40 (1.7 KB) | 10 | 15 | 14 | 12 | 14 |
| Exception ² | 33 (980 B) | 7 | 8 | 12 | 9 | 3 |
| NotFound | 31 (914 B) | 14 | 0 | 10 | 5 | 4 |

1. Fiber runs on fasthttp's `RequestCtx`; each iteration calls `RequestCtx.Init` and
   `Response.Reset`, so its numbers include about one allocation of harness work (its Static
   allocation is that one).
2. Measured in a second run, after the scenario was made fair (see Scenarios); that run was slower
   overall (Owlkei Go Static 2,441 ns), so compare within the row.

Single runs vary by about ±15 % on this laptop (thermal throttling); compare medians, with
`benchstat`, on the same machine.

## Load test: requests per second over HTTP

`wrk -t4`, 5 s per run, client and server on the same machine (so the kernel's loopback and `wrk`
take a large share of the CPU and compress the differences). p99 on a laptop is noisy; treat it as
an indication only.

| Scenario | Connections | Owlkei Go | net/http | Gin | Echo | Chi | Fiber |
|---|---|---|---|---|---|---|---|
| Static | 64 | 81.8k | 88.1k | 97.6k | 76.6k | 79.1k | 106.3k |
| Param | 64 | 75.1k | 82.3k | 82.3k | 69.2k | 66.1k | 90.4k |
| JSONBody | 64 | 52.9k | 74.3k | 81.9k | 69.5k | 67.7k | 92.2k |
| Chain | 64 | 75.8k | 84.3k | 86.9k | 71.7k | 75.7k | 96.1k |
| Static | 256 | 93.0k | 102.7k | 116.7k | 92.0k | 97.2k | 107.4k |
| Param | 256 | 90.0k | 97.2k | 102.0k | 83.5k | 86.8k | 93.5k |
| JSONBody | 256 | 77.8k | 90.7k | 81.6k | 67.3k | 83.4k | 91.1k |
| Chain | 256 | 82.1k | 86.9k | 92.9k | 86.7k | 92.0k | 97.5k |

Owlkei Go p50 at 64 connections: 509 µs (Static) to 843 µs (JSONBody); every run returned only 2xx.
Owlkei Go is already in the range of Echo and Chi over the network; the 3-14× gap in framework cost
shows up as the JSONBody gap here and will grow on machines where the network is not the
bottleneck.

## Where the time goes

CPU profile of all Owlkei Go scenarios (`-cpuprofile`, cumulative share of samples) and allocation
profile (`-memprofilerate 1`, share of allocated objects). Each line names the roadmap that owns it.

| # | Hot spot | Evidence | Package | Roadmap |
|---|---|---|---|---|
| 1 | Handlers are called through reflection on every request (`reflect.Value.Call`, argument lists built per call) | 17 % CPU (`invokeHTTPHandlerByProviders`), 11 % in `reflect.Value.call`; `reflect.unsafe_New` + `methodReceiver` are 31 % of objects | `core` (`fn.go`) | 0003 (consider moving to 0002: it is the biggest single cost) |
| 2 | A crypto-random UUID is generated for every request (`ctx.SetID` → `crypto.UUID`) | 8 % CPU, 4 % of objects | `ctx`, `internal/crypto` | 0002 |
| 3 | JSON responses: `toJSONBuffer` (`ctx/fn.go:14`) marshals `&data` (a pointer to an `any`) with `encoding/json`, whose `Marshal` copies its internal buffer into a new slice on every call; a string payload also goes through `fmt.Sprintf` | 11 % CPU; the copy is 2.4 % of objects | `ctx` | 0002 |
| 4 | Context initialization (`HTTPContext.Init`, including the ID) | 10 % CPU | `ctx` | 0002 |
| 5 | Response headers through `Header().Set` (canonicalization and a new slice each time) | 8 % of objects | `ctx`, `core` | 0002 |
| 6 | Closures allocated per request in `handleRequest` | 8.7 % of objects (2.6 per Static request) | `core` (`http.go`) | 0003 |
| 7 | `http.MaxBytesReader` wraps every body, including bodiless `GET`s | 4 % of objects, 1 per request | `core` (`app.go`) | 0003 |
| 8 | 404s build an exception, a map and JSON per request (31 allocs, 3.4 µs; Gin: 0 allocs). A client can trigger it at will, so it counts as hot | `returnNotFound` 3.4 % of objects | `core`, `exception` | 0003 |
| 9 | Exceptions cost 6-8 µs (panic, recover, filter chain, map, JSON), against 1.0-1.4 µs for the error mechanisms of Gin, Echo and Fiber | `RunHTTPCatchChain`, `globalHTTPExceptionFilter.Catch` | `exception`, `common`, `core` | 0002 / 0003 |
| 10 | Query and body binding walk struct fields with reflection per request (`BindStruct`, `BindStrArr`); query parsed by `url.ParseQuery` | `BindStruct`, `BindStrArr`, `url.parseQuery` in the top objects | `ctx` (`binding.go`) | 0002 |
| 11 | Interceptors allocate closures per request (`BuildHTTPInterceptMiddleware`) | 1.4 % of objects | `common` | 0002 |
| 12 | Garbage collection from the allocation rate | 11 % CPU in `gcBgMarkWorker` | all of the above | falls out of 1-11 |

## Notes for the next roadmaps

- The `Static` gap (13.9×) is the purest measure of per-request overhead: everything except the
  handler's two bytes is framework cost. Hot spots 1, 2, 4, 5, 6 and 7 are all on it.
- Targets are not fixed yet. A reasonable first goal for 0002 + 0003: Static under 500 ns and
  under 4 allocations, every scenario within 1.5× of net/http.
- Found while removing gRPC (step 1): `EnableDevtool` without `EnableVersioning` panics once a route
  exists (the builder dereferences a nil `versioning`). For roadmap 0003.
