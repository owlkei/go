package dto

import (
	"strings"

	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
)

type UserDTO struct {
	Email    string `bind:"email"`
	Name     string `bind:"name"`
	Password string `bind:"password"`
}

func (userDTO UserDTO) Transform(body owlky.Body, arg owlky.ArgumentMetadata) any {
	bound, _ := body.Bind(userDTO)
	dto := bound.(UserDTO)

	dto.Email = strings.TrimSpace(dto.Email)
	dto.Name = strings.TrimSpace(dto.Name)

	if dto.Email == "" || dto.Name == "" || dto.Password == "" {
		panic(exception.BadRequestException("email, name and password are required"))
	}
	if !strings.Contains(dto.Email, "@") {
		panic(exception.BadRequestException("email must be a valid email address"))
	}
	if len(dto.Password) < 8 {
		panic(exception.BadRequestException("password must be at least 8 characters long"))
	}

	return dto
}

type QueryDTO struct {
	Email    string `bind:"email"`
	Name     string `bind:"name"`
	Password string `bind:"password"`
}

func (userDTO QueryDTO) Transform(body owlky.Query, arg owlky.ArgumentMetadata) any {
	bound, _ := body.Bind(userDTO)
	dto := bound.(QueryDTO)

	dto.Email = strings.TrimSpace(dto.Email)
	dto.Name = strings.TrimSpace(dto.Name)

	if dto.Email == "" || dto.Name == "" || dto.Password == "" {
		panic(exception.BadRequestException("email, name and password are required"))
	}
	if !strings.Contains(dto.Email, "@") {
		panic(exception.BadRequestException("email must be a valid email address"))
	}
	if len(dto.Password) < 8 {
		panic(exception.BadRequestException("password must be at least 8 characters long"))
	}

	return dto
}
