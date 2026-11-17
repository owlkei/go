# Aggregation

*Aggregation is a part of `Ginject` framework: it carries an interceptor's result through the request and applies the operators the interceptor pipes over the handler's return value.*

- [Aggregation](#aggregation)
  - [Key Features](#key-features)
  - [Usage](#usage)
  - [`Aggregation` Fields](#aggregation-fields)
    - [InterceptorData](#interceptordata)
    - [Name](#name)
    - [IsMainHandlerCalled](#ismainhandlercalled)
  - [`Operator` Type](#operator-type)
  - [Functions](#functions)
    - [NewAggregation](#newaggregation)
  - [`Aggregation` Methods](#aggregation-methods)
    - [Transform](#transform)
    - [Tap](#tap)
    - [Filter](#filter)
    - [Pipe](#pipe)
    - [SetMainData](#setmaindata)
    - [Aggregate](#aggregate)
  - [Performance notes](#performance-notes)
  - [Benchmarks](#benchmarks)

## Key Features
- Operators run in the order they are passed to `Pipe`
- Creating an operator registers nothing: only piped operators run
- A filter is decided by its predicate alone, so `nil` data can pass
- Up to four piped operators are stored without an allocation

## Usage

An interceptor receives a fresh `*aggregation.Aggregation` before the handler runs. Returning `Pipe(...)` lets the handler run, then the framework applies the piped operators to the handler's result:

```go
package main

import (
	"github.com/dangduoc08/ginject/internal/aggregation"
	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/ctx"
)

type EnvelopeInterceptor struct{}

func (instance EnvelopeInterceptor) Intercept(c *ctx.HTTPContext, a *aggregation.Aggregation) any {
	return a.Pipe(
		a.Filter(func(data any) bool { return data != nil }),
		a.Transform(func(data any) any { return ctx.Map{"data": data} }),
	)
}

type UserController struct {
	common.HTTP
	common.Interceptor
}

func (instance UserController) NewController() core.Controller {
	instance.BindInterceptor(EnvelopeInterceptor{})
	return instance
}

func (instance UserController) READ() string {
	return "users"
}

func main() {
	app := core.New()
	app.Create(core.ModuleBuilder().Controllers(UserController{}).Build())
	app.Listen(3000, nil)
}
```

`GET /` responds with `{"data":"users"}`. An interceptor that returns anything other than the result of `Pipe` skips the handler, and its return value is the response.

## `Aggregation` Fields

### InterceptorData
Type: `any`

Default: `nil`

Required: `false`

The value the interceptor returned. The framework sets it after `Intercept` runs and sends it as the response when the interceptor did not call `Pipe`.

### Name
Type: `string`

Default: `""`

Required: `false`

The interceptor's name, used by the framework in post-interceptor trace events.

### IsMainHandlerCalled
Type: `bool`

Default: `false`

Required: `false`

`true` once `Pipe` was called: the handler runs and the piped operators are applied to its result.

## `Operator` Type

`Operator` is an opaque step created by `Transform`, `Tap` or `Filter` and passed to `Pipe`. Its fields are unexported. The zero `Operator{}` is valid and does nothing when piped.

## Functions

### NewAggregation

Returns an empty aggregation: no data, no operators, `IsMainHandlerCalled` is `false`. The framework creates one per interceptor per request.

#### Returns
- 1st value: `*Aggregation`

- Description: A new, empty aggregation

#### Usage

```go
a := aggregation.NewAggregation()
```

#### Rules
- `IsMainHandlerCalled` is `false`, the main data is `nil`, `InterceptorData` and `Name` are empty.
- `Aggregate()` on a new aggregation returns `nil`.

### Transform

Creates an operator that replaces the data with the function's result.

#### Parameters
- 1st parameter: `func(any) any`

- Description: Receives the current data and returns the new data

#### Returns
- 1st value: `Operator`

- Description: The step to pass to `Pipe`

#### Usage

```go
a.Pipe(a.Transform(func(data any) any { return ctx.Map{"data": data} }))
```

#### Rules
- The operator runs only when passed to `Pipe`; creating it registers nothing, even on the aggregation that created it.
- A `nil` result replaces the data and the chain goes on with `nil`.
- `Transform(nil)` returns an operator that is skipped.

### Tap

Creates an operator that calls the function with the current data and ignores its result. Use it for side effects such as logging.

#### Parameters
- 1st parameter: `func(any) any`

- Description: Receives the current data; its result is discarded

#### Returns
- 1st value: `Operator`

- Description: The step to pass to `Pipe`

#### Usage

```go
a.Pipe(a.Tap(func(data any) any {
	log.Println(data)
	return nil
}))
```

#### Rules
- The data after a tap is unchanged, whatever the function returns.
- `Tap(nil)` returns an operator that is skipped.

### Filter

Creates an operator that stops the chain when the predicate returns `false`.

#### Parameters
- 1st parameter: `func(any) bool`

- Description: Receives the current data; `true` continues, `false` stops

#### Returns
- 1st value: `Operator`

- Description: The step to pass to `Pipe`

#### Usage

```go
a.Pipe(a.Filter(func(data any) bool { return data != nil }))
```

#### Rules
- `true` keeps the data unchanged and the next operator runs, also when the data is `nil`, `0` or `""`.
- `false` makes `Aggregate` return `nil`; later operators do not run.
- `Filter(nil)` returns an operator that is skipped.

### Pipe

Sets the operators to apply to the handler's result and lets the handler run. Return its result from `Intercept`.

#### Parameters
- Variadic parameter: `...Operator`

- Description: The operators, applied in argument order

#### Returns
- 1st value: `any`

- Description: Always `nil`

#### Usage

```go
return a.Pipe(a.Transform(addOne), a.Transform(timesTwo))
```

#### Rules
- Sets `IsMainHandlerCalled` to `true` and returns `nil`, with or without operators.
- Operators run in argument order, not in the order they were created.
- Only the operators passed run; an operator created by another aggregation runs here too.
- A second call replaces the operators of the first; `Pipe()` with no operators clears them.
- Operators with a `nil` function and the zero `Operator{}` are skipped.
- Up to four operators make no allocation; more work the same and keep their order.

### SetMainData

Sets the data the operators start from. The framework calls it with the handler's result.

#### Parameters
- 1st parameter: `any`

- Description: The data

#### Returns
- 1st value: `*Aggregation`

- Description: The same aggregation, for chaining

#### Usage

```go
result := a.SetMainData(handlerResult).Aggregate()
```

#### Rules
- Returns the receiver.
- A later call overwrites the value; `nil` clears it.

### Aggregate

Applies the piped operators to the main data and returns the result. The framework calls it after the handler.

#### Returns
- 1st value: `any`

- Description: The final data, or `nil` when a filter stopped the chain

#### Usage

```go
a.SetMainData(1)
a.Pipe(a.Transform(func(data any) any { return data.(int) + 1 }))
result := a.Aggregate()
```

#### Rules
- With no operators it returns the main data unchanged.
- Operators apply in `Pipe` order: `(0 + 1) * 3` gives `3` for a `+1` then a `*3` transform.
- The final data is kept as the main data.

## Performance notes

- An `Operator` holds a one-byte kind and its function, so `Aggregate` switches on the kind and `Filter` needs no wrapper closure.
- `Pipe` copies up to four operators into an array inside the `Aggregation`, so piping allocates nothing. More operators go into a slice that is allocated once and reused by later calls.
- The inline array makes an `Aggregation` larger (about 190 bytes instead of 80), still one allocation.
- An `Aggregation` belongs to one request and one goroutine and has no lock. `Operator` values are immutable and can be shared.

## Benchmarks

Benchmark results on darwin/amd64 (Intel Core i7-9750H @ 2.60GHz), Go 1.27.4, median of 5 runs:

| Benchmark | ns/op | B/op | allocs/op |
|-----------|-------|------|-----------|
| BenchmarkNewAggregation | 4.272 | 0 | 0 |
| BenchmarkAggregate_NoOperators | 2.746 | 0 | 0 |
| BenchmarkAggregate_Transform | 6.142 | 0 | 0 |
| BenchmarkAggregate_AllOperators | 11.01 | 0 | 0 |
| BenchmarkPipe_Two | 14.04 | 0 | 0 |
| BenchmarkAggregate_Filter | 4.888 | 0 | 0 |
| BenchmarkAggregate_Filter_False | 4.273 | 0 | 0 |
| BenchmarkAggregate_MultipleFilters | 6.821 | 0 | 0 |
| BenchmarkAggregate_Filter_And_Transform | 7.738 | 0 | 0 |
| BenchmarkInterceptorCycle | 17.41 | 0 | 0 |

*Note: These benchmarks are machine-dependent and were captured at documentation generation time (November 16, 2026).*
