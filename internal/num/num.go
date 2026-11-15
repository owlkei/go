package num

import (
	"math"
	"math/bits"
	"reflect"
)

func F64ToAnyNum(f64 float64, kind reflect.Kind) any {
	v, _ := F64ToAnyNumOK(f64, kind)
	return v
}

func F64ToAnyNumOK(f64 float64, kind reflect.Kind) (any, bool) {
	switch kind {
	case reflect.Int:
		n, ok := toSigned(f64, bits.UintSize)
		return int(n), ok

	case reflect.Int8:
		n, ok := toSigned(f64, 8)
		return int8(n), ok

	case reflect.Int16:
		n, ok := toSigned(f64, 16)
		return int16(n), ok

	case reflect.Int32:
		n, ok := toSigned(f64, 32)
		return int32(n), ok

	case reflect.Int64:
		return toSigned(f64, 64)

	case reflect.Uint:
		n, ok := toUnsigned(f64, bits.UintSize)
		return uint(n), ok

	case reflect.Uint8:
		n, ok := toUnsigned(f64, 8)
		return uint8(n), ok

	case reflect.Uint16:
		n, ok := toUnsigned(f64, 16)
		return uint16(n), ok

	case reflect.Uint32:
		n, ok := toUnsigned(f64, 32)
		return uint32(n), ok

	case reflect.Uint64:
		return toUnsigned(f64, 64)

	case reflect.Float32:
		return toFloat32(f64)

	case reflect.Float64:
		return f64, true

	case reflect.Complex64:
		f32, ok := toFloat32(f64)
		return complex(f32, 0), ok

	case reflect.Complex128:
		return complex(f64, 0), true
	}

	return 0, false
}

func toSigned(f64 float64, size int) (int64, bool) {
	if math.IsNaN(f64) {
		return 0, false
	}
	maxInt := int64(uint64(math.MaxUint64) >> (65 - size))
	limit := float64(uint64(1) << (size - 1))
	t := math.Trunc(f64)
	if t >= limit {
		return maxInt, false
	}
	if t < -limit {
		return -maxInt - 1, false
	}
	return int64(t), true
}

func toUnsigned(f64 float64, size int) (uint64, bool) {
	if math.IsNaN(f64) {
		return 0, false
	}
	t := math.Trunc(f64)
	if t < 0 {
		return 0, false
	}
	if t >= 2*float64(uint64(1)<<(size-1)) {
		return uint64(math.MaxUint64) >> (64 - size), false
	}
	return uint64(t), true
}

func toFloat32(f64 float64) (float32, bool) {
	if f64 > math.MaxFloat32 && !math.IsInf(f64, 1) {
		return math.MaxFloat32, false
	}
	if f64 < -math.MaxFloat32 && !math.IsInf(f64, -1) {
		return -math.MaxFloat32, false
	}
	return float32(f64), true
}
