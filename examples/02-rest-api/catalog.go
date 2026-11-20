package main

import (
	"slices"
	"strconv"
	"sync"

	"go.owlkei.com/owlkei"
)

type Book struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Author string `json:"author"`
	Year   int    `json:"year"`
}

// Catalog keeps the library's books in memory. Owlkei Go injects providers by value, so the
// shelf that every copy shares sits behind a pointer.
type Catalog struct {
	Shelf *shelf
}

type shelf struct {
	mu     sync.Mutex
	books  []Book
	nextID int
}

func NewCatalog() Catalog {
	c := Catalog{Shelf: &shelf{nextID: 1}}
	c.Add(Book{Title: "The Go Programming Language", Author: "Alan Donovan", Year: 2015})
	c.Add(Book{Title: "Designing Data-Intensive Applications", Author: "Martin Kleppmann", Year: 2017})
	return c
}

func (c Catalog) NewProvider() owlkei.Provider {
	return c
}

func (c Catalog) All() []Book {
	c.Shelf.mu.Lock()
	defer c.Shelf.mu.Unlock()
	return slices.Clone(c.Shelf.books)
}

func (c Catalog) Find(id string) (Book, bool) {
	c.Shelf.mu.Lock()
	defer c.Shelf.mu.Unlock()
	i := c.Shelf.index(id)
	if i < 0 {
		return Book{}, false
	}
	return c.Shelf.books[i], true
}

func (c Catalog) Add(b Book) Book {
	c.Shelf.mu.Lock()
	defer c.Shelf.mu.Unlock()
	b.ID = strconv.Itoa(c.Shelf.nextID)
	c.Shelf.nextID++
	c.Shelf.books = append(c.Shelf.books, b)
	return b
}

func (c Catalog) Replace(id string, b Book) (Book, bool) {
	c.Shelf.mu.Lock()
	defer c.Shelf.mu.Unlock()
	i := c.Shelf.index(id)
	if i < 0 {
		return Book{}, false
	}
	b.ID = id
	c.Shelf.books[i] = b
	return b, true
}

func (c Catalog) Remove(id string) (Book, bool) {
	c.Shelf.mu.Lock()
	defer c.Shelf.mu.Unlock()
	i := c.Shelf.index(id)
	if i < 0 {
		return Book{}, false
	}
	b := c.Shelf.books[i]
	c.Shelf.books = slices.Delete(c.Shelf.books, i, i+1)
	return b, true
}

func (s *shelf) index(id string) int {
	return slices.IndexFunc(s.books, func(b Book) bool { return b.ID == id })
}
