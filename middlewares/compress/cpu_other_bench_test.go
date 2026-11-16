//go:build !unix

package compress

func processCPU() int64 { return 0 }
