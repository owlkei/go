package trace

import (
	"context"
	"testing"
)

func BenchmarkTransport_NoScope(b *testing.B) {
	benchTransport(b, context.Background(), "")
}

func BenchmarkTransport_ExistingHeader(b *testing.B) {
	c, _ := scopeContext(b)
	benchTransport(b, c, "00-"+transportTraceID+"-"+transportParent+"-01")
}

func BenchmarkTransport_Scope(b *testing.B) {
	c, _ := scopeContext(b)
	benchTransport(b, c, "")
}
