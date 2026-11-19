package shop

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/sample/shop/accounts"
	accountsdto "go.owlky.com/owlky/sample/shop/accounts/dto"
	"go.owlky.com/owlky/sample/shop/catalog"
)

type UsersController struct {
	owlky.HTTP

	accounts.UserService
	catalog.StoreService
}

func (instance UsersController) NewController() owlky.Controller {
	return instance
}

func (instance UsersController) CREATE_users(q accountsdto.QueryDTO, userDTO accountsdto.UserDTO) owlky.Map {
	user, err := instance.UserService.Register(userDTO.Email, userDTO.Name, userDTO.Password)
	if err != nil {
		panic(err)
	}

	store, err := instance.StoreService.CreateStore(user.ID, userDTO.Name+"'s Store")
	if err != nil {
		panic(err)
	}

	return owlky.Map{
		"user":  user,
		"store": store,
	}
}
