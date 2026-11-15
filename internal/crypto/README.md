# Crypto

*Package `crypto` generates identifiers: RFC 4122 version 4 UUIDs from `crypto/rand`, and fast random hex strings for request correlation IDs.*

- [Crypto](#crypto)
  - [Usage](#usage)
  - [Functions](#functions)
    - [UUID](#uuid)
    - [RandomHex](#randomhex)
    - [AppendRandomHex](#appendrandomhex)
  - [Security](#security)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

The package is internal: only code in the Ginject module can import it.

## Usage

```go
package main

import (
	"fmt"

	"github.com/dangduoc08/ginject/internal/crypto"
)

func main() {
	id, err := crypto.UUID()
	if err != nil {
		panic(err)
	}
	fmt.Println(id)
	fmt.Println(crypto.RandomHex(8))

	buf := make([]byte, 0, 16)
	buf = crypto.AppendRandomHex(buf, 16)
	fmt.Println(string(buf))
}
```

## Functions

| Function | Source | Use |
|---|---|---|
| `UUID` | `crypto/rand` | WebSocket connection IDs, broker IDs, anything secret |
| `RandomHex` | `math/rand/v2` | request correlation IDs |
| `AppendRandomHex` | `math/rand/v2` | the same, into a buffer the caller owns |

### UUID

Returns a random RFC 4122 version 4 UUID.

#### Returns
1. `string`: 36 characters in the form `8-4-4-4-12`, lower-case hex.
2. `error`: the error of `crypto/rand`, if any.

#### Usage

```go
id, err := crypto.UUID()
```

#### Rules
- The result has 36 characters with dashes at indexes 8, 13, 18 and 23, and lower-case hex elsewhere.
- The version nibble (index 14) is `4`. The variant nibble (index 19) is one of `8`, `9`, `a`, `b`.
- The error is nil, and 1,000 calls give 1,000 distinct values.

### RandomHex

Returns `n` random lower-case hex characters.

#### Parameters
1. `n` `int`: the number of characters.

#### Returns
1. `string`: `n` characters in `0-9a-f`, or `""` when `n <= 0`.

#### Usage

```go
requestID := crypto.RandomHex(16)
```

#### Rules
- The result has exactly `n` characters (tested for 1, 8, 16, 17 and 33), each in `0-9a-f`.
- `0` and negative lengths return `""`.
- 10,000 calls of `RandomHex(16)` give 10,000 distinct values, and every hex digit appears at every position.
- It is safe for concurrent use.

### AppendRandomHex

Appends `n` random lower-case hex characters to `dst`.

#### Parameters
1. `dst` `[]byte`: the buffer to append to.
2. `n` `int`: the number of characters.

#### Returns
1. `[]byte`: `dst` with `n` hex characters appended.

#### Usage

```go
buf := make([]byte, 0, 16)
buf = crypto.AppendRandomHex(buf[:0], 16)
```

#### Rules
- Exactly `n` characters in `0-9a-f` are appended. The existing content of `dst` is unchanged.
- `0` and negative lengths return `dst` unchanged. `AppendRandomHex(nil, 0)` returns an empty slice.
- With enough capacity in `dst`, it allocates nothing.
- It is safe for concurrent use.

## Security

`RandomHex` and `AppendRandomHex` are for correlation IDs, **not for secrets**. They read the top-level generator of `math/rand/v2` (ChaCha8, seeded by the operating system), which is unpredictable in practice but makes no promise about it. Tokens, session IDs and anything a client must not guess use `UUID`, which reads `crypto/rand`.

An 8-character ID holds 32 bits: two requests share one with a probability of 50 % after about 77,000 requests. `RandomHex(16)` costs about the same.

## Performance notes

- `RandomHex` and `AppendRandomHex` take 16 hex digits from each 64-bit random number. The top-level `math/rand/v2` functions keep their state per thread, so they need no lock and no system call. That makes a request ID about 20 times faster than `UUID`, and about 250 times faster under parallel load.
- `RandomHex` allocates once (the string) when the result escapes. `AppendRandomHex` allocates nothing when `dst` has room.
- `UUID` makes a `crypto/rand` call per ID (about 400 ns, slower under parallel load). It runs once per WebSocket connection, not per request.

## Benchmarks

Results depend on the machine. They were captured when this README was generated, with `go test -run=^$ -bench=. -benchmem ./internal/crypto`:

```console
goos: darwin
goarch: amd64
cpu: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
BenchmarkUUID-12                  	 2763403	       430.9 ns/op	      48 B/op	       1 allocs/op
BenchmarkUUID_Parallel-12         	 1000000	      1081 ns/op	      48 B/op	       1 allocs/op
BenchmarkRandomHex-12             	53644411	        22.41 ns/op	       0 B/op	       0 allocs/op
BenchmarkRandomHex_Parallel-12    	321081697	         4.023 ns/op	       0 B/op	       0 allocs/op
BenchmarkAppendRandomHex-12       	58313593	        20.44 ns/op	       0 B/op	       0 allocs/op
```

The benchmarks of `RandomHex` show no allocation because the result does not escape there.
