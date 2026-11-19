// Command hello is the smallest Owlky Go app: one controller and two routes.
package main

import (
	"log"

	"go.owlky.com/owlky"
)

type GreetingController struct {
	owlky.HTTP
}

func (c GreetingController) NewController() owlky.Controller {
	return c
}

// GET /hello
func (c GreetingController) READ_hello() string {
	return "Hello from Owlky Go"
}

// GET /hello/{name}
func (c GreetingController) READ_hello_BY_name(param owlky.Param) owlky.Map {
	return owlky.Map{"greeting": "Hello, " + param.Get("name")}
}

func newApp() owlky.App {
	app := owlky.New()
	app.Create(owlky.ModuleBuilder().Controllers(GreetingController{}).Build())
	return app
}

func main() {
	if err := newApp().Listen(3000, nil); err != nil {
		log.Fatal(err)
	}
}
