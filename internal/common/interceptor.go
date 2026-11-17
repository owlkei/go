package common

import (
	"errors"
	"reflect"

	"github.com/dangduoc08/ginject/internal/color"
)

const InterceptorMethodName = "Intercept"

type InterceptorItem struct {
	HTTP HTTPInterceptorItem
	WS   WSInterceptorItem
}

type interceptorHandler struct {
	interceptable any
	handlers      []any
}

type Interceptor struct {
	interceptorHandlers []interceptorHandler
}

func (i *Interceptor) BindInterceptor(interceptor any, handlers ...any) *Interceptor {
	interceptorHandler := interceptorHandler{
		interceptable: interceptor,
		handlers:      handlers,
	}

	i.interceptorHandlers = append(i.interceptorHandlers, interceptorHandler)

	return i
}

func InterceptorShapeError(interceptable any) error {
	return errors.New(color.FmtRed(
		"invalid interceptor: %v has no %s method usable as an interceptor",
		reflect.TypeOf(interceptable),
		InterceptorMethodName,
	))
}
