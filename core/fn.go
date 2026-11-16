package core

import (
	"errors"
	"fmt"
	"go/token"
	"net"
	"net/http"
	"os"
	"path"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dangduoc08/ginject/event"
	"github.com/dangduoc08/ginject/internal/color"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/trace"
	"golang.org/x/net/websocket"
)

var pkgFromControllerKeyReg = regexp.MustCompile(`\[.*?\]`)

func isDynamicModule(moduleType string) bool {
	return strings.HasPrefix(moduleType, "func(") && strings.HasSuffix(moduleType, "*core.Module")
}

func isInjectedProvider(providerFieldType reflect.Type) bool {
	instance := reflect.New(providerFieldType)
	_, ok := instance.Interface().(Provider)
	return ok
}

func genProviderKey(p Provider) string {
	return genFieldKey(reflect.TypeOf(p))
}

func genControllerKey(m *Module, c Controller) string {
	return "[" + m.ID() + "]" + genFieldKey(reflect.TypeOf(c))
}

func getPkgFromControllerKey(k string) string {
	return pkgFromControllerKeyReg.ReplaceAllString(k, "")
}

func genFieldKey(t reflect.Type) string {
	return t.PkgPath() + "/" + t.String()
}

func createStaticModuleFromDynamicModule(dynamicModule any) *Module {
	dynamicModuleType := reflect.TypeOf(dynamicModule)
	var args []reflect.Value

	genError := func(dynamicModuleType reflect.Type, dynamicArgKey string, index int) error {
		return errors.New(
			color.FmtRed(
				"dependency injection: can't resolve argument of '%v'. Please make sure that the argument '%v' at index [%v] is available in the injected providers",
				strings.Replace(dynamicModuleType.String(), ") *core.Module", ")", 1),
				dynamicArgKey,
				index,
			),
		)
	}

	for i := 0; i < dynamicModuleType.NumIn(); i++ {
		dynamicArgKey := genFieldKey(dynamicModuleType.In(i))
		if p, ok := globalProviderByKey.Load(dynamicArgKey); ok {
			args = append(args, reflect.ValueOf(p))
		} else if iface, ok := globalInterfaceByKey.Load(dynamicArgKey); ok {
			args = append(args, reflect.ValueOf(iface))
		} else {
			panic(genError(dynamicModuleType, dynamicArgKey, i))
		}
	}

	return reflect.ValueOf(dynamicModule).Call(args)[0].Interface().(*Module)
}

func injectDependencies(component any, kind string, injectedProviders map[string]Provider) (reflect.Value, error) {
	componentType := reflect.TypeOf(component)
	componentValue := reflect.ValueOf(component)
	newComponent := reflect.New(componentType)

	componentName := path.Base(componentType.PkgPath()) + "." + componentType.Name()
	for j := 0; j < componentType.NumField(); j++ {
		componentField := componentType.Field(j)
		componentFieldType := componentField.Type
		componentFieldKey := genFieldKey(componentFieldType)
		componentFieldName := componentField.Name

		if !token.IsExported(componentFieldName) {
			panic(errors.New(
				color.FmtRed(
					"dependency injection: can't set value to unexported '%v' field of the %v %v",
					componentFieldName,
					componentName,
					kind,
				),
			))
		}

		if componentFieldKey != "" && injectedProviders[componentFieldKey] != nil {
			newComponent.Elem().Field(j).Set(reflect.ValueOf(injectedProviders[componentFieldKey]))
		} else if p, ok := globalProviderByKey.Load(componentFieldKey); componentFieldKey != "" && ok {
			newComponent.Elem().Field(j).Set(reflect.ValueOf(p))
		} else if iface, ok := globalInterfaceByKey.Load(componentFieldKey); componentFieldKey != "" && ok {
			newComponent.Elem().Field(j).Set(reflect.ValueOf(iface))
		} else if !isInjectedProvider(componentFieldType) {

			newComponent.Elem().Field(j).Set(componentValue.Field(j))
		} else {
			return reflect.ValueOf(nil), errors.New(
				color.FmtRed(
					"dependency injection: can't resolve dependency '%v' of the %v. Please make sure that the argument dependency at index [%v] is available in the '%v' %v",
					componentFieldType.String(),
					kind,
					j,
					componentName,
					kind,
				),
			)
		}
	}

	return newComponent, nil
}

func buildFieldInjectionCallback(kind string, injectedProviders map[string]Provider) func(int, reflect.Type, reflect.Value, reflect.Value) {
	return func(i int, ownerType reflect.Type, ownerValue, newInstance reflect.Value) {
		field := ownerType.Field(i)
		fieldType := field.Type
		fieldName := field.Name
		injectProviderKey := genFieldKey(fieldType)

		if !token.IsExported(fieldName) {
			panic(errors.New(
				color.FmtRed(
					"dependency injection: can't set value to unexported '%v' field of the '%v' %v",
					fieldName,
					ownerType.Name(),
					kind,
				),
			))
		}

		if injectedProviders[injectProviderKey] != nil {
			newInstance.Elem().Field(i).Set(reflect.ValueOf(injectedProviders[injectProviderKey]))
		} else if p, ok := globalProviderByKey.Load(injectProviderKey); ok {
			newInstance.Elem().Field(i).Set(reflect.ValueOf(p))
		} else if iface, ok := globalInterfaceByKey.Load(injectProviderKey); ok {
			newInstance.Elem().Field(i).Set(reflect.ValueOf(iface))
		} else if !isInjectedProvider(fieldType) {
			newInstance.Elem().Field(i).Set(ownerValue.Field(i))
		} else {
			panic(errors.New(
				color.FmtRed(
					"dependency injection: can't resolve dependencies of the '%v' provider. Please make sure that the argument dependency at index [%v] is available in the '%v' %v",
					fieldType.String(),
					i,
					ownerType.Name(),
					kind,
				),
			))
		}
	}
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

func logBoostrap(port int) {
	accessURLs := color.FmtBold("%s", color.FmtBGYellow(" GG! Here Are Your Access URLs: ")) + "\n"
	divider := color.FmtDim("--------------------------------------------") + "\n"
	host := color.FmtBold("%s", color.FmtWhite("Localhost: ")) + color.FmtMagenta("%v:%v", "localhost", port) + "\n"
	lan := color.FmtBold("%s", color.FmtWhite("      LAN: ")) + color.FmtMagenta("%v:%v", getLocalIP(), port) + "\n"
	close := color.FmtItalic("%s", color.FmtGreen("Press CTRL+C to stop")) + "\n"

	_, _ = fmt.Fprint(os.Stdout, "\n"+accessURLs+divider+host+lan+divider+close)
}

func returnHTTP(c *ctx.HTTPContext, data reflect.Value) {
	switch data.Kind() {
	case reflect.Interface:
		if elem := data.Elem(); elem.Kind() == reflect.String {
			c.Text(elem.String())
			return
		}
		c.JSON(data.Interface())
	case
		reflect.Map,
		reflect.Slice,
		reflect.Struct,
		reflect.Array,
		reflect.Pointer:
		c.JSON(data.Interface())
	case reflect.Bool:
		c.Text(strconv.FormatBool(data.Bool()))
	case
		reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64:
		c.Text(strconv.FormatInt(data.Int(), 10))
	case
		reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64:
		c.Text(strconv.FormatUint(data.Uint(), 10))
	case reflect.Float32:
		c.Text(strconv.FormatFloat(data.Float(), 'g', -1, 32))
	case reflect.Float64:
		c.Text(strconv.FormatFloat(data.Float(), 'g', -1, 64))
	case reflect.Complex64:
		c.Text(strconv.FormatComplex(data.Complex(), 'g', -1, 64))
	case reflect.Complex128:
		c.Text(strconv.FormatComplex(data.Complex(), 'g', -1, 128))
	case reflect.String:
		c.Text(data.String())
	case reflect.Func:
		c.Text(data.Type().String())
	}
}

func setStatusCode(c *ctx.HTTPContext, statusCode reflect.Value) {
	switch statusCode.Kind() {
	case reflect.Int:
		status := int(statusCode.Int())
		if http.StatusText(status) != "" {
			c.Status(status)
		}
	case reflect.Interface:
		if status, ok := statusCode.Interface().(int); ok &&
			http.StatusText(status) != "" {
			c.Status(status)
		}
	}
}

func toUniqueControllers(module *Module, controllers *[]Controller) {
	seen := make(map[string]struct{}, len(*controllers))
	uniqueControllers := make([]Controller, 0, len(*controllers))
	for _, controller := range *controllers {
		controllerKey := genControllerKey(module, controller)
		if _, ok := seen[controllerKey]; !ok {
			seen[controllerKey] = struct{}{}
			uniqueControllers = append(uniqueControllers, controller)
		}
	}

	*controllers = uniqueControllers
}

func buildUseMiddleware(useFn common.Use, event *event.Event, name, transport string) ctx.HTTPHandler {
	return func(c *ctx.HTTPContext) {
		defer func() {
			if rec := recover(); rec != nil {
				globalHTTPExceptionFilter{}.Catch(c, common.NormalizeRecovered(rec))
			}
		}()

		next := func() {
			c.Next()
		}

		if !event.HasListeners(trace.EventName) {
			useFn(c.Request, c.ResponseWriter, next)
			return
		}

		start := time.Now()
		useFn(c.Request, c.ResponseWriter, next)
		event.Emit(trace.EventName, trace.Event{ID: c.GetID(), Stage: trace.StageMiddleware, Name: name, Transport: transport, Duration: time.Since(start)})
	}
}

func traceHTTPHandler(ev *event.Event, stage, name string, h ctx.HTTPHandler) ctx.HTTPHandler {
	return func(c *ctx.HTTPContext) {
		if !ev.HasListeners(trace.EventName) {
			h(c)
			return
		}

		start := time.Now()
		defer func() {
			ev.Emit(trace.EventName, trace.Event{ID: c.GetID(), Stage: stage, Name: name, Duration: time.Since(start)})
		}()
		h(c)
	}
}

func traceHTTPCatch(ev *event.Event, name string, fn common.HTTPCatch) common.HTTPCatch {
	return func(c *ctx.HTTPContext, ex *exception.Exception) {
		if !ev.HasListeners(trace.EventName) {
			fn(c, ex)
			return
		}

		start := time.Now()
		defer func() {
			ev.Emit(trace.EventName, trace.Event{ID: c.GetID(), Stage: trace.StageExceptionFilter, Name: name, Duration: time.Since(start)})
		}()
		fn(c, ex)
	}
}

func traceWSHandler(ev *event.Event, stage, name string, h ctx.WSHandler) ctx.WSHandler {
	return func(c *ctx.WSContext) {
		if !ev.HasListeners(trace.EventName) {
			h(c)
			return
		}

		start := time.Now()
		defer func() {
			ev.Emit(trace.EventName, trace.Event{ID: c.GetID(), Stage: stage, Name: name, Transport: trace.TransportWS, Duration: time.Since(start)})
		}()
		h(c)
	}
}

func traceWSCatch(ev *event.Event, name string, fn common.WSCatch) common.WSCatch {
	return func(c *ctx.WSContext, ex *exception.Exception) {
		if !ev.HasListeners(trace.EventName) {
			fn(c, ex)
			return
		}

		start := time.Now()
		defer func() {
			ev.Emit(trace.EventName, trace.Event{ID: c.GetID(), Stage: trace.StageExceptionFilter, Name: name, Transport: trace.TransportWS, Duration: time.Since(start)})
		}()
		fn(c, ex)
	}
}

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

func (p *handlerPipe[C]) resolveTraced(c C, ev *event.Event, transport string, elapsed *time.Duration) reflect.Value {
	start := time.Now()
	defer func() {
		d := time.Since(start)
		*elapsed += d
		ev.Emit(trace.EventName, trace.Event{ID: c.GetID(), Stage: trace.StagePipe, Name: p.name, Transport: transport, Duration: d})
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

func (s handlerSignature) publisher(i int) reflect.Value {
	p, ok := globalInterfaceByKey.Load(publisherKey)
	if !ok {
		panic(s.unresolved(i))
	}
	if p == nil {
		return reflect.Zero(publisherType)
	}
	return reflect.ValueOf(p)
}

func newHandlerArg[C ctx.Context](s handlerSignature, i int, kindByType map[reflect.Type]handlerArgKind, pipeKinds []pipeKind[C], contextType string, injectedProviders map[string]Provider) handlerArg[C] {
	t := s.method.Type.In(i + 1)
	if s.method.Type.IsVariadic() && i == s.numIn()-1 {
		panic(s.unresolved(i))
	}
	if kind, ok := kindByType[t]; ok {
		return handlerArg[C]{kind: kind}
	}
	if t == publisherType {
		return handlerArg[C]{kind: handlerArgFixed, fixed: s.publisher(i)}
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
			pipe: handlerPipe[C]{typ: t, name: t.String(), transform: pk.bind(newPipePrototype(t, injectedProviders), meta)},
		}
	}
	panic(s.unresolved(i))
}

func newPipePrototype(t reflect.Type, injectedProviders map[string]Provider) any {
	if t.Kind() != reflect.Struct {
		return reflect.Zero(t).Interface()
	}
	_, _ = ctx.BinderOf(t)
	prototype, err := injectDependencies(reflect.Zero(t).Interface(), "pipe", injectedProviders)
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

func newHTTPHandlerPlan(recv reflect.Value, method reflect.Method, injectedProviders map[string]Provider) *httpHandlerPlan {
	s := newHandlerSignature(recv, method)
	p := &httpHandlerPlan{fn: method.Func, recv: recv, name: method.Name, args: make([]handlerArg[*ctx.HTTPContext], s.numIn())}
	for i := range p.args {
		p.args[i] = newHandlerArg(s, i, httpArgKindByType, httpPipeKinds, common.ContextTypeHTTP, injectedProviders)
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
		return reflect.ValueOf(c.Next)
	case handlerArgFixed:
		return a.fixed
	}
	return a.pipe.resolve(c)
}

func (p *httpHandlerPlan) invoke(c *ctx.HTTPContext, ev *event.Event) []reflect.Value {
	var stack [handlerStackArgs + 1]reflect.Value
	in := handlerCallArgs(stack[:], p.recv, len(p.args))
	if ev.HasListeners(trace.EventName) {
		return p.invokeTraced(c, ev, in)
	}
	for i := range p.args {
		in[i+1] = resolveHTTPArg(&p.args[i], c)
	}
	return p.fn.Call(in)
}

func (p *httpHandlerPlan) invokeTraced(c *ctx.HTTPContext, ev *event.Event, in []reflect.Value) []reflect.Value {
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
		ev.Emit(trace.EventName, trace.Event{ID: c.GetID(), Stage: trace.StageHandler, Name: p.name, Duration: time.Since(start) - pipes})
	}()
	return p.fn.Call(in)
}

type wsHandlerPlan struct {
	fn   reflect.Value
	recv reflect.Value
	args []handlerArg[*ctx.WSContext]
}

func newWSHandlerPlan(recv reflect.Value, method reflect.Method, injectedProviders map[string]Provider) *wsHandlerPlan {
	s := newHandlerSignature(recv, method)
	p := &wsHandlerPlan{fn: method.Func, recv: recv, args: make([]handlerArg[*ctx.WSContext], s.numIn())}
	for i := range p.args {
		p.args[i] = newHandlerArg(s, i, wsArgKindByType, wsPipeKinds, common.ContextTypeWS, injectedProviders)
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
		return reflect.ValueOf(c.Next)
	case handlerArgFixed:
		return a.fixed
	}
	return a.pipe.resolve(c)
}

func (p *wsHandlerPlan) invoke(c *ctx.WSContext, ev *event.Event) []reflect.Value {
	var stack [handlerStackArgs + 1]reflect.Value
	in := handlerCallArgs(stack[:], p.recv, len(p.args))
	if ev.HasListeners(trace.EventName) {
		return p.invokeTraced(c, ev, in)
	}
	for i := range p.args {
		in[i+1] = resolveWSArg(&p.args[i], c)
	}
	return p.fn.Call(in)
}

func (p *wsHandlerPlan) invokeTraced(c *ctx.WSContext, ev *event.Event, in []reflect.Value) []reflect.Value {
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
