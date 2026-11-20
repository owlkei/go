package main

import (
	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/exception"
)

// BookingPages renders the booking pages. Each RENDER_ route renders the template named by its path
// (web/bookings/index.html for /bookings); the SUBMIT_ route posts to the page that shows its form.
type BookingPages struct {
	owlkei.HTTP
	Bookings BookingService
}

func (p BookingPages) NewController() owlkei.Controller {
	return p
}

// GET /bookings
func (p BookingPages) RENDER_bookings() owlkei.Map {
	return owlkei.Map{"Bookings": p.Bookings.All()}
}

// GET /bookings/{id}
func (p BookingPages) RENDER_bookings_BY_id(param owlkei.Param) Booking {
	booking, ok := p.Bookings.Find(param.Get("id"))
	if !ok {
		panic(exception.NotFoundException("No booking has this number"))
	}
	return booking
}

// GET /bookings/new
func (p BookingPages) RENDER_new_OF_bookings() BookingForm {
	return BookingForm{Nights: "1"}
}

// SUBMIT_new_OF_bookings receives POST /bookings/new. An invalid form is rendered again with 422;
// a valid one is stored and the browser goes to the list with 303.
func (p BookingPages) SUBMIT_new_OF_bookings(ctx owlkei.HTTPContext, form BookingForm) BookingForm {
	if len(form.Errors) == 0 {
		p.Bookings.Add(form)
		ctx.Redirect("/bookings")
	}
	return form
}
