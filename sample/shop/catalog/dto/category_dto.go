package dto

import (
	"strings"

	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
)

type CategoryDTO struct {
	Name string `bind:"name"`
}

func (categoryDTO CategoryDTO) Transform(body owlky.Body, arg owlky.ArgumentMetadata) any {
	bound, _ := body.Bind(categoryDTO)
	dto := bound.(CategoryDTO)

	dto.Name = strings.TrimSpace(dto.Name)

	if dto.Name == "" {
		panic(exception.BadRequestException("name is required"))
	}

	return dto
}
