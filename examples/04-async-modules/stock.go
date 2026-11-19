package main

import (
	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
)

// StockController injects the warehouse, so its routes answer 503 until the warehouse module is
// ready.
type StockController struct {
	owlky.HTTP
	Warehouse Warehouse
}

func (c StockController) NewController() owlky.Controller {
	return c
}

// GET /stock/{sku}
func (c StockController) READ_stock_BY_sku(param owlky.Param) owlky.Map {
	sku := param.Get("sku")
	quantity, ok := c.Warehouse.Quantity(sku)
	if !ok {
		panic(exception.NotFoundException("Unknown product " + sku))
	}
	return owlky.Map{"sku": sku, "quantity": quantity}
}

// HealthController injects nothing from the warehouse, so it answers from the first request.
type HealthController struct {
	owlky.HTTP
}

func (c HealthController) NewController() owlky.Controller {
	return c
}

// GET /health
func (c HealthController) READ_health() string {
	return "ok"
}
