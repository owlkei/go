package owlkei_test

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/exception"
	"go.owlkei.com/owlkei/internal/test"
	"go.owlkei.com/owlkei/log"
	"golang.org/x/net/websocket"
)

func exportedMethods(typ reflect.Type) []string {
	var names []string
	for i := 0; i < typ.NumMethod(); i++ {
		names = append(names, typ.Method(i).Name)
	}
	sort.Strings(names)
	return names
}

func exportedFields(typ reflect.Type) []string {
	var names []string
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).IsExported() {
			names = append(names, typ.Field(i).Name)
		}
	}
	sort.Strings(names)
	return names
}

func assertNoMethods(t *testing.T, typ reflect.Type, rule string, forbidden ...string) {
	t.Helper()
	for _, name := range forbidden {
		if _, ok := typ.MethodByName(name); ok {
			t.Error(test.DiffMessage(name, "<absent>", rule+": "+typ.String()+" must not have method "+name))
		}
	}
}

type pointerCatchFilter struct{}

func (pointerCatchFilter) Catch(c owlkei.HTTPContext, e *exception.Exception) {
	c.Status(http.StatusTeapot).JSON(owlkei.Map{"caught": true})
}

type pointerCatchController struct {
	owlkei.HTTP
	owlkei.ExceptionFilter
}

func (instance pointerCatchController) NewController() owlkei.Controller {
	instance.BindExceptionFilter(pointerCatchFilter{}, instance.READ_boom)
	return instance
}

func (instance pointerCatchController) READ_boom() string {
	panic(exception.BadRequestException("boom"))
}

const modulePath = "go.owlkei.com/owlkei"

var movedPackages = map[string]bool{
	"common":       true,
	"ctx":          true,
	"core":         true,
	"pattern":      true,
	"devtool":      true,
	"wsevent":      true,
	"routing":      true,
	"memorybroker": true,
	"memorycache":  true,
	"accesslog":    true,
	"trace":        true,
	"aggregation":  true,
	"versioning":   true,
}

func forbiddenImport(path string) bool {
	if !strings.HasPrefix(path, modulePath+"/") {
		return false
	}
	elements := strings.Split(strings.TrimPrefix(path, modulePath+"/"), "/")
	for _, element := range elements {
		if element == "internal" {
			return true
		}
	}
	return movedPackages[elements[0]]
}

func collectGoFiles(t *testing.T, root string, recursive bool) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && !recursive {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

type loggerProvider struct {
	Logger log.Logger
}

func (instance loggerProvider) NewProvider() owlkei.Provider { return instance }

type loggerGuard struct {
	Logger log.Logger
}

var guardLogger log.Logger

func (instance loggerGuard) CanActivate(c owlkei.HTTPContext) bool {
	guardLogger = instance.Logger
	return true
}

type injectedController struct {
	owlkei.HTTP
	owlkei.Guard
	Logger    log.Logger
	Publisher owlkei.Publisher
	Provider  loggerProvider
}

var (
	controllerLogger    log.Logger
	providerLogger      log.Logger
	controllerPublisher owlkei.Publisher
)

func (instance injectedController) NewController() owlkei.Controller {
	instance.BindGuard(loggerGuard{}, instance.READ_fields)
	return instance
}

func (instance injectedController) READ_fields() string {
	controllerLogger = instance.Logger
	providerLogger = instance.Provider.Logger
	controllerPublisher = instance.Publisher
	return "ok"
}

func injectedApp(t *testing.T) (owlkei.App, log.Logger) {
	t.Helper()
	logger := log.NewLog(&log.LogOptions{Level: log.LevelError})
	app := owlkei.New(&owlkei.AppOptions{Logger: logger})
	app.Create(owlkei.ModuleBuilder().Providers(loggerProvider{}).Controllers(injectedController{}).Build())
	return app, logger
}

type paramsSeen struct {
	hasContext, hasNext, hasRedirect, hasPublisher bool
	query, header, param                           string
	body                                           owlkei.Body
	form                                           owlkei.Form
	fileNames                                      []string
}

var seen paramsSeen

type paramsController struct {
	owlkei.HTTP
}

func (instance paramsController) NewController() owlkei.Controller { return instance }

func (instance paramsController) READ_inspect_BY_id(
	c owlkei.HTTPContext,
	query owlkei.Query,
	header owlkei.Header,
	param owlkei.Param,
	next owlkei.Next,
	publisher owlkei.Publisher,
) string {
	seen.hasContext = c != nil
	seen.hasNext = next != nil
	seen.hasPublisher = publisher != nil
	seen.query = query.Get("q")
	seen.header = header.Get("X-Probe")
	seen.param = param.Get("id")
	return "ok"
}

func (instance paramsController) CREATE_body(body owlkei.Body) string {
	seen.body = body
	return "ok"
}

func (instance paramsController) CREATE_upload(form owlkei.Form, file owlkei.File) string {
	seen.form = form
	for _, headers := range file {
		for _, h := range headers {
			seen.fileNames = append(seen.fileNames, h.Filename)
		}
	}
	return "ok"
}

func (instance paramsController) READ_away(redirect owlkei.Redirect) {
	seen.hasRedirect = redirect != nil
	redirect("/elsewhere")
}

type wsSeen struct {
	hasContext, hasPublisher, hasNext bool
	payload                           owlkei.WSPayload
}

var wsSeenCh = make(chan wsSeen, 4)

type wsParamsGateway struct {
	owlkei.WS
}

func (instance wsParamsGateway) NewController() owlkei.Controller { return instance }

func (instance wsParamsGateway) SUBSCRIBE_probe(c owlkei.WSContext, payload owlkei.WSPayload, publisher owlkei.Publisher, next owlkei.Next) string {
	wsSeenCh <- wsSeen{hasContext: c != nil, hasPublisher: publisher != nil, hasNext: next != nil, payload: payload}
	return "ok"
}

type greeter struct {
	Word string
}

func (instance greeter) NewProvider() owlkei.Provider { return instance }

type greetController struct {
	owlkei.HTTP
	Greeter greeter
}

func (instance greetController) NewController() owlkei.Controller { return instance }

func (instance greetController) READ_greet() string { return instance.Greeter.Word }

var greeterModule = func() owlkei.Module {
	m := owlkei.ModuleBuilder().Providers(greeter{Word: "hi"}).Build()
	m.IsGlobal = true
	return m
}

var funcModule = func(g greeter) owlkei.Module {
	return owlkei.ModuleBuilder().Controllers(greetController{}).Build()
}

var funcModuleCalls int

var countedModule = func() owlkei.Module {
	funcModuleCalls++
	return owlkei.ModuleBuilder().Build()
}

var namedModule = func() owlkei.Module {
	return owlkei.ModuleBuilder().Build()
}

type wrongGuard struct{}

func (wrongGuard) NewGuard() wrongGuard { return wrongGuard{} }

func (wrongGuard) CanActivate(string) bool { return true }

func (wrongGuard) NewController() owlkei.Controller { return nil }

type wrongShapeController struct {
	owlkei.HTTP
	owlkei.Guard
}

func (c wrongShapeController) NewController() owlkei.Controller {
	c.BindGuard(wrongGuard{}, c.READ_x)
	return c
}

func (wrongShapeController) READ_x() string { return "x" }

type wrongInterceptor struct{}

func (wrongInterceptor) NewInterceptor() wrongInterceptor { return wrongInterceptor{} }

func (wrongInterceptor) Intercept(string) any { return nil }

type wrongFilter struct{}

func (wrongFilter) NewExceptionFilter() wrongFilter { return wrongFilter{} }

func (wrongFilter) Catch(string) {}

type wrongHTTPInterceptorController struct {
	owlkei.HTTP
	owlkei.Interceptor
}

func (c wrongHTTPInterceptorController) NewController() owlkei.Controller {
	c.BindInterceptor(wrongInterceptor{}, c.READ_x)
	return c
}

func (wrongHTTPInterceptorController) READ_x() string { return "x" }

type wrongHTTPFilterController struct {
	owlkei.HTTP
	owlkei.ExceptionFilter
}

func (c wrongHTTPFilterController) NewController() owlkei.Controller {
	c.BindExceptionFilter(wrongFilter{}, c.READ_x)
	return c
}

func (wrongHTTPFilterController) READ_x() string { return "x" }

type wrongWSGuardController struct {
	owlkei.WS
	owlkei.Guard
}

func (c wrongWSGuardController) NewController() owlkei.Controller {
	c.BindGuard(wrongGuard{}, c.SUBSCRIBE_x)
	return c
}

func (wrongWSGuardController) SUBSCRIBE_x() string { return "x" }

type wrongWSInterceptorController struct {
	owlkei.WS
	owlkei.Interceptor
}

func (c wrongWSInterceptorController) NewController() owlkei.Controller {
	c.BindInterceptor(wrongInterceptor{}, c.SUBSCRIBE_x)
	return c
}

func (wrongWSInterceptorController) SUBSCRIBE_x() string { return "x" }

type wrongWSFilterController struct {
	owlkei.WS
	owlkei.ExceptionFilter
}

func (c wrongWSFilterController) NewController() owlkei.Controller {
	c.BindExceptionFilter(wrongFilter{}, c.SUBSCRIBE_x)
	return c
}

func (wrongWSFilterController) SUBSCRIBE_x() string { return "x" }

type surfaceBooking struct {
	Nights string `bind:"nights"`
	Errors map[string]string
}

func (surfaceBooking) Transform(f owlkei.Form, _ owlkei.ArgumentMetadata) any {
	bound, _ := f.Bind(surfaceBooking{})
	form := bound.(surfaceBooking)
	form.Errors = map[string]string{}
	if form.Nights == "" {
		form.Errors["nights"] = "Enter the nights"
	}
	return form
}

type surfaceBookingPages struct {
	owlkei.HTTP
}

func (instance surfaceBookingPages) NewController() owlkei.Controller { return instance }

func (instance surfaceBookingPages) RENDER_bookings() owlkei.Map {
	return owlkei.Map{"Bookings": []string{"one", "two"}}
}

func (instance surfaceBookingPages) RENDER_bookings_BY_id(param owlkei.Param) surfaceBooking {
	if param.Get("id") != "1" {
		panic(exception.NotFoundException("No booking has this number"))
	}
	return surfaceBooking{Nights: "3"}
}

func (instance surfaceBookingPages) RENDER_new_OF_bookings() surfaceBooking {
	return surfaceBooking{Nights: "1"}
}

func (instance surfaceBookingPages) SUBMIT_new_OF_bookings(c owlkei.HTTPContext, form surfaceBooking) surfaceBooking {
	if len(form.Errors) == 0 {
		c.Redirect("/bookings")
	}
	return form
}

type tokenController struct {
	owlkei.HTTP
}

func (instance tokenController) NewController() owlkei.Controller {
	instance.Path("/oauth2/token", instance.CREATE_token)
	return instance
}

func (instance tokenController) CREATE_token() owlkei.Map { return owlkei.Map{"token": "t"} }

func (instance tokenController) READ_payment_intents() []string { return []string{"pi"} }

var billingModule = owlkei.ModuleBuilder().
	PathStyle(owlkei.PathStyleKebab).
	Controllers(billingController{}).
	Build()

type billingController struct {
	owlkei.HTTP
}

func (instance billingController) NewController() owlkei.Controller { return instance }

func (instance billingController) READ_invoice_lines() string { return "lines" }

func serve(app http.Handler, method, target string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, body)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

type helloController struct {
	owlkei.HTTP
}

func (instance helloController) NewController() owlkei.Controller { return instance }

func (instance helloController) READ_hello() string { return "hello" }

var chatCalls atomic.Int32

type chatGateway struct {
	owlkei.WS
}

func (instance chatGateway) NewController() owlkei.Controller { return instance }

func (instance chatGateway) SUBSCRIBE_chat_ANY() string {
	chatCalls.Add(1)
	return "ok"
}

type wireMessage struct {
	Type  string   `json:"type"`
	ID    string   `json:"id"`
	Topic []string `json:"topic,omitempty"`
	Msg   any      `json:"message"`
}

func receiveUntil(t *testing.T, conn *websocket.Conn, id string) wireMessage {
	t.Helper()
	for {
		if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		var m wireMessage
		if err := websocket.JSON.Receive(conn, &m); err != nil {
			t.Fatalf("receive reply %q: %v", id, err)
		}
		if m.ID == id && m.Type != "ping" && m.Type != "event" {
			return m
		}
	}
}

var layerCalls = struct{ guard, interceptor, filter, middleware atomic.Int32 }{}

type layerGuard struct{}

func (instance layerGuard) CanActivate(c owlkei.HTTPContext) bool {
	layerCalls.guard.Add(1)
	return c.Query().Get("deny") != "true"
}

type layerInterceptor struct{}

func (instance layerInterceptor) Intercept(c owlkei.HTTPContext, a owlkei.Aggregation) any {
	layerCalls.interceptor.Add(1)
	return a.Pipe(a.Transform(func(data any) any {
		return owlkei.Map{"wrapped": data}
	}))
}

type layerFilter struct{}

func (instance layerFilter) Catch(c owlkei.HTTPContext, e *exception.Exception) {
	layerCalls.filter.Add(1)
	c.Status(http.StatusTeapot)
	c.Text("filtered")
}

type layerMiddleware struct{}

func (instance layerMiddleware) Use(r *http.Request, w http.ResponseWriter, next owlkei.Next) {
	layerCalls.middleware.Add(1)
	w.Header().Set("X-Layer", "on")
	next()
}

type layersController struct {
	owlkei.HTTP
	owlkei.Guard
	owlkei.Interceptor
	owlkei.ExceptionFilter
	owlkei.Middleware
}

func (instance layersController) NewController() owlkei.Controller {
	instance.BindGuard(layerGuard{}, instance.READ_bound)
	instance.BindInterceptor(layerInterceptor{}, instance.READ_bound)
	instance.BindExceptionFilter(layerFilter{}, instance.READ_bound)
	instance.BindMiddleware(layerMiddleware{}, instance.READ_bound)
	return instance
}

func (instance layersController) READ_bound() string {
	panic(exception.BadRequestException("boom"))
}

func (instance layersController) READ_plain() string { return "plain" }

type wrapController struct {
	owlkei.HTTP
	owlkei.Interceptor
}

func (instance wrapController) NewController() owlkei.Controller {
	instance.BindInterceptor(layerInterceptor{}, instance.READ_wrapped)
	return instance
}

func (instance wrapController) READ_wrapped() string { return "x" }

func (instance wrapController) READ_bare() string { return "y" }
