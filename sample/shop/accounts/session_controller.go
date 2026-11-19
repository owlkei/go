package accounts

import (
	"strings"

	"go.owlky.com/owlky"
	"go.owlky.com/owlky/sample/shop/accounts/dto"
)

type SessionsController struct {
	owlky.HTTP
	owlky.Guard

	UserService
}

func (instance SessionsController) NewController() owlky.Controller {
	instance.BindGuard(AuthGuard{}, instance.DELETE_sessions)

	return instance
}

func (instance SessionsController) CREATE_sessions(loginDTO dto.LoginDTO) owlky.Map {
	user, token, err := instance.UserService.Authenticate(loginDTO.Email, loginDTO.Password)
	if err != nil {
		panic(err)
	}

	return owlky.Map{
		"user":  user,
		"token": token,
	}
}

func (instance SessionsController) DELETE_sessions(header owlky.Header) owlky.Map {
	token := strings.TrimPrefix(header.Get("Authorization"), "Bearer ")
	instance.UserService.Logout(token)

	return owlky.Map{
		"message": "logged out",
	}
}
