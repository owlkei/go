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

func (form BookingForm) Transform(values owlky.Form, _ owlky.ArgumentMetadata) any {
	bound, fields := values.Bind(form)
	form = bound.(BookingForm)
	form.Guest = strings.TrimSpace(form.Guest)
	form.Email = strings.TrimSpace(form.Email)
	form.Errors = map[string]string{}

	// A field that the request did not send, or sent empty, is required.
	for _, field := range fields {
		if value, _ := field.Value().(string); !field.IsValue() || strings.TrimSpace(value) == "" {
			form.Errors[field.Tag()] = requiredMessages[field.Tag()]
		}
	}

	if form.Errors["email"] == "" && !strings.Contains(form.Email, "@") {
		form.Errors["email"] = "Enter an email address such as ann@example.com"
	}
	if form.Errors["date"] == "" {
		if _, err := time.Parse(time.DateOnly, form.Date); err != nil {
			form.Errors["date"] = "Enter the date as YYYY-MM-DD"
		}
	}
	if form.Errors["nights"] == "" {
		form.Nights = strings.TrimSpace(form.Nights)
		if nights, err := strconv.Atoi(form.Nights); err != nil || nights < 1 || nights > 30 {
			form.Errors["nights"] = "Enter a number of nights from 1 to 30"
		}
	}

	return form
}
