package core

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/dangduoc08/ginject/internal/color"
	"github.com/dangduoc08/ginject/internal/common"
)

const maxModuleNameStackDepth = 32

var moduleNameFileCache sync.Map

type parsedModuleFile struct {
	fset *token.FileSet
	file *ast.File
	ok   bool
}

type ModuleBuilder struct {
	imports      []any
	asyncImports []any
	providers    []Provider
	controllers  []Controller
	pathStyle    common.PathStyle
}

func NewModuleBuilder() *ModuleBuilder {
	return &ModuleBuilder{}
}

func (m *ModuleBuilder) Imports(modules ...any) *ModuleBuilder {
	m.imports = append(m.imports, modules...)
	return m
}

func (m *ModuleBuilder) AsyncImports(modules ...any) *ModuleBuilder {
	m.asyncImports = append(m.asyncImports, modules...)
	return m
}

func (m *ModuleBuilder) getModuleType() ([]*Module, []any) {
	m.checkImports()

	staticModules := make([]*Module, 0, len(m.imports))
	var moduleFactories []any
	for _, arg := range m.imports {
		if module, ok := arg.(*Module); ok {
			staticModules = append(staticModules, module)
		} else {
			moduleFactories = append(moduleFactories, arg)
		}
	}

	return staticModules, moduleFactories
}

func (m *ModuleBuilder) checkImports() {
	var errors []string
	errors = appendImportErrors(errors, m.imports, "Imports")
	errors = appendImportErrors(errors, m.asyncImports, "AsyncImports")
	if len(errors) > 0 {
		panic(color.FmtRed("invalid module: %s", strings.Join(errors, "\n       ")))
	}

	for _, async := range m.asyncImports {
		for _, imported := range m.imports {
			if sameImport(async, imported) {
				panic(color.FmtRed("invalid module: '%s' is in both Imports and AsyncImports", importLabel(async)))
			}
		}
	}
}

func appendImportErrors(errors []string, entries []any, list string) []string {
	for _, entry := range entries {
		if module, ok := entry.(*Module); ok && module != nil {
			continue
		}
		typeName := "<nil>"
		if entry != nil {
			typeName = reflect.TypeOf(entry).String()
			if isModuleFactoryType(reflect.TypeOf(entry)) {
				continue
			}
		}
		errors = append(errors, "can't import '"+typeName+"' with "+list+importShapesHint)
	}
	return errors
}

const importShapesHint = ": an import is a ginject.Module or a module factory: func(deps...) ginject.Module, func(deps...) (ginject.Module, error) or func(ctx context.Context, deps...) (ginject.Module, error)"

var (
	moduleType  = reflect.TypeFor[*Module]()
	errorType   = reflect.TypeFor[error]()
	contextType = reflect.TypeFor[context.Context]()
)

func isModuleFactoryType(t reflect.Type) bool {
	if t.Kind() != reflect.Func || t.IsVariadic() {
		return false
	}
	switch t.NumOut() {
	case 1:
		if t.Out(0) != moduleType {
			return false
		}
	case 2:
		if t.Out(0) != moduleType || t.Out(1) != errorType {
			return false
		}
	default:
		return false
	}
	for i := 0; i < t.NumIn(); i++ {
		if t.In(i) == contextType && (i != 0 || t.NumOut() != 2) {
			return false
		}
	}
	return true
}

func isModuleFactory(moduleType string) bool {
	return strings.HasPrefix(moduleType, "func(") && strings.HasSuffix(moduleType, "*core.Module")
}

func (m *ModuleBuilder) Providers(providers ...Provider) *ModuleBuilder {
	m.providers = append(m.providers, providers...)
	return m
}

func (m *ModuleBuilder) Controllers(controllers ...Controller) *ModuleBuilder {
	m.controllers = append(m.controllers, controllers...)
	return m
}

func (m *ModuleBuilder) PathStyle(style common.PathStyle) *ModuleBuilder {
	if style != common.PathStyleKebab && style != common.PathStyleAsWritten {
		panic(color.FmtRed("invalid module: PathStyle needs ginject.PathStyleKebab or ginject.PathStyleAsWritten"))
	}
	m.pathStyle = style
	return m
}

func (m *ModuleBuilder) Build() *Module {
	staticModules, moduleFactories := m.getModuleType()

	module := &Module{
		imports:         slices.Clone(m.imports),
		asyncImports:    slices.Clone(m.asyncImports),
		staticModules:   staticModules,
		moduleFactories: moduleFactories,
		providers:       m.providers,
		controllers:     m.controllers,
		pathStyle:       m.pathStyle,
	}

	module.id = strconv.FormatUint(uint64(reflect.ValueOf(module).Pointer()), 10)

	module.Name = resolveModuleName()

	return module
}

func resolveModuleName() string {
	pcs := make([]uintptr, maxModuleNameStackDepth)
	n := runtime.Callers(3, pcs)
	if n == 0 {
		return ""
	}

	frames := runtime.CallersFrames(pcs[:n])
	var funcFallback string
	isInnermost := true

	for {
		frame, more := frames.Next()

		if !isSkippableModuleNameFrame(frame.Function) {
			if fset, astFile, ok := parseModuleFile(frame.File); ok {
				varName, funcName := moduleNameCandidatesAtLine(fset, astFile, frame.Line, isInnermost)
				if varName != "" {
					return astFile.Name.Name + "." + varName
				}
				if funcFallback == "" && funcName != "" {
					funcFallback = astFile.Name.Name + "." + funcName
				}
			}
			isInnermost = false
		}

		if !more {
			break
		}
	}

	return funcFallback
}

func parseModuleFile(path string) (*token.FileSet, *ast.File, bool) {
	if cached, ok := moduleNameFileCache.Load(path); ok {
		p := cached.(*parsedModuleFile)
		return p.fset, p.file, p.ok
	}

	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	p := &parsedModuleFile{fset: fset, file: astFile, ok: err == nil}
	moduleNameFileCache.Store(path, p)

	return p.fset, p.file, p.ok
}

func isSkippableModuleNameFrame(function string) bool {
	return strings.HasPrefix(function, "reflect.") || strings.HasPrefix(function, "runtime.")
}

func moduleNameCandidatesAtLine(fset *token.FileSet, astFile *ast.File, line int, allowFuncLitMatch bool) (varName, funcName string) {
	for _, decl := range astFile.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue
			}
			for _, spec := range d.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, value := range valueSpec.Values {
					if i >= len(valueSpec.Names) {
						continue
					}
					if _, isFuncLit := value.(*ast.FuncLit); isFuncLit && !allowFuncLitMatch {
						continue
					}
					start := fset.Position(value.Pos()).Line
					end := fset.Position(value.End()).Line
					if line >= start && line <= end {
						varName = valueSpec.Names[i].Name
					}
				}
			}
		case *ast.FuncDecl:
			start := fset.Position(d.Pos()).Line
			end := fset.Position(d.End()).Line
			if line >= start && line <= end {
				funcName = d.Name.Name
			}
		}
	}
	return
}
