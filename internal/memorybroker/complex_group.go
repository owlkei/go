package memorybroker

import ptrn "github.com/dangduoc08/ginject/internal/pattern"

type complexGroup struct {
	pattern  ptrn.Pattern
	subsByID map[string]*subscription
}
