package ctx

import "context"

type Context interface {
	ID() string
	Context() context.Context
}

type (
	Map         map[string]any
	HTTPHandler = func(*HTTPContext)
	WSHandler   = func(*WSContext)
	Next        = func()
	Redirect    = func(string)
)
