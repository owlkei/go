package common

import (
	"errors"
	"testing"

	"go.owlky.com/owlky/exception"
)

func BenchmarkNormalizeRecovered_Exception(b *testing.B) {
	ex := exception.NotFoundException("nope")
	b.ReportAllocs()
	for b.Loop() {
		_ = NormalizeRecovered(ex)
	}
}

func BenchmarkNormalizeRecovered_Pointer(b *testing.B) {
	ex := exception.NotFoundException("nope")
	b.ReportAllocs()
	for b.Loop() {
		_ = NormalizeRecovered(&ex)
	}
}

func BenchmarkNormalizeRecovered_Error(b *testing.B) {
	err := errors.New("boom")
	b.ReportAllocs()
	for b.Loop() {
		_ = NormalizeRecovered(err)
	}
}
