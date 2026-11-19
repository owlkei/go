package accounts

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/sample/shop/infra"
)

var Module = func() owlky.Module {
	var module = owlky.ModuleBuilder().
		Imports(infra.CacheModule).
		Providers(UserRepository{}, UserService{}).
		Controllers(SessionsController{}).
		Build()

	return module
}
