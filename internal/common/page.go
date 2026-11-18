package common

type Page HTTP

func (p *Page) Prefix(v string, handlers ...any) *Page {
	(*HTTP)(p).Prefix(v, handlers...)
	return p
}

type View struct {
	Name string
	Data any
}
