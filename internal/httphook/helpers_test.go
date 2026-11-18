package httphook

import (
	"net/http"
	"time"
)

type fakeWriter struct {
	http.ResponseWriter
	inner http.ResponseWriter
	hooks []Hook
}

func (f *fakeWriter) Inner() http.ResponseWriter { return f.inner }

func (f *fakeWriter) SetInner(w http.ResponseWriter) { f.inner = w }

func (f *fakeWriter) OnDone(h Hook) { f.hooks = append(f.hooks, h) }

type countHook struct{ calls int }

func (h *countHook) Done(time.Duration) { h.calls++ }
