package catalog

import (
	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/sample/shop/accounts"
)

var Module = func() ginject.Module {
	var module = ginject.ModuleBuilder().
		Imports(accounts.Module).
		Providers(StoreRepository{}, StoreService{}).
		Controllers(StoreController{}).
		Build()

	return module
}
