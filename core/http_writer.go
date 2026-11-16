package core

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
)

var errResponseEnded = errors.New("core: response writer used after the request ended")

type responseWriter struct {
	w    http.ResponseWriter
	code int
}

func (w *responseWriter) reset(rw http.ResponseWriter) {
	w.w = rw
	w.code = 0
}

func (w *responseWriter) Header() http.Header {
	if w.w == nil {
		return http.Header{}
	}
	return w.w.Header()
}

func (w *responseWriter) Write(p []byte) (int, error) {
	if w.w == nil {
		return 0, errResponseEnded
	}
	if w.code == 0 {
		w.code = http.StatusOK
	}
	return w.w.Write(p)
}

func (w *responseWriter) WriteHeader(code int) {
	if w.w == nil {
		return
	}
	if w.code == 0 && !isInformational(code) {
		w.code = code
	}
	w.w.WriteHeader(code)
}

func isInformational(code int) bool {
	return code >= 100 && code < 200 && code != http.StatusSwitchingProtocols
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.w
}

func (w *responseWriter) Flush() {
	flusher, ok := w.w.(http.Flusher)
	if !ok {
		return
	}
	if w.code == 0 {
		w.code = http.StatusOK
	}
	flusher.Flush()
}

func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.w.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	conn, rw, err := hijacker.Hijack()
	if err == nil && w.code == 0 {
		w.code = http.StatusSwitchingProtocols
	}
	return conn, rw, err
}

func (w *responseWriter) ReadFrom(r io.Reader) (int64, error) {
	readerFrom, ok := w.w.(io.ReaderFrom)
	if !ok {
		return io.Copy(writerOnly{w}, r)
	}
	if w.code == 0 {
		w.code = http.StatusOK
	}
	return readerFrom.ReadFrom(r)
}

type writerOnly struct {
	io.Writer
}

func (w *responseWriter) status() int {
	return w.code
}
