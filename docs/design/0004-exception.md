# 0004: Errors and results (`exception`, `aggregation`)

Status: Approved
Packages: `exception`, `aggregation` (roadmap 0002, step 4). Depends on design 0003
(`ctx.JSONAppender`). `bench/owlkei.go` and `core` compile unchanged.

## Goal
Raising an exception and writing its body stop allocating: the constructors build no error object,
and the standard body `{"code":…,"error":…,"message":…}` is appended to a buffer instead of going
through a map and reflection. Interceptor operators run in the order they are piped, a filter no
longer mistakes `nil` data for a rejection, and an aggregation with its operators is one allocation.

Measured on Go 1.27.4, darwin/amd64, i7-9750H.

## Public API

### `exception`
Unchanged signatures: `NewException(message string, code int, opts ...any) Exception`, the 22 HTTP and
9 WS constructors, `ExceptionOptions`, `Error`, `Unwrap`, `GetCode`, `GetMessage`, `GetStatusText`.

Added:
```go
func (e Exception) AppendJSON(dst []byte) []byte
func (e Exception) MarshalJSON() ([]byte, error)
```
`Exception` and `*Exception` both satisfy `ctx.JSONAppender` (design 0003), so `c.JSON(ex)` writes the
body without `encoding/json`.

Changed behavior (not signatures): `Unwrap` (E4), `json.Marshal` of an `Exception` (E7: today `{}`).

### `aggregation` (breaking)
```go
type Operator struct{ /* unexported */ }                        // was struct{ Aggregation AggregationOperator; Name string }

func (a *Aggregation) Transform(fn func(any) any) Operator      // was … AggregationOperator
func (a *Aggregation) Tap(fn func(any) any) Operator            // was … AggregationOperator
func (a *Aggregation) Filter(predicate func(any) bool) Operator // was … AggregationOperator
func (a *Aggregation) Pipe(operators ...Operator) any           // was Pipe(operators ...AggregationOperator) any
```
Removed: `AggregationOperator` (an alias of `func(any) any`), the exported fields of `Operator`, the
constants `OperatorTransform`, `OperatorTap`, `OperatorFilter`. None is used outside the package.

Unchanged: `NewAggregation`, `SetMainData`, `Aggregate`, the fields `InterceptorData`, `Name`,
`IsMainHandlerCalled`. The usual interceptor compiles unchanged:
`return aggregation.Pipe(aggregation.Transform(func(data any) any { … }))`.

## Behavior

### `exception`: bugs
- **E1** `exception/exception.go:55-77`. An option of an unknown type is dropped and leaves the error
  empty: `BadRequestException("m", &ExceptionOptions{Description: "d"}).Error()`,
  `BadRequestException("m", map[string]any{"description": "d"}).Error()` and
  `BadRequestException("m", nil).Error()` all return `""`, where no option at all returns
  `"Bad Request"`. `common/exception_filter.go:61` passes such a map, so its
  `"Unknown exception"` never reaches anyone. Rules: a non-nil `*ExceptionOptions` is read like the
  value; any other unsupported type, `nil` and a nil `*ExceptionOptions` behave as no option
  (`Error()` is the status text).
- **E2** `exception/exception.go:57`. An empty string option gives `Error() == ""` for a code that
  has a status text. Rule: `""` behaves as no option.

### `exception`: rules
`text` is `GetStatusText()`; "first option" is `opts[0]`, later options are ignored (unchanged).

- **E3** `Error()` by first option:

  | First option | `Error()` |
  |---|---|
  | none, or E1 / E2 | `text` (`""` for a code with no text) |
  | non-empty `string` s | s |
  | `error` err (non-nil) | `err.Error()` |
  | `ExceptionOptions{Description: d}` | d, or `text` when d is empty |
  | `ExceptionOptions{Description: d, Cause: c}` | `d + ": " + c.Error()`; with d empty `text + ": " + c.Error()`; with d and `text` both empty `c.Error()` |

  These are today's strings except E1 and E2.
- **E4** `Unwrap()` returns the cause: the `error` option or `ExceptionOptions.Cause`, else `nil`.
  `errors.Is(ex, cause)` and `errors.As` through the cause hold as today. Change: with no option, a
  string option or a description alone, `Unwrap()` is `nil` (today an anonymous `errors.New` value
  that nothing can match).
- **E5** `GetCode`, `GetMessage`, `GetStatusText` are unchanged: HTTP codes use `http.StatusText`,
  WS codes the table at `exception/ws_exception.go:16`, unknown codes `""`.
- **E6** `AppendJSON(dst)` appends `{"code":<code>,"error":<Error()>,"message":<GetMessage()>}` to
  `dst` and returns the extended slice. The appended bytes equal
  `json.Marshal(map[string]any{"code": e.GetCode(), "error": e.Error(), "message": e.GetMessage()})`
  for every exception: same key order, same escaping (`<`, `>`, `&`, U+2028, U+2029, control
  characters, quotes, backslashes, invalid UTF-8 as U+FFFD). It never fails and keeps what `dst`
  already holds. It writes the fields as they are: defaults for a zero code or an empty message stay
  the caller's job (`core/default.go`).
- **E7** `MarshalJSON()` returns `AppendJSON(nil)` and a nil error, so `json.Marshal(ex)`,
  `json.Marshal(&ex)` and an exception nested in a map all give the E6 body.
- **E8** Two exceptions built with the same message, code and no option, a string option or a
  description are equal with `==`.
- **E9** No constructor allocates for: no option, a string option, an `error` option,
  `ExceptionOptions` by value. `Error()` allocates only for the description-and-cause form.

### `aggregation`: bugs
- **A1** `aggregation/aggregation.go:22-26`. `Pipe` ignores its arguments. Because the operator type
  is an alias of `func(any) any`, `a.Pipe(func(d any) any { return "changed" })` compiles and the
  function never runs; an operator created by another aggregation is dropped the same way. Rule:
  `Pipe` runs exactly the operators it is given; a plain function no longer compiles as one.
- **A2** `aggregation/operation.go:9-27`. Operators are registered when `Transform / Tap / Filter` are
  called, not when they are piped: `t2 := a.Transform(add2); t1 := a.Transform(add1); a.Pipe(t1, t2)`
  runs `add2` first. Rule: operators run in `Pipe` argument order; creating one registers nothing.
- **A3** `aggregation/operation.go:19-27`, `aggregation/aggregation.go:49-54`. A filter signals
  rejection by returning `nil`, so data that is `nil` (a handler that returns nothing) is rejected by
  a predicate that returned `true`: `a.Pipe(a.Filter(alwaysTrue), a.Transform(wrap))` with `nil` data
  never calls `wrap`. Rule: only the predicate's result decides.
- **A4** `aggregation/aggregation.go:46`. `a.Transform(nil)` panics with a nil dereference inside
  `Aggregate`, after the handler has run. Rule: an operator with a nil function, and the zero
  `Operator{}`, are skipped.

### `aggregation`: rules
- **A5** `Pipe(ops...)` sets `IsMainHandlerCalled = true`, replaces any operators piped before, and
  returns `nil` (with or without operators).
- **A6** `Aggregate()` starts from the main data and applies the operators in order: `Transform`
  replaces the data with the function's result (a `nil` result included, and the chain goes on);
  `Tap` calls the function and ignores its result; `Filter` calls the predicate: `true` continues with
  the data unchanged, `false` stops and `Aggregate` returns `nil` without calling later operators.
  With no operators it returns the main data. The final data is kept as the main data (unchanged).
- **A7** `NewAggregation()` returns an aggregation with `IsMainHandlerCalled == false`, nil data, no
  operators. `SetMainData` returns the receiver.
- **A8** An operator created by one aggregation and piped into another runs there.
- **A9** `Pipe` with up to 4 operators allocates nothing; with more it works the same and may
  allocate once.

## Performance

### Hot or cold
| Code | Path | Why |
|---|---|---|
| `NewException` and every constructor | hot (warm, client-triggered) | a refused guard (`common/http_guard.go:104`), a 404 (`core/http.go:250`), a body over the limit, any handler exception |
| `Error`, `GetCode`, `GetMessage`, `AppendJSON` | hot | the default filter on every exception (`core/default.go:12-33`) |
| `MarshalJSON`, `GetStatusText`, `Unwrap` | warm | only when called by user code or `errors.Is` |
| `NewAggregation`, `Pipe`, `Transform / Tap / Filter`, `SetMainData`, `Aggregate` | hot | once per interceptor per request (`common/http_interceptor.go:98-112`, `core/http.go:131-148`) |

### Planned changes (current → expected)
Current: the packages' benchmarks, median of 3. Expected ≈: throwaway prototypes on the same machine.

| # | Change | Type | Current | Expected |
|---|---|---|---|---|
| P1 | `Exception` stores `description string` and `cause error` instead of an error built with `errors.New` / `fmt.Errorf` at construction; `Error()` derives the text | A | no option 34 ns, 16 B, 1 alloc; string option 26 ns, 1 alloc; description + cause 260 ns, 104 B, 4 allocs | ≈ 16 ns, 0 allocs; description + cause est. 20 ns, 0 allocs at construction and one concatenation in `Error()` |
| P2 | `AppendJSON` writes the body by hand | B (H1) | map + `json.Marshal`: 1,940 ns, 576 B, 16 allocs (a struct through `encoding/json` would be 500 ns, 2 allocs) | ≈ 55 ns, 0 allocs for plain strings |
| P3 | `Operator` carries a one-byte kind and the function or predicate; `Aggregate` switches on the kind instead of comparing strings; `Filter` allocates no wrapper closure | A | `Aggregate` with transform + tap + filter 7.5 ns; `Filter` creation 1 closure | est. 6 ns; 0 allocs |
| P4 | `Aggregation` holds its first 4 operators in an inline array, more in a slice | A | registering two operators 78 ns, 72 B, 2 allocs (slice growth) | est. 10 ns, 0 allocs; the aggregation itself grows from 80 B to about 190 B, still one allocation |

Effect on the Exception scenario (7.9 µs, 33 allocations today): about −2 µs and −17 allocations, of
which P1 lands with this step and P2 lands when `core/default.go` writes the body through it (Open
question 1). On Chain: −2 allocations per interceptor.

### Heavy techniques (approving the design approves exactly these)
- **H1: a hand-written JSON encoder for one fixed shape** (`AppendJSON`). It appends the three keys
  and the code with `strconv.AppendInt`; a string that is only printable ASCII without `"`, `\`, `<`,
  `>`, `&` is copied between quotes (fast path), any other string is encoded with `json.Marshal` and
  appended (slow path, so escaping is `encoding/json`'s by construction). Justified by P2.
  Constraints: the key order and escaping must stay equal to `encoding/json` (E6 is tested against it
  on random and hostile strings); a new field in the body means changing this function.

No `unsafe`, no `sync.Pool`, no inlining restructure. Not taken: pooling `Aggregation` values (design
0005 creates one per interceptor per request and keeps them in the context's list).

## Concurrency
- `Exception` is an immutable value; copying and sharing it between goroutines is safe. Package-level
  exceptions (`core/default.go:8`) stay safe.
- An `Aggregation` belongs to one request and one goroutine; it has no lock. `Operator` values are
  immutable.
- No goroutines, no shared mutable state in either package.

## Security
- The message and the description of an exception can hold client input (`"Cannot GET " + path` at
  `core/http.go:250`, a file name at `ctx/http_file.go:52`). E6's escaping is the protection: quotes,
  backslashes, control characters, `<`, `>`, `&`, U+2028, U+2029 and invalid UTF-8 can't break out of
  the JSON string or of a page that embeds it. The test plan feeds it hostile strings.
- `Error()` of a wrapped cause is sent to the client by the default filter (unchanged). Wrapping an
  internal error as the cause exposes its text; the README says to put only client-safe text in an
  exception.
- No limits apply: both packages handle values the application built.

## Impact
- **`common` (design 0005).** `NormalizeRecovered` (`common/exception_filter.go:48-65`) passes a map
  as option: with E1 the resulting `Error()` is `"Internal Server Error"`, which is what clients
  already get after the default filter's fallback. 0005 owns that call (its B23) and the panicked
  `*exception.Exception` (its B21). What 0005 expects from this package holds: `Exception` stays a
  value type, and `ForbiddenException(message)` / `NewException(message, code)` can be built once at
  package initialization (E8, E9).
- **`core`.** Compiles unchanged. `core/default.go:28-32` and `core/http.go:249-277` keep working
  through the map; switching them to `c.Status(code).JSON(ex)` is Open question 1.
- **Applications.** `aggregation.AggregationOperator`, `Operator`'s fields and the three constants
  are gone; code that only uses `Pipe(Transform / Tap / Filter …)` is unaffected. Code that called
  `Transform` without piping the result (it ran anyway, in call order) must pipe it. `json.Marshal`
  of an exception gives the body instead of `{}`. `errors.Unwrap(ex)` is `nil` without a cause.
- **Docs:** new `exception/README.md` and `aggregation/README.md`; `.owlkei/package-reference.md`
  (lines 544-562: operator types; the exception section), `.owlkei/request-pipeline.md`
  (interceptor operators run in `Pipe` order).

## Test plan
Internal packages, `Test<Subject>_<Behavior>`, one function per behavior, `test.DiffMessage`.
Existing tests stay and must pass unless listed as replaced.

`exception`:
- E1 `TestNewException_PointerOptions`, `TestNewException_UnknownOptionTypeIsNoOption` (map, int),
  `TestNewException_NilOptionIsNoOption`, `TestNewException_NilPointerOptionsIsNoOption`.
- E2 `TestNewException_EmptyStringOptIsNoOption`.
- E3 one test per table row, plus `TestNewException_CauseWithoutTextOrDescription` (unknown code) and
  `TestNewException_OnlyFirstOptionIsRead`.
- E4 `TestException_Unwrap_ReturnsCause` (error option, options cause), `TestException_Unwrap_NilWithoutCause`
  (no option, string, description); `TestException_ErrorsIs_ChainWorks` and `_ErrorsAs_` stay.
- E6 `TestException_AppendJSON` (exact bytes for a 400 with message and description),
  `TestException_AppendJSON_KeepsPrefix` (non-empty `dst`), `TestException_AppendJSON_ZeroValue`
  (`{"code":0,"error":"","message":""}`), `TestException_AppendJSON_WSCode`,
  `TestException_AppendJSON_HostileStrings` (`"`, `\`, newline, `\u0000`, `</script>`, `&`, U+2028,
  invalid UTF-8, a 64 KiB message), and the fast-path test
  `TestException_AppendJSON_EqualsMarshal_Random`: 2,000 random message / description pairs (ASCII,
  multi-byte, escapes, invalid bytes) and codes compared with `json.Marshal` of the map.
- E7 `TestException_MarshalJSON`, `TestException_MarshalJSON_Pointer`, `TestException_MarshalJSON_InsideMap`.
- E8 `TestException_Equal`. E9 `TestNewException_NoAllocs` (`testing.AllocsPerRun` for the four forms).
- `TestHTTPExceptionHelpers`, `TestWSExceptionHelpers` stay (code and text per constructor).

`aggregation`:
- A1 `TestPipe_RunsOnlyPipedOperators` (an operator created but not piped does not run),
  `TestPipe_OperatorFromAnotherAggregation` (A8).
- A2 `TestPipe_RunsInArgumentOrder`.
- A3 `TestFilter_NilDataPredicateTrueContinues` (the later transform runs and its result is
  returned); `TestFilter_WithNilData` stays.
- A4 `TestPipe_SkipsNilFunction` (`Transform(nil)`, `Tap(nil)`, `Filter(nil)`), `TestPipe_SkipsZeroOperator`.
- A5 `TestPipe_SetsIsMainHandlerCalled`, `TestPipe_ReturnsNil`, `TestPipe_ReplacesPreviousOperators`.
- A6 `TestAggregate_TransformReturningNilContinues`, `TestAggregate_FilterFalseStopsAndReturnsNil`,
  `TestAggregate_TapResultIgnored`, plus the existing `TestAggregate_*` and `TestFilter_*` tests,
  rewritten to pipe their operators (`a.Pipe(a.Transform(f))` where they call `a.Transform(f)` alone).
- A9 `TestPipe_FourOperatorsNoAllocs` (`testing.AllocsPerRun`), `TestPipe_MoreThanFourOperators`
  (6 operators, order kept).

Benchmarks: every benchmark in the three `exception/*_bench_test.go` files and
`aggregation/aggregation_bench_test.go` moves to `for b.Loop()` + `b.ReportAllocs()`. New:
`BenchmarkException_AppendJSON`, `BenchmarkException_AppendJSON_Escaped`, `BenchmarkException_MarshalMap`
(the old body, kept as the reference), `BenchmarkNewException_DescriptionAndCause`,
`BenchmarkPipe_Two` (replaces `BenchmarkSetOperators_Two`), `BenchmarkInterceptorCycle` (new
aggregation, pipe one transform, set data, aggregate). Before and after with `-count 5` and
`benchstat` in the commit.

## Open questions
1. **Who switches the default filter to `AppendJSON`?** The gain of P2 shows only when
   `core/default.go:28-32` writes `c.Status(code).JSON(ex)` (when the exception needs none of the
   filter's defaults; otherwise it builds the normalized exception first). `core` outside `fn.go` is
   roadmap 0003. Recommendation: allow that one edit in `core/default.go` in this step, with a test
   that the body is byte-identical to today's, so step 7 measures the Exception scenario with it; the
   404 body (`core/http.go:249`, hot spot 8) stays for 0003.
2. **`Transform` without `Pipe` no longer applies (A1, A2).** `core` already ignores an aggregation
   whose `Pipe` was not called (`core/http.go:134`), so no pipeline behavior changes; only direct
   uses of the package do. Recommendation: accept.
3. **`Tap` keeps the signature `func(any) any`** with an ignored result, to leave interceptors
   untouched. `func(any)` would be more honest and is one more breaking change. Recommendation: keep.

## Decisions (2026-11-16)

Approved by Claude under the user's one-time delegation for roadmap 0002 (see its Log). Heavy
techniques are **not** approved: they are built without and listed as proposals for the user.
The open questions above are answered as follows.

- Q1 one edit in `core/default.go` so the default filter writes the new error body: yes.
- Q2 `Transform` without `Pipe` no longer applies: yes.
- Q3 `Tap` stays `func(any) any`: yes.
- **H1 (hand-written `AppendJSON`): not approved.** The error body is a fixed struct
  (`code`, `error`, `message`) encoded with `encoding/json` (about 500 ns, 2 allocs instead of
  1,940 ns, 16 allocs), with the same bytes as today.
