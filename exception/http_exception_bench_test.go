package exception

import (
	"testing"
)

func BenchmarkBadRequestException(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = BadRequestException("validation error")
	}
}

func BenchmarkNotFoundException(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = NotFoundException("not found")
	}
}

func BenchmarkInternalServerErrorException(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = InternalServerErrorException("internal error")
	}
}
