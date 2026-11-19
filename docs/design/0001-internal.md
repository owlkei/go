# 0001: Internal foundations (str, slice, num, color, crypto, runtime)

Status: Approved
Packages: `internal/str`, `internal/slice`, `internal/num`, `internal/color`, `internal/crypto`,
`internal/runtime`; one-line call-site changes in `core/http.go` (see Impact)

Roadmap: `docs/roadmap/0002-request-hot-path.md`, step 1.

## Goal
The leaf helpers every request touches are correct for hostile input, documented, and cheap. The
per-request ID (hot spot 2 of `docs/review/http-baseline.md`) gets a generator that costs about
25 ns instead of 440 ns. Dead helpers go away so the next designs build on a small surface.

## Who calls what (hot or cold)

Traced from `core/app.go:157` (`ServeHTTP`) and `core/http.go:63` (`handleRequest`).

| Function | Per-request caller | Path |
|---|---|---|
| `str.Enclose` | `routing/router.go:178` (every request, until design 0002 removes the call); `core/ws.go:90` `isWSPath` (every request when WS is enabled, `core/app.go:165`) | **hot** |
| `str.Segment` | `internal/ds/trie.go:119` (every request) | hot today; deleted by design 0002 |
| `str.RemoveDup` | `core/http.go:227` (`serveContent`, every static file request) | **hot** |
| `str.RemoveSpace` | `ctx/http_context.go:84` (every JSONP response) | warm |
| `str.AddBegin`, `AddEnd`, `IsLower`, `IsUpper` | none in the module | dead |
| `slice.Get`, `StrParse*`, `Map`, `Filter`, `Iter` | `ctx/binding_str_arr.go`, `ctx/http_body.go:48,73`, `ctx/binding_file.go`, `ctx/fn.go:336` (query, param, header, form and body binding) | **hot** |
| `slice.Find`, `FindIndex`, `ToUnique` | none | dead |
| `num.F64ToAnyNum` | `ctx/binding.go:87,167` (every numeric field of a JSON body) | **hot** |
| `color.Fmt*` | bootstrap panics, startup log, `log/log_handler.go:399-411` (once, at handler construction), `internal/test`, `event/event.go:164` (listener panic) | cold |
| `crypto.UUID` | `ctx/context.go:22` (`SetID`, every request: hot spot 2); `core/ws.go:122` (per WS connection); `memorybroker/fn.go:9` | **hot** through `SetID` |
| `crypto.Random` | none | dead |
| `runtime.NodeID` | none | dead |

## Public API

All packages are `internal`: "public" means exported to the rest of the module.

```go
// internal/str
func Enclose(s string, sep byte) string        // unchanged signature and results
func RemoveDup(s string, c byte) string        // CHANGED (breaking): was RemoveDup(str, pattern string)
func RemoveSpace(s string) string              // unchanged
func Segment(s string, sep byte, start int) (string, int) // unchanged here; deleted by design 0002
// REMOVED: AddBegin, AddEnd, IsLower, IsUpper

// internal/slice
// unchanged: Map, Filter, Get, StrParseBool … StrParseComplex128, StrParseAny,
//            Iter(arr []any, dimensions int, cb func(any, int))   (parameter typo fixed)
// REMOVED: Find, FindIndex, ToUnique

// internal/num
func F64ToAnyNum(f64 float64, kind reflect.Kind) any // same signature; saturates (N1-N4)

// internal/color
// unchanged: the 17 Fmt* functions

// internal/crypto
func UUID() (string, error)                      // unchanged: RFC 4122 v4 from crypto/rand
func RandomHex(n int) string                     // NEW
func AppendRandomHex(dst []byte, n int) []byte   // NEW
// REMOVED: Random (and the unexported withCharset)

// internal/runtime
// REMOVED: the whole package (NodeID has no caller). See Open questions.
```

Unexported seams the tests rely on (the builder must keep these names):
`str.encloseSlow(s string, sep byte) string` and `str.removeDupSlow(s string, c byte) string`: the
plain builder loops, without the fast path.

**Interface offered to design 0003 (`ctx`):** `crypto.RandomHex(8)` returns the same shape as today's
request ID (8 lower-case hex characters) for about 25 ns and one 8-byte allocation;
`crypto.AppendRandomHex(buf[:0], 8)` writes it into a buffer the context owns with no allocation.
Whether `SetID` calls it eagerly or lazily, and how long the ID is, is decided in 0003. Step 1 only
replaces the call in `ctx/context.go:22` if 0003 is not approved yet: nothing in this design depends
on it.

## Behavior

### str

- **S1 (Enclose, pinned).** `Enclose("", sep)` and a string of only ASCII white space return `""`.
  Otherwise the result is the input with every ASCII white space byte (space, `\t`, `\n`, `\v`,
  `\f`, `\r`) removed, every run of `sep` collapsed to one, every run of `*` collapsed to one, and
  exactly one `sep` at the start and at the end. All other bytes are kept as they are (multi-byte
  UTF-8 included). Examples: `"users"` → `"/users/"`, `" /users/ "` → `"/users/"`,
  `"/a//b///c/"` → `"/a/b/c/"`, `"/a/**/b/"` → `"/a/*/b/"`, `"/"` → `"/"`, `"foo..bar"` with `.` →
  `".foo.bar."`.
- **S2 (Enclose fast path).** When the input is already in that form (first and last byte are `sep`,
  no ASCII white space, no two `sep` in a row, no `**`), `Enclose` returns the input string itself:
  `testing.AllocsPerRun` reports 0.
- **S3 (fast equals slow).** For any string, `Enclose(s, sep) == encloseSlow(s, sep)`.
- **S4 (RemoveDup, bug).** `RemoveDup(s, c)` collapses every run of the byte `c` to a single `c` and
  keeps every other byte unchanged, valid UTF-8 or not.
  Bug today, `internal/str/str.go:102-107`: the loop ranges over runes and writes them back with
  `WriteRune`, so an invalid byte becomes U+FFFD. `RemoveDup("a\xff//b", "/")` returns
  `"a\xef\xbf\xbd/b"`; expected `"a\xff/b"`. The input is the client's URL path
  (`core/http.go:227`), so a file name with a non-UTF-8 byte can never be served.
  Kept: `RemoveDup("/**/school**/***", '*')` → `"/*/school*/*"`; `RemoveDup("", c)` → `""`.
- **S5 (RemoveDup fast path).** When `s` holds no two `c` in a row, `RemoveDup` returns `s` itself
  with 0 allocations. For any string, `RemoveDup(s, c) == removeDupSlow(s, c)`.
- **S6 (RemoveSpace, pinned).** Removes every Unicode white space rune (`unicode.IsSpace`); a string
  without one is returned unchanged.

### slice

No demonstrable bug found. Pinned rules for the rewritten tests:

- **L1.** `Map` returns a non-nil slice of the same length, calling `cb(el, i)` in order. `Filter`
  keeps the elements for which `cb` is true, in order. `Get(arr, i)` returns `(arr[i], true)` for
  `0 <= i < len(arr)`, else `(zero, false)` (negative index included).
- **L2.** Every `StrParse<T>` returns one element per input, and the zero value for an element that
  does not parse **or is out of range for `T`** (`StrParseInt8([]string{"300"})` → `[0]`,
  `StrParseUint([]string{"-1"})` → `[0]`, `StrParseFloat32([]string{"1e40"})` → `[0]`).
  `StrParseComplex*` ignores spaces (`"1 + 2i"` → `(1+2i)`). `StrParseAny` boxes each string.
- **L3.** `Iter` visits depth first; for a nested `[]any` it visits the children with
  `dimensions-1`, then calls `cb` on the nested slice itself with `dimensions`.

### num

- **N1 (signed, bug).** For `Int`, `Int8`, `Int16`, `Int32`, `Int64`: NaN → 0; a value below the
  type's minimum → the minimum; above the maximum → the maximum; otherwise truncation toward zero.
  The dynamic type of the result is the Go type of the kind (`int8` for `reflect.Int8`).
  Bug today, `internal/num/num.go:9-22`: a float to integer conversion that does not fit is
  implementation-defined in Go. Measured on amd64: `F64ToAnyNum(300, reflect.Int8)` → `44`,
  `(-129, Int8)` → `127`, `(70000, Int16)` → `4464`, `(5e9, Int32)` → `-2147483648`,
  `(1e20, Int)` → `-9223372036854775808`. arm64 gives other values. The input is a number in a
  client's JSON body (`ctx/binding.go:87`). Expected: `127`, `-128`, `32767`, `2147483647`,
  `math.MaxInt`.
- **N2 (unsigned, bug).** For `Uint` … `Uint64`: NaN or a value ≤ 0 → 0 (kept); above the maximum →
  the maximum. Today (`num.go:24-52`): `(256, Uint8)` → `0`, `(5e9, Uint32)` → `705032704`,
  `(1e30, Uint64)` → `9223372036854775808`. Expected: `255`, `4294967295`, `math.MaxUint64`.
- **N3 (floats).** `Float64` → the value. `Float32`: beyond ±`math.MaxFloat32` → ±`math.MaxFloat32`
  (today `+Inf`, which `encoding/json` then refuses to write back); NaN stays NaN.
  `Complex64` / `Complex128`: the real part follows the `Float32` / `Float64` rule, imaginary 0.
- **N4.** Any other kind → `0` (an `int`), as today.

### color

- **C1 (pinned).** Each `Fmt*` returns `<open> + text + <close>` with the codes it has today
  (`color.go`; `FmtPurple` closes with `\033[39m`, `FmtBGGrey` with `\033[49m`, the others with
  `\x1b[0m`). With no arguments the format is used **verbatim** (a `%` in it is not interpreted:
  `FmtRed("100%")` → `"\x1b[31m100%\x1b[0m"`); with arguments the text is
  `fmt.Sprintf(format, a...)`.

### crypto

- **K1 (pinned).** `UUID()` returns 36 characters `8-4-4-4-12` of lower-case hex, version nibble
  `4` (index 14), variant nibble one of `8 9 a b` (index 19), a nil error, and 1,000 calls give
  1,000 distinct values.
- **K2.** `RandomHex(n)` returns exactly `n` characters, each in `0-9a-f`; `n <= 0` → `""`.
- **K3.** `AppendRandomHex(dst, n)` returns `dst` with exactly `n` hex characters appended; the
  existing content of `dst` is unchanged; `n <= 0` returns `dst`; with `cap(dst)-len(dst) >= n` it
  allocates nothing (`testing.AllocsPerRun` = 0).
- **K4.** 10,000 calls of `RandomHex(16)` give 10,000 distinct values, and across them each of the
  16 hex digits appears at every one of the 16 positions (a broken nibble loop fails this).
- **K5.** `RandomHex` and `AppendRandomHex` are safe for concurrent use (race test).

`Random` is removed with its `Random(-1)` panic (`crypto.go:12`, `make` with a negative length) and
the endless loop of `withCharset` for a charset longer than 256 (`crypto.go:10`: `limit` becomes 0).
Neither is reachable from the module today.

## Performance

Current numbers (this machine, quick run: `-count 3 -benchtime 0.3s`; the step's commit carries the
`-count 5` `benchstat` before and after):

| Benchmark | Now | Planned | Expected |
|---|---|---|---|
| `Enclose` (dirty input `//users//profile//settings//`) | 90 ns, 32 B, 1 alloc | unchanged slow path | same |
| `Enclose` (canonical input, new benchmark) | about 80 ns, 1 alloc (measured as part of `Clean + Enclose` on `/users/all/`) | S2 fast path: one scan, return the input | ≤ 20 ns, 0 allocs |
| `RemoveDup` (dirty) | 163 ns, 48 B, 1 alloc | byte loop instead of rune loop | ≤ 100 ns, 1 alloc |
| `RemoveDup` (no duplicate, new benchmark) | about 160 ns, 1 alloc | S5 fast path (`strings.Index` for the two-byte run) | ≤ 15 ns, 0 allocs |
| `RemoveSpace` | 547 ns, 112 B, 1 alloc (input with 51 spaces) | none: `strings.Map` already returns a space-free input without allocating | same |
| `F64ToAnyNum` | 3-12 ns; 1 alloc (8 B) when the value is boxed | range checks only; boxing stays (the caller stores the `any`) | +1-2 ns, same allocs |
| `StrParseInt` (1,024 elements) | 7.1 µs = 6.9 ns per element, 1 alloc | plain loops instead of `Map` + closure in the 15 `StrParse*` | −10 to −20 %; keep only if `benchstat` shows it |
| `Map`, `Filter` | 1.5 µs / 3.6 µs for 1,024 elements, 1 alloc | none | same |
| `crypto.UUID` | 439 ns, 48 B, 1 alloc; **1,100 ns** under `RunParallel` (12 threads) | none (per WS connection, not per message) | same |
| request ID: `RandomHex(8)` (prototype measured) | today's `UUID()[:8]`: 439 ns / 1,100 ns parallel, 48 B | `math/rand/v2` top-level generator (ChaCha8, seeded by the OS, per-thread state, no lock, no syscall) | **24 ns serial, 5 ns parallel, 8 B, 1 alloc**; `AppendRandomHex`: 20 ns, 0 allocs |
| `color.Fmt*` | 90-130 ns, 1-2 allocs | cold: one private helper replaces 17 copies of the body | same |

Effect on the baseline: the ID is 8 % of CPU and 4 % of objects there; replacing it removes about
415 ns of the 1,672 ns Static request (about 25 %) once `ctx` uses it (design 0003).

Type B changes (fast paths, each with an equality test: S3, S5): `Enclose`, `RemoveDup`.

**Heavy techniques: none.** No `unsafe`, no `sync.Pool`, no hand-managed buffer inside these
packages (`AppendRandomHex` only appends to the caller's slice), no inlining restructure.

Recorded, not done here (belongs to 0003): `F64ToAnyNum` boxes every result; a setter on
`reflect.Value` would avoid the allocation, but `ctx/binding.go` keeps the boxed value in
`FieldLevel`, so the binding design decides.

## Concurrency
Every function is pure except the generators. `UUID` uses `crypto/rand` (safe). `RandomHex` /
`AppendRandomHex` use the top-level functions of `math/rand/v2`, which are safe for concurrent use
and keep their state per thread, so there is no shared lock (K5). No package-level mutable state
remains (`runtime.nodeID` goes with the package).

## Security
- `RandomHex` is for correlation IDs, **not for secrets**: tokens, session IDs and anything a client
  must not guess keep using `UUID` (`crypto/rand`). The README says so.
- N1-N3 make the result of a client-supplied number the same on every platform.
- S4 stops the rewriting of client path bytes.
- No function here bounds its input: all of them are linear in the input, and the callers bound it
  (path length in design 0002, body size in `core`).
- For 0003: `ctx/http_context.go:84` only strips white space from the JSONP `callback` query value
  before writing it into the response; it needs an identifier check there, not in `str`.
- For 0003: `requestIDLength = 8` (`ctx/context.go:11`) is 32 bits: two requests share an ID with
  50 % probability after about 77,000 requests. `RandomHex(16)` costs the same.

## Impact
- `core/http.go:227`: `str.RemoveDup(c.URL.Path, "/")` → `str.RemoveDup(c.URL.Path, '/')`.
- Removed functions have no caller (checked with `grep` over the module, `sample/` and `bench/`
  included); their tests and benchmarks are deleted.
- `crypto.UUID` callers (`core/ws.go:122`, `memorybroker/fn.go:9`, `sample/shop/accounts/service.go:74`)
  are untouched.
- Docs: a `README.md` in `internal/str`, `internal/slice`, `internal/num`, `internal/color`,
  `internal/crypto` (structure of `modules/config/README.md`, shortened: what the package is for, a
  table of functions with one line each, hot / cold and the performance notes above, the benchmark
  table). `.owlky/package-reference.md` if it lists these helpers.

## Test plan

Internal test packages, one function per behavior, `test.DiffMessage`.

| Rule | Test | Cases |
|---|---|---|
| S1 | `TestEnclose_Empty`, `TestEnclose_AddsSeparators`, `TestEnclose_CollapsesSeparators`, `TestEnclose_CollapsesStars`, `TestEnclose_StripsASCIISpace`, `TestEnclose_KeepsUTF8` | the examples of S1; `"/"`; `"*"` → `"/*/"`; `"/é/"` |
| S2 | `TestEnclose_CanonicalNoAlloc` | `AllocsPerRun` on `"/users/all/"` = 0 and the result equals the input |
| S3 | `TestEnclose_FastEqualsSlow` | 10,000 random strings over `a b / * space \t é`, fixed seed |
| S4 | `TestRemoveDup_CollapsesRuns`, `TestRemoveDup_KeepsInvalidUTF8`, `TestRemoveDup_Empty` | `"a\xff//b"`; the existing `*` case |
| S5 | `TestRemoveDup_NoRunNoAlloc`, `TestRemoveDup_FastEqualsSlow` | `"/a/b/c"`; 10,000 random strings with `\xff` in the alphabet |
| S6 | `TestRemoveSpace_RemovesUnicodeSpace`, `TestRemoveSpace_NoSpaceUnchanged` | `"a b c\n"` |
| L1 | `TestMap_*`, `TestFilter_*`, `TestGet_InRange`, `TestGet_OutOfRange`, `TestGet_Negative` | nil and empty input too |
| L2 | one `TestStrParse<T>_Valid` and `_InvalidIsZero` per function; `TestStrParseInt8_OutOfRangeIsZero`, `TestStrParseUint_NegativeIsZero`, `TestStrParseFloat32_OverflowIsZero`, `TestStrParseComplex64_IgnoresSpaces` | |
| L3 | `TestIter_VisitsLeavesDepthFirst`, `TestIter_ReportsDimension` | the existing four-level input |
| N1 | `TestF64ToAnyNum_SignedInRange`, `_SignedSaturatesHigh`, `_SignedSaturatesLow`, `_SignedNaNIsZero`, `_SignedTruncates`, `_ReturnsKindType` | every signed kind; the measured inputs above; `1.9` → `1`, `-1.9` → `-1` |
| N2 | `TestF64ToAnyNum_UnsignedNegativeIsZero`, `_UnsignedSaturatesHigh`, `_UnsignedNaNIsZero` | every unsigned kind |
| N3 | `TestF64ToAnyNum_Float32Saturates`, `_Float64Unchanged`, `_Complex` | `1e300`, `-1e300` |
| N4 | `TestF64ToAnyNum_UnknownKindIsZero` | `reflect.String` |
| C1 | `TestFmt_NoArgsVerbatim`, `TestFmt_ArgsFormatted`, `TestFmt_Codes` | all 17 functions against a table of open / close codes; a format with `%` |
| K1 | `TestUUID_Format`, `TestUUID_Distinct` | |
| K2 | `TestRandomHex_Length`, `TestRandomHex_OnlyHex`, `TestRandomHex_ZeroAndNegative` | n = 1, 8, 16, 17, 33 |
| K3 | `TestAppendRandomHex_AppendsAfterPrefix`, `TestAppendRandomHex_NoAllocWithCapacity`, `TestAppendRandomHex_ZeroAndNegative` | |
| K4 | `TestRandomHex_Distinct`, `TestRandomHex_EveryDigitAtEveryPosition` | |
| K5 | `TestRandomHex_ConcurrentNoRace` | 8 goroutines × 1,000 calls under `-race` |

Benchmarks (all with `for b.Loop()` + `b.ReportAllocs()`; `RunParallel` ones use a per-goroutine
sink): rewrite `BenchmarkEnclose` → `BenchmarkEnclose_Dirty`, add `BenchmarkEnclose_Canonical`;
`BenchmarkRemoveDup` → `_Dirty`, add `_Clean`; `BenchmarkRemoveSpace`; `BenchmarkSegment` (kept until
0002); `BenchmarkMap`, `BenchmarkFilter`, `BenchmarkStrParseInt`, add `BenchmarkStrParseInt_One`
(a one-element slice: the realistic query size); the four `BenchmarkF64ToAnyNum_*`, add
`_Saturated`; the four color benchmarks; new `BenchmarkUUID`, `BenchmarkUUID_Parallel`,
`BenchmarkRandomHex`, `BenchmarkRandomHex_Parallel`, `BenchmarkAppendRandomHex`. Delete
`BenchmarkIsLower`, `BenchmarkIsUpper`, `BenchmarkToUnique`, `BenchmarkNodeID`.

## Open questions
1. **Delete the dead code?** `str.AddBegin`, `AddEnd`, `IsLower`, `IsUpper`; `slice.Find`,
   `FindIndex`, `ToUnique`; `crypto.Random`; the whole `internal/runtime` package (`NodeID`). None
   has a caller. Recommendation: delete (the design assumes it). If you keep `internal/runtime` for
   the WebSocket roadmap, it stays as it is and only gets a README and the benchmark conversion.
2. **Request IDs from `math/rand/v2` instead of `crypto/rand`?** They are unpredictable in practice
   (ChaCha8 seeded by the OS) but the package does not promise it. Recommendation: yes for request
   IDs, with `UUID` kept for everything secret. The alternative (keep `crypto/rand`, read 4 bytes
   instead of 16) still costs a system call per request: about 400 ns here.
3. **Saturate or reject out-of-range numbers?** This design saturates (it extends the existing
   "negative → 0" rule for unsigned types). Rejecting with a binding error is a `ctx` behavior change
   and belongs to 0003. Recommendation: saturate now.

## Decisions (2026-11-16)

Approved by Claude under the user's one-time delegation for roadmap 0002 (see its Log). Heavy
techniques are **not** approved: they are built without and listed as proposals for the user.
The open questions above are answered as follows.

- Q1 dead code: delete (`str.AddBegin/AddEnd/IsLower/IsUpper`, `slice.Find/FindIndex/ToUnique`,
  `crypto.Random`, the `internal/runtime` package).
- Q2 request IDs from `math/rand/v2` (`crypto.RandomHex`); `crypto.UUID` stays for secrets.
- Q3 numbers: `num` offers a conversion that reports whether the value fit (the saturated value plus
  `ok bool`), so callers choose. Binding (design 0003) uses `ok == false` to reject the field as a
  bind error instead of storing a saturated or garbage value: a client must not turn `1e20` into
  `MaxInt64` silently. Fractions still truncate (0003 Q4).
