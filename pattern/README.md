# Pattern

*Package `pattern` matches dotted topic names (`user.created`, `tenant.abc.user.updated`) against patterns with `*` wildcards. Ginject uses it for WebSocket event names, the topics of `memorybroker`, and the wildcard rules of log masking.*

- [Pattern](#pattern)
  - [Key features](#key-features)
  - [Usage](#usage)
  - [Syntax](#syntax)
  - [Functions](#functions)
    - [NewPattern](#newpattern)
  - [Pattern methods](#pattern-methods)
    - [Match](#match)
    - [Kind](#kind)
    - [SimplePrefix](#simpleprefix)
    - [Raw, IsExact, IsGlobal](#raw-isexact-isglobal)
  - [Kind values](#kind-values)
  - [Edge cases](#edge-cases)
  - [Benchmarks](#benchmarks)

## Key features

- Four kinds of pattern, picked once by `NewPattern`: exact, global (`*`), a trailing wildcard (`user.*`) and any other mix (`tenant.*.user.*`).
- `Match` never allocates, whatever the kind.
- A `Pattern` is a small value: copy it, compare its `Kind`, keep it in a slice.
- Callers that index many patterns use `Kind` and `SimplePrefix` to skip `Match` for most of them (an exact pattern in a map, a trailing wildcard in a map by prefix).

## Usage

```go
package main

import (
	"fmt"

	"github.com/dangduoc08/ginject/pattern"
)

func main() {
	p := pattern.NewPattern("tenant.*.user.*")
	fmt.Println(p.Match("tenant.abc.user.created"))
	fmt.Println(p.Match("tenant.abc.user"))
	fmt.Println(pattern.NewPattern("user.*").SimplePrefix())
}
```

Output:

```console
true
false
user
```

## Syntax

A pattern and a topic are split into segments at each `.`. A segment of the pattern that is exactly `*` is a wildcard. Any other segment, including one that only contains a `*` (`a*`, `**`), must equal the topic's segment byte for byte. Matching is case-sensitive.

| Pattern | Matches | Does not match |
|---|---|---|
| `user.created` | `user.created` | `user.updated`, `user.created.extra`, `user` |
| `*` | every topic, the empty one included | |
| `user.*` | `user.created`, `user.profile.updated` | `user`, `userx.created`, `order.created` |
| `*.created` | `user.created`, `order.created` | `a.b.created`, `user.updated` |
| `tenant.*.user.created` | `tenant.1.user.created` | `tenant.1.2.user.created`, `tenant.user.created` |
| `tenant.*.user.*` | `tenant.1.user.created`, `tenant.1.user.profile.updated` | `tenant.1.user`, `tenant.1.admin.created` |

- A `*` segment in any position but the last matches exactly one segment.
- A `*` as the last segment matches one or more segments.
- `*` alone matches any topic.

## Functions

### NewPattern

```go
func NewPattern(raw string) Pattern
```

Parses `raw` and picks its [kind](#kind-values). It never fails: every string is a valid pattern.

- Exact, global and trailing wildcard patterns don't allocate. Other patterns allocate their list of segments once.
- The returned `Pattern` refers to `raw`; it keeps no other state and is safe for concurrent use.

## Pattern methods

### Match

```go
func (p Pattern) Match(topic string) bool
```

Reports whether `topic` matches the pattern, by the rules in [Syntax](#syntax). It never allocates.

### Kind

```go
func (p Pattern) Kind() Kind
```

Returns the kind `NewPattern` picked, see [Kind values](#kind-values).

### SimplePrefix

```go
func (p Pattern) SimplePrefix() string
```

For a `KindSuffixWildcard` pattern, the part before the trailing `.*` (`user.*` gives `user`). A topic matches such a pattern when it starts with that prefix and a `.`, followed by at least one byte. For every other kind it returns `""`.

### Raw, IsExact, IsGlobal

```go
func (p Pattern) Raw() string
func (p Pattern) IsExact() bool
func (p Pattern) IsGlobal() bool
```

`Raw` returns the string given to `NewPattern`. `IsExact` and `IsGlobal` are shorthands for `Kind() == KindExact` and `Kind() == KindGlobal`.

## Kind values

| Kind | Pattern | How `Match` decides |
|---|---|---|
| `KindExact` | no `*` segment (`user.created`) | string equality |
| `KindGlobal` | `*` | always true |
| `KindSuffixWildcard` | one `*`, as the last segment (`user.*`, `a.b.*`) | a prefix check |
| `KindComplex` | any other `*` (`*.created`, `tenant.*.user.*`) | segment by segment, without splitting the topic |

## Edge cases

Empty segments (two dots in a row, a leading or trailing dot) are segments like any other:

| Pattern | Topic | Match |
|---|---|---|
| `user.*` | `user.` | no: nothing follows the prefix |
| `user.*` | `user..x` | yes |
| `*.created` | `.created` | yes: the first segment is empty |
| `a.*.b` | `a..b` | yes |
| `""` (empty) | `""` | yes |
| `""` (empty) | `a` | no |

## Benchmarks

`go test ./pattern -bench . -benchmem`, Go 1.27.4, Intel Core i7-9750H, mean of 6 runs.

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| `NewPattern_Exact` | 12.5 | 0 | 0 |
| `NewPattern_SuffixWildcard` | 12.5 | 0 | 0 |
| `NewPattern_Complex` | 68.4 | 64 | 1 |
| `Match_Exact` | 3.9 | 0 | 0 |
| `Match_Global` | 3.7 | 0 | 0 |
| `Match_SuffixWildcard_OneSegment` | 6.0 | 0 | 0 |
| `Match_SuffixWildcard_Miss` | 6.1 | 0 | 0 |
| `Match_SuffixWildcard_DeepTopic` | 5.1 | 0 | 0 |
| `Match_Complex_3Seg` | 36.9 | 0 | 0 |
| `Match_Complex_MiddleAndTrailing` | 30.1 | 0 | 0 |
