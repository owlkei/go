package core

import (
	"fmt"
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
	imports     []any
	providers   []Provider
	controllers []Controller
	pathStyle   common.PathStyle
}

func NewModuleBuilder() *ModuleBuilder {
	return &ModuleBuilder{}
}

func (m *ModuleBuilder) Imports(modules ...any) *ModuleBuilder {
	m.imports = append(m.imports, modules...)
	return m
}

func (m *ModuleBuilder) getModuleType() ([]*Module, []any) {
	staticModules := make([]*Module, 0, len(m.imports))
	var moduleFactories []any
	var errors []string

	for _, arg := range m.imports {
		switch module := arg.(type) {
		case *Module:
			staticModules = append(staticModules, module)
		default:
			moduleType := reflect.TypeOf(module)
			if isModuleFactory(moduleType.String()) {
				moduleFactories = append(moduleFactories, module)
			} else {
				errors = append(errors, fmt.Sprintf("can't pass '%v' type as module", moduleType))
			}
		}
	}

	if len(errors) > 0 {
		panic(color.FmtRed("invalid module: %s", strings.Join(errors, "\n       ")))
	}

	return staticModules, moduleFactories
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
