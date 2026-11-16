package ctx

import "context"

type Context interface {
	Reset()
	GetID() string
	Context() context.Context
}

type (
	Map         map[string]any
	ErrFunc     func(error)
	HTTPHandler = func(*HTTPContext)
	WSHandler   = func(*WSContext)
	Next        = func()
	Redirect    = func(string)
)
