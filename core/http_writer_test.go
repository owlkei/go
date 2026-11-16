package core

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

var errWriterHijack = errors.New("hijack probe")

type wrFullWriter struct {
	*httptest.ResponseRecorder
	flushes   int
	readFroms int
	hijacks   int
	deadlines []time.Time
}

func (w *wrFullWriter) Flush() {
	w.flushes++
	w.ResponseRecorder.Flush()
}

func (w *wrFullWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijacks++
	return nil, nil, errWriterHijack
}

func (w *wrFullWriter) ReadFrom(r io.Reader) (int64, error) {
	w.readFroms++
	return io.Copy(w.ResponseRecorder, r)
}

func (w *wrFullWriter) SetWriteDeadline(t time.Time) error {
	w.deadlines = append(w.deadlines, t)
	return nil
}

func wrServe(t *testing.T, w http.ResponseWriter, handler func(c *ctx.HTTPContext) any) {
	t.Helper()
	app := lcNewApp(t, lcConfig{noGlobalFilters: true})
	lcSetHandler(handler)
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/bare", nil))
}

func TestResponseWriter_TracksWriteAndWriteHeader(t *testing.T) {
	var found bool
	var before, afterHeader, afterSecond int
	wrServe(t, httptest.NewRecorder(), func(c *ctx.HTTPContext) any {
		rw, ok := c.ResponseWriter.(*responseWriter)
		if !ok {
			return nil
		}
		found = true
		before = rw.status()
		rw.WriteHeader(http.StatusNoContent)
		afterHeader = rw.status()
		rw.WriteHeader(http.StatusTeapot)
		afterSecond = rw.status()
		return nil
	})

	if !found {
		t.Fatal(test.DiffMessage("<other writer>", "*responseWriter", "E4: layers receive the tracking writer"))
	}
	if before != 0 {
		t.Error(test.DiffMessage(before, 0, "E4: status() is 0 before anything is written"))
	}
	if afterHeader != http.StatusNoContent {
		t.Error(test.DiffMessage(afterHeader, http.StatusNoContent, "E4: status() is the code of WriteHeader"))
	}
	if afterSecond != http.StatusNoContent {
		t.Error(test.DiffMessage(afterSecond, http.StatusNoContent, "E4: a second WriteHeader does not change the status sent"))
	}
}

func TestResponseWriter_WriteCountsAsStatus200(t *testing.T) {
	var after int
	var n int
	w := httptest.NewRecorder()
	wrServe(t, w, func(c *ctx.HTTPContext) any {
		rw, ok := c.ResponseWriter.(*responseWriter)
		if !ok {
			return nil
		}
		n, _ = rw.Write([]byte("abc"))
		after = rw.status()
		return nil
	})

	if after != http.StatusOK {
		t.Error(test.DiffMessage(after, http.StatusOK, "E4: a write without WriteHeader sends 200"))
	}
	if n != 3 {
		t.Error(test.DiffMessage(n, 3, "E4: Write returns the count of the underlying writer"))
	}
	if !strings.HasPrefix(w.Body.String(), "abc") {
		t.Error(test.DiffMessage(w.Body.String(), "abc...", "E4: Write reaches the underlying writer"))
	}
}

func TestResponseWriter_InformationalStatusDoesNotCount(t *testing.T) {
	var found bool
	var after int
	w := &lcStrictWriter{}
	wrServe(t, w, func(c *ctx.HTTPContext) any {
		rw, ok := c.ResponseWriter.(*responseWriter)
		if !ok {
			return nil
		}
		found = true
		rw.WriteHeader(http.StatusEarlyHints)
		after = rw.status()
		return nil
	})

	if !found {
		t.Fatal(test.DiffMessage("<other writer>", "*responseWriter", "E4: layers receive the tracking writer"))
	}
	if after != 0 {
		t.Error(test.DiffMessage(after, 0, "E4: a status under 200 is not a response"))
	}
	if len(w.infos) != 1 || w.infos[0] != http.StatusEarlyHints {
		t.Error(test.DiffMessage(w.infos, []int{http.StatusEarlyHints}, "E4: the informational status still reaches the underlying writer"))
	}
}

func TestResponseWriter_MiddlewareAndHandlerShareTheWriter(t *testing.T) {
	app := lcNewApp(t, lcConfig{noGlobalFilters: true})
	var fromMiddleware, fromHandler http.ResponseWriter
	lcSetMiddleware("g1", func(w http.ResponseWriter, _ *http.Request, next ctx.Next) {
		fromMiddleware = w
		next()
	})
	lcSetHandler(func(c *ctx.HTTPContext) any {
		fromHandler = c.ResponseWriter
		return "H"
	})
	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/bare", nil))

	if _, ok := fromMiddleware.(*responseWriter); !ok {
		t.Fatal(test.DiffMessage(fromMiddleware, "*responseWriter", "E4: middlewares receive the tracking writer"))
	}
	if fromMiddleware != fromHandler {
		t.Error(test.DiffMessage(fromHandler, fromMiddleware, "E4: every layer receives the same writer"))
	}
}

func TestResponseWriter_UnwrapWorksWithResponseController(t *testing.T) {
	underlying := &wrFullWriter{ResponseRecorder: httptest.NewRecorder()}
	deadline := time.Unix(1700000000, 0)
	var flushErr, deadlineErr error
	var unwrapped http.ResponseWriter
	wrServe(t, underlying, func(c *ctx.HTTPContext) any {
		if rw, ok := c.ResponseWriter.(*responseWriter); ok {
			unwrapped = rw.Unwrap()
		}
		rc := http.NewResponseController(c.ResponseWriter)
		flushErr = rc.Flush()
		deadlineErr = rc.SetWriteDeadline(deadline)
		return nil
	})

	if unwrapped != http.ResponseWriter(underlying) {
		t.Error(test.DiffMessage(unwrapped, underlying, "Unwrap returns the server's writer"))
	}
	if flushErr != nil || underlying.flushes < 1 {
		t.Error(test.DiffMessage([]any{flushErr, underlying.flushes}, []any{nil, "at least 1"}, "http.ResponseController reaches Flush of the server's writer"))
	}
	if deadlineErr != nil || len(underlying.deadlines) != 1 || !underlying.deadlines[0].Equal(deadline) {
		t.Error(test.DiffMessage([]any{deadlineErr, underlying.deadlines}, []any{nil, []time.Time{deadline}}, "http.ResponseController reaches SetWriteDeadline of the server's writer"))
	}
}

func TestResponseWriter_FlushHijackReadFromForward(t *testing.T) {
	underlying := &wrFullWriter{ResponseRecorder: httptest.NewRecorder()}
	var flusherOK, hijackerOK, readerFromOK bool
	var hijackErr error
	var copied int64
	var status int
	wrServe(t, underlying, func(c *ctx.HTTPContext) any {
		flusher, ok := c.ResponseWriter.(http.Flusher)
		flusherOK = ok
		if ok {
			flusher.Flush()
		}
		hijacker, ok := c.ResponseWriter.(http.Hijacker)
		hijackerOK = ok
		if ok {
			_, _, hijackErr = hijacker.Hijack()
		}
		readerFrom, ok := c.ResponseWriter.(io.ReaderFrom)
		readerFromOK = ok
		if ok {
			copied, _ = readerFrom.ReadFrom(strings.NewReader("abc"))
		}
		if rw, ok := c.ResponseWriter.(*responseWriter); ok {
			status = rw.status()
		}
		return nil
	})

	if !flusherOK || !hijackerOK || !readerFromOK {
		t.Fatal(test.DiffMessage([]bool{flusherOK, hijackerOK, readerFromOK}, []bool{true, true, true}, "the writer implements http.Flusher, http.Hijacker and io.ReaderFrom"))
	}
	if underlying.flushes < 1 {
		t.Error(test.DiffMessage(underlying.flushes, "at least 1", "Flush forwards when the server supports it"))
	}
	if underlying.hijacks != 1 || hijackErr != errWriterHijack {
		t.Error(test.DiffMessage([]any{underlying.hijacks, hijackErr}, []any{1, errWriterHijack}, "Hijack forwards to the server's writer"))
	}
	if underlying.readFroms != 1 || copied != 3 {
		t.Error(test.DiffMessage([]any{underlying.readFroms, copied}, []any{1, int64(3)}, "ReadFrom forwards to the server's writer"))
	}
	if status != http.StatusOK {
		t.Error(test.DiffMessage(status, http.StatusOK, "E4: ReadFrom counts as a response"))
	}
	if !strings.HasPrefix(underlying.Body.String(), "abc") {
		t.Error(test.DiffMessage(underlying.Body.String(), "abc...", "ReadFrom writes the bytes"))
	}
}

func TestResponseWriter_HijackWithoutSupportReturnsAnError(t *testing.T) {
	var hijackerOK bool
	var err error
	wrServe(t, &lcStrictWriter{}, func(c *ctx.HTTPContext) any {
		hijacker, ok := c.ResponseWriter.(http.Hijacker)
		hijackerOK = ok
		if ok {
			_, _, err = hijacker.Hijack()
		}
		return nil
	})

	if !hijackerOK {
		t.Fatal(test.DiffMessage(false, true, "the writer implements http.Hijacker"))
	}
	if err == nil {
		t.Error(test.DiffMessage(err, "an error", "Hijack on a server writer that cannot hijack returns an error"))
	}
}

func TestResponseWriter_FlushWithoutSupportDoesNothing(t *testing.T) {
	var flusherOK bool
	wrServe(t, &lcStrictWriter{}, func(c *ctx.HTTPContext) any {
		if flusher, ok := c.ResponseWriter.(http.Flusher); ok {
			flusherOK = true
			flusher.Flush()
		}
		return nil
	})

	if !flusherOK {
		t.Error(test.DiffMessage(false, true, "the writer implements http.Flusher and does not panic without support"))
	}
}

func TestResponseWriter_ServeFileKeepsReadFrom(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(file, []byte("file body"), 0o644); err != nil {
		t.Fatal(err)
	}
	underlying := &wrFullWriter{ResponseRecorder: httptest.NewRecorder()}
	wrServe(t, underlying, func(c *ctx.HTTPContext) any {
		http.ServeFile(c.ResponseWriter, c.Request, file)
		return nil
	})

	if underlying.readFroms < 1 {
		t.Error(test.DiffMessage(underlying.readFroms, "at least 1", "http.ServeFile reaches ReadFrom of the server's writer (sendfile)"))
	}
	if !strings.HasPrefix(underlying.Body.String(), "file body") {
		t.Error(test.DiffMessage(underlying.Body.String(), "file body...", "http.ServeFile writes the file"))
	}
}

func TestResponseWriter_ResetDropsLateWrites(t *testing.T) {
	var late http.ResponseWriter
	w := httptest.NewRecorder()
	wrServe(t, w, func(c *ctx.HTTPContext) any {
		late = c.ResponseWriter
		return "H"
	})
	bodyBefore, codeBefore := w.Body.String(), w.Code

	var rec any
	func() {
		defer func() { rec = recover() }()
		_, _ = late.Write([]byte("late"))
		late.WriteHeader(http.StatusTeapot)
		if flusher, ok := late.(http.Flusher); ok {
			flusher.Flush()
		}
	}()

	if rec != nil {
		t.Error(test.DiffMessage(rec, nil, "a late write on an idle exchange does not panic"))
	}
	if w.Body.String() != bodyBefore || w.Code != codeBefore {
		t.Error(test.DiffMessage([]any{w.Code, w.Body.String()}, []any{codeBefore, bodyBefore}, "a late write on an idle exchange is dropped"))
	}
}
