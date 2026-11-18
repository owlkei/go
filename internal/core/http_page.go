package core

import (
	"embed"
	"errors"
	"io/fs"
	stdHTTP "net/http"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/devtool"
	"github.com/dangduoc08/ginject/internal/render"
	"github.com/dangduoc08/ginject/internal/trace"
	"github.com/dangduoc08/ginject/internal/tracecontext"
)

type RenderOptions struct {
	FS       fs.FS
	Dir      string
	IsReload bool
}

const defaultRenderDir = "web"

const (
	textHTMLUTF8         = "text/html; charset=utf-8"
	errorPageTraceName   = "error"
	builtInPageTraceName = "error (built-in)"
	errorTemplateName    = "error.html"
	builtInTemplateName  = "built-in"
)

var (
	textHTMLValue = []string{textHTMLUTF8}

	errNotView = errors.New("render: the result after the interceptors is not a ginject.View")
)

func (app *App) initRender() {
	if app.http.render == nil {
		return
	}
	if err := app.http.render.Load(); err != nil {
		panic(color.FmtRed("invalid render: %v", err))
	}
	if app.http.render.IsReload() {
		app.logger.Warn("RenderReloadOn", "dir", app.renderDir, "note", "templates and assets are read from disk on every render; turn IsReload off in production")
	}
}

func renderFS(opts *RenderOptions) (fs.FS, string) {
	dir := opts.Dir
	if dir == "" {
		dir = defaultRenderDir
	}
	if _, embedded := opts.FS.(embed.FS); embedded && opts.IsReload {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			panic(color.FmtRed("invalid render: IsReload reads %q from disk, but there is no such directory under the working directory", dir))
		}
		return os.DirFS(dir), dir
	}
	if dir == "." {
		return opts.FS, dir
	}
	info, err := fs.Stat(opts.FS, dir)
	if err != nil || !info.IsDir() {
		panic(color.FmtRed("invalid render: RenderOptions.FS has no %q directory: embed it with //go:embed %s, or set Dir", dir, dir))
	}
	sub, err := fs.Sub(opts.FS, dir)
	if err != nil {
		panic(color.FmtRed("invalid render: %v", err))
	}
	return sub, dir
}

func (app *App) renderSnapshot() *devtool.Render {
	engine := app.http.render
	if engine == nil {
		return nil
	}
	snap := engine.Snapshot()
	out := &devtool.Render{
		IsReload:      snap.IsReload,
		Pages:         make([]*devtool.RenderPage, 0, len(snap.Pages)),
		Partials:      snap.Partials,
		ErrorTemplate: builtInTemplateName,
		Assets:        snap.Assets,
		Counters:      engine.Counters,
	}
	if snap.HasErrorPage {
		out.ErrorTemplate = errorTemplateName
	}
	for _, p := range snap.Pages {
		out.Pages = append(out.Pages, &devtool.RenderPage{Name: p.Name, Layout: p.Layout})
	}
	slices.SortFunc(out.Pages, func(a, b *devtool.RenderPage) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func (http *HTTP) writePage(ex *httpExchange, result reflect.Value) {
	if ex.writer.status() != 0 {
		return
	}
	c := &ex.HTTPContext
	var view common.View
	isView := false
	if result.IsValid() && result.Type() == viewType {
		view, isView = result.Interface().(common.View)
	}
	if !isView {
		http.renderFailed(ex, "", errNotView)
		return
	}

	buf, err := http.renderPage(c, view.Name, view.Data)
	if err != nil {
		http.renderFailed(ex, view.Name, err)
		return
	}
	ex.writer.placeNonce(buf.Nonce())
	h := ex.writer.Header()
	h["Content-Type"] = textHTMLValue
	h["Content-Length"] = []string{strconv.Itoa(buf.Len())}
	setDefaultCacheControl(h)
	ex.writer.WriteHeader(ctx.HTTPCode(c))
	if c.Method != stdHTTP.MethodHead {
		_, _ = ex.writer.Write(buf.Bytes())
	}
	http.render.Release(buf)
}

func (http *HTTP) renderFailed(ex *httpExchange, name string, err error) {
	c := &ex.HTTPContext
	l := http.logger.From(c.Context())
	var reloadErr *render.FileError
	if errors.As(err, &reloadErr) {
		l.Error("RenderReloadFailed", "file", reloadErr.File, "cause", reloadErr.Err)
	} else {
		l.Error("RenderFailed", "template", name, "cause", err)
	}
	http.writeHTMLError(c, &ex.writer, stdHTTP.StatusInternalServerError, internalServerErrorException.Error(), internalServerErrorException.Message())
	ctx.HTTPSetCode(c, stdHTTP.StatusInternalServerError)
}

func (http *HTTP) renderPage(c *ctx.HTTPContext, name string, data any) (*render.Buffer, error) {
	if !http.tracer.Enabled() {
		return http.render.Render(name, data)
	}
	start := time.Now()
	buf, err := http.render.Render(name, data)
	http.emitRender(c, name, time.Since(start), err != nil)
	return buf, err
}

func (http *HTTP) renderErrorPage(c *ctx.HTTPContext, data any) (*render.Buffer, error) {
	if !http.tracer.Enabled() {
		return http.render.RenderError(data)
	}
	start := time.Now()
	buf, err := http.render.RenderError(data)
	if !errors.Is(err, render.ErrNoErrorPage) {
		http.emitRender(c, errorPageTraceName, time.Since(start), err != nil)
	}
	return buf, err
}

func (http *HTTP) builtInPage(c *ctx.HTTPContext, code int, status, message string) []byte {
	if !http.tracer.Enabled() {
		return builtInErrorPage(code, status, message)
	}
	start := time.Now()
	page := builtInErrorPage(code, status, message)
	http.emitRender(c, builtInPageTraceName, time.Since(start), false)
	return page
}

func (http *HTTP) emitRender(c *ctx.HTTPContext, name string, duration time.Duration, isFailed bool) {
	e := stageEvent(tracecontext.ScopeOf(c.Context()), c.ID(), trace.StageRender, name, trace.TransportHTTP, duration)
	if isFailed {
		e.Code = stdHTTP.StatusInternalServerError
	}
	http.tracer.Emit(e)
}
