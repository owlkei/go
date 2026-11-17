package dto

import (
	"strings"

	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
)

type CategoryDTO struct {
	Name string `bind:"name"`
}

func (categoryDTO CategoryDTO) Transform(body ginject.Body, arg ginject.ArgumentMetadata) any {
	bound, _ := body.Bind(categoryDTO)
	dto := bound.(CategoryDTO)

	dto.Name = strings.TrimSpace(dto.Name)

	if dto.Name == "" {
		panic(exception.BadRequestException("name is required"))
	}

	return dto
}
