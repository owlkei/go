package benchmarks

import (
	"github.com/dangduoc08/ginject/core"
)

var Module = func() *core.Module {
	var module = core.ModuleBuilder().
		Controllers(Controller{}).
		Build()

	return module
}
