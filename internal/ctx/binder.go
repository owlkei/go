package ctx

import (
	"errors"
	"fmt"
	"net/textproto"
	"reflect"
	"sync"
)

const (
	TagBind = "bind"
)

var ErrBindTarget = errors.New("owlky: bind target must be a struct")

type Binder struct {
	typ    reflect.Type
	fields []bindField
	flHint int
}

type bindField struct {
	node     *bindNode
	typ      reflect.Type
	name     string
	tag      string
	header   string
	nsSuffix string
	index    int
	tagIndex int
	kind     reflect.Kind
	strArr   strArrKind
	file     fileKind
}

type bindNode struct {
	typ    reflect.Type
	elem   *bindNode
	binder *Binder
	kind   reflect.Kind
	class  valueClass
	ok     bool
}

type valueClass uint8

const (
	classOther valueClass = iota
	classBool
	classInt
	classUint
	classFloat
	classComplex
	classString
	classAny
	classSlice
	classMap
	classStruct
	classPointer
)

type strArrKind uint8

const (
	strArrNone strArrKind = iota
	strArrScalar
	strArrAny
	strArrStrings
	strArrSlice
)

type fileKind uint8

const (
	fileNone fileKind = iota
	filePtr
	fileSlice
)

var (
	bindersByType   sync.Map
	anyType         = reflect.TypeFor[any]()
	stringSliceType = reflect.TypeFor[[]string]()
	dataFileType    = reflect.TypeFor[*DataFile]()
	dataFilesType   = reflect.TypeFor[[]*DataFile]()
)

func BinderOf(t reflect.Type) (*Binder, error) {
	if t == nil || t.Kind() != reflect.Struct {
		return nil, ErrBindTarget
	}
	if b, ok := bindersByType.Load(t); ok {
		return b.(*Binder), nil
	}

	building := map[reflect.Type]*Binder{}
	b := buildBinder(t, building)
	for typ, built := range building {
		if typ != t {
			bindersByType.LoadOrStore(typ, built)
		}
	}
	actual, _ := bindersByType.LoadOrStore(t, b)
	return actual.(*Binder), nil
}

func mustBinderOf(t reflect.Type) *Binder {
	b, err := BinderOf(t)
	if err != nil {
		panic(fmt.Errorf("%w, got %v", err, t))
	}
	return b
}

func binderOfValue(s any) *Binder {
	return mustBinderOf(reflect.TypeOf(s))
}

func buildBinder(t reflect.Type, building map[reflect.Type]*Binder) *Binder {
	if b, ok := building[t]; ok {
		return b
	}
	if b, ok := bindersByType.Load(t); ok {
		return b.(*Binder)
	}

	b := &Binder{typ: t}
	building[t] = b

	n := t.NumField()
	fields := make([]bindField, 0, n)
	for i := 0; i < n; i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		bindValues, ok := sf.Tag.Lookup(TagBind)
		if !ok {
			continue
		}
		params := GetTagParams(bindValues)
		if len(params) == 0 {
			continue
		}
		tagIndex, tag := GetTagParamIndex(params[0])
		node := buildNode(sf.Type, building)
		fields = append(fields, bindField{
			node:     node,
			typ:      sf.Type,
			name:     sf.Name,
			tag:      tag,
			header:   textproto.CanonicalMIMEHeaderKey(tag),
			nsSuffix: t.Name() + "." + sf.Name,
			index:    i,
			tagIndex: tagIndex,
			kind:     sf.Type.Kind(),
			strArr:   strArrKindOf(sf.Type),
			file:     fileKindOf(sf.Type),
		})
	}
	b.fields = fields
	b.flHint = len(fields)
	for i := range fields {
		if nested := fields[i].node.binder; nested != nil && nested != b {
			b.flHint += nested.flHint
		}
	}
	return b
}

func buildNode(t reflect.Type, building map[reflect.Type]*Binder) *bindNode {
	node := &bindNode{typ: t, kind: t.Kind(), class: classOf(t)}
	switch node.class {
	case classBool, classInt, classUint, classFloat, classComplex, classString:
		node.ok = true
	case classAny:
		node.ok = t == anyType
	case classStruct:
		node.binder = buildBinder(t, building)
		node.ok = true
	case classSlice, classPointer:
		node.elem = buildNode(t.Elem(), building)
		node.ok = node.elem.ok && node.elem.class != classPointer
	case classMap:
		node.elem = buildNode(t.Elem(), building)
		node.ok = t.Key().Kind() == reflect.String && node.elem.ok && node.elem.class != classPointer
	}
	return node
}

func classOf(t reflect.Type) valueClass {
	switch t.Kind() {
	case reflect.Bool:
		return classBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return classInt
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return classUint
	case reflect.Float32, reflect.Float64:
		return classFloat
	case reflect.Complex64, reflect.Complex128:
		return classComplex
	case reflect.String:
		return classString
	case reflect.Interface:
		return classAny
	case reflect.Slice:
		return classSlice
	case reflect.Map:
		return classMap
	case reflect.Struct:
		return classStruct
	case reflect.Pointer:
		return classPointer
	}
	return classOther
}

func isScalarClass(c valueClass) bool {
	return c >= classBool && c <= classString
}

func strArrKindOf(t reflect.Type) strArrKind {
	c := classOf(t)
	switch {
	case isScalarClass(c):
		return strArrScalar
	case c == classAny:
		if stringType.Implements(t) {
			return strArrAny
		}
	case c == classSlice:
		elem := classOf(t.Elem())
		switch {
		case t.Elem() == stringType && stringSliceType.AssignableTo(t):
			return strArrStrings
		case isScalarClass(elem):
			return strArrSlice
		case elem == classAny && stringType.Implements(t.Elem()):
			return strArrSlice
		}
	}
	return strArrNone
}

func fileKindOf(t reflect.Type) fileKind {
	switch {
	case t == dataFileType:
		return filePtr
	case t.Kind() == reflect.Slice && dataFilesType.AssignableTo(t):
		return fileSlice
	}
	return fileNone
}

func (b *Binder) StrArr(d map[string][]string) (any, []FieldLevel) {
	fls := make([]FieldLevel, 0, len(b.fields))
	return b.bindStrArr(d, false, &fls), fls
}

func (b *Binder) Header(h Header) (any, []FieldLevel) {
	fls := make([]FieldLevel, 0, len(b.fields))
	return b.bindStrArr(h, true, &fls), fls
}

func (b *Binder) Struct(d map[string]any) (any, []FieldLevel) {
	fls := make([]FieldLevel, 0, b.flHint)
	return b.bindTop(d, &fls, "", ""), fls
}

func (b *Binder) File(f File) (map[string][]*DataFile, any) {
	return b.bindFile(f)
}

func (b *Binder) bindTop(d map[string]any, fls *[]FieldLevel, parentNS, parentTag string) any {
	ptr := reflect.New(b.typ)
	b.bindStruct(d, ptr.Elem(), fls, parentNS, parentTag)
	return ptr.Elem().Interface()
}
