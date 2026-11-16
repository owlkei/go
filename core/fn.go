package core

import (
	"errors"
	"go/token"
	"net/http"
	"path"
	"reflect"
	"runtime"
	"strconv"
	"time"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/trace"
)

func isInjectedProvider(providerFieldType reflect.Type) bool {
	instance := reflect.New(providerFieldType)
	_, ok := instance.Interface().(Provider)
	return ok
}

func genProviderKey(p Provider) string {
	return genFieldKey(reflect.TypeOf(p))
}

func genFieldKey(t reflect.Type) string {
	return t.PkgPath() + "/" + t.String()
}

func injectDependencies(component any, kind string, injectedProviders map[string]Provider, c *container) (reflect.Value, error) {
	componentType := reflect.TypeOf(component)
	componentValue := reflect.ValueOf(component)
	newComponent := reflect.New(componentType)

	componentName := path.Base(componentType.PkgPath()) + "." + componentType.Name()
	for j := 0; j < componentType.NumField(); j++ {
		componentField := componentType.Field(j)
		componentFieldType := componentField.Type
		componentFieldKey := genFieldKey(componentFieldType)
		componentFieldName := componentField.Name

		if !token.IsExported(componentFieldName) {
			panic(errors.New(
				color.FmtRed(
					"dependency injection: can't set value to unexported '%v' field of the %v %v",
					componentFieldName,
					componentName,
					kind,
				),
			))
		}

		if componentFieldKey != "" && injectedProviders[componentFieldKey] != nil {
			newComponent.Elem().Field(j).Set(reflect.ValueOf(injectedProviders[componentFieldKey]))
		} else if p, ok := c.global(componentFieldKey); componentFieldKey != "" && ok {
			newComponent.Elem().Field(j).Set(reflect.ValueOf(p))
		} else if iface, ok := c.interfaceFor(componentFieldKey); componentFieldKey != "" && ok {
			newComponent.Elem().Field(j).Set(reflect.ValueOf(iface))
		} else if !isInjectedProvider(componentFieldType) {
			newComponent.Elem().Field(j).Set(componentValue.Field(j))
		} else {
			return reflect.ValueOf(nil), errors.New(
				color.FmtRed(
					"dependency injection: can't resolve dependency '%v' of the %v. Please make sure that the argument dependency at index [%v] is available in the '%v' %v",
					componentFieldType.String(),
					kind,
					j,
					componentName,
					kind,
				),
			)
		}
	}

	return newComponent, nil
}

func returnHTTP(c *ctx.HTTPContext, data reflect.Value) {
	switch data.Kind() {
	case reflect.Interface:
		if elem := data.Elem(); elem.Kind() == reflect.String {
			c.Text(elem.String())
			return
		}
		c.JSON(data.Interface())
	case
		reflect.Map,
		reflect.Slice,
		reflect.Struct,
		reflect.Array,
		reflect.Pointer:
		c.JSON(data.Interface())
	case reflect.Bool:
		c.Text(strconv.FormatBool(data.Bool()))
	case
		reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64:
		c.Text(strconv.FormatInt(data.Int(), 10))
	case
		reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64:
		c.Text(strconv.FormatUint(data.Uint(), 10))
	case reflect.Float32:
		c.Text(strconv.FormatFloat(data.Float(), 'g', -1, 32))
	case reflect.Float64:
		c.Text(strconv.FormatFloat(data.Float(), 'g', -1, 64))
	case reflect.Complex64:
		c.Text(strconv.FormatComplex(data.Complex(), 'g', -1, 64))
	case reflect.Complex128:
		c.Text(strconv.FormatComplex(data.Complex(), 'g', -1, 128))
	case reflect.String:
		c.Text(data.String())
	case reflect.Func:
		c.Text(data.Type().String())
	}
}

func setStatusCode(c *ctx.HTTPContext, statusCode reflect.Value) {
	switch statusCode.Kind() {
	case reflect.Int:
		status := int(statusCode.Int())
		if http.StatusText(status) != "" {
			c.Status(status)
		}
	case reflect.Interface:
		if status, ok := statusCode.Interface().(int); ok &&
			http.StatusText(status) != "" {
			c.Status(status)
		}
	}
}

type httpMiddleware struct {
	use  common.Use
	name string
}

func newHTTPMiddleware(m common.MiddlewareFn, injectedProviders map[string]Provider, c *container) httpMiddleware {
	name := reflect.TypeOf(m).String()
	injected, err := injectDependencies(m, "middleware", injectedProviders, c)
	if err != nil {
		panic(err)
	}
	m = c.construct(injected.Interface(), "NewMiddleware").(common.MiddlewareFn)
	return httpMiddleware{use: m.Use, name: name}
}

func (m *httpMiddleware) emit(ev *trace.Tracer, id string, duration time.Duration, code int) {
	ev.Emit(trace.Event{ID: id, Stage: trace.StageMiddleware, Name: m.name, Transport: trace.TransportHTTP, Duration: duration, Code: code})
}

const panicStackLimit = 4 << 10

var (
	defaultException             = exception.InternalServerErrorException("Unhandled exception has occurred")
	internalServerErrorException = exception.InternalServerErrorException(http.StatusText(http.StatusInternalServerError))
	internalErrorBody            = internalServerErrorException.AppendJSON(nil)
)

func asException(rec any) (*exception.Exception, bool) {
	switch value := rec.(type) {
	case *exception.Exception:
		return value, value != nil
	case exception.Exception:
		return &value, true
	case error:
		var pointer *exception.Exception
		if errors.As(value, &pointer) && pointer != nil {
			return pointer, true
		}
		var found exception.Exception
		if errors.As(value, &found) {
			return &found, true
		}
	}
	return nil, false
}

func writeErrorResponse(w http.ResponseWriter, e *exception.Exception) int {
	code := e.GetCode()
	if code < http.StatusBadRequest || code > 599 {
		code = http.StatusInternalServerError
	}
	rawErrorText, rawMessage := e.Error(), e.GetMessage()
	errorText, message := rawErrorText, rawMessage
	if errorText == "" {
		errorText = http.StatusText(code)
	}
	if errorText == "" {
		errorText = defaultException.Error()
	}
	if message == "" {
		message = defaultException.GetMessage()
	}

	switch {
	case code == http.StatusInternalServerError && errorText == internalServerErrorException.Error() && message == internalServerErrorException.GetMessage():
		writeJSONError(w, code, internalErrorBody)
	case code == e.GetCode() && errorText == rawErrorText && message == rawMessage:
		writeJSONError(w, code, e.AppendJSON(nil))
	default:
		normalized := exception.NewException(message, code, errorText)
		writeJSONError(w, code, normalized.AppendJSON(nil))
	}
	return code
}

func writeJSONError(w http.ResponseWriter, code int, body []byte) {
	h := w.Header()
	delete(h, "Content-Length")
	delete(h, "Content-Encoding")
	delete(h, "Etag")
	delete(h, "Last-Modified")
	h["Content-Type"] = []string{applicationJSON}
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

const applicationJSON = "application/json"

func logPanic(logger common.Logger, msg string, c *ctx.HTTPContext, rec any) {
	logger.Error(msg, "id", c.GetID(), "method", c.Method, "path", c.URL.Path, "panic", rec, "stack", panicStack())
}

func panicStack() string {
	buf := make([]byte, panicStackLimit)
	return string(buf[:runtime.Stack(buf, false)])
}

const publisherKey = "github.com/dangduoc08/ginject/common/common.Publisher"
