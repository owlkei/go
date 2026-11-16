package exception

import (
	"encoding/json"
	"net/http"
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

type exceptionBody struct {
	Code    int    `json:"code"`
	Error   string `json:"error"`
	Message string `json:"message"`
}

func (e Exception) Error() string {
	if e.cause != nil && !e.isJoined {
		return e.cause.Error()
	}

	text := e.description
	if text == "" {
		text = e.GetStatusText()
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

func (e Exception) GetCode() int {
	return e.code
}

func (e Exception) GetMessage() string {
	return e.message
}

func (e Exception) GetStatusText() string {
	statusText := http.StatusText(e.code)
	if statusText == "" {
		statusText = wsCloseStatusText[e.code]
	}
	return statusText
}

func (e Exception) AppendJSON(dst []byte) []byte {
	body, err := json.Marshal(&exceptionBody{Code: e.code, Error: e.Error(), Message: e.message})
	if err != nil {
		return dst
	}
	if len(dst) == 0 {
		return body
	}
	return append(dst, body...)
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
