package ctx

import (
	"reflect"

	"go.owlkei.com/owlkei/internal/num"
	"go.owlkei.com/owlkei/internal/slice"
)

type number interface {
	int | int8 | int16 | int32 | int64 |
		uint | uint8 | uint16 | uint32 | uint64 |
		float32 | float64 | complex64 | complex128
}

func asBool(v any, _ reflect.Kind) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}

func asString(v any, _ reflect.Kind) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func asNumber[T number](v any, kind reflect.Kind) (T, bool) {
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	n, ok := num.F64ToAnyNumOK(f, kind)
	if !ok {
		return 0, false
	}
	t, ok := n.(T)
	return t, ok
}

func scalarSlice[T any](arr []any, nested bool, kind reflect.Kind, conv func(any, reflect.Kind) (T, bool)) []T {
	if nested {
		out := make([]T, 0, len(arr))
		for _, el := range arr {
			if t, ok := conv(el, kind); ok {
				out = append(out, t)
			}
		}
		return out
	}
	if len(arr) == 0 {
		return nil
	}
	out := make([]T, len(arr))
	for i, el := range arr {
		t, ok := conv(el, kind)
		if !ok {
			return []T{}
		}
		out[i] = t
	}
	return out
}

func scalarMap[T any](obj map[string]any, lenient bool, kind reflect.Kind, conv func(any, reflect.Kind) (T, bool)) map[string]T {
	out := make(map[string]T, len(obj))
	for k, v := range obj {
		t, ok := conv(v, kind)
		if !ok {
			if lenient {
				continue
			}
			return map[string]T{}
		}
		out[k] = t
	}
	return out
}

func bindTypedSlice(dst reflect.Value, arr []any, nested bool) bool {
	switch p := dst.Addr().Interface().(type) {
	case *[]any:
		if nested {
			*p = append(make([]any, 0, len(arr)), arr...)
		} else {
			*p = arr
		}
	case *[]bool:
		*p = scalarSlice(arr, nested, reflect.Bool, asBool)
	case *[]string:
		*p = scalarSlice(arr, nested, reflect.String, asString)
	case *[]int:
		*p = scalarSlice(arr, nested, reflect.Int, asNumber[int])
	case *[]int8:
		*p = scalarSlice(arr, nested, reflect.Int8, asNumber[int8])
	case *[]int16:
		*p = scalarSlice(arr, nested, reflect.Int16, asNumber[int16])
	case *[]int32:
		*p = scalarSlice(arr, nested, reflect.Int32, asNumber[int32])
	case *[]int64:
		*p = scalarSlice(arr, nested, reflect.Int64, asNumber[int64])
	case *[]uint:
		*p = scalarSlice(arr, nested, reflect.Uint, asNumber[uint])
	case *[]uint8:
		*p = scalarSlice(arr, nested, reflect.Uint8, asNumber[uint8])
	case *[]uint16:
		*p = scalarSlice(arr, nested, reflect.Uint16, asNumber[uint16])
	case *[]uint32:
		*p = scalarSlice(arr, nested, reflect.Uint32, asNumber[uint32])
	case *[]uint64:
		*p = scalarSlice(arr, nested, reflect.Uint64, asNumber[uint64])
	case *[]float32:
		*p = scalarSlice(arr, nested, reflect.Float32, asNumber[float32])
	case *[]float64:
		*p = scalarSlice(arr, nested, reflect.Float64, asNumber[float64])
	case *[]complex64:
		*p = scalarSlice(arr, nested, reflect.Complex64, asNumber[complex64])
	case *[]complex128:
		*p = scalarSlice(arr, nested, reflect.Complex128, asNumber[complex128])
	default:
		return false
	}
	return true
}

func bindTypedMap(dst reflect.Value, obj map[string]any, lenient bool) bool {
	switch p := dst.Addr().Interface().(type) {
	case *map[string]any:
		*p = obj
	case *map[string]bool:
		*p = scalarMap(obj, lenient, reflect.Bool, asBool)
	case *map[string]string:
		*p = scalarMap(obj, lenient, reflect.String, asString)
	case *map[string]int:
		*p = scalarMap(obj, lenient, reflect.Int, asNumber[int])
	case *map[string]int8:
		*p = scalarMap(obj, lenient, reflect.Int8, asNumber[int8])
	case *map[string]int16:
		*p = scalarMap(obj, lenient, reflect.Int16, asNumber[int16])
	case *map[string]int32:
		*p = scalarMap(obj, lenient, reflect.Int32, asNumber[int32])
	case *map[string]int64:
		*p = scalarMap(obj, lenient, reflect.Int64, asNumber[int64])
	case *map[string]uint:
		*p = scalarMap(obj, lenient, reflect.Uint, asNumber[uint])
	case *map[string]uint8:
		*p = scalarMap(obj, lenient, reflect.Uint8, asNumber[uint8])
	case *map[string]uint16:
		*p = scalarMap(obj, lenient, reflect.Uint16, asNumber[uint16])
	case *map[string]uint32:
		*p = scalarMap(obj, lenient, reflect.Uint32, asNumber[uint32])
	case *map[string]uint64:
		*p = scalarMap(obj, lenient, reflect.Uint64, asNumber[uint64])
	case *map[string]float32:
		*p = scalarMap(obj, lenient, reflect.Float32, asNumber[float32])
	case *map[string]float64:
		*p = scalarMap(obj, lenient, reflect.Float64, asNumber[float64])
	case *map[string]complex64:
		*p = scalarMap(obj, lenient, reflect.Complex64, asNumber[complex64])
	case *map[string]complex128:
		*p = scalarMap(obj, lenient, reflect.Complex128, asNumber[complex128])
	default:
		return false
	}
	return true
}

func bindTypedStrSlice(dst reflect.Value, values []string) bool {
	switch p := dst.Addr().Interface().(type) {
	case *[]any:
		*p = slice.StrParseAny(values)
	case *[]bool:
		*p = slice.StrParseBool(values)
	case *[]int:
		*p = slice.StrParseInt(values)
	case *[]int8:
		*p = slice.StrParseInt8(values)
	case *[]int16:
		*p = slice.StrParseInt16(values)
	case *[]int32:
		*p = slice.StrParseInt32(values)
	case *[]int64:
		*p = slice.StrParseInt64(values)
	case *[]uint:
		*p = slice.StrParseUint(values)
	case *[]uint8:
		*p = slice.StrParseUint8(values)
	case *[]uint16:
		*p = slice.StrParseUint16(values)
	case *[]uint32:
		*p = slice.StrParseUint32(values)
	case *[]uint64:
		*p = slice.StrParseUint64(values)
	case *[]float32:
		*p = slice.StrParseFloat32(values)
	case *[]float64:
		*p = slice.StrParseFloat64(values)
	case *[]complex64:
		*p = slice.StrParseComplex64(values)
	case *[]complex128:
		*p = slice.StrParseComplex128(values)
	default:
		return false
	}
	return true
}
