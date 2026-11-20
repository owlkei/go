package main

import (
	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/exception"
)

type BooksController struct {
	owlkei.HTTP
	Catalog Catalog
}

func (c BooksController) NewController() owlkei.Controller {
	return c
}

// GET /books
func (c BooksController) READ_books() []Book {
	return c.Catalog.All()
}

// GET /books/{id}
func (c BooksController) READ_books_BY_id(param owlkei.Param) Book {
	book, ok := c.Catalog.Find(param.Get("id"))
	if !ok {
		panic(bookNotFound(param.Get("id")))
	}
	return book
}

// POST /books answers 201 with the new book.
func (c BooksController) CREATE_books(in BookInput) Book {
	return c.Catalog.Add(in.Book())
}

// PUT /books/{id}
func (c BooksController) UPDATE_books_BY_id(param owlkei.Param, in BookInput) Book {
	book, ok := c.Catalog.Replace(param.Get("id"), in.Book())
	if !ok {
		panic(bookNotFound(param.Get("id")))
	}
	return book
}

// DELETE /books/{id} answers with the book it removed.
func (c BooksController) DELETE_books_BY_id(param owlkei.Param) Book {
	book, ok := c.Catalog.Remove(param.Get("id"))
	if !ok {
		panic(bookNotFound(param.Get("id")))
	}
	return book
}

func bookNotFound(id string) exception.Exception {
	return exception.NotFoundException("No book with id " + id)
}

// NewBooksModule builds a fresh catalog each time, so every app (and every test) starts from the
// same two books.
func NewBooksModule() owlkei.Module {
	return owlkei.ModuleBuilder().
		Controllers(BooksController{}).
		Providers(NewCatalog()).
		Build()
}
