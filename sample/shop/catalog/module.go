package catalog

import (
	"github.com/dangduoc08/ginject/core"
	"github.com/dangduoc08/ginject/sample/shop/accounts"
)

var Module = func() *core.Module {
	var module = core.ModuleBuilder().
		Imports(accounts.Module).
		Providers(StoreRepository{}, StoreService{}).
		Controllers(StoreController{}).
		Build()

	return module
}
