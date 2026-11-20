package ctx

import (
	"errors"
	"net/http"
	"strings"

	"go.owlkei.com/owlkei/exception"
)

type Form map[string][]string

const (
	applicationXWWWFormUrlencoded = "application/x-www-form-urlencoded"
	defaultMaxMemory              = 32 << 20
)

func (c *HTTPContext) Form() Form {
	if c.form != nil {
		return c.form
	}

	mt := c.contentType()
	switch {
	case strings.EqualFold(mt, multipartFormData):
		c.parseMultipartForm()
	case strings.EqualFold(mt, applicationXWWWFormUrlencoded):
		if err := c.ParseForm(); err != nil {
			panic(formException(err))
		}
	default:
		c.form = Form{}
		return c.form
	}

	if c.Request.Form == nil {
		c.form = Form{}
	} else {
		c.form = Form(c.Request.Form)
	}
	return c.form
}

func (c *HTTPContext) parseMultipartForm() {
	if err := c.ParseMultipartForm(defaultMaxMemory); err != nil {
		panic(formException(err))
	}
}

func formException(err error) exception.Exception {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return exception.RequestEntityTooLargeException("Request body too large", err)
	}
	return exception.BadRequestException("Invalid form body", err)
}

func (f Form) Get(k string) string {
	fs := f[k]
	if len(fs) == 0 {
		return ""
	}
	return fs[0]
}

func (f Form) Set(k, v string) {
	f[k] = []string{v}
}

func (f Form) Add(k, v string) {
	f[k] = append(f[k], v)
}

func (f Form) Del(k string) {
	delete(f, k)
}

func (f Form) Has(k string) bool {
	_, ok := f[k]
	return ok
}

func (f Form) Bind(s any) (any, []FieldLevel) {
	b := binderOfValue(s)
	fls := make([]FieldLevel, 0, len(b.fields))
	return b.bindStrArr(f, false, &fls), fls
}

func (f Form) BindValue(s any) any {
	return binderOfValue(s).bindStrArr(f, false, nil)
}
