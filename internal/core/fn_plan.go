package core

import (
	"errors"
	"net/http"
	"reflect"
	"time"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/trace"
	"github.com/dangduoc08/ginject/internal/tracecontext"
	"golang.org/x/net/websocket"
)

const handlerStackArgs = 8

var (
	httpContextType    = reflect.TypeFor[*ctx.HTTPContext]()
	requestType        = reflect.TypeFor[*http.Request]()
	responseWriterType = reflect.TypeFor[http.ResponseWriter]()
	bodyType           = reflect.TypeFor[ctx.Body]()
	formType           = reflect.TypeFor[ctx.Form]()
	queryType          = reflect.TypeFor[ctx.Query]()
	headerType         = reflect.TypeFor[ctx.Header]()
	paramType          = reflect.TypeFor[ctx.Param]()
	fileType           = reflect.TypeFor[ctx.File]()
	redirectType       = reflect.TypeFor[ctx.Redirect]()
	wsContextType      = reflect.TypeFor[*ctx.WSContext]()
	wsConnType         = reflect.TypeFor[*websocket.Conn]()
	wsPayloadType      = reflect.TypeFor[ctx.WSPayload]()
	nextType           = reflect.TypeFor[ctx.Next]()
	publisherType      = reflect.TypeFor[common.Publisher]()

	nilResponseWriter = reflect.Zero(responseWriterType)
)

type pipeKind[C any] struct {
	typ  reflect.Type
	key  string
	bind func(prototype any, meta common.ArgumentMetadata) func(C) any
}

var httpPipeKinds = []pipeKind[*ctx.HTTPContext]{
	{reflect.TypeFor[common.ContextPipeable](), common.ContextPipeableKey, func(prototype any, meta common.ArgumentMetadata) func(*ctx.HTTPContext) any {
		p := prototype.(common.ContextPipeable)
		return func(c *ctx.HTTPContext) any { return p.Transform(c, meta) }
	}},
	{reflect.TypeFor[common.BodyPipeable](), common.BodyPipeableKey, func(prototype any, meta common.ArgumentMetadata) func(*ctx.HTTPContext) any {
		p := prototype.(common.BodyPipeable)
		return func(c *ctx.HTTPContext) any { return p.Transform(c.Body(), meta) }
	}},
	{reflect.TypeFor[common.FormPipeable](), common.FormPipeableKey, func(prototype any, meta common.ArgumentMetadata) func(*ctx.HTTPContext) any {
		p := prototype.(common.FormPipeable)
		return func(c *ctx.HTTPContext) any { return p.Transform(c.Form(), meta) }
	}},
	{reflect.TypeFor[common.QueryPipeable](), common.QueryPipeableKey, func(prototype any, meta common.ArgumentMetadata) func(*ctx.HTTPContext) any {
		p := prototype.(common.QueryPipeable)
		return func(c *ctx.HTTPContext) any { return p.Transform(c.Query(), meta) }
	}},
	{reflect.TypeFor[common.HeaderPipeable](), common.HeaderPipeableKey, func(prototype any, meta common.ArgumentMetadata) func(*ctx.HTTPContext) any {
		p := prototype.(common.HeaderPipeable)
		return func(c *ctx.HTTPContext) any { return p.Transform(c.Header(), meta) }
	}},
	{reflect.TypeFor[common.ParamPipeable](), common.ParamPipeableKey, func(prototype any, meta common.ArgumentMetadata) func(*ctx.HTTPContext) any {
		p := prototype.(common.ParamPipeable)
		return func(c *ctx.HTTPContext) any { return p.Transform(c.Param(), meta) }
	}},
	{reflect.TypeFor[common.FilePipeable](), common.FilePipeableKey, func(prototype any, meta common.ArgumentMetadata) func(*ctx.HTTPContext) any {
		p := prototype.(common.FilePipeable)
		return func(c *ctx.HTTPContext) any { return p.Transform(c.File(), meta) }
	}},
}

var wsPipeKinds = []pipeKind[*ctx.WSContext]{
	{reflect.TypeFor[common.WSPayloadPipeable](), common.WSPayloadPipeableKey, func(prototype any, meta common.ArgumentMetadata) func(*ctx.WSContext) any {
		p := prototype.(common.WSPayloadPipeable)
		return func(c *ctx.WSContext) any { return p.Transform(c.WSPayload(), meta) }
	}},
}

type handlerArgKind uint8

const (
	handlerArgContext handlerArgKind = iota
	handlerArgRequest
	handlerArgResponseWriter
	handlerArgBody
	handlerArgForm
	handlerArgQuery
	handlerArgHeader
	handlerArgParam
	handlerArgFile
	handlerArgRedirect
	handlerArgConn
	handlerArgWSPayload
	handlerArgNext
	handlerArgFixed
	handlerArgPipe
)

type handlerPipe[C ctx.Context] struct {
	typ       reflect.Type
	name      string
	transform func(C) any
}

func (p *handlerPipe[C]) resolve(c C) reflect.Value {
	out := p.transform(c)
	if out == nil {
		panic(exception.InternalServerErrorException("pipe " + p.name + ": Transform returned nil, want " + p.name))
	}
	v := reflect.ValueOf(out)
	if t := v.Type(); t != p.typ && !t.AssignableTo(p.typ) {
		panic(exception.InternalServerErrorException("pipe " + p.name + ": Transform returned " + t.String() + ", want " + p.name))
	}
	return v
}

func (p *handlerPipe[C]) resolveTraced(c C, ev *trace.Tracer, transport string, elapsed *time.Duration) reflect.Value {
	start := time.Now()
	defer func() {
		d := time.Since(start)
		*elapsed += d
		ev.Emit(stageEvent(tracecontext.ScopeOf(c.Context()), c.ID(), trace.StagePipe, p.name, transport, d))
	}()
	return p.resolve(c)
}

type handlerArg[C ctx.Context] struct {
	kind  handlerArgKind
	fixed reflect.Value
	pipe  handlerPipe[C]
}

type handlerSignature struct {
	method   reflect.Method
	funcType string
}

func newHandlerSignature(recv reflect.Value, method reflect.Method) handlerSignature {
	return handlerSignature{method: method, funcType: recv.Method(method.Index).Type().String()}
}

func (s handlerSignature) numIn() int {
	return s.method.Type.NumIn() - 1
}

func (s handlerSignature) unresolved(i int) error {
	return errors.New(color.FmtRed(
		"dependency injection: can't resolve dependencies of the '%v'. Please make sure that the argument dependency at index [%v] is available in the handler",
		s.funcType,
		i,
	))
}

func (s handlerSignature) publisher(i int, c *container) reflect.Value {
	p, ok := c.interfaceFor(publisherKey)
	if !ok {
		panic(s.unresolved(i))
	}
	if p == nil {
		return reflect.Zero(publisherType)
	}
	return reflect.ValueOf(p)
}

func newHandlerArg[C ctx.Context](s handlerSignature, i int, kindByType map[reflect.Type]handlerArgKind, pipeKinds []pipeKind[C], contextType string, injectedProviders map[string]Provider, c *container) handlerArg[C] {
	t := s.method.Type.In(i + 1)
	if s.method.Type.IsVariadic() && i == s.numIn()-1 {
		panic(s.unresolved(i))
	}
	if kind, ok := kindByType[t]; ok {
		return handlerArg[C]{kind: kind}
	}
	if t == publisherType {
		return handlerArg[C]{kind: handlerArgFixed, fixed: s.publisher(i, c)}
	}
	if t.Kind() == reflect.Interface {
		panic(s.unresolved(i))
	}
	for _, pk := range pipeKinds {
		if !t.Implements(pk.typ) {
			continue
		}
		if t.Kind() == reflect.Pointer {
			panic(errors.New(color.FmtRed(
				"dependency injection: pipe '%v' of the '%v' must not be a pointer",
				t.String(),
				s.funcType,
			)))
		}
		meta := common.ArgumentMetadata{ContextType: contextType, ParamType: pk.key}
		return handlerArg[C]{
			kind: handlerArgPipe,
			pipe: handlerPipe[C]{typ: t, name: t.String(), transform: pk.bind(newPipePrototype(t, injectedProviders, c), meta)},
		}
	}
	panic(s.unresolved(i))
}

func newPipePrototype(t reflect.Type, injectedProviders map[string]Provider, c *container) any {
	if t.Kind() != reflect.Struct {
		return reflect.Zero(t).Interface()
	}
	_, _ = ctx.BinderOf(t)
	prototype, err := injectDependencies(reflect.Zero(t).Interface(), "pipe", injectedProviders, c)
	if err != nil {
		panic(err)
	}
	return prototype.Elem().Interface()
}

var httpArgKindByType = map[reflect.Type]handlerArgKind{
	httpContextType:    handlerArgContext,
	requestType:        handlerArgRequest,
	responseWriterType: handlerArgResponseWriter,
	bodyType:           handlerArgBody,
	formType:           handlerArgForm,
	queryType:          handlerArgQuery,
	headerType:         handlerArgHeader,
	paramType:          handlerArgParam,
	fileType:           handlerArgFile,
	redirectType:       handlerArgRedirect,
	nextType:           handlerArgNext,
}

var wsArgKindByType = map[reflect.Type]handlerArgKind{
	wsContextType: handlerArgContext,
	wsConnType:    handlerArgConn,
	wsPayloadType: handlerArgWSPayload,
	nextType:      handlerArgNext,
}

func handlerCallArgs(stack []reflect.Value, recv reflect.Value, numIn int) []reflect.Value {
	in := stack
	if numIn >= len(stack) {
		in = make([]reflect.Value, numIn+1)
	}
	in = in[:numIn+1]
	in[0] = recv
	return in
}

type httpHandlerPlan struct {
	fn   reflect.Value
	recv reflect.Value
	name string
	args []handlerArg[*ctx.HTTPContext]
}

func newHTTPHandlerPlan(recv reflect.Value, method reflect.Method, injectedProviders map[string]Provider, c *container) *httpHandlerPlan {
	s := newHandlerSignature(recv, method)
	p := &httpHandlerPlan{fn: method.Func, recv: recv, name: method.Name, args: make([]handlerArg[*ctx.HTTPContext], s.numIn())}
	for i := range p.args {
		p.args[i] = newHandlerArg(s, i, httpArgKindByType, httpPipeKinds, common.ContextTypeHTTP, injectedProviders, c)
	}
	return p
}

func resolveHTTPArg(a *handlerArg[*ctx.HTTPContext], c *ctx.HTTPContext) reflect.Value {
	switch a.kind {
	case handlerArgContext:
		return reflect.ValueOf(c)
	case handlerArgRequest:
		return reflect.ValueOf(c.Request)
	case handlerArgResponseWriter:
		if c.ResponseWriter == nil {
			return nilResponseWriter
		}
		return reflect.ValueOf(c.ResponseWriter)
	case handlerArgBody:
		return reflect.ValueOf(c.Body())
	case handlerArgForm:
		return reflect.ValueOf(c.Form())
	case handlerArgQuery:
		return reflect.ValueOf(c.Query())
	case handlerArgHeader:
		return reflect.ValueOf(c.Header())
	case handlerArgParam:
		return reflect.ValueOf(c.Param())
	case handlerArgFile:
		return reflect.ValueOf(c.File())
	case handlerArgRedirect:
		return reflect.ValueOf(c.Redirect)
	case handlerArgNext:
		return reflect.ValueOf(ctx.HTTPNext(c))
	case handlerArgFixed:
		return a.fixed
	}
	return a.pipe.resolve(c)
}

func (p *httpHandlerPlan) invoke(c *ctx.HTTPContext, ev *trace.Tracer) []reflect.Value {
	return p.call(c, ev, ev.Enabled())
}

func (p *httpHandlerPlan) call(c *ctx.HTTPContext, ev *trace.Tracer, traced bool) []reflect.Value {
	var stack [handlerStackArgs + 1]reflect.Value
	in := handlerCallArgs(stack[:], p.recv, len(p.args))
	if traced {
		return p.invokeTraced(c, ev, in)
	}
	for i := range p.args {
		in[i+1] = resolveHTTPArg(&p.args[i], c)
	}
	return p.fn.Call(in)
}

func (p *httpHandlerPlan) invokeTraced(c *ctx.HTTPContext, ev *trace.Tracer, in []reflect.Value) []reflect.Value {
	start := time.Now()
	var pipes time.Duration
	for i := range p.args {
		a := &p.args[i]
		if a.kind == handlerArgPipe {
			in[i+1] = a.pipe.resolveTraced(c, ev, trace.TransportHTTP, &pipes)
		} else {
			in[i+1] = resolveHTTPArg(a, c)
		}
	}
	defer func() {
		ev.Emit(stageEvent(tracecontext.ScopeOf(c.Context()), c.ID(), trace.StageHandler, p.name, trace.TransportHTTP, time.Since(start)-pipes))
	}()
	return p.fn.Call(in)
}

type wsHandlerPlan struct {
	fn   reflect.Value
	recv reflect.Value
	args []handlerArg[*ctx.WSContext]
}

func newWSHandlerPlan(recv reflect.Value, method reflect.Method, injectedProviders map[string]Provider, c *container) *wsHandlerPlan {
	s := newHandlerSignature(recv, method)
	p := &wsHandlerPlan{fn: method.Func, recv: recv, args: make([]handlerArg[*ctx.WSContext], s.numIn())}
	for i := range p.args {
		p.args[i] = newHandlerArg(s, i, wsArgKindByType, wsPipeKinds, common.ContextTypeWS, injectedProviders, c)
	}
	return p
}

func resolveWSArg(a *handlerArg[*ctx.WSContext], c *ctx.WSContext) reflect.Value {
	switch a.kind {
	case handlerArgContext:
		return reflect.ValueOf(c)
	case handlerArgConn:
		return reflect.ValueOf(c.Conn)
	case handlerArgWSPayload:
		return reflect.ValueOf(c.WSPayload())
	case handlerArgNext:
		return reflect.ValueOf(ctx.WSNext(c))
	case handlerArgFixed:
		return a.fixed
	}
	return a.pipe.resolve(c)
}

func (p *wsHandlerPlan) invoke(c *ctx.WSContext, ev *trace.Tracer) []reflect.Value {
	var stack [handlerStackArgs + 1]reflect.Value
	in := handlerCallArgs(stack[:], p.recv, len(p.args))
	if ev.Enabled() {
		return p.invokeTraced(c, ev, in)
	}
	for i := range p.args {
		in[i+1] = resolveWSArg(&p.args[i], c)
	}
	return p.fn.Call(in)
}

func (p *wsHandlerPlan) invokeTraced(c *ctx.WSContext, ev *trace.Tracer, in []reflect.Value) []reflect.Value {
	var pipes time.Duration
	for i := range p.args {
		a := &p.args[i]
		if a.kind == handlerArgPipe {
			in[i+1] = a.pipe.resolveTraced(c, ev, trace.TransportWS, &pipes)
		} else {
			in[i+1] = resolveWSArg(a, c)
		}
	}
	return p.fn.Call(in)
}
