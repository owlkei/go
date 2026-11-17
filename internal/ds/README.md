# DS

*Package `ds` holds the generic path trie that the router uses to match request paths against route patterns: static segments, parameters, wildcards and globs, with backtracking and hard limits.*

- [DS](#ds)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [Constants and errors](#constants-and-errors)
    - [MaxSegments](#maxsegments)
    - [ErrTooManySegments](#errtoomanysegments)
  - [`Trie` Methods](#trie-methods)
    - [NewTrie](#newtrie)
    - [Insert](#insert)
    - [Find](#find)
    - [Len](#len)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

The package is internal: only code in the Ginject module can import it.

## Key Features
- Generic: each pattern end holds a `*T` of the caller (the router stores its `*RouterItem`)
- Precedence static > glob > parameter > wildcard, with backtracking, independent of insert order
- Request segments are never interpreted: a literal `$` or `*` in a path is plain text
- Bounded: at most 64 segments per pattern or path, every node visited at most once per lookup
- No allocation for a static match or a miss; none for a parameter match when the caller passes a buffer

## Usage

```go
package main

import (
	"fmt"

	"github.com/dangduoc08/ginject/internal/ds"
)

func main() {
	tr := ds.NewTrie[string]()
	user, files, rest := "user", "html file", "anything under /lv1"
	_ = tr.Insert("/users/$", &user)
	_ = tr.Insert("/files/*.html", &files)
	_ = tr.Insert("/lv1/*", &rest)

	value, params := tr.Find("/users/7", nil)
	fmt.Println(*value, params)

	value, _ = tr.Find("/files/index.html", nil)
	fmt.Println(*value)

	value, _ = tr.Find("/lv1/a/b/c", nil)
	fmt.Println(*value)

	value, _ = tr.Find("/nope", nil)
	fmt.Println(value == nil)
}
```

Output:

```console
user [7]
html file
anything under /lv1
true
```

## Constants and errors

### MaxSegments
Type: `int` (untyped constant)

Value: `64`

The most segments a pattern given to `Insert` or a path given to `Find` may have.

### ErrTooManySegments
Type: `error`

Message: `ds: pattern has more than 64 segments`

Returned by `Insert` for a pattern with more than `MaxSegments` segments. Compare with `errors.Is`.

## `Trie` Methods

`Trie[T]` has no exported fields. Segments are the non-empty pieces between `/`: `""` and `"/"` are the root, and repeated or trailing slashes are ignored.

Pattern syntax of `Insert`:

| Pattern segment | Kind | Matches in a path |
|---|---|---|
| `$` | parameter | exactly one non-empty segment, captured |
| `*` | wildcard | one segment in the middle; zero or more remaining segments at the end |
| text with `*` (`in*.html`, `image.*`) | glob | one segment; each `*` matches any run of bytes, empty included |
| anything else | static | that exact segment |

### NewTrie

Returns an empty trie.

#### Returns
1. `*Trie[T]`: a trie with no pattern.

#### Usage

```go
tr := ds.NewTrie[string]()
```

### Insert

Adds `pattern` with its value. Inserting a pattern again replaces its value. The trie is not safe for writes while other goroutines read it: insert everything first.

#### Parameters
1. `pattern` `string`: the pattern, such as `/users/$/friends/$`.
2. `value` `*T`: the value returned by `Find`; must not be nil.

#### Returns
1. `error`: `ErrTooManySegments` when the pattern has more than `MaxSegments` segments, else nil.

#### Usage

```go
value := "user"
if err := tr.Insert("/users/$", &value); err != nil {
	panic(err)
}
```

#### Rules
- A pattern with 64 segments is accepted; 65 return `ErrTooManySegments` and change nothing (no value, no prefix left behind).
- `""` and `"/"` are the same pattern (the root).
- Inserting the same pattern twice keeps the second value and counts once.

### Find

Returns the value of the pattern that matches `path`, and `params` followed by one value per `$` on that pattern's path.

#### Parameters
1. `path` `string`: the request path.
2. `params` `[]string`: values to keep in front of the captured ones, and the buffer the captured values are appended to; nil is fine.

#### Returns
1. `*T`: the matched value, or nil.
2. `[]string`: on a match, `params` plus the captured values in path order (written into `params`' backing array when its capacity is enough, otherwise a new slice); on a miss, `params` unchanged.

#### Usage

```go
value, params := tr.Find("/users/7/friends/9", nil)
if value != nil {
	fmt.Println(params[0], params[1])
}
```

#### Rules
- Root: a root pattern matches `/`, `""` and `//`; an empty trie matches nothing.
- Static: `/feeds/all` matches `/feeds/all` and `/feeds/all/`, not `/feeds`, `/` or `/feeds/all/x`.
- Parameter: `$` matches exactly one segment: `/users/$` matches `/users/7` (`["7"]`), not `/users` or `/users/7/x`.
- A literal `$` or `*` in a path is a value for a parameter (`/users/$/friends/*` gives `["$", "*"]`) and matches no static pattern.
- Wildcard in the middle (`/*/feeds`) matches exactly one segment; at the end (`/lv1/*`) it matches `/lv1`, `/lv1/`, `/lv1/a` and `/lv1/a/b/c`. Neither captures.
- Glob: `in*.html` matches `in.html` and `index.html`, not `in.htm` or `xin.html`; `ab*ba` does not match `aba`. A glob is one segment.
- Precedence at each node: static child, glob children, parameter child, wildcard as one segment, wildcard as the end; the first whose subtree matches the rest of the path wins. When the path ends at a node, the node's value wins over its end wildcard. Examples:
  - `/users/all/x` + `/users/$/y`: `/users/all/y` is the second, `["all"]`.
  - `/x/*` + `/x/*.html`: `/x/a.html` is the glob; `/f/$` + `/f/*.html`: `/f/a.html` is the glob.
  - `/lv1/*` + `/lv1/lv2/lv3/pong`: `/lv1/lv2/lv3/extra` is `/lv1/*`.
  - `/a/$/x` + `/a/*`: `/a/1/y` is `/a/*` with no values.
- Values captured on abandoned branches are dropped; a miss returns `params` unchanged.
- The result does not depend on the order of `Insert` calls. Globs are tried by number of literal bytes, most first, then by pattern in byte order (`/g/a*` + `/g/*bc`: `/g/abc` is `/g/*bc`).
- `//a///b/` equals `/a/b`. Dot segments (`.`, `..`) are plain text.
- A path with more than 64 segments matches nothing.
- No allocation for a static match or a miss.

### Len

Returns the number of patterns that hold a value.

#### Returns
1. `int`: the count; prefixes of a pattern do not count.

#### Usage

```go
fmt.Println(tr.Len())
```

#### Rules
- An empty trie has 0; `/a/b` alone counts 1.

## Performance notes

- Each node keeps the parameter child, the wildcard child and a sorted glob list in their own fields; the map holds static segments only, so a segment costs one map lookup plus the globs, parameter and wildcard tried in order.
- `Find` walks the path by index (no splitting, no copy) and recurses to backtrack; the depth is at most 64. Each node is reached at most once per call, so a lookup costs at most the size of the tree plus one glob test per glob child visited.
- The segment count of a path is only computed when the path is longer than 128 bytes (a shorter path cannot hold 65 segments).
- Captured values are written while the recursion unwinds into one slice taken at the matched end: the caller's buffer when its capacity is enough (`core` passes `ctx.HTTPParamValues(c)[:0]`, whose capacity the pooled context keeps), otherwise a new slice of the exact length. With a buffer a parameter match allocates nothing (about 80 ns against 130 ns before); a static match or a miss never allocates. The returned slice shares the buffer: it is valid until the owner reuses it.

## Benchmarks

Results depend on the machine. They were captured when this README was generated, with `go test -run=^$ -bench=. -benchmem ./internal/ds`:

```console
goos: darwin
goarch: amd64
pkg: github.com/dangduoc08/ginject/internal/ds
cpu: Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz
BenchmarkMatchGlob-12                     	38214238	        31.31 ns/op	       0 B/op	       0 allocs/op
BenchmarkTrieFind_Static-12               	26811342	        45.98 ns/op	       0 B/op	       0 allocs/op
BenchmarkTrieFind_WithParam-12            	18982029	        62.22 ns/op	      16 B/op	       1 allocs/op
BenchmarkTrieFind_DeepParam-12            	12026916	       101.3 ns/op	      32 B/op	       1 allocs/op
BenchmarkTrieFind_ParamReusedBuffer-12    	15223388	        80.49 ns/op	       0 B/op	       0 allocs/op
BenchmarkTrieFind_NoMatch-12              	25850860	        45.64 ns/op	       0 B/op	       0 allocs/op
BenchmarkTrieFind_Backtrack-12            	17222401	        71.18 ns/op	       0 B/op	       0 allocs/op
BenchmarkTrieInsert-12                    	 1201609	       998.3 ns/op	    1312 B/op	      15 allocs/op
```
