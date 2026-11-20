package main

import (
	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/exception"
)

// StockController injects the warehouse, so its routes answer 503 until the warehouse module is
// ready.
type StockController struct {
	owlkei.HTTP
	Warehouse Warehouse
}

func (c StockController) NewController() owlkei.Controller {
	return c
}

// GET /stock/{sku}
func (c StockController) READ_stock_BY_sku(param owlkei.Param) owlkei.Map {
	sku := param.Get("sku")
	quantity, ok := c.Warehouse.Quantity(sku)
	if !ok {
		panic(exception.NotFoundException("Unknown product " + sku))
	}
	return owlkei.Map{"sku": sku, "quantity": quantity}
}

// HealthController injects nothing from the warehouse, so it answers from the first request.
type HealthController struct {
	owlkei.HTTP
}

func (c HealthController) NewController() owlkei.Controller {
	return c
}

// GET /health
func (c HealthController) READ_health() string {
	return "ok"
}
