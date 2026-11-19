// Command stock is a stock service that starts before its warehouse does. The warehouse module is
// imported with AsyncImports: the app serves at once and its stock routes switch on when the
// warehouse answers.
package main

import (
	"log"
	"os"
	"strconv"

	"go.owlky.com/owlky"
)

func newApp() owlky.App {
	stock := owlky.ModuleBuilder().
		AsyncImports(NewWarehouseModule).
		Controllers(StockController{}).
		Build()

	app := owlky.New()
	app.Create(owlky.ModuleBuilder().
		Imports(stock).
		Controllers(HealthController{}).
		Build())
	return app
}

func main() {
	port := 8081
	if v := os.Getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			log.Fatalf("PORT must be a number, got %q", v)
		}
		port = p
	}
	if err := newApp().Listen(port, nil); err != nil {
		log.Fatal(err)
	}
}
