package ctx

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func buildMultipartFileBody(t *testing.T, field, filename, content string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

func TestHTTPContext_File(t *testing.T) {
	buf, contentType := buildMultipartFileBody(t, "avatar", "photo.png", "fake-image-bytes")

	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("POST", "/", buf)
	c.Request.Header.Set("Content-Type", contentType)

	if err := c.ParseMultipartForm(defaultMaxMemory); err != nil {
		t.Fatal(err)
	}

	f := c.File()
	if len(f["avatar"]) != 1 {
		t.Fatal(test.DiffMessage(len(f["avatar"]), 1, "File should expose the uploaded file under its field name"))
	}
	if f["avatar"][0].Filename != "photo.png" {
		t.Error(test.DiffMessage(f["avatar"][0].Filename, "photo.png", "uploaded file should keep its original filename"))
	}
}

func TestHTTPContext_FileNoMultipartForm(t *testing.T) {
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("GET", "/", nil)

	if len(c.File()) != 0 {
		t.Error(test.DiffMessage(c.File(), File{}, "File should have length 0 when there is no multipart form"))
	}
}

type filePtrDTO struct {
	Avatar *DataFile `bind:"avatar"`
}

type fileSliceDTO struct {
	Photos []*DataFile `bind:"photos"`
}

func TestFile_BindPtr(t *testing.T) {
	buf, contentType := buildMultipartFileBody(t, "avatar", "photo.png", "fake-image-bytes")
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("POST", "/", buf)
	c.Request.Header.Set("Content-Type", contentType)
	if err := c.ParseMultipartForm(defaultMaxMemory); err != nil {
		t.Fatal(err)
	}

	result := c.File().Bind(filePtrDTO{})
	dto, ok := result.(filePtrDTO)
	if !ok {
		t.Fatal(test.DiffMessage(result, filePtrDTO{}, "Bind should return a filePtrDTO"))
	}
	if dto.Avatar == nil || dto.Avatar.Filename != "photo.png" {
		t.Error(test.DiffMessage(dto.Avatar, "photo.png", "Bind should populate the pointer field with the uploaded file"))
	}
}

func TestFile_BindSlice(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, name := range []string{"a.png", "b.png"} {
		fw, err := mw.CreateFormFile("photos", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("POST", "/", &buf)
	c.Request.Header.Set("Content-Type", mw.FormDataContentType())
	if err := c.ParseMultipartForm(defaultMaxMemory); err != nil {
		t.Fatal(err)
	}

	result := c.File().Bind(fileSliceDTO{})
	dto, ok := result.(fileSliceDTO)
	if !ok {
		t.Fatal(test.DiffMessage(result, fileSliceDTO{}, "Bind should return a fileSliceDTO"))
	}
	if len(dto.Photos) != 2 {
		t.Fatal(test.DiffMessage(len(dto.Photos), 2, "Bind should populate the slice field with all uploaded files"))
	}
	if dto.Photos[0].Total != 2 {
		t.Error(test.DiffMessage(dto.Photos[0].Total, 2, "each bound DataFile should report the total count of uploaded files"))
	}
}

type fileValidatorDTO struct {
	Avatar *DataFile `bind:"avatar"`
}

func (fileValidatorDTO) IsValid(f *DataFile) bool {
	return f.Filename == "allowed.png"
}

func TestFile_BindPanicsWhenValidatorRejects(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic when the validator rejects the uploaded file")
		}
	}()

	buf, contentType := buildMultipartFileBody(t, "avatar", "rejected.png", "x")
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("POST", "/", buf)
	c.Request.Header.Set("Content-Type", contentType)
	if err := c.ParseMultipartForm(defaultMaxMemory); err != nil {
		t.Fatal(err)
	}

	c.File().Bind(fileValidatorDTO{})
}

type fileHandlerDTO struct {
	Avatar *DataFile `bind:"avatar"`
}

var storedContent string

func (fileHandlerDTO) Store(f *DataFile, src multipart.File) {
	b, _ := io.ReadAll(src)
	storedContent = string(b)
}

func TestFile_BindInvokesHandler(t *testing.T) {
	buf, contentType := buildMultipartFileBody(t, "avatar", "photo.png", "hello-bytes")
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("POST", "/", buf)
	c.Request.Header.Set("Content-Type", contentType)
	if err := c.ParseMultipartForm(defaultMaxMemory); err != nil {
		t.Fatal(err)
	}

	c.File().Bind(fileHandlerDTO{})
	if storedContent != "hello-bytes" {
		t.Error(test.DiffMessage(storedContent, "hello-bytes", "Bind should invoke the handler's Store with the file content"))
	}
}

func newUploadContext(t *testing.T, field, filename, content string) (*HTTPContext, *int) {
	t.Helper()
	buf, contentType := buildMultipartFileBody(t, field, filename, content)
	reads := 0
	c := newTestHTTPContext()
	c.Request = httptest.NewRequest("POST", "/", io.NopCloser(readCounter{r: buf, n: &reads}))
	c.Request.Header.Set("Content-Type", contentType)
	return c, &reads
}

func TestHTTPContext_FileNeverNil(t *testing.T) {
	requests := map[string]*HTTPContext{
		"get":  newFormContext("", nil),
		"json": newFormContext(applicationJSON, strings.NewReader(`{"a":1}`)),
		"form": newFormContext(applicationXWWWFormUrlencoded, strings.NewReader("a=1")),
	}
	for name, c := range requests {
		var f File
		if !noPanic(t, "B13 File of a "+name+" request", func() { f = c.File() }) {
			continue
		}
		if len(f) != 0 {
			t.Error(test.DiffMessage(f, File{}, "File of a "+name+" request should have length 0"))
		}
	}
}

func TestHTTPContext_FileWithoutForm(t *testing.T) {
	c, _ := newUploadContext(t, "avatar", "photo.png", "fake-image-bytes")

	f := c.File()

	if len(f["avatar"]) != 1 || f["avatar"][0].Filename != "photo.png" {
		t.Fatal(test.DiffMessage(f, "<one file avatar: photo.png>", "B13: File should parse the multipart form itself"))
	}
}

func TestHTTPContext_FileThenFormParsesOnce(t *testing.T) {
	c, reads := newUploadContext(t, "avatar", "photo.png", "bytes")

	files := c.File()
	afterFile := *reads
	parsed := c.Request.MultipartForm
	form := c.Form()

	if len(files["avatar"]) != 1 {
		t.Error(test.DiffMessage(len(files["avatar"]), 1, "B13: File should hold the upload"))
	}
	if *reads != afterFile || c.Request.MultipartForm != parsed {
		t.Error(test.DiffMessage(*reads, afterFile, "B13: Form after File should not read the body again"))
	}
	if form == nil {
		t.Error(test.DiffMessage(form, Form{}, "B13: Form after File should not be nil"))
	}
}

func TestHTTPContext_FormThenFileParsesOnce(t *testing.T) {
	c, reads := newUploadContext(t, "avatar", "photo.png", "bytes")

	form := c.Form()
	afterForm := *reads
	parsed := c.Request.MultipartForm
	files := c.File()

	if form == nil {
		t.Error(test.DiffMessage(form, Form{}, "B13: Form should not be nil"))
	}
	if len(files["avatar"]) != 1 {
		t.Error(test.DiffMessage(len(files["avatar"]), 1, "B13: File after Form should hold the upload"))
	}
	if *reads != afterForm || c.Request.MultipartForm != parsed {
		t.Error(test.DiffMessage(*reads, afterForm, "B13: File after Form should not read the body again"))
	}
}

func TestHTTPContext_FileAndFormShareFields(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("title", "holiday")
	fw, _ := mw.CreateFormFile("avatar", "photo.png")
	_, _ = fw.Write([]byte("x"))
	_ = mw.Close()
	c := newFormContext(mw.FormDataContentType(), &buf)

	files := c.File()
	form := c.Form()

	if len(files["avatar"]) != 1 || form.Get("title") != "holiday" {
		t.Error(test.DiffMessage([2]any{len(files["avatar"]), form.Get("title")}, [2]any{1, "holiday"}, "B13: File and Form should come from the one parse"))
	}
}

func TestHTTPContext_FileBrokenMultipartIsBadRequest(t *testing.T) {
	c := newFormContext("multipart/form-data; boundary=xyz", strings.NewReader("garbage"))
	exc, _ := panicException(t, func() { c.File() })
	if exc.Code() != http.StatusBadRequest || exc.Message() != "Invalid form body" {
		t.Error(test.DiffMessage([2]any{exc.Code(), exc.Message()}, [2]any{http.StatusBadRequest, "Invalid form body"}, "B13: File should fail like Form on a broken multipart body"))
	}
}

func TestHTTPContext_FileTooLargeIs413(t *testing.T) {
	c, _ := newUploadContext(t, "avatar", "photo.png", strings.Repeat("x", 500))
	c.Request.Body = http.MaxBytesReader(httptest.NewRecorder(), c.Request.Body, 50)

	exc, err := panicException(t, func() { c.File() })

	if exc.Code() != http.StatusRequestEntityTooLarge {
		t.Error(test.DiffMessage(exc.Code(), http.StatusRequestEntityTooLarge, "B13: File over the limit should panic with code 413"))
	}
	var tooLarge *http.MaxBytesError
	if !errors.As(err, &tooLarge) {
		t.Error(test.DiffMessage(err, "<*http.MaxBytesError in the chain>", "B13: errors.As should reach the limit error"))
	}
}

type panickingStoreDTO struct {
	Avatar *DataFile `bind:"avatar"`
}

var panickingStoreSource multipart.File

func (panickingStoreDTO) Store(f *DataFile, src multipart.File) {
	panickingStoreSource = src
	panic("store failed")
}

func TestFile_BindClosesSourceWhenStorePanics(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("avatar", "photo.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte("hello-bytes"))
	_ = mw.Close()

	form, err := multipart.NewReader(&buf, mw.Boundary()).ReadForm(1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = form.RemoveAll() }()

	panickingStoreSource = nil
	func() {
		defer func() { _ = recover() }()
		File(form.File).Bind(panickingStoreDTO{})
	}()

	if panickingStoreSource == nil {
		t.Fatal(test.DiffMessage(panickingStoreSource, "<the opened file>", "B22: Store should have received the source"))
	}
	if _, err := panickingStoreSource.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Error(test.DiffMessage(err, os.ErrClosed, "B22: the source should be closed before the panic leaves File.Bind"))
	}
}

func TestFile_BindStorePanicStillPropagates(t *testing.T) {
	buf, contentType := buildMultipartFileBody(t, "avatar", "photo.png", "x")
	c := newFormContext(contentType, buf)
	err := func() (r any) {
		defer func() { r = recover() }()
		c.File().Bind(panickingStoreDTO{})
		return nil
	}()
	if err != "store failed" {
		t.Error(test.DiffMessage(err, "store failed", "B22: the Store panic should still reach the caller"))
	}
}

func TestHTTPContext_FileBindFromRequestWithoutForm(t *testing.T) {
	c, _ := newUploadContext(t, "avatar", "photo.png", "bytes")
	result := c.File().Bind(filePtrDTO{})
	dto := result.(filePtrDTO)
	if dto.Avatar == nil || dto.Avatar.Filename != "photo.png" {
		t.Error(test.DiffMessage(dto.Avatar, "photo.png", "B13: a handler that injects only the files should get them bound"))
	}
}
