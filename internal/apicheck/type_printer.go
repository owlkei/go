package main

import (
	"go/types"
	"strconv"
	"strings"
)

type typePrinter struct {
	strings.Builder
	mention func(*types.Named)
}

func (p *typePrinter) write(t types.Type) {
	switch t := t.(type) {
	case *types.Basic:
		if t.Kind() == types.UnsafePointer {
			p.WriteString("unsafe.")
		}
		p.WriteString(t.Name())
	case *types.Pointer:
		p.WriteString("*")
		p.write(t.Elem())
	case *types.Slice:
		p.WriteString("[]")
		p.write(t.Elem())
	case *types.Array:
		p.WriteString("[" + strconv.FormatInt(t.Len(), 10) + "]")
		p.write(t.Elem())
	case *types.Map:
		p.WriteString("map[")
		p.write(t.Key())
		p.WriteString("]")
		p.write(t.Elem())
	case *types.Chan:
		p.chanType(t)
	case *types.Signature:
		p.WriteString("func")
		p.signature(t)
	case *types.Struct:
		p.structType(t)
	case *types.Interface:
		p.interfaceType(t)
	case *types.Union:
		for i := 0; i < t.Len(); i++ {
			if i > 0 {
				p.WriteString(" | ")
			}
			if t.Term(i).Tilde() {
				p.WriteString("~")
			}
			p.write(t.Term(i).Type())
		}
	case *types.TypeParam:
		p.WriteString(t.Obj().Name())
	case *types.Alias:
		p.qualified(t.Obj())
		p.typeArgs(t.TypeArgs())
		if t.Obj().Pkg() != nil {
			p.mentionIn(t.Rhs())
		}
	case *types.Named:
		p.qualified(t.Obj())
		p.typeArgs(t.TypeArgs())
		p.mention(t)
	default:
		p.WriteString(types.TypeString(t, nil))
	}
}

func (p *typePrinter) mentionIn(t types.Type) {
	discard := &typePrinter{mention: p.mention}
	discard.write(t)
}

func (p *typePrinter) qualified(obj *types.TypeName) {
	if obj.Pkg() != nil {
		p.WriteString(obj.Pkg().Name() + ".")
	}
	p.WriteString(obj.Name())
}

func (p *typePrinter) typeArgs(list *types.TypeList) {
	if list.Len() == 0 {
		return
	}
	p.WriteString("[")
	for i := 0; i < list.Len(); i++ {
		if i > 0 {
			p.WriteString(", ")
		}
		p.write(list.At(i))
	}
	p.WriteString("]")
}

func (p *typePrinter) typeParams(list *types.TypeParamList) {
	p.WriteString("[")
	for i := 0; i < list.Len(); i++ {
		if i > 0 {
			p.WriteString(", ")
		}
		p.WriteString(list.At(i).Obj().Name() + " ")
		p.write(list.At(i).Constraint())
	}
	p.WriteString("]")
}

func (p *typePrinter) chanType(t *types.Chan) {
	switch t.Dir() {
	case types.SendOnly:
		p.WriteString("chan<- ")
	case types.RecvOnly:
		p.WriteString("<-chan ")
	default:
		p.WriteString("chan ")
	}
	elem, isChan := t.Elem().(*types.Chan)
	if isChan && t.Dir() == types.SendRecv && elem.Dir() == types.RecvOnly {
		p.WriteString("(")
		p.write(elem)
		p.WriteString(")")
		return
	}
	p.write(t.Elem())
}

func (p *typePrinter) structType(t *types.Struct) {
	p.WriteString("struct{")
	for i := 0; i < t.NumFields(); i++ {
		if i > 0 {
			p.WriteString("; ")
		}
		field := t.Field(i)
		if !field.Embedded() {
			p.WriteString(field.Name() + " ")
		}
		p.write(field.Type())
	}
	p.WriteString("}")
}

func (p *typePrinter) interfaceType(t *types.Interface) {
	if t.Empty() {
		p.WriteString("any")
		return
	}
	p.WriteString("interface{")
	n := 0
	for i := 0; i < t.NumExplicitMethods(); i++ {
		if n > 0 {
			p.WriteString("; ")
		}
		method := t.ExplicitMethod(i)
		p.WriteString(method.Name())
		p.signature(method.Signature())
		n++
	}
	for i := 0; i < t.NumEmbeddeds(); i++ {
		if n > 0 {
			p.WriteString("; ")
		}
		p.write(t.EmbeddedType(i))
		n++
	}
	p.WriteString("}")
}

func (p *typePrinter) signature(sig *types.Signature) {
	if sig.TypeParams().Len() > 0 {
		p.typeParams(sig.TypeParams())
	}
	p.WriteString("(")
	params := sig.Params()
	for i := 0; i < params.Len(); i++ {
		if i > 0 {
			p.WriteString(", ")
		}
		if sig.Variadic() && i == params.Len()-1 {
			p.WriteString("...")
			if slice, ok := params.At(i).Type().(*types.Slice); ok {
				p.write(slice.Elem())
				continue
			}
		}
		p.write(params.At(i).Type())
	}
	p.WriteString(")")
	results := sig.Results()
	switch results.Len() {
	case 0:
	case 1:
		p.WriteString(" ")
		p.write(results.At(0).Type())
	default:
		p.WriteString(" (")
		for i := 0; i < results.Len(); i++ {
			if i > 0 {
				p.WriteString(", ")
			}
			p.write(results.At(i).Type())
		}
		p.WriteString(")")
	}
}
