# Log pipe

*Package `logpipe` is the output side of the built-in logger: a bounded, batched queue in front of one output (stdout), written by at most one goroutine, that drops Debug and Info first under overload and reports what it dropped.*

- [Log pipe](#log-pipe)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [Constants](#constants)
  - [`Level`](#level)
  - [`AccessLine` Fields](#accessline-fields)
    - [Msg](#msg)
    - [Attrs](#attrs)
  - [Functions](#functions)
    - [Stdout](#stdout)
    - [New](#new)
    - [SetStdout](#setstdout)
  - [`Sink` Methods](#sink-methods)
    - [Write](#write)
    - [StartAsync](#startasync)
    - [StopAsync](#stopasync)
    - [Flush](#flush)
  - [Limits](#limits)
  - [Concurrency](#concurrency)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

The package is internal: only code in the Ginject module can import it. `log` encodes records and
writes them here; `core` switches the stdout sink to asynchronous while an app serves and flushes it
at shutdown; `accesslog` passes its line to the built-in logger as an `AccessLine`.

## Key Features
- Two modes: synchronous (a new sink: the record is written before `Write` returns) and asynchronous (between `StartAsync` and the matching `StopAsync`: `Write` only appends to the queue).
- Bounded memory: at most 1 MiB queued per output, records cut at 64 KiB, big buffers released when the queue runs empty.
- No idle cost: no goroutine and no timer while the queue is empty; the writer goroutine exists only while there is something to write.
- Drop by level under overload: Debug and Info first, Warn and Error after one bounded wait, Fatal never; every drop is counted and reported.
- Records are never interleaved and keep the order in which they were accepted.

## Usage

```go
package main

import (
	"os"

	"github.com/dangduoc08/ginject/internal/logpipe"
)

func report(dropped [4]uint64) []byte {
	return []byte("records were dropped\n")
}

func main() {
	s := logpipe.New(os.Stdout)
	s.Write(logpipe.Info, []byte("written before Write returns\n"), report)

	s.StartAsync()
	s.Write(logpipe.Info, []byte("queued, written by the writer goroutine\n"), report)
	s.StopAsync(logpipe.FlushTimeout)
}
```

## Constants

| Name | Value | Meaning |
|---|---|---|
| `FlushTimeout` | 2 s | How long `Fatal`, shutdown and the startup banner wait for the stdout sink |
| `maxRecord` (unexported) | 64 KiB | Longest record; longer ones are cut |
| `queueLimit` (unexported) | 1 MiB | Most bytes queued and not yet written |
| `softLimit` (unexported) | 80 % of `queueLimit` | Above it Debug and Info are dropped |
| `batchSize` (unexported) | 64 KiB | Most bytes per `Write` of the output, unless one record is larger |
| `fullWait` (unexported) | 100 ms | Longest wait of a Warn or Error for room |
| `reportInterval` (unexported) | 1 s | Least time between two drop reports while the queue is busy |

## `Level`

`type Level uint8` with `Debug`, `Info`, `Warn`, `Error` and `Fatal`, in that order. The first four index the drop counts.

## `AccessLine` Fields

The access line `accesslog` hands to the built-in logger (through an unexported interface): the logger writes it as an Info record and the Pretty format colors its durations.

### Msg
Type: `string`

Default: `""`

Required: `true`

The record's message (the request's path).

### Attrs
Type: `[]slog.Attr`

Default: `nil`

Required: `false`

The pairs of the line, every value a string or an int. The logger may replace a masked value in place.

## Functions

### Stdout

Returns the process-wide sink of `os.Stdout`, shared by every logger from `log.NewLog`.

#### Returns
- 1st value: `*Sink`

#### Rules
- Every call returns the same sink.

### New

Returns a new, private sink over `w`, synchronous.

#### Parameters
- 1st parameter: `io.Writer`

- Description: The output

#### Returns
- 1st value: `*Sink`

#### Usage

```go
s := logpipe.New(&buf)
```

#### Rules
- Every call returns a new sink.

### SetStdout

Points the stdout sink at `w` and makes it synchronous (async count 0), for the tests of `log`, `accesslog` and `core`. It flushes first (at most `FlushTimeout`).

#### Parameters
- 1st parameter: `io.Writer`

- Description: The writer the stdout sink writes to from now on

#### Returns
- 1st value: `func()`

- Description: Restores the previous writer and mode (after a flush)

#### Usage

```go
restore := logpipe.SetStdout(&buf)
defer restore()
```

#### Rules
- Records written after the call reach `w`; after `restore` they reach the previous writer again.

## `Sink` Methods

### Write

`Write(level Level, record []byte, report func(dropped [4]uint64) []byte)` copies the record into the queue (the caller may reuse `record` at once). `report` encodes the `LogDropped` record; the sink calls it, outside its lock, with the counts per level since the last report, using the `report` of the record dropped last.

#### Rules
- Synchronous mode: `Write` returns after the record was passed to the output; nothing is dropped and no goroutine is started; concurrent calls write one at a time and lose nothing. An error or a panic of the output never reaches the caller.
- Asynchronous mode: `Write` appends and returns without waiting for the output, except for the Warn and Error wait below.
- One goroutine at most writes to the output at any time; it passes whole records only, at most 64 KiB per `Write` call (one record when the record alone is larger than what is left), and ends when the queue is empty.
- Debug and Info: a record that would take the queue past `softLimit` is dropped and counted, without waiting; one that makes it exactly `softLimit` is accepted.
- Warn and Error: accepted while they fit in `queueLimit`. Otherwise the caller waits for room for at most 100 ms, then the record is dropped and counted. After one wait timed out, Warn and Error that find no room are dropped at once, until the output completes a `Write`.
- Fatal is always accepted, without waiting, even when the queue is full; in synchronous mode too, so `Fatal` never hangs on a stuck output.
- Drops are reported once: the writer writes the report after a batch when `reportInterval` has passed since the last one, when the queue runs empty, and before `Flush` returns. The report bypasses the limits and is written where the writer is (after the queued records), with no marker where the drop happened. No report without a drop. For each level, records written plus counts reported equal records submitted.
- An error of the output discards that batch (no retry, one `Write` per batch); a panic of the output is recovered and treated the same; the writer goes on.
- A record over 64 KiB is cut on a UTF-8 boundary to at most 64 KiB and ends with `...[TRUNCATED]` and a line feed (a color reset first when the record holds an ANSI escape); 64 KiB and 64 KiB − 1 are not cut.
- With the output blocked forever: no Debug or Info call waits, a stuck output costs the Warn and Error callers one wait, queued bytes never pass `queueLimit` plus one record, and exactly one goroutine is blocked.
- Records of one goroutine are written in call order; a call that returned before another started is written first.

### StartAsync

Switches the sink to asynchronous. Counted: two `StartAsync` need two `StopAsync`. It starts no goroutine.

### StopAsync

`StopAsync(timeout time.Duration) bool` flushes (see `Flush`), then undoes one `StartAsync`; after the last one the sink is synchronous again. It returns what `Flush` returned.

#### Rules
- After one `StopAsync` of two `StartAsync` the sink is still asynchronous; after the second, every `Write` returns after its record was written.

### Flush

`Flush(timeout time.Duration) bool` returns `true` once every record accepted before the call was passed to the output and pending drop counts were reported, `false` after `timeout`.

#### Rules
- It never drops anything: records still queued when it times out are written later.
- An empty sink (never used, or asynchronous with nothing queued) returns `true` at once.

## Limits

| Part | Idle | Worst case |
|---|---|---|
| A sink never used | about 300 B, no goroutine, no timer | - |
| A sink after use, queue empty | buffers of at most 64 KiB, no goroutine, no timer | two buffers of about 1 MiB, one goroutine |

Measured with a stuck output and 200,000 records of 200 B in all four levels: about 1.2 MB of heap, one goroutine, about 20 ns per call (the records past the limits are dropped). Once the output writes again and the queue empties, the big buffers are released.

## Concurrency

- One mutex guards the buffers, the record offsets, the byte counters, the drop counts, the mode count and the writer and "stuck" flags. A caller holds it for an append; the writer holds it to swap buffers and to account a finished batch, never during the output's `Write`.
- Waiters (a full Warn or Error, `Flush`, a synchronous caller behind the writer) wait on a channel the writer closes after each batch, with a timer where the rule has one. No `sync.Cond`.
- The report function runs outside the lock.

## Performance notes

- **Two swapped buffers.** Callers append to one buffer while the writer writes the other; at the end of a batch they swap under the lock. A record is copied once, from the caller's encoder into the queue. Growth is by doubling, capped at `queueLimit` (one record more for Fatal).
- **Batches cut on record ends.** The offsets of record ends are kept next to the buffer, so a batch is a sub-slice: no copy for the write.
- **No goroutine while idle.** The writer is started by the `Write` that makes the queue non-empty (about 0.3 µs, once per burst) and ends when the queue is empty, after releasing buffers over 64 KiB. A long-lived goroutine would cost a wakeup source and a stack on a small machine.

## Benchmarks

Machine-dependent, captured when this README was written (darwin/amd64, Intel Core i7-9750H, a 120-byte record, a discarding output unless the name says otherwise):

```sh
go test -run '^$' -bench . -benchmem ./internal/logpipe/
```

```console
BenchmarkSink_Write_Async_1-12                	17869485	        68.85 ns/op	     228 B/op	       0 allocs/op
BenchmarkSink_Write_Async_12-12               	10810381	       109.6 ns/op	     262 B/op	       0 allocs/op
BenchmarkSink_Write_Sync_1-12                 	19077236	        60.63 ns/op	       0 B/op	       0 allocs/op
BenchmarkSink_Write_Async_SlowOutput_1-12     	76494364	        14.30 ns/op	       0 B/op	       0 allocs/op
BenchmarkSink_Write_Async_SlowOutput_12-12    	19016257	        62.70 ns/op	       0 B/op	       0 allocs/op
```

The bytes per operation of the asynchronous benchmarks are the buffers grown again after the writer emptied the queue and released them (amortized, no allocation per call). With a slow output almost every record is dropped above `softLimit`, which is why those calls are cheap.
