// Command hello is the smallest Owlkei Go app: one controller and two routes.
package main

import (
	"log"

	"go.owlkei.com/owlkei"
)

type GreetingController struct {
	owlkei.HTTP
}

func (c GreetingController) NewController() owlkei.Controller {
	return c
}

// GET /hello
func (c GreetingController) READ_hello() string {
	return "Hello from Owlkei Go"
}

// GET /hello/{name}
func (c GreetingController) READ_hello_BY_name(param owlkei.Param) owlkei.Map {
	return owlkei.Map{"greeting": "Hello, " + param.Get("name")}
}

func newApp() owlkei.App {
	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().Controllers(GreetingController{}).Build())
	return app
}

func main() {
	if err := newApp().Listen(); err != nil {
		log.Fatal(err)
	}
}
