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
	"strings"
	"sync"
	"sync/atomic"
)

const (
	pageLimit   = 8 << 20
	retainLimit = 256 << 10
	initialSize = 4 << 10
	assetsDir   = "assets"
	assetsURL   = "/assets/"
	wellKnown   = ".well-known"
	nonceLength = 32
)

var (
	ErrUnknownPage  = errors.New("render: no page with this name")
	ErrPageTooLarge = errors.New("render: page larger than the 8 MiB limit")
	ErrNoErrorPage  = errors.New("render: no error.html")
	errUnknownAsset = errors.New("render: unknown asset")
	errDictPairs    = errors.New("render: dict needs key and value pairs")
	errDictKey      = errors.New("render: dict keys must be strings")
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
	fsys        fs.FS
	assets      fs.FS
	isReload    bool
	placeholder string
	pattern     []byte
	funcs       template.FuncMap
	hashByAsset map[string]string
	assetCount  int
	pool        sync.Pool

	nonceDraws atomic.Int64
	renders    atomic.Int64
	failures   atomic.Int64

	mu      sync.RWMutex
	site    *site
	stamp   string
	loadErr error
}

type site struct {
	pageByName map[string]*pageSet
	errorPage  *pageSet
	pages      []Page
	partials   []string
}

type pageSet struct {
	*template.Template
	usesNonce bool
}

type Page struct {
	Name   string
	Layout string
}

type Snapshot struct {
	IsReload     bool
	Pages        []Page
	Partials     []string
	HasErrorPage bool
	Assets       int
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
		nonceFunc: e.nonce,
		"asset":   e.asset,
		"dict":    dict,
	}
	return e
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
	assets, err := listAssets(e.fsys, !e.isReload)
	if err != nil {
		return err
	}
	if assets != nil {
		sub, err := fs.Sub(e.fsys, assetsDir)
		if err != nil {
			return err
		}
		e.assets = sub
	}
	e.assetCount = len(assets)
	if !e.isReload {
		e.hashByAsset = assets
	}
	stamp := ""
	if e.isReload {
		stamp = fingerprint(e.fsys)
	}
	s, err := e.parse()
	if err != nil {
		return err
	}
	e.site = s
	e.stamp = stamp
	return nil
}

func (e *Engine) Render(name string, data any) (*Buffer, error) {
	s, err := e.current()
	if err != nil {
		e.failures.Add(1)
		return nil, err
	}
	set := s.pageByName[name]
	if set == nil {
		e.failures.Add(1)
		return nil, ErrUnknownPage
	}
	return e.execute(set, data)
}

func (e *Engine) RenderError(data any) (*Buffer, error) {
	s, err := e.current()
	if err != nil {
		e.failures.Add(1)
		return nil, err
	}
	if s.errorPage == nil {
		return nil, ErrNoErrorPage
	}
	return e.execute(s.errorPage, data)
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

func (e *Engine) Snapshot() Snapshot {
	snap := Snapshot{IsReload: e.isReload, Assets: e.assetCount}
	s, err := e.current()
	if err != nil || s == nil {
		return snap
	}
	snap.Pages = append([]Page(nil), s.pages...)
	snap.Partials = append([]string(nil), s.partials...)
	snap.HasErrorPage = s.errorPage != nil
	return snap
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
		s, err := e.parse()
		e.stamp = stamp
		e.site, e.loadErr = s, nil
		if err != nil {
			e.site, e.loadErr = nil, err
		}
	}
	return siteOrError(e.site, e.loadErr)
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

func listAssets(fsys fs.FS, isHashed bool) (map[string]string, error) {
	info, err := fs.Stat(fsys, assetsDir)
	if err != nil || !info.IsDir() {
		return nil, nil
	}
	hashByAsset := map[string]string{}
	err = fs.WalkDir(fsys, assetsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return &FileError{File: p, Err: err}
		}
		if p != assetsDir && d.Name()[0] == '.' && d.Name() != wellKnown {
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
		hashByAsset[p[len(assetsDir)+1:]] = hash
		return nil
	})
	return hashByAsset, err
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
