package main

import "github.com/dangduoc08/ginject"

var StockModule = ginject.ModuleBuilder().
	AsyncImports(NewWarehouseModule).
	Controllers(StockController{}).
	Build()

var Module = ginject.ModuleBuilder().
	Imports(StockModule).
	Controllers(HealthController{}).
	Build()
