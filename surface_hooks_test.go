package ginject_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"reflect"
	"slices"
	"sort"
	"testing"

	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
	"github.com/dangduoc08/ginject/internal/core"
	"github.com/dangduoc08/ginject/internal/test"
)

func exportedMethods(typ reflect.Type) []string {
	var names []string
	for i := 0; i < typ.NumMethod(); i++ {
		names = append(names, typ.Method(i).Name)
	}
	sort.Strings(names)
	return names
}

func exportedFields(typ reflect.Type) []string {
	var names []string
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).IsExported() {
			names = append(names, typ.Field(i).Name)
		}
	}
	sort.Strings(names)
	return names
}

func assertNoMethods(t *testing.T, typ reflect.Type, rule string, forbidden ...string) {
	t.Helper()
	for _, name := range forbidden {
		if _, ok := typ.MethodByName(name); ok {
			t.Error(test.DiffMessage(name, "<absent>", rule+": "+typ.String()+" must not have method "+name))
		}
	}
}

func TestSurface_HTTPContextHasNoHooks(t *testing.T) {
	typ := reflect.TypeFor[ginject.HTTPContext]()
	assertNoMethods(t, typ, "B5", "Init", "Reset", "AddAggregation", "Aggregations", "GetID")
	if _, ok := typ.MethodByName("ID"); !ok {
		t.Error(test.DiffMessage("<missing>", "ID", "B16: HTTPContext has ID()"))
	}
	fields := exportedFields(typ.Elem())
	want := []string{"Request", "ResponseWriter"}
	if !slices.Equal(fields, want) {
		t.Error(test.DiffMessage(fields, want, "B5: the only exported fields of HTTPContext are Request and ResponseWriter"))
	}
}

func TestSurface_WSContextHasNoHooks(t *testing.T) {
	typ := reflect.TypeFor[ginject.WSContext]()
	assertNoMethods(t, typ, "B5", "Init", "Reset", "AddAggregation", "Aggregations", "SetSend", "SetWSPayload", "GetID")
	if _, ok := typ.MethodByName("ID"); !ok {
		t.Error(test.DiffMessage("<missing>", "ID", "B16: WSContext has ID()"))
	}
	fields := exportedFields(typ.Elem())
	want := []string{"Conn"}
	if !slices.Equal(fields, want) {
		t.Error(test.DiffMessage(fields, want, "B5: the only exported field of WSContext is Conn"))
	}
}

func TestSurface_AggregationMembers(t *testing.T) {
	typ := reflect.TypeFor[ginject.Aggregation]()
	got := exportedMethods(typ)
	want := []string{"Filter", "Pipe", "Tap", "Transform"}
	if !slices.Equal(got, want) {
		t.Error(test.DiffMessage(got, want, "B5: Aggregation has exactly Pipe, Transform, Tap, Filter"))
	}
	if fields := exportedFields(typ.Elem()); len(fields) != 0 {
		t.Error(test.DiffMessage(fields, []string(nil), "B5: Aggregation has no exported field"))
	}
}

func TestSurface_EmbeddableMembers(t *testing.T) {
	cases := []struct {
		typ  reflect.Type
		want []string
	}{
		{reflect.TypeFor[*ginject.HTTP](), []string{"Path", "Prefix"}},
		{reflect.TypeFor[*ginject.WS](), nil},
		{reflect.TypeFor[*ginject.Guard](), []string{"BindGuard"}},
		{reflect.TypeFor[*ginject.Interceptor](), []string{"BindInterceptor"}},
		{reflect.TypeFor[*ginject.ExceptionFilter](), []string{"BindExceptionFilter"}},
		{reflect.TypeFor[*ginject.Middleware](), []string{"BindMiddleware"}},
	}
	for _, c := range cases {
		got := exportedMethods(c.typ)
		if !slices.Equal(got, c.want) {
			t.Error(test.DiffMessage(got, c.want, "B6: exported methods of "+c.typ.String()))
		}
		if fields := exportedFields(c.typ.Elem()); len(fields) != 0 {
			t.Error(test.DiffMessage(fields, []string(nil), "B6: exported fields of "+c.typ.String()))
		}
	}
}

func TestSurface_ModuleBuilderIsFunc(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "aliases.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var isFunc, isVar bool
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name == "ModuleBuilder" {
				isFunc = true
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if v, ok := spec.(*ast.ValueSpec); ok {
					for _, n := range v.Names {
						isVar = isVar || n.Name == "ModuleBuilder"
					}
				}
			}
		}
	}
	if !isFunc || isVar {
		t.Error(test.DiffMessage([2]bool{isFunc, isVar}, [2]bool{true, false}, "D3: ginject.ModuleBuilder is a func declaration, not a variable"))
	}
	typ := reflect.TypeOf(ginject.ModuleBuilder)
	if typ.Kind() != reflect.Func || typ.NumIn() != 0 || typ.NumOut() != 1 || typ.Out(0) != reflect.TypeFor[*core.ModuleBuilder]() {
		t.Error(test.DiffMessage(typ.String(), "func() *core.ModuleBuilder", "D3: ModuleBuilder takes nothing and returns *core.ModuleBuilder"))
	}
}

func TestSurface_ModuleBuilderChain(t *testing.T) {
	b := ginject.ModuleBuilder()
	m := b.Imports().Providers(greeter{Word: "x"}).Controllers(greetController{}).Build()
	if m == nil {
		t.Fatal(test.DiffMessage(m, "<module>", "B9: Build returns a module"))
	}
	if again := ginject.ModuleBuilder(); again == b {
		t.Error(test.DiffMessage(again == b, false, "B9: every call returns a new builder"))
	}
}

type pointerCatchFilter struct{}

func (pointerCatchFilter) Catch(c ginject.HTTPContext, e *exception.Exception) {
	c.Status(http.StatusTeapot).JSON(ginject.Map{"caught": true})
}

type pointerCatchController struct {
	ginject.HTTP
	ginject.ExceptionFilter
}

func (instance pointerCatchController) NewController() ginject.Controller {
	instance.BindExceptionFilter(pointerCatchFilter{}, instance.READ_boom)
	return instance
}

func (instance pointerCatchController) READ_boom() string {
	panic(exception.BadRequestException("boom"))
}

func TestSurface_FilterCatchesExceptionPointer(t *testing.T) {
	app := ginject.New()
	app.Create(ginject.ModuleBuilder().Controllers(pointerCatchController{}).Build())
	w := serve(app, http.MethodGet, "/boom", nil, nil)
	if w.Code != http.StatusTeapot {
		t.Error(test.DiffMessage(w.Code, http.StatusTeapot, "duplicate 3: a filter with Catch(ctx, *exception.Exception) catches the panic"))
	}
}

func TestSurface_BindLayersAcceptAnyShape(t *testing.T) {
	for _, name := range []string{"BindGuard", "BindInterceptor", "BindExceptionFilter"} {
		var typ reflect.Type
		switch name {
		case "BindGuard":
			typ = reflect.TypeFor[*ginject.Guard]()
		case "BindInterceptor":
			typ = reflect.TypeFor[*ginject.Interceptor]()
		default:
			typ = reflect.TypeFor[*ginject.ExceptionFilter]()
		}
		m, _ := typ.MethodByName(name)
		if m.Type.In(1) != reflect.TypeFor[any]() {
			t.Error(test.DiffMessage(m.Type.In(1).String(), "interface {}", "D7: "+name+" takes any"))
		}
	}
}
