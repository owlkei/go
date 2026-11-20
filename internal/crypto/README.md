# Crypto

*Package `crypto` generates RFC 4122 version 4 UUIDs from `crypto/rand`.*

- [Crypto](#crypto)
  - [Usage](#usage)
  - [Functions](#functions)
    - [UUID](#uuid)
  - [Security](#security)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

The package is internal: only code in the Owlkei Go module can import it. Request, connection,
message and trace IDs come from `internal/tracecontext`, not from here.

## Usage

```go
package main

import (
	"fmt"

	"go.owlkei.com/owlkei/internal/crypto"
)

func main() {
	id, err := crypto.UUID()
	if err != nil {
		panic(err)
	}
	fmt.Println(id)
}
```

## Functions

| Function | Source | Use |
|---|---|---|
| `UUID` | `crypto/rand` | broker IDs, tokens, anything a client must not guess |

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

## Security

`UUID` reads 122 random bits from `crypto/rand` per call, so it is fit for tokens and session IDs.

## Performance notes

- `UUID` makes one `crypto/rand` call per ID (about 360 ns, slower under parallel load) and allocates once (the string). It is not on the request path.

## Benchmarks

Results depend on the machine. They were captured when this README was generated, with `go test -run=^$ -bench=. -benchmem ./internal/crypto`:

```console
goos: darwin
goarch: amd64
cpu: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
BenchmarkUUID-12             	 3310230	       357.6 ns/op	      48 B/op	       1 allocs/op
BenchmarkUUID_Parallel-12    	 1314070	       913.2 ns/op	      48 B/op	       1 allocs/op
```
