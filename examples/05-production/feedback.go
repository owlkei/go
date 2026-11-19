package main

import (
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go.owlky.com/owlky"
	"go.owlky.com/owlky/exception"
	"go.owlky.com/owlky/guards/throttler"
)

type Note struct {
	Message  string    `json:"message"`
	Received time.Time `json:"received"`
}

type Inbox struct {
	Box *box
}

type box struct {
	mu    sync.Mutex
	notes []Note
}

func (i Inbox) NewProvider() owlky.Provider {
	return i
}

func (i Inbox) Add(message string) Note {
	i.Box.mu.Lock()
	defer i.Box.mu.Unlock()
	note := Note{Message: message, Received: time.Now().UTC()}
	i.Box.notes = append(i.Box.notes, note)
	return note
}

func (i Inbox) All() []Note {
	i.Box.mu.Lock()
	defer i.Box.mu.Unlock()
	return slices.Clone(i.Box.notes)
}

type NoteInput struct {
	Message string `bind:"message"`
}

func (in NoteInput) Transform(body owlky.Body, _ owlky.ArgumentMetadata) any {
	in = body.BindValue(in).(NoteInput)
	in.Message = strings.TrimSpace(in.Message)
	if in.Message == "" || utf8.RuneCountInString(in.Message) > 1000 {
		panic(exception.UnprocessableEntityException("message must have 1 to 1000 characters"))
	}
	return in
}

type FeedbackController struct {
	owlky.HTTP
	owlky.Guard
	Inbox Inbox
}

// NewController gives posting its own, tighter budget on top of the app-wide one: anyone can post,
// so one client must not fill the inbox.
func (c FeedbackController) NewController() owlky.Controller {
	c.BindGuard(throttler.Throttler{Limit: 5, TTL: time.Minute}, c.CREATE_feedback)
	return c
}

// GET /feedback
func (c FeedbackController) READ_feedback() []Note {
	return c.Inbox.All()
}

// POST /feedback
func (c FeedbackController) CREATE_feedback(in NoteInput) Note {
	return c.Inbox.Add(in.Message)
}

func NewFeedbackModule() owlky.Module {
	return owlky.ModuleBuilder().
		Controllers(FeedbackController{}).
		Providers(Inbox{Box: &box{}}).
		Build()
}
