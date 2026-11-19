package main

import "go.owlky.com/owlky"

var StockModule = owlky.ModuleBuilder().
	AsyncImports(NewWarehouseModule).
	Controllers(StockController{}).
	Build()

var Module = owlky.ModuleBuilder().
	Imports(StockModule).
	Controllers(HealthController{}).
	Build()
