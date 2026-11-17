# Pattern

*Package `pattern` matches dotted topic names (`user.created`, `tenant.abc.user.updated`) against glob-style patterns: `*` is one segment, a trailing `**` is one or more. Ginject uses it for WebSocket event names, the topics of `memorybroker`, and the log's `MaskFields`.*

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
    - [Raw and IsExact](#raw-and-isexact)
  - [Kind values](#kind-values)
  - [Edge cases](#edge-cases)
  - [Benchmarks](#benchmarks)

## Key features

- Two wildcards, as in `.gitignore` and the common message brokers: `*` for exactly one segment, a trailing `**` for one or more.
- `Match` never allocates, and stops at the first segment that differs, so a long hostile topic costs no more than its first mismatch.
- `NewPattern` doesn't allocate for exact, global and trailing-`**` patterns.
- Callers that index many patterns use `Kind` and `SimplePrefix` to skip `Match` for most of them: an exact pattern in a map, a trailing-`**` pattern in a map by prefix.

## Usage

```go
package main

import (
	"fmt"

	"github.com/dangduoc08/ginject/pattern"
)

func main() {
	fmt.Println(pattern.NewPattern("chat.*").Match("chat.room1"))
	fmt.Println(pattern.NewPattern("chat.*").Match("chat.room1.typing"))
	fmt.Println(pattern.NewPattern("chat.**").Match("chat.room1.typing"))
	fmt.Println(pattern.NewPattern("chat.**").SimplePrefix())
}
```

Output:

```console
true
false
true
chat
```

## Syntax

A pattern and a topic are split into segments at each `.`. Matching is case-sensitive.

- `*` as a whole segment matches exactly one segment, in any position.
- `**` as the last segment matches one or more segments. `chat.**` doesn't match `chat`.
- `**` alone matches every topic. `*` alone matches single-segment topics only.
- Everything else is literal text: `*` inside a segment (`a*b`), `**` in any position but the last (`a.**.b`), and `>`.

Every rule with its examples is in [MATCHING.md](MATCHING.md). A test generates that file from `Match` itself and fails when it is out of date, so it is always the actual behavior. To regenerate it, run `go test ./pattern -run TestMatchingTableIsUpToDate -update-matching`.

## Functions

### NewPattern

```go
func NewPattern(raw string) Pattern
```

Parses `raw` and picks its [kind](#kind-values). It never fails: every string is a valid pattern.

- Exact, global and trailing-`**` patterns don't allocate. Other patterns allocate their list of segments once.
- The returned `Pattern` refers to `raw`. It keeps no other state and is safe for concurrent use.

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

For a `KindSuffixWildcard` pattern, the part before the trailing `.**` (`user.**` gives `user`). A topic matches such a pattern when it starts with that prefix and a `.`, followed by at least one byte. For every other kind it returns `""`.

### Raw and IsExact

```go
func (p Pattern) Raw() string
func (p Pattern) IsExact() bool
```

`Raw` returns the string given to `NewPattern`. `IsExact` is a shorthand for `Kind() == KindExact`.

## Kind values

| Kind | Pattern | How `Match` decides |
|---|---|---|
| `KindExact` | no wildcard (`user.created`, `a*b`, `a.**.b`) | string equality |
| `KindGlobal` | `**` | always true |
| `KindSuffixWildcard` | literal segments, then a trailing `**` (`user.**`, `a.b.**`) | a prefix check |
| `KindComplex` | any other wildcard (`*`, `user.*`, `*.created`, `tenant.*.user.**`) | segment by segment, in place |

## Edge cases

Topics should not hold empty segments (two dots in a row, a leading or a trailing dot). If one does, the empty segment is a segment like any other, except after a trailing-`**` prefix:

| Pattern | Topic | Match |
|---|---|---|
| `chat.**` | `chat.` | no: nothing follows the prefix |
| `chat.*.**` | `chat.room1.` | yes |
| `*.created` | `.created` | yes: the first segment is empty |
| `""` (empty) | `""` | yes |

## Benchmarks

`go test ./pattern -bench . -benchmem`, Go 1.27.4, Intel Core i7-9750H, mean of 6 runs.

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| `NewPattern_Exact` | 20.8 | 0 | 0 |
| `NewPattern_SuffixWildcard` | 31.1 | 0 | 0 |
| `NewPattern_Complex` | 84.8 | 64 | 1 |
| `Match_Exact` | 3.9 | 0 | 0 |
| `Match_Global` | 4.9 | 0 | 0 |
| `Match_SuffixWildcard_OneSegment` | 6.8 | 0 | 0 |
| `Match_SuffixWildcard_Miss` | 6.8 | 0 | 0 |
| `Match_SuffixWildcard_DeepTopic` | 5.3 | 0 | 0 |
| `Match_Complex_3Seg` | 50.7 | 0 | 0 |
| `Match_Complex_MiddleAndTrailing` | 38.9 | 0 | 0 |
| `Match_Complex_LongHostileTopic` | 15.7 | 0 | 0 |
