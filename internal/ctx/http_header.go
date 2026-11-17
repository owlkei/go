package ctx

import "net/textproto"

const (
	RequestID = "X-Request-Id"
)

type Header map[string][]string

func (c *HTTPContext) Header() Header {
	if c.header != nil {
		return c.header
	}
	if c.Request.Header == nil {
		c.header = Header{}
	} else {
		c.header = Header(c.Request.Header)
	}
	return c.header
}

func (h Header) Get(k string) string {
	return textproto.MIMEHeader(h).Get(k)
}

func (h Header) Set(k, v string) {
	textproto.MIMEHeader(h).Set(k, v)
}

func (h Header) Add(k, v string) {
	textproto.MIMEHeader(h).Add(k, v)
}

func (h Header) Del(k string) {
	textproto.MIMEHeader(h).Del(k)
}

func (h Header) Has(k string) bool {
	k = textproto.CanonicalMIMEHeaderKey(k)
	_, ok := h[k]
	return ok
}

func (h Header) Bind(s any) (any, []FieldLevel) {
	b := binderOfValue(s)
	fls := make([]FieldLevel, 0, len(b.fields))
	return b.bindStrArr(h, true, &fls), fls
}

func (h Header) BindValue(s any) any {
	return binderOfValue(s).bindStrArr(h, true, nil)
}
