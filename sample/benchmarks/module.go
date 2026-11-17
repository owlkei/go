package benchmarks

import (
	"github.com/dangduoc08/ginject"
)

var Module = func() ginject.Module {
	var module = ginject.ModuleBuilder().
		Controllers(Controller{}).
		Build()

	return module
}
