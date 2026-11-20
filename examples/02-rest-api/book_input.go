package main

import (
	"strings"
	"time"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/exception"
)

// BookInput is the body of POST /books and PUT /books/{id}. As a handler parameter it is a pipe:
// Owlkei Go calls Transform before the handler, and a refused body never reaches it.
type BookInput struct {
	Title  string `bind:"title"`
	Author string `bind:"author"`
	Year   int    `bind:"year"`
}

func (in BookInput) Transform(body owlkei.Body, _ owlkei.ArgumentMetadata) any {
	in = body.BindValue(in).(BookInput)
	in.Title = strings.TrimSpace(in.Title)
	in.Author = strings.TrimSpace(in.Author)

	var problems []string
	if in.Title == "" {
		problems = append(problems, "title is required")
	}
	if in.Author == "" {
		problems = append(problems, "author is required")
	}
	if in.Year < 1450 || in.Year > time.Now().Year() {
		problems = append(problems, "year must be between 1450 and this year")
	}
	if len(problems) > 0 {
		panic(exception.UnprocessableEntityException(strings.Join(problems, "; ")))
	}
	return in
}

func (in BookInput) Book() Book {
	return Book{Title: in.Title, Author: in.Author, Year: in.Year}
}
