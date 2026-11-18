package core

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/dangduoc08/ginject/internal/common"
	"github.com/dangduoc08/ginject/internal/trace"
	"github.com/dangduoc08/ginject/internal/wsevent"
)

type entryKey struct{}

type wsGated struct {
	gate  *moduleGate
	index int
}

type wsLive struct {
	item     wsevent.WSEventItem
	plan     *wsHandlerPlan
	catchFns []common.WSCatch
}

type wsGlobalInterceptor struct {
	name      string
	intercept common.WSIntercept
}

func openWSGate(handle any) *wsLive {
	g := handle.(*wsGated)
	live := g.gate.live.Load()
	if live == nil {
		return nil
	}
	return &live.ws[g.index]
}

func (app *App) setWSGates() {
	if app.ws == nil {
		return
	}
	for event, ref := range app.container.gateByEvent {
		app.ws.eventMatcher.SetGate(event, &wsGated{gate: ref.gate, index: ref.index})
	}
}

func (app *App) startEntries() {
	c := app.container
	if len(c.entries) == 0 {
		return
	}
	c.logPending()
	c.mu.Lock()
	failures := c.resolveUnknownsLocked()
	c.broadcastLocked()
	after := time.Since(c.createStart)
	c.mu.Unlock()
	c.logFailures(failures, after, nil, "")
	c.asyncCtx, c.asyncCancel = context.WithCancel(context.Background())
	for _, e := range c.entries {
		go app.runEntry(e)
	}
}

func (app *App) runEntry(e *asyncEntry) {
	c := app.container
	defer c.endEntry(e)
	e.goroutine.Store(goroutineID())
	ctx := context.WithValue(c.asyncCtx, entryKey{}, e)
	for {
		if !c.waitBuildable(ctx, e) {
			return
		}
		switch {
		case e.factory != nil && !e.isCalled:
			e.isCalled = true
			if !app.callEntryFactory(ctx, e, e.factory) {
				return
			}
		case len(e.unitFactories) > 0:
			if !app.callUnitFactory(ctx, e) {
				return
			}
		default:
			app.buildEntry(ctx, e)
			return
		}
	}
}

func (c *container) waitBuildable(ctx context.Context, e *asyncEntry) bool {
	s := e.state
	for {
		c.mu.Lock()
		isStopping := c.isStopping
		status := s.status()
		isBuildable := len(s.deps) == 0 && len(s.unknown) == 0
		if !isBuildable && e.phase != entryIdle {
			e.phase = entryIdle
			c.broadcastLocked()
		}
		changed := c.changed
		c.mu.Unlock()
		if status != modulePending {
			return false
		}
		if isBuildable {
			return true
		}
		if isStopping {
			return false
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return false
		}
	}
}

func (c *container) acquireWire(ctx context.Context, e *asyncEntry) bool {
	c.mu.Lock()
	e.phase = entryRequesting
	c.broadcastLocked()
	for {
		if c.isStopping {
			e.phase = entryIdle
			c.broadcastLocked()
			c.mu.Unlock()
			return false
		}
		if !c.isWiring && c.isFirstRequesterLocked(e) {
			c.isWiring = true
			e.phase = entryWiring
			c.mu.Unlock()
			return true
		}
		changed := c.changed
		c.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
		}
		c.mu.Lock()
	}
}

func (c *container) isFirstRequesterLocked(e *asyncEntry) bool {
	for _, other := range c.entries {
		if other == e {
			return true
		}
		if other.phase == entryStarting || other.phase == entryRequesting {
			return false
		}
	}
	return true
}

func (c *container) releaseWire(e *asyncEntry) {
	c.mu.Lock()
	c.isWiring = false
	e.phase = entryIdle
	c.broadcastLocked()
	c.mu.Unlock()
}

func (c *container) endEntry(e *asyncEntry) {
	c.mu.Lock()
	e.phase = entryIdle
	c.broadcastLocked()
	c.mu.Unlock()
	close(e.done)
}

func (c *container) isStoppingNow() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isStopping
}

func (app *App) callEntryFactory(ctx context.Context, e *asyncEntry, fn any) bool {
	c := app.container
	args, err := c.entryFactoryArgs(ctx, e, fn)
	if args == nil && err == nil {
		return false
	}
	if err != nil {
		c.fail(e.state, err.Error(), "")
		return false
	}

	var stack string
	r := c.runFactory(fn, args, func(_ any, s string) { stack = s })
	if ctx.Err() != nil || c.isStoppingNow() {
		return false
	}
	switch {
	case r.isPanicked:
		c.fail(e.state, fmt.Sprint(r.panicValue), stack)
		return false
	case r.err != nil:
		c.fail(e.state, r.err.Error(), "")
		return false
	case r.module == nil:
		c.fail(e.state, "module factory '"+factoryLabel(fn)+"' returned a nil module", "")
		return false
	}
	return app.adoptUnit(ctx, e, fn, r.module)
}

func (c *container) entryFactoryArgs(ctx context.Context, e *asyncEntry, fn any) ([]reflect.Value, error) {
	t := reflect.TypeOf(fn)
	if t.NumIn() == 0 || t.NumIn() == 1 && t.In(0) == contextType {
		return c.lateFactoryArgs(ctx, e, fn)
	}
	if !c.acquireWire(ctx, e) {
		return nil, nil
	}
	defer c.releaseWire(e)
	return c.lateFactoryArgs(ctx, e, fn)
}

func (c *container) lateFactoryArgs(ctx context.Context, e *asyncEntry, fn any) (args []reflect.Value, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("%v", rec)
		}
	}()
	importer := e.parent
	if e.factory == nil || !sameImport(fn, e.factory) {
		importer = e.importers[factoryPointer(fn)]
	}
	t := reflect.TypeOf(fn)
	args = make([]reflect.Value, 0, t.NumIn())
	for i := 0; i < t.NumIn(); i++ {
		if t.In(i) == contextType {
			args = append(args, reflect.ValueOf(ctx))
			continue
		}
		key := genFieldKey(t.In(i))
		if owner := c.siblingDeclarer(&factoryNode{fn: fn, parent: importer}, key); owner != nil {
			if s := c.stateOf(owner); s != nil && (s == e.state || s.status() == moduleReady) {
				c.initModule(owner)
				args = append(args, reflect.ValueOf(c.provide(key)))
				continue
			}
		}
		if p, ok := c.global(key); ok {
			args = append(args, reflect.ValueOf(p))
			continue
		}
		if v, ok := c.interfaceFor(key); ok {
			args = append(args, reflect.ValueOf(v))
			continue
		}
		return nil, errors.New("can't resolve " + t.In(i).String())
	}
	return args, nil
}

func (app *App) callUnitFactory(ctx context.Context, e *asyncEntry) bool {
	c := app.container
	fn := e.unitFactories[0]
	c.mu.Lock()
	c.addParamNeeds(e.state, fn)
	e.state.deps = slices.DeleteFunc(e.state.deps, func(d *moduleState) bool { return d.status() == moduleReady })
	unknown := slices.Clone(e.state.unknown)
	e.state.unknown = nil
	isWaiting := len(e.state.deps) > 0
	c.mu.Unlock()
	if len(unknown) > 0 {
		c.fail(e.state, "can't resolve "+unknown[0].String(), "")
		return false
	}
	if isWaiting {
		return true
	}
	e.unitFactories = e.unitFactories[1:]
	return app.callEntryFactory(ctx, e, fn)
}

func (app *App) adoptUnit(ctx context.Context, e *asyncEntry, fn any, m *Module) bool {
	c := app.container
	if !c.acquireWire(ctx, e) {
		return false
	}
	defer c.releaseWire(e)

	modules, factories := c.newModulesOf(m, e.importers)
	for _, nm := range modules {
		if len(nm.controllers) > 0 {
			c.fail(e.state, "module '"+nm.Name+"' declares controllers ("+moduleControllerTypes(nm)+") but comes from an async factory: put them in a static module that imports it", "")
			return false
		}
	}

	c.mu.Lock()
	c.moduleByFunc[factoryPointer(fn)] = m
	if e.module == nil {
		e.module = m
		if m.Name != "" {
			e.state.label = m.Name
		}
	}
	for _, nm := range modules {
		c.stateByModule[nm] = e.state
		c.unitByModule[nm] = e
		e.state.modules = append(e.state.modules, nm)
		c.modules = append(c.modules, nm)
		for _, p := range nm.providers {
			key := genProviderKey(p)
			if _, ok := c.declaredByKey[key]; !ok {
				c.declaredByKey[key] = p
				c.ownerByKey[key] = nm
			}
		}
		if nm.IsGlobal {
			c.addGlobals(nm)
		}
	}
	e.unitFactories = append(e.unitFactories, factories...)
	c.refreshVisibility()
	for _, nm := range modules {
		for _, p := range nm.providers {
			c.addFieldNeeds(e.state, reflect.TypeOf(p))
		}
	}
	e.state.deps = slices.DeleteFunc(e.state.deps, func(d *moduleState) bool { return d.status() == moduleReady })
	var failures []stateFailure
	if len(e.unitFactories) == 0 {
		failures = c.closeDiscoveryLocked(e)
	} else {
		failures = c.resolveUnknownsLocked()
	}
	c.broadcastLocked()
	after := time.Since(c.createStart)
	c.mu.Unlock()
	c.logFailures(failures, after, nil, "")

	app.callOnReadyOf(app.buildDependents())
	return e.state.status() == modulePending
}

func moduleControllerTypes(m *Module) string {
	names := make([]string, 0, len(m.controllers))
	for _, controller := range m.controllers {
		names = append(names, fmt.Sprint(reflect.TypeOf(controller)))
	}
	return strings.Join(names, ", ")
}

func (c *container) newModulesOf(m *Module, importers map[uintptr]*Module) ([]*Module, []any) {
	var modules []*Module
	var factories []any
	seen := make(map[*Module]struct{})
	var walk func(m *Module)
	walk = func(m *Module) {
		if _, ok := seen[m]; ok {
			return
		}
		seen[m] = struct{}{}
		if _, known := c.stateByModule[m]; known {
			return
		}
		for _, list := range [2][]any{m.imports, m.asyncImports} {
			for _, imported := range list {
				if child := c.moduleOfImport(imported); child != nil {
					walk(child)
				} else if !slices.ContainsFunc(factories, func(fn any) bool { return sameImport(fn, imported) }) {
					factories = append(factories, imported)
					if _, ok := importers[factoryPointer(imported)]; !ok {
						importers[factoryPointer(imported)] = m
					}
				}
			}
		}
		modules = append(modules, m)
	}
	walk(m)
	return modules, factories
}

func (c *container) refreshVisibility() {
	visibleByModule := make(map[*Module]map[string]struct{}, len(c.modules))
	var walk func(m *Module) map[string]struct{}
	walk = func(m *Module) map[string]struct{} {
		if visible, ok := visibleByModule[m]; ok {
			return visible
		}
		visible := make(map[string]struct{}, len(m.providers))
		visibleByModule[m] = visible
		imports := c.resolvedImports(m)
		c.importsByModule[m] = imports
		for _, child := range imports {
			for key := range walk(child) {
				visible[key] = struct{}{}
			}
		}
		for _, p := range m.providers {
			visible[genProviderKey(p)] = struct{}{}
		}
		return visible
	}
	for _, m := range c.modules {
		c.visibleByModule[m] = walk(m)
	}
}

func (app *App) buildEntry(ctx context.Context, e *asyncEntry) {
	c := app.container
	if !c.acquireWire(ctx, e) {
		return
	}
	defer c.releaseWire(e)
	opened, ok := app.buildState(e.state)
	if !ok {
		return
	}
	if c.afterGateOpen != nil {
		c.afterGateOpen()
	}
	if !c.markReady(e.state, opened) {
		return
	}
	app.callOnReadyOf([]*moduleState{e.state})
	app.callOnReadyOf(app.buildDependents())
}

func (app *App) buildDependents() []*moduleState {
	var built []*moduleState
	for {
		s := app.container.claimBuildable()
		if s == nil {
			return built
		}
		if opened, ok := app.buildState(s); ok && app.container.markReady(s, opened) {
			built = append(built, s)
		}
	}
}

func (c *container) claimBuildable() *moduleState {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isStopping {
		return nil
	}
	for _, s := range c.states {
		if s.entry == nil && !s.isClaimed && s.status() == modulePending && len(s.deps) == 0 && len(s.unknown) == 0 {
			s.isClaimed = true
			return s
		}
	}
	return nil
}

func (app *App) callOnReadyOf(states []*moduleState) {
	c := app.container
	for _, s := range states {
		c.mu.RLock()
		modules := slices.Clone(s.modules)
		c.mu.RUnlock()
		for _, m := range modules {
			if m.OnReady != nil && !c.isStoppingNow() {
				app.callBackgroundOnReady(m)
			}
		}
	}
}

func (app *App) callBackgroundOnReady(m *Module) {
	defer func() {
		if rec := recover(); rec != nil {
			app.logger.Error("OnReadyPanic", "module", m.Name, "panic", rec, "stack", panicStack())
		}
	}()
	m.OnReady()
}

func (app *App) buildState(s *moduleState) (opened []openedGate, ok bool) {
	c := app.container
	defer func() {
		if rec := recover(); rec != nil {
			c.fail(s, fmt.Sprint(rec), panicStack())
			opened, ok = nil, false
		}
	}()

	c.mu.RLock()
	modules := slices.Clone(s.modules)
	c.mu.RUnlock()
	for _, m := range modules {
		if c.isStoppingNow() {
			return nil, false
		}
		c.initModule(m)
		for _, p := range m.providers {
			c.provide(genProviderKey(p))
		}
	}

	opened = make([]openedGate, 0, len(modules))
	for _, m := range modules {
		gate := c.gateByModule[m]
		if gate == nil {
			continue
		}
		live, err := app.buildLive(gate)
		if err != nil {
			c.fail(s, err.Error(), "")
			return nil, false
		}
		opened = append(opened, openedGate{gate: gate, live: live})
	}
	return opened, true
}

func (app *App) buildLive(gate *moduleGate) (*moduleLive, error) {
	c := app.container
	late := newControllerBindings()
	saved := c.out
	c.out = &late
	c.isLateBinding = true
	defer func() {
		c.out = saved
		c.isLateBinding = false
	}()

	for _, key := range gate.controllers {
		httpStart, wsStart := len(late.httpMainHandlers), len(late.wsMainHandlers)
		c.injectController(c.controllerByKey[key])
		routes := make([]string, 0, len(late.httpMainHandlers)-httpStart+len(late.wsMainHandlers)-wsStart)
		for _, h := range late.httpMainHandlers[httpStart:] {
			routes = append(routes, h.Pattern)
		}
		for _, h := range late.wsMainHandlers[wsStart:] {
			routes = append(routes, "ws:"+h.EventName)
		}
		slices.Sort(routes)
		if !slices.Equal(routes, gate.routesByControl[key]) {
			return nil, fmt.Errorf("routes of '%v' changed between Create and construction", reflect.TypeOf(c.controllerByKey[key]))
		}
	}

	return &moduleLive{http: app.buildLiveRoutes(gate, &late), ws: app.buildLiveEvents(gate, &late)}, nil
}

func (app *App) buildLiveRoutes(gate *moduleGate, late *controllerBindings) []*httpRoute {
	scratch := newHTTP()
	for _, h := range late.httpMainHandlers {
		scratch.addRoute(h, late.httpPlanByPattern[h.Pattern])
	}
	for _, m := range late.httpMiddlewares {
		scratch.addMiddleware(m)
	}
	for i := len(late.httpExceptionFilters) - 1; i >= 0; i-- {
		scratch.addFilter(late.httpExceptionFilters[i])
	}
	for _, route := range scratch.routes {
		route.filters = append(route.filters, app.http.globalFilters...)
		route.guards = append(route.guards, app.http.globalGuards...)
		route.interceptors = append(route.interceptors, app.http.globalInterceptors...)
	}
	for _, g := range late.httpGuards {
		scratch.addGuard(g)
	}
	for _, i := range late.httpInterceptors {
		scratch.addInterceptor(i)
	}

	routes := make([]*httpRoute, len(gate.patterns))
	for _, h := range late.httpMainHandlers {
		index := gate.indexByPattern[h.Pattern]
		route := scratch.routeOf(h)
		if route.filters == nil {
			route.filters = []httpFilter{}
		}
		route.template = gate.templates[index]
		routes[index] = route
	}
	return routes
}

func (app *App) buildLiveEvents(gate *moduleGate, late *controllerBindings) []wsLive {
	if app.ws == nil || len(gate.events) == 0 {
		return nil
	}
	events := make([]wsLive, len(gate.events))
	for _, h := range late.wsMainHandlers {
		item := wsevent.WSEventItem{Handler: h.Handler}
		item.Middlewares = append(item.Middlewares, app.wsGlobalGuards...)
		for _, g := range late.wsGuards {
			if g.EventName == h.EventName {
				item.Middlewares = append(item.Middlewares, traceWSHandler(&app.tracer, trace.StageGuard, g.Name, common.BuildWSGuardMiddleware(g.Handler.(common.WSCanActivate))))
			}
		}
		for _, gi := range app.wsGlobalInterceptors {
			item.Middlewares = append(item.Middlewares, traceWSHandler(&app.tracer, trace.StagePreInterceptor, gi.name, common.BuildWSInterceptMiddleware(gi.name, gi.intercept)))
		}
		for _, mi := range late.wsInterceptors {
			if mi.EventName == h.EventName {
				item.Middlewares = append(item.Middlewares, traceWSHandler(&app.tracer, trace.StagePreInterceptor, mi.Name, common.BuildWSInterceptMiddleware(mi.Name, mi.Handler.(common.WSIntercept))))
			}
		}
		var catchFns []common.WSCatch
		for i := len(late.wsExceptionFilters) - 1; i >= 0; i-- {
			if ef := late.wsExceptionFilters[i]; ef.EventName == h.EventName {
				catchFns = append(catchFns, traceWSCatch(&app.tracer, ef.Name, ef.Handler.(common.WSCatch)))
			}
		}
		catchFns = append(catchFns, app.wsGlobalCatches...)
		events[gate.indexByEvent[h.EventName]] = wsLive{item: item, plan: late.wsPlanByEvent[h.EventName], catchFns: catchFns}
	}
	return events
}

func (app *App) beginAsyncShutdown() {
	c := app.container
	c.mu.Lock()
	c.isStopping = true
	c.broadcastLocked()
	c.mu.Unlock()
	if c.asyncCancel != nil {
		c.asyncCancel()
	}
}

func (app *App) waitEntries(deadline time.Time) {
	c := app.container
	self := goroutineID()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	isTimedOut := false
	for _, e := range c.entries {
		if e.goroutine.Load() == self {
			continue
		}
		if !isTimedOut {
			select {
			case <-e.done:
				continue
			case <-timer.C:
				isTimedOut = true
			}
		}
		select {
		case <-e.done:
		default:
			c.mu.RLock()
			label := e.state.label
			c.mu.RUnlock()
			app.logger.Warn("ModuleShutdownTimeout", "module", label)
		}
	}
}

func (c *container) isEntryGoroutine(id uint64) bool {
	for _, e := range c.entries {
		if e.goroutine.Load() == id {
			return true
		}
	}
	return false
}
