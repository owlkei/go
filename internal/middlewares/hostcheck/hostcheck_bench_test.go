package hostcheck_test

import (
	"testing"
)

func BenchmarkHostCheck_Hit(b *testing.B) { benchHost(b, "example.com") }

func BenchmarkHostCheck_HitWithPort(b *testing.B) { benchHost(b, "example.com:8080") }

func BenchmarkHostCheck_MixedCase(b *testing.B) { benchHost(b, "EXAMPLE.com") }

func BenchmarkHostCheck_Wildcard(b *testing.B) { benchHost(b, "a.b.example.org") }
