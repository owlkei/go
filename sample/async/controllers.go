package main

import "github.com/dangduoc08/ginject"

type StockController struct {
	ginject.HTTP
	Warehouse Warehouse
}

func (instance StockController) NewController() ginject.Controller {
	return instance
}

func (instance StockController) READ_stock_BY_sku(param ginject.Param) ginject.Map {
	sku := param.Get("sku")
	return ginject.Map{"sku": sku, "quantity": instance.Warehouse.Quantity(sku)}
}

type HealthController struct {
	ginject.HTTP
}

func (instance HealthController) NewController() ginject.Controller {
	return instance
}

func (instance HealthController) READ_health() string {
	return "ok"
}
