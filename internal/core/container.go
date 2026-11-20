package core

import (
	"errors"
	"go/token"
	"reflect"
	"strings"

	"go.owlkei.com/owlkei/internal/color"
	"go.owlkei.com/owlkei/internal/common"
	"go.owlkei.com/owlkei/internal/render"
	"go.owlkei.com/owlkei/internal/routing"
	"go.owlkei.com/owlkei/internal/str"
	"go.owlkei.com/owlkei/internal/versioning"
)

var fieldNameByRole = map[string]string{
	"http":            "HTTP",
	"guard":           "Guard",
	"interceptor":     "Interceptor",
	"exceptionFilter": "ExceptionFilter",
	"ws":              "WS",
	"middleware":      "Middleware",
}

type container struct {
	moduleByFunc       map[uintptr]*Module
	scannedModules     map[*Module]struct{}
	initializedModules map[*Module]struct{}
	importsByModule    map[*Module][]*Module
	visibleByModule    map[*Module]map[string]struct{}
	modules            []*Module

	declaredByKey    map[string]Provider
	ownerByKey       map[string]*Module
	globalByKey      map[string]Provider
	globalOwnerByKey map[string]*Module
	providerByKey    map[string]Provider
	interfaceByKey   map[string]any

	controllerKeysByModule map[*Module][]string
	controllerByKey        map[string]Controller
	ownerByController      map[string]*Module
	prefixesByController   map[string][]string
	styleByController      map[string]common.PathStyle

	layersByKey map[layerKey][]*layerEntry

	controllerBindings
	out *controllerBindings

	render              *render.Engine
	isVersioningEnabled bool

	moduleLifecycle
}

type controllerBindings struct {
	funcByRouteShape map[string]string
	funcByEvent      map[string]string

	httpExceptionFilters []common.HTTPLayer
	httpMiddlewares      []common.HTTPLayer
	httpGuards           []common.HTTPLayer
	httpInterceptors     []common.HTTPLayer
	httpMainHandlers     []common.HTTPLayer
	wsGuards             []common.WSLayer
	wsInterceptors       []common.WSLayer
	wsExceptionFilters   []common.WSLayer
	wsMainHandlers       []common.WSLayer
	httpPlanByPattern    map[string]*httpHandlerPlan
	wsPlanByEvent        map[string]*wsHandlerPlan
}

func newControllerBindings() controllerBindings {
	return controllerBindings{
		funcByRouteShape:  make(map[string]string),
		funcByEvent:       make(map[string]string),
		httpPlanByPattern: make(map[string]*httpHandlerPlan),
		wsPlanByEvent:     make(map[string]*wsHandlerPlan),
	}
}

type layerKey struct {
	typ         reflect.Type
	constructor string
}

type layerEntry struct {
	configuration any
	value         any
}

func newContainer() *container {
	c := &container{
		moduleByFunc:           make(map[uintptr]*Module),
		scannedModules:         make(map[*Module]struct{}),
		initializedModules:     make(map[*Module]struct{}),
		globalOwnerByKey:       make(map[string]*Module),
		importsByModule:        make(map[*Module][]*Module),
		visibleByModule:        make(map[*Module]map[string]struct{}),
		declaredByKey:          make(map[string]Provider),
		ownerByKey:             make(map[string]*Module),
		globalByKey:            make(map[string]Provider),
		providerByKey:          make(map[string]Provider),
		interfaceByKey:         make(map[string]any),
		controllerKeysByModule: make(map[*Module][]string),
		controllerByKey:        make(map[string]Controller),
		ownerByController:      make(map[string]*Module),
		prefixesByController:   make(map[string][]string),
		styleByController:      make(map[string]common.PathStyle),
		layersByKey:            make(map[layerKey][]*layerEntry),
		controllerBindings:     newControllerBindings(),
		moduleLifecycle:        newModuleLifecycle(),
	}
	c.out = &c.controllerBindings
	return c
}

func (c *container) addGlobal(p Provider) string {
	key := genProviderKey(p)
	if _, ok := c.globalByKey[key]; !ok {
		c.globalByKey[key] = p
	}
	return key
}

func (c *container) addInterface(key string, v any) {
	c.interfaceByKey[key] = v
}

func (c *container) interfaceFor(key string) (any, bool) {
	v, ok := c.interfaceByKey[key]
	return v, ok
}

func (c *container) global(key string) (Provider, bool) {
	if _, ok := c.globalByKey[key]; !ok {
		return nil, false
	}
	return c.provide(key), true
}

func (c *container) wire(root *Module) {
	c.addGlobals(root)
	c.scanGlobals(root)
	c.wireFactories(root)
	c.collectEntries(root)
	c.rediscover(root)
	c.computeStates(root)

	for _, m := range c.modules {
		if !c.isReadyAtCreate(m) {
			continue
		}
		c.initModule(m)
		for _, p := range m.providers {
			c.provide(genProviderKey(p))
		}
	}

	for _, key := range c.controllerKeysByModule[root] {
		if gate := c.gateOfController(key); gate != nil {
			c.bindProbe(key, gate)
			continue
		}
		c.injectController(c.controllerByKey[key])
	}
}

func (c *container) rediscover(root *Module) {
	c.importsByModule = make(map[*Module][]*Module)
	c.visibleByModule = make(map[*Module]map[string]struct{})
	c.declaredByKey = make(map[string]Provider)
	c.ownerByKey = make(map[string]*Module)
	c.controllerKeysByModule = make(map[*Module][]string)
	c.controllerByKey = make(map[string]Controller)
	c.ownerByController = make(map[string]*Module)
	c.prefixesByController = make(map[string][]string)
	c.styleByController = make(map[string]common.PathStyle)
	c.modules = nil
	c.discover(root)
}

func (c *container) addGlobals(m *Module) {
	for _, p := range m.providers {
		key := c.addGlobal(p)
		if _, ok := c.globalOwnerByKey[key]; !ok {
			c.globalOwnerByKey[key] = m
		}
	}
}

func (c *container) scanGlobals(m *Module) {
	if _, ok := c.scannedModules[m]; ok {
		return
	}
	c.scannedModules[m] = struct{}{}
	if m.IsGlobal {
		c.addGlobals(m)
	}
	for _, list := range [2][]any{m.imports, m.asyncImports} {
		for _, imported := range list {
			if staticModule, ok := imported.(*Module); ok {
				c.scanGlobals(staticModule)
			}
		}
	}
}

func (c *container) initModule(m *Module) {
	if _, ok := c.initializedModules[m]; ok {
		return
	}
	c.initializedModules[m] = struct{}{}
	if m.OnInit != nil {
		m.OnInit()
	}
}

func (c *container) discover(m *Module) {
	if _, ok := c.importsByModule[m]; ok {
		return
	}
	c.importsByModule[m] = nil

	imports := c.resolvedImports(m)
	c.importsByModule[m] = imports

	visible := make(map[string]struct{}, len(m.providers))
	var controllerKeys []string
	seenControllers := make(map[string]struct{}, len(m.controllers))
	for _, imported := range imports {
		c.discover(imported)
		for key := range c.visibleByModule[imported] {
			visible[key] = struct{}{}
		}
		controllerKeys = appendControllerKeys(controllerKeys, seenControllers, c.controllerKeysByModule[imported])
	}

	for _, p := range m.providers {
		key := genProviderKey(p)
		visible[key] = struct{}{}
		if _, ok := c.declaredByKey[key]; !ok {
			c.declaredByKey[key] = p
			c.ownerByKey[key] = m
		}
	}

	ownKeys := make([]string, 0, len(m.controllers))
	for _, controller := range m.controllers {
		key := genFieldKey(reflect.TypeOf(controller))
		if _, ok := c.controllerByKey[key]; !ok {
			c.controllerByKey[key] = controller
			c.ownerByController[key] = m
		}
		ownKeys = append(ownKeys, key)
	}
	controllerKeys = appendControllerKeys(controllerKeys, seenControllers, ownKeys)

	for _, key := range controllerKeys {
		c.prefixesByController[key] = append(c.prefixesByController[key], m.prefixes...)
		if _, ok := c.styleByController[key]; !ok && m.pathStyle != 0 {
			c.styleByController[key] = m.pathStyle
		}
	}

	c.visibleByModule[m] = visible
	c.controllerKeysByModule[m] = controllerKeys
	c.modules = append(c.modules, m)
}

func (c *container) resolvedImports(m *Module) []*Module {
	imports := make([]*Module, 0, len(m.imports)+len(m.asyncImports))
	for _, list := range [2][]any{m.imports, m.asyncImports} {
		for _, imported := range list {
			if staticModule, ok := imported.(*Module); ok {
				imports = append(imports, staticModule)
			} else if called, ok := c.moduleByFunc[factoryPointer(imported)]; ok && called != nil {
				imports = append(imports, called)
			}
		}
	}
	return imports
}

func appendControllerKeys(keys []string, seen map[string]struct{}, add []string) []string {
	for _, key := range add {
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	return keys
}

func (c *container) provide(key string) Provider {
	if p, ok := c.providerByKey[key]; ok {
		return p
	}

	declared, ok := c.declaredByKey[key]
	if !ok {
		declared = c.globalByKey[key]
	}
	visible := c.visibleByModule[c.ownerByKey[key]]

	declaredType := reflect.TypeOf(declared)
	injected := make(map[string]Provider, declaredType.NumField())
	for i := 0; i < declaredType.NumField(); i++ {
		fieldKey := genFieldKey(declaredType.Field(i).Type)
		if _, ok := visible[fieldKey]; ok {
			injected[fieldKey] = c.provide(fieldKey)
		}
	}

	newProvider, err := injectDependencies(declared, "provider", injected, c)
	if err != nil {
		panic(err)
	}

	p := newProvider.Interface().(Provider).NewProvider()
	c.mu.Lock()
	c.providerByKey[key] = p
	c.mu.Unlock()
	return p
}

func (c *container) construct(obj any, constructor string) any {
	value := reflect.ValueOf(obj)
	key := layerKey{typ: value.Type(), constructor: constructor}

	for _, entry := range c.layersByKey[key] {
		if reflect.DeepEqual(entry.configuration, obj) {
			return entry.value
		}
	}

	entry := &layerEntry{configuration: snapshotConfiguration(obj, value), value: obj}
	if objConstructor := value.MethodByName(constructor); objConstructor.IsValid() {
		entry.value = objConstructor.Call(nil)[0].Interface()
	}
	c.layersByKey[key] = append(c.layersByKey[key], entry)

	return entry.value
}

func snapshotConfiguration(obj any, value reflect.Value) any {
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return obj
	}
	snapshot := reflect.New(value.Elem().Type())
	snapshot.Elem().Set(value.Elem())
	return snapshot.Interface()
}

func (c *container) injectController(controller Controller) {
	newController, err := injectDependencies(controller, "controller", c.providerByKey, c)
	if err != nil {
		panic(err)
	}

	controller = newController.Interface().(Controller).NewController()

	c.bindHTTPController(controller)
	c.bindWSController(controller)
}

func (c *container) addRoute(pattern, fnName string) {
	shape, _ := routing.ParseToParamKey(pattern)
	if existing, ok := c.out.funcByRouteShape[shape]; ok {
		panic(errors.New(
			color.FmtRed(
				"route conflict: %v method is conflicted with %v method",
				fnName,
				existing,
			),
		))
	}
	c.out.funcByRouteShape[shape] = fnName
}

func (c *container) addEvent(eventName, fnName string) {
	if existing, ok := c.out.funcByEvent[eventName]; ok {
		panic(errors.New(
			color.FmtRed(
				"event conflict: %v event is conflicted with %v event",
				fnName,
				existing,
			),
		))
	}
	c.out.funcByEvent[eventName] = fnName
}

func (c *container) bindHTTPController(controller Controller) {
	http, templateByPattern, ok := c.bindHTTPRoutes(controller)
	if !ok {
		return
	}

	controllerType := reflect.TypeOf(controller)
	controllerValue := reflect.ValueOf(controller)
	controllerPath := controllerType.PkgPath()
	c.bindHTTPExceptionFilters(controller, controllerType, controllerPath, &http)
	c.bindHTTPMiddlewares(controller, controllerType, controllerPath, &http)
	c.bindHTTPGuards(controller, controllerType, controllerPath, &http)
	c.bindHTTPInterceptors(controller, controllerType, controllerPath, &http)

	planByName := make(map[string]*httpHandlerPlan, len(common.HTTPRouterMap(&http)))
	for pattern, handler := range common.HTTPRouterMap(&http) {
		name := common.HTTPPatternToFuncNameMap(&http)[pattern]
		plan, ok := planByName[name]
		if !ok {
			method, _ := controllerType.MethodByName(name)
			plan = newHTTPHandlerPlan(controllerValue, method, c.providerByKey, c)
			planByName[name] = plan
		}
		c.out.httpPlanByPattern[pattern] = plan
		c.out.httpMainHandlers = append(c.out.httpMainHandlers, httpMainHandlerOf(controllerPath, pattern, name, handler, templateByPattern[pattern]))
	}
}

func (c *container) bindHTTPRoutes(controller Controller) (common.HTTP, map[string]string, bool) {
	controllerType := reflect.TypeOf(controller)
	if _, isHTTP := controllerType.FieldByName(fieldNameByRole["http"]); !isHTTP {
		return common.HTTP{}, nil, false
	}

	controllerValue := reflect.ValueOf(controller)
	http := controllerValue.FieldByName(fieldNameByRole["http"]).Interface().(common.HTTP)
	pageVersion := ""
	if c.isVersioningEnabled {
		pageVersion = versioning.NeutralVersion
	}
	controllerKey := genFieldKey(controllerType)
	modulePrefixes := c.prefixesByController[controllerKey]
	style, ok := c.styleByController[controllerKey]
	if !ok {
		style = common.PathStyleKebab
	}

	templateByPattern := map[string]string{}
	for j := 0; j < controllerType.NumMethod(); j++ {
		methodName := controllerType.Method(j).Name

		handler := controllerValue.Method(j).Interface()
		common.AddHandlerToRouterMapWithPageVersion(&http, modulePrefixes, style, methodName, handler, pageVersion)
		pattern, ok := common.HTTPFuncNameToPatternMap(&http)[methodName]
		if !ok {
			continue
		}
		isPage := common.PageOperation(methodName) != ""
		if isPage && !c.isLateBinding {
			c.checkPageRoute(controllerType, methodName, pattern)
		}
		c.addRoute(pattern, methodName)
		if isPage && !c.isLateBinding {
			templateByPattern[pattern] = c.bindTemplate(controllerType.Name()+"."+methodName, pattern)
		}
	}
	common.CheckExactPaths(&http)

	return http, templateByPattern, true
}

func httpMainHandlerOf(controllerPath, pattern, name string, handler any, template string) common.HTTPLayer {
	method, route, version := routing.PatternToMethodRouteVersion(pattern)
	return common.HTTPLayer{
		ControllerPath:  controllerPath,
		Method:          method,
		Route:           str.Enclose(route, '/'),
		Version:         version,
		Handler:         handler,
		Name:            name,
		MainHandlerName: name,
		Pattern:         pattern,
		Template:        template,
		IsPage:          common.PageOperation(name) != "",
	}
}

func (c *container) checkPageRoute(controllerType reflect.Type, methodName, pattern string) {
	handlerName := controllerType.Name() + "." + methodName
	if common.PageOperation(methodName) == common.PageSubmit {
		renderName := common.RenderNameOf(methodName)
		if _, ok := controllerType.MethodByName(renderName); !ok {
			panic(errors.New(color.FmtRed("invalid handler: %v needs %v.%v: a SUBMIT_ posts to the page that shows its form", handlerName, controllerType.Name(), renderName)))
		}
	}
	if c.render == nil {
		panic(errors.New(color.FmtRed("invalid render: %v is a page route, set Render.FS in AppOptions", handlerName)))
	}
	if _, route, _ := routing.PatternToMethodRouteVersion(pattern); strings.ContainsRune(route, '*') {
		panic(errors.New(color.FmtRed("invalid route: %v is a page route and its path %v holds a wildcard", handlerName, route)))
	}
}

func (c *container) bindTemplate(handlerName, pattern string) string {
	_, route, _ := routing.PatternToMethodRouteVersion(pattern)
	name, err := c.render.Bind(route)
	if err != nil {
		panic(errors.New(color.FmtRed("invalid render: %v %v", handlerName, err)))
	}
	return name
}

func (c *container) bindHTTPExceptionFilters(controller Controller, controllerType reflect.Type, controllerPath string, http *common.HTTP) {
	if _, ok := controllerType.FieldByName(fieldNameByRole["exceptionFilter"]); !ok {
		return
	}

	exceptionFilter := reflect.ValueOf(controller).FieldByName(fieldNameByRole["exceptionFilter"]).Interface().(common.ExceptionFilter)
	items := common.InjectProvidersIntoHTTPExceptionFilters(&exceptionFilter, http, buildFieldInjectionCallback("exceptionFilter", c.providerByKey, c), c.construct)

	for _, item := range items {
		c.out.httpExceptionFilters = append(c.out.httpExceptionFilters, httpLayerOf(controllerPath, item.HTTP.Method, item.HTTP.Route, item.HTTP.Version, item.HTTP.Pattern, item.HTTP.Common))
	}
}

func (c *container) bindHTTPMiddlewares(controller Controller, controllerType reflect.Type, controllerPath string, http *common.HTTP) {
	if _, ok := controllerType.FieldByName(fieldNameByRole["middleware"]); !ok {
		return
	}

	middleware := reflect.ValueOf(controller).FieldByName(fieldNameByRole["middleware"]).Interface().(common.Middleware)
	items := common.InjectProvidersIntoHTTPMiddlewares(&middleware, http, buildFieldInjectionCallback("middleware function", c.providerByKey, c), c.construct)

	for _, item := range items {
		c.out.httpMiddlewares = append(c.out.httpMiddlewares, httpLayerOf(controllerPath, item.HTTP.Method, item.HTTP.Route, item.HTTP.Version, item.HTTP.Pattern, item.HTTP.Common))
	}
}

func (c *container) bindHTTPGuards(controller Controller, controllerType reflect.Type, controllerPath string, http *common.HTTP) {
	if _, ok := controllerType.FieldByName(fieldNameByRole["guard"]); !ok {
		return
	}

	guard := reflect.ValueOf(controller).FieldByName(fieldNameByRole["guard"]).Interface().(common.Guard)
	items := common.InjectProvidersIntoHTTPGuards(&guard, http, buildFieldInjectionCallback("guarder", c.providerByKey, c), c.construct)

	for _, item := range items {
		c.out.httpGuards = append(c.out.httpGuards, httpLayerOf(controllerPath, item.HTTP.Method, item.HTTP.Route, item.HTTP.Version, item.HTTP.Pattern, item.HTTP.Common))
	}
}

func (c *container) bindHTTPInterceptors(controller Controller, controllerType reflect.Type, controllerPath string, http *common.HTTP) {
	if _, ok := controllerType.FieldByName(fieldNameByRole["interceptor"]); !ok {
		return
	}

	interceptor := reflect.ValueOf(controller).FieldByName(fieldNameByRole["interceptor"]).Interface().(common.Interceptor)
	items := common.InjectProvidersIntoHTTPInterceptors(&interceptor, http, buildFieldInjectionCallback("interceptor", c.providerByKey, c), c.construct)

	for _, item := range items {
		c.out.httpInterceptors = append(c.out.httpInterceptors, httpLayerOf(controllerPath, item.HTTP.Method, item.HTTP.Route, item.HTTP.Version, item.HTTP.Pattern, item.HTTP.Common))
	}
}

func httpLayerOf(controllerPath, method, route, version, pattern string, item common.CommonItem) common.HTTPLayer {
	return common.HTTPLayer{
		ControllerPath:  controllerPath,
		Method:          method,
		Route:           route,
		Version:         version,
		Handler:         item.Handler,
		Name:            item.Name,
		MainHandlerName: item.MainHandlerName,
		Pattern:         pattern,
	}
}

func (c *container) bindWSController(controller Controller) {
	ws, methodByEvent, ok := c.bindWSEvents(controller)
	if !ok {
		return
	}

	controllerType := reflect.TypeOf(controller)
	c.bindWSGuards(controller, controllerType, &ws)
	c.bindWSInterceptors(controller, controllerType, &ws)
	c.bindWSExceptionFilters(controller, controllerType, &ws)

	controllerValue := reflect.ValueOf(controller)
	for eventName, handler := range common.WSEventMap(&ws) {
		c.out.wsPlanByEvent[eventName] = newWSHandlerPlan(controllerValue, methodByEvent[eventName], c.providerByKey, c)

		c.out.wsMainHandlers = append(c.out.wsMainHandlers, common.WSLayer{
			EventName: eventName,
			Handler:   handler,
		})
	}
}

func (c *container) bindWSEvents(controller Controller) (common.WS, map[string]reflect.Method, bool) {
	controllerType := reflect.TypeOf(controller)
	if _, ok := controllerType.FieldByName(fieldNameByRole["ws"]); !ok {
		return common.WS{}, nil, false
	}

	controllerValue := reflect.ValueOf(controller)
	ws := controllerValue.FieldByName(fieldNameByRole["ws"]).Interface().(common.WS)

	methodByEvent := make(map[string]reflect.Method)
	for j := 0; j < controllerType.NumMethod(); j++ {
		method := controllerType.Method(j)

		handler := controllerValue.Method(j).Interface()
		common.AddHandlerToEventMap(&ws, method.Name, handler)
		if eventName, ok := common.ParseWSFuncNameToEvent(method.Name); ok {
			c.addEvent(eventName, method.Name)
			methodByEvent[eventName] = method
		}
	}

	return ws, methodByEvent, true
}

func (c *container) bindWSGuards(controller Controller, controllerType reflect.Type, ws *common.WS) {
	if _, ok := controllerType.FieldByName(fieldNameByRole["guard"]); !ok {
		return
	}

	guard := reflect.ValueOf(controller).FieldByName(fieldNameByRole["guard"]).Interface().(common.Guard)
	items := common.InjectProvidersIntoWSGuards(&guard, ws, buildFieldInjectionCallback("guarder", c.providerByKey, c), c.construct)

	for _, item := range items {
		c.out.wsGuards = append(c.out.wsGuards, common.WSLayer{
			EventName: item.WS.EventName,
			Handler:   item.WS.Common.Handler,
			Name:      item.WS.Common.Name,
		})
	}
}

func (c *container) bindWSInterceptors(controller Controller, controllerType reflect.Type, ws *common.WS) {
	if _, ok := controllerType.FieldByName(fieldNameByRole["interceptor"]); !ok {
		return
	}

	interceptor := reflect.ValueOf(controller).FieldByName(fieldNameByRole["interceptor"]).Interface().(common.Interceptor)
	items := common.InjectProvidersIntoWSInterceptors(&interceptor, ws, buildFieldInjectionCallback("interceptor", c.providerByKey, c), c.construct)

	for _, item := range items {
		c.out.wsInterceptors = append(c.out.wsInterceptors, common.WSLayer{
			EventName: item.WS.EventName,
			Handler:   item.WS.Common.Handler,
			Name:      item.WS.Common.Name,
		})
	}
}

func (c *container) bindWSExceptionFilters(controller Controller, controllerType reflect.Type, ws *common.WS) {
	if _, ok := controllerType.FieldByName(fieldNameByRole["exceptionFilter"]); !ok {
		return
	}

	exceptionFilter := reflect.ValueOf(controller).FieldByName(fieldNameByRole["exceptionFilter"]).Interface().(common.ExceptionFilter)
	items := common.InjectProvidersIntoWSExceptionFilters(&exceptionFilter, ws, buildFieldInjectionCallback("exceptionFilter", c.providerByKey, c), c.construct)

	for _, item := range items {
		c.out.wsExceptionFilters = append(c.out.wsExceptionFilters, common.WSLayer{
			EventName: item.WS.EventName,
			Handler:   item.WS.Common.Handler,
			Name:      item.WS.Common.Name,
		})
	}
}

func buildFieldInjectionCallback(kind string, injectedProviders map[string]Provider, c *container) func(int, reflect.Type, reflect.Value, reflect.Value) {
	return func(i int, ownerType reflect.Type, ownerValue, newInstance reflect.Value) {
		field := ownerType.Field(i)
		fieldType := field.Type
		fieldName := field.Name
		injectProviderKey := genFieldKey(fieldType)

		if !token.IsExported(fieldName) {
			panic(errors.New(
				color.FmtRed(
					"dependency injection: can't set value to unexported '%v' field of the '%v' %v",
					fieldName,
					ownerType.Name(),
					kind,
				),
			))
		}

		if injectedProviders[injectProviderKey] != nil {
			newInstance.Elem().Field(i).Set(reflect.ValueOf(injectedProviders[injectProviderKey]))
		} else if p, ok := c.global(injectProviderKey); ok {
			newInstance.Elem().Field(i).Set(reflect.ValueOf(p))
		} else if iface, ok := c.interfaceFor(injectProviderKey); ok {
			newInstance.Elem().Field(i).Set(reflect.ValueOf(iface))
		} else if !isInjectedProvider(fieldType) {
			newInstance.Elem().Field(i).Set(ownerValue.Field(i))
		} else {
			panic(errors.New(
				color.FmtRed(
					"dependency injection: can't resolve dependencies of the '%v' provider. Please make sure that the argument dependency at index [%v] is available in the '%v' %v",
					fieldType.String(),
					i,
					ownerType.Name(),
					kind,
				),
			))
		}
	}
}
