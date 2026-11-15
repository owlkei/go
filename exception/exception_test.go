package exception

import (
	"errors"
	"net/http"
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
	if e.GetCode() != 999 {
		t.Error(test.DiffMessage(e.GetCode(), 999, "GetCode returns raw code"))
	}
}

func TestException_GetMessage(t *testing.T) {
	e := NewException("hello", http.StatusBadRequest)
	if e.GetMessage() != "hello" {
		t.Error(test.DiffMessage(e.GetMessage(), "hello", "GetMessage"))
	}
}

func TestException_GetStatusText_HTTPValid(t *testing.T) {
	e := NewException("body", http.StatusNotFound)
	if text := e.GetStatusText(); text != "Not Found" {
		t.Error(test.DiffMessage(text, "Not Found", "GetStatusText valid HTTP code"))
	}
}

func TestException_GetStatusText_WSValid(t *testing.T) {
	e := NewException("body", 1008)
	if text := e.GetStatusText(); text != "Policy Violation" {
		t.Error(test.DiffMessage(text, "Policy Violation", "GetStatusText valid WS code"))
	}
}

func TestException_GetStatusText_Unknown(t *testing.T) {
	e := NewException("body", 999)
	if text := e.GetStatusText(); text != "" {
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

type causeBox struct{ error }

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

var sinkException Exception

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
		sum += ex.GetCode()
	})
	if sum == 0 {
		t.Fatal("result unused")
	}
	if allocs != 0 {
		t.Error(test.DiffMessage(allocs, 0.0, "E9: constructor must not allocate for ExceptionOptions by value (result kept local: boxing the options at a call site whose result escapes is the caller's cost)"))
	}
}
