# HTTP report 0013 baseline: before App options

Measured on 2026-11-20 at `fb7220d`, before roadmap 0013 changes how the pre-routing chain is built.
Step 11 of roadmap 0013 compares against this report. Go 1.27.4, Intel Core i7-9750H (12 threads),
macOS. **The machine was busy** (load average up to 9.4 at the end of the load test), so treat
differences under about 10 % as noise.

## Load test against other frameworks

`bench/load.sh`, `wrk`, 64 connections, 8 s per run, log on for every framework (logger parity),
three rounds with the framework order reversed in the second; medians of requests per second.

| GOMAXPROCS | Scenario | Owlky Go | Gin | Echo | Chi | Fiber | Owlky ÷ best of Gin/Echo/Chi | Owlky ÷ Fiber |
|---|---|---|---|---|---|---|---|---|
| 1 | Static | 25.4k | 25.2k | 22.1k | 25.3k | 42.8k | **100 %** | 59 % |
| 1 | Chain | 18.6k | 22.0k | 20.8k | 20.9k | 30.7k | **84 %** | 61 % |
| 2 | Static | 45.7k | 41.9k | 40.3k | 36.0k | 64.9k | **109 %** | 71 % |
| 2 | Chain | 37.8k | 35.1k | 35.9k | 38.7k | 54.3k | **98 %** | 70 % |
| 12 | Static | 71.7k | 30.1k | 50.4k | 51.7k | 61.0k | **139 %** | **118 %** |
| 12 | Chain | 56.2k | 30.9k | 44.5k | 49.2k | 59.3k | **114 %** | 95 % |

The same picture as `http-0006.md`: level with the `net/http` group on one or two cores (behind on
Chain at one core, where Owlky's access line is about three times longer), ahead at twelve, Fiber
(`fasthttp`) ahead on small machines. p99 values on this run are too noisy to compare (one-core runs
show 9 to 25 ms for every `net/http` framework); they are in the raw table only.

## Hot path (`bench/`, in process)

| Scenario | Time | Bytes | Allocations |
|---|---|---|---|
| Static | 1.21 µs | 721 B | 8 |
| Param | 2.72 µs | 1540 B | 17 |
| Query | 3.81 µs | 1927 B | 27 |
| JSONBody | 5.39 µs | 2443 B | 33 |
| Chain | 3.28 µs | 1839 B | 24 |
| Exception | 1.96 µs | 881 B | 8 |
| NotFound | 0.98 µs | 673 B | 5 |
| Page: JSON list | 10.3 µs | 5666 B | 40 |
| Page: HTML list | 70.2 µs | 17.0 KiB | 447 |

## Cold path

| Measure | Value |
|---|---|
| `Create` with five controllers | 64 µs, 31.4 KiB |
| `Create` with chained providers | 163 µs, 86.4 KiB |
| `Create` with static submodules | 202 µs, 132.4 KiB |
| `Create` with module factories | 275 µs, 14.5 KiB |
| Idle heap of a small app | 13.0 KiB; 38.4 KiB with `EnableRender` and one page |
| Binary of `examples/01-hello` | 16.1 MiB; 11.0 MiB stripped (`-ldflags="-s -w"`) |

Cold-path numbers are new in this report: from roadmap 0013 on, every roadmap's measure step records
`Create` time, idle heap and binary size next to the hot path.

## Rerun

- Load: `FRAMEWORKS="owlky gin echo chi fiber" LOGS=on PROCS="1 2 all" DURATION=8s CONNECTIONS=64 SCENARIOS="Static Chain" ./bench/load.sh`, three rounds, reversed order in round two.
- Hot path: `cd bench && go test -run '^$' -bench 'BenchmarkOwlky' -benchmem -count 3 .`
- Cold path: `go test -run '^$' -bench BenchmarkCreate -benchmem -count 5 ./internal/core/`;
  binary: `go build` of `examples/01-hello`.
