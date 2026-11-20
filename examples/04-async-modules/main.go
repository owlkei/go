// Command stock is a stock service that starts before its warehouse does. The warehouse module is
// imported with AsyncImports: the app serves at once and its stock routes switch on when the
// warehouse answers.
package main

import (
	"log"

	"go.owlkei.com/owlkei"
)

func newApp() owlkei.App {
	stock := owlkei.ModuleBuilder().
		AsyncImports(NewWarehouseModule).
		Controllers(StockController{}).
		Build()

	app := owlkei.New(nil)
	app.Create(owlkei.ModuleBuilder().
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
