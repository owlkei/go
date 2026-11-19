package catalog

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/sample/shop/accounts"
)

var Module = func() owlky.Module {
	var module = owlky.ModuleBuilder().
		Imports(accounts.Module).
		Providers(StoreRepository{}, StoreService{}).
		Controllers(StoreController{}).
		Build()

	return module
}
