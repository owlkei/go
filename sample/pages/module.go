package main

import "go.owlky.com/owlky"

// Module holds the booking pages and their in-memory store.
var Module = func() owlky.Module {
	return owlky.ModuleBuilder().
		Providers(BookingService{}).
		Controllers(BookingPages{}).
		Build()
}
