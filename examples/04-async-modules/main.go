// Command stock is a stock service that starts before its warehouse does. The warehouse module is
// imported with AsyncImports: the app serves at once and its stock routes switch on when the
// warehouse answers.
package main

import (
	"log"

	"go.owlky.com/owlky"
)

func newApp() owlky.App {
	stock := owlky.ModuleBuilder().
		AsyncImports(NewWarehouseModule).
		Controllers(StockController{}).
		Build()

	app := owlky.New(nil)
	app.Create(owlky.ModuleBuilder().
		Imports(stock).
		Controllers(HealthController{}).
		Build())
	return app
}

func main() {
	if err := newApp().Listen(); err != nil {
		log.Fatal(err)
	}
}
