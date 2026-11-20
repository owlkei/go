package owlkei_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/guards/throttler"
	"go.owlkei.com/owlkei/internal/core"
	"go.owlkei.com/owlkei/internal/test"
	"go.owlkei.com/owlkei/log"
	"go.owlkei.com/owlkei/modules/cache"
	"golang.org/x/net/websocket"
)

func TestSurface_HTTPContextHasNoHooks(t *testing.T) {
	typ := reflect.TypeFor[owlkei.HTTPContext]()
	assertNoMethods(t, typ, "B5", "Init", "Reset", "AddAggregation", "Aggregations", "GetID")
	if _, ok := typ.MethodByName("ID"); !ok {
		t.Error(test.DiffMessage("<missing>", "ID", "B16: HTTPContext has ID()"))
	}
	fields := exportedFields(typ.Elem())
	want := []string{"Request", "ResponseWriter"}
	if !slices.Equal(fields, want) {
		t.Error(test.DiffMessage(fields, want, "B5: the only exported fields of HTTPContext are Request and ResponseWriter"))
	}
}

func TestSurface_WSContextHasNoHooks(t *testing.T) {
	typ := reflect.TypeFor[owlkei.WSContext]()
	assertNoMethods(t, typ, "B5", "Init", "Reset", "AddAggregation", "Aggregations", "SetSend", "SetWSPayload", "GetID")
	if _, ok := typ.MethodByName("ID"); !ok {
		t.Error(test.DiffMessage("<missing>", "ID", "B16: WSContext has ID()"))
	}
	fields := exportedFields(typ.Elem())
	want := []string{"Conn"}
	if !slices.Equal(fields, want) {
		t.Error(test.DiffMessage(fields, want, "B5: the only exported field of WSContext is Conn"))
	}
}

func TestSurface_AggregationMembers(t *testing.T) {
	typ := reflect.TypeFor[owlkei.Aggregation]()
	got := exportedMethods(typ)
	want := []string{"Filter", "Pipe", "Tap", "Transform"}
	if !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "B5: Aggregation has exactly Pipe, Transform, Tap, Filter"))
	}
	if fields := exportedFields(typ.Elem()); len(fields) != 0 {
		t.Error(test.DiffMessage(fields, []string(nil), "B5: Aggregation has no exported field"))
	}
}

func TestSurface_EmbeddableMembers(t *testing.T) {
	cases := []struct {
		typ  reflect.Type
		want []string
	}{
		{reflect.TypeFor[*owlkei.HTTP](), []string{"Path", "Prefix"}},
		{reflect.TypeFor[*owlkei.WS](), nil},
		{reflect.TypeFor[*owlkei.Guard](), []string{"BindGuard"}},
		{reflect.TypeFor[*owlkei.Interceptor](), []string{"BindInterceptor"}},
		{reflect.TypeFor[*owlkei.ExceptionFilter](), []string{"BindExceptionFilter"}},
		{reflect.TypeFor[*owlkei.Middleware](), []string{"BindMiddleware"}},
	}
	for _, c := range cases {
		got := exportedMethods(c.typ)
		if !slices.Equal(got, c.want) {
			t.Error(test.DiffMessage(got, c.want, "B6: exported methods of "+c.typ.String()))
		}
		if fields := exportedFields(c.typ.Elem()); len(fields) != 0 {
			t.Error(test.DiffMessage(fields, []string(nil), "B6: exported fields of "+c.typ.String()))
		}
	}
}

func TestSurface_ModuleBuilderIsFunc(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "aliases.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var isFunc, isVar bool
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name == "ModuleBuilder" {
				isFunc = true
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if v, ok := spec.(*ast.ValueSpec); ok {
					for _, n := range v.Names {
						isVar = isVar || n.Name == "ModuleBuilder"
					}
				}
			}
		}
	}
	if !isFunc || isVar {
		t.Error(test.DiffMessage([2]bool{isFunc, isVar}, [2]bool{true, false}, "D3: owlkei.ModuleBuilder is a func declaration, not a variable"))
	}
	typ := reflect.TypeOf(owlkei.ModuleBuilder)
	if typ.Kind() != reflect.Func || typ.NumIn() != 0 || typ.NumOut() != 1 || typ.Out(0) != reflect.TypeFor[*core.ModuleBuilder]() {
		t.Error(test.DiffMessage(typ.String(), "func() *core.ModuleBuilder", "D3: ModuleBuilder takes nothing and returns *core.ModuleBuilder"))
	}
}

func TestSurface_ModuleBuilderChain(t *testing.T) {
	b := owlkei.ModuleBuilder()
	m := b.Imports().Providers(greeter{Word: "x"}).Controllers(greetController{}).Build()
	if m == nil {
		t.Fatal(test.DiffMessage(m, "<module>", "B9: Build returns a module"))
	}
	if again := owlkei.ModuleBuilder(); again == b {
		t.Error(test.DiffMessage(again == b, false, "B9: every call returns a new builder"))
	}
}

func TestSurface_FilterCatchesExceptionPointer(t *testing.T) {
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Controllers(pointerCatchController{}).Build())
	w := serve(app, http.MethodGet, "/boom", nil, nil)
	if w.Code != http.StatusTeapot {
		t.Error(test.DiffMessage(w.Code, http.StatusTeapot, "duplicate 3: a filter with Catch(ctx, *exception.Exception) catches the panic"))
	}
}

func TestSurface_BindLayersAcceptAnyShape(t *testing.T) {
	for _, name := range []string{"BindGuard", "BindInterceptor", "BindExceptionFilter"} {
		var typ reflect.Type
		switch name {
		case "BindGuard":
			typ = reflect.TypeFor[*owlkei.Guard]()
		case "BindInterceptor":
			typ = reflect.TypeFor[*owlkei.Interceptor]()
		default:
			typ = reflect.TypeFor[*owlkei.ExceptionFilter]()
		}
		m, _ := typ.MethodByName(name)
		if m.Type.In(1) != reflect.TypeFor[any]() {
			t.Error(test.DiffMessage(m.Type.In(1).String(), "interface {}", "D7: "+name+" takes any"))
		}
	}
}

func TestSurface_ExamplesUsePublicImportsOnly(t *testing.T) {
	files := collectGoFiles(t, "examples", true)
	for _, file := range collectGoFiles(t, "bench", false) {
		if !strings.HasSuffix(file, "_test.go") {
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		t.Fatal(test.DiffMessage(0, "some files", "B4: examples/ and bench/ have Go files to check"))
	}
	fset := token.NewFileSet()
	var offenders []string
	for _, file := range files {
		parsed, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range parsed.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if forbiddenImport(path) {
				offenders = append(offenders, file+" imports "+path)
			}
		}
	}
	if len(offenders) != 0 {
		t.Error(test.DiffMessage(strings.Join(offenders, "\n"), "no internal or moved-package import", "B4: examples/ and bench/ import public paths only"))
	}
}

func TestSurface_ForbiddenImportRule(t *testing.T) {
	cases := map[string]bool{
		modulePath + "/internal/core":      true,
		modulePath + "/internal/test":      true,
		modulePath + "/core":               true,
		modulePath + "/common":             true,
		modulePath + "/ctx":                true,
		modulePath + "/trace":              true,
		modulePath + "/log":                false,
		modulePath + "/exception":          false,
		modulePath + "/middlewares/helmet": false,
		modulePath + "/modules/cache":      false,
		modulePath + "/guards/anything":    false,
		"net/http":                         false,
	}
	for path, want := range cases {
		if got := forbiddenImport(path); got != want {
			t.Error(test.DiffMessage(got, want, "B4: forbiddenImport("+path+")"))
		}
	}
}

func TestSurface_LoggerFieldInjected(t *testing.T) {
	controllerLogger, providerLogger, guardLogger = nil, nil, nil
	app, logger := injectedApp(t)

	if w := serve(app, http.MethodGet, "/fields", nil, nil); w.Code != http.StatusOK {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "B10: the route answers: "+w.Body.String()))
	}
	if controllerLogger == nil || controllerLogger != logger {
		t.Error(test.DiffMessage(controllerLogger, logger, "B10: a log.Logger field of a controller receives the app's logger"))
	}
	if providerLogger == nil || providerLogger != logger {
		t.Error(test.DiffMessage(providerLogger, logger, "B10: a log.Logger field of a provider receives the app's logger"))
	}
	if guardLogger == nil || guardLogger != logger {
		t.Error(test.DiffMessage(guardLogger, logger, "B10: a log.Logger field of a layer receives the app's logger"))
	}
	if app.Logger() != logger {
		t.Error(test.DiffMessage(app.Logger(), logger, "B10: app.Logger() returns the logger given as Logger"))
	}
}

func TestSurface_PublisherFieldInjected(t *testing.T) {
	controllerPublisher = nil
	app, _ := injectedApp(t)

	serve(app, http.MethodGet, "/fields", nil, nil)
	if controllerPublisher == nil {
		t.Fatal(test.DiffMessage(controllerPublisher, "a publisher", "B10: a owlkei.Publisher field receives the app's publisher"))
	}
	if err := controllerPublisher.Publish("nobody.listens"); err != nil {
		t.Error(test.DiffMessage(err, nil, "B10: the injected publisher publishes"))
	}
}

func TestSurface_HandlerParameters(t *testing.T) {
	seen = paramsSeen{}
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Controllers(paramsController{}).Build())

	if w := serve(app, http.MethodGet, "/inspect/42?q=7", nil, map[string]string{"X-Probe": "p"}); w.Code != http.StatusOK {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "B11: GET handler: "+w.Body.String()))
	}
	if !seen.hasContext || !seen.hasNext || !seen.hasPublisher {
		t.Error(test.DiffMessage(seen, "HTTPContext, Next and Publisher set", "B11: context, next and publisher are injected"))
	}
	if seen.query != "7" || seen.header != "p" || seen.param != "42" {
		t.Error(test.DiffMessage([]string{seen.query, seen.header, seen.param}, []string{"7", "p", "42"}, "B11: Query, Header and Param are injected"))
	}

	w := serve(app, http.MethodPost, "/body", strings.NewReader(`{"name":"owlkei"}`), map[string]string{"Content-Type": "application/json"})
	if w.Code != http.StatusCreated {
		t.Fatal(test.DiffMessage(w.Code, http.StatusCreated, "B11: Body handler: "+w.Body.String()))
	}
	if seen.body["name"] != "owlkei" {
		t.Error(test.DiffMessage(seen.body, owlkei.Map{"name": "owlkei"}, "B11: Body is injected"))
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("title", "t")
	part, _ := mw.CreateFormFile("doc", "a.txt")
	_, _ = part.Write([]byte("data"))
	_ = mw.Close()
	w = serve(app, http.MethodPost, "/upload", &buf, map[string]string{"Content-Type": mw.FormDataContentType()})
	if w.Code != http.StatusCreated {
		t.Fatal(test.DiffMessage(w.Code, http.StatusCreated, "B11: Form and File handler: "+w.Body.String()))
	}
	if seen.form.Get("title") != "t" {
		t.Error(test.DiffMessage(seen.form.Get("title"), "t", "B11: Form is injected"))
	}
	if len(seen.fileNames) != 1 || seen.fileNames[0] != "a.txt" {
		t.Error(test.DiffMessage(seen.fileNames, []string{"a.txt"}, "B11: File is injected"))
	}

	w = serve(app, http.MethodGet, "/away", nil, nil)
	if !seen.hasRedirect || w.Header().Get("Location") != "/elsewhere" {
		t.Error(test.DiffMessage(w.Header().Get("Location"), "/elsewhere", "B11: Redirect is injected and redirects"))
	}
}

func TestSurface_HandlerParametersWS(t *testing.T) {
	app := owlkei.New(&owlkei.AppOptions{WS: &owlkei.WSOptions{Path: "/ws"}})
	app.Create(owlkei.ModuleBuilder().Controllers(wsParamsGateway{}).Build())

	server := httptest.NewServer(app)
	defer server.Close()
	conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	var connected wireMessage
	if err := websocket.JSON.Receive(conn, &connected); err != nil {
		t.Fatal(err)
	}
	if err := websocket.JSON.Send(conn, wireMessage{Type: "subscribe", ID: "s1", Topic: []string{"probe"}}); err != nil {
		t.Fatal(err)
	}
	if err := websocket.JSON.Send(conn, wireMessage{Type: "publish", ID: "p1", Topic: []string{"probe"}, Msg: map[string]any{"k": "v"}}); err != nil {
		t.Fatal(err)
	}
	var got wsSeen
	select {
	case got = <-wsSeenCh:
	case <-time.After(5 * time.Second):
		t.Fatal("B11: the WS handler did not run after a publish")
	}
	if !got.hasContext || !got.hasPublisher || !got.hasNext {
		t.Error(test.DiffMessage(got, "WSContext, Publisher and Next set", "B11: WS context, publisher and next are injected"))
	}
	if got.payload == nil {
		t.Error(test.DiffMessage(got.payload, "a payload", "B11: WSPayload is injected"))
	}
}

func TestSurface_ModuleFunc(t *testing.T) {
	funcModuleCalls = 0
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Imports(greeterModule, funcModule, countedModule, countedModule).Build())

	w := serve(app, http.MethodGet, "/greet", nil, nil)
	if w.Code != http.StatusOK || w.Body.String() != "hi" {
		t.Error(test.DiffMessage(w.Body.String(), "hi", "B9: a func() owlkei.Module is imported and its parameters are resolved from global providers"))
	}
	if funcModuleCalls != 1 {
		t.Error(test.DiffMessage(funcModuleCalls, 1, "B9: a module function is called once"))
	}
}

func TestSurface_ModuleValue(t *testing.T) {
	local := owlkei.ModuleBuilder().Providers(greeter{Word: "value"}).Controllers(greetController{}).Build()
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Imports(local, cache.Register(nil)).Build())

	w := serve(app, http.MethodGet, "/greet", nil, nil)
	if w.Body.String() != "value" {
		t.Error(test.DiffMessage(w.Body.String(), "value", "B9: a owlkei.Module value (and a Register result) is imported"))
	}
}

func TestSurface_InvalidModulePanics(t *testing.T) {
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		owlkei.New(nil).Create(owlkei.ModuleBuilder().Imports("not a module").Build())
	}()
	if recovered == nil {
		t.Fatal(test.DiffMessage("no panic", "a panic", "B9: an import that is not a module panics"))
	}
	got := fmt.Sprint(recovered)
	want := "invalid module: can't import 'string' with Imports"
	if !strings.Contains(got, want) {
		t.Error(test.DiffMessage(got, want, "B9: panic text"))
	}
}

func TestSurface_ModuleNameResolved(t *testing.T) {
	m := namedModule()
	if m.Name != "owlkei_test.namedModule" {
		t.Error(test.DiffMessage(m.Name, "owlkei_test.namedModule", "B9: Name is <package>.<var> through owlkei.ModuleBuilder()"))
	}
}

func TestSurface_ConstantValues(t *testing.T) {
	cases := []struct {
		name      string
		got, want int64
	}{
		{"VersioningQuery", int64(owlkei.VersioningQuery), 1},
		{"VersioningHeader", int64(owlkei.VersioningHeader), 2},
		{"VersioningCustom", int64(owlkei.VersioningCustom), 3},
		{"VersioningMediaType", int64(owlkei.VersioningMediaType), 4},
		{"StrategyFixedWindow", int64(throttler.StrategyFixedWindow), 0},
		{"StrategySlidingWindow", int64(throttler.StrategySlidingWindow), 1},
		{"StrategyTokenBucket", int64(throttler.StrategyTokenBucket), 2},
		{"ProxyHeaderXForwarded", int64(owlkei.ProxyHeaderXForwarded), 0},
		{"ProxyHeaderForwarded", int64(owlkei.ProxyHeaderForwarded), 1},
		{"LevelDebug", int64(log.LevelDebug), int64(slog.LevelDebug)},
		{"LevelInfo", int64(log.LevelInfo), int64(slog.LevelInfo)},
		{"LevelWarn", int64(log.LevelWarn), int64(slog.LevelWarn)},
		{"LevelError", int64(log.LevelError), int64(slog.LevelError)},
		{"LevelFatal", int64(log.LevelFatal), 12},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Error(test.DiffMessage(c.got, c.want, "B15: the value of "+c.name))
		}
	}
	var zero log.Format
	if zero == log.FormatText || zero == log.FormatJSON || zero == log.FormatPretty {
		t.Error(test.DiffMessage(zero, "not one of the named formats", "B15: the zero Format still means the default format"))
	}
}

func TestSurface_WrongLayerShapeNamesTheRootTypes(t *testing.T) {
	msg := func() (msg string) {
		defer func() { msg = fmt.Sprint(recover()) }()
		app := owlkei.New(nil)
		app.Create(owlkei.ModuleBuilder().Controllers(wrongShapeController{}).Build())
		return ""
	}()
	if !strings.Contains(msg, "CanActivate must be func(owlkei.HTTPContext) bool to be bound as a HTTP guard") {
		t.Error(test.DiffMessage(msg, "invalid guard: <type>.CanActivate must be func(owlkei.HTTPContext) bool to be bound as a HTTP guard", "B12: the message spells the type as users write it"))
	}
}

func TestSurface_EveryWrongShapeMessage(t *testing.T) {
	cases := []struct {
		controller owlkei.Controller
		ws         bool
		want       string
	}{
		{wrongShapeController{}, false, ".CanActivate must be func(owlkei.HTTPContext) bool to be bound as a HTTP guard"},
		{wrongHTTPInterceptorController{}, false, ".Intercept must be func(owlkei.HTTPContext, owlkei.Aggregation) any to be bound as a HTTP interceptor"},
		{wrongHTTPFilterController{}, false, ".Catch must be func(owlkei.HTTPContext, *exception.Exception) to be bound as a HTTP exception filter"},
		{wrongWSGuardController{}, true, ".CanActivate must be func(owlkei.WSContext) bool to be bound as a WS guard"},
		{wrongWSInterceptorController{}, true, ".Intercept must be func(owlkei.WSContext, owlkei.Aggregation) any to be bound as a WS interceptor"},
		{wrongWSFilterController{}, true, ".Catch must be func(owlkei.WSContext, *exception.Exception) to be bound as a WS exception filter"},
	}
	for _, c := range cases {
		msg := func() (msg string) {
			defer func() { msg = fmt.Sprint(recover()) }()
			opts := &owlkei.AppOptions{}
			if c.ws {
				opts.WS = &owlkei.WSOptions{Path: "/ws"}
			}
			app := owlkei.New(opts)
			app.Create(owlkei.ModuleBuilder().Controllers(c.controller).Build())
			return ""
		}()
		if !strings.Contains(msg, c.want) {
			t.Error(test.DiffMessage(msg, "…"+c.want, "B12: the exact shape message"))
		}
	}
}

func TestSurface_PagesByRoute(t *testing.T) {
	var _ = owlkei.RenderOptions{IsReload: false}

	web := fstest.MapFS{
		"layout.html":         {Data: []byte(`<main>{{template "content" .}}</main>`)},
		"bookings/index.html": {Data: []byte(`<ul>{{range .Bookings}}<li>{{.}}</li>{{end}}</ul>`)},
		"bookings/{id}.html":  {Data: []byte(`<p>{{.Nights}} nights</p>`)},
		"bookings/new.html":   {Data: []byte(`<form method="post" action="/bookings/new"><input name="nights" value="{{.Nights}}">{{with .Errors.nights}}<b>{{.}}</b>{{end}}</form>`)},
		"404.html":            {Data: []byte(`<h1>{{.Code}} {{.Message}}</h1>`)},
		"public/robots.txt":   {Data: []byte("User-agent: *\n")},
	}
	app := owlkei.New(&owlkei.AppOptions{Render: &owlkei.RenderOptions{FS: web, Dir: "."}})
	app.Create(owlkei.ModuleBuilder().Controllers(surfaceBookingPages{}).Build())

	w := serve(app, http.MethodGet, "/bookings", nil, nil)
	if want := "<main><ul><li>one</li><li>two</li></ul></main>"; w.Code != http.StatusOK || w.Body.String() != want {
		t.Error(test.DiffMessage(w.Body.String(), want, "P1, P2: RENDER_bookings renders bookings/index.html in the layout"))
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Error(test.DiffMessage(ct, "text/html; charset=utf-8", "R5: content type"))
	}

	w = serve(app, http.MethodGet, "/bookings/1", nil, nil)
	if want := "<main><p>3 nights</p></main>"; w.Body.String() != want {
		t.Error(test.DiffMessage(w.Body.String(), want, "P2: a result struct is the data of bookings/{id}.html"))
	}
	w = serve(app, http.MethodGet, "/bookings/9", nil, nil)
	if want := "<main><h1>404 No booking has this number</h1></main>"; w.Code != http.StatusNotFound || w.Body.String() != want {
		t.Error(test.DiffMessage(w.Body.String(), want, "P17: an exception is rendered by 404.html"))
	}

	form := url.Values{"nights": {""}}
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	w = serve(app, http.MethodPost, "/bookings/new", strings.NewReader(form.Encode()), headers)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "<b>Enter the nights</b>") {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{http.StatusUnprocessableEntity, "the form with its message"}, "P14: an invalid SUBMIT_ renders its RENDER_'s template with 422"))
	}
	form = url.Values{"nights": {"2"}}
	w = serve(app, http.MethodPost, "/bookings/new", strings.NewReader(form.Encode()), headers)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/bookings" {
		t.Error(test.DiffMessage(w.Code, http.StatusSeeOther, "P14: a valid SUBMIT_ redirects with 303"))
	}

	w = serve(app, http.MethodGet, "/robots.txt", nil, nil)
	if w.Code != http.StatusOK || w.Body.String() != "User-agent: *\n" {
		t.Error(test.DiffMessage(w.Body.String(), "User-agent: *\n", "P22: public/robots.txt is served at the root"))
	}
}

func TestSurface_PageAndViewAreGone(t *testing.T) {
	listing, err := os.ReadFile("api/owlkei.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(listing), "\n") {
		for _, removed := range []string{"type Page =", "type View =", "common.Page", "common.View"} {
			if strings.Contains(line, removed) {
				t.Error(test.DiffMessage(line, "no such line", "P11: owlkei.Page and owlkei.View are removed from api/owlkei.txt (make api)"))
			}
		}
	}
}

func TestSurface_PathStyle(t *testing.T) {
	style := owlkei.PathStyle(owlkei.PathStyleAsWritten)
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().
		PathStyle(style).
		Imports(billingModule).
		Controllers(tokenController{}).
		Build().
		Prefix("api"))

	if w := serve(app, http.MethodPost, "/api/oauth2/token", nil, nil); w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusCreated, "B22: POST /api/oauth2/token"))
	}
	if w := serve(app, http.MethodGet, "/api/payment_intents", nil, nil); w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "B17c: GET /api/payment_intents under PathStyleAsWritten"))
	}
	if w := serve(app, http.MethodGet, "/api/payment-intents", nil, nil); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "B12: the kebab spelling is 404 under PathStyleAsWritten"))
	}
	if w := serve(app, http.MethodGet, "/api/invoice-lines", nil, nil); w.Code != http.StatusOK || w.Body.String() != "lines" {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "B17d: a module with PathStyleKebab keeps its URLs"))
	}
}

func TestSurface_PathStyleKebabIsTheDefault(t *testing.T) {
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Controllers(tokenController{}).Build())

	if w := serve(app, http.MethodGet, "/payment-intents", nil, nil); w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "B1: GET /payment-intents without a style"))
	}
}

func TestSurface_PathStyleConstantsAreDistinct(t *testing.T) {
	if owlkei.PathStyleKebab == owlkei.PathStyleAsWritten {
		t.Error(test.DiffMessage(owlkei.PathStyleKebab, "a different value", "the two styles differ"))
	}
}

func TestSurface_HTTPControllerRoutes(t *testing.T) {
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Controllers(helloController{}).Build())

	w := serve(app, http.MethodGet, "/hello", nil, nil)
	if w.Code != http.StatusOK {
		t.Error(test.DiffMessage(w.Code, http.StatusOK, "B7: GET /hello status"))
	}
	if w.Body.String() != "hello" {
		t.Error(test.DiffMessage(w.Body.String(), "hello", "B7: GET /hello body"))
	}
	if w := serve(app, http.MethodGet, "/missing", nil, nil); w.Code != http.StatusNotFound {
		t.Error(test.DiffMessage(w.Code, http.StatusNotFound, "B7: an undeclared route is 404"))
	}
}

func TestSurface_WSControllerEvents(t *testing.T) {
	chatCalls.Store(0)
	app := owlkei.New(&owlkei.AppOptions{WS: &owlkei.WSOptions{Path: "/ws"}})
	app.Create(owlkei.ModuleBuilder().Controllers(chatGateway{}).Build())

	server := httptest.NewServer(app)
	defer server.Close()

	conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	var connected wireMessage
	if err := websocket.JSON.Receive(conn, &connected); err != nil {
		t.Fatal(err)
	}
	if connected.Type != "connected" {
		t.Error(test.DiffMessage(connected.Type, "connected", "B7: the first frame"))
	}

	if err := websocket.JSON.Send(conn, wireMessage{Type: "subscribe", ID: "s1", Topic: []string{"chat.room1"}}); err != nil {
		t.Fatal(err)
	}
	if got := receiveUntil(t, conn, "s1"); got.Type != "ack" {
		t.Error(test.DiffMessage(got.Type, "ack", "B7: subscribe to chat.room1 is acknowledged"))
	}
	if err := websocket.JSON.Send(conn, wireMessage{Type: "publish", ID: "p1", Topic: []string{"chat.room1"}, Msg: "hi"}); err != nil {
		t.Fatal(err)
	}
	if got := receiveUntil(t, conn, "p1"); got.Type != "ack" {
		t.Error(test.DiffMessage(got.Type, "ack", "B7: publish to chat.room1 is handled by SUBSCRIBE_chat_ANY"))
	}
	if chatCalls.Load() == 0 {
		t.Error(test.DiffMessage(chatCalls.Load(), "at least 1", "B7: SUBSCRIBE_chat_ANY ran"))
	}
}

func TestSurface_BindLayersThroughAliases(t *testing.T) {
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Controllers(layersController{}).Build())

	plain := serve(app, http.MethodGet, "/plain?deny=true", nil, nil)
	if plain.Code != http.StatusOK || plain.Body.String() != "plain" {
		t.Error(test.DiffMessage(plain.Code, http.StatusOK, "B8: the unbound route is untouched: "+plain.Body.String()))
	}
	if plain.Header().Get("X-Layer") != "" {
		t.Error(test.DiffMessage(plain.Header().Get("X-Layer"), "", "B8: the middleware does not run on the other route"))
	}
	if layerCalls.guard.Load()+layerCalls.interceptor.Load()+layerCalls.filter.Load()+layerCalls.middleware.Load() != 0 {
		t.Error(test.DiffMessage([]int32{layerCalls.guard.Load(), layerCalls.interceptor.Load(), layerCalls.filter.Load(), layerCalls.middleware.Load()}, []int32{0, 0, 0, 0}, "B8: no layer ran for the unbound route"))
	}

	bound := serve(app, http.MethodGet, "/bound", nil, nil)
	if bound.Code != http.StatusTeapot || bound.Body.String() != "filtered" {
		t.Error(test.DiffMessage(bound.Code, http.StatusTeapot, "B8: the exception filter answers the bound route: "+bound.Body.String()))
	}
	if bound.Header().Get("X-Layer") != "on" {
		t.Error(test.DiffMessage(bound.Header().Get("X-Layer"), "on", "B8: the middleware runs on the bound route"))
	}
	if layerCalls.guard.Load() != 1 || layerCalls.middleware.Load() != 1 || layerCalls.filter.Load() != 1 {
		t.Error(test.DiffMessage([]int32{layerCalls.guard.Load(), layerCalls.middleware.Load(), layerCalls.filter.Load()}, []int32{1, 1, 1}, "B8: layers bound to the route ran"))
	}

	denied := serve(app, http.MethodGet, "/bound?deny=true", nil, nil)
	if denied.Code != http.StatusTeapot {
		t.Error(test.DiffMessage(denied.Code, http.StatusTeapot, "B8: the guard refuses the bound route"))
	}
}

func TestSurface_BindInterceptorThroughAliasWrapsOneRoute(t *testing.T) {
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Controllers(wrapController{}).Build())

	if got := serve(app, http.MethodGet, "/wrapped", nil, nil).Body.String(); !strings.Contains(got, "wrapped") {
		t.Error(test.DiffMessage(got, "a body with the wrapped key", "B8: the interceptor transforms the bound route"))
	}
	if got := serve(app, http.MethodGet, "/bare", nil, nil).Body.String(); got != "y" {
		t.Error(test.DiffMessage(got, "y", "B8: the interceptor leaves the other route"))
	}
}

func TestSurface_Retry(t *testing.T) {
	if reflect.TypeFor[owlkei.RetryOptions]() != reflect.TypeFor[core.RetryOptions]() {
		t.Error(test.DiffMessage(reflect.TypeFor[owlkei.RetryOptions](), reflect.TypeFor[core.RetryOptions](), "R: owlkei.RetryOptions is an alias of core.RetryOptions"))
	}

	var calls int
	opts := &owlkei.RetryOptions{InitialDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond, MaxAttempts: 3}
	err := owlkei.Retry(context.Background(), opts, func(context.Context) error {
		calls++
		if calls < 2 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil || calls != 2 {
		t.Error(test.DiffMessage([]any{err, calls}, []any{nil, 2}, "R1: owlkei.Retry retries until fn succeeds"))
	}
}

func TestSurface_AsyncImports_ServesWhileFactoryBlocks(t *testing.T) {
	release := make(chan struct{})
	factory := func(context.Context) (owlkei.Module, error) {
		<-release
		return owlkei.ModuleBuilder().Build(), nil
	}
	root := owlkei.ModuleBuilder().AsyncImports(factory).Build()

	app := owlkei.New(&owlkei.AppOptions{StartupTimeout: 5 * time.Second})
	created := make(chan struct{})
	go func() {
		defer close(created)
		app.Create(root)
	}()
	select {
	case <-created:
	case <-time.After(10 * time.Second):
		t.Fatal("A1: Create waited for an AsyncImports factory")
	}
	close(release)
	app.Stop()
}

type clientProbe struct {
	ip, scheme, host string
}

func (p *clientProbe) Use(r *http.Request, _ http.ResponseWriter, next owlkei.Next) {
	p.ip = owlkei.ClientIP(r).String()
	p.scheme = owlkei.ClientScheme(r)
	p.host = owlkei.ClientHost(r)
	next()
}

func TestSurface_NewTakesOptions(t *testing.T) {
	typ := reflect.TypeOf(owlkei.New)
	if typ.NumIn() < 1 || typ.In(0) != reflect.TypeFor[*core.AppOptions]() {
		t.Error(test.DiffMessage(typ.String(), "func(*core.AppOptions) *core.App", "API: New takes *AppOptions"))
	}
	if owlkei.New(nil) == nil || owlkei.New(&owlkei.AppOptions{}) == nil {
		t.Error(test.DiffMessage(nil, "an app", "A1: New(nil) and New(&AppOptions{})"))
	}
}

func TestSurface_ListenAndServeTakeNoOptions(t *testing.T) {
	listen, _ := reflect.TypeFor[owlkei.App]().MethodByName("Listen")
	serve, _ := reflect.TypeFor[owlkei.App]().MethodByName("Serve")
	if listen.Type.NumIn() != 1 || listen.Type.NumOut() != 1 {
		t.Error(test.DiffMessage(listen.Type.String(), "func(*App) error", "S1: Listen() error"))
	}
	if serve.Type.NumIn() != 2 || serve.Type.NumOut() != 1 {
		t.Error(test.DiffMessage(serve.Type.String(), "func(*App, net.Listener) error", "S2: Serve(l) error"))
	}
}

func TestSurface_ClientHelpers(t *testing.T) {
	probe := &clientProbe{}
	app := owlkei.New(&owlkei.AppOptions{
		Logger: log.NewLog(nil),
		Proxy:  &owlkei.ProxyOptions{TrustedProxies: []string{"192.0.2.1"}, IsHostForwarded: true},
	})
	app.BindGlobalMiddlewares(probe)
	app.Create(owlkei.ModuleBuilder().Build())
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "app.example.com")
	app.ServeHTTP(httptest.NewRecorder(), r)
	got := []string{probe.ip, probe.scheme, probe.host}
	want := []string{"203.0.113.9", "https", "app.example.com"}
	if !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "P2-P9: ClientIP, ClientScheme, ClientHost keep the proxy behavior"))
	}
}

func TestSurface_ClientHelpersWithoutProxy(t *testing.T) {
	probe := &clientProbe{}
	app := owlkei.New(nil)
	app.BindGlobalMiddlewares(probe)
	app.Create(owlkei.ModuleBuilder().Build())
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	app.ServeHTTP(httptest.NewRecorder(), r)
	if probe.ip != "192.0.2.1" || probe.scheme != "http" || probe.host != "example.com" {
		t.Error(test.DiffMessage([]string{probe.ip, probe.scheme, probe.host}, []string{"192.0.2.1", "http", "example.com"}, "P2: the peer is the client without Proxy"))
	}
}

func TestSurface_ByteSizeAndProxyHeaderConstants(t *testing.T) {
	got := []owlkei.ByteSize{owlkei.KB, owlkei.MB, owlkei.GB, owlkei.KiB, owlkei.MiB, owlkei.GiB}
	want := []owlkei.ByteSize{1000, 1_000_000, 1_000_000_000, 1024, 1 << 20, 1 << 30}
	if !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "B1: the six unit constants"))
	}
	if owlkei.MiB.String() != "1MiB" || (10*owlkei.KB).String() != "10KB" {
		t.Error(test.DiffMessage(owlkei.MiB.String(), "1MiB", "B3: ByteSize.String is public"))
	}
	if owlkei.ProxyHeaderXForwarded != 0 || owlkei.ProxyHeaderForwarded != 1 {
		t.Error(test.DiffMessage([]owlkei.ProxyHeader{owlkei.ProxyHeaderXForwarded, owlkei.ProxyHeaderForwarded}, []int{0, 1}, "API: ProxyHeader constants"))
	}
}

func TestSurface_GroupAliases(t *testing.T) {
	pairs := map[string][2]reflect.Type{
		"AppOptions":           {reflect.TypeFor[owlkei.AppOptions](), reflect.TypeFor[core.AppOptions]()},
		"TLSOptions":           {reflect.TypeFor[owlkei.TLSOptions](), reflect.TypeFor[core.TLSOptions]()},
		"HTTPOptions":          {reflect.TypeFor[owlkei.HTTPOptions](), reflect.TypeFor[core.HTTPOptions]()},
		"ProxyOptions":         {reflect.TypeFor[owlkei.ProxyOptions](), reflect.TypeFor[core.ProxyOptions]()},
		"LoadShedOptions":      {reflect.TypeFor[owlkei.LoadShedOptions](), reflect.TypeFor[core.LoadShedOptions]()},
		"HTTPSRedirectOptions": {reflect.TypeFor[owlkei.HTTPSRedirectOptions](), reflect.TypeFor[core.HTTPSRedirectOptions]()},
		"HeadersOptions":       {reflect.TypeFor[owlkei.HeadersOptions](), reflect.TypeFor[core.HeadersOptions]()},
		"CORSOptions":          {reflect.TypeFor[owlkei.CORSOptions](), reflect.TypeFor[core.CORSOptions]()},
		"CSRFOptions":          {reflect.TypeFor[owlkei.CSRFOptions](), reflect.TypeFor[core.CSRFOptions]()},
		"CompressOptions":      {reflect.TypeFor[owlkei.CompressOptions](), reflect.TypeFor[core.CompressOptions]()},
	}
	for name, pair := range pairs {
		if pair[0] != pair[1] {
			t.Error(test.DiffMessage(pair[0], pair[1], "API: "+name+" is an alias of the core type"))
		}
	}
}

func TestSurface_OptionsFromEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	opts := owlkei.OptionsFromEnv()
	if opts == nil || opts.Server == nil || opts.Server.TLS == nil || opts.HTTP == nil || opts.Headers == nil || opts.CSRF == nil {
		t.Fatal(test.DiffMessage(opts, "every group non-nil", "E1: OptionsFromEnv fills every pointer group"))
	}
	t.Setenv("OK_HTTP__MAX_BODY_BYTES", "2KiB")
	if got := owlkei.OptionsFromEnv().HTTP.MaxBodyBytes; got != 2*owlkei.KiB {
		t.Error(test.DiffMessage(got, 2*owlkei.KiB, "E5: a variable reaches the public struct"))
	}
}
