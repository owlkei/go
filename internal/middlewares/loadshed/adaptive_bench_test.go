package loadshed

import (
	"slices"
	"testing"
)

func BenchmarkLoadShed_L12_CPUBound_5x(b *testing.B) { benchLoad(b, "cpu", 5) }

func BenchmarkLoadShed_L12_CPUBoundNoYield_5x(b *testing.B) { benchLoad(b, "cpu-noyield", 5) }

func BenchmarkLoadShed_L12_CPUBound_0_7x(b *testing.B) { benchLoad(b, "cpu", 0.7) }

func BenchmarkLoadShed_L12_IOBound_5x(b *testing.B) { benchLoad(b, "io", 5) }

func BenchmarkLoadShed_L12_IOBound_0_7x(b *testing.B) { benchLoad(b, "io", 0.7) }

func BenchmarkLoadShed_L13_SlowRouteTuned(b *testing.B) {
	for b.Loop() {
		s := runSteering(b, slowRouteTuned)
		limits := s.limitsBetween(steerAttackAt, steerAttackTo)
		slices.Sort(limits)
		if len(limits) > 0 {
			b.ReportMetric(float64(limits[0]), "limit-min")
			b.ReportMetric(float64(limits[len(limits)/2]), "limit-median")
			b.ReportMetric(float64(limits[len(limits)-1]), "limit-max")
		}
		b.ReportMetric(s.servedShare(), "served/capacity")
	}
}
