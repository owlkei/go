package core

import (
	"io/fs"
	stdHTTP "net/http"
	"strings"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/routing"
	"github.com/dangduoc08/ginject/internal/str"
	"github.com/dangduoc08/ginject/internal/versioning"
)

const (
	assetRouteName = "ginject.assets"
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
	urlPath := str.RemoveDup(r.URL.Path, '/')
	if !strings.HasPrefix(urlPath, assetPrefix) {
		serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
		return
	}
	name := urlPath[len(assetPrefix):]
	if !fs.ValidPath(name) || name == "." || hasHiddenSegment(name) {
		serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
		return
	}

	h := ex.writer.Header()
	isPage := strings.HasPrefix(name, pagesPrefix)
	if http.assetByName == nil {
		if isPage {
			if _, ok := http.render.PageAsset(name[len(pagesPrefix):]); !ok {
				serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
				return
			}
		}
		h[cacheControlHeader] = noCacheValue
	} else {
		entry, ok := http.assetByName[name]
		if !ok {
			serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
			return
		}
		isPage = entry.isPage
		h[etagHeader] = entry.etag
		if queryValue(r.URL.RawQuery, "v") == entry.hash {
			h[cacheControlHeader] = immutableValue
		} else {
			h[cacheControlHeader] = noCacheValue
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
