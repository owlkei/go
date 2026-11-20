package core

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"go.owlkei.com/owlkei/internal/color"
)

type factoryNode struct {
	fn       any
	parent   *Module
	isCalled bool
}

type factoryResult struct {
	module     *Module
	err        error
	panicValue any
	isPanicked bool
}

func (c *container) wireFactories(root *Module) {
	c.root = root
	c.scanSync(root)
	for c.callNextSyncFactory() {
	}
	c.refuseUncalledFactories()
}

func (c *container) scanSync(m *Module) {
	if _, ok := c.syncModules[m]; ok {
		return
	}
	c.syncModules[m] = struct{}{}
	c.syncOrder = append(c.syncOrder, m)
	for _, imported := range m.imports {
		if child, ok := imported.(*Module); ok {
			c.scanSync(child)
			continue
		}
		if called, ok := c.moduleByFunc[factoryPointer(imported)]; ok {
			c.scanSync(called)
			continue
		}
		if !c.hasFactoryNode(imported) {
			c.factoryNodes = append(c.factoryNodes, &factoryNode{fn: imported, parent: m})
		}
	}
}

func (c *container) hasFactoryNode(fn any) bool {
	for _, node := range c.factoryNodes {
		if sameImport(node.fn, fn) {
			return true
		}
	}
	return false
}

func (c *container) callNextSyncFactory() bool {
	for _, node := range c.factoryNodes {
		if node.isCalled || !c.isSyncFactoryCallable(node) {
			continue
		}
		node.isCalled = true
		args := c.syncFactoryArgs(node)
		m := c.callSyncFactory(node.fn, args)
		c.moduleByFunc[factoryPointer(node.fn)] = m
		c.scanGlobals(m)
		c.scanSync(m)
		return true
	}
	return false
}

func (c *container) isSyncFactoryCallable(node *factoryNode) bool {
	t := reflect.TypeOf(node.fn)
	for i := 0; i < t.NumIn(); i++ {
		if t.In(i) == contextType {
			continue
		}
		if !c.isSyncParamResolvable(node, t.In(i)) {
			return false
		}
	}
	return true
}

func (c *container) isSyncParamResolvable(node *factoryNode, t reflect.Type) bool {
	key := genFieldKey(t)
	if owner := c.siblingDeclarer(node, key); owner != nil {
		return c.isSyncModule(owner) && c.isComplete(owner, map[*Module]struct{}{})
	}
	if owner, ok := c.globalOwnerByKey[key]; ok {
		return c.isSyncModule(owner)
	}
	_, ok := c.interfaceByKey[key]
	return ok
}

func (c *container) siblingDeclarer(node *factoryNode, key string) *Module {
	for _, list := range [2][]any{node.parent.imports, node.parent.asyncImports} {
		for _, entry := range list {
			if sameImport(entry, node.fn) {
				continue
			}
			sibling := c.moduleOfImport(entry)
			if sibling == nil {
				continue
			}
			if owner := c.declarerOf(sibling, key, map[*Module]struct{}{}); owner != nil {
				return owner
			}
		}
	}
	return nil
}

func (c *container) moduleOfImport(entry any) *Module {
	if m, ok := entry.(*Module); ok {
		return m
	}
	return c.moduleByFunc[factoryPointer(entry)]
}

func (c *container) declarerOf(m *Module, key string, seen map[*Module]struct{}) *Module {
	if _, ok := seen[m]; ok {
		return nil
	}
	seen[m] = struct{}{}
	for _, p := range m.providers {
		if genProviderKey(p) == key {
			return m
		}
	}
	for _, child := range c.resolvedImports(m) {
		if owner := c.declarerOf(child, key, seen); owner != nil {
			return owner
		}
	}
	return nil
}

func (c *container) isComplete(m *Module, seen map[*Module]struct{}) bool {
	if _, ok := seen[m]; ok {
		return true
	}
	seen[m] = struct{}{}
	for _, list := range [2][]any{m.imports, m.asyncImports} {
		for _, entry := range list {
			child := c.moduleOfImport(entry)
			if child == nil || !c.isComplete(child, seen) {
				return false
			}
		}
	}
	return true
}

func (c *container) isSyncModule(m *Module) bool {
	_, ok := c.syncModules[m]
	return ok
}

func (c *container) syncFactoryArgs(node *factoryNode) []reflect.Value {
	c.rediscover(c.root)
	t := reflect.TypeOf(node.fn)
	args := make([]reflect.Value, 0, t.NumIn())
	for i := 0; i < t.NumIn(); i++ {
		if t.In(i) == contextType {
			args = append(args, reflect.ValueOf(c.createCtx))
			continue
		}
		key := genFieldKey(t.In(i))
		if owner := c.siblingDeclarer(node, key); owner != nil {
			c.initModule(owner)
			args = append(args, reflect.ValueOf(c.provide(key)))
			continue
		}
		if owner, ok := c.globalOwnerByKey[key]; ok {
			c.initModule(owner)
			p, _ := c.global(key)
			args = append(args, reflect.ValueOf(p))
			continue
		}
		v, _ := c.interfaceFor(key)
		args = append(args, reflect.ValueOf(v))
	}
	return args
}

func (c *container) callSyncFactory(fn any, args []reflect.Value) *Module {
	label := factoryLabel(fn)
	done := make(chan factoryResult, 1)
	go func() {
		done <- c.runFactory(fn, args, func(rec any, stack string) {
			c.logger.Error("ModuleFactoryPanic", "module", label, "panic", rec, "stack", stack)
		})
	}()

	timer := time.NewTimer(time.Until(c.createStart.Add(c.startupTimeout)))
	defer timer.Stop()
	var r factoryResult
	select {
	case r = <-done:
	case <-timer.C:
		panic(color.FmtRed("invalid module: module factory '%s' did not return within the startup timeout (%v)", label, c.startupTimeout))
	}

	if r.isPanicked {
		panic(color.FmtRed("invalid module: module factory '%s' panicked: %v", label, r.panicValue))
	}
	if r.err != nil {
		panic(color.FmtRed("invalid module: module factory '%s' failed: %v", resultLabel(r.module, label), r.err))
	}
	if r.module == nil {
		panic(color.FmtRed("invalid module: module factory '%s' returned a nil module", label))
	}
	return r.module
}

func (c *container) runFactory(fn any, args []reflect.Value, onPanic func(rec any, stack string)) (r factoryResult) {
	defer func() {
		if rec := recover(); rec != nil {
			r = factoryResult{panicValue: rec, isPanicked: true}
			onPanic(rec, panicStack())
		}
	}()
	out := reflect.ValueOf(fn).Call(args)
	r.module, _ = out[0].Interface().(*Module)
	if len(out) == 2 {
		r.err, _ = out[1].Interface().(error)
	}
	return r
}

func resultLabel(m *Module, fallback string) string {
	if m != nil && m.Name != "" {
		return m.Name
	}
	return fallback
}

func (c *container) refuseUncalledFactories() {
	var remaining []*factoryNode
	for _, node := range c.factoryNodes {
		if !node.isCalled {
			remaining = append(remaining, node)
		}
	}
	if len(remaining) == 0 {
		return
	}

	for _, node := range remaining {
		t := reflect.TypeOf(node.fn)
		for i := 0; i < t.NumIn(); i++ {
			if t.In(i) == contextType || c.isSyncParamResolvable(node, t.In(i)) {
				continue
			}
			if owner := c.asyncDeclarer(node, genFieldKey(t.In(i))); owner != nil {
				panic(color.FmtRed(
					"invalid module: module factory '%s' is in Imports and needs '%v' of '%s', which is not ready at Create: import it with AsyncImports",
					factoryLabel(node.fn), t.In(i), owner.Name,
				))
			}
		}
	}

	if len(remaining) == 1 {
		node := remaining[0]
		t := reflect.TypeOf(node.fn)
		i := c.firstUnresolved(node)
		panic(errors.New(color.FmtRed(
			"dependency injection: can't resolve argument of '%v'. Please make sure that the argument '%v' at index [%v] is available in the injected providers",
			strings.Replace(t.String(), ") *core.Module", ")", 1),
			genFieldKey(t.In(i)),
			i,
		)))
	}

	links := make([]string, 0, len(remaining))
	for _, node := range remaining {
		t := reflect.TypeOf(node.fn)
		links = append(links, fmt.Sprintf("'%s' needs '%v'", factoryLabel(node.fn), t.In(c.firstUnresolved(node))))
	}
	panic(errors.New(color.FmtRed(
		"dependency injection: module factories can't be ordered: %s. Nobody provides these types, or the factories need each other",
		strings.Join(links, " → "),
	)))
}

func (c *container) firstUnresolved(node *factoryNode) int {
	t := reflect.TypeOf(node.fn)
	for i := 0; i < t.NumIn(); i++ {
		if t.In(i) != contextType && !c.isSyncParamResolvable(node, t.In(i)) {
			return i
		}
	}
	return 0
}

func (c *container) asyncDeclarer(node *factoryNode, key string) *Module {
	if owner := c.siblingDeclarer(node, key); owner != nil && !c.isSyncModule(owner) {
		return owner
	}
	if owner, ok := c.globalOwnerByKey[key]; ok && !c.isSyncModule(owner) {
		return owner
	}
	return nil
}
