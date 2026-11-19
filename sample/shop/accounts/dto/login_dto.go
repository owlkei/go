package dto

import (
	"strings"

	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
)

type LoginDTO struct {
	Email    string `bind:"email"`
	Password string `bind:"password"`
}

func (loginDTO LoginDTO) Transform(body owlky.Body, arg owlky.ArgumentMetadata) any {
	bound, _ := body.Bind(loginDTO)
	dto := bound.(LoginDTO)

	dto.Email = strings.TrimSpace(dto.Email)

	if dto.Email == "" || dto.Password == "" {
		panic(exception.BadRequestException("email and password are required"))
	}

	return dto
}
