# Str

*Package `str` holds the string helpers that Ginject runs on request paths: enclosing a route path in separators, collapsing repeated bytes and removing white space.*

- [Str](#str)
  - [Usage](#usage)
  - [Functions](#functions)
    - [Enclose](#enclose)
    - [RemoveDup](#removedup)
    - [RemoveSpace](#removespace)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

The package is internal: only code in the Ginject module can import it.

## Usage

```go
package main

import (
	"fmt"

	"github.com/dangduoc08/ginject/internal/str"
)

func main() {
	fmt.Println(str.Enclose(" //users//profile ", '/'))
	fmt.Println(str.RemoveDup("/static//css///app.css", '/'))
	fmt.Println(str.RemoveSpace("my callback"))
}
```

Output:

```console
/users/profile/
/static/css/app.css
mycallback
```

## Functions

| Function | Use | Path |
|---|---|---|
| `Enclose` | normalizes a route path: one separator at each end, no repeated separators or stars | hot (WebSocket path check); cold (route registration) |
| `RemoveDup` | collapses runs of one byte | hot (static file paths) |
| `RemoveSpace` | removes Unicode white space | warm (JSONP callback) |

### Enclose

Returns `s` with ASCII white space removed, runs of `sep` and runs of `*` collapsed, and exactly one `sep` at the start and at the end.

#### Parameters
1. `s` `string`: the input, such as a route path.
2. `sep` `byte`: the separator, such as `'/'`.

#### Returns
1. `string`: the enclosed string, or `""` when the input is empty or only white space.

#### Usage

```go
str.Enclose("users", '/')      // "/users/"
str.Enclose("/a/**/b/", '/')   // "/a/*/b/"
str.Enclose("foo..bar", '.')   // ".foo.bar."
```

#### Rules
- `""` and a string of only ASCII white space (space, `\t`, `\n`, `\v`, `\f`, `\r`) return `""`.
- A separator is added at the start and at the end when missing: `"users"` → `"/users/"`, `"*"` → `"/*/"`.
- Runs of `sep` collapse to one: `"/a//b///c/"` → `"/a/b/c/"`, `"//"` → `"/"`.
- Runs of `*` collapse to one: `"/a/***/b/"` → `"/a/*/b/"`.
- ASCII white space anywhere is removed: `"/us ers/"` → `"/users/"`.
- Every other byte is kept, multi-byte UTF-8 and invalid bytes included: `"/日本//語/"` → `"/日本/語/"`.
- An input already in that form is returned as is, with no allocation.
- The fast path returns the same result as the full rewrite for any input.

### RemoveDup

Collapses every run of the byte `c` to a single `c`. Every other byte is kept unchanged, whether the input is valid UTF-8 or not.

#### Parameters
1. `s` `string`: the input, such as a URL path.
2. `c` `byte`: the byte whose runs collapse.

#### Returns
1. `string`: the input without repeated `c`.

#### Usage

```go
str.RemoveDup("/**/school**/***", '*') // "/*/school*/*"
str.RemoveDup("a\xff//b", '/')         // "a\xff/b"
```

#### Rules
- Runs of `c` collapse to one: `"//a///b////"` → `"/a/b/"`.
- Runs of other bytes are kept: `RemoveDup("aabbcc", 'b')` → `"aabcc"`.
- Invalid UTF-8 bytes are kept: `"\xff\xff//\xc3"` → `"\xff\xff/\xc3"`.
- `""` returns `""`.
- An input without two `c` in a row is returned as is, with no allocation.
- The fast path returns the same result as the full rewrite for any input.

### RemoveSpace

Removes every Unicode white space rune (`unicode.IsSpace`).

#### Parameters
1. `s` `string`: the input.

#### Returns
1. `string`: the input without white space.

#### Usage

```go
str.RemoveSpace("a b c\n") // "abc"
```

#### Rules
- Unicode white space is removed, `\u0085` included: `"a b c\u0085d"` → `"abcd"`.
- `"  "` and `""` return `""`.
- An input without white space is returned as is, with no allocation.

## Performance notes

- `Enclose` and `RemoveDup` first scan the input once. When nothing would change, they return the input itself, with no allocation. Only an input that needs a change goes through the builder loop (`encloseSlow`, `removeDupSlow`). Tests compare both paths over 10,000 random strings.
- `RemoveDup` scans with a plain byte loop. It is faster than a search with `strings.IndexByte` on path-sized input (about 9 ns against 22 ns for `/static/css/app.min.css`).
- `RemoveSpace` uses `strings.Map`, which returns an input without white space unchanged and allocates nothing for it.

## Benchmarks

Results depend on the machine. They were captured when this README was generated, with `go test -run=^$ -bench=. -benchmem ./internal/str`:

```console
goos: darwin
goarch: amd64
cpu: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
BenchmarkRemoveSpace-12          	 1962733	       610.4 ns/op	     112 B/op	       1 allocs/op
BenchmarkEnclose_Dirty-12        	10390152	       115.4 ns/op	      32 B/op	       1 allocs/op
BenchmarkEnclose_Canonical-12    	83314483	        14.31 ns/op	       0 B/op	       0 allocs/op
BenchmarkRemoveDup_Dirty-12      	12523947	        94.79 ns/op	      48 B/op	       1 allocs/op
BenchmarkRemoveDup_Clean-12      	100000000	        11.05 ns/op	       0 B/op	       0 allocs/op
```
