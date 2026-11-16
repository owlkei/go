package log

import (
	"reflect"
	"sync"
	"unsafe"
)

const logTagKey = "log"

type typeInfo struct {
	leaf      bool
	text      bool
	fixed     bool
	plain     bool
	plainKeep bool
	fields    []fieldInfo
	unsafeRd  bool
}

type fieldInfo struct {
	index      int
	key        string
	path       string
	unexported bool
}

var typeInfoByType sync.Map

func typeInfoOf(t reflect.Type) *typeInfo {
	if info, ok := typeInfoByType.Load(t); ok {
		return info.(*typeInfo)
	}
	info := buildTypeInfo(t, map[reflect.Type]bool{})
	actual, _ := typeInfoByType.LoadOrStore(t, info)
	return actual.(*typeInfo)
}

func buildTypeInfo(t reflect.Type, visiting map[reflect.Type]bool) *typeInfo {
	info := &typeInfo{}
	if t == timeType || t == durationType {
		info.leaf, info.fixed, info.plain, info.plainKeep = true, true, true, true
		return info
	}
	if implementsText(t) {
		info.text, info.plainKeep = true, true
		return info
	}
	if visiting[t] {
		return info
	}
	visiting[t] = true
	defer delete(visiting, t)

	switch t.Kind() {
	case reflect.Struct:
		info.fields, info.unsafeRd = structFields(t)
		if len(info.fields) == 0 {
			info.leaf, info.fixed, info.plain, info.plainKeep = true, true, true, true
		}
	case reflect.Interface:
	case reflect.Pointer, reflect.Slice, reflect.Array:
		elem := buildTypeInfo(t.Elem(), visiting)
		info.fixed, info.plain, info.plainKeep = elem.fixed, elem.plain, elem.plainKeep
	case reflect.Map:
		if !isWalkableKey(t.Key().Kind()) {
			info.leaf, info.fixed, info.plain, info.plainKeep = true, true, true, true
			break
		}
		elem := buildTypeInfo(t.Elem(), visiting)
		info.plain, info.plainKeep = elem.plain, elem.plainKeep
	default:
		info.leaf, info.fixed, info.plain, info.plainKeep = true, true, true, true
	}
	return info
}

func isWalkableKey(k reflect.Kind) bool {
	switch k {
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	}
	return false
}

func structFields(t reflect.Type) ([]fieldInfo, bool) {
	var out []fieldInfo
	var unexported bool
	for i := range t.NumField() {
		f := t.Field(i)
		tag, hasTag := f.Tag.Lookup(logTagKey)
		if !hasTag {
			if f.PkgPath != "" {
				continue
			}
			out = append(out, fieldInfo{index: i, key: f.Name, path: lowerASCII(f.Name)})
			continue
		}
		if tag == "-" {
			continue
		}
		hidden := f.PkgPath != ""
		unexported = unexported || hidden
		out = append(out, fieldInfo{index: i, key: tag, path: lowerASCII(tag), unexported: hidden})
	}
	return out, unexported
}

func taggedFields(rv reflect.Value) []kv {
	info := typeInfoOf(rv.Type())
	if len(info.fields) == 0 {
		return nil
	}
	rv = addressable(rv, info)
	out := make([]kv, len(info.fields))
	for i, f := range info.fields {
		out[i] = kv{f.key, fieldValue(rv, f)}
	}
	return out
}

func addressable(rv reflect.Value, info *typeInfo) reflect.Value {
	if !info.unsafeRd || rv.CanAddr() {
		return rv
	}
	addr := reflect.New(rv.Type()).Elem()
	addr.Set(rv)
	return addr
}

func fieldValue(rv reflect.Value, f fieldInfo) reflect.Value {
	fv := rv.Field(f.index)
	if f.unexported {
		fv = reflect.NewAt(fv.Type(), unsafe.Pointer(fv.UnsafeAddr())).Elem()
	}
	return fv
}
