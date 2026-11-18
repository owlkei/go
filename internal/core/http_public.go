package core

import (
	stdHTTP "net/http"
	"slices"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/routing"
	"github.com/dangduoc08/ginject/internal/versioning"
)

const publicRoutePrefix = "public/"

type publicEntry struct {
	name string
	etag []string
}

func (app *App) addPublicRoutes() {
	engine := app.http.render
	if engine == nil || engine.Public() == nil {
		return
	}
	version := ""
	if app.http.isVersioningEnabled {
		version = versioning.NeutralVersion
	}
	hashByName := engine.PublicFiles()
	names := make([]string, 0, len(hashByName))
	for name := range hashByName {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		route := "/" + name + "/"
		pattern := routing.MethodRouteVersionToPattern(stdHTTP.MethodGet, route, version)
		app.container.addRoute(pattern, publicRoutePrefix+name)
		layer := common.HTTPLayer{
			Method:          stdHTTP.MethodGet,
			Route:           route,
			Version:         version,
			Handler:         servePublicFile,
			Name:            publicRoutePrefix + name,
			MainHandlerName: publicRoutePrefix + name,
			Pattern:         pattern,
		}
		app.container.httpMainHandlers = append(app.container.httpMainHandlers, layer)
		app.http.addRoute(layer, nil)
		entry := &publicEntry{name: name}
		if !engine.IsReload() {
			entry.etag = []string{`"` + hashByName[name] + `"`}
		}
		app.http.routeOf(layer).public = entry
	}
}

func servePublicFile() {}

func (http *HTTP) servePublic(ex *httpExchange, entry *publicEntry) {
	h := ex.writer.Header()
	h[cacheControlHeader] = noCacheValue
	if entry.etag != nil {
		h[etagHeader] = entry.etag
	}
	serveFile(ex, http.render.Public(), entry.name)
}
