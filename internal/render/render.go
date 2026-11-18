package render

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"html/template"
	"io"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	pageLimit         = 8 << 20
	retainLimit       = 256 << 10
	initialSize       = 4 << 10
	assetsDir         = "assets"
	assetsURL         = "/assets/"
	pagesDir          = "_pages"
	pagesURL          = assetsURL + pagesDir + "/"
	reservedPagesPath = assetsDir + "/" + pagesDir
	wellKnown         = ".well-known"
	nonceLength       = 32
)

var (
	ErrNoTemplate    = errors.New("render: no template for this route")
	ErrTwoTemplates  = errors.New("render: two templates for this route")
	ErrReservedDir   = errors.New("render: the route is in a reserved directory")
	ErrErrorPageName = errors.New("render: the route has the name of an error page")
	ErrPageTooLarge  = errors.New("render: page larger than the 8 MiB limit")
	ErrNoErrorPage   = errors.New("render: no error page for this status")
	errUnknownAsset  = errors.New("render: unknown asset")
	errDictPairs     = errors.New("render: dict needs key and value pairs")
	errDictKey       = errors.New("render: dict keys must be strings")
)

type FileError struct {
	File string
	Err  error
}

func (e *FileError) Error() string {
	return e.File + ": " + e.Err.Error()
}

func (e *FileError) Unwrap() error {
	return e.Err
}

type Buffer struct {
	buf      bytes.Buffer
	hasNonce bool
	raw      [nonceLength / 2]byte
	text     [nonceLength]byte
}

func (b *Buffer) Write(p []byte) (int, error) {
	if b.buf.Len()+len(p) > pageLimit {
		return 0, ErrPageTooLarge
	}
	return b.buf.Write(p)
}

func (b *Buffer) Bytes() []byte {
	return b.buf.Bytes()
}

func (b *Buffer) Len() int {
	return b.buf.Len()
}

func (b *Buffer) Nonce() string {
	if !b.hasNonce {
		return ""
	}
	return string(b.text[:])
}

type Engine struct {
	fsys         fs.FS
	assets       fs.FS
	public       fs.FS
	isReload     bool
	placeholder  string
	pattern      []byte
	funcs        template.FuncMap
	hashByAsset  map[string]string
	hashByPublic map[string]string
	assetCount   int
	routes       []string
	pool         sync.Pool

	nonceDraws atomic.Int64
	renders    atomic.Int64
	failures   atomic.Int64

	mu      sync.RWMutex
	site    *site
	stamp   string
	loadErr error
}

type site struct {
	setByFile    map[string]*pageSet
	setByRoute   map[string]*pageSet
	errorByCode  map[int]*pageSet
	errorByClass [10]*pageSet
	pageFiles    map[string]string
	pageOrder    []string
	errorFiles   []string
	partials     []string
}

type pageSet struct {
	*template.Template
	file      string
	name      string
	layout    string
	assets    []string
	usesNonce bool
}

type Page struct {
	Name   string
	Route  string
	Layout string
	Assets []string
}

type Snapshot struct {
	IsReload       bool
	Pages          []Page
	Partials       []string
	ErrorTemplates []string
	Assets         int
	Public         int
}

func New(fsys fs.FS, isReload bool) *Engine {
	e := &Engine{
		fsys:        fsys,
		isReload:    isReload,
		placeholder: newPlaceholder(),
		pool:        sync.Pool{New: newBuffer},
	}
	e.pattern = []byte(e.placeholder)
	e.funcs = template.FuncMap{
		nonceFunc:      e.nonce,
		pageAssetsFunc: noPageAssets,
		"asset":        e.asset,
		"dict":         dict,
	}
	return e
}

func noPageAssets() template.HTML {
	return ""
}

func newBuffer() any {
	b := &Buffer{}
	b.buf.Grow(initialSize)
	return b
}

func newPlaceholder() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (e *Engine) Load() error {
	if _, err := fs.Stat(e.fsys, reservedPagesPath); err == nil {
		return &checkError{file: reservedPagesPath, text: reservedPagesPath + " is reserved for page CSS and JS"}
	}
	assets, err := listFiles(e.fsys, assetsDir, !e.isReload)
	if err != nil {
		return err
	}
	if assets != nil {
		if e.assets, err = fs.Sub(e.fsys, assetsDir); err != nil {
			return err
		}
	}
	public, err := listFiles(e.fsys, publicDir, !e.isReload)
	if err != nil {
		return err
	}
	if public != nil {
		if e.public, err = fs.Sub(e.fsys, publicDir); err != nil {
			return err
		}
	}
	e.assetCount = len(assets)
	e.hashByPublic = public
	if !e.isReload {
		e.hashByAsset = assets
	}
	stamp := ""
	if e.isReload {
		stamp = fingerprint(e.fsys)
	}
	s, err := e.parse(true)
	if err != nil {
		return err
	}
	e.site = s
	e.stamp = stamp
	return nil
}

func (e *Engine) Bind(route string) (string, error) {
	key := strings.Trim(route, "/")
	set, err := e.site.resolve(key)
	if err != nil {
		return "", err
	}
	if _, ok := e.site.setByRoute[key]; !ok {
		e.site.setByRoute[key] = set
		e.routes = append(e.routes, key)
	}
	return set.name, nil
}

func (e *Engine) CheckBindings() error {
	bound := make(map[string]bool, len(e.site.setByRoute))
	for _, set := range e.site.setByRoute {
		bound[set.file] = true
	}
	var errs []error
	for _, file := range e.site.pageOrder {
		if !bound[file] {
			errs = append(errs, &checkError{file: file, text: file + " has no RENDER_ route: add one, move it to partials/, or delete it"})
		}
	}
	return errors.Join(errs...)
}

func (e *Engine) Render(route string, data any) (*Buffer, error) {
	s, err := e.current()
	if err != nil {
		e.failures.Add(1)
		return nil, err
	}
	set := s.setByRoute[strings.Trim(route, "/")]
	if set == nil {
		e.failures.Add(1)
		return nil, ErrNoTemplate
	}
	return e.execute(set, data)
}

func (e *Engine) RenderError(code int, data any) (*Buffer, error) {
	s, err := e.current()
	if err != nil {
		e.failures.Add(1)
		return nil, err
	}
	set := s.errorPage(code)
	if set == nil {
		return nil, ErrNoErrorPage
	}
	return e.execute(set, data)
}

func (e *Engine) ErrorTemplate(code int) string {
	s := e.loaded()
	if s == nil {
		return ""
	}
	if set := s.errorPage(code); set != nil {
		return set.name
	}
	return ""
}

func (s *site) errorPage(code int) *pageSet {
	if code < 400 || code > 599 {
		return nil
	}
	if set := s.errorByCode[code]; set != nil {
		return set
	}
	return s.errorByClass[code/100]
}

func (e *Engine) execute(set *pageSet, data any) (*Buffer, error) {
	b := e.pool.Get().(*Buffer)
	if err := set.Execute(b, data); err != nil {
		e.failures.Add(1)
		e.Release(b)
		return nil, err
	}
	if set.usesNonce {
		e.placeNonce(b)
	}
	e.renders.Add(1)
	return b, nil
}

func (e *Engine) placeNonce(b *Buffer) {
	_, _ = rand.Read(b.raw[:])
	hex.Encode(b.text[:], b.raw[:])
	e.nonceDraws.Add(1)
	page := b.buf.Bytes()
	for i := 0; ; {
		j := bytes.Index(page[i:], e.pattern)
		if j < 0 {
			break
		}
		i += j
		copy(page[i:], b.text[:])
		i += nonceLength
	}
	b.hasNonce = true
}

func (e *Engine) Release(b *Buffer) {
	if b.buf.Cap() > retainLimit {
		return
	}
	b.buf.Reset()
	b.hasNonce = false
	e.pool.Put(b)
}

func (e *Engine) NonceDraws() int64 {
	return e.nonceDraws.Load()
}

func (e *Engine) Counters() (renders, failures int64) {
	return e.renders.Load(), e.failures.Load()
}

func (e *Engine) IsReload() bool {
	return e.isReload
}

func (e *Engine) Assets() fs.FS {
	return e.assets
}

func (e *Engine) AssetHashes() map[string]string {
	return maps.Clone(e.hashByAsset)
}

func (e *Engine) Public() fs.FS {
	return e.public
}

func (e *Engine) PublicFiles() map[string]string {
	return maps.Clone(e.hashByPublic)
}

func (e *Engine) Files() fs.FS {
	return e.fsys
}

func (e *Engine) PageAssets() map[string]string {
	if s := e.loaded(); s != nil {
		return maps.Clone(s.pageFiles)
	}
	return nil
}

func (e *Engine) PageAsset(p string) (string, bool) {
	s, err := e.current()
	if err != nil {
		return "", false
	}
	hash, ok := s.pageFiles[p]
	return hash, ok
}

func (e *Engine) Snapshot() Snapshot {
	snap := Snapshot{IsReload: e.isReload, Assets: e.assetCount, Public: len(e.hashByPublic)}
	s := e.loaded()
	if s == nil {
		return snap
	}
	for _, key := range e.routes {
		set := s.setByRoute[key]
		snap.Pages = append(snap.Pages, Page{Name: set.name, Route: "/" + key, Layout: set.layout, Assets: slices.Clone(set.assets)})
	}
	snap.Partials = slices.Clone(s.partials)
	snap.ErrorTemplates = slices.Clone(s.errorFiles)
	slices.Sort(snap.ErrorTemplates)
	return snap
}

func (e *Engine) loaded() *site {
	if !e.isReload {
		return e.site
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.site
}

func (e *Engine) current() (*site, error) {
	if !e.isReload {
		return e.site, nil
	}
	stamp := fingerprint(e.fsys)
	e.mu.RLock()
	if stamp == e.stamp {
		s, err := e.site, e.loadErr
		e.mu.RUnlock()
		return siteOrError(s, err)
	}
	e.mu.RUnlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	if stamp != e.stamp {
		s, err := e.parse(false)
		e.stamp = stamp
		e.site, e.loadErr = s, nil
		if err != nil {
			e.site, e.loadErr = nil, asFileError(err)
		}
	}
	return siteOrError(e.site, e.loadErr)
}

func asFileError(err error) error {
	var check *checkError
	if errors.As(err, &check) {
		return &FileError{File: check.file, Err: err}
	}
	var bind *bindError
	if errors.As(err, &bind) {
		return &FileError{File: bind.file, Err: err}
	}
	return err
}

func siteOrError(s *site, err error) (*site, error) {
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (e *Engine) nonce() string {
	return e.placeholder
}

func (e *Engine) asset(name string) (string, error) {
	if e.isReload {
		if e.assets == nil {
			return "", errUnknownAsset
		}
		info, err := fs.Stat(e.fsys, assetsDir+"/"+name)
		if err != nil || info.IsDir() || isHiddenAsset(name) {
			return "", errUnknownAsset
		}
		return assetsURL + name, nil
	}
	hash, ok := e.hashByAsset[name]
	if !ok {
		return "", errUnknownAsset
	}
	return assetsURL + name + "?v=" + hash, nil
}

func dict(values ...any) (map[string]any, error) {
	if len(values)%2 != 0 {
		return nil, errDictPairs
	}
	m := make(map[string]any, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			return nil, errDictKey
		}
		m[key] = values[i+1]
	}
	return m, nil
}

func listFiles(fsys fs.FS, dir string, isHashed bool) (map[string]string, error) {
	info, err := fs.Stat(fsys, dir)
	if err != nil || !info.IsDir() {
		return nil, nil
	}
	hashByFile := map[string]string{}
	err = fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return &FileError{File: p, Err: err}
		}
		if p != dir && d.Name()[0] == '.' && d.Name() != wellKnown {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		hash := ""
		if isHashed {
			hash, err = hashFile(fsys, p)
			if err != nil {
				return &FileError{File: p, Err: err}
			}
		}
		hashByFile[p[len(dir)+1:]] = hash
		return nil
	})
	return hashByFile, err
}

func isHiddenAsset(p string) bool {
	for segment := range strings.SplitSeq(p, "/") {
		if len(segment) > 0 && segment[0] == '.' && segment != wellKnown {
			return true
		}
	}
	return false
}

func hashFile(fsys fs.FS, p string) (string, error) {
	f, err := fsys.Open(p)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)[:8]), nil
}
