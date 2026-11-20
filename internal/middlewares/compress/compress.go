package compress

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.owlky.com/owlky/internal/color"
	"go.owlky.com/owlky/internal/common"
	"go.owlky.com/owlky/internal/ctx"
	"go.owlky.com/owlky/internal/httphook"
	"go.owlky.com/owlky/internal/inspect"
)

const (
	defaultMinSize       = 1024
	level                = gzip.BestSpeed
	writerCap            = 8
	maxAcceptEncoding    = 256
	sniffLength          = 512
	varyHeader           = "Vary"
	acceptEncodingHeader = "Accept-Encoding"
	contentEncodingKey   = "Content-Encoding"
	contentLengthKey     = "Content-Length"
	contentTypeKey       = "Content-Type"
	contentRangeKey      = "Content-Range"
	cacheControlKey      = "Cache-Control"
	acceptRangesKey      = "Accept-Ranges"
	etagKey              = "Etag"
	rangeHeader          = "Range"
	upgradeHeader        = "Upgrade"
)

var (
	varyAcceptEncoding = []string{acceptEncodingHeader}
	gzipEncoding       = []string{"gzip"}
)

type Compress struct {
	MinSize int
}

type compiledCompress struct {
	minSize    int
	writers    sync.Pool
	responses  sync.Pool
	inUse      atomic.Int64
	compressed atomic.Int64
	noWriter   atomic.Int64
	bytesIn    atomic.Int64
	bytesOut   atomic.Int64
}

func (instance Compress) NewMiddleware() common.MiddlewareFn {
	c, err := Compile(&instance)
	if err != nil {
		panic(color.FmtRed("invalid compress: " + err.Error()))
	}
	return c
}

func (instance Compress) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	instance.NewMiddleware().Use(r, w, next)
}

func Compile(c *Compress) (*compiledCompress, error) {
	if c.MinSize < 0 {
		return nil, fmt.Errorf("MinSize is %d, it must be 0 or more", c.MinSize)
	}
	compiled := &compiledCompress{minSize: c.MinSize}
	if compiled.minSize == 0 {
		compiled.minSize = defaultMinSize
	}
	compiled.writers.New = func() any {
		zw, _ := gzip.NewWriterLevel(nil, level)
		return zw
	}
	compiled.responses.New = func() any {
		return &response{c: compiled, held: make([]byte, 0, min(compiled.minSize, 64<<10))}
	}
	return compiled, nil
}

func (c *compiledCompress) Use(r *http.Request, w http.ResponseWriter, next ctx.Next) {
	hw := httphook.Of(w)
	if hw == nil {
		next()
		return
	}
	h := r.Header
	if len(h[upgradeHeader]) != 0 {
		next()
		return
	}
	addVary(w.Header())
	if r.Method == http.MethodHead || len(h[rangeHeader]) != 0 || !acceptsGzip(h[acceptEncodingHeader]) {
		next()
		return
	}
	resp := c.responses.Get().(*response)
	resp.inner = hw.Inner()
	hw.SetInner(resp)
	hw.OnDone(resp)
	next()
}

func addVary(h http.Header) {
	existing, ok := h[varyHeader]
	if !ok {
		h[varyHeader] = varyAcceptEncoding
		return
	}
	for _, value := range existing {
		for token := range strings.SplitSeq(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), acceptEncodingHeader) {
				return
			}
		}
	}
	h[varyHeader] = append(existing, acceptEncodingHeader)
}

func acceptsGzip(values []string) bool {
	budget := maxAcceptEncoding
	for _, value := range values {
		if budget <= 0 {
			return false
		}
		if len(value) > budget {
			value = value[:budget]
		}
		budget -= len(value)
		for value != "" {
			item := value
			if i := strings.IndexByte(value, ','); i >= 0 {
				item, value = value[:i], value[i+1:]
			} else {
				value = ""
			}
			coding, params, _ := strings.Cut(item, ";")
			if strings.EqualFold(strings.TrimSpace(coding), "gzip") && !isZeroQ(params) {
				return true
			}
		}
	}
	return false
}

func isZeroQ(params string) bool {
	for params != "" {
		param := params
		if i := strings.IndexByte(params, ';'); i >= 0 {
			param, params = params[:i], params[i+1:]
		} else {
			params = ""
		}
		name, value, ok := strings.Cut(strings.TrimSpace(param), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "q") {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" || value[0] != '0' {
			return false
		}
		for i := 1; i < len(value); i++ {
			if value[i] != '.' && value[i] != '0' {
				return false
			}
		}
		return true
	}
	return false
}

type counter struct {
	w io.Writer
	n int64
}

func (c *counter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

type response struct {
	c           *compiledCompress
	inner       http.ResponseWriter
	held        []byte
	zw          *gzip.Writer
	out         counter
	in          int64
	code        int
	decided     bool
	compressing bool
}

func (resp *response) Header() http.Header {
	return resp.inner.Header()
}

func (resp *response) WriteHeader(code int) {
	if resp.decided {
		resp.inner.WriteHeader(code)
		return
	}
	if code >= 100 && code < 200 {
		resp.inner.WriteHeader(code)
		return
	}
	if resp.code == 0 {
		resp.code = code
	}
}

func (resp *response) Write(p []byte) (int, error) {
	if !resp.decided {
		if resp.mustHold(len(p)) {
			resp.held = append(resp.held, p...)
			return len(p), nil
		}
		if err := resp.decide(p, false, false); err != nil {
			return 0, err
		}
	}
	if resp.compressing {
		resp.in += int64(len(p))
		return resp.zw.Write(p)
	}
	return resp.inner.Write(p)
}

func (resp *response) mustHold(n int) bool {
	if len(resp.held)+n >= resp.c.minSize || resp.isExcluded() {
		return false
	}
	_, declared := resp.inner.Header()[contentLengthKey]
	return !declared
}

func (resp *response) Flush() {
	if !resp.decided {
		_ = resp.decide(nil, false, true)
	}
	if resp.compressing {
		_ = resp.zw.Flush()
	}
	if f, ok := resp.inner.(http.Flusher); ok {
		f.Flush()
	}
}

func (resp *response) ReadFrom(r io.Reader) (int64, error) {
	if !resp.decided {
		if _, declared := resp.inner.Header()[contentLengthKey]; !declared {
			return io.Copy(writerOnly{resp}, r)
		}
		if err := resp.decide(nil, false, false); err != nil {
			return 0, err
		}
	}
	if resp.compressing {
		return io.Copy(writerOnly{resp}, r)
	}
	if rf, ok := resp.inner.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(writerOnly{resp.inner}, r)
}

type writerOnly struct {
	io.Writer
}

func (resp *response) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := resp.inner.(http.Hijacker)
	if !ok || (resp.compressing && resp.in > 0) {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (resp *response) Unwrap() http.ResponseWriter {
	return resp.inner
}

func (resp *response) isExcluded() bool {
	code := resp.code
	if code == http.StatusNoContent || code == http.StatusPartialContent || code == http.StatusNotModified {
		return true
	}
	h := resp.inner.Header()
	if len(h[contentEncodingKey]) != 0 || len(h[contentRangeKey]) != 0 || hasNoTransform(h[cacheControlKey]) {
		return true
	}
	if values := h[contentTypeKey]; len(values) != 0 && !isCompressible(values[0]) {
		return true
	}
	return false
}

func (resp *response) decide(p []byte, final, flushing bool) error {
	resp.decided = true
	h := resp.inner.Header()
	if len(h[contentTypeKey]) == 0 && len(resp.held)+len(p) > 0 {
		h[contentTypeKey] = []string{http.DetectContentType(resp.sniff(p))}
	}
	if !resp.isExcluded() && len(h[contentTypeKey]) != 0 && (flushing || resp.size(h, p) >= resp.c.minSize) {
		if resp.acquire() {
			return resp.startGzip()
		}
	}
	return resp.sendPlain(final)
}

func (resp *response) sniff(p []byte) []byte {
	switch {
	case len(resp.held) >= sniffLength || len(p) == 0:
		return resp.held
	case len(resp.held) == 0:
		return p
	}
	n := min(len(resp.held)+len(p), sniffLength)
	buf := make([]byte, 0, n)
	buf = append(buf, resp.held...)
	return append(buf, p[:n-len(resp.held)]...)
}

func (resp *response) size(h http.Header, p []byte) int {
	if values := h[contentLengthKey]; len(values) != 0 {
		if n, err := strconv.Atoi(values[0]); err == nil {
			return n
		}
	}
	return len(resp.held) + len(p)
}

func (resp *response) acquire() bool {
	if resp.c.inUse.Add(1) > writerCap {
		resp.c.inUse.Add(-1)
		resp.c.noWriter.Add(1)
		return false
	}
	resp.zw = resp.c.writers.Get().(*gzip.Writer)
	return true
}

func (resp *response) startGzip() error {
	resp.compressing = true
	h := resp.inner.Header()
	delete(h, contentLengthKey)
	delete(h, acceptRangesKey)
	h[contentEncodingKey] = gzipEncoding
	if values := h[etagKey]; len(values) != 0 && !strings.HasPrefix(values[0], "W/") {
		h[etagKey] = []string{"W/" + values[0]}
	}
	if resp.code != 0 {
		resp.inner.WriteHeader(resp.code)
	}
	resp.out = counter{w: resp.inner}
	resp.zw.Reset(&resp.out)
	if len(resp.held) == 0 {
		return nil
	}
	resp.in += int64(len(resp.held))
	_, err := resp.zw.Write(resp.held)
	resp.held = resp.held[:0]
	return err
}

func (resp *response) sendPlain(final bool) error {
	h := resp.inner.Header()
	if final && len(resp.held) > 0 && len(h[contentLengthKey]) == 0 {
		h[contentLengthKey] = []string{strconv.Itoa(len(resp.held))}
	}
	if resp.code != 0 {
		resp.inner.WriteHeader(resp.code)
	}
	if len(resp.held) == 0 {
		return nil
	}
	_, err := resp.inner.Write(resp.held)
	resp.held = resp.held[:0]
	return err
}

func (resp *response) Done(time.Duration) {
	if !resp.decided {
		_ = resp.decide(nil, true, false)
	}
	c := resp.c
	if resp.compressing {
		_ = resp.zw.Close()
		c.compressed.Add(1)
		c.bytesIn.Add(resp.in)
		c.bytesOut.Add(resp.out.n)
		resp.zw.Reset(io.Discard)
		c.writers.Put(resp.zw)
		c.inUse.Add(-1)
	}
	held := resp.held[:0]
	if cap(held) > max(c.minSize, sniffLength)*2 {
		held = nil
	}
	*resp = response{c: c, held: held}
	c.responses.Put(resp)
}

func hasNoTransform(values []string) bool {
	for _, value := range values {
		for token := range strings.SplitSeq(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "no-transform") {
				return true
			}
		}
	}
	return false
}

func isCompressible(contentType string) bool {
	mediaType, _, _ := strings.Cut(contentType, ";")
	mediaType = strings.TrimSpace(mediaType)
	if len(mediaType) > 5 && strings.EqualFold(mediaType[:5], "text/") {
		return !strings.EqualFold(mediaType, "text/event-stream")
	}
	switch {
	case strings.EqualFold(mediaType, "application/json"),
		strings.EqualFold(mediaType, "application/javascript"),
		strings.EqualFold(mediaType, "application/xml"),
		strings.EqualFold(mediaType, "application/wasm"),
		strings.EqualFold(mediaType, "image/svg+xml"):
		return true
	}
	return hasSuffixFold(mediaType, "+json") || hasSuffixFold(mediaType, "+xml")
}

func hasSuffixFold(s, suffix string) bool {
	return len(s) > len(suffix) && strings.EqualFold(s[len(s)-len(suffix):], suffix)
}

func (c *compiledCompress) Snapshot() inspect.Snapshot {
	return inspect.Snapshot{Name: "compress", Fields: []inspect.Field{
		{Key: "min_size", Value: int64(c.minSize)},
		{Key: "level", Value: int64(level)},
		{Key: "writer_cap", Value: int64(writerCap)},
		{Key: "writers_in_use", Value: c.inUse.Load()},
		{Key: "compressed", Value: c.compressed.Load()},
		{Key: "uncompressed_no_writer", Value: c.noWriter.Load()},
		{Key: "bytes_in", Value: c.bytesIn.Load()},
		{Key: "bytes_out", Value: c.bytesOut.Load()},
	}}
}
