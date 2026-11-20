package core

import (
	"bytes"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"

	stdHTTP "net/http"
)

const (
	contentTypeHeader        = "Content-Type"
	contentDispositionHeader = "Content-Disposition"
	noSniffHeader            = "X-Content-Type-Options"
	rangeHeader              = "Range"
	wellKnownSegment         = ".well-known"
	maxServeDirs             = 64
	maxLoggedName            = 256
	indexFile                = "index.html"
	serveRootNote            = "the directory is not under HTTP.ServeDirs"
	serveAttachmentNote      = "files that run in a browser are sent as downloads; set Content-Disposition: inline in the handler when these files are yours, never for uploads"
)

var (
	noSniffValue           = []string{"nosniff"}
	attachmentValue        = []string{"attachment"}
	sandboxValue           = []string{"sandbox; default-src 'none'"}
	privateNoCacheValue    = []string{"private, no-cache"}
	privateImmutableValue  = []string{"private, max-age=31536000, immutable"}
	octetStreamValue       = []string{"application/octet-stream"}
	contentTypeByExtension = []struct {
		ext   string
		value []string
	}{
		{".css", []string{"text/css; charset=utf-8"}},
		{".js", []string{"text/javascript; charset=utf-8"}},
		{".mjs", []string{"text/javascript; charset=utf-8"}},
		{".html", []string{"text/html; charset=utf-8"}},
		{".htm", []string{"text/html; charset=utf-8"}},
		{".svg", []string{"image/svg+xml"}},
		{".json", []string{"application/json"}},
		{".map", []string{"application/json"}},
		{".wasm", []string{"application/wasm"}},
		{".xml", []string{"text/xml; charset=utf-8"}},
		{".txt", []string{"text/plain; charset=utf-8"}},
	}
	activeExtensions  = []string{".html", ".htm", ".xhtml", ".xht", ".shtml", ".svg", ".svgz", ".xml", ".xsl", ".xslt", ".js", ".mjs"}
	activeTypes       = []string{"text/html", "application/xhtml+xml", "image/svg+xml", "text/xml", "application/xml", "text/javascript", "application/javascript"}
	forbiddenSuffixes = []string{
		".go", ".env", ".pem", ".key", ".p12", ".pfx",
		"~", ".swp", ".swo", ".bak", ".orig", ".rej", ".old", ".tmp",
		".sql", ".sqlite", ".sqlite3", ".db", ".map",
	}
	forbiddenNames = []string{"go.mod", "go.sum", "go.work", "go.work.sum", "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519"}
	deviceNames    = []string{"con", "prn", "aux", "nul"}
)

func isServableName(name string, isMapAllowed bool) bool {
	last := ""
	for len(name) > 0 {
		segment := name
		if i := strings.IndexByte(name, '/'); i >= 0 {
			segment, name = name[:i], name[i+1:]
		} else {
			name = ""
		}
		if segment == "" {
			continue
		}
		if !isServableSegment(segment) {
			return false
		}
		last = segment
	}
	if last == "" {
		return true
	}
	if isMapAllowed && strings.HasSuffix(last, ".map") {
		return true
	}
	return !isForbiddenFile(last)
}

func isServableSegment(segment string) bool {
	if segment[0] == '.' && segment != wellKnownSegment {
		return false
	}
	if end := segment[len(segment)-1]; end == '.' || end == ' ' {
		return false
	}
	for i := 0; i < len(segment); i++ {
		switch b := segment[i]; {
		case b < 0x20, b == 0x7f, b == '\\', b == ':':
			return false
		case b == '~' && i+1 < len(segment) && segment[i+1] >= '0' && segment[i+1] <= '9':
			return false
		case isASCIIFoldAt(segment, i):
			return false
		}
	}
	return !isDeviceName(segment)
}

func isASCIIFoldAt(s string, i int) bool {
	switch s[i] {
	case 0xc4:
		return i+1 < len(s) && (s[i+1] == 0xb0 || s[i+1] == 0xb1)
	case 0xc5:
		return i+1 < len(s) && s[i+1] == 0xbf
	case 0xe2:
		return i+2 < len(s) && s[i+1] == 0x84 && s[i+2] == 0xaa
	}
	return false
}

func isDeviceName(segment string) bool {
	stem := segment
	if i := strings.IndexByte(segment, '.'); i >= 0 {
		stem = segment[:i]
	}
	switch len(stem) {
	case 3:
		for _, name := range deviceNames {
			if strings.EqualFold(stem, name) {
				return true
			}
		}
	case 4:
		digit := stem[3]
		return digit >= '1' && digit <= '9' && (strings.EqualFold(stem[:3], "com") || strings.EqualFold(stem[:3], "lpt"))
	}
	return false
}

func isForbiddenFile(name string) bool {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	if len(name) >= 4 && strings.EqualFold(name[:4], ".env") {
		return true
	}
	if len(name) >= 2 && name[0] == '#' && name[len(name)-1] == '#' {
		return true
	}
	for _, suffix := range forbiddenSuffixes {
		if hasSuffixFold(name, suffix) {
			return true
		}
	}
	for _, exact := range forbiddenNames {
		if strings.EqualFold(name, exact) {
			return true
		}
	}
	return false
}

func hasSuffixFold(s, suffix string) bool {
	return len(s) >= len(suffix) && strings.EqualFold(s[len(s)-len(suffix):], suffix)
}

func contentTypeOf(name string) []string {
	ext := path.Ext(name)
	if ext == "" {
		return octetStreamValue
	}
	for _, entry := range contentTypeByExtension {
		if strings.EqualFold(ext, entry.ext) {
			return entry.value
		}
	}
	if typ := mime.TypeByExtension(ext); typ != "" {
		return []string{typ}
	}
	return octetStreamValue
}

func isActiveFile(name, contentType string) bool {
	ext := path.Ext(name)
	for _, active := range activeExtensions {
		if strings.EqualFold(ext, active) {
			return true
		}
	}
	typ, _, _ := strings.Cut(contentType, ";")
	typ = strings.TrimSpace(typ)
	if hasSuffixFold(typ, "+xml") {
		return true
	}
	for _, active := range activeTypes {
		if strings.EqualFold(typ, active) {
			return true
		}
	}
	return false
}

func setStaticHeaders(h stdHTTP.Header, name string) {
	h[noSniffHeader] = noSniffValue
	if _, ok := h[contentTypeHeader]; !ok {
		h[contentTypeHeader] = contentTypeOf(name)
	}
}

func singleRangeRequest(r *stdHTTP.Request) *stdHTTP.Request {
	ranges := r.Header[rangeHeader]
	if len(ranges) == 0 || (len(ranges) == 1 && strings.IndexByte(ranges[0], ',') < 0) {
		return r
	}
	full := *r
	full.Header = r.Header.Clone()
	delete(full.Header, rangeHeader)
	return &full
}

func sendFile(ex *httpExchange, f fs.File, info fs.FileInfo, name string) {
	content, ok := f.(io.ReadSeeker)
	if !ok {
		data, err := io.ReadAll(f)
		if err != nil {
			serveAssetError(ex)
			return
		}
		content = bytes.NewReader(data)
	}
	stdHTTP.ServeContent(&ex.writer, singleRangeRequest(ex.Request), name, info.ModTime(), content)
}

func serveFile(ex *httpExchange, fsys fs.FS, name string) {
	f, err := fsys.Open(name)
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
	setStaticHeaders(ex.writer.Header(), name)
	sendFile(ex, f, info, name)
}

func serveAssetError(ex *httpExchange) {
	delete(ex.writer.Header(), etagHeader)
	serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
}

func loadServeDirs(entries []string) (dirs []string, workDir string) {
	if len(entries) > maxServeDirs {
		failOption("HTTP.ServeDirs", "HTTP.ServeDirs has %d entries, at most %d are allowed", len(entries), maxServeDirs)
	}
	for _, entry := range entries {
		if entry == "" {
			failOption("HTTP.ServeDirs", "HTTP.ServeDirs has an empty entry")
		}
	}
	if len(entries) == 0 {
		return nil, ""
	}
	workDir, err := os.Getwd()
	if err != nil {
		failOption("HTTP.ServeDirs", "HTTP.ServeDirs needs the working directory: %v", err)
	}
	for _, entry := range entries {
		dir := absPath(workDir, entry)
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			failOption("HTTP.ServeDirs", "HTTP.ServeDirs entry %q is not a directory", entry)
		}
		if dir == workDir || isBelow(workDir, dir) || filepath.Dir(dir) == dir {
			failOption("HTTP.ServeDirs", "HTTP.ServeDirs entry %q is the working directory or above it: serve a subdirectory", entry)
		}
		dirs = append(dirs, dir)
	}
	return dirs, workDir
}

func absPath(workDir, p string) string {
	p = filepath.Clean(p)
	if !filepath.IsAbs(p) {
		p = filepath.Join(workDir, p)
	}
	return p
}

func isBelow(p, dir string) bool {
	return len(p) > len(dir) && strings.HasPrefix(p, dir) && (p[len(dir)] == filepath.Separator || dir[len(dir)-1] == filepath.Separator)
}

func hasControl(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

func (http *HTTP) declaredDirOf(root string) (string, bool) {
	found := ""
	for _, dir := range http.serveDirs {
		if (root == dir || isBelow(root, dir)) && len(dir) > len(found) {
			found = dir
		}
	}
	return found, found != ""
}

func (http *HTTP) serveContent(ex *httpExchange, route *httpRoute, result any) {
	dir, ok := result.(string)
	if !ok || dir == "" {
		serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
		return
	}
	root := absPath(http.workDir, dir)
	declared, ok := http.declaredDirOf(root)
	if !ok || hasControl(root) {
		http.refuseServeRoot(ex, route, root)
		return
	}
	sub := ""
	if root != declared {
		sub = filepath.ToSlash(strings.TrimLeft(root[len(declared):], string(filepath.Separator)))
	}
	name := ""
	if route.serveIndex != 0 {
		name = wildcardRest(ex.URL.Path, route.serveIndex)
	}
	if !isServableName(sub+"/"+name, false) {
		serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
		return
	}

	base, err := os.OpenRoot(declared)
	if err != nil {
		serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
		return
	}
	defer func() { _ = base.Close() }()
	if route.serveIndex == 0 {
		http.serveRootFile(ex, route, base.FS(), sub)
		return
	}
	handlerRoot := base
	if sub != "" {
		handlerRoot, err = base.OpenRoot(sub)
		if err != nil {
			serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
			return
		}
		defer func() { _ = handlerRoot.Close() }()
	}
	http.serveRootFile(ex, route, handlerRoot.FS(), name)
}

func wildcardRest(urlPath string, serveIndex int) string {
	for i := 0; i < len(urlPath); i++ {
		if urlPath[i] != '/' {
			continue
		}
		serveIndex--
		if serveIndex == 0 {
			return urlPath[i+1:]
		}
	}
	return ""
}

func (http *HTTP) serveRootFile(ex *httpExchange, route *httpRoute, fsys fs.FS, name string) {
	name = strings.TrimSuffix(name, "/")
	if name == "" {
		name = "."
	}
	f, err := fsys.Open(name)
	if err != nil {
		serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
		return
	}
	if info.IsDir() {
		index := path.Join(name, indexFile)
		indexed, err := fsys.Open(index)
		if err != nil {
			serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
			return
		}
		defer func() { _ = indexed.Close() }()
		indexInfo, err := indexed.Stat()
		if err != nil || indexInfo.IsDir() {
			serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
			return
		}
		if urlPath := ex.URL.Path; !strings.HasSuffix(urlPath, "/") {
			redirectToSlash(ex, urlPath)
			return
		}
		f, info, name = indexed, indexInfo, index
	}

	h := ex.writer.Header()
	setStaticHeaders(h, name)
	if _, ok := h[contentDispositionHeader]; !ok && isActiveFile(name, h.Get(contentTypeHeader)) {
		h[contentDispositionHeader] = attachmentValue
		h[cspHeader] = sandboxValue
		if route.isAttachmentLogged.CompareAndSwap(false, true) {
			http.logger.From(ex.Context()).Warn("ServeAttachment", "route", route.name, "file", cut(name, maxLoggedName), "note", serveAttachmentNote)
		}
	}
	if _, ok := h[cacheControlHeader]; !ok {
		h[cacheControlHeader] = noCacheValue
		if len(route.guards) > 0 {
			h[cacheControlHeader] = privateNoCacheValue
		}
	}
	sendFile(ex, f, info, name)
}

func redirectToSlash(ex *httpExchange, urlPath string) {
	if !strings.HasPrefix(urlPath, "/") || strings.HasPrefix(urlPath, "//") || strings.IndexByte(urlPath, '\\') >= 0 {
		serveError(ex, stdHTTP.StatusNotFound, notFoundBody)
		return
	}
	target := urlPath + "/"
	if q := ex.URL.RawQuery; q != "" {
		target += "?" + q
	}
	ex.writer.Header()["Location"] = []string{target}
	ex.writer.WriteHeader(stdHTTP.StatusMovedPermanently)
}

func (http *HTTP) refuseServeRoot(ex *httpExchange, route *httpRoute, root string) {
	http.logger.From(ex.Context()).Error("ServeRootRefused", "route", route.name, "root", cut(root, maxLoggedName), "note", serveRootNote)
	serveError(ex, stdHTTP.StatusInternalServerError, internalErrorBody)
}

func cut(s string, limit int) string {
	if len(s) > limit {
		return s[:limit]
	}
	return s
}
