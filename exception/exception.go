package exception

import (
	"encoding/json"
	"net/http"
	"strconv"
)

var wsCloseStatusText = map[int]string{
	1000: "Normal Closure",
	1001: "Going Away",
	1002: "Protocol Error",
	1003: "Unsupported Data",
	1007: "Invalid Frame Payload Data",
	1008: "Policy Violation",
	1009: "Message Too Big",
	1010: "Mandatory Extension",
	1011: "Internal Error",
	4001: "Not Subscribed",
	4004: "Topic Not Found",
}

type Exception struct {
	message     string
	description string
	cause       error
	code        int
	isJoined    bool
}

type ExceptionOptions struct {
	Description string
	Cause       error
}

func (e Exception) Error() string {
	if e.cause != nil && !e.isJoined {
		return e.cause.Error()
	}

	text := e.description
	if text == "" {
		text = e.StatusText()
	}

	if e.cause == nil {
		return text
	}
	if text == "" {
		return e.cause.Error()
	}
	return text + ": " + e.cause.Error()
}

func (e Exception) Unwrap() error {
	return e.cause
}

func (e Exception) Code() int {
	return e.code
}

func (e Exception) Message() string {
	return e.message
}

func (e Exception) StatusText() string {
	statusText := http.StatusText(e.code)
	if statusText == "" {
		statusText = wsCloseStatusText[e.code]
	}
	return statusText
}

func (e Exception) AppendJSON(dst []byte) []byte {
	errorText := e.Error()
	if needsJSONEscape(errorText) || needsJSONEscape(e.message) {
		body, err := json.Marshal(&exceptionBody{Code: e.code, Error: errorText, Message: e.message})
		if err != nil {
			return dst
		}
		return append(dst, body...)
	}

	size := len(`{"code":,"error":"","message":""}`) + 20 + len(errorText) + len(e.message)
	if cap(dst)-len(dst) < size {
		grown := make([]byte, len(dst), len(dst)+size)
		copy(grown, dst)
		dst = grown
	}
	dst = append(dst, `{"code":`...)
	dst = strconv.AppendInt(dst, int64(e.code), 10)
	dst = append(dst, `,"error":"`...)
	dst = append(dst, errorText...)
	dst = append(dst, `","message":"`...)
	dst = append(dst, e.message...)
	return append(dst, `"}`...)
}

type exceptionBody struct {
	Code    int    `json:"code"`
	Error   string `json:"error"`
	Message string `json:"message"`
}

func needsJSONEscape(s string) bool {
	for i := 0; i < len(s); i++ {
		if b := s[i]; b < 0x20 || b > 0x7e || b == '"' || b == '\\' || b == '<' || b == '>' || b == '&' {
			return true
		}
	}
	return false
}

func (e Exception) MarshalJSON() ([]byte, error) {
	return e.AppendJSON(nil), nil
}

func (e *Exception) applyOptions(options ExceptionOptions) {
	e.description = options.Description
	e.cause = options.Cause
	e.isJoined = options.Cause != nil
}

func NewException(message string, code int, opts ...any) Exception {
	e := Exception{message: message, code: code}
	if len(opts) == 0 {
		return e
	}

	switch option := opts[0].(type) {
	case string:
		e.description = option
	case error:
		e.cause = option
	case ExceptionOptions:
		e.applyOptions(option)
	case *ExceptionOptions:
		if option != nil {
			e.applyOptions(*option)
		}
	}

	return e
}
