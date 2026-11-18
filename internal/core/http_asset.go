package core

import (
	"bytes"
	"io"
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
)

var immutableValue = []string{"public, max-age=31536000, immutable"}

type assetEntry struct {
	hash string
	etag []string
}

func (app *App) addAssetRoute() {
	engine := app.http.render
	if engine == nil || engine.Assets() == nil {
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
	app.http.assetByName = make(map[string]assetEntry, len(hashes))
	for name, hash := range hashes {
		app.http.assetByName[name] = assetEntry{hash: hash, etag: []string{`"` + hash + `"`}}
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
	if http.assetByName == nil {
		h[cacheControlHeader] = noCacheValue
	} else {
		entry, ok := http.assetByName[name]
		if !ok {
			serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
			return
		}
		h[etagHeader] = entry.etag
		if queryValue(r.URL.RawQuery, "v") == entry.hash {
			h[cacheControlHeader] = immutableValue
		} else {
			h[cacheControlHeader] = noCacheValue
		}
	}

	f, err := http.render.Assets().Open(name)
	if err != nil {
		serveAssetError(ex)
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		serveAssetError(ex)
		return
	}
	content, ok := f.(io.ReadSeeker)
	if !ok {
		data, err := io.ReadAll(f)
		if err != nil {
			serveAssetError(ex)
			return
		}
		content = bytes.NewReader(data)
	}
	stdHTTP.ServeContent(&ex.writer, r, name, info.ModTime(), content)
}

func serveAssetError(ex *httpExchange) {
	delete(ex.writer.Header(), etagHeader)
	serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
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
