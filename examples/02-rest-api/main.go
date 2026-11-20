// Command library is a REST API for a small library's books: a module, a provider, a validating
// pipe and the errors a client gets back.
package main

import (
	"log"

	"go.owlky.com/owlky"
)

func newApp() owlky.App {
	app := owlky.New(nil)
	app.Create(NewBooksModule())
	return app
}

func main() {
	if err := newApp().Listen(); err != nil {
		log.Fatal(err)
	}
}
