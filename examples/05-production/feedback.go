package main

import (
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go.owlkei.com/owlkei"
	"go.owlkei.com/owlkei/exception"
	"go.owlkei.com/owlkei/guards/throttler"
)

type Note struct {
	Message  string    `json:"message"`
	Received time.Time `json:"received"`
}

const maxNotes = 500

type Inbox struct {
	Box *box
}

type box struct {
	mu    sync.Mutex
	notes []Note
}

func (ib Inbox) NewProvider() owlkei.Provider {
	return ib
}

func (ib Inbox) Add(message string) Note {
	ib.Box.mu.Lock()
	defer ib.Box.mu.Unlock()
	note := Note{Message: message, Received: time.Now().UTC()}
	ib.Box.notes = append(ib.Box.notes, note)
	// Anyone can post, so the inbox keeps the newest notes only: memory stays bounded however
	// many arrive.
	if len(ib.Box.notes) > maxNotes {
		ib.Box.notes = slices.Delete(ib.Box.notes, 0, len(ib.Box.notes)-maxNotes)
	}
	return note
}

func (ib Inbox) All() []Note {
	ib.Box.mu.Lock()
	defer ib.Box.mu.Unlock()
	return slices.Clone(ib.Box.notes)
}

type NoteInput struct {
	Message string `bind:"message"`
}

func (in NoteInput) Transform(body owlkei.Body, _ owlkei.ArgumentMetadata) any {
	in = body.BindValue(in).(NoteInput)
	in.Message = strings.TrimSpace(in.Message)
	if in.Message == "" || utf8.RuneCountInString(in.Message) > 1000 {
		panic(exception.UnprocessableEntityException("message must have 1 to 1000 characters"))
	}
	return in
}

type FeedbackController struct {
	owlkei.HTTP
	owlkei.Guard
	Inbox Inbox
}

// NewController gives posting its own, tighter budget on top of the app-wide one: anyone can post,
// so one client must not fill the inbox.
func (c FeedbackController) NewController() owlkei.Controller {
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

func NewFeedbackModule() owlkei.Module {
	return owlkei.ModuleBuilder().
		Controllers(FeedbackController{}).
		Providers(Inbox{Box: &box{}}).
		Build()
}
