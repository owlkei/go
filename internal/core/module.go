package core

import (
	"github.com/dangduoc08/ginject/internal/str"
)

type Module struct {
	id       string
	Name     string
	prefixes []string

	imports        []any
	staticModules  []*Module
	dynamicModules []any
	providers      []Provider
	controllers    []Controller

	IsGlobal   bool
	OnInit     func()
	OnReady    func()
	OnShutdown func()
}

func (m *Module) Prefix(prefix string) *Module {
	m.prefixes = append([]string{str.Enclose(prefix, '/')}, m.prefixes...)

	return m
}
