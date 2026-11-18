package common

import "reflect"

type CommonItem struct {
	Name            string
	Handler         any
	MainHandlerName string
}

func BoundLayerValues(controller any) []any {
	value := reflect.ValueOf(controller)
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return nil
	}
	var layers []any
	if g, ok := fieldOf[Guard](value, "Guard"); ok {
		for _, h := range g.guardHandlers {
			layers = append(layers, h.guarder)
		}
	}
	if i, ok := fieldOf[Interceptor](value, "Interceptor"); ok {
		for _, h := range i.interceptorHandlers {
			layers = append(layers, h.interceptable)
		}
	}
	if f, ok := fieldOf[ExceptionFilter](value, "ExceptionFilter"); ok {
		for _, h := range f.exceptionFilterHandlers {
			layers = append(layers, h.exceptionFilterable)
		}
	}
	if m, ok := fieldOf[Middleware](value, "Middleware"); ok {
		for _, h := range m.middlewareHandlers {
			layers = append(layers, h.middlewareFn)
		}
	}
	return layers
}

func fieldOf[T any](value reflect.Value, name string) (T, bool) {
	field := value.FieldByName(name)
	if !field.IsValid() || !field.CanInterface() {
		var zero T
		return zero, false
	}
	t, ok := field.Interface().(T)
	return t, ok
}
