package core

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"embed"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math/big"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/aggregation"
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/httphook"
	"github.com/dangduoc08/ginject/internal/logpipe"
	"github.com/dangduoc08/ginject/internal/test"
	ctxlookalike "github.com/dangduoc08/ginject/internal/test/lookalike/ctx"
	"github.com/dangduoc08/ginject/internal/trace"
	"github.com/dangduoc08/ginject/internal/tracecontext"
	"github.com/dangduoc08/ginject/internal/versioning"
	"github.com/dangduoc08/ginject/log"
	"github.com/dangduoc08/ginject/middlewares/proxy"
	"golang.org/x/net/websocket"
)

type roleGuard struct{ Role string }

func (g roleGuard) CanActivate(c *ctx.HTTPContext) bool {
	return c.Request.Header.Get("X-Role") == g.Role
}

type roleAlphaController struct {
	common.HTTP
	common.Guard
}

func (c roleAlphaController) NewController() Controller {
	c.BindGuard(roleGuard{Role: "admin"})
	return c
}

func (c roleAlphaController) READ_alpha() string { return "alpha" }

type roleBetaController struct {
	common.HTTP
	common.Guard
}

func (c roleBetaController) NewController() Controller {
	c.BindGuard(roleGuard{Role: "user"})
	return c
}

func (c roleBetaController) READ_beta() string { return "beta" }

func serveWithRole(app *App, path, role string) int {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("X-Role", role)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w.Code
}

type nonRouteGuardController struct {
	common.HTTP
	common.Guard
}

func (c nonRouteGuardController) NewController() Controller {
	c.BindGuard(roleGuard{Role: "admin"}, c.Helper)
	return c
}

func (c nonRouteGuardController) READ_open() string { return "open" }

func (c nonRouteGuardController) Helper() string { return "helper" }

var rethrowCalls int

type rethrowFilter struct{}

func (rethrowFilter) Catch(_ *ctx.HTTPContext, ex *exception.Exception) {
	rethrowCalls++
	panic(ex)
}

type finalFilter struct{}

func (finalFilter) Catch(c *ctx.HTTPContext, ex *exception.Exception) {
	c.Status(ex.Code())
	c.Text(ex.Message())
}

type rethrowController struct {
	common.HTTP
	common.ExceptionFilter
}

func (c rethrowController) NewController() Controller {
	c.BindExceptionFilter(finalFilter{})
	c.BindExceptionFilter(rethrowFilter{})
	return c
}

func (c rethrowController) READ_gone() string {
	panic(exception.NotFoundException("nope"))
}

type mockLogger struct{}

func (m *mockLogger) Debug(msg string, args ...any) {}

func (m *mockLogger) Info(msg string, args ...any) {}

func (m *mockLogger) Warn(msg string, args ...any) {}

func (m *mockLogger) Error(msg string, args ...any) {}

func (m *mockLogger) Fatal(msg string, args ...any) {}

type bodyLimitController struct{ common.HTTP }

func (c bodyLimitController) NewController() Controller { return c }

func (c bodyLimitController) CREATE(body ctx.Body) any {
	return ctx.Map{"got": body["k"]}
}

func newBodyLimitApp(t *testing.T, limit int64) *App {
	t.Helper()

	app := New()
	app.maxRequestBodyBytes.Store(limit)
	app.Create(NewModuleBuilder().Controllers(bodyLimitController{}).Build())

	return app
}

type raceGlobalProvider struct{ Tag string }

func (p raceGlobalProvider) NewProvider() Provider { return p }

type raceGlobalMiddleware struct{ P raceGlobalProvider }

func (mw raceGlobalMiddleware) Use(_ *http.Request, _ http.ResponseWriter, next ctx.Next) { next() }

type panicMiddleware struct{}

func (panicMiddleware) Use(_ *http.Request, _ http.ResponseWriter, _ ctx.Next) {
	panic(exception.ForbiddenException("nope"))
}

type panicMiddlewareController struct {
	common.HTTP
}

func (c panicMiddlewareController) NewController() Controller { return c }

func (c panicMiddlewareController) READ_panicmiddleware() string {
	return "ok"
}

type denyGlobalGuard struct{}

func (denyGlobalGuard) CanActivate(_ *ctx.HTTPContext) bool { return false }

type shapelessGlobalGuard struct{}

type globalGuardController struct {
	common.HTTP
}

func (c globalGuardController) NewController() Controller { return c }

func (c globalGuardController) READ_globalguard() string { return "ok" }

type traceMiddleware struct{}

func (traceMiddleware) Use(_ *http.Request, _ http.ResponseWriter, next ctx.Next) { next() }

type traceGuard struct{}

func (traceGuard) CanActivate(_ *ctx.HTTPContext) bool { return true }

type traceInterceptor struct{}

func (traceInterceptor) Intercept(_ *ctx.HTTPContext, agg *aggregation.Aggregation) any {
	return agg.Pipe()
}

type tracePipelineController struct {
	common.HTTP
	common.Middleware
	common.Guard
	common.Interceptor
}

func (c tracePipelineController) NewController() Controller {
	c.BindMiddleware(traceMiddleware{})
	c.BindGuard(traceGuard{})
	c.BindInterceptor(traceInterceptor{})
	return c
}

func (c tracePipelineController) READ_tracepipeline() string { return "ok" }

const traceSlowPipeSleep = 30 * time.Millisecond

type traceSlowPipeDTO struct{}

func (d traceSlowPipeDTO) Transform(ctx.Query, common.ArgumentMetadata) any {
	time.Sleep(traceSlowPipeSleep)
	return traceSlowPipeDTO{}
}

type tracePipeController struct {
	common.HTTP
}

func (c tracePipeController) NewController() Controller { return c }

func (c tracePipeController) READ_tracepipe(traceSlowPipeDTO) string { return "ok" }

type tracePanicController struct {
	common.HTTP
}

func (c tracePanicController) NewController() Controller { return c }

func (c tracePanicController) READ_tracepanic() string {
	panic(exception.InternalServerErrorException("boom"))
}

type tracePanickingPipeDTO struct{}

func (d tracePanickingPipeDTO) Transform(ctx.Query, common.ArgumentMetadata) any {
	panic(exception.BadRequestException("bad query"))
}

type tracePipePanicController struct {
	common.HTTP
}

func (c tracePipePanicController) NewController() Controller { return c }

func (c tracePipePanicController) READ_tracepipepanic(tracePanickingPipeDTO) string { return "ok" }

type traceHandlerPanicAfterPipeController struct {
	common.HTTP
}

func (c traceHandlerPanicAfterPipeController) NewController() Controller { return c }

func (c traceHandlerPanicAfterPipeController) READ_tracehandlerpanicafterpipe(traceSlowPipeDTO) string {
	panic(exception.InternalServerErrorException("handler exploded"))
}

type multiInterceptorA struct{}

func (multiInterceptorA) Intercept(_ *ctx.HTTPContext, agg *aggregation.Aggregation) any {
	return agg.Pipe()
}

type multiInterceptorB struct{}

func (multiInterceptorB) Intercept(_ *ctx.HTTPContext, agg *aggregation.Aggregation) any {
	return agg.Pipe()
}

type multiInterceptorC struct{}

func (multiInterceptorC) Intercept(_ *ctx.HTTPContext, agg *aggregation.Aggregation) any {
	return agg.Pipe()
}

type multiInterceptorController struct {
	common.HTTP
	common.Interceptor
}

func (c multiInterceptorController) NewController() Controller {
	c.BindInterceptor(multiInterceptorA{})
	c.BindInterceptor(multiInterceptorB{})
	c.BindInterceptor(multiInterceptorC{})
	return c
}

func (c multiInterceptorController) READ_multiintercept() string { return "ok" }

type shortCircuitInterceptor struct{}

func (shortCircuitInterceptor) Intercept(_ *ctx.HTTPContext, agg *aggregation.Aggregation) any {
	return "short-circuited"
}

type shortCircuitController struct {
	common.HTTP
	common.Interceptor
}

func (c shortCircuitController) NewController() Controller {
	c.BindInterceptor(multiInterceptorA{})
	c.BindInterceptor(shortCircuitInterceptor{})
	c.BindInterceptor(multiInterceptorC{})
	return c
}

func (c shortCircuitController) READ_shortcircuit() string { return "ok" }

type capturingMockLogger struct {
	mu   sync.Mutex
	args []any
}

func (m *capturingMockLogger) capture(args []any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.args = args
}

func (m *capturingMockLogger) lastArgs() []any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.args
}

func (m *capturingMockLogger) Debug(_ string, args ...any) { m.capture(args) }

func (m *capturingMockLogger) Info(_ string, args ...any) { m.capture(args) }

func (m *capturingMockLogger) Warn(_ string, args ...any) { m.capture(args) }

func (m *capturingMockLogger) Error(_ string, args ...any) { m.capture(args) }

func (m *capturingMockLogger) Fatal(_ string, args ...any) { m.capture(args) }

type capturedLogCall struct {
	msg  string
	args []any
}

type allCallsCapturingLogger struct {
	mu    sync.Mutex
	calls []capturedLogCall
}

func (l *allCallsCapturingLogger) capture(msg string, args []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, capturedLogCall{msg: msg, args: args})
}

func (l *allCallsCapturingLogger) findCall(msg string) *capturedLogCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.calls {
		if l.calls[i].msg == msg {
			return &l.calls[i]
		}
	}
	return nil
}

func (l *allCallsCapturingLogger) findAllCalls(msg string) []capturedLogCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	var found []capturedLogCall
	for _, c := range l.calls {
		if c.msg == msg {
			found = append(found, c)
		}
	}
	return found
}

func (l *allCallsCapturingLogger) Debug(msg string, args ...any) { l.capture(msg, args) }

func (l *allCallsCapturingLogger) Info(msg string, args ...any) { l.capture(msg, args) }

func (l *allCallsCapturingLogger) Warn(msg string, args ...any) { l.capture(msg, args) }

func (l *allCallsCapturingLogger) Error(msg string, args ...any) { l.capture(msg, args) }

func (l *allCallsCapturingLogger) Fatal(msg string, args ...any) { l.capture(msg, args) }

var testListenModule = func() *Module {
	return NewModuleBuilder().Build()
}

var testListenChildModule = func() *Module {
	return NewModuleBuilder().Build()
}

var testListenParentModule = func() *Module {
	return NewModuleBuilder().Imports(testListenChildModule).Build()
}

type z1Controller struct {
	common.HTTP
}

func (c z1Controller) NewController() Controller { return c }

func (c z1Controller) READ_users() string { return "list" }

func (c z1Controller) CREATE_users() string { return "created" }

func (c z1Controller) READ_users_BY_id(p ctx.Param) string { return "user:" + p.Get("id") }

func (c z1Controller) DELETE_users_BY_id() string { return "deleted" }

func (c z1Controller) READ_report_VERSION_1() string { return "report1" }

func (c z1Controller) READ_report_VERSION_2() string { return "report2" }

func (c z1Controller) READ_files_ANY() string { return "glob" }

type z1VersionedController struct {
	common.HTTP
}

func (c z1VersionedController) NewController() Controller { return c }

func (c z1VersionedController) READ_report_VERSION_1() string { return "report1" }

func (c z1VersionedController) READ_report_VERSION_2() string { return "report2" }

func (c z1VersionedController) READ_report_BY_id_VERSION_2() string { return "byid2" }

type z1PrefixedController struct {
	common.HTTP
}

func (c z1PrefixedController) NewController() Controller { return c }

func (c z1PrefixedController) READ_ping() string { return "pong" }

type z1OwnController struct {
	common.HTTP
}

func (c z1OwnController) NewController() Controller { return c }

func (c z1OwnController) READ_own() string { return "own" }

var z1Trace createRecorder

type z1Middleware struct{ Name string }

func (m z1Middleware) Use(_ *http.Request, _ http.ResponseWriter, next ctx.Next) {
	z1Trace.add("mw:" + m.Name)
	next()
}

type z1Guard struct{ Name string }

func (g z1Guard) CanActivate(_ *ctx.HTTPContext) bool {
	z1Trace.add("guard:" + g.Name)
	return true
}

type z1Interceptor struct{ Name string }

func (i z1Interceptor) Intercept(_ *ctx.HTTPContext, agg *aggregation.Aggregation) any {
	z1Trace.add("intercept:" + i.Name)
	return agg.Pipe()
}

type z1LayeredController struct {
	common.HTTP
	common.Middleware
	common.Guard
	common.Interceptor
}

func (c z1LayeredController) NewController() Controller {
	c.BindMiddleware(z1Middleware{Name: "c1"})
	c.BindMiddleware(z1Middleware{Name: "c2"})
	c.BindGuard(z1Guard{Name: "c1"})
	c.BindGuard(z1Guard{Name: "c2"})
	c.BindInterceptor(z1Interceptor{Name: "c1"})
	c.BindInterceptor(z1Interceptor{Name: "c2"})
	return c
}

func (c z1LayeredController) READ_layered() string {
	z1Trace.add("handler")
	return "ok"
}

var z1Built createRecorder

type z1ProvLeafA struct{}

func (p z1ProvLeafA) NewProvider() Provider { z1Built.add("leafA"); return p }

type z1ProvShared struct{}

func (p z1ProvShared) NewProvider() Provider { z1Built.add("shared"); return p }

type z1ProvLeafB struct{ Shared z1ProvShared }

func (p z1ProvLeafB) NewProvider() Provider { z1Built.add("leafB"); return p }

type z1ProvRoot struct {
	A z1ProvLeafA
	B z1ProvLeafB
}

func (p z1ProvRoot) NewProvider() Provider { z1Built.add("root"); return p }

type z1ProvRootDep struct{}

func (p z1ProvRootDep) NewProvider() Provider { z1Built.add("rootDep"); return p }

type cfByIDController struct{ common.HTTP }

func (c cfByIDController) NewController() Controller { return c }

func (c cfByIDController) READ_cf_BY_id() string { return "id" }

func (c cfByIDController) READ_cf_BY_name() string { return "name" }

type cfShapesController struct{ common.HTTP }

func (c cfShapesController) NewController() Controller { return c }

func (c cfShapesController) READ_cfs_BY_id() string { return "a" }

func (c cfShapesController) READ_cfs_BY_id_AND_name() string { return "b" }

func (c cfShapesController) UPDATE_cfs_BY_id() string { return "c" }

func (c cfShapesController) READ_cfs_all() string { return "d" }

type dvController struct{ common.HTTP }

func (c dvController) NewController() Controller { return c }

func (c dvController) READ_dv_VERSION_2() string { return "dv" }

type dvGuard struct{}

func (dvGuard) NewGuard() dvGuard { return dvGuard{} }

func (dvGuard) CanActivate(c *ctx.HTTPContext) bool { return true }

type dvGuardedController struct {
	common.HTTP
	common.Guard
}

func (c dvGuardedController) NewController() Controller {
	c.BindGuard(dvGuard{}, c.READ_dv)
	return c
}

func (c dvGuardedController) READ_dv() string { return "dv" }

func hkModule(rec *createRecorder, name string, imports ...any) *Module {
	m := NewModuleBuilder().Imports(imports...).Build()
	m.OnInit = rec.hook("init:" + name)
	m.OnReady = rec.hook("ready:" + name)
	m.OnShutdown = rec.hook("shutdown:" + name)
	return m
}

func hkDiamond(rec *createRecorder) *Module {
	b := hkModule(rec, "B")
	a := hkModule(rec, "A", b)
	c := hkModule(rec, "C", b)
	return hkModule(rec, "root", a, c)
}

func hkFilter(items []string, prefix string) []string {
	var out []string
	for _, item := range items {
		if strings.HasPrefix(item, prefix) {
			out = append(out, strings.TrimPrefix(item, prefix))
		}
	}
	return out
}

type hkProvider struct{}

var hkEvents createRecorder

func (p hkProvider) NewProvider() Provider {
	hkEvents.add("provider")
	return p
}

type hkController struct{ common.HTTP }

func (c hkController) NewController() Controller { return c }

func (c hkController) READ_hk() string { return "hk" }

type isoAlphaController struct{ common.HTTP }

func (c isoAlphaController) NewController() Controller { return c }

func (c isoAlphaController) READ_alpha() string { return "alpha" }

type isoBetaController struct{ common.HTTP }

func (c isoBetaController) NewController() Controller { return c }

func (c isoBetaController) READ_beta() string { return "beta" }

func isoExpect(t *testing.T, app *App, target string, code int, rule string) {
	t.Helper()
	if w := createGet(app, target); w.Code != code {
		t.Error(test.DiffMessage(w.Code, code, rule+": status of GET "+target))
	}
}

type isoPingController struct{ common.HTTP }

func (c isoPingController) NewController() Controller { return c }

func (c isoPingController) READ_ping() string { return "pong" }

func isoPrefixedRoot() *Module {
	child := NewModuleBuilder().Controllers(isoPingController{}).Build()
	child.Prefix("v1")
	root := NewModuleBuilder().Imports(child).Build()
	root.Prefix("api")
	return root
}

var isoBuilt atomic.Int64

type isoStateProvider struct{ State *int }

func (p isoStateProvider) NewProvider() Provider {
	isoBuilt.Add(1)
	p.State = new(int)
	return p
}

var isoControllerBuilt atomic.Int64

type isoCountedController struct{ common.HTTP }

func (c isoCountedController) NewController() Controller {
	isoControllerBuilt.Add(1)
	return c
}

func (c isoCountedController) READ_counted() string { return "counted" }

type isoPlainProvider struct{}

func (p isoPlainProvider) NewProvider() Provider { return p }

type isoLogProvider struct{ Log log.Logger }

func (p isoLogProvider) NewProvider() Provider {
	p.Log.Info("provider-ready")
	return p
}

type isoPubController struct{ common.HTTP }

func (c isoPubController) NewController() Controller { return c }

func (c isoPubController) READ_publish(pub common.Publisher) string {
	_ = pub.Publish("iso.topic")
	return "published"
}

type isoGlobalProvider struct{ Tag string }

func (p isoGlobalProvider) NewProvider() Provider { return p }

type isoNeedsGlobalProvider struct{ G isoGlobalProvider }

func (p isoNeedsGlobalProvider) NewProvider() Provider { return p }

type isoDupController struct{ common.HTTP }

func (c isoDupController) NewController() Controller { return c }

func (c isoDupController) READ_dup() string { return "dup" }

type isoWSController struct{ common.WS }

func (c isoWSController) NewController() Controller { return c }

func (c isoWSController) SUBSCRIBE_dupevent() string { return "ok" }

type isoConflictA struct{ common.HTTP }

func (c isoConflictA) NewController() Controller { return c }

func (c isoConflictA) READ_clash() string { return "a" }

type isoConflictB struct{ common.HTTP }

func (c isoConflictB) NewController() Controller { return c }

func (c isoConflictB) READ_clash() string { return "b" }

type isoEventA struct{ common.WS }

func (c isoEventA) NewController() Controller { return c }

func (c isoEventA) SUBSCRIBE_clash() string { return "a" }

type isoEventB struct{ common.WS }

func (c isoEventB) NewController() Controller { return c }

func (c isoEventB) SUBSCRIBE_clash() string { return "b" }

var isoGuardBuilt atomic.Int64

type isoGuard struct{ Role string }

func (g isoGuard) NewGuard() any {
	isoGuardBuilt.Add(1)
	return isoGuard{Role: g.Role}
}

func (g isoGuard) CanActivate(_ *ctx.HTTPContext) bool { return true }

type isoGuardedController struct {
	common.HTTP
	common.Guard
}

func (c isoGuardedController) NewController() Controller {
	c.BindGuard(isoGuard{Role: "iso-per-app"})
	return c
}

func (c isoGuardedController) READ_guarded() string { return "guarded" }

type isoGuardedOther struct {
	common.HTTP
	common.Guard
}

func (c isoGuardedOther) NewController() Controller {
	c.BindGuard(isoGuard{Role: "iso-equal"})
	c.BindGuard(isoGuard{Role: "iso-different"})
	return c
}

func (c isoGuardedOther) READ_other() string { return "other" }

type isoGuardedSame struct {
	common.HTTP
	common.Guard
}

func (c isoGuardedSame) NewController() Controller {
	c.BindGuard(isoGuard{Role: "iso-equal"})
	return c
}

func (c isoGuardedSame) READ_same() string { return "same" }

type isoConcurrentProvider struct{}

func (p isoConcurrentProvider) NewProvider() Provider { return p }

type isoConcurrentController struct{ common.HTTP }

func (c isoConcurrentController) NewController() Controller { return c }

func (c isoConcurrentController) READ_concurrent() string { return "ok" }

var lyRec createRecorder

var lyBuilt atomic.Int64

type lyGuard struct{ Role string }

func (g lyGuard) NewGuard() any {
	lyBuilt.Add(1)
	return lyGuard{Role: g.Role}
}

func (g lyGuard) CanActivate(_ *ctx.HTTPContext) bool {
	lyRec.add(g.Role)
	return true
}

type lyPlainGuard struct{ Role string }

func (g lyPlainGuard) CanActivate(_ *ctx.HTTPContext) bool {
	lyRec.add("plain:" + g.Role)
	return true
}

type lyBoth struct{ Role string }

func (b lyBoth) NewGuard() any {
	lyBuilt.Add(1)
	return lyBothGuard(b)
}

func (b lyBoth) NewMiddleware() any {
	lyBuilt.Add(100)
	return lyBothMiddleware(b)
}

func (b lyBoth) Use(_ *http.Request, _ http.ResponseWriter, next ctx.Next) { next() }

type lyBothGuard struct{ Role string }

func (g lyBothGuard) CanActivate(_ *ctx.HTTPContext) bool {
	lyRec.add("guard:" + g.Role)
	return true
}

type lyBothMiddleware struct{ Role string }

func (m lyBothMiddleware) Use(_ *http.Request, _ http.ResponseWriter, next ctx.Next) {
	lyRec.add("middleware:" + m.Role)
	next()
}

type lyFuncGuard struct {
	Role string
	Hook func()
}

func (g lyFuncGuard) NewGuard() any {
	lyBuilt.Add(1)
	return g
}

func (g lyFuncGuard) CanActivate(_ *ctx.HTTPContext) bool { return true }

type lyChanGuard struct {
	Role string
	Ch   chan int
}

func (g lyChanGuard) NewGuard() any {
	lyBuilt.Add(1)
	return g
}

func (g lyChanGuard) CanActivate(_ *ctx.HTTPContext) bool { return true }

type lyController struct{ common.HTTP }

func (c lyController) NewController() Controller { return c }

func (c lyController) READ_ly() string { return "ly" }

func lyRun(t *testing.T, setup func(app *App)) {
	t.Helper()
	lyRec = createRecorder{}
	lyBuilt.Store(0)

	app := New()
	setup(app)
	mustCreate(t, app, NewModuleBuilder().Controllers(lyController{}).Build())
	if w := createGet(app, "/ly"); w.Code != http.StatusOK {
		t.Fatal(test.DiffMessage(w.Code, http.StatusOK, "the route answers"))
	}
}

func msExpectPanic(t *testing.T, rule, want string, fn func()) {
	t.Helper()
	text, panicked := createPanicText(fn)
	if !panicked {
		t.Fatal(test.DiffMessage("no panic", want, rule+": misuse panics"))
	}
	if !strings.Contains(text, want) {
		t.Error(test.DiffMessage(text, want, rule+": the panic text"))
	}
}

func msCreated() *App {
	app := New()
	app.Create(NewModuleBuilder().Build())
	return app
}

type msMiddleware struct{}

func (msMiddleware) Use(_ *http.Request, _ http.ResponseWriter, next ctx.Next) { next() }

type pathTokenController struct{ common.HTTP }

func (c pathTokenController) NewController() Controller {
	c.Path("/oauth2/token", c.CREATE_token)
	return c
}

func (c pathTokenController) CREATE_token() string { return "token" }

func (c pathTokenController) READ_payment_intents() string {
	return "pi"
}

type pathPrefixController struct{ common.HTTP }

func (c pathPrefixController) NewController() Controller {
	c.Prefix("v1")
	c.Path("/oauth2/token", c.CREATE_token_VERSION_NEUTRAL, c.READ_info_VERSION_2)
	return c
}

func (c pathPrefixController) CREATE_token_VERSION_NEUTRAL() string { return "token" }

func (c pathPrefixController) READ_info_VERSION_2() string { return "v2" }

type pathStyleController struct{ common.HTTP }

func (c pathStyleController) NewController() Controller {
	c.Path("/a_b/{id}", c.READ_payment_intents)
	return c
}

func (c pathStyleController) READ_payment_intents(p ctx.Param) string { return "id=" + p.Get("id") }

type pathRootController struct{ common.HTTP }

func (c pathRootController) NewController() Controller {
	c.Path("/", c.READ_home_page)
	return c
}

func (c pathRootController) READ_home_page() string { return "home" }

type pathBadNameController struct{ common.HTTP }

func (c pathBadNameController) NewController() Controller {
	c.Path("/x", c.READ_orgs_FB)
	return c
}

func (c pathBadNameController) READ_orgs_FB() string { return "x" }

type pathNoHandlerController struct{ common.HTTP }

func (c pathNoHandlerController) NewController() Controller {
	c.Path("/x")
	return c
}

func (c pathNoHandlerController) READ_ok() string { return "x" }

type pathHelperController struct{ common.HTTP }

func (c pathHelperController) NewController() Controller {
	c.Path("/x", c.Helper)
	return c
}

func (c pathHelperController) READ_ok() string { return "x" }

func (c pathHelperController) Helper() string { return "x" }

type pathWSController struct {
	common.HTTP
	common.WS
}

func (c pathWSController) NewController() Controller {
	c.Path("/x", c.SUBSCRIBE_chat)
	return c
}

func (c pathWSController) READ_ok() string { return "x" }

func (c pathWSController) SUBSCRIBE_chat() string { return "x" }

type pathNotFuncController struct{ common.HTTP }

func (c pathNotFuncController) NewController() Controller {
	c.Path("/x", "text")
	return c
}

func (c pathNotFuncController) READ_ok() string { return "x" }

type pathTwiceController struct{ common.HTTP }

func (c pathTwiceController) NewController() Controller {
	c.Path("/one", c.READ_ok)
	c.Path("/two", c.READ_ok)
	return c
}

func (c pathTwiceController) READ_ok() string { return "x" }

type pathQueryController struct{ common.HTTP }

func (c pathQueryController) NewController() Controller {
	c.Path("/x?y=1", c.READ_ok)
	return c
}

func (c pathQueryController) READ_ok() string { return "x" }

type pathFragmentController struct{ common.HTTP }

func (c pathFragmentController) NewController() Controller {
	c.Path("/x#top", c.READ_ok)
	return c
}

func (c pathFragmentController) READ_ok() string { return "x" }

type pathTwoOpsController struct{ common.HTTP }

func (c pathTwoOpsController) NewController() Controller {
	c.Path("/token", c.READ_token, c.CREATE_token)
	return c
}

func (c pathTwoOpsController) READ_token() string { return "read" }

func (c pathTwoOpsController) CREATE_token() string { return "create" }

type pathSameOpController struct{ common.HTTP }

func (c pathSameOpController) NewController() Controller {
	c.Path("/same", c.READ_one, c.READ_two)
	return c
}

func (c pathSameOpController) READ_one() string { return "one" }

func (c pathSameOpController) READ_two() string { return "two" }

type pathNameConflictController struct{ common.HTTP }

func (c pathNameConflictController) NewController() Controller {
	c.Path("/users", c.READ_other)
	return c
}

func (c pathNameConflictController) READ_other() string { return "other" }

func (c pathNameConflictController) READ_users() string { return "users" }

type pathGuardDeny struct{}

var pathGuardCalls int

func (g pathGuardDeny) CanActivate(c *ctx.HTTPContext) bool {
	pathGuardCalls++
	return false
}

type pathLayerController struct {
	common.HTTP
	common.Guard
}

func (c pathLayerController) NewController() Controller {
	c.Path("/oauth2/token", c.CREATE_token)
	c.BindGuard(pathGuardDeny{}, c.CREATE_token)
	c.Prefix("v1", c.CREATE_token)
	return c
}

func (c pathLayerController) CREATE_token() string { return "token" }

func (c pathLayerController) READ_open() string { return "open" }

type psRootController struct{ common.HTTP }

func (c psRootController) NewController() Controller { return c }

func (c psRootController) READ_root_page() string { return "root" }

type psLeafController struct{ common.HTTP }

func (c psLeafController) NewController() Controller { return c }

func (c psLeafController) READ_leaf_page() string { return "leaf" }

type psMidController struct{ common.HTTP }

func (c psMidController) NewController() Controller { return c }

func (c psMidController) READ_mid_page() string { return "mid" }

type psDynamicController struct{ common.HTTP }

func (c psDynamicController) NewController() Controller { return c }

func (c psDynamicController) READ_dynamic_page() string { return "dynamic" }

func psExpect(t *testing.T, app *App, rule, target string, code int) {
	t.Helper()
	if w := createGet(app, target); w.Code != code {
		t.Error(test.DiffMessage(w.Code, code, rule+": status of GET "+target))
	}
}

func psTwoParents(first, second string) *App {
	m := NewModuleBuilder().Controllers(psLeafController{}).Build()
	a := NewModuleBuilder().PathStyle(common.PathStyleAsWritten).Imports(m).Build()
	b := NewModuleBuilder().PathStyle(common.PathStyleKebab).Imports(m).Build()
	byName := map[string]*Module{"A": a, "B": b}
	app := New()
	app.Create(NewModuleBuilder().Imports(byName[first], byName[second]).Build())
	return app
}

type psPrefixController struct{ common.HTTP }

func (c psPrefixController) NewController() Controller {
	c.Prefix("my_ctl")
	return c
}

func (c psPrefixController) READ_root_page() string { return "p" }

type psParamController struct{ common.HTTP }

func (c psParamController) NewController() Controller { return c }

func (c psParamController) READ_user_profile_BY_user_id_VERSION_NEUTRAL(p ctx.Param) string {
	return p.Get("user_id")
}

func (c psParamController) READ_ver_sion_VERSION_v1_v1() string { return "versioned" }

type psWSController struct{ common.WS }

func (c psWSController) NewController() Controller { return c }

func (c psWSController) SUBSCRIBE_chat_room() string { return "room" }

type psUpperController struct{ common.HTTP }

func (c psUpperController) NewController() Controller { return c }

func (c psUpperController) READ_orgs_FB() string { return "x" }

type psHelperController struct{ common.HTTP }

func (c psHelperController) NewController() Controller { return c }

func (c psHelperController) READ_ok() string { return "ok" }

func (c psHelperController) Helper_FB() string { return "helper" }

type psMalformedController struct{ common.HTTP }

func (c psMalformedController) NewController() Controller { return c }

func (c psMalformedController) READ_users_BY() string { return "x" }

var pvBuilt atomic.Int64

type pvOnceProvider struct{}

func (p pvOnceProvider) NewProvider() Provider {
	pvBuilt.Add(1)
	return p
}

type pvTagProvider struct{ Value string }

func (p pvTagProvider) NewProvider() Provider {
	p.Value = "built"
	return p
}

type pvTagReader struct{ Tag pvTagProvider }

var pvSeen createRecorder

func (p pvTagReader) NewProvider() Provider {
	pvSeen.add("reader:" + p.Tag.Value)
	return p
}

type pvChainC struct{ B pvChainB }

type pvChainB struct{ A pvChainA }

type pvChainA struct{}

var pvChain createRecorder

func (p pvChainA) NewProvider() Provider { pvChain.add("A"); return p }

func (p pvChainB) NewProvider() Provider { pvChain.add("B"); return p }

func (p pvChainC) NewProvider() Provider { pvChain.add("C"); return p }

type pvGlobalDep struct{ Value string }

func (p pvGlobalDep) NewProvider() Provider {
	p.Value = "built"
	return p
}

type pvGlobalUser struct{ G pvGlobalDep }

var pvGlobalSeen createRecorder

func (p pvGlobalUser) NewProvider() Provider {
	pvGlobalSeen.add("user:" + p.G.Value)
	return p
}

type pvDeepDep struct{ Value string }

func (p pvDeepDep) NewProvider() Provider {
	p.Value = "built"
	return p
}

type pvDeepUser struct{ D pvDeepDep }

var pvDeepSeen createRecorder

func (p pvDeepUser) NewProvider() Provider {
	pvDeepSeen.add("user:" + p.D.Value)
	return p
}

type pvUnresolvedDep struct{}

func (p pvUnresolvedDep) NewProvider() Provider { return p }

type pvUnresolvedUser struct{ Missing pvUnresolvedDep }

func (p pvUnresolvedUser) NewProvider() Provider { return p }

type pvHiddenUser struct {
	hidden pvUnresolvedDep //nolint:unused
}

func (p pvHiddenUser) NewProvider() Provider { return p }

type cpPageController struct{ common.HTTP }

func (c cpPageController) NewController() Controller { return c }

func (c cpPageController) RENDER_x() {}

type cpAnyResultController struct{ common.HTTP }

func (c cpAnyResultController) NewController() Controller { return c }

func (c cpAnyResultController) RENDER_x() any { return 1 }

func (c cpAnyResultController) RENDER_s() string { return "s" }

func (c cpAnyResultController) RENDER_n() {}

func (c cpAnyResultController) RENDER_i() (int, ctx.Map) { return 422, nil }

func (c cpAnyResultController) RENDER_e() error { return nil }

func (c cpAnyResultController) SERVE_files() string { return "." }

type cpBookingParam struct{ common.HTTP }

func (c cpBookingParam) NewController() Controller { return c }

func (c cpBookingParam) RENDER_bookings_BY_id(ctx.Param) {}

type cpPartialsPrefix struct{ common.HTTP }

func (c cpPartialsPrefix) NewController() Controller {
	c.Prefix("partials")
	return c
}

func (c cpPartialsPrefix) RENDER_app() {}

type cpAssetsPrefix struct{ common.HTTP }

func (c cpAssetsPrefix) NewController() Controller {
	c.Prefix("assets")
	return c
}

func (c cpAssetsPrefix) RENDER_app() {}

type cpPublicPath struct{ common.HTTP }

func (c cpPublicPath) NewController() Controller {
	c.Path("/public/app", c.RENDER_x)
	return c
}

func (c cpPublicPath) RENDER_x() {}

type cpAny struct{ common.HTTP }

func (c cpAny) NewController() Controller { return c }

func (c cpAny) RENDER_files_ANY() {}

type cpVersion struct{ common.HTTP }

func (c cpVersion) NewController() Controller { return c }

func (c cpVersion) RENDER_x_VERSION_1() {}

type cpFile struct{ common.HTTP }

func (c cpFile) NewController() Controller { return c }

func (c cpFile) RENDER_feed_xml_FILE() {}

type cpSubmitAny struct{ common.HTTP }

func (c cpSubmitAny) NewController() Controller { return c }

func (c cpSubmitAny) RENDER_x() {}

func (c cpSubmitAny) SUBMIT_x_ANY() {}

type cpSubmitVersion struct{ common.HTTP }

func (c cpSubmitVersion) NewController() Controller { return c }

func (c cpSubmitVersion) RENDER_x() {}

func (c cpSubmitVersion) SUBMIT_x_VERSION_1() {}

type cpSubmitFile struct{ common.HTTP }

func (c cpSubmitFile) NewController() Controller { return c }

func (c cpSubmitFile) RENDER_x() {}

func (c cpSubmitFile) SUBMIT_x_FILE() {}

type cpWildcardPath struct{ common.HTTP }

func (c cpWildcardPath) NewController() Controller {
	c.Path("/docs/*", c.RENDER_x)
	return c
}

func (c cpWildcardPath) RENDER_x() {}

type cpWildcardPrefix struct{ common.HTTP }

func (c cpWildcardPrefix) NewController() Controller {
	c.Prefix("docs/*")
	return c
}

func (c cpWildcardPrefix) RENDER_x() {}

type cpR404 struct{ common.HTTP }

func (c cpR404) NewController() Controller { return c }

func (c cpR404) RENDER_404() {}

type cpR5xx struct{ common.HTTP }

func (c cpR5xx) NewController() Controller { return c }

func (c cpR5xx) RENDER_5xx() {}

type cpPath404 struct{ common.HTTP }

func (c cpPath404) NewController() Controller {
	c.Path("/404", c.RENDER_x)
	return c
}

func (c cpPath404) RENDER_x() {}

type cpAdmin404 struct{ common.HTTP }

func (c cpAdmin404) NewController() Controller {
	c.Prefix("admin")
	return c
}

func (c cpAdmin404) RENDER_404() {}

type cpSubmitOnly struct{ common.HTTP }

func (c cpSubmitOnly) NewController() Controller { return c }

func (c cpSubmitOnly) SUBMIT_x() {}

type cpRenderElsewhere struct{ common.HTTP }

func (c cpRenderElsewhere) NewController() Controller { return c }

func (c cpRenderElsewhere) RENDER_x() {}

type cpSubmitPath struct{ common.HTTP }

func (c cpSubmitPath) NewController() Controller {
	c.Path("/y", c.SUBMIT_x)
	return c
}

func (c cpSubmitPath) RENDER_x() {}

func (c cpSubmitPath) SUBMIT_x() {}

type cpSubmitPrefix struct{ common.HTTP }

func (c cpSubmitPrefix) NewController() Controller {
	c.Prefix("p", c.SUBMIT_x)
	return c
}

func (c cpSubmitPrefix) RENDER_x() {}

func (c cpSubmitPrefix) SUBMIT_x() {}

type cpSubmitMovesWithRender struct{ common.HTTP }

func (c cpSubmitMovesWithRender) NewController() Controller {
	c.Path("/moved", c.RENDER_x)
	c.Prefix("p", c.RENDER_x)
	return c
}

func (c cpSubmitMovesWithRender) RENDER_x() {}

func (c cpSubmitMovesWithRender) SUBMIT_x(h *ctx.HTTPContext) { h.Redirect("/done") }

func cpFS() fstest.MapFS {
	return fstest.MapFS{"x.html": {Data: []byte("x")}}
}

func cpCreate(opts *RenderOptions, controllers ...Controller) func() {
	return func() {
		app := New()
		if opts != nil {
			app.EnableRender(opts)
		}
		app.Create(NewModuleBuilder().Controllers(controllers...).Build())
	}
}

func cpCreateFiles(files map[string]string) func() {
	return cpCreate(&RenderOptions{FS: pgFiles(files), Dir: "."}, cpPageController{})
}

func cpCreateWith(files map[string]string, controllers ...Controller) func() {
	return cpCreate(&RenderOptions{FS: pgFiles(files), Dir: "."}, controllers...)
}

type cpSubZ struct{ common.HTTP }

func (c cpSubZ) NewController() Controller { return c }

func (c cpSubZ) RENDER_z_OF_sub() {}

type rvDep struct{ Value string }

func (p rvDep) NewProvider() Provider {
	p.Value = "built"
	return p
}

func rvGlobal() *Module {
	m := NewModuleBuilder().Providers(rvDep{}).Build()
	m.IsGlobal = true
	return m
}

func rvFuncModule(seen *createRecorder) func(d rvDep) *Module {
	return func(d rvDep) *Module {
		seen.add("func:" + d.Value)
		return NewModuleBuilder().Build()
	}
}

func rvCreate(t *testing.T, root *Module, rule string) {
	t.Helper()
	if text, panicked := createPanicText(func() { New().Create(root) }); panicked {
		t.Fatal(test.DiffMessage(text, "no panic", rule))
	}
}

var rvOrder createRecorder

type rvOrderDep struct{}

func (p rvOrderDep) NewProvider() Provider {
	rvOrder.add("provider")
	return p
}

func rvRelease(t *testing.T, release chan struct{}) {
	t.Helper()
	select {
	case <-release:
	default:
		close(release)
	}
}

func rvStopWaitsForHooks(t *testing.T, app *App, entered, release chan struct{}, finished *atomic.Bool, rule string) {
	t.Helper()
	defer rvRelease(t, release)

	first := make(chan struct{})
	go func() {
		app.Stop()
		close(first)
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal(test.DiffMessage("timeout", "hook entered", rule))
	}

	secondFinished := make(chan bool, 1)
	go func() {
		app.Stop()
		secondFinished <- finished.Load()
	}()

	select {
	case done := <-secondFinished:
		t.Error(test.DiffMessage(done, false, rule+": a second Stop returned while a hook was still running"))
	case <-time.After(100 * time.Millisecond):
	}

	rvRelease(t, release)
	select {
	case done := <-secondFinished:
		if !done {
			t.Error(test.DiffMessage(done, true, rule+": the second Stop returns only after the hooks finished"))
		}
	case <-time.After(2 * time.Second):
		t.Error(test.DiffMessage("timeout", "second Stop returns", rule))
	}
	select {
	case <-first:
	case <-time.After(2 * time.Second):
		t.Error(test.DiffMessage("timeout", "first Stop returns", rule))
	}
}

var svRoot string

type svNameAnyController struct{ common.HTTP }

func (c svNameAnyController) NewController() Controller { return c }

func (c svNameAnyController) SERVE_static_ANY() string { return svRoot }

type svAnyOfController struct{ common.HTTP }

func (c svAnyOfController) NewController() Controller { return c }

func (c svAnyOfController) SERVE_ANY_OF_static() string { return svRoot }

func svWrite(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func svApp(t *testing.T) *App {
	t.Helper()
	svRoot = filepath.Join(t.TempDir(), "public")
	svWrite(t, filepath.Join(svRoot, "a.txt"), "alpha")
	svWrite(t, filepath.Join(svRoot, "sub", "x.txt"), "nested")

	app := New()
	mustCreate(t, app, NewModuleBuilder().Controllers(svNameAnyController{}).Build())
	return app
}

const freshProcessEnv = "GINJECT_FRESH_PROCESS"

func freshProcess(t *testing.T, body func(t *testing.T)) {
	t.Helper()
	if os.Getenv(freshProcessEnv) == t.Name() {
		body(t)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), freshProcessEnv+"="+t.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed in a fresh process:\n%s", t.Name(), out)
	}
	if !bytes.Contains(out, []byte("--- PASS: "+t.Name())) {
		t.Fatalf("%s did not run in the fresh process:\n%s", t.Name(), out)
	}
}

func createPanicText(fn func()) (text string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			text = fmt.Sprint(r)
		}
	}()
	fn()
	return "", false
}

func createPanicValue(fn func()) (value any, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			value = r
		}
	}()
	fn()
	return nil, false
}

type createRecorder struct {
	mu    sync.Mutex
	items []string
}

func (r *createRecorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, s)
}

func (r *createRecorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.items...)
}

func (r *createRecorder) count(s string) int {
	n := 0
	for _, item := range r.list() {
		if item == s {
			n++
		}
	}
	return n
}

func (r *createRecorder) hook(s string) func() {
	return func() { r.add(s) }
}

type createLogCall struct {
	level string
	msg   string
	args  []any
}

type createLogger struct {
	mu    sync.Mutex
	calls []createLogCall
}

func (l *createLogger) record(level, msg string, args []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, createLogCall{level: level, msg: msg, args: append([]any(nil), args...)})
}

func (l *createLogger) Debug(msg string, args ...any) { l.record("debug", msg, args) }

func (l *createLogger) Info(msg string, args ...any) { l.record("info", msg, args) }

func (l *createLogger) Warn(msg string, args ...any) { l.record("warn", msg, args) }

func (l *createLogger) Error(msg string, args ...any) { l.record("error", msg, args) }

func (l *createLogger) Fatal(msg string, args ...any) { l.record("fatal", msg, args) }

func (l *createLogger) named(msg string) []createLogCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []createLogCall
	for _, c := range l.calls {
		if c.msg == msg {
			out = append(out, c)
		}
	}
	return out
}

func createRequest(app *App, method, target string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

func createGet(app *App, target string) *httptest.ResponseRecorder {
	return createRequest(app, http.MethodGet, target)
}

func createDump(v reflect.Value) string {
	var b strings.Builder
	dumpValue(&b, v, map[uintptr]bool{})
	return b.String()
}

func dumpValue(b *strings.Builder, v reflect.Value, seen map[uintptr]bool) {
	if !v.IsValid() {
		b.WriteString("invalid")
		return
	}
	b.WriteString(v.Type().String())
	b.WriteByte('{')
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			b.WriteString("nil")
		} else if seen[v.Pointer()] {
			b.WriteString("cycle")
		} else {
			seen[v.Pointer()] = true
			dumpValue(b, v.Elem(), seen)
			delete(seen, v.Pointer())
		}
	case reflect.Interface:
		if v.IsNil() {
			b.WriteString("nil")
		} else {
			dumpValue(b, v.Elem(), seen)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			b.WriteString(v.Type().Field(i).Name)
			b.WriteByte('=')
			dumpValue(b, v.Field(i), seen)
			b.WriteByte(';')
		}
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			b.WriteString("nil")
			break
		}
		for i := 0; i < v.Len(); i++ {
			dumpValue(b, v.Index(i), seen)
			b.WriteByte(',')
		}
	case reflect.Map:
		if v.IsNil() {
			b.WriteString("nil")
			break
		}
		entries := make([]string, 0, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			var e strings.Builder
			dumpValue(&e, iter.Key(), seen)
			e.WriteString("=>")
			dumpValue(&e, iter.Value(), seen)
			entries = append(entries, e.String())
		}
		sort.Strings(entries)
		b.WriteString(strings.Join(entries, ","))
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		fmt.Fprintf(b, "nil=%v", v.IsNil())
	case reflect.String:
		b.WriteString(v.String())
	case reflect.Bool:
		fmt.Fprintf(b, "%v", v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fmt.Fprintf(b, "%d", v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		fmt.Fprintf(b, "%d", v.Uint())
	case reflect.Float32, reflect.Float64:
		fmt.Fprintf(b, "%v", v.Float())
	}
	b.WriteByte('}')
}

func mustCreate(t *testing.T, app *App, m *Module) {
	t.Helper()
	if text, panicked := createPanicText(func() { app.Create(m) }); panicked {
		t.Fatal(test.DiffMessage(text, "no panic", "Create succeeds"))
	}
}

func sprint(v any) string { return fmt.Sprint(v) }

func newHTTPContext() *ctx.HTTPContext {
	c := ctx.NewHTTPContext()
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.ResponseWriter = httptest.NewRecorder()
	return c
}

func defaultFilterBody(ex exception.Exception) (int, string, string) {
	filter := globalHTTPExceptionFilter{}
	c := newHTTPContext()
	w := c.ResponseWriter.(*httptest.ResponseRecorder)
	filter.Catch(c, &ex)
	return w.Code, w.Header().Get("Content-Type"), w.Body.String()
}

type dvrPage struct{ common.HTTP }

func (c dvrPage) NewController() Controller { return c }

func (c dvrPage) RENDER_dvrpage() {}

func (c dvrPage) SUBMIT_dvrpage() {}

type dvrAPI struct{ common.HTTP }

func (c dvrAPI) NewController() Controller { return c }

func (c dvrAPI) READ_dvrapi() string { return "api" }

type dvrSnap struct{ common.HTTP }

func (c dvrSnap) NewController() Controller { return c }

func (c dvrSnap) RENDER_a() {}

func (c dvrSnap) RENDER_c_OF_b() {}

func (c dvrSnap) RENDER_nodata() {}

func (c dvrSnap) RENDER_rows() {}

func (c dvrSnap) RENDER_user_BY_id(ctx.Param) {}

func dvrDecode(t *testing.T, app *App) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(app.devtool.GetConfigurationResponse)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func dvrKinds(t *testing.T, snapshot map[string]any) map[string]any {
	t.Helper()
	controller, _ := snapshot["controller"].(map[string]any)
	entries, _ := controller["http"].([]any)
	kinds := map[string]any{}
	for _, e := range entries {
		entry, _ := e.(map[string]any)
		route, _ := entry["route"].(string)
		method, _ := entry["http_method"].(string)
		kinds[method+" "+route] = entry["kind"]
	}
	return kinds
}

func dvrApp(t *testing.T, files map[string]string, controllers ...Controller) *App {
	t.Helper()
	app := New()
	app.SetLogger(&lcRecordingLogger{})
	app.EnableDevtool()
	if files != nil {
		app.EnableRender(&RenderOptions{FS: pgFiles(files), Dir: "."})
	}
	mustCreate(t, app, NewModuleBuilder().Controllers(controllers...).Build())
	return app
}

func dvrSnapFiles() map[string]string {
	return map[string]string{
		"layout.html":             `{{pageAssets}}<html>{{template "content" .}}</html>`,
		"layout.css":              `l{}`,
		"a.html":                  `A`,
		"a.css":                   `a{}`,
		"b/layout.html":           `{{pageAssets}}<div>{{template "content" .}}</div>`,
		"b/c.html":                `C`,
		"nodata.html":             `{{template "partials/p" .}}`,
		"rows.html":               `{{template "partials/q" (dict "odd")}}`,
		"user/{id}.html":          `U`,
		"partials/p.html":         `P`,
		"partials/q.html":         `Q`,
		"404.html":                `{{.Code}}`,
		"5xx.html":                `{{.Code}}`,
		"assets/app.css":          `a{}`,
		"assets/js/app.js":        `1`,
		"assets/.hidden/x.js":     `2`,
		"public/robots.txt":       `User-agent: *`,
		"public/site.webmanifest": `{}`,
	}
}

type appLookalikeController struct {
	common.HTTP
}

func (c appLookalikeController) NewController() Controller { return c }

func (c appLookalikeController) READ_lookalike(x *ctxlookalike.HTTPContext) string {
	return ""
}

type appResultController struct {
	common.HTTP
}

func (c appResultController) NewController() Controller { return c }

func (c appResultController) READ_pointer() *returnUser {
	return &returnUser{Name: "ann"}
}

func (c appResultController) READ_created() (int, *returnUser) {
	return http.StatusAccepted, &returnUser{Name: "bob"}
}

func (c appResultController) READ_array() [3]int { return [3]int{1, 2, 3} }

func (c appResultController) READ_anystring() any { return "ok" }

func (c appResultController) READ_anyint() any { return 7 }

func (c appResultController) READ_wrongpipe(p nilPipe) string {
	return "unreachable"
}

func serveResult(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	app := New()
	app.Create(NewModuleBuilder().Controllers(appResultController{}).Build())
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

var appServeDir string

type appServeController struct {
	common.HTTP
}

func (c appServeController) NewController() Controller { return c }

func (c appServeController) SERVE_logo_png_FILE() string {
	return filepath.Join(appServeDir, "logo.png")
}

func (c appServeController) READ_named() string { return "named" }

func handlerEventNames(t *testing.T, path string) []string {
	t.Helper()
	appServeDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(appServeDir, "logo.png"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := New()
	var mu sync.Mutex
	var names []string
	app.tracer.Listen(func(e trace.Event) {
		mu.Lock()
		defer mu.Unlock()
		if te := e; te.Stage == trace.StageHandler {
			names = append(names, te.Name)
		}
	})
	app.Create(NewModuleBuilder().Controllers(appServeController{}).Build())

	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))

	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), names...)
}

type benchQueryDTO struct {
	Name string
	Page int
}

func (d benchQueryDTO) Transform(ctx.Query, common.ArgumentMetadata) any { return d }

type benchProviderDTO struct {
	P    fnTestProvider
	Name string
}

func (d benchProviderDTO) Transform(ctx.Body, common.ArgumentMetadata) any { return d }

type benchWSDTO struct {
	Topic string
}

func (d benchWSDTO) Transform(ctx.WSPayload, common.ArgumentMetadata) any { return d }

type benchPlanCtl struct{}

func (benchPlanCtl) NoArgs() string { return "ok" }

func (benchPlanCtl) Param(ctx.Param) ctx.Map {
	return nil
}

func (benchPlanCtl) QueryPipe(benchQueryDTO) ctx.Map { return nil }

func (benchPlanCtl) BodyPipeWithProvider(benchProviderDTO) ctx.Map { return nil }

func (benchPlanCtl) FiveBuiltins(*ctx.HTTPContext, *http.Request, http.ResponseWriter, ctx.Query, ctx.Header) {
}

func (benchPlanCtl) Payload(ctx.WSPayload) {}

func (benchPlanCtl) PayloadPipe(benchWSDTO) {}

func benchPlanContext() *ctx.HTTPContext {
	c := benchHTTPContext("/?name=ann&page=2")
	ctx.HTTPInit(c, c.ResponseWriter, c.Request)
	ctx.HTTPSetParams(c, map[string][]int{"id": {0}}, []string{"7"})
	return c
}

func benchHTTPPlan(b *testing.B, name string) *httpHandlerPlan {
	b.Helper()
	recv, m := planMethod(b, benchPlanCtl{}, name)
	return newHTTPHandlerPlan(recv, m, planProviders(), newContainer())
}

func benchWSPlan(b *testing.B, name string) *wsHandlerPlan {
	b.Helper()
	recv, m := planMethod(b, benchPlanCtl{}, name)
	return newWSHandlerPlan(recv, m, nil, newContainer())
}

func benchWSContext() *ctx.WSContext {
	c := ctx.NewWSContext()
	ctx.WSInit(c, &websocket.Conn{})
	ctx.WSSetPayload(c, ctx.WSPayload{"topic": "chat"})
	return c
}

var (
	planPipeCalls     []planPipeCall
	planOrder         []string
	planTransformHits int
	planNextCalls     int
)

type planPipeCall struct {
	Kind string
	Src  any
	Meta common.ArgumentMetadata
}

type planPublisher struct{}

func (*planPublisher) Publish(string, ...any) error { return nil }

type planTraceLog struct {
	mu     sync.Mutex
	events []trace.Event
	stop   func()
}

func planListen(ev *trace.Tracer) *planTraceLog {
	l := &planTraceLog{}
	l.stop = ev.Listen(func(e trace.Event) {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.events = append(l.events, e)
	})
	return l
}

func (l *planTraceLog) all() []trace.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]trace.Event(nil), l.events...)
}

func (l *planTraceLog) byStage(stage string) []trace.Event {
	var out []trace.Event
	for _, e := range l.all() {
		if e.Stage == stage {
			out = append(out, e)
		}
	}
	return out
}

func planPanicOf(f func()) (rec any) {
	defer func() { rec = recover() }()
	f()
	return nil
}

func planExceptionOf(rec any) (exception.Exception, bool) {
	switch v := rec.(type) {
	case exception.Exception:
		return v, true
	case *exception.Exception:
		if v != nil {
			return *v, true
		}
	}
	return exception.Exception{}, false
}

func planUnresolved(funcType string, index int) string {
	return "dependency injection: can't resolve dependencies of the '" + funcType +
		"'. Please make sure that the argument dependency at index [" + strconv.Itoa(index) +
		"] is available in the handler"
}

func planSameMap(a, b any) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

var planCtr = newContainer()

func planInstallPublisher(t testing.TB) *planPublisher {
	t.Helper()
	p := &planPublisher{}
	planCtr = newContainer()
	planCtr.addInterface(publisherKey, common.Publisher(p))
	t.Cleanup(func() { planCtr = newContainer() })
	return p
}

func planRemovePublisher(t testing.TB) {
	t.Helper()
	planCtr = newContainer()
	t.Cleanup(func() { planCtr = newContainer() })
}

func planResetState(t testing.TB) {
	t.Helper()
	planPipeCalls = nil
	planOrder = nil
	planTransformHits = 0
	planNextCalls = 0
	t.Cleanup(func() {
		planPipeCalls = nil
		planOrder = nil
		planTransformHits = 0
		planNextCalls = 0
	})
}

func planProviders() map[string]Provider {
	return map[string]Provider{
		genFieldKey(reflect.TypeOf(fnTestProvider{})): fnTestProvider{Tag: "injected"},
	}
}

func planMethod(t testing.TB, controller any, name string) (reflect.Value, reflect.Method) {
	t.Helper()
	m, ok := reflect.TypeOf(controller).MethodByName(name)
	if !ok {
		t.Fatalf("fixture: %T has no method %s", controller, name)
	}
	return reflect.ValueOf(controller), m
}

func planHTTP(t testing.TB, controller any, name string, providers map[string]Provider) *httpHandlerPlan {
	t.Helper()
	recv, m := planMethod(t, controller, name)
	return newHTTPHandlerPlan(recv, m, providers, planCtr)
}

func planRequestContext() *ctx.HTTPContext {
	r := httptest.NewRequest(http.MethodPost, "/items?q=1&q=2", strings.NewReader(`{"a":1}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Probe", "p")
	c := ctx.NewHTTPContext()
	ctx.HTTPInit(c, httptest.NewRecorder(), r)
	ctx.HTTPSetParams(c, map[string][]int{"id": {0}}, []string{"7"})
	return c
}

func planInvoke(p *httpHandlerPlan, c *ctx.HTTPContext) []reflect.Value {
	return p.invoke(c, &trace.Tracer{})
}

type recCtxPipe struct{ Kind string }

func (d recCtxPipe) Transform(c *ctx.HTTPContext, m common.ArgumentMetadata) any {
	planPipeCalls = append(planPipeCalls, planPipeCall{"context", c, m})
	d.Kind = "context"
	return d
}

type recBodyPipe struct{ Kind string }

func (d recBodyPipe) Transform(b ctx.Body, m common.ArgumentMetadata) any {
	planPipeCalls = append(planPipeCalls, planPipeCall{"body", b, m})
	d.Kind = "body"
	return d
}

type provBodyPipe struct {
	P    fnTestProvider
	Kind string
	Name string
}

func (d provBodyPipe) Transform(b ctx.Body, m common.ArgumentMetadata) any {
	planPipeCalls = append(planPipeCalls, planPipeCall{"body", b, m})
	d.Kind = "body"
	return d
}

type recFormPipe struct{ Kind string }

func (d recFormPipe) Transform(f ctx.Form, m common.ArgumentMetadata) any {
	planPipeCalls = append(planPipeCalls, planPipeCall{"form", f, m})
	d.Kind = "form"
	return d
}

type recQueryPipe struct{ Kind string }

func (d recQueryPipe) Transform(q ctx.Query, m common.ArgumentMetadata) any {
	planPipeCalls = append(planPipeCalls, planPipeCall{"query", q, m})
	d.Kind = "query"
	return d
}

type recHeaderPipe struct{ Kind string }

func (d recHeaderPipe) Transform(h ctx.Header, m common.ArgumentMetadata) any {
	planPipeCalls = append(planPipeCalls, planPipeCall{"header", h, m})
	d.Kind = "header"
	return d
}

type recParamPipe struct{ Kind string }

func (d recParamPipe) Transform(p ctx.Param, m common.ArgumentMetadata) any {
	planPipeCalls = append(planPipeCalls, planPipeCall{"param", p, m})
	d.Kind = "param"
	return d
}

type recFilePipe struct{ Kind string }

func (d recFilePipe) Transform(f ctx.File, m common.ArgumentMetadata) any {
	planPipeCalls = append(planPipeCalls, planPipeCall{"file", f, m})
	d.Kind = "file"
	return d
}

type recWSPayloadPipe struct{ Kind string }

func (d recWSPayloadPipe) Transform(p ctx.WSPayload, m common.ArgumentMetadata) any {
	planPipeCalls = append(planPipeCalls, planPipeCall{"wsPayload", p, m})
	d.Kind = "wsPayload"
	return d
}

type scalarPipe int

func (scalarPipe) Transform(p ctx.Param, _ common.ArgumentMetadata) any {
	planTransformHits++
	return scalarPipe(len(p) + 41)
}

type mapPipe map[string]int

func (mapPipe) Transform(ctx.Query, common.ArgumentMetadata) any {
	return map[string]int{"a": 1}
}

type nilPipe struct{}

func (nilPipe) Transform(ctx.Query, common.ArgumentMetadata) any { return nil }

type pointerResultPipe struct{}

func (pointerResultPipe) Transform(ctx.Query, common.ArgumentMetadata) any {
	return &pointerResultPipe{}
}

type otherResultPipe struct{}

func (otherResultPipe) Transform(ctx.Query, common.ArgumentMetadata) any { return "text" }

type nilWSPipe struct{}

func (nilWSPipe) Transform(ctx.WSPayload, common.ArgumentMetadata) any { return nil }

type countingPipe struct{}

func (countingPipe) Transform(ctx.Query, common.ArgumentMetadata) any {
	planTransformHits++
	return countingPipe{}
}

type seenPipe struct {
	Seen int
	Log  []string
}

func (d seenPipe) Transform(ctx.Query, common.ArgumentMetadata) any {
	d.Seen++
	d.Log = append(d.Log, "x")
	return d
}

type orderPipeA struct{}

func (d orderPipeA) Transform(ctx.Query, common.ArgumentMetadata) any {
	planOrder = append(planOrder, "a")
	return d
}

type orderPipeB struct{}

func (d orderPipeB) Transform(ctx.Header, common.ArgumentMetadata) any {
	planOrder = append(planOrder, "b")
	return d
}

type orderPipeC struct{}

func (d orderPipeC) Transform(ctx.Param, common.ArgumentMetadata) any {
	planOrder = append(planOrder, "c")
	return d
}

type panicPipe struct{}

func (panicPipe) Transform(ctx.Header, common.ArgumentMetadata) any {
	planOrder = append(planOrder, "boom")
	panic("pipe exploded")
}

type slowPipe struct{}

func (d slowPipe) Transform(ctx.Query, common.ArgumentMetadata) any {
	time.Sleep(20 * time.Millisecond)
	return d
}

type slowPipeB struct{}

func (d slowPipeB) Transform(ctx.Header, common.ArgumentMetadata) any {
	time.Sleep(20 * time.Millisecond)
	return d
}

type provQueryPipe struct{ P fnTestProvider }

func (d provQueryPipe) Transform(ctx.Query, common.ArgumentMetadata) any { return d }

type ifaceQueryPipe struct{ Pub common.Publisher }

func (d ifaceQueryPipe) Transform(ctx.Query, common.ArgumentMetadata) any { return d }

type unexportedFieldPipe struct{ hidden int }

func (d unexportedFieldPipe) Transform(ctx.Query, common.ArgumentMetadata) any {
	return unexportedFieldPipe{hidden: d.hidden}
}

type unresolvedFieldPipe struct{ Missing unresolvableProviderType }

func (d unresolvedFieldPipe) Transform(ctx.Query, common.ArgumentMetadata) any { return d }

type mutPipe struct{ Seen int }

func (d mutPipe) Transform(ctx.Query, common.ArgumentMetadata) any {
	d.Seen++
	return d
}

type planUnknownArg struct{}

type planCtl struct {
	calls int
	tag   string
	got   []any
}

func (h *planCtl) HCtx(c *ctx.HTTPContext) { h.calls++; h.got = []any{c} }

func (h *planCtl) HReq(r *http.Request) { h.calls++; h.got = []any{r} }

func (h *planCtl) HWriter(w http.ResponseWriter) {
	h.calls++
	h.got = []any{w}
}

func (h *planCtl) HBody(b ctx.Body) { h.calls++; h.got = []any{b} }

func (h *planCtl) HForm(f ctx.Form) { h.calls++; h.got = []any{f} }

func (h *planCtl) HQuery(q ctx.Query) { h.calls++; h.got = []any{q} }

func (h *planCtl) HHeader(x ctx.Header) { h.calls++; h.got = []any{x} }

func (h *planCtl) HParam(p ctx.Param) { h.calls++; h.got = []any{p} }

func (h *planCtl) HFile(f ctx.File) { h.calls++; h.got = []any{f} }

func (h *planCtl) HNext(n ctx.Next) { h.calls++; n() }

func (h *planCtl) HRedirect(r ctx.Redirect) {
	h.calls++
	r("/elsewhere")
}

func (h *planCtl) HPublisher(p common.Publisher) { h.calls++; h.got = []any{p} }

func (h *planCtl) HNone() { h.calls++ }

func (h *planCtl) HOne() string { h.calls++; return "one" }

func (h *planCtl) HTwo() (int, string) { h.calls++; return 201, "two" }

func (h *planCtl) HThree() (int, string, any) { h.calls++; return 202, "a", "b" }

func (h *planCtl) HTag() string { h.calls++; return h.tag }

func (h *planCtl) HPanics() { h.calls++; panic("handler exploded") }

func (h *planCtl) HCtxPipe(p recCtxPipe) { h.calls++; h.got = []any{p} }

func (h *planCtl) HBodyPipe(p recBodyPipe) { h.calls++; h.got = []any{p} }

func (h *planCtl) HFormPipe(p recFormPipe) { h.calls++; h.got = []any{p} }

func (h *planCtl) HQueryPipe(p recQueryPipe) { h.calls++; h.got = []any{p} }

func (h *planCtl) HHeaderPipe(p recHeaderPipe) { h.calls++; h.got = []any{p} }

func (h *planCtl) HParamPipe(p recParamPipe) { h.calls++; h.got = []any{p} }

func (h *planCtl) HFilePipe(p recFilePipe) { h.calls++; h.got = []any{p} }

func (h *planCtl) HScalarPipe(p scalarPipe) { h.calls++; h.got = []any{p} }

func (h *planCtl) HMapPipe(p mapPipe) { h.calls++; h.got = []any{p} }

func (h *planCtl) HNilPipe(p nilPipe) { h.calls++; h.got = []any{p} }

func (h *planCtl) HPointerResult(p pointerResultPipe) {
	h.calls++
	h.got = []any{p}
}

func (h *planCtl) HOtherResult(p otherResultPipe) { h.calls++; h.got = []any{p} }

func (h *planCtl) HCountingPipe(p countingPipe) { h.calls++; h.got = []any{p} }

func (h *planCtl) HSeenPipe(p seenPipe) { h.calls++; h.got = []any{p.Seen, len(p.Log)} }

func (h *planCtl) HProvQuery(p provQueryPipe) { h.calls++; h.got = []any{p} }

func (h *planCtl) HIfaceQuery(p ifaceQueryPipe) { h.calls++; h.got = []any{p} }

func (h *planCtl) HUnexported(p unexportedFieldPipe) {
	h.calls++
}

func (h *planCtl) HUnresolved(p unresolvedFieldPipe) { h.calls++ }

func (h *planCtl) HOrder(a orderPipeA, b orderPipeB, c orderPipeC) {
	h.calls++
	planOrder = append(planOrder, "handler")
}

func (h *planCtl) HPanicMid(a orderPipeA, b panicPipe, c orderPipeC) {
	h.calls++
	planOrder = append(planOrder, "handler")
}

func (h *planCtl) HBodyThenPipe(b ctx.Body, a orderPipeA) {
	h.calls++
	planOrder = append(planOrder, "handler")
}

func (h *planCtl) HSlow(p slowPipe) { h.calls++ }

func (h *planCtl) HTwoSlow(p slowPipe, q slowPipeB) { h.calls++ }

func (h *planCtl) HMixed(c *ctx.HTTPContext, p recQueryPipe, r *http.Request) {
	h.calls++
	h.got = []any{c, p, r}
}

func (h *planCtl) HAll(c *ctx.HTTPContext, r *http.Request, w http.ResponseWriter, body ctx.Body, form ctx.Form, query ctx.Query, header ctx.Header, param ctx.Param, file ctx.File, next ctx.Next, redirect ctx.Redirect, pub common.Publisher, cp recCtxPipe, bp provBodyPipe, fp recFormPipe, qp recQueryPipe, hp recHeaderPipe, pp recParamPipe, filep recFilePipe) (int, string) {
	h.calls++
	planNextCalls = 0
	next()
	nextSeen := planNextCalls
	redirect("/x")
	h.got = []any{c, r, w, body, form, query, header, param, file, nextSeen, ctx.HTTPCode(c), pub, cp, bp, fp, qp, hp, pp, filep}
	return 202, "all"
}

type planValueCtl struct{ tag string }

func (h planValueCtl) HTag() string { return h.tag }

type planConcurrentCtl struct{}

func (planConcurrentCtl) Run(p mutPipe, q ctx.Query) string {
	return strconv.Itoa(p.Seen) + ":" + q.Get("q")
}

type planBadCtl struct{}

func (planBadCtl) Unknown(c *ctx.HTTPContext, n int) string { return "" }

func (planBadCtl) UnknownStruct(u planUnknownArg) {}

func (planBadCtl) Variadic(a ...int) {}

func (planBadCtl) Interface(e error) {}

func (planBadCtl) WSContext(c *ctx.WSContext) {}

func (planBadCtl) WSPayloadPipe(p recWSPayloadPipe) {}

func (planBadCtl) Lookalike(c *ctxlookalike.HTTPContext) string { return "" }

func (planBadCtl) NeedsPublisher(p common.Publisher) {}

func (planBadCtl) PointerPipe(p *recQueryPipe) {}

func (planBadCtl) CountedPipe(p countingPipe) {}

func (planBadCtl) Plain(c *ctx.HTTPContext, r *http.Request) string { return "" }

func planReferenceHTTP(t testing.TB, recv reflect.Value, name string, providers map[string]Provider, c *ctx.HTTPContext) []reflect.Value {
	t.Helper()
	handler := recv.MethodByName(name)
	ft := handler.Type()
	args := make([]reflect.Value, ft.NumIn())
	for i := range args {
		args[i] = planReferenceArg(t, ft.In(i), providers, c)
	}
	return handler.Call(args)
}

var (
	refHTTPContextT = reflect.TypeOf((*ctx.HTTPContext)(nil))
	refRequestT     = reflect.TypeOf((*http.Request)(nil))
	refWriterT      = reflect.TypeOf((*http.ResponseWriter)(nil)).Elem()
	refBodyT        = reflect.TypeOf(ctx.Body(nil))
	refFormT        = reflect.TypeOf(ctx.Form(nil))
	refQueryT       = reflect.TypeOf(ctx.Query(nil))
	refHeaderT      = reflect.TypeOf(ctx.Header(nil))
	refParamT       = reflect.TypeOf(ctx.Param(nil))
	refFileT        = reflect.TypeOf(ctx.File(nil))
	refNextT        = reflect.TypeOf(func() {})
	refRedirectT    = reflect.TypeOf(func(string) {})
	refPublisherT   = reflect.TypeOf((*common.Publisher)(nil)).Elem()
)

func planReferenceArg(t testing.TB, at reflect.Type, providers map[string]Provider, c *ctx.HTTPContext) reflect.Value {
	t.Helper()
	switch at {
	case refHTTPContextT:
		return reflect.ValueOf(c)
	case refRequestT:
		return reflect.ValueOf(c.Request)
	case refWriterT:
		return reflect.ValueOf(c.ResponseWriter)
	case refBodyT:
		return reflect.ValueOf(c.Body())
	case refFormT:
		return reflect.ValueOf(c.Form())
	case refQueryT:
		return reflect.ValueOf(c.Query())
	case refHeaderT:
		return reflect.ValueOf(c.Header())
	case refParamT:
		return reflect.ValueOf(c.Param())
	case refFileT:
		return reflect.ValueOf(c.File())
	case refNextT:
		return reflect.ValueOf(ctx.HTTPNext(c))
	case refRedirectT:
		return reflect.ValueOf(c.Redirect)
	case refPublisherT:
		p, _ := planCtr.interfaceFor(publisherKey)
		return reflect.ValueOf(p)
	}

	inst, err := injectDependencies(reflect.New(at).Elem().Interface(), "pipe", providers, planCtr)
	if err != nil {
		t.Fatal(err)
	}
	meta := func(kind string) common.ArgumentMetadata {
		return common.ArgumentMetadata{ContextType: common.ContextTypeHTTP, ParamType: kind}
	}
	var out any
	switch p := inst.Interface().(type) {
	case common.ContextPipeable:
		out = p.Transform(c, meta(common.ContextPipeableKey))
	case common.BodyPipeable:
		out = p.Transform(c.Body(), meta(common.BodyPipeableKey))
	case common.FormPipeable:
		out = p.Transform(c.Form(), meta(common.FormPipeableKey))
	case common.QueryPipeable:
		out = p.Transform(c.Query(), meta(common.QueryPipeableKey))
	case common.HeaderPipeable:
		out = p.Transform(c.Header(), meta(common.HeaderPipeableKey))
	case common.ParamPipeable:
		out = p.Transform(c.Param(), meta(common.ParamPipeableKey))
	case common.FilePipeable:
		out = p.Transform(c.File(), meta(common.FilePipeableKey))
	default:
		t.Fatalf("fixture: %v is not a pipe", at)
	}
	return reflect.ValueOf(out)
}

func planPipeKindCase(t *testing.T, method, kind string, src func(*ctx.HTTPContext) any, wantKind string) {
	t.Helper()
	planResetState(t)
	h := &planCtl{}
	c := planRequestContext()
	planInvoke(planHTTP(t, h, method, nil), c)
	if len(planPipeCalls) != 1 {
		t.Fatal(test.DiffMessage(len(planPipeCalls), 1, "B10: Transform runs once per request"))
	}
	call := planPipeCalls[0]
	if call.Kind != wantKind {
		t.Error(test.DiffMessage(call.Kind, wantKind, "B10: Transform of the "+kind+" kind"))
	}
	want := src(c)
	if call.Src == nil || (reflect.ValueOf(want).Kind() == reflect.Map && !planSameMap(call.Src, want)) ||
		(reflect.ValueOf(want).Kind() == reflect.Pointer && call.Src != want) {
		t.Error(test.DiffMessage(call.Src, want, "B10: the "+kind+" pipe's source"))
	}
	if call.Meta.ParamType != kind {
		t.Error(test.DiffMessage(call.Meta.ParamType, kind, "B10: ParamType is the kind's key"))
	}
	if call.Meta.ContextType != common.ContextTypeHTTP {
		t.Error(test.DiffMessage(call.Meta.ContextType, common.ContextTypeHTTP, "B10: ContextType of an HTTP pipe"))
	}
	if len(h.got) != 1 || reflect.ValueOf(h.got[0]).FieldByName("Kind").String() != wantKind {
		t.Error(test.DiffMessage(h.got, wantKind, "B10: the handler receives what Transform returned"))
	}
}

func planWantTransformFailure(t *testing.T, rec any, typeName, returned string) {
	t.Helper()
	ex, ok := planExceptionOf(rec)
	if !ok {
		t.Fatal(test.DiffMessage(rec, "an exception.Exception", "B11: a wrong Transform result panics with an exception"))
	}
	if ex.Code() != http.StatusInternalServerError {
		t.Error(test.DiffMessage(ex.Code(), http.StatusInternalServerError, "B11: code"))
	}
	want := "pipe " + typeName + ": Transform returned " + returned + ", want " + typeName
	if ex.Message() != want {
		t.Error(test.DiffMessage(ex.Message(), want, "B11: message"))
	}
}

type panicWSPipe struct{}

func (panicWSPipe) Transform(ctx.WSPayload, common.ArgumentMetadata) any {
	time.Sleep(time.Millisecond)
	panic("ws pipe exploded")
}

type planWSCtl struct {
	calls int
	got   []any
}

func (h *planWSCtl) WCtx(c *ctx.WSContext) { h.calls++; h.got = []any{c} }

func (h *planWSCtl) WConn(c *websocket.Conn) { h.calls++; h.got = []any{c} }

func (h *planWSCtl) WPayload(p ctx.WSPayload) { h.calls++; h.got = []any{p} }

func (h *planWSCtl) WNext(n ctx.Next) { h.calls++; n() }

func (h *planWSCtl) WPublisher(p common.Publisher) {
	h.calls++
	h.got = []any{p}
}

func (h *planWSCtl) WPipe(p recWSPayloadPipe) { h.calls++; h.got = []any{p} }

func (h *planWSCtl) WNilPipe(p nilWSPipe) { h.calls++; h.got = []any{p} }

func (h *planWSCtl) WPanicPipe(p panicWSPipe) { h.calls++ }

func (h *planWSCtl) WNone() { h.calls++ }

func (h *planWSCtl) WOne() string { h.calls++; return "one" }

func (h *planWSCtl) WAll(c *ctx.WSContext, conn *websocket.Conn, payload ctx.WSPayload, next ctx.Next, pub common.Publisher, p recWSPayloadPipe) (int, string) {
	h.calls++
	planNextCalls = 0
	next()
	h.got = []any{c, conn, payload, planNextCalls, pub, p}
	return 7, "ws"
}

type planWSBadCtl struct{}

func (planWSBadCtl) HTTPContext(c *ctx.HTTPContext) {}

func (planWSBadCtl) Body(b ctx.Body) {}

func (planWSBadCtl) Request(r ctx.Redirect) {}

func (planWSBadCtl) QueryPipe(p recQueryPipe) {}

func (planWSBadCtl) Unknown(c *ctx.WSContext, n int) {}

func planWSContext() *ctx.WSContext {
	c := ctx.NewWSContext()
	ctx.WSInit(c, &websocket.Conn{})
	ctx.WSSetPayload(c, ctx.WSPayload{"k": "v"})
	return c
}

func planWS(t testing.TB, controller any, name string, providers map[string]Provider) *wsHandlerPlan {
	t.Helper()
	recv, m := planMethod(t, controller, name)
	return newWSHandlerPlan(recv, m, providers, planCtr)
}

type mockProvider struct{}

func (m *mockProvider) NewProvider() Provider { return m }

type mockController struct{}

func (m *mockController) NewController() Controller { return m }

type fnTestProvider struct{ Tag string }

func (p fnTestProvider) NewProvider() Provider { return p }

type injectDepsTestProvider struct{}

func (injectDepsTestProvider) NewProvider() Provider { return injectDepsTestProvider{} }

type injectDepsComponent struct {
	Provider injectDepsTestProvider
	Plain    string
}

type injectDepsUnresolvable struct {
	Missing unresolvableProviderType
}

type unresolvableProviderType struct{}

func (unresolvableProviderType) NewProvider() Provider { return unresolvableProviderType{} }

type returnUser struct {
	Name string `json:"name"`
}

func anyValue(v any) reflect.Value {
	return reflect.ValueOf(&v).Elem()
}

func writeReturn(t *testing.T, c *ctx.HTTPContext, v reflect.Value) {
	t.Helper()
	defer func() {
		if rec := recover(); rec != nil {
			t.Error(test.DiffMessage(rec, "no panic", "returnHTTP must not panic on a result a handler can return"))
		}
	}()
	returnHTTP(c, v)
}

func recorderOf(c *ctx.HTTPContext) *httptest.ResponseRecorder {
	return c.ResponseWriter.(*httptest.ResponseRecorder)
}

func allocsOfServe(app *App, method, target string) float64 {
	r := httptest.NewRequest(method, target, nil)
	w := &benchDiscardWriter{header: http.Header{}}
	return testing.AllocsPerRun(200, func() { app.ServeHTTP(w, r) })
}

const (
	assetCSS = "body{color:#123}"
	assetJS  = "console.log(1)"
	assetSVG = `<svg xmlns="http://www.w3.org/2000/svg"></svg>`
)

type assetPages struct{ common.HTTP }

func (c assetPages) NewController() Controller { return c }

func (c assetPages) RENDER_list_OF_bookings() {}

func (c assetPages) RENDER_page() {}

type assetPlain struct{ common.HTTP }

func (c assetPlain) NewController() Controller { return c }

func (c assetPlain) RENDER_x() {}

func assetFiles() fstest.MapFS {
	return pgFiles(map[string]string{
		"layout.html":              `<html>{{template "content" .}}</html>`,
		"page.html":                `{{asset "css/app.css"}}`,
		"bookings/list.html":       `LIST-TEMPLATE`,
		"partials/field.html":      `FIELD-PARTIAL`,
		"assets/css/app.css":       assetCSS,
		"assets/js/app.js":         assetJS,
		"assets/img/logo.svg":      assetSVG,
		"assets/.env":              `SECRET=1`,
		"assets/.hidden/x.txt":     `HIDDEN`,
		"assets/.well-known/t.txt": `WELL-KNOWN`,
		"secret.txt":               `OUTSIDE`,
	})
}

func assetHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:8])
}

func assetApp(t *testing.T, files fstest.MapFS, setup func(*App)) *App {
	t.Helper()
	app := New()
	app.SetLogger(&lcRecordingLogger{})
	app.EnableRender(&RenderOptions{FS: files, Dir: "."})
	if setup != nil {
		setup(app)
	}
	mustCreate(t, app, NewModuleBuilder().Controllers(assetPages{}).Build())
	return app
}

type assetUserServe struct{ common.HTTP }

func (c assetUserServe) NewController() Controller { return c }

func (c assetUserServe) SERVE_ANY_OF_assets() string { return "." }

type assetMarkMiddleware struct{}

func (assetMarkMiddleware) Use(_ *http.Request, w http.ResponseWriter, next ctx.Next) {
	w.Header().Set("X-Global-Middleware", "ran")
	next()
}

type assetDenyGuard struct{}

func (assetDenyGuard) CanActivate(c *ctx.HTTPContext) bool {
	return c.Request.Header.Get("X-Allow") == "yes"
}

type benchPlainController struct {
	common.HTTP
}

func (c benchPlainController) NewController() Controller { return c }

func (c benchPlainController) READ_bench() string { return "ok" }

func (c benchPlainController) CREATE_bench() string { return "ok" }

func (c benchPlainController) READ_benchexception() string {
	panic(exception.BadRequestException("bad input"))
}

func (c benchPlainController) READ_benchpanic() string { panic("boom") }

type benchDiscardWriter struct {
	header http.Header
}

func (w *benchDiscardWriter) Header() http.Header { return w.header }

func (w *benchDiscardWriter) WriteHeader(int) {}

func (w *benchDiscardWriter) Write(p []byte) (int, error) { return len(p), nil }

func benchHTTPContext(urlPath string) *ctx.HTTPContext {
	c := ctx.NewHTTPContext()
	c.Request = httptest.NewRequest(http.MethodGet, urlPath, nil)
	c.ResponseWriter = httptest.NewRecorder()
	return c
}

func benchServe(b *testing.B, app *App, method, target string) {
	b.Helper()
	r := httptest.NewRequest(method, target, nil)
	w := &benchDiscardWriter{header: http.Header{}}

	b.ReportAllocs()
	for b.Loop() {
		app.ServeHTTP(w, r)
	}
}

func benchApp(controllers ...Controller) *App {
	app := New()
	app.SetLogger(&mockLogger{})
	app.Create(NewModuleBuilder().Controllers(controllers...).Build())
	return app
}

type bodyProbeController struct {
	common.HTTP
}

func (ctl bodyProbeController) NewController() Controller { return ctl }

func (ctl bodyProbeController) READ_probe(c *ctx.HTTPContext) string {
	switch c.Request.Body {
	case nil:
		return "nil"
	case http.NoBody:
		return "nobody"
	}
	return "wrapped"
}

func bodyProbe(t *testing.T, mutate func(*http.Request)) string {
	t.Helper()
	app := New()
	app.Create(NewModuleBuilder().Controllers(bodyProbeController{}).Build())

	r := httptest.NewRequest(http.MethodGet, "/probe", nil)
	if mutate != nil {
		mutate(r)
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w.Body.String()
}

type clientIPEvents struct {
	mu     sync.Mutex
	events []trace.Event
}

func (c *clientIPEvents) listen(e trace.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *clientIPEvents) all() []trace.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]trace.Event(nil), c.events...)
}

func (c *clientIPEvents) complete(t *testing.T) trace.Event {
	t.Helper()
	for _, e := range c.all() {
		if e.Stage == trace.StageComplete {
			return e
		}
	}
	t.Fatal(test.DiffMessage(c.all(), "a complete event", "fixture: the request emitted a complete event"))
	return trace.Event{}
}

func clientIPApp(t *testing.T, events *clientIPEvents, withProxy bool) *App {
	t.Helper()
	return lcNewApp(t, lcConfig{setup: func(app *App) {
		app.tracer.Listen(events.listen)
		if withProxy {
			app.BindGlobalMiddlewares(proxy.Proxy{TrustedProxies: []string{"10.0.0.0/8"}})
		}
	}})
}

func clientIPServe(app *App, remote string, headers ...string) {
	r := httptest.NewRequest(http.MethodGet, "/ok", nil)
	r.RemoteAddr = remote
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	app.ServeHTTP(httptest.NewRecorder(), r)
}

type ccMiddleware struct{}

func (ccMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	if r.Header.Get("X-Stop") != "" {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	next()
}

type ccGuard struct{}

func (ccGuard) CanActivate(c *ctx.HTTPContext) bool { return c.Header().Get("X-Deny") == "" }

type ccInterceptor struct{}

func (ccInterceptor) Intercept(_ *ctx.HTTPContext, agg *aggregation.Aggregation) any {
	return agg.Pipe(agg.Transform(func(data any) any { return fmt.Sprint(data) + ">t" }))
}

type ccFilter struct{}

func (ccFilter) Catch(c *ctx.HTTPContext, ex *exception.Exception) {
	c.Status(ex.Code()).Text("cc:" + ex.Message())
}

type ccController struct {
	common.HTTP
	common.Middleware
	common.Guard
	common.Interceptor
	common.ExceptionFilter
}

func (ctl ccController) NewController() Controller {
	ctl.BindMiddleware(ccMiddleware{})
	ctl.BindGuard(ccGuard{})
	ctl.BindInterceptor(ccInterceptor{})
	ctl.BindExceptionFilter(ccFilter{})
	return ctl
}

func (ctl ccController) READ_ok() string { return "ok" }

func (ctl ccController) CREATE_ok() string { return "created" }

func (ctl ccController) READ_panic() string { panic("secret") }

func (ctl ccController) READ_exception() string { panic(exception.BadRequestException("bad")) }

type ccCase struct {
	method  string
	target  string
	headers [2]string
	code    int
	body    string
}

type epPages struct{ common.HTTP }

func (c epPages) NewController() Controller { return c }

func (c epPages) RENDER_fail(h *ctx.HTTPContext) {
	code, _ := strconv.Atoi(h.Query().Get("code"))
	if code == http.StatusNotFound {
		panic(exception.NotFoundException("it failed"))
	}
	panic(exception.NewException("it failed", code))
}

type epGuard struct{}

func (epGuard) CanActivate(*ctx.HTTPContext) bool {
	panic(exception.NewException("guard says no", http.StatusUnprocessableEntity))
}

type epGuarded struct {
	common.HTTP
	common.Guard
}

func (c epGuarded) NewController() Controller {
	c.BindGuard(epGuard{})
	return c
}

func (c epGuarded) RENDER_guarded() {}

func epFiles(names ...string) map[string]string {
	files := map[string]string{"fail.html": "x", "guarded.html": "x"}
	for _, name := range names {
		files[name+".html"] = name + "-file:{{.Code}}"
	}
	return files
}

func epBody(code int, template string) string {
	return template + "-file:" + strconv.Itoa(code)
}

type epAdmin struct{ common.HTTP }

func (c epAdmin) NewController() Controller {
	c.Prefix("admin")
	return c
}

func (c epAdmin) RENDER_page(h *ctx.HTTPContext) {
	if h.Query().Get("fail") != "" {
		panic(exception.NotFoundException("missing"))
	}
}

type lcLogEntry struct {
	level string
	msg   string
	args  []any
}

func (e lcLogEntry) field(key string) any {
	for i := 0; i+1 < len(e.args); i += 2 {
		if k, ok := e.args[i].(string); ok && k == key {
			return e.args[i+1]
		}
	}
	return nil
}

type lcRecordingLogger struct {
	mu      sync.Mutex
	entries []lcLogEntry
}

func (l *lcRecordingLogger) record(level, msg string, args []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, lcLogEntry{level: level, msg: msg, args: append([]any(nil), args...)})
}

func (l *lcRecordingLogger) named(msg string) []lcLogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []lcLogEntry
	for _, e := range l.entries {
		if e.msg == msg {
			out = append(out, e)
		}
	}
	return out
}

func (l *lcRecordingLogger) Debug(msg string, args ...any) { l.record("debug", msg, args) }

func (l *lcRecordingLogger) Info(msg string, args ...any) { l.record("info", msg, args) }

func (l *lcRecordingLogger) Warn(msg string, args ...any) { l.record("warn", msg, args) }

func (l *lcRecordingLogger) Error(msg string, args ...any) { l.record("error", msg, args) }

func (l *lcRecordingLogger) Fatal(msg string, args ...any) { l.record("fatal", msg, args) }

func lcText(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	}
	return fmt.Sprint(v)
}

func lcRuntimeErrorPanic(*ctx.HTTPContext) any {
	items := []int{}
	i := len(os.Args) + 10
	return items[i]
}

func lcRuntimeErrorMiddleware(http.ResponseWriter, *http.Request, ctx.Next) {
	_ = lcRuntimeErrorPanic(nil)
}

func lcNilPanic(*ctx.HTTPContext) any {
	panic(new(runtime.PanicNilError))
}

func lcNilPanicMiddleware(http.ResponseWriter, *http.Request, ctx.Next) {
	panic(new(runtime.PanicNilError))
}

func lcErrorResponses(t *testing.T, value any) (fromMiddleware, fromHandler *httptest.ResponseRecorder) {
	t.Helper()
	app := lcNewApp(t, lcConfig{noGlobalFilters: true})
	lcSetMiddleware("g1", lcPanicMiddleware(value))
	fromMiddleware = lcServe(app, http.MethodGet, "/bare")

	app = lcNewApp(t, lcConfig{noGlobalFilters: true})
	lcSetHandler(lcPanicWith(value))
	fromHandler = lcServe(app, http.MethodGet, "/bare")
	return fromMiddleware, fromHandler
}

func lcExpectBothAnswer(t *testing.T, value any, code int, body, desc string) {
	t.Helper()
	fromMiddleware, fromHandler := lcErrorResponses(t, value)
	lcExpectResponse(t, fromMiddleware, code, body, desc+" (HTTP layer)")
	lcExpectResponse(t, fromHandler, code, body, desc+" (built-in filter)")
}

func lcDirtyHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Length", "5")
	h.Set("Content-Encoding", "gzip")
	h.Set("ETag", `"abc"`)
	h.Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
	h.Set("X-Custom", "kept")
	h.Set("Access-Control-Allow-Origin", "*")
}

func lcExpectEntityHeadersRemoved(t *testing.T, w *httptest.ResponseRecorder, desc string) {
	t.Helper()
	for _, name := range []string{"Content-Length", "Content-Encoding", "ETag", "Last-Modified"} {
		if got := w.Header().Get(name); got != "" {
			t.Error(test.DiffMessage(got, "", "E1: "+name+" set earlier in the request is removed, "+desc))
		}
	}
	for name, want := range map[string]string{"X-Custom": "kept", "Access-Control-Allow-Origin": "*"} {
		if got := w.Header().Get(name); got != want {
			t.Error(test.DiffMessage(got, want, "E1: "+name+" is kept, "+desc))
		}
	}
	if w.Header().Get("X-Request-Id") == "" {
		t.Error(test.DiffMessage("", "<request id>", "E1: X-Request-Id is kept, "+desc))
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Error(test.DiffMessage(got, "application/json", "E1: Content-Type is JSON, "+desc))
	}
}

type lcStrictWriter struct {
	header http.Header
	status int
	infos  []int
	body   bytes.Buffer
}

func (w *lcStrictWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *lcStrictWriter) WriteHeader(code int) {
	if code < 100 || code > 999 {
		panic("invalid WriteHeader code " + strconv.Itoa(code))
	}
	if code >= 100 && code <= 199 {
		w.infos = append(w.infos, code)
		return
	}
	if w.status == 0 {
		w.status = code
	}
}

func (w *lcStrictWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}

func lcTraceApp(t *testing.T, events *[]trace.Event, mu *sync.Mutex) *App {
	t.Helper()
	return lcNewApp(t, lcConfig{setup: func(app *App) {
		app.tracer.Listen(func(e trace.Event) {
			mu.Lock()
			defer mu.Unlock()
			*events = append(*events, e)
		})
	}})
}

func lcTraceStage(events []trace.Event, stage string) []trace.Event {
	var out []trace.Event
	for _, e := range events {
		if e.Stage == stage {
			out = append(out, e)
		}
	}
	return out
}

func lcDeepPanic(depth int) any {
	if depth == 0 {
		panic("deep secret")
	}
	return lcDeepPanic(depth-1) != nil
}

type hookFunc func(time.Duration)

func (f hookFunc) Done(elapsed time.Duration) { f(elapsed) }

type spyWriter struct {
	header   http.Header
	codes    []int
	written  []byte
	flushed  int
	readFrom int
	hijacked int
}

func newSpyWriter() *spyWriter { return &spyWriter{header: http.Header{}} }

func (s *spyWriter) Header() http.Header { return s.header }

func (s *spyWriter) WriteHeader(code int) {
	s.codes = append(s.codes, code)
}

func (s *spyWriter) Write(p []byte) (int, error) {
	s.written = append(s.written, p...)
	return len(p), nil
}

func (s *spyWriter) Flush() { s.flushed++ }

func (s *spyWriter) ReadFrom(r io.Reader) (int64, error) {
	s.readFrom++
	return io.Copy(io.Discard, r)
}

func (s *spyWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	s.hijacked++
	return nil, nil, nil
}

type tagWriter struct{ http.ResponseWriter }

func (w tagWriter) Write(p []byte) (int, error) {
	w.Header().Set("X-Wrapped", "1")
	return w.ResponseWriter.Write(p)
}

func hkRegister(name string, onDone func(time.Duration)) {
	lcSetMiddleware(name, func(w http.ResponseWriter, r *http.Request, next ctx.Next) {
		hw := httphook.Of(w)
		if hw == nil {
			lcLog("nohook:" + name)
			next()
			return
		}
		hw.OnDone(hookFunc(onDone))
		next()
	})
}

func hkServe(app *App, rec *httptest.ResponseRecorder, method, target string) {
	app.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
}

type hookProbeMiddleware struct{}

func (hookProbeMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	_ = httphook.Of(w)
	next()
}

type passMiddleware struct{}

func (passMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) { next() }

type lcCaught struct {
	name string
	ex   exception.Exception
}

type lcFixture struct {
	mu         sync.Mutex
	log        []string
	caught     []lcCaught
	middleware map[string]func(http.ResponseWriter, *http.Request, ctx.Next)
	guard      map[string]func(*ctx.HTTPContext) bool
	intercept  map[string]func(*ctx.HTTPContext, *aggregation.Aggregation) any
	filter     map[string]func(*ctx.HTTPContext, *exception.Exception)
	handler    func(*ctx.HTTPContext) any
}

var lc lcFixture

func lcLog(entry string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.log = append(lc.log, entry)
}

func lcEntries(prefix string) []string {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	var out []string
	for _, entry := range lc.log {
		if strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return out
}

func lcAll() []string {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return slices.Clone(lc.log)
}

func lcClearLog() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.log = nil
}

func lcCaughtBy(name string) (exception.Exception, bool) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	for _, c := range lc.caught {
		if c.name == name {
			return c.ex, true
		}
	}
	return exception.Exception{}, false
}

func lcHook[T any](m map[string]T, name string) (T, bool) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	h, ok := m[name]
	return h, ok
}

func lcSetMiddleware(name string, h func(http.ResponseWriter, *http.Request, ctx.Next)) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.middleware[name] = h
}

func lcSetGuard(name string, h func(*ctx.HTTPContext) bool) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.guard[name] = h
}

func lcSetIntercept(name string, h func(*ctx.HTTPContext, *aggregation.Aggregation) any) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.intercept[name] = h
}

func lcSetFilter(name string, h func(*ctx.HTTPContext, *exception.Exception)) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.filter[name] = h
}

func lcSetHandler(h func(*ctx.HTTPContext) any) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.handler = h
}

func lcReset(t *testing.T) {
	t.Helper()
	lc.mu.Lock()
	lc.log = nil
	lc.caught = nil
	lc.middleware = map[string]func(http.ResponseWriter, *http.Request, ctx.Next){}
	lc.guard = map[string]func(*ctx.HTTPContext) bool{}
	lc.intercept = map[string]func(*ctx.HTTPContext, *aggregation.Aggregation) any{}
	lc.filter = map[string]func(*ctx.HTTPContext, *exception.Exception){}
	lc.handler = nil
	lc.mu.Unlock()
}

type lcMiddleware struct{ Name string }

func (m lcMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	lcLog("mw:" + m.Name)
	if h, ok := lcHook(lc.middleware, m.Name); ok {
		h(w, r, next)
		return
	}
	next()
}

type lcGuard struct{ Name string }

func (g lcGuard) CanActivate(c *ctx.HTTPContext) bool {
	lcLog("guard:" + g.Name)
	if h, ok := lcHook(lc.guard, g.Name); ok {
		return h(c)
	}
	return true
}

type lcInterceptor struct{ Name string }

func (i lcInterceptor) Intercept(c *ctx.HTTPContext, agg *aggregation.Aggregation) any {
	lcLog("intercept:" + i.Name)
	if h, ok := lcHook(lc.intercept, i.Name); ok {
		return h(c, agg)
	}
	return agg.Pipe(agg.Transform(func(data any) any {
		lcLog("post:" + i.Name)
		return fmt.Sprint(data) + ">" + i.Name
	}))
}

type lcFilter struct{ Name string }

func (f lcFilter) Catch(c *ctx.HTTPContext, ex *exception.Exception) {
	lcLog("filter:" + f.Name)
	lc.mu.Lock()
	lc.caught = append(lc.caught, lcCaught{name: f.Name, ex: *ex})
	lc.mu.Unlock()
	if h, ok := lcHook(lc.filter, f.Name); ok {
		h(c, ex)
		return
	}
	c.Status(ex.Code()).Text("F[" + f.Name + "]" + ex.Message())
}

func lcHandle(c *ctx.HTTPContext) any {
	lcLog("handler")
	lc.mu.Lock()
	h := lc.handler
	lc.mu.Unlock()
	if h != nil {
		return h(c)
	}
	return "H"
}

type lcPanicPipe struct{}

func (lcPanicPipe) Transform(ctx.Query, common.ArgumentMetadata) any {
	panic(exception.BadRequestException("pipe boom"))
}

type lcController struct {
	common.HTTP
	common.Middleware
	common.Guard
	common.Interceptor
	common.ExceptionFilter
}

func (ctl lcController) NewController() Controller {
	ctl.BindMiddleware(lcMiddleware{Name: "m1"})
	ctl.BindMiddleware(lcMiddleware{Name: "m2"})
	ctl.BindGuard(lcGuard{Name: "m1"})
	ctl.BindGuard(lcGuard{Name: "m2"})
	ctl.BindInterceptor(lcInterceptor{Name: "m1"})
	ctl.BindInterceptor(lcInterceptor{Name: "m2"})
	ctl.BindExceptionFilter(lcFilter{Name: "mf1"})
	ctl.BindExceptionFilter(lcFilter{Name: "mf2"})
	return ctl
}

func (ctl lcController) READ_ok(c *ctx.HTTPContext) any { return lcHandle(c) }

func (ctl lcController) CREATE_ok(c *ctx.HTTPContext) any { return lcHandle(c) }

func (ctl lcController) READ_pipe(lcPanicPipe) string { return "unreachable" }

func (ctl lcController) CREATE_bind(b ctx.Body) any {
	_ = b
	lcLog("handler")
	return "bound"
}

type lcBareController struct {
	common.HTTP
}

func (ctl lcBareController) NewController() Controller { return ctl }

func (ctl lcBareController) READ_bare(c *ctx.HTTPContext) any { return lcHandle(c) }

func (ctl lcBareController) CREATE_bare(c *ctx.HTTPContext) any { return lcHandle(c) }

func (ctl lcBareController) DELETE_bare(c *ctx.HTTPContext) any { return lcHandle(c) }

func (ctl lcBareController) UPDATE_bare(c *ctx.HTTPContext) any { return lcHandle(c) }

func (ctl lcBareController) MODIFY_bare(c *ctx.HTTPContext) any { return lcHandle(c) }

func (ctl lcBareController) PREFLIGHT_bare(c *ctx.HTTPContext) any { return lcHandle(c) }

type lcMethodController struct {
	common.HTTP
}

func (ctl lcMethodController) NewController() Controller { return ctl }

func (ctl lcMethodController) READ_readonly(c *ctx.HTTPContext) any { return lcHandle(c) }

func (ctl lcMethodController) CREATE_postonly(c *ctx.HTTPContext) any {
	return lcHandle(c)
}

type lcConfig struct {
	noGlobalFilters bool
	logger          log.LogAdapter
	setup           func(*App)
}

func lcNewApp(t *testing.T, cfg lcConfig) *App {
	t.Helper()
	lcReset(t)
	app := New()
	if cfg.logger != nil {
		app.SetLogger(cfg.logger)
	}
	if cfg.setup != nil {
		cfg.setup(app)
	}
	app.BindGlobalMiddlewares(lcMiddleware{Name: "g1"}, lcMiddleware{Name: "g2"})
	app.BindGlobalGuards(lcGuard{Name: "g1"}, lcGuard{Name: "g2"})
	app.BindGlobalInterceptors(lcInterceptor{Name: "g1"}, lcInterceptor{Name: "g2"})
	if !cfg.noGlobalFilters {
		app.BindGlobalExceptionFilters(lcFilter{Name: "gf1"}, lcFilter{Name: "gf2"})
	}
	app.Create(NewModuleBuilder().Controllers(lcController{}, lcBareController{}, lcMethodController{}).Build())
	return app
}

func lcApp(t *testing.T) *App {
	t.Helper()
	return lcNewApp(t, lcConfig{})
}

func lcServe(app *App, method, target string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

func lcExpectEntries(t *testing.T, got, want []string, desc string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, desc))
	}
}

func lcExpectResponse(t *testing.T, w *httptest.ResponseRecorder, code int, body, desc string) {
	t.Helper()
	if w.Code != code {
		t.Error(test.DiffMessage(w.Code, code, desc+": status"))
	}
	if w.Body.String() != body {
		t.Error(test.DiffMessage(w.Body.String(), body, desc+": body"))
	}
}

func lcBefore(t *testing.T, earlierPrefix, laterPrefix, desc string) {
	t.Helper()
	all := lcAll()
	last, first := -1, -1
	for i, entry := range all {
		if strings.HasPrefix(entry, earlierPrefix) {
			last = i
		}
		if first == -1 && strings.HasPrefix(entry, laterPrefix) {
			first = i
		}
	}
	if last == -1 || first == -1 {
		t.Error(test.DiffMessage(all, "entries with both "+earlierPrefix+" and "+laterPrefix, desc))
		return
	}
	if last >= first {
		t.Error(test.DiffMessage(all, earlierPrefix+" entries all before "+laterPrefix+" entries", desc))
	}
}

func lcPanicWith(value any) func(*ctx.HTTPContext) any {
	return func(*ctx.HTTPContext) any { panic(value) }
}

func lcRethrow(_ *ctx.HTTPContext, ex *exception.Exception) { panic(ex) }

func lcFilterPanicWith(value any) func(*ctx.HTTPContext, *exception.Exception) {
	return func(*ctx.HTTPContext, *exception.Exception) { panic(value) }
}

func lcPanicMiddleware(value any) func(http.ResponseWriter, *http.Request, ctx.Next) {
	return func(http.ResponseWriter, *http.Request, ctx.Next) { panic(value) }
}

func lcDialWS(t *testing.T, server *httptest.Server) (*websocket.Conn, error) {
	t.Helper()
	return websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", server.URL)
}

type multipartController struct {
	common.HTTP
}

func (c multipartController) NewController() Controller { return c }

func (c multipartController) CREATE_ok(h *ctx.HTTPContext) string {
	h.Form()
	return "ok"
}

func (c multipartController) CREATE_boom(h *ctx.HTTPContext) string {
	h.Form()
	panic("boom after parsing the form")
}

func (c multipartController) CREATE_abort(h *ctx.HTTPContext) string {
	h.Form()
	panic(http.ErrAbortHandler)
}

type quietLogger struct{}

func (quietLogger) Debug(string, ...any) {}

func (quietLogger) Info(string, ...any) {}

func (quietLogger) Warn(string, ...any) {}

func (quietLogger) Error(string, ...any) {}

func (quietLogger) Fatal(string, ...any) {}

func largeMultipart(t *testing.T) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", "big.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(part, zeroReader{}, 33<<20); err != nil {
		t.Fatal(err)
	}
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func multipartTempFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

// serveRecovering serves r and swallows http.ErrAbortHandler, which core panics again for
// net/http to abort the connection.
func serveRecovering(app *App, w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil && rec != http.ErrAbortHandler {
			panic(rec)
		}
	}()
	app.ServeHTTP(w, r)
}

type paBookings struct{ common.HTTP }

func (c paBookings) NewController() Controller { return c }

func (c paBookings) RENDER_new_OF_bookings() {}

func (c paBookings) RENDER_bookings_BY_id(ctx.Param) {}

const (
	paLayoutCSS = "l{color:#111}"
	paNewCSS    = "n{color:#222}"
	paNewJS     = "console.log('new')"
	paIDCSS     = "i{color:#333}"
)

func paFiles() map[string]string {
	return map[string]string{
		"layout.html":          `<head>{{pageAssets}}</head>{{template "content" .}}`,
		"layout.css":           paLayoutCSS,
		"bookings/new.html":    `NEW`,
		"bookings/new.css":     paNewCSS,
		"bookings/new.form.js": paNewJS,
		"bookings/{id}.html":   `ONE`,
		"bookings/{id}.css":    paIDCSS,
		"partials/field.html":  `FIELD`,
		"secret.txt":           `OUTSIDE`,
	}
}

func paApp(t *testing.T, files map[string]string, setup func(*App), controllers ...Controller) *App {
	t.Helper()
	if len(controllers) == 0 {
		controllers = []Controller{paBookings{}}
	}
	app := New()
	app.SetLogger(&lcRecordingLogger{})
	app.EnableRender(&RenderOptions{FS: pgFiles(files), Dir: "."})
	if setup != nil {
		setup(app)
	}
	mustCreate(t, app, NewModuleBuilder().Controllers(controllers...).Build())
	return app
}

const (
	peFixed       = "Internal Server Error"
	peFixedJSON   = `{"code":500,"error":"Internal Server Error","message":"Internal Server Error"}`
	peTextBody    = "{{.Code}}|{{.Status}}|{{.Message}}"
	peAcceptHTML  = "text/html"
	peAcceptJSON  = "application/json"
	peSecretWords = "hunter2"
)

func peBoom(kind string) {
	switch kind {
	case "notfound":
		panic(exception.NotFoundException("no such thing"))
	case "description":
		panic(exception.NotFoundException("no such thing", exception.ExceptionOptions{Description: "gone away"}))
	case "emptymessage":
		panic(exception.NotFoundException(""))
	case "cause":
		panic(exception.BadRequestException("bad input", errors.New("sql: password=hunter2")))
	case "causeoptions":
		panic(exception.BadRequestException("bad input", exception.ExceptionOptions{Description: "mine", Cause: errors.New("sql: password=hunter2")}))
	case "wrapped":
		panic(fmt.Errorf("service: %w", exception.ConflictException("taken", errors.New("pq: duplicate key hunter2"))))
	case "internal":
		panic(exception.InternalServerErrorException("db down: password=hunter2"))
	case "internalcause":
		panic(exception.InternalServerErrorException("db down", errors.New("dial tcp 10.0.0.5: refused hunter2")))
	case "fixedinternal":
		panic(exception.InternalServerErrorException("Internal Server Error"))
	case "outside":
		panic(exception.NewException("odd hunter2", 700))
	case "string":
		panic("secret string hunter2")
	case "error":
		panic(errors.New("secret error hunter2"))
	case "hostile":
		panic(exception.BadRequestException(`<b>"x"&</b>`))
	}
}

type pePage struct{ common.HTTP }

func (c pePage) NewController() Controller {
	c.Prefix("page")
	return c
}

func (c pePage) RENDER_boom(h *ctx.HTTPContext) ctx.Map {
	k := h.Query().Get("k")
	peBoom(k)
	return ctx.Map{"K": k}
}

func (c pePage) SUBMIT_boom(h *ctx.HTTPContext) { h.Redirect("/page/ok") }

func (c pePage) RENDER_ok() {}

type peAPI struct{ common.HTTP }

func (c peAPI) NewController() Controller {
	c.Prefix("api")
	return c
}

func (c peAPI) READ_boom(h *ctx.HTTPContext) string {
	peBoom(h.Query().Get("k"))
	return "ok"
}

func (c peAPI) CREATE_boom() string { return "ok" }

func (c peAPI) READ_ok() string { return "ok" }

type peGuard struct{}

func (peGuard) CanActivate(c *ctx.HTTPContext) bool {
	switch c.Request.Header.Get("X-Guard") {
	case "false":
		return false
	case "panic":
		panic(exception.ForbiddenException("guard refuses"))
	}
	return true
}

type peInterceptor struct{}

func (peInterceptor) Intercept(c *ctx.HTTPContext, agg *aggregation.Aggregation) any {
	if c.Request.Header.Get("X-Intercept") == "panic" {
		panic(exception.ConflictException("interceptor refuses"))
	}
	return agg.Pipe()
}

type peFilter struct{}

func (peFilter) Catch(c *ctx.HTTPContext, e *exception.Exception) {
	switch c.Request.Header.Get("X-Filter") {
	case "refilter":
		panic(exception.GoneException("filter refuses"))
	case "handle":
		c.Status(http.StatusTeapot).Text("handled by the user filter")
		return
	}
	panic(e)
}

type peDTO struct{}

func (d peDTO) Transform(q ctx.Query, _ common.ArgumentMetadata) any {
	if q.Get("pipe") == "boom" {
		panic(exception.BadRequestException("pipe refuses"))
	}
	return d
}

type peLayeredPage struct {
	common.HTTP
	common.Guard
	common.Interceptor
	common.ExceptionFilter
}

func (c peLayeredPage) NewController() Controller {
	c.Prefix("lp")
	c.BindGuard(peGuard{})
	c.BindInterceptor(peInterceptor{})
	c.BindExceptionFilter(peFilter{})
	return c
}

func (c peLayeredPage) RENDER_run(_ peDTO, h *ctx.HTTPContext) {
	if h.Query().Get("h") == "boom" {
		panic(exception.NotFoundException("handler refuses"))
	}
}

type peLayeredAPI struct {
	common.HTTP
	common.Guard
	common.Interceptor
	common.ExceptionFilter
}

func (c peLayeredAPI) NewController() Controller {
	c.Prefix("la")
	c.BindGuard(peGuard{})
	c.BindInterceptor(peInterceptor{})
	c.BindExceptionFilter(peFilter{})
	return c
}

func (c peLayeredAPI) READ_run(_ peDTO, h *ctx.HTTPContext) string {
	if h.Query().Get("h") == "boom" {
		panic(exception.NotFoundException("handler refuses"))
	}
	return "ok"
}

type peMiddleware struct{}

func (peMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	switch r.Header.Get("X-Mw") {
	case "panic":
		panic(exception.ServiceUnavailableException("mw refuses"))
	case "retry":
		w.Header().Set("Retry-After", "7")
		panic(exception.ServiceUnavailableException("mw busy"))
	case "dirty":
		h := w.Header()
		h.Set("Content-Length", "5")
		h.Set("Content-Encoding", "gzip")
		h.Set("ETag", `"abc"`)
		h.Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		h.Set("X-Custom", "kept")
		panic(exception.BadRequestException("dirty refuses"))
	}
	next()
}

func peDo(app *App, method, target string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i] == "Host" {
			r.Host = headers[i+1]
			continue
		}
		r.Header.Add(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

func peApp(t *testing.T, files map[string]string, setup func(*App), controllers ...Controller) (*App, *lcRecordingLogger) {
	t.Helper()
	logger := &lcRecordingLogger{}
	app := New()
	app.SetLogger(logger)
	if files != nil {
		app.EnableRender(&RenderOptions{FS: pgFiles(files), Dir: "."})
	}
	if setup != nil {
		setup(app)
	}
	mustCreate(t, app, NewModuleBuilder().Controllers(controllers...).Build())
	return app, logger
}

func peIsHTML(w *httptest.ResponseRecorder) bool {
	return strings.HasPrefix(w.Header().Get("Content-Type"), "text/html")
}

func peIsJSON(w *httptest.ResponseRecorder) bool {
	return w.Header().Get("Content-Type") == "application/json" && strings.HasPrefix(w.Body.String(), "{")
}

func peExpectHTML(t *testing.T, rule string, w *httptest.ResponseRecorder, code int) {
	t.Helper()
	if w.Code != code {
		t.Error(test.DiffMessage(w.Code, code, rule+": status"))
	}
	if !peIsHTML(w) {
		t.Error(test.DiffMessage(w.Header().Get("Content-Type"), "text/html; charset=utf-8", rule+": the error is HTML"))
	}
	if strings.HasPrefix(w.Body.String(), "{") {
		t.Error(test.DiffMessage(w.Body.String(), "an HTML page", rule+": no JSON body"))
	}
}

func peExpectJSON(t *testing.T, rule string, w *httptest.ResponseRecorder, code int) {
	t.Helper()
	if w.Code != code {
		t.Error(test.DiffMessage(w.Code, code, rule+": status"))
	}
	if !peIsJSON(w) {
		t.Error(test.DiffMessage(w.Header().Get("Content-Type")+" "+w.Body.String(), "application/json with a JSON body", rule+": the error is JSON"))
	}
}

func peLayerFiles(groups ...string) map[string]string {
	files := map[string]string{}
	for _, group := range groups {
		switch group {
		case "page":
			files["page/ok.html"] = "OK"
			files["page/boom.html"] = `{{if eq .K "render"}}{{index .K 99}}{{end}}OK`
		case "lp":
			files["lp/run.html"] = "OK"
		}
	}
	return files
}

func peFilesWith(extra map[string]string, groups ...string) map[string]string {
	files := peLayerFiles(groups...)
	for name, content := range extra {
		files[name] = content
	}
	return files
}

var (
	peBlockStarted chan struct{}
	peBlockRelease chan struct{}
)

type peBlocking struct{ common.HTTP }

func (c peBlocking) NewController() Controller {
	c.Prefix("block")
	return c
}

func (c peBlocking) READ_hold() string {
	select {
	case <-peBlockStarted:
	default:
		close(peBlockStarted)
	}
	<-peBlockRelease
	return "held"
}

type peCase struct {
	kind    string
	code    int
	status  string
	message string
	logMsg  string
	level   string
}

func peCases() []peCase {
	return []peCase{
		{"notfound", 404, "Not Found", "no such thing", "", ""},
		{"description", 404, "gone away", "no such thing", "", ""},
		{"emptymessage", 404, "Not Found", "Unhandled exception has occurred", "", ""},
		{"cause", 400, "Bad Request", "bad input", "ExceptionCause", "debug"},
		{"causeoptions", 400, "Bad Request", "bad input", "ExceptionCause", "debug"},
		{"wrapped", 409, "Conflict", "taken", "ExceptionCause", "debug"},
		{"internal", 500, peFixed, peFixed, "InternalServerError", "error"},
		{"internalcause", 500, peFixed, peFixed, "InternalServerError", "error"},
		{"fixedinternal", 500, peFixed, peFixed, "", ""},
		{"outside", 500, peFixed, peFixed, "InternalServerError", "error"},
		{"string", 500, peFixed, peFixed, "UnhandledPanic", "error"},
		{"error", 500, peFixed, peFixed, "UnhandledPanic", "error"},
	}
}

func peLogCount(l *lcRecordingLogger, msg string) int { return len(l.named(msg)) }

type peBodyAPI struct{ common.HTTP }

func (c peBodyAPI) NewController() Controller {
	c.Prefix("in")
	return c
}

func (c peBodyAPI) CREATE_json(b ctx.Body) any { return ctx.Map{"got": b["k"]} }

func (c peBodyAPI) CREATE_form(f ctx.Form) any { return ctx.Map{"got": f.Get("k")} }

type pePageIn struct{ common.HTTP }

func (c pePageIn) NewController() Controller {
	c.Prefix("pin")
	return c
}

func (c pePageIn) RENDER_json() {}

func (c pePageIn) RENDER_form() {}

func (c pePageIn) SUBMIT_json(b ctx.Body) ctx.Map { return ctx.Map{"got": b["k"]} }

func (c pePageIn) SUBMIT_form(f ctx.Form) ctx.Map { return ctx.Map{"got": f.Get("k")} }

func pePost(app *App, target, contentType, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	r.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

const (
	pfURLEncoded = "application/x-www-form-urlencoded"
	pfHostile    = `"><script>alert(1)</script>`
)

type pfBooking struct {
	Name   string `bind:"name"`
	Email  string `bind:"email"`
	Errors map[string]string
}

func (b pfBooking) Transform(f ctx.Form, _ common.ArgumentMetadata) any {
	bound, fls := f.Bind(b)
	dto := bound.(pfBooking)
	dto.Errors = map[string]string{}
	for _, fl := range fls {
		if !fl.IsValue() {
			dto.Errors[fl.Tag()] = fl.Tag() + " is required"
		}
	}
	if dto.Errors["name"] == "" && strings.TrimSpace(dto.Name) == "" {
		dto.Errors["name"] = "name is required"
	}
	if dto.Errors["email"] == "" && !strings.Contains(dto.Email, "@") {
		dto.Errors["email"] = "email must contain @"
	}
	return dto
}

type pfPages struct{ common.HTTP }

func (c pfPages) NewController() Controller {
	c.Prefix("bookings")
	return c
}

func (c pfPages) RENDER_new() pfBooking { return pfBooking{Errors: map[string]string{}} }

func (c pfPages) RENDER_goto(h *ctx.HTTPContext) { h.Redirect("/bookings") }

func (c pfPages) SUBMIT_new(h *ctx.HTTPContext, form pfBooking) pfBooking {
	if len(form.Errors) == 0 {
		h.Redirect("/bookings")
	}
	return form
}

func (c pfPages) RENDER_viaparam() pfBooking { return pfBooking{Errors: map[string]string{}} }

func (c pfPages) SUBMIT_viaparam(redirect ctx.Redirect, form pfBooking) pfBooking {
	if len(form.Errors) == 0 {
		redirect("/bookings")
	}
	return form
}

func (c pfPages) RENDER_explicit() {}

func (c pfPages) SUBMIT_explicit(h *ctx.HTTPContext, _ pfBooking) {
	h.Status(http.StatusTemporaryRedirect)
	h.Redirect("/bookings")
}

func (c pfPages) RENDER_found() {}

func (c pfPages) SUBMIT_found(h *ctx.HTTPContext, _ pfBooking) {
	h.Status(http.StatusFound)
	h.Redirect("/bookings")
}

func (c pfPages) RENDER_echo() {}

func (c pfPages) SUBMIT_echo(h *ctx.HTTPContext, form pfBooking) pfBooking {
	h.Status(http.StatusOK)
	return form
}

type pfAPI struct{ common.HTTP }

func (c pfAPI) NewController() Controller {
	c.Prefix("api")
	return c
}

func (c pfAPI) READ_goto(h *ctx.HTTPContext) string {
	h.Redirect("/bookings")
	return "marker"
}

func (c pfAPI) CREATE_new(h *ctx.HTTPContext, _ pfBooking) string {
	h.Redirect("/bookings")
	return "marker"
}

func (c pfAPI) CREATE_explicit(h *ctx.HTTPContext, _ pfBooking) string {
	h.Status(http.StatusTemporaryRedirect)
	h.Redirect("/bookings")
	return "marker"
}

func (c pfAPI) CREATE_echo(form pfBooking) ctx.Map {
	return ctx.Map{"name": form.Name, "email": form.Email}
}

func pfFiles() map[string]string {
	form := `<form method="post" action="/bookings/new">` +
		`<input name="name" value="{{.Name}}">{{with .Errors.name}}<p class="err">{{.}}</p>{{end}}` +
		`<input name="email" value="{{.Email}}">{{with .Errors.email}}<p class="err">{{.}}</p>{{end}}</form>`
	return map[string]string{
		"layout.html":            `<html><body>{{template "content" .}}</body></html>`,
		"bookings/new.html":      form,
		"bookings/viaparam.html": form,
		"bookings/echo.html":     `name={{.Name}};email={{.Email}}`,
		"bookings/goto.html":     `MARKER`,
		"bookings/explicit.html": `MARKER`,
		"bookings/found.html":    `MARKER`,
	}
}

func pfApp(t *testing.T, setup func(*App)) (*App, *lcRecordingLogger) {
	return peApp(t, pfFiles(), setup, pfPages{}, pfAPI{})
}

func pfPost(app *App, target, contentType, body string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	r.Header.Set("Content-Type", contentType)
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Add(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

func pfMultipart(t *testing.T, fields map[string]string) (string, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return mw.FormDataContentType(), buf.String()
}

func pfEncode(fields map[string]string) string {
	v := url.Values{}
	for k, val := range fields {
		v.Set(k, val)
	}
	return v.Encode()
}

var (
	nonceHex      = regexp.MustCompile(`^[0-9a-f]{32}$`)
	nonceAttr     = regexp.MustCompile(`(?:nonce|data-n)="([^"]*)"`)
	nonceFilesMap = map[string]string{
		"used.html":  `<script nonce="{{nonce}}">var n="{{nonce}}";</script><style nonce="{{nonce}}"></style><i data-n="{{nonce}}"></i>`,
		"plain.html": `<p>plain</p>`,
	}
)

type nonceFailing struct{ common.HTTP }

func (c nonceFailing) NewController() Controller { return c }

func (c nonceFailing) RENDER_boom() { panic(exception.NotFoundException("nothing here")) }

type noncePages struct{ common.HTTP }

func (c noncePages) NewController() Controller { return c }

func (c noncePages) RENDER_used() {}

func (c noncePages) RENDER_plain() {}

func nonceOf(t *testing.T, body string) string {
	t.Helper()
	found := nonceAttr.FindAllStringSubmatch(body, -1)
	if len(found) != 3 {
		t.Fatal(test.DiffMessage(len(found), 3, "R33: three nonce attributes in "+body))
	}
	for _, m := range found {
		if m[1] != found[0][1] {
			t.Error(test.DiffMessage(m[1], found[0][1], "R33: the same nonce at every use"))
		}
	}
	if !nonceHex.MatchString(found[0][1]) {
		t.Error(test.DiffMessage(found[0][1], "32 lowercase hexadecimal characters", "R33: nonce format"))
	}
	if n := strings.Count(body, found[0][1]); n != 4 {
		t.Error(test.DiffMessage(n, 4, "R33: the script string holds the same value"))
	}
	return found[0][1]
}

type rlSwapFS struct {
	mu    sync.RWMutex
	files fstest.MapFS
}

func (f *rlSwapFS) Open(name string) (fs.File, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.files.Open(name)
}

func (f *rlSwapFS) set(files fstest.MapFS) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files = files
}

type rlOne struct{ common.HTTP }

func (c rlOne) NewController() Controller { return c }

func (c rlOne) RENDER_a() {}

type rlTwo struct{ common.HTTP }

func (c rlTwo) NewController() Controller { return c }

func (c rlTwo) RENDER_a() {}

func (c rlTwo) RENDER_p() {}

type rlX struct{ common.HTTP }

func (c rlX) NewController() Controller { return c }

func (c rlX) RENDER_x() {}

type rlPageOnly struct{ common.HTTP }

func (c rlPageOnly) NewController() Controller { return c }

func (c rlPageOnly) RENDER_page() {}

func rlApp(t *testing.T, fsys fs.FS, isReload bool, controllers ...Controller) (*App, *lcRecordingLogger) {
	t.Helper()
	if len(controllers) == 0 {
		controllers = []Controller{rlOne{}}
	}
	logger := &lcRecordingLogger{}
	app := New()
	app.SetLogger(logger)
	app.EnableRender(&RenderOptions{FS: fsys, Dir: ".", IsReload: isReload})
	mustCreate(t, app, NewModuleBuilder().Controllers(controllers...).Build())
	return app, logger
}

func rlFile(content string, mod time.Time) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(content), ModTime: mod}
}

var rlEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type pgRows struct{ Rows []string }

type pgBig struct{ Rows []int }

type pgCountFS struct {
	inner fs.FS
	opens atomic.Int64
}

func (f *pgCountFS) Open(name string) (fs.File, error) {
	f.opens.Add(1)
	return f.inner.Open(name)
}

var pgHandlerCalls atomic.Int32

type pgDTO struct{ Word string }

func (pgDTO) Transform(q ctx.Query, _ common.ArgumentMetadata) any {
	return pgDTO{Word: "piped-" + q.Get("w")}
}

type pgGuard struct{}

func (pgGuard) CanActivate(c *ctx.HTTPContext) bool {
	return c.Request.Header.Get("X-Allow") == "yes"
}

type pgInterceptor struct{}

func (pgInterceptor) Intercept(_ *ctx.HTTPContext, agg *aggregation.Aggregation) any {
	return agg.Pipe(agg.Transform(func(any) any { return ctx.Map{"Word": "intercepted"} }))
}

type pgStringInterceptor struct{}

func (pgStringInterceptor) Intercept(_ *ctx.HTTPContext, agg *aggregation.Aggregation) any {
	return agg.Pipe(agg.Transform(func(any) any { return "replaced" }))
}

type pgShortInterceptor struct{}

func (pgShortInterceptor) Intercept(_ *ctx.HTTPContext, _ *aggregation.Aggregation) any {
	return ctx.Map{"Word": "short-circuit"}
}

type pgMiddleware struct{}

func (pgMiddleware) Use(_ *http.Request, w http.ResponseWriter, next ctx.Next) {
	w.Header().Set("X-Page-Middleware", "ran")
	next()
}

type pgPages struct{ common.HTTP }

func (c pgPages) NewController() Controller { return c }

func (c pgPages) RENDER_nodata() {}

func (c pgPages) RENDER_view(h *ctx.HTTPContext) ctx.Map { return ctx.Map{"X": h.Query().Get("x")} }

func (c pgPages) RENDER_plain(h *ctx.HTTPContext) ctx.Map { return ctx.Map{"X": h.Query().Get("x")} }

func (c pgPages) RENDER_custom(h *ctx.HTTPContext) ctx.Map { return ctx.Map{"X": h.Query().Get("x")} }

func (c pgPages) RENDER_esc(h *ctx.HTTPContext) ctx.Map { return ctx.Map{"X": h.Query().Get("x")} }

func (c pgPages) RENDER_url(h *ctx.HTTPContext) ctx.Map { return ctx.Map{"X": h.Query().Get("x")} }

func (c pgPages) RENDER_rows() pgRows { return pgRows{Rows: make([]string, 1000)} }

func (c pgPages) RENDER_big(h *ctx.HTTPContext) pgBig {
	n, _ := strconv.Atoi(h.Query().Get("n"))
	return pgBig{Rows: make([]int, n)}
}

func (c pgPages) RENDER_statusctx(h *ctx.HTTPContext) { h.Status(http.StatusUnprocessableEntity) }

func (c pgPages) RENDER_statusresult() (int, ctx.Map) {
	return http.StatusUnprocessableEntity, ctx.Map{"Word": "result"}
}

func (c pgPages) RENDER_cached(h *ctx.HTTPContext) {
	h.ResponseWriter.Header().Set("Cache-Control", "max-age=60")
}

func (c pgPages) RENDER_redirect(h *ctx.HTTPContext) { h.Redirect("/elsewhere") }

func (c pgPages) RENDER_direct(h *ctx.HTTPContext) { h.Text("direct write") }

func (c pgPages) RENDER_dict(h *ctx.HTTPContext) ctx.Map {
	return ctx.Map{"Value": h.Query().Get("v")}
}

var pgRoutes = []string{
	"nodata", "view", "plain", "custom", "esc", "url", "rows", "big",
	"statusctx", "statusresult", "cached", "redirect", "direct", "dict",
}

type pgPrefixed struct{ common.HTTP }

func (c pgPrefixed) NewController() Controller {
	c.Prefix("site")
	return c
}

func (c pgPrefixed) RENDER_home() {}

func (c pgPrefixed) SUBMIT_home(h *ctx.HTTPContext) { h.Redirect("/done") }

type pgLayered struct {
	common.HTTP
	common.Guard
	common.Interceptor
	common.Middleware
}

func (c pgLayered) NewController() Controller {
	c.BindGuard(pgGuard{})
	c.BindInterceptor(pgInterceptor{})
	c.BindMiddleware(pgMiddleware{})
	return c
}

func (c pgLayered) RENDER_layered(d pgDTO) ctx.Map { return ctx.Map{"Word": d.Word} }

type pgPipeOnly struct{ common.HTTP }

func (c pgPipeOnly) NewController() Controller { return c }

func (c pgPipeOnly) RENDER_piped(d pgDTO) ctx.Map { return ctx.Map{"Word": d.Word} }

type pgReplaced struct {
	common.HTTP
	common.Interceptor
}

func (c pgReplaced) NewController() Controller {
	c.BindInterceptor(pgStringInterceptor{})
	return c
}

func (c pgReplaced) RENDER_replaced() ctx.Map { return ctx.Map{"Word": "handler"} }

type pgShort struct {
	common.HTTP
	common.Interceptor
}

func (c pgShort) NewController() Controller {
	c.BindInterceptor(pgShortInterceptor{})
	return c
}

func (c pgShort) RENDER_short() ctx.Map {
	pgHandlerCalls.Add(1)
	return ctx.Map{"Word": "handler"}
}

type pgBookings struct{ common.HTTP }

func (c pgBookings) NewController() Controller { return c }

func (c pgBookings) RENDER_list_OF_bookings(h *ctx.HTTPContext) ctx.Map {
	return ctx.Map{"X": h.Query().Get("x")}
}

type pgNested struct{ common.HTTP }

func (c pgNested) NewController() Controller {
	c.Path("/admin/dash", c.RENDER_dash)
	c.Path("/admin/deep/inner", c.RENDER_inner)
	c.Path("/info", c.RENDER_info)
	return c
}

func (c pgNested) RENDER_home() {}

func (c pgNested) RENDER_dash() {}

func (c pgNested) RENDER_inner() {}

func (c pgNested) RENDER_info() {}

func pgFiles(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for name, content := range files {
		m[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return m
}

func pgPageFileMap(extra map[string]string) map[string]string {
	files := map[string]string{}
	for _, name := range pgRoutes {
		files[name+".html"] = "DEFAULT"
	}
	maps.Copy(files, extra)
	return files
}

func pgPageFiles(extra map[string]string) fstest.MapFS {
	return pgFiles(pgPageFileMap(extra))
}

func pgNew(t *testing.T, fsys fs.FS, controllers ...Controller) (*App, *lcRecordingLogger) {
	t.Helper()
	logger := &lcRecordingLogger{}
	app := New()
	app.SetLogger(logger)
	app.EnableRender(&RenderOptions{FS: fsys, Dir: "."})
	mustCreate(t, app, NewModuleBuilder().Controllers(controllers...).Build())
	return app, logger
}

func pgView(route, x string) string {
	return "/" + route + "?x=" + url.QueryEscape(x)
}

func pgExpectHTMLFixed500(t *testing.T, rule string, w *httptest.ResponseRecorder) {
	t.Helper()
	body := w.Body.String()
	if w.Code != http.StatusInternalServerError {
		t.Error(test.DiffMessage(w.Code, http.StatusInternalServerError, rule+": status"))
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Error(test.DiffMessage(ct, "text/html; charset=utf-8", rule+": the 500 is HTML"))
	}
	if !strings.Contains(body, "Internal Server Error") {
		t.Error(test.DiffMessage(body, "a body holding Internal Server Error", rule+": the fixed text"))
	}
}

type panicBenchController struct{ common.HTTP }

func (c panicBenchController) NewController() Controller { return c }

func (c panicBenchController) READ_boom(*ctx.HTTPContext) any { panic("boom") }

type panicBenchWriter struct{ header http.Header }

func (w *panicBenchWriter) Header() http.Header { return w.header }

func (w *panicBenchWriter) Write(p []byte) (int, error) { return len(p), nil }

func (w *panicBenchWriter) WriteHeader(int) {}

const (
	puRobots   = "User-agent: *\nDisallow: /private\n"
	puSecurity = "Contact: mailto:security@example.com\n"
	puRawHTML  = `<h1>{{.NotParsed}}</h1><script>alert(1)</script>`
	puSVG      = `<svg xmlns="http://www.w3.org/2000/svg"></svg>`
)

type puAPI struct{ common.HTTP }

func (c puAPI) NewController() Controller { return c }

func (c puAPI) READ_ping() string { return "pong" }

type puRobotsRoute struct{ common.HTTP }

func (c puRobotsRoute) NewController() Controller { return c }

func (c puRobotsRoute) READ_robots_txt_FILE() string {
	return "mine"
}

type puAbout struct{ common.HTTP }

func (c puAbout) NewController() Controller { return c }

func (c puAbout) RENDER_about() {}

func puFiles() fstest.MapFS {
	return pgFiles(map[string]string{
		"public/robots.txt":               puRobots,
		"public/.well-known/security.txt": puSecurity,
		"public/docs/index.html":          puRawHTML,
		"public/img/logo.svg":             puSVG,
		"public/.env":                     "SECRET=1",
		"public/.git/config":              "SECRET=2",
		"public/a/.hidden/x.txt":          "SECRET=3",
		"secret.txt":                      "OUTSIDE",
	})
}

func puApp(t *testing.T, files fstest.MapFS, setup func(*App), controllers ...Controller) *App {
	t.Helper()
	if len(controllers) == 0 {
		controllers = []Controller{puAPI{}}
	}
	app := New()
	app.SetLogger(&lcRecordingLogger{})
	app.EnableRender(&RenderOptions{FS: files, Dir: "."})
	if setup != nil {
		setup(app)
	}
	mustCreate(t, app, NewModuleBuilder().Controllers(controllers...).Build())
	return app
}

type puVersioned struct{ common.HTTP }

func (c puVersioned) NewController() Controller { return c }

func (c puVersioned) READ_ping_VERSION_1() string { return "pong" }

type rnItem struct{ Name string }

type rnData struct{ common.HTTP }

func (c rnData) NewController() Controller { return c }

func (c rnData) RENDER_map() ctx.Map { return ctx.Map{"Name": rnHostile} }

func (c rnData) RENDER_structdata() rnItem { return rnItem{Name: rnHostile} }

func (c rnData) RENDER_ptr() *rnItem { return &rnItem{Name: "ptr"} }

func (c rnData) RENDER_slice() []rnItem { return []rnItem{{Name: "a"}, {Name: rnHostile}} }

func (c rnData) RENDER_text() string { return "plain " + rnHostile }

func (c rnData) RENDER_nilresult() any { return nil }

func (c rnData) RENDER_noresult() {}

func (c rnData) RENDER_last() (int, ctx.Map) { return http.StatusAccepted, ctx.Map{"Name": "last"} }

const rnHostile = `<script>alert("x")</script>`

func rnDataFiles() map[string]string {
	return map[string]string{
		"map.html":        `<p>{{.Name}}</p>`,
		"structdata.html": `<p>{{.Name}}</p>`,
		"ptr.html":        `<p>{{.Name}}</p>`,
		"slice.html":      `{{range .}}<i>{{.Name}}</i>{{end}}`,
		"text.html":       `<p>{{.}}</p>`,
		"nilresult.html":  `{{if .}}data{{else}}nil{{end}}`,
		"noresult.html":   `{{if .}}data{{else}}nil{{end}}`,
		"last.html":       `<p>{{.Name}}</p>`,
	}
}

type rnMixed struct{ common.HTTP }

func (c rnMixed) NewController() Controller { return c }

func (c rnMixed) READ_api(h *ctx.HTTPContext) ctx.Map {
	if h.Query().Get("k") == "boom" {
		panic(exception.NotFoundException("no such api thing"))
	}
	return ctx.Map{"ok": true}
}

func (c rnMixed) RENDER_dashboard(h *ctx.HTTPContext) ctx.Map {
	if h.Query().Get("k") == "boom" {
		panic(exception.NotFoundException("no such page thing"))
	}
	return ctx.Map{"Who": "me"}
}

type rnRoot struct{ common.HTTP }

func (c rnRoot) NewController() Controller { return c }

func (c rnRoot) RENDER() ctx.Map { return ctx.Map{"Who": "root"} }

type rnBookings struct{ common.HTTP }

func (c rnBookings) NewController() Controller { return c }

func (c rnBookings) RENDER_bookings() ctx.Map { return ctx.Map{"Who": "list"} }

func (c rnBookings) RENDER_bookings_BY_id(p ctx.Param) ctx.Map {
	return ctx.Map{"Who": "id=" + p.Get("id")}
}

type rnAdminUsers struct{ common.HTTP }

func (c rnAdminUsers) NewController() Controller {
	c.Prefix("admin")
	return c
}

func (c rnAdminUsers) RENDER_users() ctx.Map { return ctx.Map{"Who": "users"} }

type rnAdminRoot struct{ common.HTTP }

func (c rnAdminRoot) NewController() Controller {
	c.Prefix("admin")
	return c
}

func (c rnAdminRoot) RENDER() ctx.Map { return ctx.Map{"Who": "admin"} }

type rnKebab struct{ common.HTTP }

func (c rnKebab) NewController() Controller { return c }

func (c rnKebab) RENDER_court_bookings() ctx.Map { return ctx.Map{"Who": "court"} }

type rnExact struct{ common.HTTP }

func (c rnExact) NewController() Controller {
	c.Path("/courts/layout", c.RENDER_lay)
	c.Path("/x/index", c.RENDER_idx)
	c.Path("/exact/page", c.RENDER_other)
	return c
}

func (c rnExact) RENDER_lay() ctx.Map { return ctx.Map{"Who": "lay"} }

func (c rnExact) RENDER_idx() ctx.Map { return ctx.Map{"Who": "idx"} }

func (c rnExact) RENDER_other() ctx.Map { return ctx.Map{"Who": "other"} }

type rnAssetsName struct{ common.HTTP }

func (c rnAssetsName) NewController() Controller { return c }

func (c rnAssetsName) RENDER_assets() ctx.Map { return ctx.Map{"Who": "assets"} }

func rnServe(t *testing.T, files map[string]string, build func() *Module, target string) *httpResult {
	t.Helper()
	app := New()
	app.SetLogger(&lcRecordingLogger{})
	app.EnableRender(&RenderOptions{FS: pgFiles(files), Dir: "."})
	mustCreate(t, app, build())
	w := createGet(app, target)
	return &httpResult{code: w.Code, body: w.Body.String()}
}

type httpResult struct {
	code int
	body string
}

func rnModule(controllers ...Controller) func() *Module {
	return func() *Module { return NewModuleBuilder().Controllers(controllers...).Build() }
}

type pgNodataOnly struct{ common.HTTP }

func (c pgNodataOnly) NewController() Controller { return c }

func (c pgNodataOnly) RENDER_nodata() {}

type rnMixedAPIOnly struct{ common.HTTP }

func (c rnMixedAPIOnly) NewController() Controller { return c }

func (c rnMixedAPIOnly) READ_x() string { return "x" }

const scImmutable = "public, max-age=31536000, immutable"

var scRoot string

type scController struct{ common.HTTP }

func (c scController) NewController() Controller { return c }

func (c scController) SERVE_ANY_OF_files() string { return scRoot }

func (c scController) SERVE_logo_png_FILE() string {
	return filepath.Join(scRoot, "logo.png")
}

func (c scController) SERVE_missing_png_FILE() string {
	return filepath.Join(scRoot, "missing.png")
}

func (c scController) SERVE_docs() string { return scRoot }

func (c scController) SERVE_ANY_OF_assets(hc *ctx.HTTPContext) string {
	hc.ResponseWriter.Header().Set("Cache-Control", scImmutable)
	return scRoot
}

type scMiddleware struct{ Value string }

func (m scMiddleware) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	w.Header().Set("Cache-Control", m.Value)
	next()
}

type scGuard struct{ Value string }

func (g scGuard) CanActivate(c *ctx.HTTPContext) bool {
	c.ResponseWriter.Header().Set("Cache-Control", g.Value)
	return true
}

func scApp(t *testing.T, setup func(*App)) *App {
	t.Helper()
	scRoot = filepath.Join(t.TempDir(), "public")
	writeServeFile(t, filepath.Join(scRoot, "logo.png"), "hello")
	writeServeFile(t, filepath.Join(scRoot, "css", "app.css"), "body{}")
	writeServeFile(t, filepath.Join(scRoot, "index.html"), "<p>home</p>")
	writeServeFile(t, filepath.Join(scRoot, ".env"), "SECRET=1")
	app := New()
	app.SetLogger(&mockLogger{})
	if setup != nil {
		setup(app)
	}
	app.Create(NewModuleBuilder().Controllers(scController{}).Build())
	return app
}

func scGet(app *App, target string, headers ...string) *httptest.ResponseRecorder {
	return createRequest(app, http.MethodGet, target, headers...)
}

func expectCacheControl(t *testing.T, w *httptest.ResponseRecorder, want, desc string) {
	t.Helper()
	got := w.Header().Values("Cache-Control")
	if want == "" {
		if len(got) != 0 {
			t.Error(test.DiffMessage(got, []string{}, desc))
		}
		return
	}
	if len(got) != 1 || got[0] != want {
		t.Error(test.DiffMessage(got, []string{want}, desc))
	}
}

type serveDotController struct {
	common.HTTP
}

func (c serveDotController) NewController() Controller { return c }

func (c serveDotController) SERVE_ANY_OF_dot() string { return "." }

type sbPages struct{ common.HTTP }

func (c sbPages) NewController() Controller { return c }

func (c sbPages) RENDER_new_OF_bookings() pfBooking { return pfBooking{Errors: map[string]string{}} }

func (c sbPages) SUBMIT_new_OF_bookings(h *ctx.HTTPContext, form pfBooking) pfBooking {
	if len(form.Errors) == 0 {
		h.Redirect("/bookings")
	}
	return form
}

func (c sbPages) RENDER_bookings_BY_id(p ctx.Param) ctx.Map { return ctx.Map{"Id": p.Get("id")} }

func (c sbPages) SUBMIT_bookings_BY_id(p ctx.Param, form pfBooking) ctx.Map {
	return ctx.Map{"Id": p.Get("id"), "Name": form.Name}
}

func (c sbPages) RENDER() {}

func (c sbPages) SUBMIT(h *ctx.HTTPContext) { h.Redirect("/bookings/new") }

func (c sbPages) CREATE_api(h *ctx.HTTPContext) ctx.Map { return ctx.Map{"created": true} }

func sbFiles() map[string]string {
	return map[string]string{
		"index.html":         "HOME",
		"bookings/new.html":  `FORM name={{.Name}} errors={{len .Errors}}`,
		"bookings/{id}.html": `BOOKING {{.Id}} {{.Name}}`,
	}
}

func sbValid() string {
	return pfEncode(map[string]string{"name": "Ann", "email": "ann@example.com"})
}

type sbStatus struct{ common.HTTP }

func (c sbStatus) NewController() Controller { return c }

func (c sbStatus) RENDER_ctx() {}

func (c sbStatus) SUBMIT_ctx(h *ctx.HTTPContext) { h.Status(http.StatusOK) }

func (c sbStatus) RENDER_result() {}

func (c sbStatus) SUBMIT_result() (int, ctx.Map) { return http.StatusCreated, ctx.Map{"Word": "made"} }

func (c sbStatus) RENDER_plain() {}

func (c sbStatus) SUBMIT_plain() {}

func (c sbStatus) RENDER_nodata() {}

func (c sbStatus) SUBMIT_nodata() ctx.Map { return nil }

type sbGuarded struct {
	common.HTTP
	common.Guard
}

func (c sbGuarded) NewController() Controller {
	c.BindGuard(pgGuard{}, c.RENDER_only)
	c.BindGuard(pgGuard{})
	return c
}

func (c sbGuarded) RENDER_only() {}

func (c sbGuarded) SUBMIT_only(h *ctx.HTTPContext) { h.Redirect("/done") }

type sbGuardedRenderOnly struct {
	common.HTTP
	common.Guard
}

func (c sbGuardedRenderOnly) NewController() Controller {
	c.BindGuard(pgGuard{}, c.RENDER_only)
	return c
}

func (c sbGuardedRenderOnly) RENDER_only() {}

func (c sbGuardedRenderOnly) SUBMIT_only(h *ctx.HTTPContext) { h.Redirect("/done") }

type sbFailing struct{ common.HTTP }

func (c sbFailing) NewController() Controller { return c }

func (c sbFailing) RENDER_fail() {}

func (c sbFailing) SUBMIT_fail() { panic(exception.NotFoundException("no such booking")) }

const (
	wantNotFoundJSON         = `{"code":404,"error":"Not Found","message":"Not Found"}`
	wantMethodNotAllowedJSON = `{"code":405,"error":"Method Not Allowed","message":"Method Not Allowed"}`
	wantGoneJSON             = `{"code":410,"error":"Gone","message":"Deprecated URL usage"}`
	wantInternalErrorJSON    = `{"code":500,"error":"Internal Server Error","message":"Internal Server Error"}`
	wantInvalidURLJSON       = `{"code":400,"error":"Bad Request","message":"Invalid URL path"}`
)

var serveRoot string

type serveController struct {
	common.HTTP
}

func (c serveController) NewController() Controller { return c }

func (c serveController) SERVE_ANY_OF_files() string { return serveRoot }

func (c serveController) SERVE_logo_png_FILE() string {
	return filepath.Join(serveRoot, "logo.png")
}

func (c serveController) SERVE_missing_png_FILE() string {
	return filepath.Join(serveRoot, "missing.png")
}

func (c serveController) SERVE_docs() string { return serveRoot }

func (c serveController) SERVE_number() int { return 42 }

func newServeApp(t *testing.T) *App {
	t.Helper()
	serveRoot = filepath.Join(t.TempDir(), "public")
	if err := os.MkdirAll(filepath.Join(serveRoot, "css"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeServeFile(t, filepath.Join(serveRoot, "logo.png"), "hello")
	writeServeFile(t, filepath.Join(serveRoot, "css", "app.css"), "body{}")
	writeServeFile(t, filepath.Join(serveRoot, ".env"), "SECRET=1")

	app := New()
	app.Create(NewModuleBuilder().Controllers(serveController{}).Build())
	return app
}

func writeServeFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func serveGet(app *App, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

type verController struct {
	common.HTTP
}

func (ctl verController) NewController() Controller { return ctl }

func (ctl verController) READ_ver_VERSION_1() string { return "v1" }

func (ctl verController) READ_ver_VERSION_2() string { return "v2" }

func (ctl verController) READ_neutral_VERSION_NEUTRAL() string { return "neutral" }

func (ctl verController) READ_plain() string { return "plain" }

func verApp(t *testing.T, defaultVersion string) *App {
	t.Helper()
	lcReset(t)
	app := New()
	app.EnableVersioning(&versioning.Options{Type: versioning.TypeHeader, Key: "X-Version", DefaultVersion: defaultVersion})
	app.BindGlobalMiddlewares(lcMiddleware{Name: "g1"})
	app.Create(NewModuleBuilder().Controllers(verController{}).Build())
	return app
}

var errWriterHijack = errors.New("hijack probe")

type wrFullWriter struct {
	*httptest.ResponseRecorder
	flushes   int
	readFroms int
	hijacks   int
	deadlines []time.Time
}

func (w *wrFullWriter) Flush() {
	w.flushes++
	w.ResponseRecorder.Flush()
}

func (w *wrFullWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijacks++
	return nil, nil, errWriterHijack
}

func (w *wrFullWriter) ReadFrom(r io.Reader) (int64, error) {
	w.readFroms++
	return io.Copy(w.ResponseRecorder, r)
}

func (w *wrFullWriter) SetWriteDeadline(t time.Time) error {
	w.deadlines = append(w.deadlines, t)
	return nil
}

func wrServe(t *testing.T, w http.ResponseWriter, handler func(c *ctx.HTTPContext) any) {
	t.Helper()
	app := lcNewApp(t, lcConfig{noGlobalFilters: true})
	lcSetHandler(handler)
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/bare", nil))
}

// pooledHook stands for a middleware that pools the writer it set as inner (compress) in Done.
type pooledHook struct{}

func (pooledHook) Done(time.Duration) {}

type coreHoldWriter struct {
	mu      sync.Mutex
	gate    chan struct{}
	entered chan struct{}
	buf     bytes.Buffer
}

func (w *coreHoldWriter) hold() {
	w.mu.Lock()
	if w.gate == nil {
		w.gate = make(chan struct{})
	}
	w.mu.Unlock()
}

func (w *coreHoldWriter) openAll() {
	w.mu.Lock()
	if w.gate != nil {
		close(w.gate)
		w.gate = nil
	}
	w.mu.Unlock()
}

func (w *coreHoldWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	gate := w.gate
	w.mu.Unlock()
	select {
	case w.entered <- struct{}{}:
	default:
	}
	if gate != nil {
		<-gate
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *coreHoldWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func redirectStdout(t *testing.T) *coreHoldWriter {
	t.Helper()
	w := &coreHoldWriter{entered: make(chan struct{}, 1<<12)}
	restore := logpipe.SetStdout(w)
	t.Cleanup(func() {
		w.openAll()
		restore()
	})
	return w
}

func withinCore(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal(test.DiffMessage("blocked for 30s", "returned", what))
	}
}

func scopedContext() context.Context {
	return tracecontext.With(context.Background(), &tracecontext.Scope{TraceID: scopeTraceID, Request: "00f067aa0ba902b7"})
}

type fromProvider struct{ Log log.Logger }

var fromProviderSeen log.Logger

func (p fromProvider) NewProvider() Provider {
	fromProviderSeen = p.Log
	return p
}

type bootstrapPanicProvider struct{ Log log.Logger }

func (p bootstrapPanicProvider) NewProvider() Provider {
	p.Log.Info("before-bootstrap-panic")
	panic("provider exploded")
}

func openWhenEntered(w *coreHoldWriter) {
	go func() {
		<-w.entered
		w.openAll()
	}()
}

var panicSites = [...]func(){
	func() { panic("site 0") },
	func() { panic("site 1") },
	func() { panic("site 2") },
	func() { panic("site 3") },
	func() { panic("site 4") },
	func() { panic("site 5") },
	func() { panic("site 6") },
	func() { panic("site 7") },
	func() { panic("site 8") },
	func() { panic("site 9") },
	func() { panic("site 10") },
	func() { panic("site 11") },
}

func sampleHandler(c *ctx.HTTPContext) any {
	n, _ := strconv.Atoi(c.URL.Query().Get("s"))
	panicSites[n]()
	return nil
}

type fakeStackTimer struct {
	d       time.Duration
	f       func()
	stopped bool
}

type fakeStackClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeStackTimer
}

func installStackSeams(t *testing.T) *fakeStackClock {
	t.Helper()
	c := &fakeStackClock{now: time.Unix(1_000_000, 250_000_000)}
	savedNow, savedAfter := stackNow, stackAfter
	stackNow = func() time.Time {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.now
	}
	stackAfter = func(d time.Duration, f func()) func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		timer := &fakeStackTimer{d: d, f: f}
		c.timers = append(c.timers, timer)
		return func() bool {
			c.mu.Lock()
			defer c.mu.Unlock()
			was := !timer.stopped
			timer.stopped = true
			return was
		}
	}
	t.Cleanup(func() { stackNow, stackAfter = savedNow, savedAfter })
	return c
}

func (c *fakeStackClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func (c *fakeStackClock) armed() []*fakeStackTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.timers)
}

func sampleApp(t *testing.T) (*App, *lcRecordingLogger) {
	t.Helper()
	logger := &lcRecordingLogger{}
	app := lcNewApp(t, lcConfig{noGlobalFilters: true, logger: logger})
	lcSetHandler(sampleHandler)
	return app, logger
}

func serveFresh(app *App, target string) *httptest.ResponseRecorder {
	var w *httptest.ResponseRecorder
	done := make(chan struct{})
	go func() {
		defer close(done)
		w = lcServe(app, http.MethodGet, target)
	}()
	<-done
	return w
}

func panicAt(app *App, site int) *httptest.ResponseRecorder {
	return serveFresh(app, "/bare?s="+strconv.Itoa(site))
}

func hasKey(args []any, key string) bool {
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == key {
			return true
		}
	}
	return false
}

func keysOf(args []any) []string {
	var keys []string
	for i := 0; i+1 < len(args); i += 2 {
		keys = append(keys, args[i].(string))
	}
	return keys
}

func stackCount(entries []lcLogEntry) int {
	n := 0
	for _, e := range entries {
		if hasKey(e.args, "stack") {
			n++
		}
	}
	return n
}

type benchModuleController0 struct{ common.HTTP }

func (c benchModuleController0) NewController() Controller { return c }

func (c benchModuleController0) READ_benchmoduleresource0() string { return "ok" }

type benchModuleController1 struct{ common.HTTP }

func (c benchModuleController1) NewController() Controller { return c }

func (c benchModuleController1) READ_benchmoduleresource1() string { return "ok" }

type benchModuleController2 struct{ common.HTTP }

func (c benchModuleController2) NewController() Controller { return c }

func (c benchModuleController2) READ_benchmoduleresource2() string { return "ok" }

type benchModuleController3 struct{ common.HTTP }

func (c benchModuleController3) NewController() Controller { return c }

func (c benchModuleController3) READ_benchmoduleresource3() string { return "ok" }

type benchModuleController4 struct{ common.HTTP }

func (c benchModuleController4) NewController() Controller { return c }

func (c benchModuleController4) READ_benchmoduleresource4() string { return "ok" }

type benchChainProvider0 struct{}

func (p benchChainProvider0) NewProvider() Provider { return p }

type benchChainProvider1 struct{ Prev benchChainProvider0 }

func (p benchChainProvider1) NewProvider() Provider { return p }

type benchChainProvider2 struct{ Prev benchChainProvider1 }

func (p benchChainProvider2) NewProvider() Provider { return p }

type benchChainProvider3 struct{ Prev benchChainProvider2 }

func (p benchChainProvider3) NewProvider() Provider { return p }

type benchChainProvider4 struct{ Prev benchChainProvider3 }

func (p benchChainProvider4) NewProvider() Provider { return p }

type benchChainProvider5 struct{ Prev benchChainProvider4 }

func (p benchChainProvider5) NewProvider() Provider { return p }

type benchChainProvider6 struct{ Prev benchChainProvider5 }

func (p benchChainProvider6) NewProvider() Provider { return p }

type benchChainProvider7 struct{ Prev benchChainProvider6 }

func (p benchChainProvider7) NewProvider() Provider { return p }

type benchChainProvider8 struct{ Prev benchChainProvider7 }

func (p benchChainProvider8) NewProvider() Provider { return p }

type benchChainProvider9 struct{ Prev benchChainProvider8 }

func (p benchChainProvider9) NewProvider() Provider { return p }

type benchChainProvider10 struct{ Prev benchChainProvider9 }

func (p benchChainProvider10) NewProvider() Provider { return p }

type benchChainProvider11 struct{ Prev benchChainProvider10 }

func (p benchChainProvider11) NewProvider() Provider { return p }

type benchChainProvider12 struct{ Prev benchChainProvider11 }

func (p benchChainProvider12) NewProvider() Provider { return p }

type benchChainProvider13 struct{ Prev benchChainProvider12 }

func (p benchChainProvider13) NewProvider() Provider { return p }

type benchChainProvider14 struct{ Prev benchChainProvider13 }

func (p benchChainProvider14) NewProvider() Provider { return p }

type benchChainProvider15 struct{ Prev benchChainProvider14 }

func (p benchChainProvider15) NewProvider() Provider { return p }

type benchChainProvider16 struct{ Prev benchChainProvider15 }

func (p benchChainProvider16) NewProvider() Provider { return p }

type benchChainProvider17 struct{ Prev benchChainProvider16 }

func (p benchChainProvider17) NewProvider() Provider { return p }

type benchChainProvider18 struct{ Prev benchChainProvider17 }

func (p benchChainProvider18) NewProvider() Provider { return p }

type benchChainProvider19 struct{ Prev benchChainProvider18 }

func (p benchChainProvider19) NewProvider() Provider { return p }

type benchChainProvider20 struct{ Prev benchChainProvider19 }

func (p benchChainProvider20) NewProvider() Provider { return p }

type benchChainProvider21 struct{ Prev benchChainProvider20 }

func (p benchChainProvider21) NewProvider() Provider { return p }

type benchChainProvider22 struct{ Prev benchChainProvider21 }

func (p benchChainProvider22) NewProvider() Provider { return p }

type benchChainProvider23 struct{ Prev benchChainProvider22 }

func (p benchChainProvider23) NewProvider() Provider { return p }

type benchChainProvider24 struct{ Prev benchChainProvider23 }

func (p benchChainProvider24) NewProvider() Provider { return p }

type benchChainProvider25 struct{ Prev benchChainProvider24 }

func (p benchChainProvider25) NewProvider() Provider { return p }

type benchChainProvider26 struct{ Prev benchChainProvider25 }

func (p benchChainProvider26) NewProvider() Provider { return p }

type benchChainProvider27 struct{ Prev benchChainProvider26 }

func (p benchChainProvider27) NewProvider() Provider { return p }

type benchChainProvider28 struct{ Prev benchChainProvider27 }

func (p benchChainProvider28) NewProvider() Provider { return p }

type benchChainProvider29 struct{ Prev benchChainProvider28 }

func (p benchChainProvider29) NewProvider() Provider { return p }

type benchChainProvider30 struct{ Prev benchChainProvider29 }

func (p benchChainProvider30) NewProvider() Provider { return p }

type benchChainProvider31 struct{ Prev benchChainProvider30 }

func (p benchChainProvider31) NewProvider() Provider { return p }

type benchChainProvider32 struct{ Prev benchChainProvider31 }

func (p benchChainProvider32) NewProvider() Provider { return p }

type benchChainProvider33 struct{ Prev benchChainProvider32 }

func (p benchChainProvider33) NewProvider() Provider { return p }

type benchChainProvider34 struct{ Prev benchChainProvider33 }

func (p benchChainProvider34) NewProvider() Provider { return p }

type benchChainProvider35 struct{ Prev benchChainProvider34 }

func (p benchChainProvider35) NewProvider() Provider { return p }

type benchChainProvider36 struct{ Prev benchChainProvider35 }

func (p benchChainProvider36) NewProvider() Provider { return p }

type benchChainProvider37 struct{ Prev benchChainProvider36 }

func (p benchChainProvider37) NewProvider() Provider { return p }

type benchChainProvider38 struct{ Prev benchChainProvider37 }

func (p benchChainProvider38) NewProvider() Provider { return p }

type benchChainProvider39 struct{ Prev benchChainProvider38 }

func (p benchChainProvider39) NewProvider() Provider { return p }

type benchChainProvider40 struct{ Prev benchChainProvider39 }

func (p benchChainProvider40) NewProvider() Provider { return p }

type benchChainProvider41 struct{ Prev benchChainProvider40 }

func (p benchChainProvider41) NewProvider() Provider { return p }

type benchChainProvider42 struct{ Prev benchChainProvider41 }

func (p benchChainProvider42) NewProvider() Provider { return p }

type benchChainProvider43 struct{ Prev benchChainProvider42 }

func (p benchChainProvider43) NewProvider() Provider { return p }

type benchChainProvider44 struct{ Prev benchChainProvider43 }

func (p benchChainProvider44) NewProvider() Provider { return p }

type benchChainProvider45 struct{ Prev benchChainProvider44 }

func (p benchChainProvider45) NewProvider() Provider { return p }

type benchChainProvider46 struct{ Prev benchChainProvider45 }

func (p benchChainProvider46) NewProvider() Provider { return p }

type benchChainProvider47 struct{ Prev benchChainProvider46 }

func (p benchChainProvider47) NewProvider() Provider { return p }

type benchChainProvider48 struct{ Prev benchChainProvider47 }

func (p benchChainProvider48) NewProvider() Provider { return p }

type benchChainProvider49 struct{ Prev benchChainProvider48 }

func (p benchChainProvider49) NewProvider() Provider { return p }

type benchChainProvider50 struct{ Prev benchChainProvider49 }

func (p benchChainProvider50) NewProvider() Provider { return p }

type benchChainProvider51 struct{ Prev benchChainProvider50 }

func (p benchChainProvider51) NewProvider() Provider { return p }

type benchChainProvider52 struct{ Prev benchChainProvider51 }

func (p benchChainProvider52) NewProvider() Provider { return p }

type benchChainProvider53 struct{ Prev benchChainProvider52 }

func (p benchChainProvider53) NewProvider() Provider { return p }

type benchChainProvider54 struct{ Prev benchChainProvider53 }

func (p benchChainProvider54) NewProvider() Provider { return p }

type benchChainProvider55 struct{ Prev benchChainProvider54 }

func (p benchChainProvider55) NewProvider() Provider { return p }

type benchChainProvider56 struct{ Prev benchChainProvider55 }

func (p benchChainProvider56) NewProvider() Provider { return p }

type benchChainProvider57 struct{ Prev benchChainProvider56 }

func (p benchChainProvider57) NewProvider() Provider { return p }

type benchChainProvider58 struct{ Prev benchChainProvider57 }

func (p benchChainProvider58) NewProvider() Provider { return p }

type benchChainProvider59 struct{ Prev benchChainProvider58 }

func (p benchChainProvider59) NewProvider() Provider { return p }

type benchChainProvider60 struct{ Prev benchChainProvider59 }

func (p benchChainProvider60) NewProvider() Provider { return p }

type benchChainProvider61 struct{ Prev benchChainProvider60 }

func (p benchChainProvider61) NewProvider() Provider { return p }

type benchChainProvider62 struct{ Prev benchChainProvider61 }

func (p benchChainProvider62) NewProvider() Provider { return p }

type benchChainProvider63 struct{ Prev benchChainProvider62 }

func (p benchChainProvider63) NewProvider() Provider { return p }

type benchChainProvider64 struct{ Prev benchChainProvider63 }

func (p benchChainProvider64) NewProvider() Provider { return p }

type benchChainProvider65 struct{ Prev benchChainProvider64 }

func (p benchChainProvider65) NewProvider() Provider { return p }

type benchChainProvider66 struct{ Prev benchChainProvider65 }

func (p benchChainProvider66) NewProvider() Provider { return p }

type benchChainProvider67 struct{ Prev benchChainProvider66 }

func (p benchChainProvider67) NewProvider() Provider { return p }

type benchChainProvider68 struct{ Prev benchChainProvider67 }

func (p benchChainProvider68) NewProvider() Provider { return p }

type benchChainProvider69 struct{ Prev benchChainProvider68 }

func (p benchChainProvider69) NewProvider() Provider { return p }

type benchChainProvider70 struct{ Prev benchChainProvider69 }

func (p benchChainProvider70) NewProvider() Provider { return p }

type benchChainProvider71 struct{ Prev benchChainProvider70 }

func (p benchChainProvider71) NewProvider() Provider { return p }

type benchChainProvider72 struct{ Prev benchChainProvider71 }

func (p benchChainProvider72) NewProvider() Provider { return p }

type benchChainProvider73 struct{ Prev benchChainProvider72 }

func (p benchChainProvider73) NewProvider() Provider { return p }

type benchChainProvider74 struct{ Prev benchChainProvider73 }

func (p benchChainProvider74) NewProvider() Provider { return p }

type benchChainProvider75 struct{ Prev benchChainProvider74 }

func (p benchChainProvider75) NewProvider() Provider { return p }

type benchChainProvider76 struct{ Prev benchChainProvider75 }

func (p benchChainProvider76) NewProvider() Provider { return p }

type benchChainProvider77 struct{ Prev benchChainProvider76 }

func (p benchChainProvider77) NewProvider() Provider { return p }

type benchChainProvider78 struct{ Prev benchChainProvider77 }

func (p benchChainProvider78) NewProvider() Provider { return p }

type benchChainProvider79 struct{ Prev benchChainProvider78 }

func (p benchChainProvider79) NewProvider() Provider { return p }

type benchChainProvider80 struct{ Prev benchChainProvider79 }

func (p benchChainProvider80) NewProvider() Provider { return p }

type benchChainProvider81 struct{ Prev benchChainProvider80 }

func (p benchChainProvider81) NewProvider() Provider { return p }

type benchChainProvider82 struct{ Prev benchChainProvider81 }

func (p benchChainProvider82) NewProvider() Provider { return p }

type benchChainProvider83 struct{ Prev benchChainProvider82 }

func (p benchChainProvider83) NewProvider() Provider { return p }

type benchChainProvider84 struct{ Prev benchChainProvider83 }

func (p benchChainProvider84) NewProvider() Provider { return p }

type benchChainProvider85 struct{ Prev benchChainProvider84 }

func (p benchChainProvider85) NewProvider() Provider { return p }

type benchChainProvider86 struct{ Prev benchChainProvider85 }

func (p benchChainProvider86) NewProvider() Provider { return p }

type benchChainProvider87 struct{ Prev benchChainProvider86 }

func (p benchChainProvider87) NewProvider() Provider { return p }

type benchChainProvider88 struct{ Prev benchChainProvider87 }

func (p benchChainProvider88) NewProvider() Provider { return p }

type benchChainProvider89 struct{ Prev benchChainProvider88 }

func (p benchChainProvider89) NewProvider() Provider { return p }

type benchChainProvider90 struct{ Prev benchChainProvider89 }

func (p benchChainProvider90) NewProvider() Provider { return p }

type benchChainProvider91 struct{ Prev benchChainProvider90 }

func (p benchChainProvider91) NewProvider() Provider { return p }

type benchChainProvider92 struct{ Prev benchChainProvider91 }

func (p benchChainProvider92) NewProvider() Provider { return p }

type benchChainProvider93 struct{ Prev benchChainProvider92 }

func (p benchChainProvider93) NewProvider() Provider { return p }

type benchChainProvider94 struct{ Prev benchChainProvider93 }

func (p benchChainProvider94) NewProvider() Provider { return p }

type benchChainProvider95 struct{ Prev benchChainProvider94 }

func (p benchChainProvider95) NewProvider() Provider { return p }

type benchChainProvider96 struct{ Prev benchChainProvider95 }

func (p benchChainProvider96) NewProvider() Provider { return p }

type benchChainProvider97 struct{ Prev benchChainProvider96 }

func (p benchChainProvider97) NewProvider() Provider { return p }

type benchChainProvider98 struct{ Prev benchChainProvider97 }

func (p benchChainProvider98) NewProvider() Provider { return p }

type benchChainProvider99 struct{ Prev benchChainProvider98 }

func (p benchChainProvider99) NewProvider() Provider { return p }

func benchChainProviders() []Provider {
	providers := make([]Provider, 100)
	providers[0] = benchChainProvider0{}
	providers[1] = benchChainProvider1{}
	providers[2] = benchChainProvider2{}
	providers[3] = benchChainProvider3{}
	providers[4] = benchChainProvider4{}
	providers[5] = benchChainProvider5{}
	providers[6] = benchChainProvider6{}
	providers[7] = benchChainProvider7{}
	providers[8] = benchChainProvider8{}
	providers[9] = benchChainProvider9{}
	providers[10] = benchChainProvider10{}
	providers[11] = benchChainProvider11{}
	providers[12] = benchChainProvider12{}
	providers[13] = benchChainProvider13{}
	providers[14] = benchChainProvider14{}
	providers[15] = benchChainProvider15{}
	providers[16] = benchChainProvider16{}
	providers[17] = benchChainProvider17{}
	providers[18] = benchChainProvider18{}
	providers[19] = benchChainProvider19{}
	providers[20] = benchChainProvider20{}
	providers[21] = benchChainProvider21{}
	providers[22] = benchChainProvider22{}
	providers[23] = benchChainProvider23{}
	providers[24] = benchChainProvider24{}
	providers[25] = benchChainProvider25{}
	providers[26] = benchChainProvider26{}
	providers[27] = benchChainProvider27{}
	providers[28] = benchChainProvider28{}
	providers[29] = benchChainProvider29{}
	providers[30] = benchChainProvider30{}
	providers[31] = benchChainProvider31{}
	providers[32] = benchChainProvider32{}
	providers[33] = benchChainProvider33{}
	providers[34] = benchChainProvider34{}
	providers[35] = benchChainProvider35{}
	providers[36] = benchChainProvider36{}
	providers[37] = benchChainProvider37{}
	providers[38] = benchChainProvider38{}
	providers[39] = benchChainProvider39{}
	providers[40] = benchChainProvider40{}
	providers[41] = benchChainProvider41{}
	providers[42] = benchChainProvider42{}
	providers[43] = benchChainProvider43{}
	providers[44] = benchChainProvider44{}
	providers[45] = benchChainProvider45{}
	providers[46] = benchChainProvider46{}
	providers[47] = benchChainProvider47{}
	providers[48] = benchChainProvider48{}
	providers[49] = benchChainProvider49{}
	providers[50] = benchChainProvider50{}
	providers[51] = benchChainProvider51{}
	providers[52] = benchChainProvider52{}
	providers[53] = benchChainProvider53{}
	providers[54] = benchChainProvider54{}
	providers[55] = benchChainProvider55{}
	providers[56] = benchChainProvider56{}
	providers[57] = benchChainProvider57{}
	providers[58] = benchChainProvider58{}
	providers[59] = benchChainProvider59{}
	providers[60] = benchChainProvider60{}
	providers[61] = benchChainProvider61{}
	providers[62] = benchChainProvider62{}
	providers[63] = benchChainProvider63{}
	providers[64] = benchChainProvider64{}
	providers[65] = benchChainProvider65{}
	providers[66] = benchChainProvider66{}
	providers[67] = benchChainProvider67{}
	providers[68] = benchChainProvider68{}
	providers[69] = benchChainProvider69{}
	providers[70] = benchChainProvider70{}
	providers[71] = benchChainProvider71{}
	providers[72] = benchChainProvider72{}
	providers[73] = benchChainProvider73{}
	providers[74] = benchChainProvider74{}
	providers[75] = benchChainProvider75{}
	providers[76] = benchChainProvider76{}
	providers[77] = benchChainProvider77{}
	providers[78] = benchChainProvider78{}
	providers[79] = benchChainProvider79{}
	providers[80] = benchChainProvider80{}
	providers[81] = benchChainProvider81{}
	providers[82] = benchChainProvider82{}
	providers[83] = benchChainProvider83{}
	providers[84] = benchChainProvider84{}
	providers[85] = benchChainProvider85{}
	providers[86] = benchChainProvider86{}
	providers[87] = benchChainProvider87{}
	providers[88] = benchChainProvider88{}
	providers[89] = benchChainProvider89{}
	providers[90] = benchChainProvider90{}
	providers[91] = benchChainProvider91{}
	providers[92] = benchChainProvider92{}
	providers[93] = benchChainProvider93{}
	providers[94] = benchChainProvider94{}
	providers[95] = benchChainProvider95{}
	providers[96] = benchChainProvider96{}
	providers[97] = benchChainProvider97{}
	providers[98] = benchChainProvider98{}
	providers[99] = benchChainProvider99{}
	return providers
}

type benchStaticSubProvider struct{}

func (p benchStaticSubProvider) NewProvider() Provider { return p }

func makeBenchStaticSubmodules(n int) []any {
	imports := make([]any, n)
	for i := range imports {
		imports[i] = NewModuleBuilder().Providers(benchStaticSubProvider{}).Build()
	}
	return imports
}

func benchCollectModulesFactory() *Module {
	return NewModuleBuilder().Build()
}

type hookBuilderProvider struct{}

func (p hookBuilderProvider) NewProvider() Provider { return p }

type hookBuilderController struct{}

func (c hookBuilderController) NewController() Controller { return c }

var testStaticModule = func() *Module {
	return NewModuleBuilder().Build()
}

var (
	testGroupedModuleA = func() *Module { return NewModuleBuilder().Build() }
	testGroupedModuleB = func() *Module { return NewModuleBuilder().Build() }
)

func testModuleFactory() *Module {
	return NewModuleBuilder().Build()
}

var testFactoryBackedModule = testModuleFactory()

var testOuterModuleWithNestedFactoryImport = func() *Module {
	return NewModuleBuilder().Imports(testModuleFactory()).Build()
}

var mtConstructOrder []string

type mtOrderedProviderA struct{}

func (p mtOrderedProviderA) NewProvider() Provider {
	mtConstructOrder = append(mtConstructOrder, "A")
	return p
}

type mtOrderedProviderB struct{ A mtOrderedProviderA }

func (p mtOrderedProviderB) NewProvider() Provider {
	mtConstructOrder = append(mtConstructOrder, "B")
	return p
}

var mtOnceConstructCount int

type mtOnceProvider struct{}

func (p mtOnceProvider) NewProvider() Provider {
	mtOnceConstructCount++
	return p
}

type mtGlobalSubProvider struct{ Source string }

func (p mtGlobalSubProvider) NewProvider() Provider { p.Source = "built"; return p }

type mtGlobalSubConsumer struct{ G mtGlobalSubProvider }

func (p mtGlobalSubConsumer) NewProvider() Provider { return p }

type mtDynMissingProvider struct{}

func (p mtDynMissingProvider) NewProvider() Provider { return p }

func mtModuleFactoryNeedingProvider(_ mtDynMissingProvider) *Module {
	return NewModuleBuilder().Build()
}

type mtLocalProvider struct{ Source string }

func (p mtLocalProvider) NewProvider() Provider { p.Source = "local"; return p }

type mtUnexportedFieldProvider struct {
	hidden mtLocalProvider //nolint:unused
}

func (p mtUnexportedFieldProvider) NewProvider() Provider { return p }

type mtDedupController struct{ common.HTTP }

func (c mtDedupController) NewController() Controller { return c }

func (c mtDedupController) READ_mtdedup() string { return "ok" }

type mtGlobalOnlyProvider struct{ Source string }

func (p mtGlobalOnlyProvider) NewProvider() Provider { return p }

type mtPassthroughValue struct{ Tag string }

type mtUnresolvedProvider struct{}

func (p mtUnresolvedProvider) NewProvider() Provider { return p }

type mtPriorityGuard struct {
	Local       mtLocalProvider
	GlobalOnly  mtGlobalOnlyProvider
	Log         log.Logger
	Pub         common.Publisher
	Passthrough mtPassthroughValue
}

var mtPriorityGuardSeen mtPriorityGuard

func (g mtPriorityGuard) CanActivate(_ *ctx.HTTPContext) bool {
	mtPriorityGuardSeen = g
	return true
}

type mtUnexportedFieldGuard struct {
	hidden mtLocalProvider //nolint:unused
}

func (g mtUnexportedFieldGuard) CanActivate(_ *ctx.HTTPContext) bool { return true }

type mtUnresolvedFieldGuard struct{ Missing mtUnresolvedProvider }

func (g mtUnresolvedFieldGuard) CanActivate(_ *ctx.HTTPContext) bool { return true }

type mtSimpleMiddleware struct{ P mtLocalProvider }

var mtSimpleMiddlewareSeen mtSimpleMiddleware

func (mw mtSimpleMiddleware) Use(_ *http.Request, _ http.ResponseWriter, next ctx.Next) {
	mtSimpleMiddlewareSeen = mw
	next()
}

type mtSimpleInterceptor struct{ P mtLocalProvider }

var mtSimpleInterceptorSeen mtSimpleInterceptor

func (ic mtSimpleInterceptor) Intercept(_ *ctx.HTTPContext, _ *aggregation.Aggregation) any {
	mtSimpleInterceptorSeen = ic
	return nil
}

var mtExFilterSeen createRecorder

type mtExFilterOneField struct{ P mtLocalProvider }

func (e mtExFilterOneField) Catch(_ *ctx.HTTPContext, _ *exception.Exception) {
	mtExFilterSeen.add(e.P.Source)
}

func mtPriorityRoot(controller Controller) (*Module, *Module) {
	global := NewModuleBuilder().Providers(mtGlobalOnlyProvider{Source: "global"}).Build()
	global.IsGlobal = true
	consumer := NewModuleBuilder().Providers(mtLocalProvider{}).Controllers(controller).Build()
	return NewModuleBuilder().Imports(global, consumer).Build(), global
}

func mtAssertPriorityChain(t *testing.T, seen mtPriorityGuard, app *App, label string) {
	t.Helper()
	if seen.Local.Source != "local" {
		t.Error(test.DiffMessage(seen.Local.Source, "local", label+": a field with a module provider is injected as constructed"))
	}
	if seen.GlobalOnly.Source != "global" {
		t.Error(test.DiffMessage(seen.GlobalOnly.Source, "global", label+": a field with no local provider falls back to the global providers"))
	}
	if seen.Log != app.Logger() {
		t.Error(test.DiffMessage(seen.Log, app.Logger(), label+": a log.Logger field falls back to the app's logger"))
	}
	if seen.Pub == nil {
		t.Error(test.DiffMessage(nil, "the app's publisher", label+": a common.Publisher field falls back to the app's publisher"))
	}
	if seen.Passthrough.Tag != "original" {
		t.Error(test.DiffMessage(seen.Passthrough.Tag, "original", label+": a non-Provider field passes through the bound instance's original value"))
	}
}

type mtPrefixedController struct{ common.HTTP }

func (c mtPrefixedController) NewController() Controller { return c }

func (c mtPrefixedController) READ_mtprefixtarget() string { return "ok" }

type mtGuardPriorityController struct {
	common.HTTP
	common.Guard
}

func (c mtGuardPriorityController) NewController() Controller {
	c.BindGuard(mtPriorityGuard{Passthrough: mtPassthroughValue{Tag: "original"}})
	return c
}

func (c mtGuardPriorityController) READ_mtguardpriority() string { return "ok" }

type mtGuardUnexportedController struct {
	common.HTTP
	common.Guard
}

func (c mtGuardUnexportedController) NewController() Controller {
	c.BindGuard(mtUnexportedFieldGuard{})
	return c
}

func (c mtGuardUnexportedController) READ_mtguardunexported() string { return "ok" }

type mtGuardUnresolvedController struct {
	common.HTTP
	common.Guard
}

func (c mtGuardUnresolvedController) NewController() Controller {
	c.BindGuard(mtUnresolvedFieldGuard{})
	return c
}

func (c mtGuardUnresolvedController) READ_mtguardunresolved() string { return "ok" }

type mtMiddlewareController struct {
	common.HTTP
	common.Middleware
}

func (c mtMiddlewareController) NewController() Controller {
	c.BindMiddleware(mtSimpleMiddleware{})
	return c
}

func (c mtMiddlewareController) READ_mtmiddleware() string { return "ok" }

type mtInterceptorController struct {
	common.HTTP
	common.Interceptor
}

func (c mtInterceptorController) NewController() Controller {
	c.BindInterceptor(mtSimpleInterceptor{})
	return c
}

func (c mtInterceptorController) READ_mtinterceptor() string { return "ok" }

type mtExFilterSingleController struct {
	common.HTTP
	common.ExceptionFilter
}

func (c mtExFilterSingleController) NewController() Controller {
	c.BindExceptionFilter(mtExFilterOneField{})
	return c
}

func (c mtExFilterSingleController) READ_mtexfiltersingle() string { panic("boom") }

type mtExFilterMultiController0 struct {
	common.HTTP
	common.ExceptionFilter
}

func (c mtExFilterMultiController0) NewController() Controller {
	c.BindExceptionFilter(mtExFilterOneField{})
	return c
}

func (c mtExFilterMultiController0) READ_mtexfilterbugc0() string { panic("boom") }

type mtExFilterMultiController1 struct {
	common.HTTP
	common.ExceptionFilter
}

func (c mtExFilterMultiController1) NewController() Controller {
	c.BindExceptionFilter(mtExFilterOneField{})
	return c
}

func (c mtExFilterMultiController1) READ_mtexfilterbugc1() string { panic("boom") }

func mtWSSubscribe(t *testing.T, app *App, topic string) WSPayload {
	t.Helper()
	server := httptest.NewServer(app)
	defer server.Close()

	conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	var connected WSPayload
	if err := websocket.JSON.Receive(conn, &connected); err != nil {
		t.Fatal(err)
	}
	if err := websocket.JSON.Send(conn, WSPayload{Type: TypeSubscribe, ID: "1", Topic: []string{topic}}); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var reply WSPayload
	if err := websocket.JSON.Receive(conn, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Type == TypePing {
		t.Fatal(test.DiffMessage(reply.Type, "a reply to the subscribe", "mtWSSubscribe got a keep-alive ping instead of a reply"))
	}
	return reply
}

func mtWSSend(t *testing.T, app *App, topic string) {
	t.Helper()
	server := httptest.NewServer(app)
	defer server.Close()

	conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var connected WSPayload
	if err := websocket.JSON.Receive(conn, &connected); err != nil {
		t.Fatal(err)
	}
	if err := websocket.JSON.Send(conn, WSPayload{Type: TypeSubscribe, ID: "1", Topic: []string{topic}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-mtWSExFilterSignal:
	case <-time.After(2 * time.Second):
		t.Fatal(test.DiffMessage("timeout", "the filter ran", "the WS exception filter must run for "+topic))
	}
}

func mtWSApp(t *testing.T, root *Module) *App {
	t.Helper()
	app := New()
	app.EnableWS(&WSOptions{Path: "/ws"})
	mustCreate(t, app, root)
	return app
}

type mtWSMainController struct{ common.WS }

func (c mtWSMainController) NewController() Controller { return c }

func (c mtWSMainController) SUBSCRIBE_mtwsmainhandler() string { return "ok" }

type mtPriorityGuardWS struct {
	Local       mtLocalProvider
	GlobalOnly  mtGlobalOnlyProvider
	Log         log.Logger
	Pub         common.Publisher
	Passthrough mtPassthroughValue
}

var mtPriorityGuardWSSeen mtPriorityGuardWS

func (g mtPriorityGuardWS) CanActivate(_ *ctx.WSContext) bool {
	mtPriorityGuardWSSeen = g
	return true
}

type mtWSGuardPriorityController struct {
	common.WS
	common.Guard
}

func (c mtWSGuardPriorityController) NewController() Controller {
	c.BindGuard(mtPriorityGuardWS{Passthrough: mtPassthroughValue{Tag: "original"}})
	return c
}

func (c mtWSGuardPriorityController) SUBSCRIBE_mtwsguardpriority() string { return "ok" }

type mtWSGuardUnexportedController struct {
	common.WS
	common.Guard
}

func (c mtWSGuardUnexportedController) NewController() Controller {
	c.BindGuard(mtUnexportedFieldGuard{})
	return c
}

func (c mtWSGuardUnexportedController) SUBSCRIBE_mtwsguardunexported() string { return "ok" }

type mtWSGuardUnresolvedController struct {
	common.WS
	common.Guard
}

func (c mtWSGuardUnresolvedController) NewController() Controller {
	c.BindGuard(mtUnresolvedFieldGuard{})
	return c
}

func (c mtWSGuardUnresolvedController) SUBSCRIBE_mtwsguardunresolved() string { return "ok" }

type mtSimpleInterceptorWS struct{ P mtLocalProvider }

var mtSimpleInterceptorWSSeen mtSimpleInterceptorWS

func (ic mtSimpleInterceptorWS) Intercept(_ *ctx.WSContext, _ *aggregation.Aggregation) any {
	mtSimpleInterceptorWSSeen = ic
	return nil
}

type mtWSInterceptorController struct {
	common.WS
	common.Interceptor
}

func (c mtWSInterceptorController) NewController() Controller {
	c.BindInterceptor(mtSimpleInterceptorWS{})
	return c
}

func (c mtWSInterceptorController) SUBSCRIBE_mtwsinterceptor() string { return "ok" }

var mtWSExFilterSeen createRecorder

type mtExFilterOneFieldWS struct{ P mtLocalProvider }

var mtWSExFilterSignal = make(chan struct{}, 8)

func (e mtExFilterOneFieldWS) Catch(_ *ctx.WSContext, _ *exception.Exception) {
	mtWSExFilterSeen.add(e.P.Source)
	mtWSExFilterSignal <- struct{}{}
}

type mtDenyGuardWS struct{}

func (mtDenyGuardWS) CanActivate(_ *ctx.WSContext) bool { return false }

type mtWSExFilterController0 struct {
	common.WS
	common.Guard
	common.ExceptionFilter
}

func (c mtWSExFilterController0) NewController() Controller {
	c.BindGuard(mtDenyGuardWS{})
	c.BindExceptionFilter(mtExFilterOneFieldWS{})
	return c
}

func (c mtWSExFilterController0) SUBSCRIBE_mtwsexfilterc0() string { return "ok" }

type mtWSExFilterController1 struct {
	common.WS
	common.Guard
	common.ExceptionFilter
}

func (c mtWSExFilterController1) NewController() Controller {
	c.BindGuard(mtDenyGuardWS{})
	c.BindExceptionFilter(mtExFilterOneFieldWS{})
	return c
}

func (c mtWSExFilterController1) SUBSCRIBE_mtwsexfilterc1() string { return "ok" }

type mtConcurrentProvider struct{}

func (p mtConcurrentProvider) NewProvider() Provider { return p }

type mtConcurrentController struct{ common.HTTP }

func (c mtConcurrentController) NewController() Controller { return c }

func (c mtConcurrentController) READ_mtconcurrentresource() string { return "ok" }

type publisherTestController struct {
	common.HTTP
	common.WS
}

func (c publisherTestController) NewController() Controller { return c }

func (c publisherTestController) READ_ping(publisher common.Publisher) ctx.Map {
	_ = publisher.Publish("chat.123")
	return ctx.Map{"message": "ok"}
}

func (c publisherTestController) SUBSCRIBE_chat_ANY() ctx.Map {
	return ctx.Map{"ok": true}
}

type publisherWSTestController struct {
	common.WS
}

func (c publisherWSTestController) NewController() Controller { return c }

func (c publisherWSTestController) SUBSCRIBE_relay_ANY(publisher common.Publisher) ctx.Map {
	_ = publisher.Publish("relay.out")
	return ctx.Map{"ok": true}
}

func newTestWSBare(t testing.TB) *WS {
	t.Helper()

	ws := NewWS(&WSOptions{logger: log.NewLog(nil)})
	ws.newCtx = func() *ctx.WSContext {
		c := ctx.NewWSContext()
		return c
	}
	ws.releaseCtx = func(c *ctx.WSContext) {
		ctx.WSReset(c)
	}
	ws.resolveAndCallHandler = func(_ string, f any, c *ctx.WSContext) []reflect.Value {
		args := make([]reflect.Value, reflect.TypeOf(f).NumIn())
		for i := range args {
			args[i] = reflect.ValueOf(c.WSPayload())
		}
		return reflect.ValueOf(f).Call(args)
	}
	ws.emitPostInterceptor = func(c *ctx.WSContext, name string, duration time.Duration) {}
	ws.emitComplete = func(c *ctx.WSContext, operation, target string) {}

	return ws
}

func newTestWS(t testing.TB, eventPatterns ...string) *WS {
	t.Helper()

	ws := newTestWSBare(t)
	for _, p := range eventPatterns {
		ws.eventMatcher.AddInjectableHandler(p, func() {})
	}

	return ws
}

func recvWSPayload(t testing.TB, conn *websocket.Conn) WSPayload {
	t.Helper()

	var p WSPayload
	if err := websocket.JSON.Receive(conn, &p); err != nil {
		t.Fatalf("receive: %v", err)
	}
	return p
}

//go:embed testdata/web
var rdEmbedded embed.FS

type rdX struct{ common.HTTP }

func (c rdX) NewController() Controller { return c }

func (c rdX) RENDER_x() {}

func rdApp(t *testing.T, opts *RenderOptions) (*App, *lcRecordingLogger) {
	t.Helper()
	logger := &lcRecordingLogger{}
	app := New()
	app.SetLogger(logger)
	app.EnableRender(opts)
	mustCreate(t, app, NewModuleBuilder().Controllers(rdX{}).Build())
	return app, logger
}

const (
	scopeTraceID  = "4bf92f3577b34da6a3ce929d0e0e4736"
	scopeParentID = "00f067aa0ba902b7"
	scopeHeader   = "00-" + scopeTraceID + "-" + scopeParentID + "-01"
)

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func serveWithHeader(app *App, method, target string, header http.Header) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	for k, values := range header {
		r.Header[k] = values
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

func expectScopeHeaders(t *testing.T, w *httptest.ResponseRecorder, desc string) (requestID, traceID string, flags byte) {
	t.Helper()
	ids := w.Header().Values(ctx.RequestID)
	parents := w.Header().Values("Traceparent")
	if len(ids) != 1 || len(parents) != 1 {
		t.Fatal(test.DiffMessage([]int{len(ids), len(parents)}, []int{1, 1}, "S7: "+desc+": one X-Request-Id and one Traceparent"))
	}
	requestID = ids[0]
	if len(requestID) != 16 || !isLowerHex(requestID) {
		t.Error(test.DiffMessage(requestID, "<16 lowercase hexadecimal characters>", "S7: "+desc+": X-Request-Id is a span ID"))
	}
	if len(parents[0]) != 55 {
		t.Error(test.DiffMessage(len(parents[0]), 55, "S7: "+desc+": the traceparent is 55 bytes"))
	}
	traceID, parentID, flags, ok := tracecontext.ParseTraceparent(parents[0])
	if !ok {
		t.Fatal(test.DiffMessage(parents[0], "<a valid traceparent>", "S7: "+desc+": the response traceparent is valid"))
	}
	if parentID != requestID {
		t.Error(test.DiffMessage(parentID, requestID, "S7: "+desc+": the response traceparent names our request as the span"))
	}
	return requestID, traceID, flags
}

const (
	srvWait       = 10 * time.Second
	srvStreamWait = 300 * time.Millisecond
	srvShort      = 100 * time.Millisecond
)

type srvStateT struct {
	mu         sync.Mutex
	hooks      []string
	rootExtra  func()
	childExtra func()

	entered   chan struct{}
	release   chan struct{}
	cancelled chan struct{}
	writeErr  chan error

	releaseOnce sync.Once
	cancelOnce  sync.Once
	handlerDone atomic.Bool
	rootSawDone atomic.Bool
}

var srvCur *srvStateT

func srvReset() {
	srvCur = &srvStateT{
		entered:   make(chan struct{}, 16),
		release:   make(chan struct{}),
		cancelled: make(chan struct{}),
		writeErr:  make(chan error, 1),
	}
}

func (s *srvStateT) open() {
	s.releaseOnce.Do(func() { close(s.release) })
}

func (s *srvStateT) hookNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.hooks...)
}

func (s *srvStateT) hook(name string) func() {
	return func() {
		s.mu.Lock()
		s.hooks = append(s.hooks, name)
		var extra func()
		if name == "root" {
			s.rootSawDone.Store(s.handlerDone.Load())
			extra = s.rootExtra
		} else {
			extra = s.childExtra
		}
		s.mu.Unlock()
		if extra != nil {
			extra()
		}
	}
}

var srvChildModule = func() *Module {
	m := NewModuleBuilder().Build()
	m.OnShutdown = srvCur.hook("child")
	return m
}

var srvRootModule = func() *Module {
	m := NewModuleBuilder().Imports(srvChildModule).Controllers(srvController{}).Build()
	m.OnShutdown = srvCur.hook("root")
	return m
}

type srvController struct{ common.HTTP }

func (ctl srvController) NewController() Controller { return ctl }

func (ctl srvController) READ_ok() string { return "ok" }

func (ctl srvController) READ_proto(c *ctx.HTTPContext) string { return c.Proto }

func (ctl srvController) READ_panic() string { panic("secret") }

func (ctl srvController) READ_block(c *ctx.HTTPContext) string {
	s := srvCur
	s.entered <- struct{}{}
	select {
	case <-s.release:
	case <-c.Request.Context().Done():
		s.cancelOnce.Do(func() { close(s.cancelled) })
		return "cancelled"
	}
	s.handlerDone.Store(true)
	return "finished"
}

func (ctl srvController) READ_stream(c *ctx.HTTPContext) string {
	_ = http.NewResponseController(c.ResponseWriter).SetWriteDeadline(time.Time{})
	time.Sleep(srvStreamWait)
	return "late"
}

func (ctl srvController) READ_nostream() string {
	time.Sleep(srvStreamWait)
	return "late"
}

func (ctl srvController) READ_flood(c *ctx.HTTPContext) string {
	s := srvCur
	chunk := make([]byte, 1<<20)
	for range 4096 {
		if _, err := c.ResponseWriter.Write(chunk); err != nil {
			s.writeErr <- err
			return ""
		}
	}
	s.writeErr <- nil
	return ""
}

func (ctl srvController) CREATE_upload(body ctx.Body) any {
	return ctx.Map{"got": body["k"]}
}

type srvLogEntry struct {
	level string
	msg   string
	args  []any
}

func (e srvLogEntry) field(key string) any {
	for i := 0; i+1 < len(e.args); i += 2 {
		if k, ok := e.args[i].(string); ok && k == key {
			return e.args[i+1]
		}
	}
	return nil
}

type srvLogger struct {
	mu      sync.Mutex
	entries []srvLogEntry
	seenBy  map[string]chan struct{}
}

func (l *srvLogger) chanLocked(msg string) chan struct{} {
	if l.seenBy == nil {
		l.seenBy = make(map[string]chan struct{})
	}
	ch, ok := l.seenBy[msg]
	if !ok {
		ch = make(chan struct{})
		l.seenBy[msg] = ch
	}
	return ch
}

func (l *srvLogger) record(level, msg string, args []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, srvLogEntry{level: level, msg: msg, args: append([]any(nil), args...)})
	ch := l.chanLocked(msg)
	select {
	case <-ch:
	default:
		close(ch)
	}
}

func (l *srvLogger) seen(msg string) <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.chanLocked(msg)
}

func (l *srvLogger) all() []srvLogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]srvLogEntry(nil), l.entries...)
}

func (l *srvLogger) named(msg string) []srvLogEntry {
	var out []srvLogEntry
	for _, e := range l.all() {
		if e.msg == msg {
			out = append(out, e)
		}
	}
	return out
}

func (l *srvLogger) Debug(msg string, args ...any) { l.record("debug", msg, args) }

func (l *srvLogger) Info(msg string, args ...any) { l.record("info", msg, args) }

func (l *srvLogger) Warn(msg string, args ...any) { l.record("warn", msg, args) }

func (l *srvLogger) Error(msg string, args ...any) { l.record("error", msg, args) }

func (l *srvLogger) Fatal(msg string, args ...any) { l.record("fatal", msg, args) }

type srvHarness struct {
	t       *testing.T
	app     *App
	root    *Module
	log     *srvLogger
	sigs    chan os.Signal
	regs    atomic.Int32
	stops   atomic.Int32
	ln      net.Listener
	done    chan error
	started bool
	waited  bool
}

func newSrv(t *testing.T, tweak ...func(*App)) *srvHarness {
	t.Helper()
	srvReset()

	h := &srvHarness{
		t:    t,
		log:  &srvLogger{},
		sigs: make(chan os.Signal, 4),
		done: make(chan error, 1),
	}
	h.app = New()
	h.app.SetLogger(h.log)
	h.app.notifySignals = func() (<-chan os.Signal, func()) {
		h.regs.Add(1)
		return h.sigs, func() { h.stops.Add(1) }
	}
	for _, f := range tweak {
		f(h.app)
	}
	h.root = srvRootModule()
	h.app.Create(h.root)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h.ln = ln
	t.Cleanup(h.cleanup)
	return h
}

func (h *srvHarness) cleanup() {
	srvCur.open()
	if h.started && !h.waited {
		go h.app.Stop()
		select {
		case err := <-h.done:
			_ = err
		case <-time.After(srvWait):
		}
	}
	_ = h.ln.Close()
}

func (h *srvHarness) addr() string { return h.ln.Addr().String() }

func (h *srvHarness) startWith(f func() error) {
	h.t.Helper()
	h.started = true
	go func() { h.done <- f() }()
	select {
	case <-h.log.seen("ServerListening"):
	case err := <-h.done:
		h.waited = true
		h.t.Fatalf("the serving call returned before it was serving: %v", err)
	case <-time.After(srvWait):
		h.t.Fatal("the server did not start")
	}
}

// startOnFreePort starts serve on a free port and returns it. Another process may take the port
// between srvFreePort and the bind, so a bind that fails with "address already in use" is retried
// on a new port.
func (h *srvHarness) startOnFreePort(serve func(port int) error) int {
	h.t.Helper()
	h.started = true
	for attempt := 1; ; attempt++ {
		port := srvFreePort(h.t)
		go func() { h.done <- serve(port) }()
		select {
		case <-h.log.seen("ServerListening"):
			return port
		case err := <-h.done:
			if attempt < 5 && err != nil && strings.Contains(err.Error(), "address already in use") {
				continue
			}
			h.waited = true
			h.t.Fatalf("the serving call returned before it was serving: %v", err)
		case <-time.After(srvWait):
			h.t.Fatal("the server did not start")
		}
		return port
	}
}

func (h *srvHarness) start(opts *ServerOptions) {
	h.t.Helper()
	h.startWith(func() error { return h.app.Serve(h.ln, opts) })
}

func (h *srvHarness) wait() error {
	h.t.Helper()
	select {
	case err := <-h.done:
		h.waited = true
		return err
	case <-time.After(srvWait):
		h.t.Fatal("the serving call did not return")
	}
	return nil
}

func srvWaitCh(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(srvWait):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func serveOnce(t *testing.T, app *App) {
	t.Helper()
	sigs := make(chan os.Signal, 1)
	sigs <- syscall.SIGTERM
	app.notifySignals = func() (<-chan os.Signal, func()) { return sigs, func() {} }
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	if err := app.Serve(ln, nil); err != nil {
		t.Fatal(err)
	}
}

func srvFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// srvHeldPort returns a port that stays bound by the test until it ends. A call that must fail
// before binding returns its own error on it; one that binds first gets "address already in use"
// instead. Unlike checking afterwards that the port is free, another process can't take it meanwhile.
func srvHeldPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l.Addr().(*net.TCPAddr).Port
}

func srvListenerOpen(ln net.Listener) bool {
	return ln.Close() == nil
}

type srvResp struct {
	status int
	proto  string
	body   string
	err    error
}

func srvClient() *http.Client {
	return &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: srvWait}
}

func srvFetch(c *http.Client, method, url, body string, chunked bool) srvResp {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
		if chunked {
			rd = io.NopCloser(rd)
		}
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		return srvResp{err: err}
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.Do(req)
	if err != nil {
		return srvResp{err: err}
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	return srvResp{status: res.StatusCode, proto: res.Proto, body: string(b), err: err}
}

func srvFetchAsync(c *http.Client, url string) <-chan srvResp {
	ch := make(chan srvResp, 1)
	go func() { ch <- srvFetch(c, http.MethodGet, url, "", false) }()
	return ch
}

func srvAwait(t *testing.T, ch <-chan srvResp) srvResp {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(srvWait):
		t.Fatal("timed out waiting for the response")
	}
	return srvResp{}
}

func srvDial(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, srvWait)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.SetDeadline(time.Now().Add(srvWait))
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func srvClosedByServer(c net.Conn) bool {
	_ = c.SetReadDeadline(time.Now().Add(srvWait))
	_, err := io.Copy(io.Discard, c)
	if err == nil {
		return true
	}
	var ne net.Error
	return !errors.As(err, &ne) || !ne.Timeout()
}

func srvWaitRefused(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(srvWait)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, srvWait)
		if err != nil {
			return
		}
		_ = c.Close()
		runtime.Gosched()
	}
	t.Fatal("the listener still accepts connections")
}

func srvCaptureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	var buf bytes.Buffer
	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(copied)
	}()
	f()
	_ = w.Close()
	os.Stdout = old
	<-copied
	_ = r.Close()
	return buf.String()
}

type srvCerts struct {
	cert    tls.Certificate
	pool    *x509.CertPool
	certPEM []byte
	keyPEM  []byte
}

func srvMakeCert(t *testing.T) srvCerts {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "ginject test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return srvCerts{
		cert:    tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key},
		pool:    pool,
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		keyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
	}
}

func srvTLSClient(pool *x509.CertPool, h2 bool) *http.Client {
	tr := &http.Transport{
		TLSClientConfig:   &tls.Config{RootCAs: pool},
		DisableKeepAlives: true,
	}
	if h2 {
		tr.ForceAttemptHTTP2 = true
	} else {
		tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	}
	return &http.Client{Transport: tr, Timeout: srvWait}
}

func srvH2CClient() *http.Client {
	var p http.Protocols
	p.SetUnencryptedHTTP2(true)
	return &http.Client{Transport: &http.Transport{Protocols: &p, DisableKeepAlives: true}, Timeout: srvWait}
}

type srvFailingListener struct {
	net.Listener
	err error
}

func (l srvFailingListener) Accept() (net.Conn, error) { return nil, l.err }

func traceRenderApp(t *testing.T, files map[string]string, listen bool, controllers ...Controller) (*App, *traceRecorder) {
	t.Helper()
	if len(controllers) == 0 {
		controllers = []Controller{pgPages{}}
		files = pgPageFileMap(files)
	}
	app := New()
	app.SetLogger(&lcRecordingLogger{})
	var rec *traceRecorder
	if listen {
		rec = newTraceRecorder(app)
	}
	app.EnableRender(&RenderOptions{FS: pgFiles(files), Dir: "."})
	mustCreate(t, app, NewModuleBuilder().Controllers(controllers...).Build())
	return app, rec
}

func traceIndex(events []trace.Event, stage string) int {
	for i, e := range events {
		if e.Stage == stage {
			return i
		}
	}
	return -1
}

func traceStages(events []trace.Event, stage string) []trace.Event {
	var out []trace.Event
	for _, e := range events {
		if e.Stage == stage {
			out = append(out, e)
		}
	}
	return out
}

type pgRowsOnly struct{ common.HTTP }

func (c pgRowsOnly) NewController() Controller { return c }

func (c pgRowsOnly) RENDER_rows() {}

type spanPipe struct{}

func (spanPipe) Transform(ctx.Query, common.ArgumentMetadata) any { return spanPipe{} }

type spanController struct {
	common.HTTP
	common.Middleware
	common.Guard
	common.Interceptor
	common.ExceptionFilter
}

func (ctl spanController) NewController() Controller {
	ctl.BindMiddleware(lcMiddleware{Name: "m1"})
	ctl.BindGuard(lcGuard{Name: "m1"})
	ctl.BindInterceptor(lcInterceptor{Name: "m1"})
	ctl.BindExceptionFilter(lcFilter{Name: "mf1"})
	return ctl
}

func (ctl spanController) READ_tree(spanPipe) string { return "tree" }

func (ctl spanController) READ_boom(*ctx.HTTPContext) any {
	panic(exception.BadRequestException("boom"))
}

func newSpanApp(t *testing.T) (*App, *traceRecorder) {
	t.Helper()
	lcReset(t)
	app := New()
	app.SetLogger(&mockLogger{})
	rec := newTraceRecorder(app)
	app.BindGlobalMiddlewares(lcMiddleware{Name: "g1"})
	app.BindGlobalGuards(lcGuard{Name: "g1"})
	app.BindGlobalInterceptors(lcInterceptor{Name: "g1"})
	app.BindGlobalExceptionFilters(lcFilter{Name: "gf1"})
	app.Create(NewModuleBuilder().Controllers(spanController{}).Build())
	return app, rec
}

var stageRank = map[string]int{
	trace.StageMiddleware:      0,
	trace.StageGuard:           1,
	trace.StagePreInterceptor:  2,
	trace.StagePipe:            3,
	trace.StageHandler:         4,
	trace.StagePostInterceptor: 5,
	trace.StageExceptionFilter: 6,
	trace.StageComplete:        7,
}

func expectTree(t *testing.T, events []trace.Event, transport, id, traceID, rootParent string, wantStages ...string) {
	t.Helper()
	if len(events) < 2 {
		t.Fatal(test.DiffMessage(len(events), "> 1", "E4: a unit emits its stages and its root"))
	}
	root := events[len(events)-1]
	if root.Stage != trace.StageComplete {
		t.Fatal(test.DiffMessage(root.Stage, trace.StageComplete, "E4: the root is emitted last"))
	}
	if root.ID != id || root.SpanID != id || root.ParentID != rootParent {
		t.Error(test.DiffMessage([]string{root.ID, root.SpanID, root.ParentID}, []string{id, id, rootParent}, "E2: complete is the root span, SpanID == ID, ParentID is the scope's parent"))
	}
	spans := map[string]struct{}{}
	previous := -1
	seen := map[string]bool{}
	for _, e := range events {
		if e.Transport != transport {
			t.Error(test.DiffMessage(e.Transport, transport, "E1: Transport of "+e.Stage))
		}
		if e.ID != id || e.TraceID != traceID {
			t.Error(test.DiffMessage([]string{e.ID, e.TraceID}, []string{id, traceID}, "E1: every event carries the unit's ID and the trace ID ("+e.Stage+")"))
		}
		if len(e.SpanID) != 16 || !isLowerHex(e.SpanID) {
			t.Error(test.DiffMessage(e.SpanID, "<16 hex characters>", "E3: SpanID of "+e.Stage))
		}
		if _, dup := spans[e.SpanID]; dup {
			t.Error(test.DiffMessage(e.SpanID, "<unique within the unit>", "E4: all SpanIDs of one unit differ ("+e.Stage+")"))
		}
		spans[e.SpanID] = struct{}{}
		if e.Stage != trace.StageComplete {
			if e.ParentID != id || e.SpanID == id {
				t.Error(test.DiffMessage([]string{e.SpanID, e.ParentID}, []string{"<fresh>", id}, "E3: "+e.Stage+" is a child of the unit"))
			}
		}
		rank, known := stageRank[e.Stage]
		if !known || rank < previous {
			t.Error(test.DiffMessage(e.Stage, "<a stage not before an earlier one>", "E4: events arrive in lifecycle order"))
		}
		previous = rank
		seen[e.Stage] = true
	}
	for _, stage := range wantStages {
		if !seen[stage] {
			t.Error(test.DiffMessage(seen, "<has "+stage+">", "E3: the tree holds a "+stage+" span"))
		}
	}
}

type countingReader struct {
	inner io.Reader
	bytes atomic.Int64
}

func (r *countingReader) Read(b []byte) (int, error) {
	r.bytes.Add(int64(len(b)))
	return r.inner.Read(b)
}

func countRandomBytes(t *testing.T, fn func()) int64 {
	t.Helper()
	counter := &countingReader{inner: rand.Reader}
	previous := rand.Reader
	rand.Reader = counter
	defer func() { rand.Reader = previous }()
	fn()
	return counter.bytes.Load()
}

type traceRecorder struct {
	mu     sync.Mutex
	events []trace.Event
	ch     chan trace.Event
}

func newTraceRecorder(app *App) *traceRecorder {
	r := &traceRecorder{ch: make(chan trace.Event, 256)}
	app.tracer.Listen(func(e trace.Event) {
		r.mu.Lock()
		r.events = append(r.events, e)
		r.mu.Unlock()
		select {
		case r.ch <- e:
		default:
		}
	})
	return r
}

func (r *traceRecorder) all() []trace.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]trace.Event(nil), r.events...)
}

func (r *traceRecorder) stages() map[string]bool {
	seen := map[string]bool{}
	for _, e := range r.all() {
		seen[e.Stage] = true
	}
	return seen
}

func (r *traceRecorder) waitComplete(t *testing.T, target string) trace.Event {
	t.Helper()
	for {
		select {
		case e := <-r.ch:
			if e.Stage == trace.StageComplete && e.Target == target {
				return e
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("no complete event for %q", target)
		}
	}
}

type traceWSAllGuard struct{}

func (traceWSAllGuard) CanActivate(*ctx.WSContext) bool { return true }

type traceWSAllInterceptor struct{}

func (traceWSAllInterceptor) Intercept(_ *ctx.WSContext, agg *aggregation.Aggregation) any {
	return agg.Pipe()
}

type traceWSAllPipe struct{}

func (traceWSAllPipe) Transform(ctx.WSPayload, common.ArgumentMetadata) any { return traceWSAllPipe{} }

type traceWSAllController struct {
	common.WS
	common.Guard
	common.Interceptor
}

func (c traceWSAllController) NewController() Controller {
	c.BindGuard(traceWSAllGuard{})
	c.BindInterceptor(traceWSAllInterceptor{})
	return c
}

func (c traceWSAllController) SUBSCRIBE_traced(traceWSAllPipe) string { return "ok" }

func (c traceWSAllController) SUBSCRIBE_boom() string {
	panic(exception.BadRequestException("boom"))
}

type wsAllController struct{ common.WS }

func (c wsAllController) NewController() Controller { return c }

func (c wsAllController) SUBSCRIBE_chat_ALL() string { return "all" }

type wsAllNotLastController struct{ common.WS }

func (c wsAllNotLastController) NewController() Controller { return c }

func (c wsAllNotLastController) SUBSCRIBE_chat_ALL_x() string { return "x" }

type httpAnyAfterParamController struct{ common.HTTP }

func (c httpAnyAfterParamController) NewController() Controller { return c }

func (c httpAnyAfterParamController) READ_files_BY_name_ANY(p ctx.Param) string {
	return "name=" + p.Get("name")
}

func benchHandshakeContext() *ctx.HTTPContext {
	c := ctx.NewHTTPContext()
	c.Request = httptest.NewRequest(http.MethodGet, "/ws", nil)
	c.ResponseWriter = httptest.NewRecorder()
	return c
}

func newTestWSConnPair(t testing.TB) (server *websocket.Conn, client *websocket.Conn, cleanup func()) {
	t.Helper()

	serverConnCh := make(chan *websocket.Conn, 1)
	handlerDone := make(chan struct{})

	httpServer := httptest.NewServer(websocket.Handler(func(c *websocket.Conn) {
		serverConnCh <- c
		<-handlerDone
	}))

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	clientConn, err := websocket.Dial(wsURL, "", httpServer.URL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	serverConn := <-serverConnCh

	return serverConn, clientConn, func() {
		close(handlerDone)
		_ = clientConn.Close()
		httpServer.Close()
	}
}

func subscribeAndDrainAck(t testing.TB, ws *WS, conn *WSConnection, clientConn *websocket.Conn, topic string) {
	t.Helper()

	handleSubscribe(conn, ws, WSPayload{ID: "sub-" + topic, Type: TypeSubscribe, Topic: []string{topic}})
	if p := recvWSPayload(t, clientConn); p.Type != TypeAck {
		t.Fatalf("expected subscribe ack for topic %q, got %v", topic, p.Type)
	}
}

func expectNoFrame(t testing.TB, clientConn *websocket.Conn) {
	t.Helper()

	frame := make(chan WSPayload, 1)
	go func() {
		var p WSPayload
		if err := websocket.JSON.Receive(clientConn, &p); err == nil {
			frame <- p
		}
	}()

	select {
	case p := <-frame:
		t.Error(test.DiffMessage(p, nil, "expected no frame delivered, but one arrived"))
	case <-time.After(150 * time.Millisecond):
	}
}

type wsScopeFixture struct {
	mu   sync.Mutex
	hook func(*ctx.WSContext)
}

var wsScope wsScopeFixture

func wsScopeSetHook(fn func(*ctx.WSContext)) {
	wsScope.mu.Lock()
	defer wsScope.mu.Unlock()
	wsScope.hook = fn
}

func wsScopeCall(c *ctx.WSContext) {
	wsScope.mu.Lock()
	fn := wsScope.hook
	wsScope.mu.Unlock()
	if fn != nil {
		fn(c)
	}
}

type wsScopeController struct{ common.WS }

func (c wsScopeController) NewController() Controller { return c }

func (c wsScopeController) SUBSCRIBE_a(wc *ctx.WSContext) string {
	wsScopeCall(wc)
	return "a"
}

func (c wsScopeController) SUBSCRIBE_b(wc *ctx.WSContext) string {
	wsScopeCall(wc)
	return "b"
}

type wsScopeEnv struct {
	app    *App
	server *httptest.Server
	rec    *traceRecorder
}

func newWSScopeEnv(t *testing.T, middlewares ...common.MiddlewareFn) *wsScopeEnv {
	t.Helper()
	lcReset(t)
	wsScopeSetHook(nil)
	t.Cleanup(func() { wsScopeSetHook(nil) })
	app := New()
	app.SetLogger(&mockLogger{})
	rec := newTraceRecorder(app)
	app.EnableWS(&WSOptions{Path: "/ws"}, middlewares...)
	app.Create(NewModuleBuilder().Controllers(wsScopeController{}, traceWSAllController{}).Build())
	server := httptest.NewServer(app)
	t.Cleanup(server.Close)
	return &wsScopeEnv{app: app, server: server, rec: rec}
}

func (e *wsScopeEnv) dial(t *testing.T, header map[string]string) (*websocket.Conn, WSPayload) {
	t.Helper()
	config, err := websocket.NewConfig("ws"+strings.TrimPrefix(e.server.URL, "http")+"/ws", e.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		config.Header.Set(k, v)
	}
	conn, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	var connected WSPayload
	if err := websocket.JSON.Receive(conn, &connected); err != nil {
		t.Fatal(err)
	}
	if connected.Type != TypeConnected {
		t.Fatal(test.DiffMessage(connected.Type, TypeConnected, "fixture: the first frame is connected"))
	}
	return conn, connected
}

func sendAndAwaitAck(conn *websocket.Conn, p WSPayload) error {
	if err := websocket.JSON.Send(conn, p); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
	for {
		var reply WSPayload
		if err := websocket.JSON.Receive(conn, &reply); err != nil {
			return fmt.Errorf("waiting for the ack of %q: %w", p.ID, err)
		}
		if reply.ID == p.ID && reply.Type == TypeAck {
			return nil
		}
		if reply.ID == p.ID && reply.Type == TypeError {
			return fmt.Errorf("the frame %q was refused: %v", p.ID, reply.Message)
		}
	}
}

func (e *wsScopeEnv) send(t *testing.T, conn *websocket.Conn, p WSPayload) {
	t.Helper()
	if err := sendAndAwaitAck(conn, p); err != nil {
		t.Fatal(err)
	}
}

func (e *wsScopeEnv) waitCompletes(t *testing.T, target string, n int) []trace.Event {
	t.Helper()
	var out []trace.Event
	timeout := time.After(10 * time.Second)
	for len(out) < n {
		select {
		case ev := <-e.rec.ch:
			if ev.Stage == trace.StageComplete && ev.Target == target {
				out = append(out, ev)
			}
		case <-timeout:
			t.Fatalf("got %d of %d complete events for %q", len(out), n, target)
		}
	}
	return out
}

func rawUpgrade(t *testing.T, base string, header map[string]string) (*http.Response, func()) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Origin", base)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	transport := &http.Transport{DisableKeepAlives: true}
	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp, func() {
		_ = resp.Body.Close()
		transport.CloseIdleConnections()
	}
}

type wsHandshakeTraceMiddleware struct{}

func (wsHandshakeTraceMiddleware) Use(_ *http.Request, _ http.ResponseWriter, next ctx.Next) {
	next()
}

type wsHandshakeTraceMiddlewareB struct{}

func (wsHandshakeTraceMiddlewareB) Use(_ *http.Request, _ http.ResponseWriter, next ctx.Next) {
	next()
}

type lfSQL struct{ Ready bool }

func (p lfSQL) NewProvider() Provider { p.Ready = true; return p }

type lfRedis struct{ Ready bool }

func (p lfRedis) NewProvider() Provider { p.Ready = true; return p }

type lfRepo struct{ ID string }

func (p lfRepo) NewProvider() Provider { p.ID = "built"; return p }

type lfCache struct{ ID string }

func (p lfCache) NewProvider() Provider { p.ID = "built"; return p }

type lfUserSvc struct{ DB lfSQL }

func (p lfUserSvc) NewProvider() Provider { return p }

type lfBoomProvider struct{}

func (p lfBoomProvider) NewProvider() Provider { panic("lf-provider-boom") }

type lfBoomSvc struct{ DB lfSQL }

func (p lfBoomSvc) NewProvider() Provider { panic("lf-svc-boom") }

type lfMissingProv struct{}

func (p lfMissingProv) NewProvider() Provider { return p }

type lfMissing struct{}
type lfMissingA struct{}
type lfMissingB struct{}

type lfPlainCtl struct{ common.HTTP }

func (c lfPlainCtl) NewController() Controller { c.Prefix("lfplain"); return c }

func (c lfPlainCtl) READ_hello() string { return "plain" }

type lfSQLCtl struct {
	common.HTTP
	DB lfSQL
}

func (c lfSQLCtl) NewController() Controller { c.Prefix("lfsql"); return c }

func (c lfSQLCtl) READ_hello() string {
	if c.DB.Ready {
		return "ok"
	}
	return "zero"
}

type lfUserCtl struct {
	common.HTTP
	Svc lfUserSvc
}

func (c lfUserCtl) NewController() Controller { c.Prefix("lfuser"); return c }

func (c lfUserCtl) READ_hello() string { return "user" }

type lfBookingCtl struct {
	common.HTTP
	Users lfUserSvc
}

func (c lfBookingCtl) NewController() Controller { c.Prefix("lfbooking"); return c }

func (c lfBookingCtl) READ_hello() string { return "booking" }

type lfRedisCtl struct {
	common.HTTP
	R lfRedis
}

func (c lfRedisCtl) NewController() Controller { c.Prefix("lfredis"); return c }

func (c lfRedisCtl) READ_hello() string { return "redis" }

type lfSQLGuard struct{ DB lfSQL }

func (g lfSQLGuard) CanActivate(*ctx.HTTPContext) bool { return true }

type lfGuardCtl struct {
	common.HTTP
	common.Guard
}

func (c lfGuardCtl) NewController() Controller {
	c.Prefix("lfguard")
	c.BindGuard(lfSQLGuard{})
	return c
}

func (c lfGuardCtl) READ_hello() string { return "guard" }

type lfSQLPipe struct{ DB lfSQL }

func (p lfSQLPipe) Transform(ctx.Query, common.ArgumentMetadata) any { return p }

type lfPipeCtl struct{ common.HTTP }

func (c lfPipeCtl) NewController() Controller { c.Prefix("lfpipe"); return c }

func (c lfPipeCtl) READ_hello(p lfSQLPipe) string { return "pipe" }

type lfPanicCtl struct {
	common.HTTP
	DB lfSQL
}

func (c lfPanicCtl) NewController() Controller { panic("lf-newcontroller-boom") }

func (c lfPanicCtl) READ_hello() string { return "never" }

type lfDupOpenCtl struct{ common.HTTP }

func (c lfDupOpenCtl) NewController() Controller { c.Prefix("lfdup"); return c }

func (c lfDupOpenCtl) READ_hello() string { return "open" }

type lfDupGatedCtl struct {
	common.HTTP
	DB lfSQL
}

func (c lfDupGatedCtl) NewController() Controller { c.Prefix("lfdup"); return c }

func (c lfDupGatedCtl) READ_hello() string { return "gated" }

var lfChangingCalls atomic.Int64

type lfChangingCtl struct{ common.HTTP }

func (c lfChangingCtl) NewController() Controller {
	if lfChangingCalls.Add(1) == 1 {
		c.Prefix("lfchg1")
	} else {
		c.Prefix("lfchg2")
	}
	return c
}

func (c lfChangingCtl) READ_hello() string { return "chg" }

type lfPageCtl struct {
	common.HTTP
	DB lfSQL
}

func (c lfPageCtl) NewController() Controller { return c }

func (c lfPageCtl) RENDER_gated(h *ctx.HTTPContext) {}

var lfLayerHits atomic.Int64

type lfCountGuard struct{}

func (lfCountGuard) CanActivate(*ctx.HTTPContext) bool { lfLayerHits.Add(1); return true }

type lfCountFilter struct{}

func (lfCountFilter) Catch(*ctx.HTTPContext, *exception.Exception) { lfLayerHits.Add(1) }

type lfLayeredCtl struct {
	common.HTTP
	common.Guard
	common.ExceptionFilter
	DB lfSQL
}

func (c lfLayeredCtl) NewController() Controller {
	c.Prefix("lflayered")
	c.BindGuard(lfCountGuard{})
	c.BindExceptionFilter(lfCountFilter{})
	return c
}

func (c lfLayeredCtl) READ_hello() string { lfLayerHits.Add(100); return "layered" }

type lfCountMW struct{ Hits *atomic.Int64 }

func (m lfCountMW) Use(_ *http.Request, _ http.ResponseWriter, next ctx.Next) {
	m.Hits.Add(1)
	next()
}

type lfRefuseMW struct{}

func (lfRefuseMW) Use(_ *http.Request, w http.ResponseWriter, _ ctx.Next) {
	w.WriteHeader(http.StatusUnauthorized)
}

type lfWSGate struct {
	common.WS
	DB lfSQL
}

func (c lfWSGate) NewController() Controller { return c }

func (c lfWSGate) SUBSCRIBE_lfgated() string { lfWSGateCalls.Add(1); return "gated" }

var lfWSGateCalls atomic.Int64

type lfWSOpen struct{ common.WS }

func (c lfWSOpen) NewController() Controller { return c }

func (c lfWSOpen) SUBSCRIBE_lfopen() string { return "open" }

type lfEvents struct {
	mu   sync.Mutex
	list []string
}

func (e *lfEvents) add(s string) {
	e.mu.Lock()
	e.list = append(e.list, s)
	e.mu.Unlock()
}

func (e *lfEvents) all() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.list)
}

func lfNamed(name string, b *ModuleBuilder) *Module {
	m := b.Build()
	m.Name = name
	return m
}

func lfWait(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for " + what)
	}
}

func lfCreate(t *testing.T, app *App, root *Module) {
	t.Helper()
	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		app.Create(root)
	}()
	select {
	case r := <-done:
		if r != nil {
			t.Fatal(test.DiffMessage(fmt.Sprint(r), "no panic", "Create succeeds"))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Create did not return")
	}
}

func lfCreatePanic(t *testing.T, app *App, root *Module) string {
	t.Helper()
	text, panicked := createPanicText(func() { app.Create(root) })
	if !panicked {
		t.Fatal(test.DiffMessage("no panic", "a panic", "Create panics"))
	}
	return text
}

func lfRelease(t *testing.T, app *App, releases ...chan struct{}) {
	t.Helper()
	t.Cleanup(func() {
		for _, r := range releases {
			select {
			case <-r:
			default:
				close(r)
			}
		}
		app.Stop()
	})
}

func lfStatus(app *App, m *Module) moduleStatus { return app.container.status(m) }

func lfCode(app *App, path string) int { return createGet(app, path).Code }

func lfLogger(app *App) *lcRecordingLogger {
	l := &lcRecordingLogger{}
	app.SetLogger(l)
	return l
}

type lfListenLogger struct {
	*lcRecordingLogger
	listening chan struct{}
	once      sync.Once
}

func newLFListenLogger() *lfListenLogger {
	return &lfListenLogger{lcRecordingLogger: &lcRecordingLogger{}, listening: make(chan struct{})}
}

func (l *lfListenLogger) Info(msg string, args ...any) {
	l.record("info", msg, args)
	if msg == "ServerListening" {
		l.once.Do(func() { close(l.listening) })
	}
}

func lfServe(t *testing.T, app *App, timeout time.Duration, logger *lfListenLogger) chan error {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	done := make(chan error, 1)
	go func() { done <- app.Serve(ln, &ServerOptions{ShutdownTimeout: timeout}) }()
	lfWait(t, logger.listening, "ServerListening")
	return done
}

func lfCause(e lcLogEntry) string { return fmt.Sprint(e.field("cause")) }

func lfFailedFor(l *lcRecordingLogger, module string) []lcLogEntry {
	var out []lcLogEntry
	for _, e := range l.named("ModuleFailed") {
		if fmt.Sprint(e.field("module")) == module {
			out = append(out, e)
		}
	}
	return out
}

type lfAlphaSvc struct{}

func (p lfAlphaSvc) NewProvider() Provider { return p }

type lfBetaSvc struct{}

func (p lfBetaSvc) NewProvider() Provider { return p }

type lfNeedsBetaCtl struct {
	common.HTTP
	B lfBetaSvc
}

func (c lfNeedsBetaCtl) NewController() Controller { c.Prefix("lfneedsbeta"); return c }

func (c lfNeedsBetaCtl) READ_hello() string { return "beta" }

type lfNeedsAlphaCtl struct {
	common.HTTP
	A lfAlphaSvc
}

func (c lfNeedsAlphaCtl) NewController() Controller { c.Prefix("lfneedsalpha"); return c }

func (c lfNeedsAlphaCtl) READ_hello() string { return "alpha" }

type lfSubmitOnlyCtl struct {
	common.HTTP
	DB lfSQL
}

func (c lfSubmitOnlyCtl) NewController() Controller { return c }

func (c lfSubmitOnlyCtl) SUBMIT_x() {}

const lfWant503JSON = `{"code":503,"error":"Service Unavailable","message":"Service Unavailable"}`

func lfGateApp(t *testing.T, files map[string]string, setup func(*App)) (*App, *Module, *lcRecordingLogger) {
	t.Helper()
	logger := &lcRecordingLogger{}
	app := New()
	app.SetLogger(logger)
	if files != nil {
		app.EnableRender(&RenderOptions{FS: pgFiles(files), Dir: "."})
	}
	if setup != nil {
		setup(app)
	}
	sql := lfNamed("sql", NewModuleBuilder().Providers(lfSQL{}))
	release := lfHold(sql)
	b := NewModuleBuilder().AsyncImports(sql).Controllers(lfSQLCtl{}, lfLayeredCtl{})
	if files != nil {
		b.Controllers(lfPageCtl{})
	}
	gated := lfNamed("gated", b)
	root := lfNamed("root", NewModuleBuilder().Imports(gated).Controllers(lfPlainCtl{}))
	mustCreate(t, app, root)
	lfRelease(t, app, release)
	return app, sql, logger
}

func lfWSApp(t *testing.T) (*App, *Module, *websocket.Conn) {
	t.Helper()
	lfWSGateCalls.Store(0)
	sql := lfNamed("sql", NewModuleBuilder().Providers(lfSQL{}))
	release := lfHold(sql)
	gated := lfNamed("gated", NewModuleBuilder().AsyncImports(sql).Controllers(lfWSGate{}))
	root := lfNamed("root", NewModuleBuilder().Imports(gated).Controllers(lfWSOpen{}))

	app := New()
	lfLogger(app)
	app.EnableWS(&WSOptions{Path: "/ws"})
	mustCreate(t, app, root)
	lfRelease(t, app, release)

	server := httptest.NewServer(app)
	t.Cleanup(server.Close)
	conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if p := recvWSPayload(t, conn); p.Type != TypeConnected {
		t.Fatalf("expected connected, got %v", p.Type)
	}
	return app, sql, conn
}

func lfWSSend(t *testing.T, conn *websocket.Conn, typ WSPayloadType, id, topic string) WSPayload {
	t.Helper()
	if err := websocket.JSON.Send(conn, WSPayload{Type: typ, ID: id, Topic: []string{topic}, Message: "hi"}); err != nil {
		t.Fatal(err)
	}
	return recvWSPayload(t, conn)
}

func lfWSErrorCode(p WSPayload) (code int, message string) {
	m, _ := p.Message.(map[string]any)
	c, _ := m["code"].(float64)
	s, _ := m["message"].(string)
	return int(c), s
}

func lfHold(ms ...*Module) chan struct{} {
	release := make(chan struct{})
	for _, m := range ms {
		m.OnInit = func() { <-release }
	}
	return release
}

type lfWatchLog struct {
	*lcRecordingLogger
	mu   sync.Mutex
	subs []chan struct{}
}

func newLFWatchLog() *lfWatchLog { return &lfWatchLog{lcRecordingLogger: &lcRecordingLogger{}} }

func (l *lfWatchLog) notify() {
	l.mu.Lock()
	for _, s := range l.subs {
		select {
		case s <- struct{}{}:
		default:
		}
	}
	l.mu.Unlock()
}

func (l *lfWatchLog) Debug(msg string, args ...any) { l.record("debug", msg, args); l.notify() }
func (l *lfWatchLog) Info(msg string, args ...any)  { l.record("info", msg, args); l.notify() }
func (l *lfWatchLog) Warn(msg string, args ...any)  { l.record("warn", msg, args); l.notify() }
func (l *lfWatchLog) Error(msg string, args ...any) { l.record("error", msg, args); l.notify() }
func (l *lfWatchLog) Fatal(msg string, args ...any) { l.record("fatal", msg, args); l.notify() }

func (l *lfWatchLog) waitCount(t *testing.T, msg string, n int) {
	t.Helper()
	sub := make(chan struct{}, 1)
	l.mu.Lock()
	l.subs = append(l.subs, sub)
	l.mu.Unlock()
	for len(l.named(msg)) < n {
		select {
		case <-sub:
		case <-time.After(10 * time.Second):
			t.Fatalf("timed out waiting for %d %s lines, have %d", n, msg, len(l.named(msg)))
		}
	}
}

func lfStop(t *testing.T, app *App) {
	t.Helper()
	done := make(chan struct{})
	go func() { app.Stop(); close(done) }()
	lfWait(t, done, "Stop to return")
}

type lfOtherSvc struct{ DB lfSQL }

func (p lfOtherSvc) NewProvider() Provider { return p }

type lfCacheCtl struct {
	common.HTTP
	C lfCache
}

func (c lfCacheCtl) NewController() Controller { c.Prefix("lfcache"); return c }

func (c lfCacheCtl) READ_hello() string { return "cache" }

type lfNeedsBoom struct{ B lfBoomProvider }

func (p lfNeedsBoom) NewProvider() Provider { return p }

type lfRecProvider struct{ Ev *lfEvents }

func (p lfRecProvider) NewProvider() Provider { p.Ev.add("provider"); return p }

type lfRecCtl struct {
	common.HTTP
	Ev *lfEvents
}

func (c lfRecCtl) NewController() Controller { c.Ev.add("controller"); c.Prefix("lfrec"); return c }

func (c lfRecCtl) READ_hello() string { return "rec" }

type lfOrderGuard struct {
	Tag string
	Log *lfEvents
}

func (g lfOrderGuard) CanActivate(*ctx.HTTPContext) bool { g.Log.add(g.Tag); return true }

type lfOrderMW struct {
	Tag string
	Log *lfEvents
}

func (m lfOrderMW) Use(_ *http.Request, _ http.ResponseWriter, next ctx.Next) {
	m.Log.add(m.Tag)
	next()
}

type lfOrderCtl struct {
	common.HTTP
	common.Guard
	DB  lfSQL
	Log *lfEvents
}

func (c lfOrderCtl) NewController() Controller {
	c.Prefix("lforder")
	c.BindGuard(lfOrderGuard{Tag: "ctl-guard", Log: c.Log})
	return c
}

func (c lfOrderCtl) READ_hello() string { c.Log.add("handler"); return "order" }

type lfMissingCtl struct {
	common.HTTP
	P lfMissingProv
}

func (c lfMissingCtl) NewController() Controller { return c }

func (c lfMissingCtl) READ_hello() string { return "missing" }

type lfAsyncWS struct {
	release func()
	settled <-chan struct{}
}

func lfWSAsyncApp(t *testing.T) (*App, *lfAsyncWS, *websocket.Conn) {
	t.Helper()
	lfWSGateCalls.Store(0)
	sql := lfNamed("sql", NewModuleBuilder().Providers(lfSQL{}))
	hold := lfHold(sql)
	gated := lfNamed("gated", NewModuleBuilder().AsyncImports(sql).Controllers(lfWSGate{}))
	root := lfNamed("root", NewModuleBuilder().Imports(gated).Controllers(lfWSOpen{}))

	app := New()
	lfLogger(app)
	app.EnableWS(&WSOptions{Path: "/ws"})
	mustCreate(t, app, root)
	lfRelease(t, app, hold)

	server := httptest.NewServer(app)
	t.Cleanup(server.Close)
	conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if p := recvWSPayload(t, conn); p.Type != TypeConnected {
		t.Fatalf("expected connected, got %v", p.Type)
	}
	return app, &lfAsyncWS{release: func() { close(hold) }, settled: app.container.settled(gated)}, conn
}
