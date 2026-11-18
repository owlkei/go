package main

import (
	"net/http"

	"github.com/dangduoc08/ginject"
	"github.com/dangduoc08/ginject/exception"
)

type BookingPages struct {
	ginject.Page
	Bookings BookingService
}

func (p BookingPages) NewController() ginject.Controller {
	return p
}

func (p BookingPages) READ_bookings() ginject.View {
	return ginject.View{Name: "bookings/list", Data: ginject.Map{"Bookings": p.Bookings.All()}}
}

func (p BookingPages) READ_new_OF_bookings() ginject.View {
	return ginject.View{Name: "bookings/new", Data: BookingForm{Nights: "1"}}
}

func (p BookingPages) READ_bookings_BY_id(param ginject.Param) ginject.View {
	booking, ok := p.Bookings.Find(param.Get("id"))
	if !ok {
		panic(exception.NotFoundException("No booking has this number"))
	}
	return ginject.View{Name: "bookings/detail", Data: booking}
}

func (p BookingPages) CREATE_bookings(c ginject.HTTPContext, form BookingForm) ginject.View {
	if len(form.Errors) > 0 {
		c.Status(http.StatusUnprocessableEntity)
		return ginject.View{Name: "bookings/new", Data: form}
	}
	p.Bookings.Add(form)
	c.Redirect("/bookings")
	return ginject.View{}
}
