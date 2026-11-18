package main

import (
	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
)

// BookingPages renders the booking pages. Each RENDER_ route renders the template named by its path
// (web/bookings/index.html for /bookings); the SUBMIT_ route posts to the page that shows its form.
type BookingPages struct {
	ginject.HTTP
	Bookings BookingService
}

func (p BookingPages) NewController() ginject.Controller {
	return p
}

func (p BookingPages) RENDER_bookings() ginject.Map {
	return ginject.Map{"Bookings": p.Bookings.All()}
}

func (p BookingPages) RENDER_bookings_BY_id(param ginject.Param) Booking {
	booking, ok := p.Bookings.Find(param.Get("id"))
	if !ok {
		panic(exception.NotFoundException("No booking has this number"))
	}
	return booking
}

func (p BookingPages) RENDER_new_OF_bookings() BookingForm {
	return BookingForm{Nights: "1"}
}

// SUBMIT_new_OF_bookings receives POST /bookings/new. An invalid form is rendered again with 422;
// a valid one is stored and the browser goes to the list with 303.
func (p BookingPages) SUBMIT_new_OF_bookings(c ginject.HTTPContext, form BookingForm) BookingForm {
	if len(form.Errors) == 0 {
		p.Bookings.Add(form)
		c.Redirect("/bookings")
	}
	return form
}
