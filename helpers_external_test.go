package ginject_test

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

	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/log"
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

func (pointerCatchFilter) Catch(c ginject.HTTPContext, e *exception.Exception) {
	c.Status(http.StatusTeapot).JSON(ginject.Map{"caught": true})
}

type pointerCatchController struct {
	ginject.HTTP
	ginject.ExceptionFilter
}

func (instance pointerCatchController) NewController() ginject.Controller {
	instance.BindExceptionFilter(pointerCatchFilter{}, instance.READ_boom)
	return instance
}

func (instance pointerCatchController) READ_boom() string {
	panic(exception.BadRequestException("boom"))
}

const modulePath = "github.com/dangduoc08/ginject"

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

func (instance loggerProvider) NewProvider() ginject.Provider { return instance }

type loggerGuard struct {
	Logger log.Logger
}

var guardLogger log.Logger

func (instance loggerGuard) CanActivate(c ginject.HTTPContext) bool {
	guardLogger = instance.Logger
	return true
}

type injectedController struct {
	ginject.HTTP
	ginject.Guard
	Logger    log.Logger
	Publisher ginject.Publisher
	Provider  loggerProvider
}

var (
	controllerLogger    log.Logger
	providerLogger      log.Logger
	controllerPublisher ginject.Publisher
)

func (instance injectedController) NewController() ginject.Controller {
	instance.BindGuard(loggerGuard{}, instance.READ_fields)
	return instance
}

func (instance injectedController) READ_fields() string {
	controllerLogger = instance.Logger
	providerLogger = instance.Provider.Logger
	controllerPublisher = instance.Publisher
	return "ok"
}

func injectedApp(t *testing.T) (ginject.App, log.Logger) {
	t.Helper()
	logger := log.NewLog(&log.LogOptions{Level: log.LevelError})
	app := ginject.New()
	app.SetLogger(logger)
	app.Create(ginject.ModuleBuilder().Providers(loggerProvider{}).Controllers(injectedController{}).Build())
	return app, logger
}

type paramsSeen struct {
	hasContext, hasNext, hasRedirect, hasPublisher bool
	query, header, param                           string
	body                                           ginject.Body
	form                                           ginject.Form
	fileNames                                      []string
}

var seen paramsSeen

type paramsController struct {
	ginject.HTTP
}

func (instance paramsController) NewController() ginject.Controller { return instance }

func (instance paramsController) READ_inspect_BY_id(
	c ginject.HTTPContext,
	query ginject.Query,
	header ginject.Header,
	param ginject.Param,
	next ginject.Next,
	publisher ginject.Publisher,
) string {
	seen.hasContext = c != nil
	seen.hasNext = next != nil
	seen.hasPublisher = publisher != nil
	seen.query = query.Get("q")
	seen.header = header.Get("X-Probe")
	seen.param = param.Get("id")
	return "ok"
}

func (instance paramsController) CREATE_body(body ginject.Body) string {
	seen.body = body
	return "ok"
}

func (instance paramsController) CREATE_upload(form ginject.Form, file ginject.File) string {
	seen.form = form
	for _, headers := range file {
		for _, h := range headers {
			seen.fileNames = append(seen.fileNames, h.Filename)
		}
	}
	return "ok"
}

func (instance paramsController) READ_away(redirect ginject.Redirect) {
	seen.hasRedirect = redirect != nil
	redirect("/elsewhere")
}

type wsSeen struct {
	hasContext, hasPublisher, hasNext bool
	payload                           ginject.WSPayload
}

var wsSeenCh = make(chan wsSeen, 4)

type wsParamsGateway struct {
	ginject.WS
}

func (instance wsParamsGateway) NewController() ginject.Controller { return instance }

func (instance wsParamsGateway) SUBSCRIBE_probe(c ginject.WSContext, payload ginject.WSPayload, publisher ginject.Publisher, next ginject.Next) string {
	wsSeenCh <- wsSeen{hasContext: c != nil, hasPublisher: publisher != nil, hasNext: next != nil, payload: payload}
	return "ok"
}

type greeter struct {
	Word string
}

func (instance greeter) NewProvider() ginject.Provider { return instance }

type greetController struct {
	ginject.HTTP
	Greeter greeter
}

func (instance greetController) NewController() ginject.Controller { return instance }

func (instance greetController) READ_greet() string { return instance.Greeter.Word }

var greeterModule = func() ginject.Module {
	m := ginject.ModuleBuilder().Providers(greeter{Word: "hi"}).Build()
	m.IsGlobal = true
	return m
}

var funcModule = func(g greeter) ginject.Module {
	return ginject.ModuleBuilder().Controllers(greetController{}).Build()
}

var funcModuleCalls int

var countedModule = func() ginject.Module {
	funcModuleCalls++
	return ginject.ModuleBuilder().Build()
}

var namedModule = func() ginject.Module {
	return ginject.ModuleBuilder().Build()
}

type wrongGuard struct{}

func (wrongGuard) NewGuard() wrongGuard { return wrongGuard{} }

func (wrongGuard) CanActivate(string) bool { return true }

func (wrongGuard) NewController() ginject.Controller { return nil }

type wrongShapeController struct {
	ginject.HTTP
	ginject.Guard
}

func (c wrongShapeController) NewController() ginject.Controller {
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
	ginject.HTTP
	ginject.Interceptor
}

func (c wrongHTTPInterceptorController) NewController() ginject.Controller {
	c.BindInterceptor(wrongInterceptor{}, c.READ_x)
	return c
}

func (wrongHTTPInterceptorController) READ_x() string { return "x" }

type wrongHTTPFilterController struct {
	ginject.HTTP
	ginject.ExceptionFilter
}

func (c wrongHTTPFilterController) NewController() ginject.Controller {
	c.BindExceptionFilter(wrongFilter{}, c.READ_x)
	return c
}

func (wrongHTTPFilterController) READ_x() string { return "x" }

type wrongWSGuardController struct {
	ginject.WS
	ginject.Guard
}

func (c wrongWSGuardController) NewController() ginject.Controller {
	c.BindGuard(wrongGuard{}, c.SUBSCRIBE_x)
	return c
}

func (wrongWSGuardController) SUBSCRIBE_x() string { return "x" }

type wrongWSInterceptorController struct {
	ginject.WS
	ginject.Interceptor
}

func (c wrongWSInterceptorController) NewController() ginject.Controller {
	c.BindInterceptor(wrongInterceptor{}, c.SUBSCRIBE_x)
	return c
}

func (wrongWSInterceptorController) SUBSCRIBE_x() string { return "x" }

type wrongWSFilterController struct {
	ginject.WS
	ginject.ExceptionFilter
}

func (c wrongWSFilterController) NewController() ginject.Controller {
	c.BindExceptionFilter(wrongFilter{}, c.SUBSCRIBE_x)
	return c
}

func (wrongWSFilterController) SUBSCRIBE_x() string { return "x" }

type surfaceBooking struct {
	Nights string `bind:"nights"`
	Errors map[string]string
}

func (surfaceBooking) Transform(f ginject.Form, _ ginject.ArgumentMetadata) any {
	bound, _ := f.Bind(surfaceBooking{})
	form := bound.(surfaceBooking)
	form.Errors = map[string]string{}
	if form.Nights == "" {
		form.Errors["nights"] = "Enter the nights"
	}
	return form
}

type surfaceBookingPages struct {
	ginject.HTTP
}

func (instance surfaceBookingPages) NewController() ginject.Controller { return instance }

func (instance surfaceBookingPages) RENDER_bookings() ginject.Map {
	return ginject.Map{"Bookings": []string{"one", "two"}}
}

func (instance surfaceBookingPages) RENDER_bookings_BY_id(param ginject.Param) surfaceBooking {
	if param.Get("id") != "1" {
		panic(exception.NotFoundException("No booking has this number"))
	}
	return surfaceBooking{Nights: "3"}
}

func (instance surfaceBookingPages) RENDER_new_OF_bookings() surfaceBooking {
	return surfaceBooking{Nights: "1"}
}

func (instance surfaceBookingPages) SUBMIT_new_OF_bookings(c ginject.HTTPContext, form surfaceBooking) surfaceBooking {
	if len(form.Errors) == 0 {
		c.Redirect("/bookings")
	}
	return form
}

type tokenController struct {
	ginject.HTTP
}

func (instance tokenController) NewController() ginject.Controller {
	instance.Path("/oauth2/token", instance.CREATE_token)
	return instance
}

func (instance tokenController) CREATE_token() ginject.Map { return ginject.Map{"token": "t"} }

func (instance tokenController) READ_payment_intents() []string { return []string{"pi"} }

var billingModule = ginject.ModuleBuilder().
	PathStyle(ginject.PathStyleKebab).
	Controllers(billingController{}).
	Build()

type billingController struct {
	ginject.HTTP
}

func (instance billingController) NewController() ginject.Controller { return instance }

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
	ginject.HTTP
}

func (instance helloController) NewController() ginject.Controller { return instance }

func (instance helloController) READ_hello() string { return "hello" }

var chatCalls atomic.Int32

type chatGateway struct {
	ginject.WS
}

func (instance chatGateway) NewController() ginject.Controller { return instance }

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

func (instance layerGuard) CanActivate(c ginject.HTTPContext) bool {
	layerCalls.guard.Add(1)
	return c.Query().Get("deny") != "true"
}

type layerInterceptor struct{}

func (instance layerInterceptor) Intercept(c ginject.HTTPContext, a ginject.Aggregation) any {
	layerCalls.interceptor.Add(1)
	return a.Pipe(a.Transform(func(data any) any {
		return ginject.Map{"wrapped": data}
	}))
}

type layerFilter struct{}

func (instance layerFilter) Catch(c ginject.HTTPContext, e *exception.Exception) {
	layerCalls.filter.Add(1)
	c.Status(http.StatusTeapot)
	c.Text("filtered")
}

type layerMiddleware struct{}

func (instance layerMiddleware) Use(r *http.Request, w http.ResponseWriter, next ginject.Next) {
	layerCalls.middleware.Add(1)
	w.Header().Set("X-Layer", "on")
	next()
}

type layersController struct {
	ginject.HTTP
	ginject.Guard
	ginject.Interceptor
	ginject.ExceptionFilter
	ginject.Middleware
}

func (instance layersController) NewController() ginject.Controller {
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
	ginject.HTTP
	ginject.Interceptor
}

func (instance wrapController) NewController() ginject.Controller {
	instance.BindInterceptor(layerInterceptor{}, instance.READ_wrapped)
	return instance
}

func (instance wrapController) READ_wrapped() string { return "x" }

func (instance wrapController) READ_bare() string { return "y" }
