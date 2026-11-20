package core

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.owlkei.com/owlkei/internal/color"
	"go.owlkei.com/owlkei/internal/common"
	"go.owlkei.com/owlkei/internal/devtool"
	"go.owlkei.com/owlkei/log"
)

type moduleStatus uint32

const (
	modulePending moduleStatus = iota
	moduleReady
	moduleFailed
)

func (s moduleStatus) String() string {
	switch s {
	case moduleReady:
		return "ready"
	case moduleFailed:
		return "failed"
	}
	return "pending"
}

const defaultStartupTimeout = 30 * time.Second

type moduleState struct {
	value      atomic.Uint32
	done       chan struct{}
	entry      *asyncEntry
	label      string
	modules    []*Module
	deps       []*moduleState
	unknown    []reflect.Type
	cause      string
	readyAfter time.Duration
	isAsync    bool
	isClaimed  bool
}

func newModuleState(label string) *moduleState {
	return &moduleState{label: label, done: make(chan struct{})}
}

func (s *moduleState) status() moduleStatus {
	return moduleStatus(s.value.Load())
}

type asyncEntry struct {
	c             *container
	state         *moduleState
	parent        *Module
	module        *Module
	factory       any
	unitFactories []any
	importers     map[uintptr]*Module
	isCalled      bool
	isDiscovering bool
	phase         entryPhase
	attempts      atomic.Int64
	lastError     atomic.Pointer[string]
	nextRetry     atomic.Int64
	goroutine     atomic.Uint64
	done          chan struct{}
}

func (e *asyncEntry) label() string {
	e.c.mu.RLock()
	defer e.c.mu.RUnlock()
	return e.state.label
}

type controllerProbe struct {
	controller Controller
	panicValue any
	isPanicked bool
}

type moduleGate struct {
	live            atomic.Pointer[moduleLive]
	state           *moduleState
	retry           []*asyncEntry
	controllers     []string
	patterns        []string
	templates       []string
	events          []string
	indexByPattern  map[string]int
	indexByEvent    map[string]int
	routesByControl map[string][]string
}

type moduleLive struct {
	http []*httpRoute
	ws   []wsLive
}

type openedGate struct {
	gate *moduleGate
	live *moduleLive
}

type gateRef struct {
	gate  *moduleGate
	index int
}

type entryPhase uint8

const (
	entryStarting entryPhase = iota
	entryIdle
	entryRequesting
	entryWiring
)

type moduleLifecycle struct {
	mu       sync.RWMutex
	changed  chan struct{}
	isWiring bool

	logger         log.Logger
	createStart    time.Time
	startupTimeout time.Duration
	createCtx      context.Context
	asyncCtx       context.Context
	asyncCancel    context.CancelFunc
	root           *Module
	isLateBinding  bool
	isStopping     bool

	syncModules       map[*Module]struct{}
	syncOrder         []*Module
	factoryNodes      []*factoryNode
	entries           []*asyncEntry
	entryByImport     map[any]*asyncEntry
	unitByModule      map[*Module]*asyncEntry
	states            []*moduleState
	stateByModule     map[*Module]*moduleState
	stateByFunc       map[uintptr]*moduleState
	probeByController map[string]*controllerProbe
	gateByModule      map[*Module]*moduleGate
	gateByPattern     map[string]gateRef
	gateByEvent       map[string]gateRef
	readyOrder        []*Module
	hasAsyncFactory   bool
	afterGateOpen     func()
}

func newModuleLifecycle() moduleLifecycle {
	return moduleLifecycle{
		changed:           make(chan struct{}),
		startupTimeout:    defaultStartupTimeout,
		syncModules:       make(map[*Module]struct{}),
		entryByImport:     make(map[any]*asyncEntry),
		unitByModule:      make(map[*Module]*asyncEntry),
		stateByModule:     make(map[*Module]*moduleState),
		stateByFunc:       make(map[uintptr]*moduleState),
		probeByController: make(map[string]*controllerProbe),
		gateByModule:      make(map[*Module]*moduleGate),
		gateByPattern:     make(map[string]gateRef),
		gateByEvent:       make(map[string]gateRef),
	}
}

func importKey(entry any) any {
	if m, ok := entry.(*Module); ok {
		return m
	}
	return factoryPointer(entry)
}

func (c *container) collectEntries(root *Module) {
	c.checkImportedBothWays(root)

	for _, m := range c.syncOrder {
		for _, imported := range m.asyncImports {
			key := importKey(imported)
			if _, ok := c.entryByImport[key]; ok {
				continue
			}
			if static, ok := imported.(*Module); ok {
				if e, ok := c.unitByModule[static]; ok {
					c.entryByImport[key] = e
					continue
				}
			}
			c.addEntry(m, imported)
		}
	}
}

func (c *container) addEntry(parent *Module, imported any) {
	e := &asyncEntry{c: c, parent: parent, importers: make(map[uintptr]*Module), done: make(chan struct{})}
	c.entryByImport[importKey(imported)] = e
	c.entries = append(c.entries, e)
	if static, ok := imported.(*Module); ok {
		e.module = static
		e.state = newModuleState(static.Name)
		e.state.entry = e
		c.assignUnit(e, static)
	} else {
		e.factory = imported
		e.state = newModuleState(factoryLabel(imported))
		e.state.entry = e
		c.stateByFunc[factoryPointer(imported)] = e.state
		c.hasAsyncFactory = true
	}
	if e.factory != nil || len(e.unitFactories) > 0 {
		e.isDiscovering = true
	}
}

func (c *container) assignUnit(e *asyncEntry, m *Module) {
	if c.isSyncModule(m) {
		return
	}
	if _, ok := c.unitByModule[m]; ok {
		return
	}
	c.unitByModule[m] = e
	for _, list := range [2][]any{m.imports, m.asyncImports} {
		for _, imported := range list {
			if child, ok := imported.(*Module); ok {
				c.assignUnit(e, child)
				continue
			}
			if called, ok := c.moduleByFunc[factoryPointer(imported)]; ok {
				c.assignUnit(e, called)
				continue
			}
			if !slices.ContainsFunc(e.unitFactories, func(fn any) bool { return sameImport(fn, imported) }) {
				e.unitFactories = append(e.unitFactories, imported)
				e.importers[factoryPointer(imported)] = m
				c.hasAsyncFactory = true
			}
		}
	}
}

func (c *container) checkImportedBothWays(root *Module) {
	importedBy := make(map[any]*Module)
	asyncBy := make(map[any]*Module)
	seen := make(map[*Module]struct{})
	var walk func(m *Module)
	walk = func(m *Module) {
		if _, ok := seen[m]; ok {
			return
		}
		seen[m] = struct{}{}
		for _, imported := range m.imports {
			key := importKey(imported)
			if _, ok := importedBy[key]; !ok {
				importedBy[key] = m
			}
			if y, ok := asyncBy[key]; ok {
				panicImportedBothWays(imported, m, y)
			}
		}
		for _, imported := range m.asyncImports {
			key := importKey(imported)
			if _, ok := asyncBy[key]; !ok {
				asyncBy[key] = m
			}
			if x, ok := importedBy[key]; ok {
				panicImportedBothWays(imported, x, m)
			}
		}
		for _, child := range c.resolvedImports(m) {
			walk(child)
		}
	}
	walk(root)
}

func panicImportedBothWays(imported any, x, y *Module) {
	panic(color.FmtRed("invalid module: '%s' is imported with Imports by '%s' and with AsyncImports by '%s'", importLabel(imported), x.Name, y.Name))
}

func (c *container) computeStates(root *Module) {
	for _, m := range c.modules {
		if e, ok := c.unitByModule[m]; ok {
			c.stateByModule[m] = e.state
			e.state.modules = append(e.state.modules, m)
			continue
		}
		s := newModuleState(m.Name)
		s.modules = []*Module{m}
		c.stateByModule[m] = s
		c.states = append(c.states, s)
	}
	for _, e := range c.entries {
		c.states = append(c.states, e.state)
	}

	if len(c.entries) == 0 {
		for _, s := range c.states {
			c.setReadyAtCreate(s)
		}
		c.readyOrder = slices.Clone(c.modules)
		return
	}

	for _, m := range c.modules {
		s := c.stateByModule[m]
		for _, p := range m.providers {
			c.addFieldNeeds(s, reflect.TypeOf(p))
		}
	}
	for _, key := range c.controllerKeysByModule[root] {
		c.addControllerNeeds(key, nil)
	}
	for _, e := range c.entries {
		if e.factory != nil {
			c.addParamNeeds(e.state, e.factory)
		}
	}
	pending := c.pendingStates()

	for _, key := range c.controllerKeysByModule[root] {
		owner := c.ownerByController[key]
		if _, ok := pending[c.stateByModule[owner]]; ok || hasLayerEmbed(reflect.TypeOf(c.controllerByKey[key])) {
			if probe := c.probe(key); !probe.isPanicked {
				c.addControllerNeeds(key, probe.controller)
			}
		}
	}
	pending = c.pendingStates()
	c.checkCycles(pending)

	for _, s := range c.states {
		if _, ok := pending[s]; ok {
			s.isAsync = true
			s.deps = slices.DeleteFunc(s.deps, func(d *moduleState) bool { _, isPending := pending[d]; return !isPending })
			continue
		}
		s.deps = nil
		c.setReadyAtCreate(s)
	}
	for _, m := range c.modules {
		if c.stateByModule[m].status() == moduleReady {
			c.readyOrder = append(c.readyOrder, m)
		}
	}

	for _, key := range c.controllerKeysByModule[root] {
		owner := c.ownerByController[key]
		s := c.stateByModule[owner]
		if s.status() == moduleReady {
			continue
		}
		probe := c.probe(key)
		if probe.isPanicked {
			panic(color.FmtRed(
				"invalid module: NewController of '%v' failed before its module '%s' is ready: %v. NewController must not use injected providers",
				reflect.TypeOf(c.controllerByKey[key]), owner.Name, probe.panicValue,
			))
		}
		gate := c.gateByModule[owner]
		if gate == nil {
			gate = &moduleGate{
				state:           s,
				retry:           c.retryEntries(s),
				indexByPattern:  make(map[string]int),
				indexByEvent:    make(map[string]int),
				routesByControl: make(map[string][]string),
			}
			c.gateByModule[owner] = gate
		}
		gate.controllers = append(gate.controllers, key)
	}
}

func (c *container) setReadyAtCreate(s *moduleState) {
	s.value.Store(uint32(moduleReady))
	close(s.done)
}

func (c *container) isReadyAtCreate(m *Module) bool {
	return c.stateByModule[m].status() == moduleReady
}

func (c *container) pendingStates() map[*moduleState]struct{} {
	pending := make(map[*moduleState]struct{})
	for _, e := range c.entries {
		pending[e.state] = struct{}{}
	}
	for changed := true; changed; {
		changed = false
		for _, s := range c.states {
			if _, ok := pending[s]; ok {
				continue
			}
			isPending := len(s.unknown) > 0
			for _, d := range s.deps {
				if _, ok := pending[d]; ok {
					isPending = true
				}
			}
			if isPending {
				pending[s] = struct{}{}
				changed = true
			}
		}
	}
	return pending
}

func (c *container) checkCycles(pending map[*moduleState]struct{}) {
	const (
		unvisited = iota
		visiting
		visited
	)
	mark := make(map[*moduleState]int)
	var path []*moduleState
	var visit func(s *moduleState)
	visit = func(s *moduleState) {
		mark[s] = visiting
		path = append(path, s)
		for _, d := range s.deps {
			if _, ok := pending[d]; !ok {
				continue
			}
			switch mark[d] {
			case visiting:
				start := slices.Index(path, d)
				labels := make([]string, 0, len(path)-start+1)
				for _, p := range path[start:] {
					labels = append(labels, "'"+p.label+"'")
				}
				labels = append(labels, "'"+d.label+"'")
				panic(color.FmtRed("invalid module: async modules need each other: %s", strings.Join(labels, " → ")))
			case unvisited:
				visit(d)
			}
		}
		path = path[:len(path)-1]
		mark[s] = visited
	}
	for _, e := range c.entries {
		if mark[e.state] == unvisited {
			visit(e.state)
		}
	}
	for _, s := range c.states {
		if _, ok := pending[s]; ok && mark[s] == unvisited {
			visit(s)
		}
	}
}

func (c *container) retryEntries(s *moduleState) []*asyncEntry {
	var out []*asyncEntry
	seen := make(map[*moduleState]struct{})
	var walk func(s *moduleState)
	walk = func(s *moduleState) {
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		if s.entry != nil {
			out = append(out, s.entry)
		}
		if len(s.unknown) > 0 {
			for _, e := range c.entries {
				if e.factory != nil {
					walk(e.state)
				}
			}
		}
		for _, d := range s.deps {
			walk(d)
		}
	}
	walk(s)
	return out
}

func (c *container) probe(key string) *controllerProbe {
	if p, ok := c.probeByController[key]; ok {
		return p
	}
	p := &controllerProbe{}
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				p.isPanicked = true
				p.panicValue = rec
			}
		}()
		p.controller = c.controllerByKey[key].NewController()
	}()
	c.probeByController[key] = p
	return p
}

func (c *container) addControllerNeeds(key string, probe Controller) {
	s := c.stateByModule[c.ownerByController[key]]
	controllerType := reflect.TypeOf(c.controllerByKey[key])
	if probe == nil {
		c.addFieldNeeds(s, controllerType)
		for i := 0; i < controllerType.NumMethod(); i++ {
			method := controllerType.Method(i).Type
			for j := 1; j < method.NumIn(); j++ {
				c.addFieldNeeds(s, method.In(j))
			}
		}
		return
	}
	for _, layer := range common.BoundLayerValues(probe) {
		c.addFieldNeeds(s, reflect.TypeOf(layer))
	}
}

func (c *container) addParamNeeds(s *moduleState, fn any) {
	t := reflect.TypeOf(fn)
	for i := 0; i < t.NumIn(); i++ {
		if t.In(i) != contextType {
			c.addNeed(s, t.In(i))
		}
	}
}

func (c *container) addFieldNeeds(s *moduleState, t reflect.Type) {
	if t == nil {
		return
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		if fieldType := t.Field(i).Type; isInjectedProvider(fieldType) {
			c.addNeed(s, fieldType)
		}
	}
}

func (c *container) addNeed(s *moduleState, t reflect.Type) {
	key := genFieldKey(t)
	if _, ok := c.interfaceByKey[key]; ok {
		return
	}
	owner := c.ownerOfKey(key)
	if owner == nil {
		if c.hasAsyncFactory && isInjectedProvider(t) && !slices.Contains(s.unknown, t) {
			s.unknown = append(s.unknown, t)
		}
		return
	}
	if d := c.stateByModule[owner]; d != nil && d != s && !slices.Contains(s.deps, d) {
		s.deps = append(s.deps, d)
	}
}

func (c *container) ownerOfKey(key string) *Module {
	if owner, ok := c.ownerByKey[key]; ok {
		return owner
	}
	return c.globalOwnerByKey[key]
}

func hasLayerEmbed(t reflect.Type) bool {
	for _, name := range [4]string{"guard", "interceptor", "exceptionFilter", "middleware"} {
		if _, ok := t.FieldByName(fieldNameByRole[name]); ok {
			return true
		}
	}
	return false
}

func (c *container) gateOfController(key string) *moduleGate {
	return c.gateByModule[c.ownerByController[key]]
}

func (c *container) bindProbe(key string, gate *moduleGate) {
	probe := c.probeByController[key].controller
	controllerType := reflect.TypeOf(probe)
	var routes []string
	if http, templateByPattern, ok := c.bindHTTPRoutes(probe); ok {
		for pattern, handler := range common.HTTPRouterMap(&http) {
			name := common.HTTPPatternToFuncNameMap(&http)[pattern]
			c.out.httpMainHandlers = append(c.out.httpMainHandlers, httpMainHandlerOf(controllerType.PkgPath(), pattern, name, handler, templateByPattern[pattern]))
			c.gateByPattern[pattern] = gateRef{gate: gate, index: len(gate.patterns)}
			gate.indexByPattern[pattern] = len(gate.patterns)
			gate.patterns = append(gate.patterns, pattern)
			gate.templates = append(gate.templates, templateByPattern[pattern])
			routes = append(routes, pattern)
		}
	}
	if ws, _, ok := c.bindWSEvents(probe); ok {
		for eventName, handler := range common.WSEventMap(&ws) {
			c.out.wsMainHandlers = append(c.out.wsMainHandlers, common.WSLayer{EventName: eventName, Handler: handler})
			c.gateByEvent[eventName] = gateRef{gate: gate, index: len(gate.events)}
			gate.indexByEvent[eventName] = len(gate.events)
			gate.events = append(gate.events, eventName)
			routes = append(routes, "ws:"+eventName)
		}
	}
	slices.Sort(routes)
	gate.routesByControl[key] = routes
}

func (c *container) stateOf(m *Module) *moduleState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stateByModule[m]
}

func (c *container) status(m *Module) moduleStatus {
	if s := c.stateOf(m); s != nil {
		return s.status()
	}
	return modulePending
}

func (c *container) settled(m *Module) <-chan struct{} {
	return c.stateOf(m).done
}

func (c *container) factoryState(fn any) *moduleState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stateByFunc[factoryPointer(fn)]
}

func (c *container) statusOfFactory(fn any) moduleStatus {
	if s := c.factoryState(fn); s != nil {
		return s.status()
	}
	return modulePending
}

func (c *container) settledFactory(fn any) <-chan struct{} {
	return c.factoryState(fn).done
}

func (c *container) settle(entry *Module, err error) {
	s := c.stateOf(entry)
	if s == nil {
		return
	}
	if err != nil {
		c.fail(s, err.Error(), "")
		return
	}
	c.markReady(s, nil)
}

type stateFailure struct {
	state *moduleState
	label string
	cause string
}

func (c *container) fail(s *moduleState, cause, stack string) {
	c.mu.Lock()
	if c.isStopping || s.status() != modulePending {
		c.mu.Unlock()
		return
	}
	failures := c.failLocked(s, cause, nil)
	failures = append(failures, c.closeDiscoveryLocked(s.entry)...)
	c.broadcastLocked()
	after := time.Since(c.createStart)
	c.mu.Unlock()
	c.logFailures(failures, after, s, stack)
}

func (c *container) logFailures(failures []stateFailure, after time.Duration, panicked *moduleState, stack string) {
	for _, f := range failures {
		if f.state == panicked && stack != "" {
			c.logger.Error("ModuleFailed", "module", f.label, "after", after, "cause", f.cause, "stack", stack)
		} else {
			c.logger.Error("ModuleFailed", "module", f.label, "after", after, "cause", f.cause)
		}
		close(f.state.done)
	}
}

func (c *container) failLocked(s *moduleState, cause string, failures []stateFailure) []stateFailure {
	if s.status() != modulePending {
		return failures
	}
	s.cause = cause
	s.value.Store(uint32(moduleFailed))
	failures = append(failures, stateFailure{state: s, label: s.label, cause: cause})
	for _, t := range c.states {
		if t.status() == modulePending && slices.Contains(t.deps, s) {
			failures = c.failLocked(t, "needs '"+s.label+"'", failures)
		}
	}
	return failures
}

func (c *container) closeDiscoveryLocked(e *asyncEntry) []stateFailure {
	if e == nil || !e.isDiscovering {
		return nil
	}
	e.isDiscovering = false
	return c.resolveUnknownsLocked()
}

func (c *container) resolveUnknownsLocked() []stateFailure {
	for _, s := range c.states {
		if s.status() != modulePending || len(s.unknown) == 0 {
			continue
		}
		unknown := s.unknown
		s.unknown = nil
		for _, t := range unknown {
			c.addNeed(s, t)
		}
		s.deps = slices.DeleteFunc(s.deps, func(d *moduleState) bool { return d.status() == moduleReady })
	}
	if c.hasRunnableDiscoveryLocked() {
		return nil
	}
	var failures []stateFailure
	for _, s := range c.states {
		if s.status() == modulePending && len(s.unknown) > 0 {
			failures = c.failLocked(s, "can't resolve "+s.unknown[0].String(), failures)
		}
	}
	return failures
}

func (c *container) hasRunnableDiscoveryLocked() bool {
	stuck := make(map[*moduleState]struct{})
	for _, s := range c.states {
		if s.status() == modulePending && len(s.unknown) > 0 {
			stuck[s] = struct{}{}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, s := range c.states {
			if _, ok := stuck[s]; ok || s.status() != modulePending {
				continue
			}
			if slices.ContainsFunc(s.deps, func(d *moduleState) bool { _, ok := stuck[d]; return ok }) {
				stuck[s] = struct{}{}
				changed = true
			}
		}
	}
	for _, e := range c.entries {
		if _, ok := stuck[e.state]; e.isDiscovering && e.state.status() == modulePending && !ok {
			return true
		}
	}
	return false
}

func (c *container) broadcastLocked() {
	close(c.changed)
	c.changed = make(chan struct{})
}

func (c *container) markReady(s *moduleState, opened []openedGate) bool {
	c.mu.Lock()
	if c.isStopping || s.status() != modulePending {
		c.mu.Unlock()
		return false
	}
	for _, o := range opened {
		o.gate.live.Store(o.live)
	}
	s.readyAfter = time.Since(c.createStart)
	s.value.Store(uint32(moduleReady))
	c.readyOrder = append(c.readyOrder, s.modules...)
	for _, t := range c.states {
		t.deps = slices.DeleteFunc(t.deps, func(d *moduleState) bool { return d == s })
	}
	c.broadcastLocked()
	modules := slices.Clone(s.modules)
	after := s.readyAfter
	c.mu.Unlock()
	for _, m := range modules {
		c.logger.Info("ModuleReady", "module", m.Name, "after", after)
	}
	close(s.done)
	return true
}

func (c *container) waitingFor(s *moduleState) []string {
	var out []string
	for _, d := range s.deps {
		if d.status() != moduleReady {
			out = append(out, d.label)
		}
	}
	for _, t := range s.unknown {
		out = append(out, "can't resolve "+t.String())
	}
	return out
}

func (c *container) logPending() {
	for _, row := range c.moduleRows() {
		if row.State == modulePending.String() {
			c.logger.Info("ModulePending", "module", row.Name, "waiting_for", row.WaitingFor)
		}
	}
}

func (c *container) moduleRows() []*devtool.ModuleStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	rows := make([]*devtool.ModuleStatus, 0, len(c.modules)+len(c.entries))
	for _, m := range c.modules {
		if s := c.stateByModule[m]; s != nil {
			isEntryRow := s.entry != nil && s.entry.module == m
			rows = append(rows, c.rowOf(m.Name, s, isEntryRow))
		}
	}
	for _, e := range c.entries {
		if len(e.state.modules) == 0 {
			rows = append(rows, c.rowOf(e.state.label, e.state, true))
		}
	}
	return rows
}

func (c *container) rowOf(name string, s *moduleState, isEntryRow bool) *devtool.ModuleStatus {
	status := s.status()
	row := &devtool.ModuleStatus{Name: name, State: status.String(), IsAsync: s.isAsync}
	switch status {
	case modulePending:
		row.WaitingFor = c.waitingFor(s)
	case moduleReady:
		row.ReadyAfterMs = s.readyAfter.Milliseconds()
	case moduleFailed:
		row.LastError = s.cause
	}
	if isEntryRow {
		row.Attempts = int(s.entry.attempts.Load())
		if last := s.entry.lastError.Load(); last != nil && row.LastError == "" {
			row.LastError = *last
		}
	}
	return row
}

func (app *App) checkAppWideLayers() {
	c := app.container
	if len(c.entries) == 0 {
		return
	}
	for _, g := range app.globalGuarders {
		c.checkAppWideLayer("guard", g)
	}
	for _, i := range app.globalInterceptors {
		c.checkAppWideLayer("interceptor", i)
	}
	for _, f := range app.globalExceptionFilters {
		c.checkAppWideLayer("exception filter", f)
	}
	for _, m := range app.globalMiddlewares {
		c.checkAppWideLayer("middleware", m)
	}
	if app.isWSEnabled {
		for _, m := range app.wsConfig.globalMiddlewares {
			c.checkAppWideLayer("middleware", m)
		}
	}
}

func (c *container) checkAppWideLayer(kind string, layer any) {
	t := reflect.TypeOf(layer)
	if t == nil {
		return
	}
	structType := t
	if structType.Kind() == reflect.Pointer {
		structType = structType.Elem()
	}
	if structType.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < structType.NumField(); i++ {
		fieldType := structType.Field(i).Type
		owner := c.ownerOfKey(genFieldKey(fieldType))
		if owner == nil || c.stateByModule[owner] == nil || c.stateByModule[owner].status() == moduleReady {
			continue
		}
		panic(errors.New(color.FmtRed(
			"dependency injection: app-wide %s '%v' injects '%v' of '%s', which is not ready at Create: app-wide layers can't depend on AsyncImports",
			kind, t, fieldType, owner.Name,
		)))
	}
}

var retryAfterValues = buildRetryAfterValues()

const (
	defaultRetryAfter = 5
	maxRetryAfter     = 60
)

func buildRetryAfterValues() [maxRetryAfter + 1][]string {
	var values [maxRetryAfter + 1][]string
	for i := range values {
		values[i] = []string{strconv.Itoa(i)}
	}
	return values
}

func (g *moduleGate) retryAfter() []string {
	var next int64
	for _, e := range g.retry {
		next = max(next, e.nextRetry.Load())
	}
	if next == 0 {
		return retryAfterValues[defaultRetryAfter]
	}
	seconds := (next - time.Now().UnixNano() + int64(time.Second) - 1) / int64(time.Second)
	return retryAfterValues[min(max(seconds, 1), maxRetryAfter)]
}
