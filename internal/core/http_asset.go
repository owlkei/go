package core

import (
	"io/fs"
	stdHTTP "net/http"
	"strings"

	"go.owlkei.com/owlkei/internal/common"
	"go.owlkei.com/owlkei/internal/routing"
	"go.owlkei.com/owlkei/internal/versioning"
)

const (
	assetRouteName = "owlkei.assets"
	assetRoute     = "/assets/*/"
	assetPrefix    = "/assets/"
	etagHeader     = "Etag"
	pagesDir       = "_pages"
	pagesPrefix    = pagesDir + "/"
)

var immutableValue = []string{"public, max-age=31536000, immutable"}

type assetEntry struct {
	hash   string
	etag   []string
	isPage bool
}

func (app *App) addAssetRoute() {
	engine := app.http.render
	if engine == nil {
		return
	}
	pageHashes := engine.PageAssets()
	if engine.Assets() == nil && len(pageHashes) == 0 {
		return
	}
	version := ""
	if app.http.isVersioningEnabled {
		version = versioning.NeutralVersion
	}
	app.container.addRoute(routing.MethodRouteVersionToPattern(routing.SERVE, assetRoute, version), assetRouteName)
	layer := common.HTTPLayer{Method: routing.SERVE, Route: assetRoute, Version: version, Handler: serveAssets, Name: assetRouteName}
	app.http.addRoute(layer, nil)
	app.http.routeOf(layer).isAsset = true

	if engine.IsReload() {
		return
	}
	hashes := engine.AssetHashes()
	app.http.assetByName = make(map[string]assetEntry, len(hashes)+len(pageHashes))
	for name, hash := range hashes {
		app.http.assetByName[name] = assetEntry{hash: hash, etag: []string{`"` + hash + `"`}}
	}
	for name, hash := range pageHashes {
		app.http.assetByName[pagesPrefix+name] = assetEntry{hash: hash, etag: []string{`"` + hash + `"`}, isPage: true}
	}
}

func serveAssets() {}

func (http *HTTP) serveAsset(ex *httpExchange) {
	r := ex.Request
	urlPath := r.URL.Path
	if !strings.HasPrefix(urlPath, assetPrefix) {
		serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
		return
	}
	name := urlPath[len(assetPrefix):]
	if !fs.ValidPath(name) || name == "." || !isServableName(name, http.render.IsReload()) {
		serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
		return
	}

	isPage := strings.HasPrefix(name, pagesPrefix)
	isPrivate := false
	if isPage {
		owner, ok := http.render.PageAssetOwner(name[len(pagesPrefix):])
		if !ok {
			serveAssetError(ex)
			return
		}
		if route := http.routeByTemplate[owner]; owner != "" && route != nil {
			if route.gate != nil {
				live := route.gate.live.Load()
				if live == nil {
					answerGatedAsset(ex, route)
					return
				}
				route = live.http[route.gateIndex]
			}
			if !http.runPageGuards(ex, route) {
				return
			}
			isPrivate = len(route.guards) > 0
		}
	}

	noCache, immutable := noCacheValue, immutableValue
	if isPrivate {
		noCache, immutable = privateNoCacheValue, privateImmutableValue
	}
	h := ex.writer.Header()
	if http.assetByName == nil {
		h[cacheControlHeader] = noCache
	} else {
		entry, ok := http.assetByName[name]
		if !ok {
			serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
			return
		}
		isPage = entry.isPage
		h[etagHeader] = entry.etag
		if queryValue(r.URL.RawQuery, "v") == entry.hash {
			h[cacheControlHeader] = immutable
		} else {
			h[cacheControlHeader] = noCache
		}
	}

	if isPage {
		serveFile(ex, http.render.Files(), name[len(pagesPrefix):])
		return
	}
	assets := http.render.Assets()
	if assets == nil || name == pagesDir {
		serveAssetError(ex)
		return
	}
	serveFile(ex, assets, name)
}

func answerGatedAsset(ex *httpExchange, route *httpRoute) {
	if route.gate.state.status() == modulePending {
		ex.writer.Header()[retryAfterHeader] = route.gate.retryAfter()
	}
	writeJSONError(&ex.writer, stdHTTP.StatusServiceUnavailable, serviceUnavailableBody)
}

func (http *HTTP) runPageGuards(ex *httpExchange, route *httpRoute) bool {
	isAllowed, rec := http.tryGuards(ex, route.guards)
	if rec != nil {
		if e, ok := asException(rec); !ok || e.Code() >= stdHTTP.StatusInternalServerError {
			panic(rec)
		}
		isAllowed = false
	}
	if !isAllowed {
		serveAssetError(ex)
	}
	return isAllowed
}

func (http *HTTP) tryGuards(ex *httpExchange, guards []httpGuard) (isAllowed bool, rec any) {
	defer func() { rec = recover() }()
	for i := range guards {
		if !http.runGuard(ex, &guards[i]) {
			return false, nil
		}
	}
	return true, nil
}

func queryValue(rawQuery, key string) string {
	for rawQuery != "" {
		var pair string
		pair, rawQuery, _ = strings.Cut(rawQuery, "&")
		if k, v, ok := strings.Cut(pair, "="); ok && k == key {
			return v
		}
	}
	return ""
}
