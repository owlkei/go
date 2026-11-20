package ctx

import (
	"reflect"
	"slices"
	"strconv"
	"strings"

	"go.owlkei.com/owlkei/internal/slice"
)

func BindStrArr(d map[string][]string, fls *[]FieldLevel, s any) (any, []FieldLevel) {
	b := binderOfValue(s)
	if fls == nil {
		fls = &[]FieldLevel{}
	}
	*fls = slices.Grow(*fls, len(b.fields))
	return b.bindStrArr(d, false, fls), *fls
}

func (b *Binder) bindStrArr(d map[string][]string, header bool, fls *[]FieldLevel) any {
	ptr := reflect.New(b.typ)
	dst := ptr.Elem()
	for i := range b.fields {
		f := &b.fields[i]
		key := f.tag
		if header {
			key = f.header
		}
		values, present := d[key]

		if !present {
			if fls != nil {
				*fls = append(*fls, FieldLevel{
					tag:       f.tag,
					nestedTag: f.tag,
					ns:        f.nsSuffix,
					field:     f.name,
					index:     f.tagIndex,
					kind:      f.kind,
					typ:       f.typ,
				})
			}
			continue
		}

		fv := dst.Field(f.index)
		if !setStrArr(f, values, fv) {
			continue
		}
		if fls != nil {
			*fls = append(*fls, FieldLevel{
				val:       fv.Interface(),
				tag:       f.tag,
				nestedTag: f.tag,
				ns:        f.nsSuffix,
				field:     f.name,
				index:     f.tagIndex,
				kind:      f.kind,
				typ:       f.typ,
				isVal:     true,
			})
		}
	}
	return dst.Interface()
}

func setStrArr(f *bindField, values []string, dst reflect.Value) bool {
	switch f.strArr {
	case strArrScalar:
		if s, ok := slice.Get(values, f.tagIndex); ok {
			parseStr(f.kind, s, dst)
		}
		return true

	case strArrAny:
		s, _ := slice.Get(values, f.tagIndex)
		dst.Set(reflect.ValueOf(s))
		return true

	case strArrStrings:
		dst.Set(reflect.ValueOf(values))
		return true

	case strArrSlice:
		if bindTypedStrSlice(dst, values) {
			return true
		}
		elem := f.typ.Elem()
		out := reflect.MakeSlice(f.typ, len(values), len(values))
		if elem.Kind() == reflect.Interface {
			for i, s := range values {
				out.Index(i).Set(reflect.ValueOf(s))
			}
		} else {
			kind := elem.Kind()
			for i, s := range values {
				parseStr(kind, s, out.Index(i))
			}
		}
		dst.Set(out)
		return true
	}
	return false
}

func parseStr(kind reflect.Kind, s string, dst reflect.Value) {
	switch kind {
	case reflect.String:
		dst.SetString(s)

	case reflect.Bool:
		v, _ := strconv.ParseBool(s)
		dst.SetBool(v)

	case reflect.Int:
		if v, err := strconv.Atoi(s); err == nil {
			dst.SetInt(int64(v))
		}

	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v, err := strconv.ParseInt(s, 10, dst.Type().Bits()); err == nil {
			dst.SetInt(v)
		}

	case reflect.Uint:
		if v, err := strconv.ParseUint(s, 10, 0); err == nil {
			dst.SetUint(v)
		}

	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if v, err := strconv.ParseUint(s, 10, dst.Type().Bits()); err == nil {
			dst.SetUint(v)
		}

	case reflect.Float32, reflect.Float64:
		if v, err := strconv.ParseFloat(s, dst.Type().Bits()); err == nil {
			dst.SetFloat(v)
		}

	case reflect.Complex64, reflect.Complex128:
		if v, err := strconv.ParseComplex(strings.ReplaceAll(s, " ", ""), dst.Type().Bits()); err == nil {
			dst.SetComplex(v)
		}
	}
}
