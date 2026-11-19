package main

import "go.owlky.com/owlky"

type StockController struct {
	owlky.HTTP
	Warehouse Warehouse
}

func (instance StockController) NewController() owlky.Controller {
	return instance
}

func (instance StockController) READ_stock_BY_sku(param owlky.Param) owlky.Map {
	sku := param.Get("sku")
	return owlky.Map{"sku": sku, "quantity": instance.Warehouse.Quantity(sku)}
}

type HealthController struct {
	owlky.HTTP
}

func (instance HealthController) NewController() owlky.Controller {
	return instance
}

func (instance HealthController) READ_health() string {
	return "ok"
}
