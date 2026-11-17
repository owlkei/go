package accounts

import (
	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/sample/shop/infra"
)

var Module = func() ginject.Module {
	var module = ginject.ModuleBuilder().
		Imports(infra.CacheModule).
		Providers(UserRepository{}, UserService{}).
		Controllers(SessionsController{}).
		Build()

	return module
}
