# Num

*Package `num` converts a JSON number (a `float64`) into the Go type of a struct field, with the same result on every platform.*

- [Num](#num)
  - [Usage](#usage)
  - [Functions](#functions)
    - [F64ToAnyNumOK](#f64toanynumok)
    - [F64ToAnyNum](#f64toanynum)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

The package is internal: only code in the Owlkei Go module can import it.

## Usage

```go
package main

import (
	"fmt"
	"reflect"

	"go.owlkei.com/owlkei/internal/num"
)

func main() {
	v, ok := num.F64ToAnyNumOK(300, reflect.Int8)
	fmt.Println(v, ok)
	fmt.Println(num.F64ToAnyNum(-1.9, reflect.Int))
}
```

Output:

```console
127 false
-1
```

## Functions

Both functions run on the hot path: `ctx` binding calls them for every numeric field of a JSON body.

### F64ToAnyNumOK

Converts `f64` to the Go type of `kind` and reports whether the value fits that type.

#### Parameters
1. `f64` `float64`: the number, such as a value decoded from a client's JSON body.
2. `kind` `reflect.Kind`: the target kind, from `Int` to `Complex128`.

#### Returns
1. `any`: the value, saturated to the type's range, with the Go type of `kind` (`int8` for `reflect.Int8`).
2. `bool`: `true` when the value fits. A fraction that truncates into range fits.

#### Usage

```go
v, ok := num.F64ToAnyNumOK(127.9, reflect.Int8) // int8(127), true
v, ok = num.F64ToAnyNumOK(256, reflect.Uint8)   // uint8(255), false
```

#### Rules
- Values in range convert with `ok == true`, at the edges too: `127` and `-128` for `Int8`, `4294967295` for `Uint32`, `0` for `Uint8`.
- A value above the maximum returns the maximum and `false`: `128` → `int8(127)`, `9223372036854775808` → `math.MaxInt64`, `+Inf` → `int16(32767)`.
- A value below the minimum returns the minimum and `false`: `-129` → `int8(-128)`, `-1e20` → `math.MinInt64`.
- A negative value for an unsigned kind returns `0` and `false`.
- `Float32` saturates at `±math.MaxFloat32` with `false`: `1e300` → `math.MaxFloat32`.
- NaN for an integer kind returns `0` and `false`.
- `Float64` and `Complex128` always fit. NaN and ±Inf fit the float kinds, which can hold them.
- An unknown kind returns `0` (an `int`) and `false`.
- The value always equals what `F64ToAnyNum` returns.

### F64ToAnyNum

Converts `f64` to the Go type of `kind`, saturated to the type's range. It is `F64ToAnyNumOK` without the flag.

#### Parameters
1. `f64` `float64`: the number.
2. `kind` `reflect.Kind`: the target kind.

#### Returns
1. `any`: the converted value with the Go type of `kind`, or `0` (an `int`) for an unknown kind.

#### Usage

```go
num.F64ToAnyNum(300, reflect.Int8)  // int8(127)
num.F64ToAnyNum(-5, reflect.Uint)   // uint(0)
```

#### Rules
- Signed kinds truncate toward zero: `1.9` → `1`, `-1.9` → `-1`, `-0.99` → `0`.
- Signed kinds saturate: `300` → `int8(127)`, `70000` → `int16(32767)`, `5e9` → `int32(2147483647)`, `1e20` → `math.MaxInt`; `-129` → `int8(-128)`, `-Inf` → `int8(-128)`.
- Unsigned kinds give `0` for a negative value, `0`, `-Inf` and NaN, and saturate above the maximum: `256` → `uint8(255)`, `1e30` → `math.MaxUint64`.
- Signed kinds give `0` for NaN.
- `Float32` saturates at `±math.MaxFloat32` and keeps NaN. `Float64` returns the value unchanged.
- `Complex64` and `Complex128` put the value in the real part with the `Float32` or `Float64` rule, and `0` in the imaginary part.
- The dynamic type of the result is the Go type of `kind`, even for saturated values.
- `reflect.String`, `Bool`, `Invalid`, `Slice` and every other kind return `0` (an `int`).

## Performance notes

- The conversion checks the range before converting, because a float to integer conversion that does not fit has an implementation-defined result in Go (amd64 and arm64 differ). The checks cost about 2 ns per call.
- Boxing the result into `any` allocates for values that are not small integers (8 bytes for a `float64`). `ctx` keeps the boxed value, so the allocation stays.

## Benchmarks

Results depend on the machine. They were captured when this README was generated, with `go test -run=^$ -bench=. -benchmem ./internal/num`:

```console
goos: darwin
goarch: amd64
cpu: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
BenchmarkF64ToAnyNum_Int-12          	177697867	         6.742 ns/op	       0 B/op	       0 allocs/op
BenchmarkF64ToAnyNum_Uint-12         	154828099	         7.751 ns/op	       0 B/op	       0 allocs/op
BenchmarkF64ToAnyNum_UintNeg-12      	213747920	         5.597 ns/op	       0 B/op	       0 allocs/op
BenchmarkF64ToAnyNum_Float64-12      	75739046	        13.64 ns/op	       8 B/op	       1 allocs/op
BenchmarkF64ToAnyNum_Saturated-12    	303208410	         3.959 ns/op	       0 B/op	       0 allocs/op
```
