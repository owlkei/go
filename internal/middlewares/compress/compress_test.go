package compress_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	. "go.owlkei.com/owlkei/internal/middlewares/compress"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/exception"
	"go.owlkei.com/owlkei/internal/core"
	"go.owlkei.com/owlkei/internal/inspect"
	"go.owlkei.com/owlkei/internal/test"
)

func TestCompress_RoundTrip_2KiB(t *testing.T) {
	body := payload(2048)
	app := terminalApp(t, Compress{}, writeJSON(body))
	w := serve(app, gzipRequest(http.MethodGet))
	expectGzip(t, w, body, "2 KiB in one write")
	expectSmaller(t, w, len(body), "2 KiB of JSON")
}

func TestCompress_RoundTrip_1MiB(t *testing.T) {
	body := payload(1 << 20)
	app := terminalApp(t, Compress{}, writeJSON(body))
	big := serve(app, gzipRequest(http.MethodGet))
	expectGzip(t, big, body, "1 MiB in one write")
	expectSmaller(t, big, len(body), "1 MiB of JSON")

	chunked := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		for off := 0; off < len(body); off += 4096 {
			_, _ = w.Write(body[off:min(off+4096, len(body))])
		}
	})
	expectGzip(t, serve(chunked, gzipRequest(http.MethodGet)), body, "1 MiB in 4 KiB writes")
}

func TestCompress_RoundTrip_ManyTinyWrites(t *testing.T) {
	body := payload(3000)
	app := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		for i := range body {
			_, _ = w.Write(body[i : i+1])
		}
	})
	expectGzip(t, serve(app, gzipRequest(http.MethodGet)), body, "3000 one-byte writes")
}

func TestCompress_RoundTrip_ExplicitWriteHeaderFirst(t *testing.T) {
	body := payload(4096)
	app := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
	expectGzip(t, serve(app, gzipRequest(http.MethodGet)), body, "WriteHeader before the body: the decision waits for the first write")
}

func TestCompress_RoundTrip_ThroughRoute(t *testing.T) {
	app := core.New(&core.AppOptions{Logger: silentLogger{}})
	app.BindGlobalMiddlewares(Compress{})
	app.Create(owlkei.ModuleBuilder().Controllers(bigController{}).Build())
	w := serve(app, request(http.MethodGet, "/big", "Accept-Encoding", "gzip"))
	res, body := resultOf(t, w)
	if res.Header.Get("Content-Encoding") != "gzip" {
		t.Fatal(test.DiffMessage(res.Header.Get("Content-Encoding"), "gzip", "Z3: a JSON response the framework writes is compressed"))
	}
	var parsed map[string]string
	if err := json.Unmarshal(gunzip(t, body), &parsed); err != nil || len(parsed["data"]) != 6000 {
		t.Error(test.DiffMessage(len(parsed["data"]), 6000, "Z6: the framework's JSON round-trips"))
	}
}

func TestCompress_Vary(t *testing.T) {
	app := terminalApp(t, Compress{}, writeJSON(payload(4096)))
	res, _ := resultOf(t, serve(app, gzipRequest(http.MethodGet)))
	if !hasVaryToken(res.Header, "Accept-Encoding") {
		t.Error(test.DiffMessage(res.Header.Values("Vary"), "<Accept-Encoding>", "Z1: a compressed response varies on Accept-Encoding"))
	}
	small := terminalApp(t, Compress{}, writeJSON(payload(10)))
	res, _ = resultOf(t, serve(small, gzipRequest(http.MethodGet)))
	if !hasVaryToken(res.Header, "Accept-Encoding") {
		t.Error(test.DiffMessage(res.Header.Values("Vary"), "<Accept-Encoding>", "Z1: a response that stays small varies too"))
	}
	res, _ = resultOf(t, serve(small, request(http.MethodGet, "/any")))
	if !hasVaryToken(res.Header, "Accept-Encoding") {
		t.Error(test.DiffMessage(res.Header.Values("Vary"), "<Accept-Encoding>", "Z1: a request that does not accept gzip still varies"))
	}
}

func TestCompress_Vary_AddedToExisting(t *testing.T) {
	body := payload(4096)
	app := newApp(t, varyBefore{Value: "Cookie"}, Compress{}, terminalMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		writeJSON(body)(w, r)
	}))
	res, _ := resultOf(t, serve(app, gzipRequest(http.MethodGet)))
	for _, token := range []string{"Cookie", "Origin", "Accept-Encoding"} {
		if !hasVaryToken(res.Header, token) {
			t.Error(test.DiffMessage(res.Header.Values("Vary"), "<"+token+">", "Z1: Accept-Encoding is added to an existing Vary"))
		}
	}
}

func TestCompress_NotCandidate(t *testing.T) {
	body := payload(4096)
	cases := []struct {
		name string
		r    *http.Request
	}{
		{"HEAD", gzipRequest(http.MethodHead)},
		{"Range", gzipRequest(http.MethodGet, "Range", "bytes=0-10")},
		{"no Accept-Encoding", request(http.MethodGet, "/any")},
		{"deflate only", request(http.MethodGet, "/any", "Accept-Encoding", "deflate")},
		{"identity", request(http.MethodGet, "/any", "Accept-Encoding", "identity")},
		{"br only", request(http.MethodGet, "/any", "Accept-Encoding", "br")},
		{"gzip;q=0", request(http.MethodGet, "/any", "Accept-Encoding", "gzip;q=0")},
		{"gzip; q=0.0", request(http.MethodGet, "/any", "Accept-Encoding", "deflate, gzip; q=0.0")},
		{"gzip after byte 256", request(http.MethodGet, "/any", "Accept-Encoding", strings.Repeat("x", 260)+", gzip")},
		{"empty", request(http.MethodGet, "/any", "Accept-Encoding", "")},
	}
	for _, c := range cases {
		app := terminalApp(t, Compress{}, writeJSON(body))
		w := serve(app, c.r)
		expectPlain(t, w, body, "Z2: "+c.name)
		if !hasVaryToken(w.Result().Header, "Accept-Encoding") {
			t.Error(test.DiffMessage(w.Result().Header.Values("Vary"), "<Accept-Encoding>", "Z1: Vary also on "+c.name))
		}
	}
}

func TestCompress_Upgrade_PassesThroughUntouched(t *testing.T) {
	body := payload(4096)
	app := terminalApp(t, Compress{}, writeJSON(body))
	w := serve(app, gzipRequest(http.MethodGet, "Upgrade", "websocket", "Connection", "Upgrade"))
	expectPlain(t, w, body, "G5, Z2: an upgrade request")
}

func TestCompress_Candidate_AcceptEncodingForms(t *testing.T) {
	body := payload(4096)
	for _, value := range []string{"gzip", "GZIP", "GZip", "gzip, deflate", "deflate, gzip;q=0.5", "gzip;q=1", "gzip;q=0.001", "deflate, br, gzip", "br;q=1.0, gzip;q=0.8", strings.Repeat("a", 240) + ",gzip"} {
		app := terminalApp(t, Compress{}, writeJSON(body))
		w := serve(app, request(http.MethodGet, "/any", "Accept-Encoding", value))
		expectGzip(t, w, body, "Z2: Accept-Encoding "+value[:min(len(value), 40)])
	}
}

func TestCompress_SkipsStatus(t *testing.T) {
	body := payload(4096)
	for _, code := range []int{http.StatusNoContent, http.StatusNotModified, http.StatusPartialContent} {
		app := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			if code == http.StatusPartialContent {
				_, _ = w.Write(body)
			}
		})
		w := serve(app, gzipRequest(http.MethodGet))
		res, got := resultOf(t, w)
		if res.Header.Get("Content-Encoding") != "" {
			t.Error(test.DiffMessage(res.Header.Get("Content-Encoding"), "", fmt.Sprintf("Z3: status %d is never compressed", code)))
		}
		if code == http.StatusPartialContent && !bytes.Equal(got, body) {
			t.Error(test.DiffMessage(len(got), len(body), "Z3: a 206 body is sent as written"))
		}
		if res.StatusCode != code {
			t.Error(test.DiffMessage(res.StatusCode, code, "Z3: the status is kept"))
		}
	}
}

func TestCompress_ErrorStatusStillCompressed(t *testing.T) {
	body := payload(4096)
	for _, code := range []int{http.StatusBadRequest, http.StatusNotFound, http.StatusInternalServerError} {
		app := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			_, _ = w.Write(body)
		})
		w := serve(app, gzipRequest(http.MethodGet))
		expectGzip(t, w, body, fmt.Sprintf("Z3: status %d", code))
		if w.Code != code {
			t.Error(test.DiffMessage(w.Code, code, "K1: the status is kept"))
		}
	}
}

func TestCompress_SkipsEncoded(t *testing.T) {
	body := payload(4096)
	app := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "br")
		_, _ = w.Write(body)
	})
	w := serve(app, gzipRequest(http.MethodGet))
	res, got := resultOf(t, w)
	if res.Header.Get("Content-Encoding") != "br" || !bytes.Equal(got, body) {
		t.Error(test.DiffMessage(res.Header.Get("Content-Encoding"), "br", "Z3: an already encoded response is left as is"))
	}
}

func TestCompress_SkipsContentRange(t *testing.T) {
	body := payload(4096)
	app := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Range", "bytes 0-4095/8192")
		_, _ = w.Write(body)
	})
	expectPlain(t, serve(app, gzipRequest(http.MethodGet)), body, "Z3: a response with Content-Range")
}

func TestCompress_SkipsType(t *testing.T) {
	body := payload(4096)
	for _, contentType := range []string{"image/png", "image/jpeg", "text/event-stream", "text/event-stream; charset=utf-8", "application/octet-stream", "video/mp4", "application/zip", "application/pdf", "audio/mpeg", "application/gzip", "font/woff2"} {
		app := terminalApp(t, Compress{}, writeType(contentType, body))
		expectPlain(t, serve(app, gzipRequest(http.MethodGet)), body, "Z4: "+contentType)
	}
}

func TestCompress_CompressibleTypes(t *testing.T) {
	body := payload(4096)
	for _, contentType := range []string{
		"text/plain", "text/plain; charset=utf-8", "text/html", "text/css", "text/csv", "application/json", "application/json; charset=utf-8", "Application/JSON",
		"application/javascript", "application/xml", "application/wasm", "image/svg+xml", "application/ld+json", "application/atom+xml", "application/vnd.api+json", "application/soap+xml",
	} {
		app := terminalApp(t, Compress{}, writeType(contentType, body))
		expectGzip(t, serve(app, gzipRequest(http.MethodGet)), body, "Z4: "+contentType)
	}
}

func TestCompress_NoTransform(t *testing.T) {
	body := payload(4096)
	for _, value := range []string{"public, no-transform", "no-transform", "max-age=60, no-transform, private"} {
		app := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", value)
			writeJSON(body)(w, r)
		})
		expectPlain(t, serve(app, gzipRequest(http.MethodGet)), body, "Z3: Cache-Control "+value)
	}
	app := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=60")
		writeJSON(body)(w, r)
	})
	expectGzip(t, serve(app, gzipRequest(http.MethodGet)), body, "Z3: other Cache-Control directives do not stop compression")
}

func TestCompress_BelowMinSize_Untouched(t *testing.T) {
	body := payload(500)
	declared := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "500")
		_, _ = w.Write(body)
	})
	w := serve(declared, gzipRequest(http.MethodGet))
	expectPlain(t, w, body, "Z3: a declared Content-Length below MinSize")
	if got := w.Result().Header.Get("Content-Length"); got != "500" {
		t.Error(test.DiffMessage(got, "500", "Z3: the declared Content-Length is kept"))
	}

	unknown := terminalApp(t, Compress{}, writeJSON(body))
	w = serve(unknown, gzipRequest(http.MethodGet))
	expectPlain(t, w, body, "Z5: an unknown length that ends below MinSize")
	if got := w.Result().Header.Get("Content-Length"); got != "500" {
		t.Error(test.DiffMessage(got, "500", "Z5: sent as written, with its Content-Length"))
	}

	split := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		for i := 0; i < 5; i++ {
			_, _ = w.Write(body[i*100 : (i+1)*100])
		}
	})
	w = serve(split, gzipRequest(http.MethodGet))
	expectPlain(t, w, body, "Z5: many small writes that end below MinSize")
	if got := w.Result().Header.Get("Content-Length"); got != "500" {
		t.Error(test.DiffMessage(got, "500", "Z5: Content-Length of the held-back bytes"))
	}
}

func TestCompress_MinSize_Boundary(t *testing.T) {
	for _, c := range []struct {
		min, size int
		gzipped   bool
	}{
		{0, 1023, false}, {0, 1024, true}, {0, 1025, true}, {100, 99, false}, {100, 100, true}, {1, 1, true}, {5000, 4999, false}, {5000, 5000, true},
	} {
		body := payload(c.size)
		app := terminalApp(t, Compress{MinSize: c.min}, writeJSON(body))
		w := serve(app, gzipRequest(http.MethodGet))
		if c.gzipped {
			expectGzip(t, w, body, fmt.Sprintf("Z9: MinSize %d, %d bytes", c.min, c.size))
		} else {
			expectPlain(t, w, body, fmt.Sprintf("Z9: MinSize %d, %d bytes", c.min, c.size))
		}
	}
}

func TestCompress_MinSize_DeclaredLength(t *testing.T) {
	body := payload(2000)
	app := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "2000")
		_, _ = w.Write(body)
	})
	expectGzip(t, serve(app, gzipRequest(http.MethodGet)), body, "Z3: a declared Content-Length at or above MinSize is compressed")
}

func TestCompress_DetectsContentType(t *testing.T) {
	html := append([]byte("<!DOCTYPE html><html><body>"), bytes.Repeat([]byte("<p>owlkei owlkei owlkei</p>"), 100)...)
	app := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(html) })
	w := serve(app, gzipRequest(http.MethodGet))
	expectGzip(t, w, html, "Z4: a missing Content-Type is detected from the bytes")
	if got := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Error(test.DiffMessage(got, "text/html; charset=utf-8", "Z4: the detected type is set as net/http would"))
	}

	text := payload(3000)
	app = terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(text) })
	w = serve(app, gzipRequest(http.MethodGet))
	expectGzip(t, w, text, "Z4: detected plain text")
	if got := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Error(test.DiffMessage(got, "text/plain; charset=utf-8", "Z4: the detected type is set"))
	}

	png := append([]byte("\x89PNG\r\n\x1a\n"), payload(3000)...)
	app = terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(png) })
	w = serve(app, gzipRequest(http.MethodGet))
	expectPlain(t, w, png, "Z4: a detected image is not compressed")
	if got := w.Result().Header.Get("Content-Type"); got != "image/png" {
		t.Error(test.DiffMessage(got, "image/png", "Z4: the detected type is set even when not compressed"))
	}
}

func TestCompress_WeakensETag(t *testing.T) {
	body := payload(4096)
	for etag, want := range map[string]string{`"abc"`: `W/"abc"`, `W/"abc"`: `W/"abc"`, `"a-b"`: `W/"a-b"`} {
		app := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("ETag", etag)
			writeJSON(body)(w, r)
		})
		res, _ := resultOf(t, serve(app, gzipRequest(http.MethodGet)))
		if got := res.Header.Get("ETag"); got != want {
			t.Error(test.DiffMessage(got, want, "Z6: ETag "+etag+" of a compressed response"))
		}
	}
	small := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"abc"`)
		writeJSON(payload(100))(w, r)
	})
	res, _ := resultOf(t, serve(small, gzipRequest(http.MethodGet)))
	if got := res.Header.Get("ETag"); got != `"abc"` {
		t.Error(test.DiffMessage(got, `"abc"`, "Z6: an uncompressed response keeps its strong ETag"))
	}
}

func TestCompress_RemovesLengthAndAcceptRanges(t *testing.T) {
	body := payload(5000)
	app := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "5000")
		w.Header().Set("Accept-Ranges", "bytes")
		_, _ = w.Write(body)
	})
	w := serve(app, gzipRequest(http.MethodGet))
	expectGzip(t, w, body, "Z6")
	if got := w.Result().Header.Values("Accept-Ranges"); len(got) != 0 {
		t.Error(test.DiffMessage(got, []string{}, "Z6: Accept-Ranges is removed from a compressed response"))
	}
}

func TestCompress_OneGzipStreamAtBestSpeed(t *testing.T) {
	body := payload(100000)
	app := terminalApp(t, Compress{}, writeJSON(body))
	_, compressed := resultOf(t, serve(app, gzipRequest(http.MethodGet)))
	reader := bytes.NewReader(compressed)
	zr, err := gzip.NewReader(reader)
	if err != nil {
		t.Fatal(err)
	}
	zr.Multistream(false)
	if _, err := io.Copy(io.Discard, zr); err != nil {
		t.Fatal(err)
	}
	if reader.Len() != 0 {
		t.Error(test.DiffMessage(reader.Len(), 0, "Z6: the body is one gzip stream, nothing follows it"))
	}
	if len(compressed) < 10 || compressed[8] != 4 {
		t.Error(test.DiffMessage(compressed[8], byte(4), "Z6: gzip.BestSpeed (XFL 4)"))
	}
}

func TestCompress_Flush(t *testing.T) {
	rec := httptest.NewRecorder()
	var flushedBytes int
	var flushedEncoding string
	var wasFlushed bool
	app := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello"))
		w.(http.Flusher).Flush()
		flushedBytes = rec.Body.Len()
		flushedEncoding = rec.Header().Get("Content-Encoding")
		wasFlushed = rec.Flushed
		_, _ = w.Write([]byte(" world"))
	})
	app.ServeHTTP(rec, gzipRequest(http.MethodGet))
	if flushedEncoding != "gzip" || flushedBytes == 0 || !wasFlushed {
		t.Error(test.DiffMessage([]any{flushedEncoding, flushedBytes > 0, wasFlushed}, []any{"gzip", true, true}, "Z7: Flush decides with what is buffered, skips MinSize, flushes the stream and the connection"))
	}
	_, body := resultOf(t, rec)
	if got := string(gunzip(t, body)); got != "hello world" {
		t.Error(test.DiffMessage(got, "hello world", "Z7: the bytes after the flush follow in the same stream"))
	}
}

func TestCompress_Flush_NotCompressible(t *testing.T) {
	rec := httptest.NewRecorder()
	var flushedBytes int
	app := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: 1\n\n"))
		w.(http.Flusher).Flush()
		flushedBytes = rec.Body.Len()
	})
	app.ServeHTTP(rec, gzipRequest(http.MethodGet))
	if flushedBytes != len("data: 1\n\n") || rec.Header().Get("Content-Encoding") != "" || !rec.Flushed {
		t.Error(test.DiffMessage(flushedBytes, len("data: 1\n\n"), "Z7: an event stream is forwarded and flushed untouched"))
	}
}

func TestCompress_ServeFileKeepsReadFrom(t *testing.T) {
	fileRoot = filepath.Join(t.TempDir(), "public")
	if err := os.MkdirAll(fileRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{"logo.png": []byte("hello"), "small.html": []byte("<p>hi</p>"), "big.html": bytes.Repeat([]byte("<p>owlkei</p>"), 500)} {
		if err := os.WriteFile(filepath.Join(fileRoot, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	app := core.New(&core.AppOptions{Logger: silentLogger{}, HTTP: &core.HTTPOptions{ServeDirs: []string{fileRoot}}})
	app.BindGlobalMiddlewares(Compress{})
	app.Create(owlkei.ModuleBuilder().Controllers(fileController{}).Build())

	for _, name := range []string{"logo.png", "small.html"} {
		w := &readerFromWriter{header: http.Header{}}
		r := request(http.MethodGet, "/files/"+name, "Accept-Encoding", "gzip")
		app.ServeHTTP(w, r)
		if w.readFrom == 0 {
			t.Error(test.DiffMessage(w.readFrom, "> 0", "Z7: an uncompressed file response forwards ReadFrom so sendfile is kept: "+name))
		}
		want, _ := os.ReadFile(filepath.Join(fileRoot, name))
		if !bytes.Equal(w.body.Bytes(), want) || w.header.Get("Content-Encoding") != "" {
			t.Error(test.DiffMessage(w.body.String(), string(want), "Z7: the file is sent as it is: "+name))
		}
	}

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, request(http.MethodGet, "/files/big.html", "Accept-Encoding", "gzip"))
	want, _ := os.ReadFile(filepath.Join(fileRoot, "big.html"))
	expectGzip(t, rec, want, "Z3: a large file of a compressible type is compressed")
}

func TestCompress_NoWriterFree_Uncompressed(t *testing.T) {
	body := payload(4096)
	acquired := make(chan struct{}, 8)
	release := make(chan struct{})
	app := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
		if r.Header.Get("X-Hold") != "" {
			acquired <- struct{}{}
			<-release
		}
	})

	recorders := make([]*httptest.ResponseRecorder, 8)
	var wg sync.WaitGroup
	for i := range recorders {
		recorders[i] = httptest.NewRecorder()
		wg.Add(1)
		go func() {
			defer wg.Done()
			app.ServeHTTP(recorders[i], gzipRequest(http.MethodGet, "X-Hold", "1"))
		}()
	}
	for range recorders {
		<-acquired
	}
	ninth := serve(app, gzipRequest(http.MethodGet))
	expectPlain(t, ninth, body, "Z8: a response that finds no writer free is sent uncompressed")
	if !hasVaryToken(ninth.Result().Header, "Accept-Encoding") {
		t.Error(test.DiffMessage(ninth.Result().Header.Values("Vary"), "<Accept-Encoding>", "Z1: Vary on an uncompressed response"))
	}
	close(release)
	wg.Wait()
	for i, rec := range recorders {
		expectGzip(t, rec, body, fmt.Sprintf("Z8: held response %d", i))
	}
	expectGzip(t, serve(app, gzipRequest(http.MethodGet)), body, "Z8: the writers are returned when the exchanges end")
}

func TestCompress_ErrorResponseAfterBind(t *testing.T) {
	app := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) { panic(exception.BadRequestException("bad input")) })
	w := serve(app, gzipRequest(http.MethodGet))
	res, body := resultOf(t, w)
	if res.StatusCode != http.StatusBadRequest || string(body) != `{"code":400,"error":"Bad Request","message":"bad input"}` {
		t.Error(test.DiffMessage(string(body), `{"code":400,"error":"Bad Request","message":"bad input"}`, "Z3: a small error response is sent as the HTTP layer wrote it"))
	}
	if !hasVaryToken(res.Header, "Accept-Encoding") {
		t.Error(test.DiffMessage(res.Header.Values("Vary"), "<Accept-Encoding>", "Z1: an error response varies too"))
	}

	long := strings.Repeat("x", 3000)
	app = terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) { panic(exception.BadRequestException(long)) })
	res, body = resultOf(t, serve(app, gzipRequest(http.MethodGet)))
	if res.StatusCode != http.StatusBadRequest {
		t.Error(test.DiffMessage(res.StatusCode, http.StatusBadRequest, "K1: the status of a compressed error"))
	}
	decoded := body
	if res.Header.Get("Content-Encoding") == "gzip" {
		decoded = gunzip(t, body)
	}
	var parsed struct {
		Code    int
		Message string
	}
	if err := json.Unmarshal(decoded, &parsed); err != nil || parsed.Code != 400 || parsed.Message != long {
		t.Error(test.DiffMessage(string(decoded[:min(len(decoded), 60)]), "a valid error JSON", "Z3: a large error response decodes to the HTTP layer's JSON"))
	}
}

func TestCompress_Concurrent(t *testing.T) {
	sizes := map[string]int{"500": 500, "1500": 1500, "20000": 20000, "300000": 300000}
	bodies := map[string][]byte{}
	for key, size := range sizes {
		bodies[key] = payload(size)
	}
	app := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(bodies[r.Header.Get("X-Size")])
	})
	var wg sync.WaitGroup
	for g := 0; g < 64; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for key, want := range bodies {
				for i := 0; i < 5; i++ {
					w := httptest.NewRecorder()
					app.ServeHTTP(w, gzipRequest(http.MethodGet, "X-Size", key))
					res := w.Result()
					body, _ := io.ReadAll(res.Body)
					if res.Header.Get("Content-Encoding") == "gzip" {
						zr, err := gzip.NewReader(bytes.NewReader(body))
						if err != nil {
							t.Errorf("Z8: a gzip response does not decode: %v", err)
							return
						}
						body, err = io.ReadAll(zr)
						if err != nil {
							t.Errorf("Z8: a gzip stream is broken: %v", err)
							return
						}
					}
					if !bytes.Equal(body, want) {
						t.Errorf("Z8: a response of %s bytes round-trips wrongly (got %d bytes)", key, len(body))
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}

func TestNewCompress_Panics_NegativeMinSize(t *testing.T) {
	text, ok := panicText(func() { Compress{MinSize: -1}.NewMiddleware() })
	if !ok || !strings.Contains(text, "invalid compress:") {
		t.Error(test.DiffMessage(text, "invalid compress: ...", "Z9, G4: a negative MinSize"))
	}
	for _, size := range []int{0, 1, 1024, 1 << 20} {
		if text, ok := panicText(func() { Compress{MinSize: size}.NewMiddleware() }); ok {
			t.Error(test.DiffMessage(text, "<no panic>", fmt.Sprintf("Z9: MinSize %d is valid", size)))
		}
	}
}

func TestCompress_OutsideAnApp_DegradesToNoop(t *testing.T) {
	mw := Compress{}.NewMiddleware()
	rec := httptest.NewRecorder()
	called := false
	mw.Use(gzipRequest(http.MethodGet), rec, func() { called = true })
	if !called {
		t.Error(test.DiffMessage(called, true, "K5: a middleware called outside an app calls next"))
	}
	if len(rec.Header()) != 0 {
		t.Error(test.DiffMessage(rec.Header(), http.Header{}, "K5: and does nothing else"))
	}
}

func TestCompress_KeepsRequestAndContext(t *testing.T) {
	app := newApp(t, probeMiddleware{Name: "before"}, Compress{}, probeMiddleware{Name: "after"})
	serve(app, gzipRequest(http.MethodGet))
	before, after := probeOf("before"), probeOf("after")
	if before.request == nil || before.request != after.request {
		t.Error(test.DiffMessage(after.request, before.request, "G2: the next layer receives the same *http.Request"))
	}
	if before.context != after.context {
		t.Error(test.DiffMessage(after.context, before.context, "G2: the next layer sees the same Context()"))
	}
}

func TestCompress_DoesNotReadBody(t *testing.T) {
	app := terminalApp(t, Compress{}, writeJSON(payload(4096)))
	r := gzipRequest(http.MethodPost)
	r.Body = failBody{t}
	serve(app, r)
}

func TestCompress_Snapshot(t *testing.T) {
	mw := Compress{}.NewMiddleware()
	s, ok := mw.(inspect.Snapshotter)
	if !ok {
		t.Fatal(test.DiffMessage(mw, "an inspect.Snapshotter", "I1: the compiled compress implements Snapshotter"))
	}
	snap := s.Snapshot()
	if snap.Name != "compress" {
		t.Error(test.DiffMessage(snap.Name, "compress", "I4: the snapshot name"))
	}
	for key, want := range map[string]int64{"min_size": 1024, "level": gzip.BestSpeed, "writer_cap": 8, "writers_in_use": 0, "compressed": 0, "uncompressed_no_writer": 0, "bytes_in": 0, "bytes_out": 0} {
		if got := field(snap, key); got != want {
			t.Error(test.DiffMessage(got, want, "I4: "+key))
		}
	}

	body := payload(20000)
	rec := httptest.NewRecorder()
	ex := newExchange(rec)
	mw.Use(gzipRequest(http.MethodGet), ex, func() {
		ex.Header().Set("Content-Type", "application/json")
		_, _ = ex.Write(body)
		if got := field(s.Snapshot(), "writers_in_use"); got != int64(1) {
			t.Error(test.DiffMessage(got, int64(1), "I4: a compressing response holds a writer"))
		}
	})
	ex.done()
	after := s.Snapshot()
	if got := field(after, "writers_in_use"); got != int64(0) {
		t.Error(test.DiffMessage(got, int64(0), "I4: the writer is returned when the exchange ends"))
	}
	if got := field(after, "compressed"); got != int64(1) {
		t.Error(test.DiffMessage(got, int64(1), "I4: responses compressed"))
	}
	if got := field(after, "bytes_in"); got != int64(len(body)) {
		t.Error(test.DiffMessage(got, int64(len(body)), "I4: bytes in"))
	}
	if got := field(after, "bytes_out"); got != int64(rec.Body.Len()) || rec.Body.Len() == 0 {
		t.Error(test.DiffMessage(got, int64(rec.Body.Len()), "I4: bytes out"))
	}
}

func TestCompress_Snapshot_NoWriterFree(t *testing.T) {
	mw := Compress{}.NewMiddleware()
	s := mw.(inspect.Snapshotter)
	body := payload(4096)
	held := make([]*exchange, 8)
	for i := range held {
		held[i] = newExchange(httptest.NewRecorder())
		mw.Use(gzipRequest(http.MethodGet), held[i], func() {
			held[i].Header().Set("Content-Type", "application/json")
			_, _ = held[i].Write(body)
		})
	}
	if got := field(s.Snapshot(), "writers_in_use"); got != int64(8) {
		t.Fatal(test.DiffMessage(got, int64(8), "Z8: eight writers in use"))
	}
	rec := httptest.NewRecorder()
	ninth := newExchange(rec)
	mw.Use(gzipRequest(http.MethodGet), ninth, func() {
		ninth.Header().Set("Content-Type", "application/json")
		_, _ = ninth.Write(body)
	})
	ninth.done()
	expectPlain(t, rec, body, "Z8: the ninth response")
	snap := s.Snapshot()
	if got := field(snap, "uncompressed_no_writer"); got != int64(1) {
		t.Error(test.DiffMessage(got, int64(1), "I4: responses sent uncompressed for lack of a writer"))
	}
	if got := field(snap, "writers_in_use"); got != int64(8) {
		t.Error(test.DiffMessage(got, int64(8), "Z8: the ninth takes no writer"))
	}
	for _, ex := range held {
		ex.done()
	}
	if got := field(s.Snapshot(), "writers_in_use"); got != int64(0) {
		t.Error(test.DiffMessage(got, int64(0), "I4: every writer returned"))
	}
}

func TestCompress_Allocations_NotCandidate(t *testing.T) {
	body := payload(2000)
	base := terminalApp(t, Compress{}, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) })
	plain := newApp(t, terminalMiddleware(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) }))
	r := request(http.MethodGet, "/any")
	w := &discardWriter{header: http.Header{}}
	with := testing.AllocsPerRun(200, func() { clear(w.header); base.ServeHTTP(w, r) })
	without := testing.AllocsPerRun(200, func() { clear(w.header); plain.ServeHTTP(w, r) })
	if with > without {
		t.Error(test.DiffMessage(with, without, "Z2: a request that is not a candidate adds no allocation"))
	}
}

func TestCompress_Allocations_SteadyState(t *testing.T) {
	if raceEnabled {
		t.Skip("sync.Pool drops items under -race")
	}
	body := payload(16 << 10)
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Content-Type"] = jsonType
		_, _ = w.Write(body)
	}
	with := terminalApp(t, Compress{}, handler)
	without := newApp(t, terminalMiddleware(handler))
	r := gzipRequest(http.MethodGet)
	w := &discardWriter{header: http.Header{}}
	for i := 0; i < 20; i++ {
		with.ServeHTTP(w, r)
	}
	compressed := testing.AllocsPerRun(200, func() { clear(w.header); with.ServeHTTP(w, r) })
	baseline := testing.AllocsPerRun(200, func() { clear(w.header); without.ServeHTTP(w, r) })
	if compressed > baseline {
		t.Error(test.DiffMessage(compressed, baseline, "Z6: compressing 16 KiB of JSON allocates nothing in steady state"))
	}
}
