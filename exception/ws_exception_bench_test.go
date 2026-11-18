package exception

import (
	"testing"
)

func BenchmarkTopicNotFoundException(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = TopicNotFoundException("no handler registered for topic: chat.to.user1")
	}
}

func BenchmarkPolicyViolationException(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = PolicyViolationException("policy violation")
	}
}

func BenchmarkWSInternalErrorException(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = WSInternalErrorException("internal error")
	}
}
