package memorybroker

import ptrn "go.owlky.com/owlky/internal/pattern"

type complexGroup struct {
	pattern  ptrn.Pattern
	subsByID map[string]*subscription
}
