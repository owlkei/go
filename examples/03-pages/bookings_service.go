package main

import (
	"strconv"
	"sync"
	"time"

	"go.owlkei.com/owlkei"
)

type Booking struct {
	ID     string
	Guest  string
	Email  string
	Date   time.Time
	Nights int
}

type bookingTable struct {
	mu     sync.RWMutex
	nextID int
	byID   map[string]Booking
	order  []string
}

// BookingService keeps the bookings in memory. Every copy shares one table.
type BookingService struct {
	Table *bookingTable
}

func (s BookingService) NewProvider() owlkei.Provider {
	s.Table = &bookingTable{nextID: 1, byID: map[string]Booking{}}
	return s
}

// All returns the bookings, oldest first.
func (s BookingService) All() []Booking {
	t := s.Table
	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make([]Booking, 0, len(t.order))
	for _, id := range t.order {
		out = append(out, t.byID[id])
	}
	return out
}

func (s BookingService) Find(id string) (Booking, bool) {
	t := s.Table
	t.mu.RLock()
	defer t.mu.RUnlock()

	b, ok := t.byID[id]
	return b, ok
}

// Add stores a form that BookingForm.Transform accepted and returns the new booking.
func (s BookingService) Add(form BookingForm) Booking {
	date, _ := time.Parse(time.DateOnly, form.Date)
	nights, _ := strconv.Atoi(form.Nights)

	t := s.Table
	t.mu.Lock()
	defer t.mu.Unlock()

	b := Booking{
		ID:     strconv.Itoa(t.nextID),
		Guest:  form.Guest,
		Email:  form.Email,
		Date:   date,
		Nights: nights,
	}
	t.nextID++
	t.byID[b.ID] = b
	t.order = append(t.order, b.ID)
	return b
}
