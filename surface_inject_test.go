package ginject_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/internal/test"
	"github.com/dangduoc08/ginject/log"
	"golang.org/x/net/websocket"
)

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
	logger := log.NewLog(&log.LogOptions{Level: log.ErrorLevel})
	app := ginject.New()
	app.SetLogger(logger)
	app.Create(ginject.ModuleBuilder().Providers(loggerProvider{}).Controllers(injectedController{}).Build())
	return app, logger
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
		t.Error(test.DiffMessage(app.Logger(), logger, "B10: app.Logger() returns the logger given to SetLogger"))
	}
}

func TestSurface_PublisherFieldInjected(t *testing.T) {
	controllerPublisher = nil
	app, _ := injectedApp(t)

	serve(app, http.MethodGet, "/fields", nil, nil)
	if controllerPublisher == nil {
		t.Fatal(test.DiffMessage(controllerPublisher, "a publisher", "B10: a ginject.Publisher field receives the app's publisher"))
	}
	if err := controllerPublisher.Publish("nobody.listens"); err != nil {
		t.Error(test.DiffMessage(err, nil, "B10: the injected publisher publishes"))
	}
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

func TestSurface_HandlerParameters(t *testing.T) {
	seen = paramsSeen{}
	app := ginject.New()
	app.Create(ginject.ModuleBuilder().Controllers(paramsController{}).Build())

	if w := serve(app, http.MethodGet, "/inspect/42?q=7", nil, map[string]string{"X-Probe": "p"}); w.Code != http.StatusOK {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "B11: GET handler: "+w.Body.String()))
	}
	if !seen.hasContext || !seen.hasNext || !seen.hasPublisher {
		t.Error(test.DiffMessage(seen, "HTTPContext, Next and Publisher set", "B11: context, next and publisher are injected"))
	}
	if seen.query != "7" || seen.header != "p" || seen.param != "42" {
		t.Error(test.DiffMessage([]string{seen.query, seen.header, seen.param}, []string{"7", "p", "42"}, "B11: Query, Header and Param are injected"))
	}

	w := serve(app, http.MethodPost, "/body", strings.NewReader(`{"name":"ginject"}`), map[string]string{"Content-Type": "application/json"})
	if w.Code != http.StatusCreated {
		t.Fatal(test.DiffMessage(w.Code, http.StatusCreated, "B11: Body handler: "+w.Body.String()))
	}
	if seen.body["name"] != "ginject" {
		t.Error(test.DiffMessage(seen.body, ginject.Map{"name": "ginject"}, "B11: Body is injected"))
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

func TestSurface_HandlerParametersWS(t *testing.T) {
	app := ginject.New()
	app.EnableWS(&ginject.WSOptions{Path: "/ws"})
	app.Create(ginject.ModuleBuilder().Controllers(wsParamsGateway{}).Build())

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
