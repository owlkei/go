# Slice

*Package `slice` holds the generic slice helpers and the string parsers that Ginject uses to bind query, param, header, form and body values.*

- [Slice](#slice)
  - [Usage](#usage)
  - [Functions](#functions)
    - [Map](#map)
    - [Filter](#filter)
    - [Get](#get)
    - [StrParse functions](#strparse-functions)
    - [StrParseAny](#strparseany)
    - [Iter](#iter)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

The package is internal: only code in the Ginject module can import it.

## Usage

```go
package main

import (
	"fmt"

	"github.com/dangduoc08/ginject/internal/slice"
)

func main() {
	ids := slice.StrParseInt([]string{"1", "2", "bad"})
	even := slice.Filter(ids, func(el int, i int) bool { return el%2 == 0 })
	first, ok := slice.Get(ids, 0)
	fmt.Println(ids, even, first, ok)
}
```

Output:

```console
[1 2 0] [2 0] 1 true
```

## Functions

Every function here runs on the hot path: `ctx` binds each request value through them.

### Map

Returns a new slice with `cb(el, i)` for each element.

#### Parameters
1. `arr` `[]T`: the input.
2. `cb` `func(el T, i int) U`: the mapping, called once per element in order.

#### Returns
1. `[]U`: the mapped slice, the same length as `arr`.

#### Usage

```go
slice.Map([]string{"a", "b"}, func(el string, i int) string { return el + el }) // ["aa" "bb"]
```

#### Rules
- `cb` is called in index order.
- A nil or empty input returns a non-nil empty slice.

### Filter

Returns the elements for which `cb` returns `true`, in order.

#### Parameters
1. `arr` `[]T`: the input.
2. `cb` `func(el T, i int) bool`: the predicate, which also receives the index.

#### Returns
1. `[]T`: the kept elements.

#### Usage

```go
slice.Filter([]int{10, 11, 12, 13}, func(el int, i int) bool { return i%2 == 1 }) // [11 13]
```

#### Rules
- The order of the kept elements is the input order.
- A nil input, or an input where nothing matches, returns an empty slice.

### Get

Returns the element at index `i` and whether `i` is in range.

#### Parameters
1. `arr` `[]T`: the input.
2. `i` `int`: the index.

#### Returns
1. `T`: `arr[i]`, or the zero value when `i` is out of range.
2. `bool`: `true` when `0 <= i < len(arr)`.

#### Usage

```go
name, ok := slice.Get([]string{"John Doe"}, 0) // "John Doe", true
```

#### Rules
- An index past the end, a negative index and a nil slice return the zero value and `false`.

### StrParse functions

`StrParseBool`, `StrParseInt`, `StrParseInt8`, `StrParseInt16`, `StrParseInt32`, `StrParseInt64`, `StrParseUint`, `StrParseUint8`, `StrParseUint16`, `StrParseUint32`, `StrParseUint64`, `StrParseFloat32`, `StrParseFloat64`, `StrParseComplex64` and `StrParseComplex128` parse each string into the named type.

#### Parameters
1. `arr` `[]string`: the strings to parse.

#### Returns
1. `[]T`: one value per input, where `T` is the named type.

#### Usage

```go
slice.StrParseInt8([]string{"127", "300"})  // [127 0]
slice.StrParseComplex64([]string{"1 + 2i"}) // [(1+2i)]
```

#### Rules
- Each function returns one element per input. A nil or empty input returns an empty slice.
- An element that does not parse becomes the zero value: `"bad"` and `""` give `0` or `false`.
- An element out of range for the type becomes the zero value: `StrParseInt8([]string{"300"})` → `[0]`, `StrParseUint([]string{"-1"})` → `[0]`, `StrParseFloat32([]string{"1e40"})` → `[0]`, `StrParseUint64` of `"18446744073709551616"` → `[0]`.
- The complex parsers ignore spaces: `"1 + 2i"` → `(1+2i)`.

### StrParseAny

Boxes each string into an `any`.

#### Parameters
1. `arr` `[]string`: the strings.

#### Returns
1. `[]any`: the same strings as `any` values.

#### Usage

```go
slice.StrParseAny([]string{"a", "1"}) // ["a" "1"]
```

#### Rules
- Each string is kept as a `string`. A nil input returns an empty slice.

### Iter

Walks a nested `[]any` depth first.

#### Parameters
1. `arr` `[]any`: the slice to walk.
2. `dimensions` `int`: the dimension reported for the elements of `arr`.
3. `cb` `func(any, int)`: called with each element and its dimension.

#### Usage

```go
slice.Iter([]any{1, []any{2, 3}, 4}, 2, func(el any, d int) {
	fmt.Printf("%v@%d ", el, d)
})
```

#### Rules
- A nested `[]any` is visited first, with `dimensions-1`, and then `cb` is called on the nested slice itself with `dimensions`: the example prints `1@2 2@1 3@1 [2 3]@2 4@2`.
- Leaves are visited depth first, in order.
- A nil or empty slice never calls `cb`.

## Performance notes

- Each `StrParse` function allocates its result once, at the input length. Rewriting them as plain loops instead of `Map` with a closure was measured and gave no gain, so they keep `Map`.
- `Map` and `Filter` allocate once: `Filter` reserves the full input length.

## Benchmarks

Results depend on the machine. They were captured when this README was generated, with `go test -run=^$ -bench=. -benchmem ./internal/slice` (1,024 elements, and one element for `StrParseInt_One`):

```console
goos: darwin
goarch: amd64
cpu: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
BenchmarkMap-12                	  556141	      1918 ns/op	    8192 B/op	       1 allocs/op
BenchmarkFilter-12             	  342555	      3481 ns/op	   18432 B/op	       1 allocs/op
BenchmarkStrParseInt-12        	  148693	      8083 ns/op	    8192 B/op	       1 allocs/op
BenchmarkStrParseInt_One-12    	157419973	         7.620 ns/op	       0 B/op	       0 allocs/op
```
