package exception

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func TestException_Error_NilError(t *testing.T) {
	var e Exception
	if e.Error() != "" {
		t.Error(test.DiffMessage(e.Error(), "", "Error on zero-value Exception must not panic"))
	}
}

func TestException_Error_WithError(t *testing.T) {
	e := NewException("body", http.StatusBadRequest, "custom message")
	if e.Error() != "custom message" {
		t.Error(test.DiffMessage(e.Error(), "custom message", "Error returns wrapped error text"))
	}
}

func TestException_Unwrap_Nil(t *testing.T) {
	var e Exception
	if e.Unwrap() != nil {
		t.Error(test.DiffMessage(e.Unwrap(), nil, "Unwrap on zero-value Exception"))
	}
}

func TestException_Unwrap_ReturnsDirectError(t *testing.T) {
	cause := errors.New("root cause")
	e := NewException("body", http.StatusBadRequest, ExceptionOptions{Cause: cause})
	if !errors.Is(e.Unwrap(), cause) && e.Unwrap() != cause {
		t.Error(test.DiffMessage(e.Unwrap(), cause, "Unwrap must return the directly wrapped error"))
	}
}

func TestException_ErrorsIs_ChainWorks(t *testing.T) {
	cause := errors.New("db connection refused")
	e := NewException("body", http.StatusInternalServerError, ExceptionOptions{Cause: cause})
	if !errors.Is(e, cause) {
		t.Error(test.DiffMessage(errors.Is(e, cause), true, "errors.Is must walk through Exception to find cause"))
	}
}

func TestException_ErrorsAs_ChainWorks(t *testing.T) {
	type customErr struct{ error }
	cause := &customErr{errors.New("wrapped")}
	e := NewException("body", http.StatusInternalServerError, ExceptionOptions{Cause: cause})

	var target *customErr
	if !errors.As(e, &target) {
		t.Error(test.DiffMessage(errors.As(e, &target), true, "errors.As must walk through Exception to find cause"))
	}
}

func TestException_GetCode(t *testing.T) {
	e := NewException("body", 999)
	if e.Code() != 999 {
		t.Error(test.DiffMessage(e.Code(), 999, "GetCode returns raw code"))
	}
}

func TestException_GetMessage(t *testing.T) {
	e := NewException("hello", http.StatusBadRequest)
	if e.Message() != "hello" {
		t.Error(test.DiffMessage(e.Message(), "hello", "GetMessage"))
	}
}

func TestException_GetStatusText_HTTPValid(t *testing.T) {
	e := NewException("body", http.StatusNotFound)
	if text := e.StatusText(); text != "Not Found" {
		t.Error(test.DiffMessage(text, "Not Found", "GetStatusText valid HTTP code"))
	}
}

func TestException_GetStatusText_WSValid(t *testing.T) {
	e := NewException("body", 1008)
	if text := e.StatusText(); text != "Policy Violation" {
		t.Error(test.DiffMessage(text, "Policy Violation", "GetStatusText valid WS code"))
	}
}

func TestException_GetStatusText_Unknown(t *testing.T) {
	e := NewException("body", 999)
	if text := e.StatusText(); text != "" {
		t.Error(test.DiffMessage(text, "", "GetStatusText unknown code"))
	}
}

func TestNewException_DefaultErrorFromHTTPCode(t *testing.T) {
	e := NewException("body", http.StatusBadRequest)
	if e.Error() != "Bad Request" {
		t.Error(test.DiffMessage(e.Error(), "Bad Request", "default error from HTTP status"))
	}
}

func TestNewException_DefaultErrorFromWSCode(t *testing.T) {
	e := NewException("body", 1008)
	if e.Error() != "Policy Violation" {
		t.Error(test.DiffMessage(e.Error(), "Policy Violation", "default error from WS close status"))
	}
}

func TestNewException_UnknownCodeHasNoDefaultError(t *testing.T) {
	e := NewException("body", 999)
	if e.Error() != "" {
		t.Error(test.DiffMessage(e.Error(), "", "unknown code has no default error text"))
	}
}

func TestNewException_StringOpt(t *testing.T) {
	e := NewException("body", http.StatusBadRequest, "custom message")
	if e.Error() != "custom message" {
		t.Error(test.DiffMessage(e.Error(), "custom message", "string opt overrides error"))
	}
}

func TestNewException_ErrorOpt(t *testing.T) {
	cause := errors.New("underlying error")
	e := NewException("body", http.StatusBadRequest, cause)
	if e.Error() != "underlying error" {
		t.Error(test.DiffMessage(e.Error(), "underlying error", "error opt sets error"))
	}
}

func TestNewException_ExceptionOptions_Description(t *testing.T) {
	e := NewException("body", http.StatusBadRequest, ExceptionOptions{
		Description: "validation failed",
	})
	if e.Error() != "validation failed" {
		t.Error(test.DiffMessage(e.Error(), "validation failed", "ExceptionOptions description"))
	}
}

func TestNewException_ExceptionOptions_Cause(t *testing.T) {
	cause := errors.New("db error")
	e := NewException("body", http.StatusInternalServerError, ExceptionOptions{
		Cause: cause,
	})
	if !errors.Is(e, cause) {
		t.Error(test.DiffMessage(errors.Is(e, cause), true, "cause is wrapped"))
	}
}

func TestNewException_ExceptionOptions_DescriptionAndCause(t *testing.T) {
	cause := errors.New("root cause")
	e := NewException("body", http.StatusBadRequest, ExceptionOptions{
		Description: "custom",
		Cause:       cause,
	})
	if !errors.Is(e, cause) {
		t.Error(test.DiffMessage(errors.Is(e, cause), true, "cause wrapped under description"))
	}
	if e.Error() == "" {
		t.Error(test.DiffMessage(e.Error(), "non-empty", "error string not empty"))
	}
}

func TestNewException_ExceptionOptions_NilCauseKeepsDefault(t *testing.T) {
	e := NewException("body", http.StatusBadRequest, ExceptionOptions{})
	if e.Error() != "Bad Request" {
		t.Error(test.DiffMessage(e.Error(), "Bad Request", "nil cause keeps default status text"))
	}
}

func TestNewException_ExceptionOptions_CauseNoDefaultText(t *testing.T) {
	cause := errors.New("root cause")
	e := NewException("body", 999, ExceptionOptions{Cause: cause})
	if e.Error() != "root cause" {
		t.Error(test.DiffMessage(e.Error(), "root cause", "cause becomes error when no default text exists"))
	}
}

func TestNewException_PointerOptions(t *testing.T) {
	cause := errors.New("root")
	ex := NewException("m", http.StatusBadRequest, &ExceptionOptions{Description: "d", Cause: cause})
	if ex.Error() != "d: root" {
		t.Error(test.DiffMessage(ex.Error(), "d: root", "E1: a non-nil *ExceptionOptions is read like the value"))
	}
	if ex.Unwrap() != cause {
		t.Error(test.DiffMessage(ex.Unwrap(), cause, "E1: pointer options keep the cause"))
	}
	only := NewException("m", http.StatusBadRequest, &ExceptionOptions{Description: "d"})
	if only.Error() != "d" {
		t.Error(test.DiffMessage(only.Error(), "d", "E1: pointer options description"))
	}
}

func TestNewException_UnknownOptionTypeIsNoOption(t *testing.T) {
	for _, opt := range []any{map[string]any{"description": "d"}, 42, []string{"x"}, struct{}{}} {
		ex := NewException("m", http.StatusBadRequest, opt)
		if ex.Error() != "Bad Request" {
			t.Error(test.DiffMessage(ex.Error(), "Bad Request", "E1: unsupported option type behaves as no option"))
		}
		if ex.Unwrap() != nil {
			t.Error(test.DiffMessage(ex.Unwrap(), nil, "E1: unsupported option has no cause"))
		}
	}
}

func TestNewException_NilOptionIsNoOption(t *testing.T) {
	ex := NewException("m", http.StatusBadRequest, nil)
	if ex.Error() != "Bad Request" {
		t.Error(test.DiffMessage(ex.Error(), "Bad Request", "E1: nil option behaves as no option"))
	}
	var err error
	ex = NewException("m", http.StatusBadRequest, err)
	if ex.Error() != "Bad Request" {
		t.Error(test.DiffMessage(ex.Error(), "Bad Request", "E1: nil error option behaves as no option"))
	}
}

func TestNewException_NilPointerOptionsIsNoOption(t *testing.T) {
	var opts *ExceptionOptions
	ex := NewException("m", http.StatusBadRequest, opts)
	if ex.Error() != "Bad Request" {
		t.Error(test.DiffMessage(ex.Error(), "Bad Request", "E1: nil *ExceptionOptions behaves as no option"))
	}
}

func TestNewException_EmptyStringOptIsNoOption(t *testing.T) {
	ex := NewException("m", http.StatusBadRequest, "")
	if ex.Error() != "Bad Request" {
		t.Error(test.DiffMessage(ex.Error(), "Bad Request", "E2: empty string behaves as no option"))
	}
	unknown := NewException("m", 999, "")
	if unknown.Error() != "" {
		t.Error(test.DiffMessage(unknown.Error(), "", "E2: empty string with unknown code stays empty"))
	}
}

func TestNewException_NoOptionErrorIsStatusText(t *testing.T) {
	if got := NewException("m", http.StatusTeapot).Error(); got != "I'm a teapot" {
		t.Error(test.DiffMessage(got, "I'm a teapot", "E3: no option gives the status text"))
	}
}

func TestNewException_DescriptionEmptyWithoutCauseUsesText(t *testing.T) {
	ex := NewException("m", http.StatusNotFound, ExceptionOptions{})
	if ex.Error() != "Not Found" {
		t.Error(test.DiffMessage(ex.Error(), "Not Found", "E3: empty description gives the text"))
	}
}

func TestNewException_DescriptionAndCauseText(t *testing.T) {
	cause := errors.New("root")
	ex := NewException("m", http.StatusBadRequest, ExceptionOptions{Description: "d", Cause: cause})
	if ex.Error() != "d: root" {
		t.Error(test.DiffMessage(ex.Error(), "d: root", "E3: description and cause"))
	}
}

func TestNewException_CauseWithEmptyDescriptionUsesTextPrefix(t *testing.T) {
	cause := errors.New("root")
	ex := NewException("m", http.StatusBadRequest, ExceptionOptions{Cause: cause})
	if ex.Error() != "Bad Request: root" {
		t.Error(test.DiffMessage(ex.Error(), "Bad Request: root", "E3: empty description uses the text"))
	}
}

func TestNewException_CauseWithoutTextOrDescription(t *testing.T) {
	cause := errors.New("root")
	ex := NewException("m", 999, ExceptionOptions{Cause: cause})
	if ex.Error() != "root" {
		t.Error(test.DiffMessage(ex.Error(), "root", "E3: no description and no text gives the cause text"))
	}
}

func TestNewException_DescriptionWithCauseAndUnknownCode(t *testing.T) {
	cause := errors.New("root")
	ex := NewException("m", 999, ExceptionOptions{Description: "d", Cause: cause})
	if ex.Error() != "d: root" {
		t.Error(test.DiffMessage(ex.Error(), "d: root", "E3: description with cause on unknown code"))
	}
}

func TestNewException_OnlyFirstOptionIsRead(t *testing.T) {
	ex := NewException("m", http.StatusBadRequest, "first", "second", errors.New("third"))
	if ex.Error() != "first" {
		t.Error(test.DiffMessage(ex.Error(), "first", "E3: later options are ignored"))
	}
	if ex.Unwrap() != nil {
		t.Error(test.DiffMessage(ex.Unwrap(), nil, "E3: a later error option is not a cause"))
	}
}

func TestException_Unwrap_ReturnsCause(t *testing.T) {
	cause := errors.New("root")
	byErr := NewException("m", http.StatusBadRequest, cause)
	if byErr.Unwrap() != cause {
		t.Error(test.DiffMessage(byErr.Unwrap(), cause, "E4: error option is the cause"))
	}
	byOpts := NewException("m", http.StatusBadRequest, ExceptionOptions{Description: "d", Cause: cause})
	if byOpts.Unwrap() != cause {
		t.Error(test.DiffMessage(byOpts.Unwrap(), cause, "E4: options cause is the cause"))
	}
}

func TestException_Unwrap_NilWithoutCause(t *testing.T) {
	forms := map[string]Exception{
		"none":        NewException("m", http.StatusBadRequest),
		"string":      NewException("m", http.StatusBadRequest, "s"),
		"description": NewException("m", http.StatusBadRequest, ExceptionOptions{Description: "d"}),
	}
	for name, ex := range forms {
		if ex.Unwrap() != nil {
			t.Error(test.DiffMessage(ex.Unwrap(), nil, "E4: Unwrap is nil for "+name))
		}
		if errors.Unwrap(ex) != nil {
			t.Error(test.DiffMessage(errors.Unwrap(ex), nil, "E4: errors.Unwrap is nil for "+name))
		}
	}
}

func TestException_ErrorsAs_Pointer(t *testing.T) {
	cause := &causeBox{errors.New("wrapped")}
	ex := NewException("m", http.StatusBadRequest, ExceptionOptions{Description: "d", Cause: cause})
	var target *causeBox
	if !errors.As(ex, &target) || target != cause {
		t.Error(test.DiffMessage(target, cause, "E4: errors.As finds the cause"))
	}
}

func TestException_Equal(t *testing.T) {
	if NewException("m", 400) != NewException("m", 400) {
		t.Error(test.DiffMessage(false, true, "E8: no option"))
	}
	if NewException("m", 400, "s") != NewException("m", 400, "s") {
		t.Error(test.DiffMessage(false, true, "E8: string option"))
	}
	if NewException("m", 400, ExceptionOptions{Description: "d"}) != NewException("m", 400, ExceptionOptions{Description: "d"}) {
		t.Error(test.DiffMessage(false, true, "E8: description"))
	}
	if NewException("m", 400) == NewException("n", 400) {
		t.Error(test.DiffMessage(true, false, "E8: different messages differ"))
	}
}

func TestNewException_NoAllocs(t *testing.T) {
	cause := errors.New("root")
	cases := map[string]func(){
		"no option":  func() { sinkException = NewException("m", http.StatusBadRequest) },
		"string":     func() { sinkException = NewException("m", http.StatusBadRequest, "custom") },
		"error":      func() { sinkException = NewException("m", http.StatusBadRequest, cause) },
		"options-ws": func() { sinkException = NewException("m", 1008, ExceptionOptions{Description: "d"}) },
	}
	for name, fn := range cases {
		if allocs := testing.AllocsPerRun(100, fn); allocs != 0 {
			t.Error(test.DiffMessage(allocs, 0.0, "E9: constructor must not allocate: "+name))
		}
	}
}

func TestNewException_NoAllocs_OptionsByValueLocalResult(t *testing.T) {
	cause := errors.New("root")
	sum := 0
	allocs := testing.AllocsPerRun(100, func() {
		ex := NewException("m", http.StatusBadRequest, ExceptionOptions{Description: "d", Cause: cause})
		sum += ex.Code()
	})
	if sum == 0 {
		t.Fatal("result unused")
	}
	if allocs != 0 {
		t.Error(test.DiffMessage(allocs, 0.0, "E9: constructor must not allocate for ExceptionOptions by value (result kept local: boxing the options at a call site whose result escapes is the caller's cost)"))
	}
}

func TestException_AppendJSON(t *testing.T) {
	ex := NewException("bad", http.StatusBadRequest, ExceptionOptions{Description: "validation failed"})
	want := `{"code":400,"error":"validation failed","message":"bad"}`
	if got := string(ex.AppendJSON(nil)); got != want {
		t.Error(test.DiffMessage(got, want, "E6: body bytes"))
	}
}

func TestException_AppendJSON_WithCause(t *testing.T) {
	ex := NewException("bad", http.StatusBadRequest, ExceptionOptions{Description: "d", Cause: errors.New("root")})
	want := `{"code":400,"error":"d: root","message":"bad"}`
	if got := string(ex.AppendJSON(nil)); got != want {
		t.Error(test.DiffMessage(got, want, "E6: error field is Error()"))
	}
}

func TestException_AppendJSON_KeepsPrefix(t *testing.T) {
	ex := NewException("m", http.StatusNotFound)
	got := ex.AppendJSON([]byte("prefix:"))
	want := `prefix:{"code":404,"error":"Not Found","message":"m"}`
	if string(got) != want {
		t.Error(test.DiffMessage(string(got), want, "E6: dst content is kept"))
	}
}

func TestException_AppendJSON_ZeroValue(t *testing.T) {
	var ex Exception
	want := `{"code":0,"error":"","message":""}`
	if got := string(ex.AppendJSON(nil)); got != want {
		t.Error(test.DiffMessage(got, want, "E6: zero value is written as is"))
	}
}

func TestException_AppendJSON_WSCode(t *testing.T) {
	ex := NewException("nope", 1008)
	want := `{"code":1008,"error":"Policy Violation","message":"nope"}`
	if got := string(ex.AppendJSON(nil)); got != want {
		t.Error(test.DiffMessage(got, want, "E6: WS code"))
	}
}

func TestException_AppendJSON_HostileStrings(t *testing.T) {
	hostile := []string{
		`"`, `\`, "line\nbreak", "nul\x00byte", "</script>", "a&b", "\u2028\u2029",
		"bad\xff\xfeutf8", "\x7f\x1f", "'single'", strings.Repeat("a\"\\<", 16*1024),
		strings.Repeat("x", 64*1024),
	}
	for _, s := range hostile {
		ex := NewException(s, http.StatusBadRequest, s)
		got := ex.AppendJSON(nil)
		want := mapBody(ex)
		if !bytes.Equal(got, want) {
			t.Error(test.DiffMessage(string(got[:min(len(got), 120)]), string(want[:min(len(want), 120)]), "E6: hostile string must equal json.Marshal"))
		}
		if !json.Valid(got) {
			t.Error(test.DiffMessage(false, true, "E6/Security: hostile body must be valid JSON"))
		}
	}
}

func TestException_AppendJSON_EscapesHTMLAndSeparators(t *testing.T) {
	ex := NewException("</script>&\u2028", http.StatusBadRequest)
	got := string(ex.AppendJSON(nil))
	for _, bad := range []string{"<", ">", "&", "\u2028"} {
		if strings.Contains(got, bad) {
			t.Error(test.DiffMessage(got, "escaped "+bad, "Security: body must escape characters"))
		}
	}
}

func TestException_AppendJSON_EqualsMarshal_Random(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	codes := []int{0, 200, 400, 404, 500, 999, 1008, 1011, -1, 65536}
	for range 2000 {
		code := codes[r.IntN(len(codes))]
		msg, desc := randomString(r), randomString(r)
		var ex Exception
		if r.IntN(2) == 0 {
			ex = NewException(msg, code, ExceptionOptions{Description: desc})
		} else {
			ex = NewException(msg, code, desc)
		}
		got, want := ex.AppendJSON(nil), mapBody(ex)
		if !bytes.Equal(got, want) {
			t.Fatal(test.DiffMessage(string(got), string(want), "E6: random message and description must equal json.Marshal"))
		}
	}
}

func TestException_AppendJSON_Pointer(t *testing.T) {
	ex := NewException("m", http.StatusBadRequest)
	var a jsonAppender = &ex
	var b jsonAppender = ex
	want := string(mapBody(ex))
	if string(a.AppendJSON(nil)) != want || string(b.AppendJSON(nil)) != want {
		t.Error(test.DiffMessage(string(a.AppendJSON(nil)), want, "E6: Exception and *Exception both append the body"))
	}
}

func TestException_MarshalJSON(t *testing.T) {
	ex := NewException("m", http.StatusBadRequest, "d")
	got, err := ex.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"code":400,"error":"d","message":"m"}`; string(got) != want {
		t.Error(test.DiffMessage(string(got), want, "E7: MarshalJSON body"))
	}
	viaJSON, err := json.Marshal(ex)
	if err != nil || string(viaJSON) != string(got) {
		t.Error(test.DiffMessage(string(viaJSON), string(got), "E7: json.Marshal of a value"))
	}
}

func TestException_MarshalJSON_Pointer(t *testing.T) {
	ex := NewException("m", http.StatusBadRequest, "d")
	got, err := json.Marshal(&ex)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"code":400,"error":"d","message":"m"}`; string(got) != want {
		t.Error(test.DiffMessage(string(got), want, "E7: json.Marshal of a pointer"))
	}
}

func TestException_MarshalJSON_InsideMap(t *testing.T) {
	ex := NewException("m", http.StatusBadRequest)
	got, err := json.Marshal(map[string]any{"e": ex, "p": &ex})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"code":400,"error":"Bad Request","message":"m"}`
	if want := `{"e":` + body + `,"p":` + body + `}`; string(got) != want {
		t.Error(test.DiffMessage(string(got), want, "E7: exception nested in a map"))
	}
}

func TestAppendJSON_EqualsEncodingJSON_Random(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 11))
	codes := []int{0, 7, 200, 400, 404, 500, 599, 600, 1008, 4001, -1, 65536}
	for range 10000 {
		code := codes[r.IntN(len(codes))]
		msg, desc := randomString(r), randomString(r)
		ex := NewException(msg, code, desc)
		got, want := ex.AppendJSON(nil), encodingJSONBody(t, ex)
		if !bytes.Equal(got, want) {
			t.Fatal(test.DiffMessage(string(got), string(want), "H1-eq: AppendJSON(nil) equals json.Marshal of the body struct for message "+msg+" and error "+desc))
		}
	}
}

func TestAppendJSON_EqualsEncodingJSON_Hostile(t *testing.T) {
	hostile := []string{
		"", `"`, `\`, "line\nbreak", "nul\x00byte", "</script>", "a&b", "  ",
		"bad\xff\xfeutf8", "\x7f\x1f", "\x1f", "\x7f", "'single'", "~", " ", "\xe2\x80",
		strings.Repeat("a\"\\<", 16*1024), strings.Repeat("x", 64*1024), strings.Repeat("\xff", 1024),
	}
	for _, s := range hostile {
		for _, code := range []int{0, 400, 500, 1008} {
			ex := NewException(s, code, s)
			got, want := ex.AppendJSON(nil), encodingJSONBody(t, ex)
			if !bytes.Equal(got, want) {
				t.Error(test.DiffMessage(string(got[:min(len(got), 120)]), string(want[:min(len(want), 120)]), "H1-eq: hostile string must equal json.Marshal"))
			}
		}
	}
}

func TestAppendJSON_AppendsToDst(t *testing.T) {
	ex := NewException("m</b>", http.StatusBadRequest, "d\"")
	body := encodingJSONBody(t, ex)
	for _, prefix := range []string{"", "x", "prefix:", strings.Repeat("p", 4096)} {
		got := ex.AppendJSON([]byte(prefix))
		want := append([]byte(prefix), body...)
		if !bytes.Equal(got, want) {
			t.Error(test.DiffMessage(string(got[:min(len(got), 80)]), string(want[:min(len(want), 80)]), "H1-eq: AppendJSON(dst) equals append(dst, body...) for a prefix of "+strconv.Itoa(len(prefix))))
		}
	}
}

func TestAppendJSON_AppendsToDstWithSpareCapacity(t *testing.T) {
	ex := NewException("m", http.StatusBadRequest)
	dst := append(make([]byte, 0, 512), "head"...)
	got := ex.AppendJSON(dst)
	want := append([]byte("head"), encodingJSONBody(t, ex)...)
	if !bytes.Equal(got, want) {
		t.Error(test.DiffMessage(string(got), string(want), "H1-eq: AppendJSON writes after the existing bytes of a dst with spare capacity"))
	}
}

func TestAppendJSON_PlainBodyAllocatesOnce(t *testing.T) {
	ex := NewException("invalid input", http.StatusBadRequest)
	allocs := testing.AllocsPerRun(100, func() {
		_ = ex.AppendJSON(nil)
	})
	if allocs > 1 {
		t.Error(test.DiffMessage(allocs, 1.0, "P8: a plain body is written with one allocation, the body itself"))
	}
}

func FuzzAppendJSON(f *testing.F) {
	f.Add(400, "Bad Request", "invalid input")
	f.Add(0, "", "")
	f.Add(500, "\"\\", "<script>& ")
	f.Add(1008, "bad\xff", "\x00\x1f\x7f")
	f.Fuzz(func(t *testing.T, code int, desc, message string) {
		ex := NewException(message, code, desc)
		got, want := ex.AppendJSON(nil), encodingJSONBody(t, ex)
		if !bytes.Equal(got, want) {
			t.Fatal(test.DiffMessage(string(got), string(want), "H1-eq: fuzz"))
		}
	})
}
