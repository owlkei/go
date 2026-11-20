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

	"go.owlkei.com/owlkei/internal/color"
	"go.owlkei.com/owlkei/internal/ctx"
	"go.owlkei.com/owlkei/internal/devtool"
	"go.owlkei.com/owlkei/internal/render"
	"go.owlkei.com/owlkei/internal/trace"
	"go.owlkei.com/owlkei/internal/tracecontext"
)

type RenderOptions struct {
	FS       fs.FS
	Dir      string
	IsReload bool
}

const defaultRenderDir = "web"

const (
	textHTMLUTF8         = "text/html; charset=utf-8"
	builtInPageTraceName = "error (built-in)"
)

var textHTMLValue = []string{textHTMLUTF8}

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
	app.logStaticFiles()
}

const (
	maxListedSkipped = 20
	maxListedPublic  = 50
)

func (app *App) logStaticFiles() {
	engine := app.http.render
	if skipped := engine.Skipped(); len(skipped) > 0 {
		app.logger.Warn("StaticFilesSkipped", "count", len(skipped), "files", skipped[:min(len(skipped), maxListedSkipped)], "note", "source, secret, backup, linked or oddly named files are never served")
	}
	if hidden := engine.Hidden(); len(hidden) > 0 {
		app.logger.Warn("RenderHiddenFiles", "count", len(hidden), "files", hidden[:min(len(hidden), maxListedSkipped)], "note", "these ship inside the binary with //go:embed all: and are never served")
	}
	public := engine.PublicFiles()
	if len(public) == 0 {
		return
	}
	urls := make([]string, 0, len(public))
	for name := range public {
		urls = append(urls, "/"+name)
	}
	slices.Sort(urls)
	app.logger.Info("PublicFiles", "count", len(urls), "urls", urls[:min(len(urls), maxListedPublic)])
}

func (app *App) checkRenderBindings() {
	if app.http.render == nil {
		return
	}
	if err := app.http.render.CheckBindings(); err != nil {
		panic(color.FmtRed("invalid render: %s", strings.ReplaceAll(err.Error(), "\n", "\ninvalid render: ")))
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
		root, err := os.OpenRoot(dir)
		if err != nil {
			panic(color.FmtRed("invalid render: IsReload reads %q from disk: %v", dir, err))
		}
		return root.FS(), dir
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
		IsReload:       snap.IsReload,
		Pages:          make([]*devtool.RenderPage, 0, len(snap.Pages)),
		Partials:       snap.Partials,
		ErrorTemplates: snap.ErrorTemplates,
		Assets:         snap.Assets,
		Public:         snap.Public,
		Counters:       engine.Counters,
	}
	for _, p := range snap.Pages {
		out.Pages = append(out.Pages, &devtool.RenderPage{Name: p.Name, Route: p.Route, Layout: p.Layout, Assets: p.Assets})
	}
	slices.SortFunc(out.Pages, func(a, b *devtool.RenderPage) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func (http *HTTP) writePage(ex *httpExchange, route *httpRoute, result reflect.Value) {
	if ex.writer.status() != 0 {
		return
	}
	c := &ex.HTTPContext
	var data any
	if result.IsValid() {
		data = result.Interface()
	}

	buf, name, err := http.renderPage(c, route, data)
	if err != nil {
		http.renderFailed(ex, name, err)
		return
	}
	ex.writer.placeNonce(buf.Nonce())
	h := ex.writer.Header()
	h["Content-Type"] = textHTMLValue
	h["Content-Length"] = []string{strconv.Itoa(buf.Len())}
	setPrivateCacheControl(h)
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

func (http *HTTP) renderPage(c *ctx.HTTPContext, route *httpRoute, data any) (*render.Buffer, string, error) {
	if !http.tracer.Enabled() {
		buf, name, err := http.render.RenderPage(route.pageRoute, data)
		return buf, renderedName(name, route), err
	}
	start := time.Now()
	buf, name, err := http.render.RenderPage(route.pageRoute, data)
	name = renderedName(name, route)
	http.emitRender(c, name, time.Since(start), err != nil)
	return buf, name, err
}

func renderedName(name string, route *httpRoute) string {
	if name == "" {
		return route.template
	}
	return name
}

func (http *HTTP) renderErrorPage(c *ctx.HTTPContext, code int, data any) (*render.Buffer, error) {
	if !http.tracer.Enabled() {
		return http.render.RenderError(code, data)
	}
	start := time.Now()
	buf, err := http.render.RenderError(code, data)
	if !errors.Is(err, render.ErrNoErrorPage) {
		http.emitRender(c, http.render.ErrorTemplate(code), time.Since(start), err != nil)
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
