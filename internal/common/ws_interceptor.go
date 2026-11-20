package common

import (
	"errors"
	"reflect"

	"go.owlkei.com/owlkei/internal/aggregation"
	"go.owlkei.com/owlkei/internal/color"
	"go.owlkei.com/owlkei/internal/ctx"
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

func InjectProvidersIntoWSInterceptors(i *Interceptor, ws *WS, cb func(int, reflect.Type, reflect.Value, reflect.Value), construct func(obj any, constructor string) any) []InterceptorItem {
	interceptorItemArr := make([]InterceptorItem, 0, len(ws.funcNameByEvent)*len(i.interceptorHandlers))

	for _, interceptorHandler := range i.interceptorHandlers {
		interceptableType := reflect.TypeOf(interceptorHandler.interceptable)
		interceptableValue := reflect.ValueOf(interceptorHandler.interceptable)
		newInterceptor := reflect.New(interceptableType)

		for j := 0; j < interceptableType.NumField(); j++ {
			cb(j, interceptableType, interceptableValue, newInterceptor)
		}

		newInterceptable := newInterceptor.Interface()
		newInterceptable = construct(newInterceptable, "NewInterceptor")
		interceptorHandler.interceptable = newInterceptable

		intercept, ok := AsWSInterceptor(interceptorHandler.interceptable)
		if !ok {
			if _, ok = AsHTTPInterceptor(interceptorHandler.interceptable); ok {
				continue
			}

			panic(errors.New(color.FmtRed(
				"invalid interceptor: %v.%s must be func(owlkei.WSContext, owlkei.Aggregation) any to be bound as a WS interceptor",
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
		aggregation.SetName(aggregationInstance, name)
		ctx.WSAddAggregation(c, aggregationInstance)
		aggregation.SetInterceptorData(aggregationInstance, interceptFn(c, aggregationInstance))
		ctx.WSNext(c)()
	}
}
