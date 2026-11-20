package core

import (
	"go.owlkei.com/owlkei/internal/common"
	"go.owlkei.com/owlkei/internal/str"
)

type Module struct {
	id        string
	Name      string
	prefixes  []string
	pathStyle common.PathStyle

	imports         []any
	asyncImports    []any
	staticModules   []*Module
	moduleFactories []any
	providers       []Provider
	controllers     []Controller

	IsGlobal   bool
	OnInit     func()
	OnReady    func()
	OnShutdown func()
}

func (m *Module) Prefix(prefix string) *Module {
	m.prefixes = append([]string{str.Enclose(prefix, '/')}, m.prefixes...)

	return m
}
