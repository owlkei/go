package ctx

import (
	"reflect"
	"slices"
	"strconv"

	"github.com/dangduoc08/ginject/internal/num"
)

func BindStruct(d map[string]any, fls *[]FieldLevel, s any, parentNS string, parentTag string) (any, []FieldLevel) {
	t, ok := s.(reflect.Type)
	if !ok {
		t = reflect.TypeOf(s)
	}
	b := mustBinderOf(t)
	if fls == nil {
		fls = &[]FieldLevel{}
	}
	*fls = slices.Grow(*fls, b.flHint)
	return b.bindTop(d, fls, parentNS, parentTag), *fls
}

func (b *Binder) bindStruct(d map[string]any, dst reflect.Value, fls *[]FieldLevel, parentNS, parentTag string) {
	for i := range b.fields {
		f := &b.fields[i]
		v, present := d[f.tag]
		if fls == nil {
			if present {
				bindValue(f.node, v, dst.Field(f.index), nil, "", "")
			}
			continue
		}

		ns := f.nsSuffix
		if parentNS != "" {
			ns = parentNS + "." + f.nsSuffix
		}
		nestedTag := f.tag
		if parentTag != "" {
			nestedTag = parentTag + "." + f.tag
		}

		if !present {
			*fls = append(*fls, FieldLevel{
				tag:       f.tag,
				nestedTag: nestedTag,
				ns:        ns,
				field:     f.name,
				kind:      f.kind,
				typ:       f.typ,
			})
			continue
		}

		fv := dst.Field(f.index)
		if !bindValue(f.node, v, fv, fls, ns, nestedTag) {
			continue
		}
		*fls = append(*fls, FieldLevel{
			val:       fieldLevelValue(f.node, fv),
			tag:       f.tag,
			nestedTag: nestedTag,
			ns:        ns,
			field:     f.name,
			kind:      f.kind,
			typ:       f.typ,
			isVal:     true,
		})
	}
}

func fieldLevelValue(node *bindNode, fv reflect.Value) any {
	if node.class == classPointer && node.elem.class == classStruct {
		return fv.Elem().Interface()
	}
	return fv.Interface()
}

func bindValue(node *bindNode, v any, dst reflect.Value, fls *[]FieldLevel, ns, tag string) bool {
	switch node.class {
	case classBool, classInt, classUint, classFloat, classComplex, classString:
		return setScalar(node, v, dst)

	case classAny:
		if !node.ok {
			return false
		}
		if v != nil {
			dst.Set(reflect.ValueOf(v))
		}
		return true

	case classSlice:
		arr, ok := v.([]any)
		if !ok || !node.ok {
			return false
		}
		bindSlice(node, arr, dst, fls, ns, tag, false)
		return true

	case classMap:
		obj, ok := v.(map[string]any)
		if !ok || !node.ok {
			return false
		}
		bindMap(node, obj, dst, fls, ns, tag, false)
		return true

	case classStruct:
		obj, ok := v.(map[string]any)
		if !ok {
			return false
		}
		node.binder.bindStruct(obj, dst, fls, ns, tag)
		return true

	case classPointer:
		if !node.ok {
			return false
		}
		ptr := reflect.New(node.elem.typ)
		if !bindValue(node.elem, v, ptr.Elem(), fls, ns, tag) {
			return false
		}
		dst.Set(ptr)
		return true
	}
	return false
}

func setScalar(node *bindNode, v any, dst reflect.Value) bool {
	switch node.class {
	case classBool:
		b, ok := v.(bool)
		if !ok {
			return false
		}
		dst.SetBool(b)
		return true

	case classString:
		s, ok := v.(string)
		if !ok {
			return false
		}
		dst.SetString(s)
		return true
	}

	f, ok := v.(float64)
	if !ok {
		return false
	}
	n, ok := num.F64ToAnyNumOK(f, node.kind)
	if !ok {
		return false
	}
	rv := reflect.ValueOf(n)
	switch node.class {
	case classInt:
		dst.SetInt(rv.Int())
	case classUint:
		dst.SetUint(rv.Uint())
	case classFloat:
		dst.SetFloat(rv.Float())
	case classComplex:
		dst.SetComplex(rv.Complex())
	}
	return true
}

func setAny(dst reflect.Value, v any) {
	if v != nil {
		dst.Set(reflect.ValueOf(v))
	}
}

func bindSlice(node *bindNode, arr []any, dst reflect.Value, fls *[]FieldLevel, ns, tag string, nested bool) {
	elem := node.elem
	if (isScalarClass(elem.class) || elem.class == classAny) && bindTypedSlice(dst, arr, nested) {
		return
	}
	switch elem.class {
	case classBool, classInt, classUint, classFloat, classComplex, classString:
		if nested {
			out := reflect.MakeSlice(node.typ, len(arr), len(arr))
			n := 0
			for _, el := range arr {
				if setScalar(elem, el, out.Index(n)) {
					n++
				}
			}
			dst.Set(out.Slice(0, n))
			return
		}
		if len(arr) == 0 {
			return
		}
		out := reflect.MakeSlice(node.typ, len(arr), len(arr))
		for i, el := range arr {
			if !setScalar(elem, el, out.Index(i)) {
				dst.Set(reflect.MakeSlice(node.typ, 0, 0))
				return
			}
		}
		dst.Set(out)

	case classAny:
		if nested {
			out := reflect.MakeSlice(node.typ, len(arr), len(arr))
			for i, el := range arr {
				setAny(out.Index(i), el)
			}
			dst.Set(out)
			return
		}
		dst.Set(reflect.ValueOf(arr))

	case classSlice:
		out := reflect.MakeSlice(node.typ, 0, len(arr))
		for _, el := range arr {
			sub, ok := el.([]any)
			if !ok {
				continue
			}
			out = reflect.Append(out, reflect.Zero(elem.typ))
			bindSlice(elem, sub, out.Index(out.Len()-1), fls, ns, tag, true)
		}
		dst.Set(out)

	case classMap, classStruct:
		out := reflect.MakeSlice(node.typ, 0, len(arr))
		for i, el := range arr {
			obj, ok := el.(map[string]any)
			if !ok {
				continue
			}
			out = reflect.Append(out, reflect.Zero(elem.typ))
			elNS, elTag := ns, tag
			if fls != nil && !nested {
				idx := strconv.Itoa(i)
				elNS, elTag = ns+"."+idx, tag+"."+idx
			}
			if elem.class == classMap {
				bindMap(elem, obj, out.Index(out.Len()-1), fls, elNS, elTag, false)
			} else {
				elem.binder.bindStruct(obj, out.Index(out.Len()-1), fls, elNS, elTag)
			}
		}
		dst.Set(out)
	}
}

func bindMap(node *bindNode, obj map[string]any, dst reflect.Value, fls *[]FieldLevel, ns, tag string, lenient bool) {
	elem := node.elem
	if (isScalarClass(elem.class) || elem.class == classAny) && bindTypedMap(dst, obj, lenient) {
		return
	}
	keyType := node.typ.Key()

	m := reflect.MakeMapWithSize(node.typ, len(obj))
	tmp := reflect.New(elem.typ).Elem()
	for k, v := range obj {
		switch elem.class {
		case classBool, classInt, classUint, classFloat, classComplex, classString:
			if !setScalar(elem, v, tmp) {
				if lenient {
					continue
				}
				dst.Set(reflect.MakeMapWithSize(node.typ, 0))
				return
			}

		case classAny:
			tmp.SetZero()
			setAny(tmp, v)

		case classSlice:
			sub, ok := v.([]any)
			if !ok {
				continue
			}
			tmp.SetZero()
			bindSlice(elem, sub, tmp, fls, keyed(fls, ns, k), keyed(fls, tag, k), false)

		case classMap:
			sub, ok := v.(map[string]any)
			if !ok {
				continue
			}
			bindMap(elem, sub, tmp, fls, ns, tag, true)

		case classStruct:
			sub, ok := v.(map[string]any)
			if !ok {
				continue
			}
			tmp.SetZero()
			elem.binder.bindStruct(sub, tmp, fls, keyed(fls, ns, k), keyed(fls, tag, k))
		}
		m.SetMapIndex(mapKey(keyType, k), tmp)
	}
	dst.Set(m)
}

func keyed(fls *[]FieldLevel, prefix, key string) string {
	if fls == nil {
		return ""
	}
	return prefix + "." + key
}

func mapKey(keyType reflect.Type, k string) reflect.Value {
	kv := reflect.ValueOf(k)
	if keyType != stringType {
		return kv.Convert(keyType)
	}
	return kv
}
