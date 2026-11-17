package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
)

type listedPackage struct {
	ImportPath string
	Export     string
	DepOnly    bool
	Error      *struct{ Err string }
	Module     *struct {
		Path string
		Main bool
	}
}

type surface struct {
	modulePath  string
	publicPaths []string
	packages    []*types.Package
	public      map[string]bool
}

func loadSurface(dir string) (*surface, error) {
	cmd := exec.Command("go", "list", "-export", "-deps", "-json", "./...")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, errors.New("go list: " + strings.TrimSpace(stderr.String()))
	}
	exportByPath := map[string]string{}
	s := &surface{public: map[string]bool{}}
	decoder := json.NewDecoder(bytes.NewReader(data))
	for decoder.More() {
		var p listedPackage
		if err := decoder.Decode(&p); err != nil {
			return nil, err
		}
		if p.Error != nil {
			return nil, errors.New(p.ImportPath + ": " + p.Error.Err)
		}
		exportByPath[p.ImportPath] = p.Export
		if p.DepOnly || p.Module == nil || !p.Module.Main {
			continue
		}
		s.modulePath = p.Module.Path
		if isPublicPath(p.ImportPath, p.Module.Path) {
			s.publicPaths = append(s.publicPaths, p.ImportPath)
			s.public[p.ImportPath] = true
		}
	}
	sort.Strings(s.publicPaths)
	lookup := func(path string) (io.ReadCloser, error) {
		file, ok := exportByPath[path]
		if !ok || file == "" {
			return nil, errors.New("no export data for " + path)
		}
		return os.Open(file)
	}
	imp := importer.ForCompiler(token.NewFileSet(), "gc", lookup)
	for _, path := range s.publicPaths {
		pkg, err := imp.Import(path)
		if err != nil {
			return nil, err
		}
		s.packages = append(s.packages, pkg)
	}
	return s, nil
}

func isPublicPath(path, modulePath string) bool {
	rel := strings.TrimPrefix(strings.TrimPrefix(path, modulePath), "/")
	for _, element := range strings.Split(rel, "/") {
		if element == "internal" || element == "sample" || element == "bench" {
			return false
		}
	}
	return true
}

func (s *surface) relPath(path string) string {
	if path == s.modulePath {
		return "."
	}
	return strings.TrimPrefix(path, s.modulePath+"/")
}

func (s *surface) lineName(path string) string {
	name := s.modulePath[strings.LastIndex(s.modulePath, "/")+1:]
	if path == s.modulePath {
		return name
	}
	return name + "/" + s.relPath(path)
}

func (s *surface) lines() []string {
	w := &lineWriter{surface: s, reached: map[*types.TypeName]bool{}}
	for _, pkg := range s.packages {
		prefix := "pkg " + s.lineName(pkg.Path()) + ", "
		scope := pkg.Scope()
		for _, name := range scope.Names() {
			if !token.IsExported(name) {
				continue
			}
			w.object(prefix, scope.Lookup(name))
		}
	}
	for len(w.queue) > 0 {
		named := w.queue[0]
		w.queue = w.queue[1:]
		w.reach(named)
	}
	sort.Strings(w.lines)
	unique := w.lines[:0]
	for i, line := range w.lines {
		if i == 0 || line != w.lines[i-1] {
			unique = append(unique, line)
		}
	}
	return unique
}

type lineWriter struct {
	surface *surface
	lines   []string
	queue   []*types.Named
	reached map[*types.TypeName]bool
}

func (w *lineWriter) add(line string) {
	w.lines = append(w.lines, line)
}

func (w *lineWriter) typeString(t types.Type) string {
	printer := &typePrinter{mention: w.mention}
	printer.write(t)
	return printer.String()
}

func (w *lineWriter) signature(sig *types.Signature) string {
	printer := &typePrinter{mention: w.mention}
	printer.signature(sig)
	return printer.String()
}

func (w *lineWriter) mention(named *types.Named) {
	named = named.Origin()
	obj := named.Obj()
	if obj.Pkg() == nil || w.reached[obj] || !w.inModule(obj.Pkg().Path()) {
		return
	}
	if obj.Exported() && w.surface.public[obj.Pkg().Path()] {
		return
	}
	w.reached[obj] = true
	w.queue = append(w.queue, named)
}

func (w *lineWriter) inModule(path string) bool {
	modulePath := w.surface.modulePath
	return path == modulePath || strings.HasPrefix(path, modulePath+"/")
}

func (w *lineWriter) object(prefix string, obj types.Object) {
	switch obj := obj.(type) {
	case *types.Const:
		w.add(prefix + "const " + obj.Name() + " " + w.typeString(obj.Type()) + " = " + obj.Val().ExactString())
	case *types.Var:
		w.add(prefix + "var " + obj.Name() + " " + w.typeString(obj.Type()))
	case *types.Func:
		w.add(prefix + "func " + obj.Name() + w.signature(obj.Signature()))
	case *types.TypeName:
		w.typeName(prefix, obj)
	}
}

func (w *lineWriter) typeName(prefix string, obj *types.TypeName) {
	if alias, ok := obj.Type().(*types.Alias); ok {
		w.add(prefix + "type " + obj.Name() + " = " + w.typeString(alias.Rhs()))
		return
	}
	if obj.IsAlias() {
		w.add(prefix + "type " + obj.Name() + " = " + w.typeString(obj.Type()))
		return
	}
	named, ok := obj.Type().(*types.Named)
	if !ok {
		return
	}
	name := obj.Name() + w.typeParams(named.TypeParams())
	head := prefix + "type " + name
	switch underlying := named.Underlying().(type) {
	case *types.Struct:
		w.add(head + " struct")
		w.fields(head+" struct, ", underlying)
	case *types.Interface:
		w.add(head + " interface")
		for i := 0; i < underlying.NumMethods(); i++ {
			method := underlying.Method(i)
			if method.Exported() {
				w.add(head + " interface, " + method.Name() + w.signature(method.Signature()))
			}
		}
	default:
		w.add(head + " " + w.typeString(underlying))
	}
	w.methods(prefix, named)
}

func (w *lineWriter) typeParams(list *types.TypeParamList) string {
	if list.Len() == 0 {
		return ""
	}
	printer := &typePrinter{mention: w.mention}
	printer.typeParams(list)
	return printer.String()
}

func (w *lineWriter) fields(prefix string, st *types.Struct) {
	for i := 0; i < st.NumFields(); i++ {
		field := st.Field(i)
		if !field.Exported() {
			if field.Embedded() {
				w.typeString(field.Type())
			}
			continue
		}
		line := prefix + field.Name() + " " + w.typeString(field.Type())
		if field.Embedded() {
			line += " embedded"
		}
		w.add(line)
	}
}

func (w *lineWriter) methods(prefix string, named *types.Named) {
	for i := 0; i < named.NumMethods(); i++ {
		method := named.Method(i)
		if !method.Exported() {
			continue
		}
		sig := method.Signature()
		w.add(prefix + "method (" + w.typeString(sig.Recv().Type()) + ") " + method.Name() + w.signature(sig))
	}
}

func (w *lineWriter) reach(named *types.Named) {
	prefix := "reach " + named.Obj().Pkg().Name() + "." + named.Obj().Name() + ", "
	switch underlying := named.Underlying().(type) {
	case *types.Struct:
		w.fields(prefix+"field ", underlying)
	case *types.Interface:
		receiver := w.typeString(named)
		for i := 0; i < underlying.NumMethods(); i++ {
			method := underlying.Method(i)
			if method.Exported() {
				w.add(prefix + "method (" + receiver + ") " + method.Name() + w.signature(method.Signature()))
			}
		}
	default:
		w.add(prefix + "type " + w.typeString(underlying))
	}
	w.methods(prefix, named)
}
