package main

import (
	"strconv"
	"strings"
	"time"

	"go.owlky.com/owlky"
)

// BookingForm is the "New booking" form. It is a Form pipe: Transform binds the
// submitted values and validates them. Errors holds one message per invalid
// field, keyed by the field's name; the page shows them next to the fields.
// A pipe's exported fields are injected and unexported ones are refused, so the
// parsed date and nights are computed again by BookingService.Add.
type BookingForm struct {
	Guest  string `bind:"guest"`
	Email  string `bind:"email"`
	Date   string `bind:"date"`
	Nights string `bind:"nights"`
	Errors map[string]string
}

var requiredMessages = map[string]string{
	"guest":  "Enter the guest's name",
	"email":  "Enter an email address",
	"date":   "Choose an arrival date",
	"nights": "Enter the number of nights",
}

func (form BookingForm) Transform(f owlky.Form, _ owlky.ArgumentMetadata) any {
	bound, fields := f.Bind(form)
	dto := bound.(BookingForm)
	dto.Guest = strings.TrimSpace(dto.Guest)
	dto.Email = strings.TrimSpace(dto.Email)
	dto.Errors = map[string]string{}

	// A field that the request did not send, or sent empty, is required.
	for _, field := range fields {
		if value, _ := field.Value().(string); !field.IsValue() || strings.TrimSpace(value) == "" {
			dto.Errors[field.Tag()] = requiredMessages[field.Tag()]
		}
	}

	if dto.Errors["email"] == "" && !strings.Contains(dto.Email, "@") {
		dto.Errors["email"] = "Enter an email address such as ann@example.com"
	}
	if dto.Errors["date"] == "" {
		if _, err := time.Parse(time.DateOnly, dto.Date); err != nil {
			dto.Errors["date"] = "Enter the date as YYYY-MM-DD"
		}
	}
	if dto.Errors["nights"] == "" {
		dto.Nights = strings.TrimSpace(dto.Nights)
		if nights, err := strconv.Atoi(dto.Nights); err != nil || nights < 1 || nights > 30 {
			dto.Errors["nights"] = "Enter a number of nights from 1 to 30"
		}
	}

	return dto
}
