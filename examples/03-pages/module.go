package main

import "go.owlky.com/owlky"

func NewBookingsModule() owlky.Module {
	return owlky.ModuleBuilder().
		Providers(BookingService{}).
		Controllers(BookingPages{}).
		Build()
}
