package ctx

type Param map[string][]string

func (c *HTTPContext) Param() Param {
	if c.param != nil {
		return c.param
	}

	total := 0
	for _, indexes := range c.ParamKeys {
		total += len(indexes)
	}
	values := make([]string, 0, total)
	param := make(Param, len(c.ParamKeys))
	for key, indexes := range c.ParamKeys {
		start := len(values)
		for _, i := range indexes {
			if i >= 0 && i < len(c.ParamValues) {
				values = append(values, c.ParamValues[i])
			}
		}
		if end := len(values); end > start {
			param[key] = values[start:end:end]
		}
	}

	c.param = param
	return c.param
}

func (p Param) Get(k string) string {
	if p == nil {
		return ""
	}
	ps := p[k]
	if len(ps) == 0 {
		return ""
	}
	return ps[0]
}

func (p Param) Set(k, v string) {
	p[k] = []string{v}
}

func (p Param) Add(k, v string) {
	p[k] = append(p[k], v)
}

func (p Param) Del(k string) {
	delete(p, k)
}

func (p Param) Has(k string) bool {
	_, ok := p[k]
	return ok
}

func (p Param) Bind(s any) (any, []FieldLevel) {
	b := binderOfValue(s)
	fls := make([]FieldLevel, 0, len(b.fields))
	return b.bindStrArr(p, false, &fls), fls
}

func (p Param) BindValue(s any) any {
	return binderOfValue(s).bindStrArr(p, false, nil)
}
