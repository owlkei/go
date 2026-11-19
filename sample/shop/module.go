package shop

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/sample/shop/accounts"
	"go.owlky.com/owlky/sample/shop/catalog"
)

var Module = func() owlky.Module {
	var module = owlky.ModuleBuilder().
		Imports(accounts.Module, catalog.Module).
		Controllers(UsersController{}).
		Build()

	return module
}
