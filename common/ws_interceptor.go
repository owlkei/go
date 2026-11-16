package common

import (
	"errors"
	"reflect"

	"github.com/dangduoc08/ginject/aggregation"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/internal/color"
)

type WSIntercept = func(*ctx.WSContext, *aggregation.Aggregation) any

type WSInterceptorItem struct {
	EventName string
	Common    CommonItem
}

type wsInterceptor interface {
	Intercept(*ctx.WSContext, *aggregation.Aggregation) any
}

func AsWSInterceptor(interceptable any) (WSIntercept, bool) {
	layer, ok := interceptable.(wsInterceptor)
	if !ok {
		return nil, false
	}
	return layer.Intercept, true
}

func (i *Interceptor) InjectProvidersIntoWSInterceptors(ws *WS, cb func(int, reflect.Type, reflect.Value, reflect.Value), construct func(obj any, constructor string) any) []InterceptorItem {
	interceptorItemArr := make([]InterceptorItem, 0, len(ws.funcNameByEvent)*len(i.InterceptorHandlers))

	for _, interceptorHandler := range i.InterceptorHandlers {
		interceptableType := reflect.TypeOf(interceptorHandler.interceptable)
		interceptableValue := reflect.ValueOf(interceptorHandler.interceptable)
		newInterceptor := reflect.New(interceptableType)

		for j := 0; j < interceptableType.NumField(); j++ {
			cb(j, interceptableType, interceptableValue, newInterceptor)
		}

		newInterceptable := newInterceptor.Interface()
		newInterceptable = construct(newInterceptable, "NewInterceptor")
		interceptorHandler.interceptable = newInterceptable.(Interceptable)

		intercept, ok := AsWSInterceptor(interceptorHandler.interceptable)
		if !ok {
			if _, ok = AsHTTPInterceptor(interceptorHandler.interceptable); ok {
				continue
			}

			panic(errors.New(color.FmtRed(
				"invalid interceptor: %v.%s must be func(*ctx.WSContext, *aggregation.Aggregation) any to be bound as a WS interceptor",
				interceptableType,
				InterceptorMethodName,
			)))
		}

		targets := wsHandlerTargets(interceptorHandler.handlers, "interceptor", interceptableType)

		for pattern := range ws.funcNameByEvent {
			if targets.has(pattern) {
				interceptorItemArr = append(interceptorItemArr, InterceptorItem{
					WS: WSInterceptorItem{
						EventName: pattern,
						Common: CommonItem{
							Handler: intercept,
							Name:    interceptableType.String(),
						},
					},
				})
			}
		}
	}

	return interceptorItemArr
}

func BuildWSInterceptMiddleware(name string, interceptFn WSIntercept) ctx.WSHandler {
	return func(c *ctx.WSContext) {
		aggregationInstance := aggregation.NewAggregation()
		aggregationInstance.Name = name
		c.AddAggregation(aggregationInstance)
		aggregationInstance.InterceptorData = interceptFn(c, aggregationInstance)
		c.Next()
	}
}
