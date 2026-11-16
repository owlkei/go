package httphook

import (
	"net/http"
	"time"
)

type Hook interface {
	Done(elapsed time.Duration)
}

type Writer interface {
	Inner() http.ResponseWriter
	SetInner(w http.ResponseWriter)
	OnDone(h Hook)
}

func Of(w http.ResponseWriter) Writer {
	hw, _ := w.(Writer)
	return hw
}
