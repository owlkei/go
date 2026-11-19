package benchmarks

import (
	"go.owlky.com/owlky"
)

var Module = func() owlky.Module {
	var module = owlky.ModuleBuilder().
		Controllers(Controller{}).
		Build()

	return module
}
