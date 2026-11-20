package memorybroker

import ptrn "go.owlkei.com/owlkei/internal/pattern"

type complexGroup struct {
	pattern  ptrn.Pattern
	subsByID map[string]*subscription
}
