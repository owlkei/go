package shop

import (
	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/sample/shop/accounts"
	"github.com/dangduoc08/ginject/sample/shop/catalog"
)

var Module = func() ginject.Module {
	var module = ginject.ModuleBuilder().
		Imports(accounts.Module, catalog.Module).
		Controllers(UsersController{}).
		Build()

	return module
}
