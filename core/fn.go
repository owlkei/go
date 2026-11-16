package core

import (
	"errors"
	"go/token"
	"net/http"
	"path"
	"reflect"
	"regexp"
	"strconv"
	"time"

	"github.com/dangduoc08/ginject/common"
	"github.com/dangduoc08/ginject/ctx"
	"github.com/dangduoc08/ginject/event"
	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/trace"
)

var pkgFromControllerKeyReg = regexp.MustCompile(`\[.*?\]`)

func isInjectedProvider(providerFieldType reflect.Type) bool {
	instance := reflect.New(providerFieldType)
	_, ok := instance.Interface().(Provider)
	return ok
}

func genProviderKey(p Provider) string {
	return genFieldKey(reflect.TypeOf(p))
}

func getPkgFromControllerKey(k string) string {
	return pkgFromControllerKeyReg.ReplaceAllString(k, "")
}

func genFieldKey(t reflect.Type) string {
	return t.PkgPath() + "/" + t.String()
}

func injectDependencies(component any, kind string, injectedProviders map[string]Provider) (reflect.Value, error) {
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
		} else if p, ok := globalProviderByKey.Load(componentFieldKey); componentFieldKey != "" && ok {
			newComponent.Elem().Field(j).Set(reflect.ValueOf(p))
		} else if iface, ok := globalInterfaceByKey.Load(componentFieldKey); componentFieldKey != "" && ok {
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

func buildUseMiddleware(useFn common.Use, event *event.Event, name, transport string) ctx.HTTPHandler {
	return func(c *ctx.HTTPContext) {
		defer func() {
			if rec := recover(); rec != nil {
				globalHTTPExceptionFilter{}.Catch(c, common.NormalizeRecovered(rec))
			}
		}()

		next := func() {
			c.Next()
		}

		if !event.HasListeners(trace.EventName) {
			useFn(c.Request, c.ResponseWriter, next)
			return
		}

		start := time.Now()
		useFn(c.Request, c.ResponseWriter, next)
		event.Emit(trace.EventName, trace.Event{ID: c.GetID(), Stage: trace.StageMiddleware, Name: name, Transport: transport, Duration: time.Since(start)})
	}
}

const publisherKey = "github.com/dangduoc08/ginject/common/common.Publisher"
