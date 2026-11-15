package ctx

type WSPayload map[string]any

func (p WSPayload) Bind(s any) (any, []FieldLevel) {
	binder := binderOfValue(s)
	fls := make([]FieldLevel, 0, binder.flHint)
	return binder.bindTop(p, &fls, "", ""), fls
}

func (p WSPayload) BindValue(s any) any {
	return binderOfValue(s).bindTop(p, nil, "", "")
}
