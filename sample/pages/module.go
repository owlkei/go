package main

import "github.com/dangduoc08/ginject"

// Module holds the booking pages and their in-memory store.
var Module = func() ginject.Module {
	return ginject.ModuleBuilder().
		Providers(BookingService{}).
		Controllers(BookingPages{}).
		Build()
}
