package core

import (
	"errors"
	"html"
	stdHTTP "net/http"
	"strconv"

	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/render"
)

const varyHeader = "Vary"

func (http *HTTP) writeEarlyError(ex *httpExchange, e *exception.Exception, body []byte) int {
	c := &ex.HTTPContext
	if http.render != nil {
		h := ex.writer.Header()
		h[varyHeader] = append(h[varyHeader], "Accept")
		if acceptsHTML(c.Request) {
			return http.writeError(c, &ex.writer, e, true)
		}
	}
	if body != nil {
		code := e.Code()
		writeJSONError(&ex.writer, code, body)
		return code
	}
	return http.writeError(c, &ex.writer, e, false)
}

func (http *HTTP) writeError(c *ctx.HTTPContext, w stdHTTP.ResponseWriter, e *exception.Exception, isHTML bool) int {
	code, status, message := clientText(e)
	if http == nil {
		writeJSONError(w, code, errorJSON(e, code, status, message))
		return code
	}
	http.logClientText(c, e, code)
	if isHTML && http.render != nil {
		http.writeHTMLError(c, w, code, status, message)
		return code
	}
	writeJSONError(w, code, errorJSON(e, code, status, message))
	return code
}

func (http *HTTP) logClientText(c *ctx.HTTPContext, e *exception.Exception, code int) {
	cause := e.Unwrap()
	if code == stdHTTP.StatusInternalServerError {
		message := e.Message()
		if cause == nil && (message == "" || message == internalServerErrorException.Message()) {
			return
		}
		http.logger.From(c.Context()).Error("InternalServerError", "method", c.Method, "path", c.URL.Path, "message", message, "cause", cause)
		return
	}
	if cause == nil {
		return
	}
	http.logger.From(c.Context()).Debug("ExceptionCause", "method", c.Method, "path", c.URL.Path, "code", code, "cause", cause)
}

func (http *HTTP) writeHTMLError(c *ctx.HTTPContext, w stdHTTP.ResponseWriter, code int, status, message string) {
	data := map[string]any{"Code": code, "Status": status, "Message": message, "RequestID": c.ID()}
	buf, err := http.renderErrorPage(c, data)
	if err != nil {
		var fileErr *render.FileError
		switch {
		case errors.As(err, &fileErr):
			http.logger.From(c.Context()).Error("RenderReloadFailed", "file", fileErr.File, "cause", fileErr.Err)
		case !errors.Is(err, render.ErrNoErrorPage):
			http.logger.From(c.Context()).Error("RenderFailed", "template", "error", "cause", err)
		}
		writeHTMLBody(w, code, http.builtInPage(c, code, status, message))
		return
	}
	if rw, ok := w.(*responseWriter); ok {
		rw.placeNonce(buf.Nonce())
	}
	writeHTMLBody(w, code, buf.Bytes())
	http.render.Release(buf)
}

func writeHTMLBody(w stdHTTP.ResponseWriter, code int, body []byte) {
	h := w.Header()
	delete(h, "Content-Length")
	delete(h, "Content-Encoding")
	delete(h, "Etag")
	delete(h, "Last-Modified")
	h["Content-Type"] = textHTMLValue
	setDefaultCacheControl(h)
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

func builtInErrorPage(code int, status, message string) []byte {
	heading := strconv.Itoa(code) + " " + html.EscapeString(status)
	page := make([]byte, 0, 192+2*len(heading)+len(message))
	page = append(page, "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n<title>"...)
	page = append(page, heading...)
	page = append(page, "</title>\n</head>\n<body>\n<h1>"...)
	page = append(page, heading...)
	page = append(page, "</h1>\n<p>"...)
	page = append(page, html.EscapeString(message)...)
	return append(page, "</p>\n</body>\n</html>\n"...)
}

func acceptsHTML(r *stdHTTP.Request) bool {
	for _, v := range r.Header["Accept"] {
		if containsTextHTML(v) {
			return true
		}
	}
	return false
}

func containsTextHTML(v string) bool {
	const want = "text/html"
	for i := 0; i+len(want) <= len(v); i++ {
		j := 0
		for j < len(want) && lowerASCII(v[i+j]) == want[j] {
			j++
		}
		if j == len(want) {
			return true
		}
	}
	return false
}

func lowerASCII(b byte) byte {
	if 'A' <= b && b <= 'Z' {
		return b + 'a' - 'A'
	}
	return b
}
