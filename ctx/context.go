package ctx

import "github.com/dangduoc08/ginject/internal/crypto"

type Context interface {
	Reset()
	SetID()
	GetID() string
}

const requestIDLength = 16

type contextID struct {
	id string
}

func (c *contextID) SetID() {
	if c.id != "" {
		return
	}
	c.id = crypto.RandomHex(requestIDLength)
}

func (c *contextID) GetID() string {
	return c.id
}

type (
	Map         map[string]any
	ErrFunc     func(error)
	HTTPHandler = func(*HTTPContext)
	WSHandler   = func(*WSContext)
	Next        = func()
	Redirect    = func(string)
)
