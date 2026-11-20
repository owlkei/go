package main

import "go.owlkei.com/owlkei"

func NewBookingsModule() owlkei.Module {
	return owlkei.ModuleBuilder().
		Providers(BookingService{}).
		Controllers(BookingPages{}).
		Build()
}
